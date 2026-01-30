package zns

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/miekg/dns"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// UpstreamHTTP makes a DNS query over UpstreamHTTP(s)
type UpstreamHTTP struct {
	Server  string
	HTTP3   bool
	Timeout time.Duration

	conn *http.Client
}

func (h *UpstreamHTTP) Query(m *dns.Msg) ([]byte, error) {
	if h.conn == nil {
		h.conn = &http.Client{Timeout: h.Timeout}

		if h.HTTP3 {
			h.conn.Transport = &http3.Transport{
				QUICConfig: &quic.Config{
					Allow0RTT: true,
				},
			}
		}
	}

	buf, err := m.Pack()
	if err != nil {
		return nil, fmt.Errorf("packing message: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, h.Server, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("creating http request to %s: %w", h.Server, err)
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := h.conn.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", h.Server, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", h.Server, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status code %d from %s", resp.StatusCode, h.Server)
	}

	return body, nil
}
