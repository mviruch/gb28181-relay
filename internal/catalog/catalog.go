package catalog

import (
	"encoding/xml"

	"github.com/mickeyzzc/gb28181-go/manscdp"
	"github.com/mviruch/gb28181-relay/internal/config"
)

// MaxCatalogBodyBytes keeps one SIP MESSAGE under sipgo's UDP MTU guard
// (UDPMTUSize-200 ≈ 1300). Headers typically consume 400–500 bytes.
const MaxCatalogBodyBytes = 800

// catalogItem is a slim A.2.1 channel entry (mandatory fields only).
type catalogItem struct {
	DeviceID     string `xml:"DeviceID"`
	Name         string `xml:"Name"`
	Manufacturer string `xml:"Manufacturer"`
	Model        string `xml:"Model"`
	Owner        string `xml:"Owner"`
	CivilCode    string `xml:"CivilCode"`
	Address      string `xml:"Address"`
	Parental     int    `xml:"Parental"`
	ParentID     string `xml:"ParentID"`
	SafetyWay    int    `xml:"SafetyWay"`
	RegisterWay  int    `xml:"RegisterWay"`
	Secrecy      int    `xml:"Secrecy"`
	Status       string `xml:"Status"`
}

type deviceList struct {
	Num  int           `xml:"Num,attr"`
	Item []catalogItem `xml:"Item"`
}

// catalogResponse is one Catalog Response page (GB/T 28181 A.2.6.3).
// SumNum is the TOTAL across all pages; DeviceList.Num is this page's count.
type catalogResponse struct {
	XMLName    xml.Name   `xml:"Response"`
	CmdType    string     `xml:"CmdType"`
	SN         int        `xml:"SN"`
	DeviceID   string     `xml:"DeviceID"`
	SumNum     int        `xml:"SumNum"`
	DeviceList deviceList `xml:"DeviceList"`
}

// BuildResponse builds a single-page Catalog Response (all channels).
// Prefer BuildResponseBodies for UDP-safe multi-MESSAGE paging.
func BuildResponse(dev config.DeviceConfig, sn int) manscdp.Catalog {
	items := channelItems(dev)
	manscdpItems := make([]manscdp.Item, 0, len(items))
	for _, it := range items {
		manscdpItems = append(manscdpItems, manscdp.Item{
			DeviceID:     it.DeviceID,
			Name:         it.Name,
			Manufacturer: it.Manufacturer,
			Model:        it.Model,
			Owner:        it.Owner,
			CivilCode:    it.CivilCode,
			Address:      it.Address,
			Parental:     it.Parental,
			ParentID:     it.ParentID,
			SafetyWay:    it.SafetyWay,
			RegisterWay:  it.RegisterWay,
			Secrecy:      it.Secrecy,
			Status:       it.Status,
		})
	}
	return manscdp.Catalog{
		CmdType:  manscdp.CmdCatalog,
		SN:       sn,
		DeviceID: dev.DeviceID,
		SumNum:   len(manscdpItems),
		Item:     manscdpItems,
	}
}

// BuildResponseBodies encodes Catalog as one or more MANSCDP XML bodies.
// Same SN and SumNum on every page; DeviceList is fragmented so each body
// stays under MaxCatalogBodyBytes (sipgo UDP MTU).
func BuildResponseBodies(dev config.DeviceConfig, sn int) ([][]byte, error) {
	return BuildResponseBodiesLimit(dev, sn, MaxCatalogBodyBytes)
}

// BuildResponseBodiesLimit is like BuildResponseBodies with an explicit size cap.
func BuildResponseBodiesLimit(dev config.DeviceConfig, sn int, maxBody int) ([][]byte, error) {
	if maxBody < 200 {
		maxBody = 200
	}
	items := channelItems(dev)
	sum := len(items)
	if sum == 0 {
		body, err := encodeCatalogPage(dev.DeviceID, sn, 0, nil)
		if err != nil {
			return nil, err
		}
		return [][]byte{body}, nil
	}

	var out [][]byte
	for start := 0; start < sum; {
		end := start
		var page []byte
		for end < sum {
			trial, err := encodeCatalogPage(dev.DeviceID, sn, sum, items[start:end+1])
			if err != nil {
				return nil, err
			}
			if len(trial) > maxBody && end > start {
				break
			}
			page = trial
			end++
			if len(trial) > maxBody {
				// Single item already over budget — still emit (unavoidable).
				break
			}
		}
		out = append(out, page)
		start = end
	}
	return out, nil
}

func encodeCatalogPage(deviceID string, sn, sumNum int, items []catalogItem) ([]byte, error) {
	resp := catalogResponse{
		CmdType:  string(manscdp.CmdCatalog),
		SN:       sn,
		DeviceID: deviceID,
		SumNum:   sumNum,
		DeviceList: deviceList{
			Num:  len(items),
			Item: items,
		},
	}
	return manscdp.Encode(resp)
}

func channelItems(dev config.DeviceConfig) []catalogItem {
	items := make([]catalogItem, 0, len(dev.Channels))
	for _, ch := range dev.Channels {
		items = append(items, catalogItem{
			DeviceID:     ch.ChannelID,
			Name:         ch.Name,
			Manufacturer: "relay",
			Model:        "FLV",
			Owner:        "Owner",
			CivilCode:    civilCode(dev),
			Address:      "-",
			Parental:     0,
			ParentID:     dev.DeviceID,
			SafetyWay:    0,
			RegisterWay:  1,
			Secrecy:      0,
			Status:       "ON",
		})
	}
	return items
}

// BuildKeepalive builds a Keepalive Notify body.
func BuildKeepalive(deviceID string, sn int) manscdp.Keepalive {
	return manscdp.Keepalive{
		CmdType:  manscdp.CmdKeepalive,
		SN:       sn,
		DeviceID: deviceID,
		Status:   "OK",
	}
}

// BuildDeviceInfo builds a minimal DeviceInfo response.
func BuildDeviceInfo(dev config.DeviceConfig, sn int) manscdp.DeviceInfo {
	return manscdp.DeviceInfo{
		CmdType:      manscdp.CmdDeviceInfo,
		SN:           sn,
		DeviceID:     dev.DeviceID,
		DeviceName:   dev.Name,
		Manufacturer: "relay",
		Model:        "FLV",
		Firmware:     "0.1.0",
		Channel:      len(dev.Channels),
		Result:       "OK",
	}
}

// BuildDeviceStatus builds a minimal DeviceStatus response.
func BuildDeviceStatus(deviceID string, sn int) manscdp.DeviceStatus {
	return manscdp.DeviceStatus{
		CmdType:  manscdp.CmdDeviceStatus,
		SN:       sn,
		DeviceID: deviceID,
		Status:   "ONLINE",
	}
}

func civilCode(dev config.DeviceConfig) string {
	if len(dev.SIP.Domain) >= 6 {
		return dev.SIP.Domain[:6]
	}
	if len(dev.DeviceID) >= 6 {
		return dev.DeviceID[:6]
	}
	return "000000"
}
