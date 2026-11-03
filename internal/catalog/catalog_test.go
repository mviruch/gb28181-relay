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
