package zns

import (
	"bytes"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var dohUpstreamRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "doh_upstream_request_duration_seconds",
		Help:    "Duration of DoH upstream requests in seconds",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"proto"},
)

func init() {
	prometheus.MustRegister(dohUpstreamRequestDuration)
}

type Handler struct {
	Upstream string
	Auther   Author
	Root     http.Dir
	Blocker  *BlockList
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI == "/metrics" {
		h.ServeMetrics(w, r)
		return
	}

	var token string
	if r.URL.Path == "/dns-query" {
		// https://${token}.zns.lehu.in/dns-query
		x := strings.SplitN(r.Host, ".", 2)
		token = x[0]
	} else {
		token = r.PathValue("token")
	}

	if token == "" {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	var err error
	var question []byte
	if r.Method == http.MethodGet {
		q := r.URL.Query().Get("dns")
		if q == "" {
			if !strings.HasPrefix(r.Host, "zns.") {
				_, host, _ := strings.Cut(r.Host, ".")
				http.Redirect(w, r, "https://"+host+r.RequestURI, http.StatusFound)
				return
			}
			f, err := h.Root.Open("/index.html")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			io.Copy(w, f)
			return
		}
		question, err = base64.RawURLEncoding.DecodeString(q)
	} else {
		question, err = io.ReadAll(r.Body)
		r.Body.Close()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ok, err := h.Auther.Auth(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if !ok {
		http.Error(w, "bye", http.StatusBadRequest)
		return
	}

	var m dns.Msg
	if err := m.Unpack(question); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(m.Question) == 0 {
		http.Error(w, "question is empty", http.StatusBadRequest)
		return
	}

	q := m.Question[0]

	if h.Blocker.Exists(strings.TrimSuffix(q.Name, ".")) {
		a := &dns.Msg{}
		a.SetReply(&m)
		a.Authoritative = true
		a.Rcode = dns.RcodeNameError

		buf, err := a.Pack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Add("content-type", "application/dns-message")
		w.Write(buf)
		return
	}

	if q.Qtype == dns.TypeSVCB && strings.HasPrefix(q.Name, "_dns.") {
		server := strings.TrimPrefix(q.Name, "_dns.")
		var dohServer, dotServer string
		if strings.HasPrefix(r.URL.Path, "/dns/") {
			dohServer = server
			dotServer = token + "." + server
		} else {
			dohServer = strings.TrimPrefix(server, token+".")
			dotServer = server
		}

		doh := &dns.SVCB{
			Hdr: dns.RR_Header{
				Name:   q.Name,
				Rrtype: dns.TypeSVCB,
				Class:  dns.ClassINET,
				Ttl:    1,
			},
			Priority: 1,
			Target:   dohServer,
			Value: []dns.SVCBKeyValue{
				&dns.SVCBAlpn{Alpn: []string{"h3", "h2"}},
				&dns.SVCBDoHPath{Template: "/dns/" + token + "/{?dns}"},
			},
		}

		dot := &dns.SVCB{
			Hdr: dns.RR_Header{
				Name:   q.Name,
				Rrtype: dns.TypeSVCB,
				Class:  dns.ClassINET,
				Ttl:    1,
			},
			Priority: 2,
			Target:   dotServer,
			Value: []dns.SVCBKeyValue{
				&dns.SVCBAlpn{Alpn: []string{"dot"}},
			},
		}

		a := &dns.Msg{}
		a.SetReply(&m)
		a.Authoritative = true
		a.Answer = []dns.RR{doh, dot}

		buf, err := a.Pack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Add("content-type", "application/dns-message")
		w.Write(buf)
		return
	}

	var hasSubnet bool
	if e := m.IsEdns0(); e != nil {
		var opts []dns.EDNS0
		for _, o := range e.Option {
			if o.Option() == dns.EDNS0SUBNET {
				a := o.(*dns.EDNS0_SUBNET).Address[:2]
				// skip empty subnet like 0.0.0.0/0
				if !bytes.HasPrefix(a, []byte{0, 0}) {
					hasSubnet = true
				}
			} else if o.Option() != dns.EDNS0PADDING {
				opts = append(opts, o)
			}
		}
		e.Option = opts
	} else {
		m.SetEdns0(dns.DefaultMsgSize, false) // TODO(tao) DNSSEC
	}

	if !hasSubnet {
		ip, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		addr := ip.Addr()
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
		opt := m.IsEdns0()
		opt.Option = append(opt.Option, ecs)
	}

	if question, err = m.Pack(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	proto := "h" + strconv.Itoa(r.ProtoMajor)
	timer := prometheus.NewTimer(dohUpstreamRequestDuration.WithLabelValues(proto))

	resp, err := http.Post(h.Upstream, "application/dns-message", bytes.NewReader(question))

	timer.ObserveDuration()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer resp.Body.Close()

	w.Header().Add("content-type", "application/dns-message")
	io.Copy(w, resp.Body)
}

func (h *Handler) ServeMetrics(w http.ResponseWriter, r *http.Request) {
	token, _, _ := r.BasicAuth()
	ok, err := h.Auther.Auth(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	promhttp.Handler().ServeHTTP(w, r)
}
