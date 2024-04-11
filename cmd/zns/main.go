package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/taoso/zns"
	"golang.org/x/crypto/acme/autocert"
)

var tlsCert string
var tlsKey string
var tlsHosts string
var listen string
var upstream string
var dbPath string
var price int
var free bool
var root string

func main() {
	flag.StringVar(&tlsCert, "tls-cert", "", "File path of TLS certificate")
	flag.StringVar(&tlsKey, "tls-key", "", "File path of TLS key")
	flag.StringVar(&tlsHosts, "tls-hosts", "", "Host name for ACME")
	flag.StringVar(&listen, "listen", ":443", "Listen address")
	flag.StringVar(&upstream, "upstream", "https://doh.pub/dns-query", "DoH upstream URL")
	flag.StringVar(&dbPath, "db", "", "File path of Sqlite database")
	flag.StringVar(&root, "root", ".", "Root path of static files")
	flag.IntVar(&price, "price", 1024, "Traffic price MB/Yuan")
	flag.BoolVar(&free, "free", true, `Whether allow free access.
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

	h := zns.Handler{Upstream: upstream, Repo: repo}
	th := zns.TicketHandler{MBpCNY: price, Pay: pay, Repo: repo}

	mux := http.NewServeMux()
	mux.Handle("/dns/{token}", h)
	mux.Handle("/ticket/{token}", th)
	mux.Handle("/", http.FileServer(http.Dir(root)))

	if err = http.Serve(lnTLS, mux); err != nil {
		log.Fatal(err)
	}
}
