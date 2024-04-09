package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"

	"github.com/miekg/dns"
	"golang.org/x/crypto/acme/autocert"
)

var tlsCert string
var tlsKey string
var tlsHosts string
var listen string

func main() {
	flag.StringVar(&tlsCert, "tls-cert", "", "tls cert file path")
	flag.StringVar(&tlsKey, "tls-key", "", "tls key file path")
	flag.StringVar(&tlsHosts, "tls-hosts", "", "tls host name")
	flag.StringVar(&listen, "listen", ":443", "listen addr")

	flag.Parse()

	var tlsCfg *tls.Config
	if tlsHosts != "" {
		acm := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(os.Getenv("HOME") + "/.autocert"),
			HostPolicy: autocert.HostWhitelist(strings.Split(tlsHosts, ",")...),
		}

		tlsCfg = acm.TLSConfig()
	} else {
		tlsCfg = &tls.Config{}
		certs, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
		if err != nil {
			panic(err)
		}
		tlsCfg.Certificates = []tls.Certificate{certs}
	}

	lnTLS, err := tls.Listen("tcp", listen, tlsCfg)
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/dns/{name}", func(w http.ResponseWriter, r *http.Request) {
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

		resp, err := http.Post("https://doh.pub/dns-query", "application/dns-message", bytes.NewReader(body))
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
	})

	if err = http.Serve(lnTLS, mux); err != nil {
		log.Fatal(err)
	}
}
