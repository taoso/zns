package zns

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type bytesCounter struct {
	w io.ReadWriter
	f func(n int)
	d time.Duration

	c atomic.Int64
	t *time.Ticker
	s chan int
}

func (bc *bytesCounter) Done() {
	bc.t.Stop()
	close(bc.s)
}

func (bc *bytesCounter) Start() {
	bc.s = make(chan int, 1)
	bc.t = time.NewTicker(bc.d)

	for {
		select {
		case <-bc.t.C:
			if n := bc.c.Swap(0); n > 0 {
				bc.f(int(n))
			}
		case <-bc.s:
			if n := bc.c.Swap(0); n > 0 {
				bc.f(int(n))
			}
			return
		}
	}
}

func (bc *bytesCounter) Write(p []byte) (n int, err error) {
	n, err = bc.w.Write(p)
	bc.c.Add(int64(n))
	return
}

func (bc *bytesCounter) Read(p []byte) (n int, err error) {
	n, err = bc.w.Read(p)
	bc.c.Add(int64(n))
	return
}

type flushWriter struct {
	w io.Writer
	r io.Reader
}

func (fw flushWriter) Write(p []byte) (n int, err error) {
	n, err = fw.w.Write(p)
	fw.w.(http.Flusher).Flush()
	return
}

func (fw flushWriter) Read(p []byte) (n int, err error) {
	return fw.r.Read(p)
}

func (fw flushWriter) Close() error {
	return nil
}

// parseBasicAuth parses an HTTP Basic Authentication string.
// "Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==" returns ("Aladdin", "open sesame", true).
func parseBasicAuth(auth string) (username, password string, ok bool) {
	const prefix = "Basic "
	// Case insensitive prefix match. See Issue 22736.
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return
	}
	c, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return
	}
	cs := string(c)
	s := strings.IndexByte(cs, ':')
	if s < 0 {
		return
	}
	return cs[:s], cs[s+1:], true
}
