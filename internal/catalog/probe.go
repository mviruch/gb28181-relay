package catalog

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/mickeyzzc/gb28181-go/manscdp"
)

// Probe is a minimal MANSCDP head: CmdType + SN + DeviceID from any root
// (Query / Response / Notify / Control). Prefer this over manscdp.Decode for
// routing — Decode returns value types (not pointers) and rejects some
// Query-rooted messages that Response-typed structs cannot unmarshal.
type Probe struct {
	XMLName  xml.Name
	CmdType  string `xml:"CmdType"`
	SN       int    `xml:"SN"`
	DeviceID string `xml:"DeviceID"`
	// Attribute-form aliases (rare firmwares / some platforms).
	CmdTypeAttr string `xml:"CmdType,attr"`
	SNAttr      int    `xml:"SN,attr"`
}

// ProbeMessage normalizes charset and reads CmdType/SN/DeviceID from any
// MANSCDP MESSAGE body without caring about the root element name.
func ProbeMessage(data []byte) (Probe, error) {
	body, err := manscdp.UTF8Body(data)
	if err != nil {
		return Probe{}, err
	}
	var p Probe
	if err := xml.Unmarshal(body, &p); err != nil {
		return Probe{}, err
	}
	if strings.TrimSpace(p.CmdType) == "" {
		p.CmdType = p.CmdTypeAttr
	}
	if p.SN == 0 && p.SNAttr != 0 {
		p.SN = p.SNAttr
	}
	p.CmdType = strings.TrimSpace(p.CmdType)
	p.DeviceID = strings.TrimSpace(p.DeviceID)
	if p.CmdType == "" {
		return Probe{}, fmt.Errorf("catalog: missing CmdType")
	}
	return p, nil
}
