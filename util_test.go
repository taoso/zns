package zns

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBytesCounter(t *testing.T) {
	b := bytes.NewBuffer(nil)
	i := 0
	bc := bytesCounter{
		w: b,
		d: 100 * time.Millisecond,
		f: func(n int) {
			i += n
		},
	}

	go bc.Start()

	for _, s := range []string{"a", "bc", "def"} {
		bc.Write([]byte(s))
	}

	var p [20]byte
	n, _ := bc.Read(p[:])
	s := string(p[:n])

	time.Sleep(1 * time.Second)

	bc.Done()

	assert.Equal(t, 12, i)
	assert.Equal(t, "abcdef", s)
}

func TestTrie(t *testing.T) {
	v := isBlackDomain("zeus.ad.xiaomi.com")
	assert.True(t, v)
	v = isBlackDomain("zeus.xiaomi.com")
	assert.False(t, v)
}
