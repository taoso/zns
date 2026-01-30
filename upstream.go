package zns

import (
	"github.com/miekg/dns"
)

type Upstream interface {
	Query(*dns.Msg) ([]byte, error)
}

// Interface guards
var (
	_ Upstream = (*UpstreamPlain)(nil)
	_ Upstream = (*UpstreamHTTP)(nil)
)
