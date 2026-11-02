package zlm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to ZLMediaKit HTTP API.
type Client struct {
	base   string
	secret string
	vhost  string
	app    string
	http   *http.Client
}

// New creates a ZLM API client.
func New(api, secret, vhost, app string) *Client {
	base := strings.TrimRight(api, "/")
	return &Client{
		base:   base,
		secret: secret,
		vhost:  vhost,
		app:    app,
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) App() string   { return c.app }
func (c *Client) VHost() string { return c.vhost }

type apiResult struct {
	Code      int             `json:"code"`
	Msg       string          `json:"msg"`
	LocalPort int             `json:"local_port"`
	Data      json.RawMessage `json:"data"`
}

// AddStreamProxy pulls an HTTP-FLV/RTSP/... source into ZLM.
func (c *Client) AddStreamProxy(ctx context.Context, stream, srcURL string) error {
	q := c.baseQuery()
	q.Set("app", c.app)
	q.Set("stream", stream)
	q.Set("url", srcURL)
	q.Set("enable_rtsp", "0")
	q.Set("enable_rtmp", "0")
	q.Set("enable_hls", "0")
	q.Set("enable_mp4", "0")
	q.Set("auto_close", "0")
	q.Set("retry_count", "-1")
	_, err := c.get(ctx, "/index/api/addStreamProxy", q)
	return err
}

// DelStreamProxy removes a previously added proxy.
func (c *Client) DelStreamProxy(ctx context.Context, stream string) error {
	key := fmt.Sprintf("%s/%s/%s", c.vhost, c.app, stream)
	q := url.Values{}
	q.Set("secret", c.secret)
	q.Set("key", key)
	_, err := c.get(ctx, "/index/api/delStreamProxy", q)
	return err
}

// StartSendRtp actively pushes PS-RTP to dst (UDP or TCP-active).
func (c *Client) StartSendRtp(ctx context.Context, stream, ssrc, dstURL string, dstPort int, isUDP bool, srcPort int) (localPort int, err error) {
	q := c.baseQuery()
	q.Set("app", c.app)
	q.Set("stream", stream)
	q.Set("ssrc", ssrc)
	q.Set("dst_url", dstURL)
	q.Set("dst_port", strconv.Itoa(dstPort))
	if isUDP {
		q.Set("is_udp", "1")
	} else {
		q.Set("is_udp", "0")
	}
	if srcPort > 0 {
		q.Set("src_port", strconv.Itoa(srcPort))
	}
	q.Set("pt", "96")
	q.Set("use_ps", "1")
	res, err := c.get(ctx, "/index/api/startSendRtp", q)
	if err != nil {
		return 0, err
	}
	return res.LocalPort, nil
}

// StartSendRtpPassive starts a TCP passive sender; returns local listen port.
func (c *Client) StartSendRtpPassive(ctx context.Context, stream, ssrc string, srcPort int) (localPort int, err error) {
	q := c.baseQuery()
	q.Set("app", c.app)
	q.Set("stream", stream)
	q.Set("ssrc", ssrc)
	if srcPort > 0 {
		q.Set("src_port", strconv.Itoa(srcPort))
	}
	q.Set("pt", "96")
	q.Set("use_ps", "1")
	res, err := c.get(ctx, "/index/api/startSendRtpPassive", q)
	if err != nil {
		return 0, err
	}
	return res.LocalPort, nil
}

// StopSendRtp stops one (or all if ssrc empty) PS-RTP senders for a stream.
func (c *Client) StopSendRtp(ctx context.Context, stream, ssrc string) error {
	q := c.baseQuery()
	q.Set("app", c.app)
	q.Set("stream", stream)
	if ssrc != "" {
		q.Set("ssrc", ssrc)
	}
	_, err := c.get(ctx, "/index/api/stopSendRtp", q)
	return err
}

func (c *Client) baseQuery() url.Values {
	q := url.Values{}
	q.Set("secret", c.secret)
	q.Set("vhost", c.vhost)
	return q
}

func (c *Client) get(ctx context.Context, path string, q url.Values) (*apiResult, error) {
	u := c.base + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var res apiResult
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("zlm decode %s: %w; body=%s", path, err, truncate(body, 256))
	}
	if res.Code != 0 {
		msg := res.Msg
		if msg == "" {
			msg = string(body)
		}
		return &res, fmt.Errorf("zlm %s code=%d msg=%s", path, res.Code, msg)
	}
	return &res, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
