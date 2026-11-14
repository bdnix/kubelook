package logs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/clock"
)

func TestBucket_AllowsTheRateWithinASecondAndRefills(t *testing.T) {
	c := clock.NewFake(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	b := newBucket(c, 10)
	allowed := 0
	for range 25 {
		if b.allow() {
			allowed++
		}
	}
	assert.Equal(t, 10, allowed)

	c.Advance(500 * time.Millisecond)
	allowed = 0
	for range 25 {
		if b.allow() {
			allowed++
		}
	}
	assert.Equal(t, 5, allowed, "half a second refills half the rate")

	c.Advance(time.Hour)
	allowed = 0
	for range 25 {
		if b.allow() {
			allowed++
		}
	}
	assert.Equal(t, 10, allowed, "the burst is one second's worth, however long it was idle")
}
