package zns

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRepo(t *testing.T) {
	r := NewTicketRepo(":memory:")

	err := r.NewTicket(Ticket{
		Token:    "foo",
		Cents:    100,
		Tier:     1,
		BuyOrder: "buy-1",
		PayOrder: "pay-1",
	})
	assert.Nil(t, err)

	ts, err := r.List("foo", 2)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ts))
	assert.Equal(t, "foo", ts[0].Token)
	assert.Equal(t, 1, ts[0].Tier)
	assert.Equal(t, "buy-1", ts[0].BuyOrder)
	assert.Equal(t, "pay-1", ts[0].PayOrder)

	n := time.Now()

	assert.True(t, ts[0].Expires.Before(n.Add(10*24*time.Hour)))
	assert.True(t, ts[0].Expires.After(n.Add(9*24*time.Hour)))
	assert.Equal(t, ts[0].Created, ts[0].Updated)
	assert.Equal(t, n.Truncate(time.Second), ts[0].Created.Truncate(time.Second))

	err = r.NewTicket(Ticket{
		Token: "foo",
		Cents: 300,
		Tier:  1,
	})
	assert.Nil(t, err)

	ts, err = r.List("foo", 2)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(ts))
	assert.True(t, ts[0].Expires.Equal(ts[1].Expires.Add(30*24*time.Hour)))
}
