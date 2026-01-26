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
	"go.yaml.in/yaml/v4"
	"golang.org/x/crypto/acme/autocert"
)

var tlsCert string
var tlsKey string
var tlsHosts string
var h12, h3, dot string
var upstream string
var dbPath string
var tokenPath string
var root string
var nodes string

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

func main() {
	flag.StringVar(&tlsCert, "tls-cert", "", "File path of TLS certificate")
	flag.StringVar(&tlsKey, "tls-key", "", "File path of TLS key")
	flag.StringVar(&tlsHosts, "tls-hosts", "", "Host name for ACME")
	flag.StringVar(&h12, "h12", ":443", "Listen address for http1 and h2")
	flag.StringVar(&h3, "h3", ":443", "Listen address for h3")
	flag.StringVar(&dot, "dot", ":853", "Listen address for DoT")
	flag.StringVar(&upstream, "upstream", "https://doh.pub/dns-query", "DoH upstream URL")
	flag.StringVar(&dbPath, "db", "", "File path of Sqlite database")
	flag.StringVar(&tokenPath, "token", "", "File path of auth tokens")
	flag.StringVar(&root, "root", ".", "Root path of static files")
	flag.StringVar(&nodes, "nodes", "", "Server nodes info file path")

	flag.Parse()

	l := zns.FileLoader{Interval: 5 * time.Minute}

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

		var cert *tls.Certificate
		l.Watch(tlsCert, func(file string) {
			c, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
			if err != nil {
				log.Println("Failed to load TLS certificate: ", err)
				return
			} else {
				log.Println("Loaded TLS certificate from ", file)
			}
			cert = &c
		})

		tlsCfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return cert, nil
		}
	}

	l.Start()
	defer l.Stop()

	tlsCfg.NextProtos = append(tlsCfg.NextProtos, "h2", "http/1.1")

	lnH12, lnDot, lnH3, err := listen()
	if err != nil {
		panic(err)
	}

	var pay zns.Pay
	var repo zns.TicketRepo
	if dbPath != "" {
		repo = zns.NewTicketRepo(dbPath)
		pay = zns.NewZzPay(
			os.Getenv("APP_PRVKEY_zns"),
			os.Getenv("APP_PUBKEY_zz"),
		)
	}

	h := &zns.Handler{
		Upstream: upstream,
		Root:     http.Dir(root),
		Auther:   zns.DBAuthor{Repo: repo},
	}

	if tokenPath != "" {
		fa := &zns.FileAuther{Path: tokenPath}
		if err = fa.Load(); err != nil {
			log.Fatal("Failed to load tokens: ", err)
		}

		l.Watch(tokenPath, func(file string) {
			if err = fa.Load(); err != nil {
				log.Println("Failed to reload tokens: ", err)
			} else {
				log.Println("Reloaded tokens from ", file)
			}
		})

		h.Auther = fa
	}

	th := &zns.TicketHandler{Pay: pay, Repo: repo}

	if nodes != "" {
		b, err := os.ReadFile(nodes)
		if err != nil {
			log.Fatal("Failed to load nodes file: ", err)
		}

		if err := yaml.Unmarshal(b, &th.Nodes); err != nil {
			log.Fatal("Failed to parse nodes file: ", err)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/dns-query", h)
	mux.Handle("/dns/{token}", h)

	if dbPath != "" {
		mux.Handle("/ticket/", th)
		mux.Handle("/ticket/{token}", th)
		mux.Handle("/my-nodes/{token}", th)
		mux.Handle("/", http.FileServer(h.Root))
	}

	altSvc := ""
	x := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			h.ServeHTTP(w, r)
			return
		}

		if r.ProtoMajor < 3 && altSvc != "" {
			w.Header().Set("Alt-Svc", altSvc)
		}

		mux.ServeHTTP(w, r)
	})

	if lnH3 != nil {
		p := lnH3.LocalAddr().(*net.UDPAddr).Port
		altSvc = fmt.Sprintf(`h3=":%d"`, p)

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
