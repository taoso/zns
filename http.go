package zns

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/miekg/dns"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/quicvarint"
)

type Handler struct {
	Upstream string
	Repo     TicketRepo
	AltSvc   string
	Root     http.Dir
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.AltSvc != "" {
		w.Header().Set("Alt-Svc", h.AltSvc)
	}

	if r.Method == http.MethodConnect {
		auth := r.Header.Get("Proxy-Authorization")
		if auth == "" {
			w.Header().Set("Proxy-Authenticate", `Basic realm="Word Wide Web"`)
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		username, _, _ := parseBasicAuth(auth)
		ts, err := h.Repo.List(username, 1)
		if err != nil {
			http.Error(w, "invalid token", http.StatusInternalServerError)
			return
		}
		if len(ts) == 0 || ts[0].Bytes <= 0 {
			w.Header().Set("Proxy-Authenticate", `Basic realm="Word Wide Web"`)
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		r.URL.User = url.User(username)
		if r.Proto == "connect-udp" {
			h.proxyUDP(w, r)
		} else {
			h.proxyHTTPS(w, r)
		}
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

	ts, err := h.Repo.List(token, 1)
	if err != nil {
		http.Error(w, "invalid token", http.StatusInternalServerError)
		return
	}
	if len(ts) == 0 || ts[0].Bytes <= 0 {
		http.Error(w, "invalid token", http.StatusUnauthorized)
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

	if q := m.Question[0]; q.Qtype == dns.TypeSVCB && strings.HasPrefix(q.Name, "_dns.") {
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

	if r.URL.Query().Get("noad") != "" {
		n := m.Question[0].Name
		n = strings.TrimRight(n, ".")
		if isBlackDomain(n) {
			non := new(dns.Msg)
			non.SetReply(&m)
			non.Rcode = dns.RcodeNameError
			answer, err := non.Pack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Add("content-type", "application/dns-message")
			w.Write(answer)
		}
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

	costFold := 100 // 主服务正常时备用线路消耗百倍流量，只在必要时使用
	remoteAddr := r.Header.Get("zns-real-addr")
	if remoteAddr == "" {
		remoteAddr = r.RemoteAddr
		costFold = 1
	}

	if !hasSubnet {
		ip, err := netip.ParseAddrPort(remoteAddr)
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

	if err = h.Repo.Cost(token, (len(question)+len(answer))*costFold); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	w.Header().Add("content-type", "application/dns-message")
	w.Write(answer)
}

func (p *Handler) proxyUDP(w http.ResponseWriter, req *http.Request) {
	addr, err := parseMasqueTarget(req.URL)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid target"))
		log.Println("invalid target", req.URL)
		return
	}

	log.Println("target:", req.URL)

	up, err := net.Dial("udp", addr)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("dial udp err: " + err.Error()))
		log.Println("dial udp err", err)
		return
	}
	defer up.Close()

	w.Header().Add("capsule-protocol", "?1")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()

	w = w.(httpsnoop.Unwrapper).Unwrap()
	str := w.(http3.HTTPStreamer).HTTPStream()
	defer str.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	user := req.URL.User.Username()

	cost := func(n int) {
		n *= 2
		err := p.Repo.Cost(user, n)
		if err != nil {
			log.Println("ticket cost error: ", user, n, err)
			str.Close()
			up.Close()
		}
	}
	u := &bytesCounter{w: up, d: 1 * time.Second, f: cost}

	go u.Start()
	defer u.Done()

	go func() {
		defer wg.Done()
		b := make([]byte, 1500)
		for {
			n, err := u.Read(b[1:])
			if err != nil {
				log.Println("up.Read err:", err)
				return
			}
			err = str.SendDatagram(b[:n+1])
			if err != nil {
				log.Println("SendDatagram err:", err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		ctx := context.Background()
		for {
			b, err := str.ReceiveDatagram(ctx)
			if err != nil {
				log.Println("ReceiveDatagram err:", err)
				return
			}
			_, n, err := quicvarint.Parse(b)
			if err != nil {
				log.Println("parse cid err:", err)
				return
			}
			_, err = u.Write(b[n:])
			if err != nil {
				log.Println("up.Write err:", err)
				return
			}
		}
	}()

	wg.Wait()
}

func (p *Handler) proxyHTTPS(w http.ResponseWriter, req *http.Request) {
	address := req.RequestURI
	upConn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer upConn.Close()

	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()

	var downConn io.ReadWriteCloser
	if req.ProtoMajor >= 2 {
		downConn = flushWriter{w: w, r: req.Body}
	} else {
		downConn, _, err = w.(http.Hijacker).Hijack()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("hijack err: " + err.Error()))
			return
		}
		defer downConn.Close()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	user := req.URL.User.Username()

	cost := func(n int) {
		n *= 2
		err := p.Repo.Cost(user, n)
		if err != nil {
			log.Println("ticket cost error: ", user, n, err)
			downConn.Close()
			upConn.Close()
		}
	}

	u := &bytesCounter{w: upConn, d: 1 * time.Second, f: cost}

	go u.Start()
	defer u.Done()

	go func() {
		defer wg.Done()
		io.Copy(u, downConn)
	}()
	go func() {
		defer wg.Done()
		io.Copy(downConn, u)
	}()

	wg.Wait()
}
