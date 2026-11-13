package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMS_FromMS_RoundTripAtMillisecondPrecision(t *testing.T) {
	in := time.Date(2026, 11, 13, 12, 30, 45, 123456789, time.FixedZone("x", 3600))
	got := fromMS(ms(in))
	assert.Equal(t, in.UnixMilli(), got.UnixMilli())
	assert.Equal(t, time.UTC, got.Location())
	assert.Equal(t, int64(0), ms(time.Unix(0, 0)))
}
