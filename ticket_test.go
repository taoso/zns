package zns

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRepo(t *testing.T) {
	r := NewTicketRepo(":memory:")

	err := r.New("foo", 100, "buy-1", "pay-1")
	assert.Nil(t, err)

	ts, err := r.List("foo", 2)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ts))
	assert.Equal(t, "foo", ts[0].Token)
	assert.Equal(t, 100, ts[0].Bytes)
	assert.Equal(t, 100, ts[0].TotalBytes)
	assert.Equal(t, "pay-1", ts[0].PayOrder)

	n := time.Now()
	exp := n.AddDate(0, 1, -n.Day()+1)
	year, month, day := exp.Date()
	exp = time.Date(year, month, day, 0, 0, 0, 0, exp.Location())
	assert.Equal(t, exp, ts[0].Expires)
	assert.Equal(t, ts[0].Created, ts[0].Updated)
	assert.Equal(t, n.Truncate(time.Second), ts[0].Created.Truncate(time.Second))

	err = r.Cost("foo", 50)
	assert.Nil(t, err)

	ts, err = r.List("foo", 2)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ts))
	assert.Equal(t, 50, ts[0].Bytes)

	err = r.New("foo", 30, "buy-2", "pay-2")
	assert.Nil(t, err)

	err = r.New("foo", 40, "buy-3", "pay-3")
	assert.Nil(t, err)

	err = r.Cost("foo", 110)
	assert.Nil(t, err)

	ts, err = r.List("foo", 4)
	assert.Nil(t, err)
	assert.Equal(t, 3, len(ts))
	assert.Equal(t, 10, ts[0].Bytes)
	assert.Equal(t, 0, ts[1].Bytes)
	assert.Equal(t, 0, ts[2].Bytes)

	err = r.Cost("foo", 20)
	assert.Nil(t, err)

	ts, err = r.List("foo", 1)
	assert.Nil(t, err)
	assert.Equal(t, -10, ts[0].Bytes)

	err = r.New("foo", 40, "buy-4", "pay-4")
	assert.Nil(t, err)

	err = r.New("foo", 10, "buy-5", "pay-5")
	assert.Nil(t, err)

	err = r.Cost("foo", 65)
	assert.Nil(t, err)

	ts, err = r.List("foo", 1)
	assert.Nil(t, err)
	assert.Equal(t, -15, ts[0].Bytes)
}
