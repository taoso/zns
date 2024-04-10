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
	Repo     TicketRepo
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	ts, err := h.Repo.List(token, 1)
	if err != nil {
		http.Error(w, "invalid token", http.StatusInternalServerError)
		return
	}
	if len(ts) == 0 || ts[0].Bytes <= 0 {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	var question []byte
	if r.Method == http.MethodGet {
		q := r.URL.Query().Get("dns")
		question, err = base64.RawURLEncoding.DecodeString(q)
	} else {
		question, err = io.ReadAll(r.Body)
		r.Body.Close()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var m dns.Msg
	if err := m.Unpack(question); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var hasSubnet bool
	if e := m.IsEdns0(); e != nil {
		for _, o := range e.Option {
			if o.Option() == dns.EDNS0SUBNET {
				hasSubnet = true
				break
			}
		}
	}

	if !hasSubnet {
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

	if question, err = m.Pack(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := http.Post(h.Upstream, "application/dns-message", bytes.NewReader(question))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err = h.Repo.Cost(token, len(question)+len(answer)); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	w.Write(answer)
}
