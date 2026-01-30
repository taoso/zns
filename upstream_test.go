package zns

import (
	"fmt"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestUpstreamPlain(t *testing.T) {
	up := UpstreamPlain{"dns.alidns.com:53", 1 * time.Second}

	q := dns.Msg{}
	q.SetQuestion("com.", dns.TypeNS)

	r, _ := up.Query(&q)

	a := dns.Msg{}
	a.Unpack(r)

	for _, r := range a.Answer {
		fmt.Printf("%+v\n", r)
	}
}

func TestUpstreamHTTP3(t *testing.T) {
	up := UpstreamHTTP{
		Server:  "https://dns.alidns.com/dns-query",
		Timeout: 1 * time.Second,
		HTTP3:   true,
	}

	q := dns.Msg{}
	q.SetQuestion("com.", dns.TypeNS)

	r, err := up.Query(&q)
	fmt.Println(err)

	a := dns.Msg{}
	a.Unpack(r)

	for _, r := range a.Answer {
		fmt.Printf("%+v\n", r)
	}
}

func TestUpstreamHTTP2(t *testing.T) {
	up := UpstreamHTTP{
		Server:  "https://dns.alidns.com/dns-query",
		Timeout: 1 * time.Second,
	}

	q := dns.Msg{}
	q.SetQuestion("com.", dns.TypeNS)

	r, err := up.Query(&q)
	fmt.Println(err)

	a := dns.Msg{}
	a.Unpack(r)

	for _, r := range a.Answer {
		fmt.Printf("%+v\n", r)
	}
}
