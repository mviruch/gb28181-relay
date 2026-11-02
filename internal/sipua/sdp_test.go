package sipua

import (
	"strings"
	"testing"

	"github.com/mviruch/gb28181-relay/internal/session"
)

func TestParseOfferUDP(t *testing.T) {
	body := []byte("v=0\r\no=- 0 0 IN IP4 1.2.3.4\r\ns=Play\r\nc=IN IP4 1.2.3.4\r\nt=0 0\r\nm=video 30000 RTP/AVP 96\r\na=recvonly\r\ny=0100000001\r\n")
	o, err := ParseOffer(body)
	if err != nil {
		t.Fatal(err)
	}
	if o.MediaIP != "1.2.3.4" || o.MediaPort != 30000 || o.SSRC != "0100000001" {
		t.Fatalf("unexpected offer: %+v", o)
	}
	if ModeFromOffer(o) != session.ModeUDP {
		t.Fatalf("want UDP mode")
	}
}

func TestParseOfferTCPActive(t *testing.T) {
	body := []byte("v=0\r\nc=IN IP4 10.0.0.1\r\nm=video 0 TCP/RTP/AVP 96\r\na=setup:active\r\ny=1234567890\r\n")
	o, err := ParseOffer(body)
	if err != nil {
		t.Fatal(err)
	}
	if ModeFromOffer(o) != session.ModeTCPPassive {
		t.Fatalf("platform setup:active => we should be passive, got %v", ModeFromOffer(o))
	}
	ans := BuildAnswer("192.168.1.2", 15060, o.SSRC, session.ModeTCPPassive)
	if !strings.Contains(ans, "a=setup:passive") || !strings.Contains(ans, "a=sendonly") {
		t.Fatalf("bad answer:\n%s", ans)
	}
}
