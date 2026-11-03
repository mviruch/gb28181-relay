package catalog

import (
	"strings"
	"testing"

	"github.com/mviruch/gb28181-relay/internal/config"
)

func TestProbeDeviceInfoQuery(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="GB2312"?>
<Query>
<CmdType>DeviceInfo</CmdType>
<SN>9</SN>
<DeviceID>34020000001320000001</DeviceID>
</Query>`)
	p, err := ProbeMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.CmdType != "DeviceInfo" || p.SN != 9 {
		t.Fatalf("unexpected %+v", p)
	}
}

func TestProbeCatalogQuery_SN123(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="GB2312"?>
<Query>
<CmdType>Catalog</CmdType>
<SN>123</SN>
<DeviceID>34020000001320000001</DeviceID>
</Query>`)
	p, err := ProbeMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.CmdType != "Catalog" || p.SN != 123 || p.DeviceID != "34020000001320000001" {
		t.Fatalf("unexpected %+v", p)
	}

	// Fake Query SN must flow into every Catalog Response page.
	dev := config.DeviceConfig{
		DeviceID: "34020000001320000001",
		Name:     "relay",
		SIP:      config.DeviceSIPConfig{Domain: "3502000000"},
		Channels: []config.ChannelConfig{
			{ChannelID: "34020000001320000001", Name: "ch01"},
			{ChannelID: "34020000001320000002", Name: "ch02"},
		},
	}
	bodies, err := BuildResponseBodiesLimit(dev, p.SN, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) < 1 {
		t.Fatal("no pages")
	}
	for i, b := range bodies {
		s := string(b)
		if !strings.Contains(s, "<SN>123</SN>") {
			t.Fatalf("page %d Response SN != Query SN=123:\n%s", i, s)
		}
		if !strings.Contains(s, "<SumNum>2</SumNum>") {
			t.Fatalf("page %d bad SumNum:\n%s", i, s)
		}
		if !strings.Contains(s, "<CmdType>Catalog</CmdType>") {
			t.Fatalf("page %d missing CmdType:\n%s", i, s)
		}
		if !strings.Contains(s, "<DeviceID>34020000001320000001</DeviceID>") {
			t.Fatalf("page %d missing device DeviceID:\n%s", i, s)
		}
		for _, field := range []string{
			"<Name>", "<Manufacturer>", "<Model>", "<Owner>",
			"<CivilCode>", "<Address>", "<Parental>", "<ParentID>",
			"<SafetyWay>", "<RegisterWay>", "<Status>", "<Secrecy>",
		} {
			if !strings.Contains(s, field) {
				t.Fatalf("page %d missing Item field %s:\n%s", i, field, s)
			}
		}
	}
}

func TestProbeCatalogQuery_AttrFormSN(t *testing.T) {
	raw := []byte(`<Query CmdType="Catalog" SN="77"><DeviceID>34020000001320000001</DeviceID></Query>`)
	p, err := ProbeMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.CmdType != "Catalog" || p.SN != 77 {
		t.Fatalf("unexpected %+v", p)
	}
}
