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

func main() {
	flag.StringVar(&tlsCert, "tls-cert", "", "tls cert file path")
	flag.StringVar(&tlsKey, "tls-key", "", "tls key file path")
	flag.StringVar(&tlsHosts, "tls-hosts", "", "tls host name")
	flag.StringVar(&listen, "listen", ":443", "listen addr")
	flag.StringVar(&upstream, "upstream", "https://doh.pub/dns-query", "DoH upstream URL")
	flag.StringVar(&dbPath, "db", "", "sqlite database file path")
	flag.IntVar(&price, "price", 1024, "Traffic MB/Yuan")

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

	repo := zns.NewTicketRepo(dbPath)
	repo.New("foo", 2048, "pay-1")

	var pay zns.Pay
	if id := os.Getenv("ALIPAY_APP_ID"); id != "" {
		pay = zns.NewPay(
			id,
			os.Getenv("ALIPAY_PRIVATE_KEY"),
			os.Getenv("ALIPAY_PUBLIC_KEY"),
		)
	}

	h := zns.Handler{Upstream: upstream, Repo: repo}
	th := zns.TicketHandler{MBpCNY: price, Pay: pay, Repo: repo}

	mux := http.NewServeMux()
	mux.Handle("/dns/{token}", h)
	mux.Handle("/ticket/{token}", th)

	if err = http.Serve(lnTLS, mux); err != nil {
		log.Fatal(err)
	}
}
