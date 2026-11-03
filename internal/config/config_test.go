package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validBase() Config {
	return Config{
		ZLM: ZLMConfig{API: "http://127.0.0.1:80", Secret: "s"},
		Devices: []DeviceConfig{
			{
				DeviceID: "dev-a",
				SIP: DeviceSIPConfig{
					Host: "10.0.0.1", ServerID: "plat-a", Domain: "3402000000",
					Password: "p", Transport: "udp",
				},
				Channels: []ChannelConfig{
					{ChannelID: "ch-shared", FLVURL: "http://x/a.flv"},
				},
			},
			{
				DeviceID: "dev-b",
				SIP: DeviceSIPConfig{
					Host: "10.0.0.2", ServerID: "plat-b", Domain: "3402000000",
					Password: "p", Transport: "udp",
				},
				Channels: []ChannelConfig{
					{ChannelID: "ch-shared", FLVURL: "http://x/b.flv"},
				},
			},
		},
	}
}

func TestValidate_AllowsSameChannelIDAcrossDevices(t *testing.T) {
	cfg := validBase()
	if err := cfg.validate(); err != nil {
		t.Fatalf("expected cross-device same channelId to be accepted, got: %v", err)
	}
}

func TestValidate_RejectsDuplicateChannelIDWithinDevice(t *testing.T) {
	cfg := validBase()
	cfg.Devices = cfg.Devices[:1]
	cfg.Devices[0].Channels = []ChannelConfig{
		{ChannelID: "ch-dup", FLVURL: "http://x/1.flv"},
		{ChannelID: "ch-dup", FLVURL: "http://x/2.flv"},
	}
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected same-device duplicate channelId to be rejected")
	}
	if !strings.Contains(err.Error(), "duplicate channelId") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_RejectsDuplicateDeviceID(t *testing.T) {
	cfg := validBase()
	cfg.Devices[1].DeviceID = cfg.Devices[0].DeviceID
	cfg.Devices[1].Channels[0].ChannelID = "ch-other"
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected duplicate deviceId to be rejected")
	}
	if !strings.Contains(err.Error(), "duplicate deviceId") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_CrossDeviceSameChannelID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	yaml := `
listen:
  publicHost: "127.0.0.1"
zlm:
  api: "http://127.0.0.1:80"
  secret: "s"
devices:
  - deviceId: "dev-a"
    sip:
      host: "10.0.0.1"
      serverId: "plat-a"
      domain: "3402000000"
      password: "p"
    channels:
      - channelId: "ch-shared"
        flvUrl: "http://x/a.flv"
  - deviceId: "dev-b"
    sip:
      host: "10.0.0.2"
      serverId: "plat-b"
      domain: "3402000000"
      password: "p"
    channels:
      - channelId: "ch-shared"
        flvUrl: "http://x/b.flv"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Devices) != 2 {
		t.Fatalf("devices=%d", len(cfg.Devices))
	}
	if cfg.Devices[0].Channels[0].ChannelID != "ch-shared" || cfg.Devices[1].Channels[0].ChannelID != "ch-shared" {
		t.Fatalf("expected shared channelId on both devices")
	}
}
