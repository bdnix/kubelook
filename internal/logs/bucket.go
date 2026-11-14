package logs

import (
	"time"

	"github.com/bdnix/kubelook/internal/clock"
)

// bucket is a token bucket: it allows rate events per second with a burst of one second's worth.
type bucket struct {
	clock  clock.Clock
	rate   float64
	tokens float64
	last   time.Time
}

func newBucket(c clock.Clock, perSecond int) *bucket {
	return &bucket{clock: c, rate: float64(perSecond), tokens: float64(perSecond), last: c.Now()}
}

// allow takes one token; it reports false when none is left.
func (b *bucket) allow() bool {
	now := b.clock.Now()
	b.tokens = min(b.rate, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
