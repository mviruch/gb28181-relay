package catalog

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mickeyzzc/gb28181-go/manscdp"
	"github.com/mviruch/gb28181-relay/internal/config"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestEncodeMANSCDP_CatalogNameGBK(t *testing.T) {
	dev := config.DeviceConfig{
		DeviceID: "34020000001320000001",
		SIP:      config.DeviceSIPConfig{Domain: "3502000000"},
		Channels: []config.ChannelConfig{
			{ChannelID: "34020000001320000001", Name: "前"},
			{ChannelID: "34020000001320000002", Name: "前门枪机"},
		},
	}
	bodies, err := BuildResponseBodies(dev, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) == 0 {
		t.Fatal("no bodies")
	}
	joined := bytes.Join(bodies, nil)
	if !bytes.Contains(joined, []byte(`encoding="GB2312"`)) {
		t.Fatalf("missing GB2312 declaration:\n%x", joined)
	}
	// 前 in GBK is C7 B0 — must appear as raw GBK, not UTF-8 E5 89 8D.
	if !bytes.Contains(joined, []byte{0xC7, 0xB0}) {
		t.Fatalf("Name 前 not GBK-encoded (want C7 B0):\n%x", joined)
	}
	if bytes.Contains(joined, []byte("前")) { // UTF-8 literal E5 89 8D
		t.Fatalf("Name still UTF-8 in body:\n%x", joined)
	}
	// Whole document with 前门枪机 must not be valid UTF-8 (BB FA breaks UTF-8).
	if utf8.Valid(joined) {
		t.Fatalf("encoded body unexpectedly valid UTF-8 (Chinese should be GBK)")
	}

	utf8Body, err := manscdp.UTF8Body(joined)
	if err != nil {
		t.Fatal(err)
	}
	s := string(utf8Body)
	if !strings.Contains(s, "<Name>前</Name>") {
		t.Fatalf("round-trip missing 前:\n%s", s)
	}
	if !strings.Contains(s, "<Name>前门枪机</Name>") {
		t.Fatalf("round-trip missing 前门枪机:\n%s", s)
	}
}

func TestEncodeMANSCDP_DeviceInfoChinese(t *testing.T) {
	dev := config.DeviceConfig{
		DeviceID: "34020000001320000001",
		Name:     "前",
		Channels: []config.ChannelConfig{{ChannelID: "34020000001320000001", Name: "ch1"}},
	}
	body, err := BuildDeviceInfoBody(dev, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`encoding="GB2312"`)) {
		t.Fatalf("decl: %s", body)
	}
	// Single 前 (GBK C7 B0) is also a valid UTF-8 sequence (U+01F0), so
	// manscdp.UTF8Body / CharsetDecode may short-circuit; decode as GBK.
	if !bytes.Contains(body, []byte{0xC7, 0xB0}) {
		t.Fatalf("DeviceName 前 not GBK: %x", body)
	}
	if bytes.Contains(body, []byte("前")) {
		t.Fatalf("DeviceName still UTF-8: %x", body)
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(body)
	if err != nil {
		t.Fatal(err)
	}
	s := string(decoded)
	if !strings.Contains(s, "<DeviceName>前</DeviceName>") || !strings.Contains(s, "<Name>前</Name>") {
		t.Fatalf("GBK round-trip: %s", s)
	}

	// Longer Chinese → invalid UTF-8 overall → UTF8Body path works.
	dev2 := config.DeviceConfig{
		DeviceID: "34020000001320000001",
		Name:     "前门枪机",
		Channels: []config.ChannelConfig{{ChannelID: "34020000001320000001"}},
	}
	body2, err := BuildDeviceInfoBody(dev2, 4)
	if err != nil {
		t.Fatal(err)
	}
	utf8Body, err := manscdp.UTF8Body(body2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(utf8Body), "<DeviceName>前门枪机</DeviceName>") {
		t.Fatalf("UTF8Body round-trip: %s", utf8Body)
	}
}

func TestEncodeMANSCDP_GB18030Fallback(t *testing.T) {
	// U+1F600 😀 is outside GBK → whole body encoded as GB18030.
	type resp struct {
		XMLName struct{} `xml:"Response"`
		Name    string   `xml:"Name"`
	}
	body, err := EncodeMANSCDP(resp{Name: "😀"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`encoding="GB2312"`)) {
		t.Fatal("missing declaration")
	}
	utf8Body, err := manscdp.UTF8Body(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(utf8Body), "😀") {
		t.Fatalf("GB18030 round-trip failed: %s / %x", utf8Body, body)
	}
}
