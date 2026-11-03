package sipua

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/mickeyzzc/gb28181-go/manscdp"

	"github.com/mviruch/gb28181-relay/internal/catalog"
	"github.com/mviruch/gb28181-relay/internal/config"
)

// DeviceUA owns REGISTER + keepalive for one configured device.
type DeviceUA struct {
	cfg        config.DeviceConfig
	listenPort int
	publicHost string
	client     *sipgo.Client
	log        *slog.Logger

	sn      atomic.Int64
	regCSeq atomic.Uint32
	regOK   atomic.Bool
	callID  string
	fromTag string
}

func newDeviceUA(dev config.DeviceConfig, publicHost string, listenPort int, client *sipgo.Client, log *slog.Logger) *DeviceUA {
	d := &DeviceUA{
		cfg:        dev,
		listenPort: listenPort,
		publicHost: publicHost,
		client:     client,
		log:        log.With("deviceId", dev.DeviceID),
		callID:     sip.GenerateBranchN(16) + "@" + publicHost,
		fromTag:    sip.GenerateTagN(8),
	}
	d.sn.Store(1)
	return d
}

func (d *DeviceUA) nextSN() int {
	return int(d.sn.Add(1))
}

func (d *DeviceUA) nextRegCSeq() uint32 {
	return d.regCSeq.Add(1)
}

func (d *DeviceUA) syncRegCSeqFrom(req *sip.Request) {
	if cseq := req.CSeq(); cseq != nil {
		for {
			cur := d.regCSeq.Load()
			if cseq.SeqNo <= cur {
				return
			}
			if d.regCSeq.CompareAndSwap(cur, cseq.SeqNo) {
				return
			}
		}
	}
}

func (d *DeviceUA) platformURI() sip.Uri {
	return sip.Uri{
		User: d.cfg.SIP.ServerID,
		Host: d.cfg.SIP.Domain,
	}
}

func (d *DeviceUA) contactURI() sip.Uri {
	return sip.Uri{
		User: d.cfg.DeviceID,
		Host: d.publicHost,
		Port: d.listenPort,
	}
}

func (d *DeviceUA) aorURI() sip.Uri {
	return sip.Uri{
		User: d.cfg.DeviceID,
		Host: d.cfg.SIP.Domain,
	}
}

// registerRecipient is the GB28181 / LiveGBS REGISTER Request-URI: sip:{serverId}@{domain}.
// From/To stay deviceId@domain (AOR). Do NOT put deviceId in Request-URI — platforms
// like LiveGBS answer 400 Bad Request. Also do NOT use sipgo.ClientRequestRegisterBuild:
// it strips userinfo per RFC 3261, which GB upper platforms still expect.
func (d *DeviceUA) registerRecipient() sip.Uri {
	host := d.cfg.SIP.Domain
	if host == "" {
		host = d.cfg.SIP.Host
	}
	return sip.Uri{User: d.cfg.SIP.ServerID, Host: host}
}

func (d *DeviceUA) transport() string {
	return strings.ToUpper(d.cfg.SIP.Transport)
}

// RunRegisterLoop keeps REGISTER + Keepalive alive until ctx is done.
func (d *DeviceUA) RunRegisterLoop(ctx context.Context) {
	backoff := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		expires, err := d.registerOnce(ctx)
		if err != nil {
			d.regOK.Store(false)
			d.log.Error("REGISTER failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		d.regOK.Store(true)
		backoff = time.Second
		d.log.Info("REGISTER ok", "expires", expires)

		reRegAt := time.Duration(expires) * time.Second * 2 / 3
		if reRegAt < 30*time.Second {
			reRegAt = 30 * time.Second
		}
		kaEvery := time.Duration(d.cfg.SIP.Keepalive) * time.Second
		reRegTimer := time.NewTimer(reRegAt)
		kaTicker := time.NewTicker(kaEvery)

		alive := true
		for alive {
			select {
			case <-ctx.Done():
				reRegTimer.Stop()
				kaTicker.Stop()
				_ = d.unregister(context.Background())
				return
			case <-reRegTimer.C:
				alive = false
			case <-kaTicker.C:
				if err := d.sendKeepalive(ctx); err != nil {
					d.log.Warn("Keepalive failed", "err", err)
					alive = false
				}
			}
		}
		reRegTimer.Stop()
		kaTicker.Stop()
	}
}

func (d *DeviceUA) buildRegisterRequest(expires int) *sip.Request {
	recipient := d.registerRecipient()
	req := sip.NewRequest(sip.REGISTER, recipient)
	req.SetTransport(d.transport())

	aor := d.aorURI()
	req.AppendHeader(&sip.ToHeader{Address: aor})
	fromParams := sip.NewParams()
	fromParams.Add("tag", d.fromTag)
	req.AppendHeader(&sip.FromHeader{Address: aor, Params: fromParams})

	cid := sip.CallIDHeader(d.callID)
	req.AppendHeader(&cid)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: d.nextRegCSeq(), MethodName: sip.REGISTER})

	contactParams := headerParams("expires", strconv.Itoa(expires))
	req.AppendHeader(&sip.ContactHeader{Address: d.contactURI(), Params: contactParams})
	req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	req.SetDestination(fmt.Sprintf("%s:%d", d.cfg.SIP.Host, d.cfg.SIP.Port))
	return req
}

func (d *DeviceUA) registerOnce(ctx context.Context) (expires int, err error) {
	req := d.buildRegisterRequest(d.cfg.SIP.Expires)

	// ClientRequestBuild fills Via/Max-Forwards/Content-Length but keeps Request-URI user
	// (unlike ClientRequestRegisterBuild, which clears userinfo per RFC 3261).
	res, err := d.client.Do(ctx, req, sipgo.ClientRequestBuild)
	// req.String() AFTER Do includes Via/CSeq added by the build option (pre-build dumps look "missing" them).
	d.log.Info("REGISTER outbound", "request", req.String())
	if err != nil {
		return 0, err
	}
	if res.StatusCode == sip.StatusUnauthorized || res.StatusCode == sip.StatusProxyAuthRequired {
		d.log.Info("REGISTER challenge", "status", res.StatusCode, "response", res.String())
		res, err = d.client.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{
			Username: d.cfg.SIP.Username,
			Password: d.cfg.SIP.Password,
		})
		d.syncRegCSeqFrom(req)
		d.log.Info("REGISTER auth outbound", "request", req.String())
		if err != nil {
			return 0, err
		}
	}
	if res.StatusCode != 200 {
		d.log.Warn("REGISTER rejected", "status", res.StatusCode, "reason", res.Reason, "response", res.String())
		return 0, fmt.Errorf("REGISTER status %d", res.StatusCode)
	}
	exp := d.cfg.SIP.Expires
	if h := res.GetHeader("Expires"); h != nil {
		if v, e := strconv.Atoi(strings.TrimSpace(h.Value())); e == nil && v > 0 {
			exp = v
		}
	}
	return exp, nil
}

func (d *DeviceUA) unregister(ctx context.Context) error {
	req := d.buildRegisterRequest(0)

	res, err := d.client.Do(ctx, req, sipgo.ClientRequestBuild)
	d.log.Info("UNREGISTER outbound", "request", req.String())
	if err != nil {
		return err
	}
	if res.StatusCode == sip.StatusUnauthorized || res.StatusCode == sip.StatusProxyAuthRequired {
		_, err = d.client.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{
			Username: d.cfg.SIP.Username,
			Password: d.cfg.SIP.Password,
		})
		d.syncRegCSeqFrom(req)
		return err
	}
	return nil
}

func (d *DeviceUA) sendKeepalive(ctx context.Context) error {
	body, err := manscdp.Encode(catalog.BuildKeepalive(d.cfg.DeviceID, d.nextSN()))
	if err != nil {
		return err
	}
	return d.sendMANSCDP(ctx, body)
}

// sendMANSCDP sends an outbound SIP MESSAGE with a MANSCDP XML body to the
// upper platform. Used for Keepalive and for Catalog/DeviceInfo/DeviceStatus
// responses (GB practice: empty 200 to the query, then Request MESSAGE).
func (d *DeviceUA) sendMANSCDP(ctx context.Context, body []byte) error {
	recipient := d.platformURI()
	req := sip.NewRequest(sip.MESSAGE, recipient)
	req.SetTransport(d.transport())
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: d.cfg.SIP.ServerID, Host: d.cfg.SIP.Domain}})
	fromParams := sip.NewParams()
	fromParams.Add("tag", sip.GenerateTagN(8))
	req.AppendHeader(&sip.FromHeader{
		Address: d.aorURI(),
		Params:  fromParams,
	})
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.SetBody(body)
	req.SetDestination(fmt.Sprintf("%s:%d", d.cfg.SIP.Host, d.cfg.SIP.Port))

	res, err := d.client.Do(ctx, req)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("MESSAGE status %d", res.StatusCode)
	}
	return nil
}
