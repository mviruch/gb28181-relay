package sipua

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"github.com/mviruch/gb28181-relay/internal/config"
)

func TestBuildRegisterRequest_UsesServerIDInRequestURI(t *testing.T) {
	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent("gb28181-relay"),
		sipgo.WithUserAgentHostname("192.168.1.100"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ua.Close() })

	client, err := sipgo.NewClient(ua,
		sipgo.WithClientHostname("192.168.1.100"),
		sipgo.WithClientPort(5070),
		sipgo.WithClientNAT(),
	)
	if err != nil {
		t.Fatal(err)
	}

	dev := config.DeviceConfig{
		DeviceID: "34020000001320000001",
		SIP: config.DeviceSIPConfig{
			Host:      "43.164.129.212",
			Port:      15060,
			ServerID:  "35020000002000000001",
			Domain:    "3502000000",
			Username:  "34020000001320000001",
			Password:  "secret",
			Transport: "udp",
			Expires:   3600,
			// RegisterID deliberately set to deviceId — must NOT leak into Request-URI
			RegisterID: "34020000001320000001",
		},
	}
	d := newDeviceUA(dev, "192.168.1.100", 5070, client, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	req := d.buildRegisterRequest(3600)
	if err := sipgo.ClientRequestBuild(client, req); err != nil {
		t.Fatal(err)
	}

	wire := req.String()
	t.Logf("wire:\n%s", wire)

	if !strings.HasPrefix(wire, "REGISTER sip:35020000002000000001@3502000000 SIP/2.0") {
		t.Fatalf("Request-URI must be serverId@domain, got start line %q", strings.Split(wire, "\r\n")[0])
	}
	if strings.Contains(wire, "REGISTER sip:34020000001320000001@") {
		t.Fatal("Request-URI must not use deviceId (LiveGBS 400)")
	}
	if !strings.Contains(wire, "From: <sip:34020000001320000001@3502000000>;tag=") {
		t.Fatalf("From AOR missing:\n%s", wire)
	}
	if !strings.Contains(wire, "To: <sip:34020000001320000001@3502000000>") {
		t.Fatalf("To AOR missing:\n%s", wire)
	}
	if !strings.Contains(wire, "Via: SIP/2.0/UDP 192.168.1.100:5070") {
		t.Fatalf("Via missing after ClientRequestBuild (pre-build dumps look truncated):\n%s", wire)
	}
	if !strings.Contains(wire, "CSeq: 1 REGISTER") {
		t.Fatalf("CSeq missing:\n%s", wire)
	}
	if !strings.Contains(wire, "Max-Forwards: 70") {
		t.Fatalf("Max-Forwards missing:\n%s", wire)
	}
	if !strings.Contains(wire, "Contact: <sip:34020000001320000001@192.168.1.100:5070>") {
		t.Fatalf("Contact missing:\n%s", wire)
	}
	if req.Recipient.User != "35020000002000000001" {
		t.Fatalf("Recipient.User=%q want serverId", req.Recipient.User)
	}
}

func TestRegisterRecipient_IgnoresRegisterID(t *testing.T) {
	d := &DeviceUA{cfg: config.DeviceConfig{
		DeviceID: "34020000001320000001",
		SIP: config.DeviceSIPConfig{
			ServerID:   "35020000002000000001",
			Domain:     "3502000000",
			RegisterID: "34020000001320000001",
		},
	}}
	r := d.registerRecipient()
	if r.User != "35020000002000000001" || r.Host != "3502000000" {
		t.Fatalf("got %s@%s", r.User, r.Host)
	}
}

func TestClientRequestRegisterBuild_StripsUser_DoNotUse(t *testing.T) {
	// Document why we avoid ClientRequestRegisterBuild for GB28181.
	ua, err := sipgo.NewUA(sipgo.WithUserAgentHostname("192.168.1.100"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ua.Close() })
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("192.168.1.100"), sipgo.WithClientPort(5070))
	if err != nil {
		t.Fatal(err)
	}
	req := sip.NewRequest(sip.REGISTER, sip.Uri{User: "35020000002000000001", Host: "3502000000"})
	req.SetTransport("UDP")
	if err := sipgo.ClientRequestRegisterBuild(client, req); err != nil {
		t.Fatal(err)
	}
	if req.Recipient.User != "" {
		t.Fatalf("expected RegisterBuild to strip user, got %q", req.Recipient.User)
	}
	if got := req.Recipient.String(); got != "sip:3502000000" {
		t.Fatalf("got %q", got)
	}
}
