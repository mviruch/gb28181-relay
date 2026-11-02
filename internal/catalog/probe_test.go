package catalog

import "testing"

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
