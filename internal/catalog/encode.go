package catalog

import (
	"encoding/xml"
	"fmt"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// manscdpXMLHeader is the GB/T 28181 §9.4.1 declaration. The body that follows
// must be GBK/GB18030 bytes (not UTF-8) so platforms that honor the charset
// (e.g. LiveGBS) decode Chinese Name fields correctly.
var manscdpXMLHeader = []byte(`<?xml version="1.0" encoding="GB2312"?>` + "\n")

// EncodeMANSCDP marshals v as MANSCDP XML: declaration encoding="GB2312" and
// a GBK body (GB18030 if any rune is outside GBK). ASCII tags are unchanged.
func EncodeMANSCDP(v any) ([]byte, error) {
	body, err := xml.Marshal(v)
	if err != nil {
		return nil, err
	}
	encoded, err := toGB2312Family(body)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(manscdpXMLHeader)+len(encoded))
	out = append(out, manscdpXMLHeader...)
	out = append(out, encoded...)
	return out, nil
}

// toGB2312Family converts UTF-8 XML body bytes to GBK, falling back to
// GB18030 when a rune cannot be represented in GBK.
func toGB2312Family(utf8Body []byte) ([]byte, error) {
	out, err := simplifiedchinese.GBK.NewEncoder().Bytes(utf8Body)
	if err == nil {
		return out, nil
	}
	out, err2 := simplifiedchinese.GB18030.NewEncoder().Bytes(utf8Body)
	if err2 != nil {
		return nil, fmt.Errorf("catalog: encode as GBK/GB18030: %w", err)
	}
	return out, nil
}
