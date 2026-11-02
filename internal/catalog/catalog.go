package catalog

import (
	"github.com/mickeyzzc/gb28181-go/manscdp"
	"github.com/mviruch/gb28181-relay/internal/config"
)

// BuildResponse builds a MANSCDP Catalog Response for a device and its channels.
func BuildResponse(dev config.DeviceConfig, sn int) manscdp.Catalog {
	items := make([]manscdp.Item, 0, len(dev.Channels))
	for _, ch := range dev.Channels {
		items = append(items, manscdp.Item{
			DeviceID:     ch.ChannelID,
			Name:         ch.Name,
			ParentID:     dev.DeviceID,
			Parental:     0,
			Status:       "ON",
			Manufacturer: "gb28181-relay",
			Model:        "FLV-Proxy",
			Owner:        "Owner",
			CivilCode:    civilCode(dev),
			Address:      "Address",
			RegisterWay:  1,
			Secrecy:      0,
		})
	}
	return manscdp.Catalog{
		CmdType:  manscdp.CmdCatalog,
		SN:       sn,
		DeviceID: dev.DeviceID,
		SumNum:   len(items),
		Item:     items,
	}
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
		Manufacturer: "gb28181-relay",
		Model:        "FLV-Proxy",
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
