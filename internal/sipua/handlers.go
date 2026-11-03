package sipua

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/mickeyzzc/gb28181-go/manscdp"

	"github.com/mviruch/gb28181-relay/internal/catalog"
	"github.com/mviruch/gb28181-relay/internal/session"
)

func (r *Runtime) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	dev, _ := r.lookupByRequestURI(req)
	if dev == nil {
		respond(tx, req, 404, "Not Found", nil)
		return
	}

	body := req.Body()

	// Always extract CmdType+SN via tolerant XML probe. manscdp.Decode returns
	// value types (not *T), so a type-switch on *CatalogQuery silently misses
	// and left SN=0 — LiveGBS then re-queries forever.
	probe, perr := catalog.ProbeMessage(body)
	if perr != nil {
		r.log.Warn("MANSCDP probe failed", "err", perr, "deviceId", dev.cfg.DeviceID, "bodyLen", len(body))
		respond(tx, req, 400, "Bad Request", nil)
		return
	}
	cmd := probe.CmdType
	sn := probe.SN
	ct := manscdp.CmdType(cmd)
	r.log.Info("inbound MESSAGE",
		"deviceId", dev.cfg.DeviceID,
		"cmd", cmd,
		"sn", sn,
		"bodyLen", len(body),
		"root", probe.XMLName.Local,
	)

	// GB/T 28181 + LiveGBS: answer the query MESSAGE with empty 200 OK, then
	// push the MANSCDP Response as outbound Request MESSAGE(s). Putting a
	// large Catalog XML in the 200 body hits sipgo's UDP MTU guard
	// (ErrUDPMTUCongestion: "size of packet larger than MTU").
	switch ct {
	case manscdp.CmdCatalog:
		respond(tx, req, 200, "OK", nil)
		bodies, encErr := catalog.BuildResponseBodies(dev.cfg, sn)
		if encErr != nil {
			r.log.Error("encode Catalog", "err", encErr)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for i, page := range bodies {
			if err := dev.sendMANSCDP(ctx, page); err != nil {
				r.log.Warn("Catalog MESSAGE failed", "err", err, "page", i+1, "pages", len(bodies), "bytes", len(page))
				return
			}
			r.log.Info("Catalog MESSAGE sent", "deviceId", dev.cfg.DeviceID, "page", i+1, "pages", len(bodies), "bytes", len(page), "sn", sn)
		}
		return
	case manscdp.CmdDeviceInfo:
		respond(tx, req, 200, "OK", nil)
		xmlBody, encErr := catalog.BuildDeviceInfoBody(dev.cfg, sn)
		if encErr != nil {
			r.log.Error("encode DeviceInfo", "err", encErr)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := dev.sendMANSCDP(ctx, xmlBody); err != nil {
			r.log.Warn("DeviceInfo MESSAGE failed", "err", err)
			return
		}
		r.log.Info("DeviceInfo MESSAGE sent", "deviceId", dev.cfg.DeviceID, "bytes", len(xmlBody), "sn", sn)
		return
	case manscdp.CmdDeviceStatus:
		respond(tx, req, 200, "OK", nil)
		xmlBody, encErr := manscdp.Encode(catalog.BuildDeviceStatus(dev.cfg.DeviceID, sn))
		if encErr != nil {
			r.log.Error("encode DeviceStatus", "err", encErr)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := dev.sendMANSCDP(ctx, xmlBody); err != nil {
			r.log.Warn("DeviceStatus MESSAGE failed", "err", err)
			return
		}
		r.log.Info("DeviceStatus MESSAGE sent", "deviceId", dev.cfg.DeviceID, "bytes", len(xmlBody), "sn", sn)
		return
	case manscdp.CmdKeepalive:
		respond(tx, req, 200, "OK", nil)
		return
	default:
		r.log.Info("ignore MANSCDP", "cmd", cmd, "sn", sn, "deviceId", dev.cfg.DeviceID)
		respond(tx, req, 200, "OK", nil)
		return
	}
}

func (r *Runtime) onInvite(req *sip.Request, tx sip.ServerTransaction) {
	dev, uriUser := r.lookupByRequestURI(req)
	if dev == nil {
		respond(tx, req, 404, "Not Found", nil)
		return
	}
	ch, ok := r.findChannel(dev, uriUser)
	if !ok {
		if sub := req.GetHeader("Subject"); sub != nil {
			parts := strings.Split(sub.Value(), ",")
			if len(parts) > 0 {
				id := strings.Split(parts[0], ":")[0]
				ch, ok = r.findChannel(dev, strings.TrimSpace(id))
				if ok {
					uriUser = ch.ChannelID
				}
			}
		}
	}
	if !ok {
		r.log.Warn("INVITE unknown channel", "user", uriUser, "deviceId", dev.cfg.DeviceID)
		respond(tx, req, 404, "Not Found", nil)
		return
	}

	dlg, err := r.dialog.ReadInvite(req, tx)
	if err != nil {
		r.log.Error("ReadInvite", "err", err)
		respond(tx, req, 500, "Server Internal Error", nil)
		return
	}

	offer, err := ParseOffer(req.Body())
	if err != nil {
		r.log.Warn("SDP parse", "err", err)
		_ = dlg.Respond(400, "Bad Request", nil)
		return
	}
	if strings.EqualFold(offer.Session, "Playback") || strings.EqualFold(offer.Session, "Download") {
		r.log.Info("reject non-live INVITE", "s", offer.Session)
		_ = dlg.Respond(488, "Not Acceptable Here", nil)
		return
	}

	mode := ModeFromOffer(offer)
	callID := ""
	if h := req.CallID(); h != nil {
		callID = h.Value()
	}
	stream := session.StreamName(ch.ChannelID, callID)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := r.zlm.AddStreamProxy(ctx, stream, ch.FLVURL); err != nil {
		r.log.Error("addStreamProxy", "err", err, "url", ch.FLVURL)
		_ = dlg.Respond(503, "Service Unavailable", nil)
		return
	}

	var localPort int
	switch mode {
	case session.ModeTCPPassive:
		localPort, err = r.zlm.StartSendRtpPassive(ctx, stream, offer.SSRC, 0)
	case session.ModeTCPActive:
		if offer.MediaIP == "" || offer.MediaPort == 0 {
			_ = r.zlm.DelStreamProxy(ctx, stream)
			_ = dlg.Respond(500, "Server Internal Error", nil)
			return
		}
		localPort, err = r.zlm.StartSendRtp(ctx, stream, offer.SSRC, offer.MediaIP, offer.MediaPort, false, 0)
	default:
		if offer.MediaIP == "" || offer.MediaPort == 0 {
			_ = r.zlm.DelStreamProxy(ctx, stream)
			_ = dlg.Respond(500, "Server Internal Error", nil)
			return
		}
		localPort, err = r.zlm.StartSendRtp(ctx, stream, offer.SSRC, offer.MediaIP, offer.MediaPort, true, 0)
	}
	if err != nil {
		r.log.Error("startSendRtp", "err", err, "mode", mode)
		_ = r.zlm.StopSendRtp(ctx, stream, offer.SSRC)
		_ = r.zlm.DelStreamProxy(ctx, stream)
		_ = dlg.Respond(503, "Service Unavailable", nil)
		return
	}
	if localPort == 0 {
		localPort = offer.MediaPort
		if localPort == 0 {
			localPort = 10000
		}
	}

	answer := BuildAnswer(r.cfg.Listen.PublicHost, localPort, offer.SSRC, mode)
	if err := dlg.RespondSDP([]byte(answer)); err != nil {
		r.log.Error("RespondSDP", "err", err)
		_ = r.zlm.StopSendRtp(ctx, stream, offer.SSRC)
		_ = r.zlm.DelStreamProxy(ctx, stream)
		return
	}

	r.sess.Put(&session.Session{
		CallID:    callID,
		DeviceID:  dev.cfg.DeviceID,
		ChannelID: ch.ChannelID,
		Stream:    stream,
		SSRC:      offer.SSRC,
		Mode:      mode,
		LocalPort: localPort,
		ProxyKey:  true,
	})
	r.log.Info("INVITE 200",
		"deviceId", dev.cfg.DeviceID,
		"channel", ch.ChannelID,
		"mode", fmt.Sprintf("%d", mode),
		"ssrc", offer.SSRC,
		"dst", fmt.Sprintf("%s:%d", offer.MediaIP, offer.MediaPort),
		"localPort", localPort,
		"stream", stream,
	)
}

func (r *Runtime) onAck(req *sip.Request, tx sip.ServerTransaction) {
	if err := r.dialog.ReadAck(req, tx); err != nil {
		r.log.Debug("ACK unmatched", "err", err)
	}
}

func (r *Runtime) onBye(req *sip.Request, tx sip.ServerTransaction) {
	callID := ""
	if h := req.CallID(); h != nil {
		callID = h.Value()
	}
	if err := r.dialog.ReadBye(req, tx); err != nil {
		r.log.Debug("BYE dialog", "err", err)
		respond(tx, req, 200, "OK", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r.sess.Remove(ctx, callID)
	r.log.Info("BYE cleaned", "callId", callID)
}
