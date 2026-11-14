package watch

import "time"

// backoff doubles its delay after every failure, up to a maximum, and starts over after a success.
type backoff struct {
	min, max, cur time.Duration
}

func newBackoff(first, ceiling time.Duration) *backoff { return &backoff{min: first, max: ceiling} }

// next returns how long to wait before the next attempt.
func (b *backoff) next() time.Duration {
	if b.cur == 0 {
		b.cur = b.min
	} else {
		b.cur = min(b.cur*2, b.max)
	}
	return b.cur
}

// reset forgets earlier failures.
func (b *backoff) reset() { b.cur = 0 }
