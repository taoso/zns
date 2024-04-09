package zns

import (
	"bytes"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/netip"

	"github.com/miekg/dns"
)

type Handler struct {
	Upstream string
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	var err error
	if r.Method == http.MethodGet {
		q := r.URL.Query().Get("dns")
		body, err = base64.RawURLEncoding.DecodeString(q)
	} else {
		body, err = io.ReadAll(r.Body)
		r.Body.Close()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var m dns.Msg
	if err := m.Unpack(body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if m.IsEdns0() == nil {
		ip, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		addr := ip.Addr()
		opt := &dns.OPT{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeOPT}}
		ecs := &dns.EDNS0_SUBNET{Code: dns.EDNS0SUBNET}
		var bits int
		if addr.Is4() {
			bits = 24
			ecs.Family = 1
		} else {
			bits = 48
			ecs.Family = 2
		}
		ecs.SourceNetmask = uint8(bits)
		p := netip.PrefixFrom(addr, bits)
		ecs.Address = net.IP(p.Masked().Addr().AsSlice())
		opt.Option = append(opt.Option, ecs)
		m.Extra = append(m.Extra, opt)
	}

	if body, err = m.Pack(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := http.Post(h.Upstream, "application/dns-message", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(body)
}
