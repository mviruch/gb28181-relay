package catalog

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mviruch/gb28181-relay/internal/config"
)

func testDev(n int) config.DeviceConfig {
	chs := make([]config.ChannelConfig, n)
	for i := 0; i < n; i++ {
		chs[i] = config.ChannelConfig{
			ChannelID: fmt.Sprintf("34020000001320000%03d", i+1),
			Name:      fmt.Sprintf("ch%02d", i+1),
		}
	}
	return config.DeviceConfig{
		DeviceID: "34020000001320000001",
		Name:     "relay",
		SIP:      config.DeviceSIPConfig{Domain: "3502000000"},
		Channels: chs,
	}
}

func TestBuildResponseBodies_SinglePage(t *testing.T) {
	bodies, err := BuildResponseBodiesLimit(testDev(1), 7, 800)
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 1 {
		t.Fatalf("pages=%d want 1", len(bodies))
	}
	s := string(bodies[0])
	if !strings.Contains(s, "<SumNum>1</SumNum>") {
		t.Fatalf("missing SumNum: %s", s)
	}
	if !strings.Contains(s, `DeviceList Num="1"`) {
		t.Fatalf("missing DeviceList Num: %s", s)
	}
	if !strings.Contains(s, "<CmdType>Catalog</CmdType>") {
		t.Fatalf("missing CmdType: %s", s)
	}
	if !strings.Contains(s, "<SN>7</SN>") {
		t.Fatalf("missing SN: %s", s)
	}
	for _, field := range []string{
		"<Name>", "<Manufacturer>", "<Model>", "<Owner>",
		"<CivilCode>", "<Address>", "<Parental>", "<ParentID>",
		"<SafetyWay>", "<RegisterWay>", "<Status>", "<Secrecy>",
	} {
		if !strings.Contains(s, field) {
			t.Fatalf("missing Item field %s: %s", field, s)
		}
	}
}

func TestBuildResponseBodies_PagedBySize(t *testing.T) {
	bodies, err := BuildResponseBodiesLimit(testDev(5), 9, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) < 3 {
		t.Fatalf("expected multiple pages, got %d", len(bodies))
	}
	for i, b := range bodies {
		s := string(b)
		if !strings.Contains(s, "<SumNum>5</SumNum>") {
			t.Fatalf("page %d missing SumNum=5: %s", i, s)
		}
		if !strings.Contains(s, "<SN>9</SN>") {
			t.Fatalf("page %d missing SN: %s", i, s)
		}
	}
}

func TestBuildResponseBodies_AllItemsCovered(t *testing.T) {
	n := 12
	bodies, err := BuildResponseBodies(testDev(n), 1)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, b := range bodies {
		s := string(b)
		if !strings.Contains(s, "<SumNum>12</SumNum>") {
			t.Fatalf("bad SumNum: %s", s)
		}
		total += strings.Count(s, "<Item>")
		if len(b) > MaxCatalogBodyBytes {
			t.Fatalf("page exceeds MaxCatalogBodyBytes: %d > %d\n%s", len(b), MaxCatalogBodyBytes, s)
		}
	}
	if total != n {
		t.Fatalf("covered %d items want %d across %d pages", total, n, len(bodies))
	}
}

func TestCatalogItemName_FromConfigAndFallback(t *testing.T) {
	dev := config.DeviceConfig{
		DeviceID: "34020000001320000001",
		Name:     "relay-cam-1",
		SIP:      config.DeviceSIPConfig{Domain: "3502000000"},
		Channels: []config.ChannelConfig{
			{ChannelID: "34020000001320000001", Name: "前门枪机"},
			{ChannelID: "34020000001320000002", Name: ""}, // fallback → channelId
		},
	}
	bodies, err := BuildResponseBodies(dev, 5)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, b := range bodies {
		joined += string(b)
	}
	if !strings.Contains(joined, "<Name>前门枪机</Name>") {
		t.Fatalf("configured channel name missing:\n%s", joined)
	}
	if !strings.Contains(joined, "<Name>34020000001320000002</Name>") {
		t.Fatalf("empty channel name should fall back to channelId:\n%s", joined)
	}
}

func TestDeviceInfoBody_NameAndDeviceName(t *testing.T) {
	dev := config.DeviceConfig{
		DeviceID: "34020000001320000001",
		Name:     "relay-cam-1",
		Channels: []config.ChannelConfig{{ChannelID: "34020000001320000001", Name: "ch1"}},
	}
	body, err := BuildDeviceInfoBody(dev, 123)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "<SN>123</SN>") {
		t.Fatalf("SN: %s", s)
	}
	if !strings.Contains(s, "<DeviceName>relay-cam-1</DeviceName>") {
		t.Fatalf("DeviceName: %s", s)
	}
	if !strings.Contains(s, "<Name>relay-cam-1</Name>") {
		t.Fatalf("Name: %s", s)
	}

	empty := config.DeviceConfig{DeviceID: "34020000001320000099"}
	body2, err := BuildDeviceInfoBody(empty, 1)
	if err != nil {
		t.Fatal(err)
	}
	s2 := string(body2)
	if !strings.Contains(s2, "<DeviceName>34020000001320000099</DeviceName>") {
		t.Fatalf("empty device name should fall back to deviceId: %s", s2)
	}
}
