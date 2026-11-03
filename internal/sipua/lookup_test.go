package sipua

import (
	"log/slog"
	"os"
	"testing"

	"github.com/emiago/sipgo/sip"

	"github.com/mviruch/gb28181-relay/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func runtimeWithDevices(devs ...config.DeviceConfig) *Runtime {
	r := &Runtime{
		log:     testLogger(),
		devices: make(map[string]*DeviceUA),
	}
	for _, dcfg := range devs {
		r.devices[dcfg.DeviceID] = &DeviceUA{cfg: dcfg, log: r.log}
	}
	return r
}

func inviteReq(uriUser, toUser string) *sip.Request {
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: uriUser, Host: "example.com", Port: 5060})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: toUser, Host: "example.com"}})
	return req
}

func TestLookup_PrefersDeviceID(t *testing.T) {
	shared := "34020000001320000099"
	r := runtimeWithDevices(
		config.DeviceConfig{
			DeviceID: "dev-a",
			Channels: []config.ChannelConfig{{ChannelID: shared, FLVURL: "http://a"}},
		},
		config.DeviceConfig{
			DeviceID: "dev-b",
			Channels: []config.ChannelConfig{{ChannelID: shared, FLVURL: "http://b"}},
		},
	)
	dev, user := r.lookupByRequestURI(inviteReq("dev-a", "dev-a"))
	if dev == nil || dev.cfg.DeviceID != "dev-a" || user != "dev-a" {
		t.Fatalf("got device=%v user=%q", deviceID(dev), user)
	}
	ch, ok := r.findChannel(dev, shared)
	if !ok || ch.FLVURL != "http://a" {
		t.Fatalf("findChannel under resolved device: ok=%v ch=%+v", ok, ch)
	}
}

func TestLookup_UniqueChannelFallback(t *testing.T) {
	r := runtimeWithDevices(
		config.DeviceConfig{
			DeviceID: "dev-a",
			Channels: []config.ChannelConfig{{ChannelID: "ch-only", FLVURL: "http://a"}},
		},
		config.DeviceConfig{
			DeviceID: "dev-b",
			Channels: []config.ChannelConfig{{ChannelID: "ch-other", FLVURL: "http://b"}},
		},
	)
	dev, user := r.lookupByRequestURI(inviteReq("ch-only", "ch-only"))
	if dev == nil || dev.cfg.DeviceID != "dev-a" || user != "ch-only" {
		t.Fatalf("got device=%v user=%q", deviceID(dev), user)
	}
}

func TestLookup_AmbiguousChannelNoMatch(t *testing.T) {
	shared := "ch-shared"
	r := runtimeWithDevices(
		config.DeviceConfig{
			DeviceID: "dev-a",
			Channels: []config.ChannelConfig{{ChannelID: shared, FLVURL: "http://a"}},
		},
		config.DeviceConfig{
			DeviceID: "dev-b",
			Channels: []config.ChannelConfig{{ChannelID: shared, FLVURL: "http://b"}},
		},
	)
	dev, user := r.lookupByRequestURI(inviteReq(shared, shared))
	if dev != nil {
		t.Fatalf("expected no match for ambiguous channelId, got device=%s user=%q", deviceID(dev), user)
	}
	if user != shared {
		t.Fatalf("expected returned user to be Request-URI user %q, got %q", shared, user)
	}
}

func TestLookup_ToHeaderDeviceID(t *testing.T) {
	shared := "ch-shared"
	r := runtimeWithDevices(
		config.DeviceConfig{
			DeviceID: "dev-a",
			Channels: []config.ChannelConfig{{ChannelID: shared, FLVURL: "http://a"}},
		},
		config.DeviceConfig{
			DeviceID: "dev-b",
			Channels: []config.ChannelConfig{{ChannelID: shared, FLVURL: "http://b"}},
		},
	)
	// Request-URI is ambiguous channel; To carries deviceId → resolve by To.
	dev, user := r.lookupByRequestURI(inviteReq(shared, "dev-b"))
	if dev == nil || dev.cfg.DeviceID != "dev-b" || user != "dev-b" {
		t.Fatalf("got device=%v user=%q", deviceID(dev), user)
	}
}

func TestFindChannel_PerDevice(t *testing.T) {
	shared := "ch-shared"
	r := runtimeWithDevices(
		config.DeviceConfig{
			DeviceID: "dev-a",
			Channels: []config.ChannelConfig{{ChannelID: shared, FLVURL: "http://a"}},
		},
		config.DeviceConfig{
			DeviceID: "dev-b",
			Channels: []config.ChannelConfig{{ChannelID: shared, FLVURL: "http://b"}},
		},
	)
	a := r.devices["dev-a"]
	b := r.devices["dev-b"]
	cha, oka := r.findChannel(a, shared)
	chb, okb := r.findChannel(b, shared)
	if !oka || !okb {
		t.Fatalf("both devices should own %q", shared)
	}
	if cha.FLVURL != "http://a" || chb.FLVURL != "http://b" {
		t.Fatalf("wrong channel binding: a=%+v b=%+v", cha, chb)
	}
	if _, ok := r.findChannel(a, "missing"); ok {
		t.Fatal("unexpected match for missing channel")
	}
}

func deviceID(d *DeviceUA) string {
	if d == nil {
		return "<nil>"
	}
	return d.cfg.DeviceID
}
