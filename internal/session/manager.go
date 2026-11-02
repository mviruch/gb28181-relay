package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/mviruch/gb28181-relay/internal/zlm"
)

// MediaMode describes how RTP is sent toward the upper platform.
type MediaMode int

const (
	ModeUDP MediaMode = iota
	ModeTCPActive     // we dial platform (startSendRtp is_udp=0)
	ModeTCPPassive    // platform dials us (startSendRtpPassive)
)

// Session holds one live INVITE media path.
type Session struct {
	CallID    string
	DeviceID  string
	ChannelID string
	Stream    string // ZLM stream id
	SSRC      string
	Mode      MediaMode
	LocalPort int
	ProxyKey  bool // true if we created addStreamProxy for this stream
}

// Manager tracks live sessions and cleans ZLM resources.
type Manager struct {
	mu   sync.Mutex
	byID map[string]*Session // call-id -> session
	zlm  *zlm.Client
	log  *slog.Logger
}

func NewManager(z *zlm.Client, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		byID: make(map[string]*Session),
		zlm:  z,
		log:  log,
	}
}

// Put stores a session (replacing any previous one with same CallID).
func (m *Manager) Put(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.byID[s.CallID]; ok {
		go m.cleanup(context.Background(), old)
	}
	m.byID[s.CallID] = s
}

// Get returns a session by Call-ID.
func (m *Manager) Get(callID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[callID]
	return s, ok
}

// Remove deletes and cleans a session by Call-ID.
func (m *Manager) Remove(ctx context.Context, callID string) {
	m.mu.Lock()
	s, ok := m.byID[callID]
	if ok {
		delete(m.byID, callID)
	}
	m.mu.Unlock()
	if ok {
		m.cleanup(ctx, s)
	}
}

// CloseAll cleans every session.
func (m *Manager) CloseAll(ctx context.Context) {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.byID))
	for _, s := range m.byID {
		all = append(all, s)
	}
	m.byID = make(map[string]*Session)
	m.mu.Unlock()
	for _, s := range all {
		m.cleanup(ctx, s)
	}
}

func (m *Manager) cleanup(ctx context.Context, s *Session) {
	if s == nil {
		return
	}
	if err := m.zlm.StopSendRtp(ctx, s.Stream, s.SSRC); err != nil {
		m.log.Warn("stopSendRtp", "callId", s.CallID, "stream", s.Stream, "err", err)
	}
	if s.ProxyKey {
		if err := m.zlm.DelStreamProxy(ctx, s.Stream); err != nil {
			m.log.Warn("delStreamProxy", "callId", s.CallID, "stream", s.Stream, "err", err)
		}
	}
	m.log.Info("session cleaned", "callId", s.CallID, "channel", s.ChannelID, "stream", s.Stream)
}

// StreamName builds a stable-enough ZLM stream id for one invite.
func StreamName(channelID, callID string) string {
	safe := callID
	if len(safe) > 16 {
		safe = safe[:16]
	}
	return fmt.Sprintf("%s_%s", channelID, safe)
}
