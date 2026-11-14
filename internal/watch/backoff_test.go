package watch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackoff_DoublesUpToTheMaximumAndResets(t *testing.T) {
	b := newBackoff(time.Second, 30*time.Second)
	var got []time.Duration
	for range 8 {
		got = append(got, b.next())
	}
	assert.Equal(t, []time.Duration{1, 2, 4, 8, 16, 30, 30, 30}, secs(got))
	b.reset()
	assert.Equal(t, time.Second, b.next())
}

func secs(in []time.Duration) []time.Duration {
	out := make([]time.Duration, len(in))
	for i, d := range in {
		out[i] = d / time.Second
	}
	return out
}
