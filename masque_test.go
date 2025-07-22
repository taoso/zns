package zns

import (
	"net/url"
	"testing"
)

func TestParseMasqueTarget(t *testing.T) {
	for i, c := range []struct {
		url  string
		addr string
	}{
		{
			url:  "https://example.org/.well-known/masque/udp/10.0.0.1/443/",
			addr: "10.0.0.1:443",
		},
		{
			url:  "https://proxy.example.org:4443/masque?h=10.0.0.1&p=443",
			addr: "10.0.0.1:443",
		},
		{
			url:  "https://proxy.example.org:4443/masque?10.0.0.1,443",
			addr: "10.0.0.1:443",
		},
		{
			url:  "https://example.org/.well-known/masque/udp/2001%3Adb8%3A%3A1/443/",
			addr: "[2001:db8::1]:443",
		},
		{
			url:  "https://proxy.example.org:4443/masque?h=2001%3Adb8%3A%3A1&p=443",
			addr: "[2001:db8::1]:443",
		},
		{
			url:  "https://proxy.example.org:4443/masque?2001%3Adb8%3A%3A1,443",
			addr: "[2001:db8::1]:443",
		},
		{
			url:  "https://example.org/.well-known/masque/udp/example.com/443/",
			addr: "example.com:443",
		},
		{
			url:  "https://example.org/.well-known/masque/udp/10.0.0.1/",
			addr: "",
		},
		{
			url:  "https://proxy.example.org:4443/masque?h=&p=443",
			addr: "",
		},
		{
			url:  "https://proxy.example.org:4443/masque?",
			addr: "",
		},
	} {
		t.Logf("case: %d", i)
		u, err := url.Parse(c.url)
		if err != nil {
			t.Fatal("invalid url case:", err)
		}

		addr, err := parseMasqueTarget(u)
		if c.addr != addr {
			t.Fatal("invalid parsed addr, expected:", c.addr, ", got:", addr)
		}
	}
}
