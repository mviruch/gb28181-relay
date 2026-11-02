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
	ct, msg, err := manscdp.Decode(body)
	sn := 0
	cmd := string(ct)

	if err != nil {
		// DeviceInfo/DeviceStatus queries are <Query> rooted; manscdp types expect <Response>.
		probe, perr := catalog.ProbeMessage(body)
		if perr != nil {
			r.log.Warn("MANSCDP decode", "err", err, "probeErr", perr, "deviceId", dev.cfg.DeviceID)
			respond(tx, req, 400, "Bad Request", nil)
			return
		}
		cmd = probe.CmdType
		sn = probe.SN
		ct = manscdp.CmdType(cmd)
	} else {
		switch m := msg.(type) {
		case *manscdp.CatalogQuery:
			sn = m.SN
		case *manscdp.Catalog:
			sn = m.SN
		case *manscdp.DeviceInfo:
			sn = m.SN
		case *manscdp.DeviceStatus:
			sn = m.SN
		case *manscdp.Keepalive:
			sn = m.SN
		}
	}

	var xmlBody []byte
	switch ct {
	case manscdp.CmdCatalog:
		xmlBody, err = manscdp.Encode(catalog.BuildResponse(dev.cfg, sn))
	case manscdp.CmdDeviceInfo:
		xmlBody, err = manscdp.Encode(catalog.BuildDeviceInfo(dev.cfg, sn))
	case manscdp.CmdDeviceStatus:
		xmlBody, err = manscdp.Encode(catalog.BuildDeviceStatus(dev.cfg.DeviceID, sn))
	case manscdp.CmdKeepalive:
		respond(tx, req, 200, "OK", nil)
		return
	default:
		r.log.Info("ignore MANSCDP", "cmd", cmd, "deviceId", dev.cfg.DeviceID)
		respond(tx, req, 200, "OK", nil)
		return
	}
	if err != nil {
		r.log.Error("encode MANSCDP", "err", err)
		respond(tx, req, 500, "Server Internal Error", nil)
		return
	}
	res := sip.NewResponseFromRequest(req, 200, "OK", xmlBody)
	res.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	if err := tx.Respond(res); err != nil {
		r.log.Warn("MESSAGE 200 failed", "err", err)
		return
	}
	r.log.Info("MESSAGE answered", "cmd", cmd, "deviceId", dev.cfg.DeviceID, "bytes", len(xmlBody))
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
