package sipua

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mviruch/gb28181-relay/internal/session"
)

// Offer is a parsed subset of GB28181 INVITE SDP we care about.
type Offer struct {
	MediaIP   string
	MediaPort int
	SSRC      string
	Proto     string // RTP/AVP or TCP/RTP/AVP
	Setup     string // active | passive | ""
	Session   string // Play | Playback | ...
}

// ParseOffer extracts media destination / mode hints from an INVITE SDP body.
func ParseOffer(body []byte) (*Offer, error) {
	o := &Offer{Proto: "RTP/AVP"}
	lines := strings.Split(string(body), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch {
		case strings.HasPrefix(line, "c=IN IP4 "):
			o.MediaIP = strings.TrimSpace(strings.TrimPrefix(line, "c=IN IP4 "))
		case strings.HasPrefix(line, "m=video "):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				port, err := strconv.Atoi(parts[1])
				if err != nil {
					return nil, fmt.Errorf("bad m=video port: %w", err)
				}
				o.MediaPort = port
				o.Proto = parts[2]
			}
		case strings.HasPrefix(line, "y="):
			o.SSRC = strings.TrimSpace(strings.TrimPrefix(line, "y="))
		case strings.HasPrefix(line, "a=setup:"):
			o.Setup = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "a=setup:")))
		case strings.HasPrefix(line, "s="):
			o.Session = strings.TrimSpace(strings.TrimPrefix(line, "s="))
		}
	}
	if o.MediaPort == 0 && !strings.Contains(strings.ToUpper(o.Proto), "TCP") {
		return nil, fmt.Errorf("sdp missing m=video port")
	}
	if o.SSRC == "" {
		o.SSRC = "0100000001"
	}
	return o, nil
}

// ModeFromOffer decides ZLM send mode from SDP.
// TCP + setup:active (platform dials) => we are passive.
// TCP + setup:passive (or empty) => we dial actively.
// Otherwise UDP.
func ModeFromOffer(o *Offer) session.MediaMode {
	proto := strings.ToUpper(o.Proto)
	tcp := strings.Contains(proto, "TCP")
	if !tcp {
		return session.ModeUDP
	}
	if o.Setup == "active" {
		return session.ModeTCPPassive
	}
	return session.ModeTCPActive
}

// BuildAnswer crafts a minimal GB28181 answer SDP (device is sender).
func BuildAnswer(publicHost string, localPort int, ssrc string, mode session.MediaMode) string {
	proto := "RTP/AVP"
	var extra strings.Builder
	switch mode {
	case session.ModeTCPPassive:
		proto = "TCP/RTP/AVP"
		extra.WriteString("a=setup:passive\r\n")
		extra.WriteString("a=connection:new\r\n")
	case session.ModeTCPActive:
		proto = "TCP/RTP/AVP"
		extra.WriteString("a=setup:active\r\n")
		extra.WriteString("a=connection:new\r\n")
	}
	var b strings.Builder
	b.WriteString("v=0\r\n")
	b.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", ssrc, publicHost))
	b.WriteString("s=Play\r\n")
	b.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", publicHost))
	b.WriteString("t=0 0\r\n")
	b.WriteString(fmt.Sprintf("m=video %d %s 96\r\n", localPort, proto))
	b.WriteString("a=sendonly\r\n")
	b.WriteString("a=rtpmap:96 PS/90000\r\n")
	b.WriteString(extra.String())
	b.WriteString(fmt.Sprintf("y=%s\r\n", ssrc))
	return b.String()
}
