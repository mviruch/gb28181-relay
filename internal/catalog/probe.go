package catalog

import (
	"encoding/xml"
	"strings"

	"github.com/mickeyzzc/gb28181-go/manscdp"
)

// Probe is a minimal MANSCDP head used when Decode rejects Query-rooted
// DeviceInfo / DeviceStatus (manscdp types are Response-rooted).
type Probe struct {
	XMLName xml.Name
	CmdType string `xml:"CmdType"`
	SN      int    `xml:"SN"`
	DeviceID string `xml:"DeviceID"`
}

// ProbeMessage normalizes charset and reads CmdType/SN/DeviceID.
func ProbeMessage(data []byte) (Probe, error) {
	body, err := manscdp.UTF8Body(data)
	if err != nil {
		return Probe{}, err
	}
	var p Probe
	if err := xml.Unmarshal(body, &p); err != nil {
		return Probe{}, err
	}
	p.CmdType = strings.TrimSpace(p.CmdType)
	return p, nil
}
