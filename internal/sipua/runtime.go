package sipua

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"github.com/mviruch/gb28181-relay/internal/config"
	"github.com/mviruch/gb28181-relay/internal/session"
	"github.com/mviruch/gb28181-relay/internal/zlm"
)

// Runtime is the shared SIP UAS/UAC for all configured devices.
type Runtime struct {
	cfg     *config.Config
	log     *slog.Logger
	zlm     *zlm.Client
	sess    *session.Manager
	ua      *sipgo.UserAgent
	client  *sipgo.Client
	server  *sipgo.Server
	dialog  *sipgo.DialogServerCache
	devices map[string]*DeviceUA // deviceId -> ua
	mu      sync.RWMutex
}

// NewRuntime wires SIP stack + per-device UAs.
func NewRuntime(cfg *config.Config, log *slog.Logger) (*Runtime, error) {
	if log == nil {
		log = slog.Default()
	}
	z := zlm.New(cfg.ZLM.API, cfg.ZLM.Secret, cfg.ZLM.VHost, cfg.ZLM.App)
	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent("gb28181-relay"),
		sipgo.WithUserAgentHostname(cfg.Listen.PublicHost),
	)
	if err != nil {
		return nil, err
	}
	client, err := sipgo.NewClient(ua,
		sipgo.WithClientHostname(cfg.Listen.PublicHost),
		sipgo.WithClientPort(cfg.Listen.Port),
		sipgo.WithClientNAT(),
	)
	if err != nil {
		_ = ua.Close()
		return nil, err
	}
	server, err := sipgo.NewServer(ua)
	if err != nil {
		_ = ua.Close()
		return nil, err
	}
	contact := sip.ContactHeader{
		Address: sip.Uri{Host: cfg.Listen.PublicHost, Port: cfg.Listen.Port},
	}
	r := &Runtime{
		cfg:     cfg,
		log:     log,
		zlm:     z,
		sess:    session.NewManager(z, log),
		ua:      ua,
		client:  client,
		server:  server,
		dialog:  sipgo.NewDialogServerCache(client, contact),
		devices: make(map[string]*DeviceUA),
	}
	for _, dcfg := range cfg.Devices {
		d := newDeviceUA(dcfg, cfg.Listen.PublicHost, cfg.Listen.Port, client, log)
		r.devices[dcfg.DeviceID] = d
	}
	r.server.OnMessage(r.onMessage)
	r.server.OnInvite(r.onInvite)
	r.server.OnAck(r.onAck)
	r.server.OnBye(r.onBye)
	return r, nil
}

// Start listens for SIP and runs per-device register loops.
func (r *Runtime) Start(ctx context.Context) error {
	addr := r.cfg.ListenAddr()
	transports := map[string]struct{}{}
	for _, d := range r.cfg.Devices {
		transports[strings.ToLower(d.SIP.Transport)] = struct{}{}
	}
	if len(transports) == 0 {
		transports["udp"] = struct{}{}
	}
	for t := range transports {
		network := t
		go func() {
			r.log.Info("SIP listen", "network", network, "addr", addr)
			if err := r.server.ListenAndServe(ctx, network, addr); err != nil && ctx.Err() == nil {
				r.log.Error("SIP listen ended", "network", network, "err", err)
			}
		}()
	}
	for _, d := range r.devices {
		dev := d
		go dev.RunRegisterLoop(ctx)
	}
	<-ctx.Done()
	r.sess.CloseAll(context.Background())
	return ctx.Err()
}

// Close releases the UA.
func (r *Runtime) Close() error {
	if r.ua != nil {
		return r.ua.Close()
	}
	return nil
}

// lookupByRequestURI resolves the DeviceUA for an inbound MESSAGE/INVITE.
//
// Prefer deviceId (Request-URI user, then To user). channelId may repeat across
// devices, so there is no global channel→device map: a channel-only URI is
// accepted only when exactly one configured device owns that channelId.
// Ambiguous channelIds (shared by ≥2 devices) yield no match — never a silent
// wrong-device bind. Platforms should INVITE/MESSAGE sip:{deviceId}@… (or put
// the deviceId in To); channel-only Request-URI is best-effort when unique.
func (r *Runtime) lookupByRequestURI(req *sip.Request) (*DeviceUA, string) {
	user := req.Recipient.User
	r.mu.RLock()
	defer r.mu.RUnlock()

	if d, ok := r.devices[user]; ok {
		return d, user
	}
	if to := req.To(); to != nil {
		if d, ok := r.devices[to.Address.User]; ok {
			return d, to.Address.User
		}
	}

	if d, ok := r.findUniqueDeviceByChannel(user); ok {
		return d, user
	}
	if to := req.To(); to != nil && to.Address.User != user {
		if d, ok := r.findUniqueDeviceByChannel(to.Address.User); ok {
			return d, to.Address.User
		}
	}
	return nil, user
}

// findUniqueDeviceByChannel returns the single device that owns channelID.
// Caller must hold r.mu (at least RLock). Zero or multiple owners → false.
func (r *Runtime) findUniqueDeviceByChannel(channelID string) (*DeviceUA, bool) {
	if channelID == "" {
		return nil, false
	}
	var match *DeviceUA
	for _, d := range r.devices {
		if _, ok := r.findChannel(d, channelID); ok {
			if match != nil {
				r.log.Warn("ambiguous channelId across devices; refusing silent bind",
					"channelId", channelID,
					"deviceA", match.cfg.DeviceID,
					"deviceB", d.cfg.DeviceID,
				)
				return nil, false
			}
			match = d
		}
	}
	if match == nil {
		return nil, false
	}
	return match, true
}

func (r *Runtime) findChannel(dev *DeviceUA, channelID string) (config.ChannelConfig, bool) {
	for _, ch := range dev.cfg.Channels {
		if ch.ChannelID == channelID {
			return ch, true
		}
	}
	// Some platforms INVITE the deviceId when it equals the only channel.
	if channelID == dev.cfg.DeviceID && len(dev.cfg.Channels) == 1 {
		return dev.cfg.Channels[0], true
	}
	return config.ChannelConfig{}, false
}

func respond(tx sip.ServerTransaction, req *sip.Request, code int, reason string, body []byte, extra ...sip.Header) {
	res := sip.NewResponseFromRequest(req, code, reason, body)
	for _, h := range extra {
		res.AppendHeader(h)
	}
	if err := tx.Respond(res); err != nil {
		slog.Default().Warn("respond failed", "code", code, "err", err)
	}
}

func headerParams(k, v string) sip.HeaderParams {
	p := sip.NewParams()
	p.Add(k, v)
	return p
}
