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

func (d *DeviceUA) platformURI() sip.Uri {
	return sip.Uri{
		User: d.cfg.SIP.ServerID,
		Host: d.cfg.SIP.Host,
		Port: d.cfg.SIP.Port,
	}
}

func (d *DeviceUA) contactURI() sip.Uri {
	return sip.Uri{
		User: d.cfg.DeviceID,
		Host: d.publicHost,
		Port: d.listenPort,
	}
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

func (d *DeviceUA) registerOnce(ctx context.Context) (expires int, err error) {
	recipient := sip.Uri{
		User: d.cfg.SIP.RegisterID,
		Host: d.cfg.SIP.Domain,
	}
	if recipient.Host == "" {
		recipient.Host = d.cfg.SIP.Host
	}
	req := sip.NewRequest(sip.REGISTER, recipient)
	req.SetTransport(d.transport())
	req.AppendHeader(sip.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", d.cfg.DeviceID, d.cfg.SIP.Domain)))
	req.AppendHeader(&sip.FromHeader{
		Address: sip.Uri{User: d.cfg.DeviceID, Host: d.cfg.SIP.Domain},
		Params:  headerParams("tag", d.fromTag),
	})
	req.AppendHeader(sip.NewHeader("Call-ID", d.callID))
	contactParams := headerParams("expires", strconv.Itoa(d.cfg.SIP.Expires))
	req.AppendHeader(&sip.ContactHeader{Address: d.contactURI(), Params: contactParams})
	req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(d.cfg.SIP.Expires)))
	req.SetDestination(fmt.Sprintf("%s:%d", d.cfg.SIP.Host, d.cfg.SIP.Port))

	res, err := d.client.Do(ctx, req, sipgo.ClientRequestRegisterBuild)
	if err != nil {
		return 0, err
	}
	if res.StatusCode == sip.StatusUnauthorized || res.StatusCode == sip.StatusProxyAuthRequired {
		res, err = d.client.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{
			Username: d.cfg.SIP.Username,
			Password: d.cfg.SIP.Password,
		})
		if err != nil {
			return 0, err
		}
	}
	if res.StatusCode != 200 {
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
	recipient := sip.Uri{User: d.cfg.SIP.RegisterID, Host: d.cfg.SIP.Domain}
	if recipient.Host == "" {
		recipient.Host = d.cfg.SIP.Host
	}
	req := sip.NewRequest(sip.REGISTER, recipient)
	req.SetTransport(d.transport())
	req.AppendHeader(sip.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", d.cfg.DeviceID, d.cfg.SIP.Domain)))
	req.AppendHeader(&sip.FromHeader{
		Address: sip.Uri{User: d.cfg.DeviceID, Host: d.cfg.SIP.Domain},
		Params:  headerParams("tag", d.fromTag),
	})
	req.AppendHeader(sip.NewHeader("Call-ID", d.callID))
	req.AppendHeader(&sip.ContactHeader{Address: d.contactURI(), Params: headerParams("expires", "0")})
	req.AppendHeader(sip.NewHeader("Expires", "0"))
	req.SetDestination(fmt.Sprintf("%s:%d", d.cfg.SIP.Host, d.cfg.SIP.Port))

	res, err := d.client.Do(ctx, req, sipgo.ClientRequestRegisterBuild)
	if err != nil {
		return err
	}
	if res.StatusCode == sip.StatusUnauthorized || res.StatusCode == sip.StatusProxyAuthRequired {
		_, err = d.client.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{
			Username: d.cfg.SIP.Username,
			Password: d.cfg.SIP.Password,
		})
		return err
	}
	return nil
}

func (d *DeviceUA) sendKeepalive(ctx context.Context) error {
	body, err := manscdp.Encode(catalog.BuildKeepalive(d.cfg.DeviceID, d.nextSN()))
	if err != nil {
		return err
	}
	recipient := d.platformURI()
	req := sip.NewRequest(sip.MESSAGE, recipient)
	req.SetTransport(d.transport())
	req.AppendHeader(sip.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", d.cfg.SIP.ServerID, d.cfg.SIP.Domain)))
	req.AppendHeader(&sip.FromHeader{
		Address: sip.Uri{User: d.cfg.DeviceID, Host: d.cfg.SIP.Domain},
		Params:  headerParams("tag", sip.GenerateTagN(8)),
	})
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.SetBody(body)
	req.SetDestination(fmt.Sprintf("%s:%d", d.cfg.SIP.Host, d.cfg.SIP.Port))

	res, err := d.client.Do(ctx, req)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("Keepalive status %d", res.StatusCode)
	}
	return nil
}
