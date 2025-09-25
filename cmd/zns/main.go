package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/taoso/zns"
	"golang.org/x/crypto/acme/autocert"
)

var tlsCert string
var tlsKey string
var tlsHosts string
var h12, h3, dot string
var upstream string
var dbPath string
var price int
var free bool
var root string

func listen() (lnH12, lnDot net.Listener, lnH3 net.PacketConn, err error) {
	if h12 != "" {
		lnH12, err = net.Listen("tcp", h12)
		if err != nil {
			return
		}
	}
	if dot != "" {
		lnDot, err = net.Listen("tcp", dot)
		if err != nil {
			return
		}
	}
	if h3 != "" {
		lnH3, err = net.ListenPacket("udp", h3)
	}
	return
}

type certLoader struct {
	certFile, keyFile string

	lastMod time.Time

	t *time.Ticker

	cert *tls.Certificate
}

func (l *certLoader) Load() {
	info, err := os.Stat(l.certFile)
	if err != nil {
		panic(err)
	}
	lastMod := info.ModTime()
	if !lastMod.After(l.lastMod) {
		return
	}
	cert, err := tls.LoadX509KeyPair(l.certFile, l.keyFile)
	if err != nil {
		panic(err)
	}
	l.cert = &cert
	l.lastMod = lastMod
}

func (l *certLoader) Stop() {
	l.t.Stop()
}

func (l *certLoader) Start() {
	l.Load()
	go l.Loop()
}

func (l *certLoader) Loop() {
	l.t = time.NewTicker(1 * time.Minute)
	for range l.t.C {
		l.Load()
	}
}

func (l *certLoader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return l.cert, nil
}

func main() {
	flag.StringVar(&tlsCert, "tls-cert", "", "File path of TLS certificate")
	flag.StringVar(&tlsKey, "tls-key", "", "File path of TLS key")
	flag.StringVar(&tlsHosts, "tls-hosts", "", "Host name for ACME")
	flag.StringVar(&h12, "h12", ":443", "Listen address for http1 and h2")
	flag.StringVar(&h3, "h3", ":443", "Listen address for h3")
	flag.StringVar(&dot, "dot", ":853", "Listen address for DoT")
	flag.StringVar(&upstream, "upstream", "https://doh.pub/dns-query", "DoH upstream URL")
	flag.StringVar(&dbPath, "db", "", "File path of Sqlite database")
	flag.StringVar(&root, "root", ".", "Root path of static files")
	flag.IntVar(&price, "price", 1024, "Traffic price MB/Yuan")
	flag.BoolVar(&free, "free", false, `Whether allow free access.
If not free, you should set the following environment variables:
	- ALIPAY_APP_ID
	- ALIPAY_PRIVATE_KEY
	- ALIPAY_PUBLIC_KEY
`)

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
		l := certLoader{certFile: tlsCert, keyFile: tlsKey}
		l.Start()
		defer l.Stop()
		tlsCfg.GetCertificate = l.GetCertificate
	}

	lnH12, lnDot, lnH3, err := listen()
	if err != nil {
		panic(err)
	}

	var pay zns.Pay
	var repo zns.TicketRepo
	if free {
		repo = zns.FreeTicketRepo{}
	} else {
		repo = zns.NewTicketRepo(dbPath)
		pay = zns.NewPay(
			os.Getenv("ALIPAY_APP_ID"),
			os.Getenv("ALIPAY_PRIVATE_KEY"),
			os.Getenv("ALIPAY_PUBLIC_KEY"),
		)
	}

	h := &zns.Handler{Upstream: upstream, Repo: repo, Root: http.Dir(root)}
	th := &zns.TicketHandler{MBpCNY: price, Pay: pay, Repo: repo}

	mux := http.NewServeMux()
	mux.Handle("/dns-query", h)
	mux.Handle("/dns/{token}", h)
	mux.Handle("/ticket/", th)
	mux.Handle("/ticket/{token}", th)
	mux.Handle("/", http.FileServer(h.Root))

	x := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			h.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	if lnH3 != nil {
		p := lnH3.LocalAddr().(*net.UDPAddr).Port
		h.AltSvc = fmt.Sprintf(`h3=":%d"`, p)
		th.AltSvc = h.AltSvc

		h3 := http3.Server{Handler: mux, TLSConfig: tlsCfg}
		go h3.Serve(lnH3)
	}

	if lnDot != nil {
		ln := tls.NewListener(lnDot, tlsCfg)
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					log.Println("Failed to accept dot connection", err)
					continue
				}
				go h.ServeDoT(c.(*tls.Conn))
			}
		}()
	}

	lnTLS := tls.NewListener(lnH12, tlsCfg)
	if err = http.Serve(lnTLS, x); err != nil {
		log.Fatal(err)
	}
}
