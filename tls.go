package zns

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"io"
	"log"
	"net/http"
)

type tlsWriter struct {
	code int
	body []byte
	conn *tls.Conn
}

func (w *tlsWriter) Header() http.Header        { return http.Header{} }
func (w *tlsWriter) WriteHeader(statusCode int) { w.code = statusCode }
func (w *tlsWriter) Write(b []byte) (n int, err error) {
	w.body = b
	return len(b), nil
}

func (p *Handler) ServeDoT(conn *tls.Conn) {
	defer conn.Close()

	conn.Handshake()

	lenBuf := make([]byte, 2)
	w := &tlsWriter{conn: conn}

	conn.Handshake()

	domain := conn.ConnectionState().ServerName

	for {
		_, err := io.ReadFull(conn, lenBuf)
		if err != nil {
			if err != io.EOF {
				log.Println("reading query length error", err)
			}
			return
		}
		queryLen := binary.BigEndian.Uint16(lenBuf)

		queryBuf := make([]byte, queryLen)
		_, err = io.ReadFull(conn, queryBuf)
		if err != nil {
			log.Printf("reading query body error", err)
			return
		}

		url := "https://" + domain + ":853/dns-query"
		req, err := http.NewRequest("POST", url, io.NopCloser(bytes.NewReader(queryBuf)))
		if err != nil {
			return
		}

		req.RemoteAddr = conn.RemoteAddr().String()

		p.ServeHTTP(w, req)

		if w.code != http.StatusOK && w.code != 0 {
			log.Println("dot query error", string(w.body))
			return
		}

		respBytes := w.body
		buf := make([]byte, 2+len(respBytes))
		binary.BigEndian.PutUint16(buf, uint16(len(respBytes)))

		copy(buf[2:], respBytes)

		if _, err := conn.Write(buf); err != nil {
			log.Println("dot writing response body error", err)
			return
		}
	}
}
