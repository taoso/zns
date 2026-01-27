package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/quic-go/quic-go/http3"
	"github.com/taoso/zns"
	"golang.org/x/crypto/acme/autocert"
)

var configPath string

type Config struct {
	TLSCert   string     `toml:"tls_cert"`
	TLSKey    string     `toml:"tls_key"`
	TLSHosts  []string   `toml:"tls_hosts"`
	DoHAddr   string     `toml:"doh_addr"`
	DoH3Addr  string     `toml:"doh3_addr"`
	DoTAddr   string     `toml:"dot_addr"`
	Upstream  string     `toml:"upstream"`
	DBPath    string     `toml:"db_path"`
	TokenPath string     `toml:"token_path"`
	Root      string     `toml:"root_path"`
	Blocklist string     `toml:"blocklist_path"`
	Nodes     []zns.Node `toml:"nodes"`
}

func listen(c Config) (lnH12, lnDot net.Listener, lnH3 net.PacketConn, err error) {
	if c.DoHAddr != "" {
		lnH12, err = net.Listen("tcp", c.DoHAddr)
		if err != nil {
			return
		}
	}
	if c.DoTAddr != "" {
		lnDot, err = net.Listen("tcp", c.DoTAddr)
		if err != nil {
			return
		}
	}
	if c.DoH3Addr != "" {
		lnH3, err = net.ListenPacket("udp", c.DoH3Addr)
	}
	return
}

func main() {
	flag.StringVar(&configPath, "c", "", "Config file path (TOML format)")
	flag.Parse()

	config := Config{}

	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		log.Fatalln("Failed to load config file:", err)
	}

	l := zns.NewFileLoader()

	var tlsCfg *tls.Config
	if len(config.TLSHosts) > 0 {
		acm := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(os.Getenv("HOME") + "/.autocert"),
			HostPolicy: autocert.HostWhitelist(config.TLSHosts...),
		}

		tlsCfg = acm.TLSConfig()
	} else {
		tlsCfg = &tls.Config{}

		var cert *tls.Certificate
		l.Watch(config.TLSCert, func(file string) {
			c, err := tls.LoadX509KeyPair(config.TLSCert, config.TLSKey)
			if err != nil {
				log.Println("Failed to load TLS certificate: ", err)
				return
			} else {
				log.Println("Loaded TLS certificate from", file)
			}
			cert = &c
		})

		tlsCfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return cert, nil
		}
	}

	tlsCfg.NextProtos = append(tlsCfg.NextProtos, "h2", "http/1.1")

	var pay zns.Pay
	var repo zns.TicketRepo
	if config.DBPath != "" {
		repo = zns.NewTicketRepo(config.DBPath)
		pay = zns.NewZzPay(
			os.Getenv("APP_PRVKEY_zns"),
			os.Getenv("APP_PUBKEY_zz"),
		)
	}

	h := &zns.Handler{
		Upstream: config.Upstream,
		Root:     http.Dir(config.Root),
		Auther:   zns.DBAuthor{Repo: repo},
	}

	if config.TokenPath != "" {
		fa := &zns.FileAuther{Path: config.TokenPath}
		if err := fa.Load(); err != nil {
			log.Fatal("Failed to load tokens: ", err)
		}

		l.Watch(config.TokenPath, func(file string) {
			if err := fa.Load(); err != nil {
				log.Println("Failed to reload tokens: ", err)
			} else {
				log.Println("Reloaded tokens from ", file)
			}
		})

		h.Auther = fa
	}

	if config.Blocklist != "" {
		b := zns.NewBlockList()
		if err := b.Load(config.Blocklist); err != nil {
			log.Fatal("Failed to load blocklist: ", err)
		}

		h.Blocker = b
		log.Println("Loaded blocklist from ", config.Blocklist)

		l.Watch(config.Blocklist, func(file string) {
			b := zns.NewBlockList()
			if err := b.Load(file); err != nil {
				log.Fatal("Failed to load blocklist: ", err)
			} else {
				h.Blocker = b
				log.Println("Reloaded blocklist from ", file)
			}
		})
	}

	l.Start()
	defer l.Stop()

	th := &zns.TicketHandler{Pay: pay, Repo: repo}

	th.Nodes = config.Nodes

	mux := http.NewServeMux()
	mux.Handle("/metrics", h)
	mux.Handle("/dns-query", h)
	mux.Handle("/dns/{token}", h)

	if config.DBPath != "" {
		mux.Handle("/ticket/", th)
		mux.Handle("/ticket/{token}", th)
		mux.Handle("/my-nodes/{token}", th)
		mux.Handle("/", http.FileServer(h.Root))
	}

	altSvc := ""
	x := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor < 3 && altSvc != "" {
			w.Header().Set("Alt-Svc", altSvc)
		}

		mux.ServeHTTP(w, r)
	})

	lnH12, lnDot, lnH3, err := listen(config)
	if err != nil {
		panic(err)
	}

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
	if err := http.Serve(lnTLS, x); err != nil {
		log.Fatal(err)
	}
}
