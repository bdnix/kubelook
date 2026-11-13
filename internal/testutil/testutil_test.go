package testutil_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/testutil"
)

func TestRecv_ReturnsReadyValue(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 7
	assert.Equal(t, 7, testutil.Recv(t, ch))
}

func TestNoRecv_PassesOnEmptyChannel(t *testing.T) {
	testutil.NoRecv(t, make(chan int))
}

func TestRand_IsDeterministicPerSeed(t *testing.T) {
	read := func(seed string, n int) []byte {
		b := make([]byte, n)
		_, err := io.ReadFull(testutil.Rand(seed), b)
		require.NoError(t, err)
		return b
	}
	assert.Equal(t, read("a", 100), read("a", 100))
	assert.NotEqual(t, read("a", 32), read("b", 32))
	// reading in pieces equals reading at once
	r := testutil.Rand("a")
	one, two := make([]byte, 10), make([]byte, 90)
	_, _ = io.ReadFull(r, one)
	_, _ = io.ReadFull(r, two)
	assert.Equal(t, read("a", 100), append(one, two...))
}
