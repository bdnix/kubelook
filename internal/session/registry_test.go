package session_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/session"
)

func TestRegistry_RegisterCountHashes(t *testing.T) {
	r := session.NewRegistry()
	assert.Empty(t, r.Hashes())
	assert.Zero(t, r.Count("b"))

	un1 := r.Register("b", "x", func() error { return nil })
	r.Register("b", "y", func() error { return nil })
	r.Register("a", "z", func() error { return nil })
	assert.Equal(t, 2, r.Count("b"))
	assert.Equal(t, []string{"a", "b"}, r.Hashes())

	un1()
	un1() // idempotent
	assert.Equal(t, 1, r.Count("b"))
}

func TestRegistry_Unregister_PreventsClose(t *testing.T) {
	r := session.NewRegistry()
	called := false
	un := r.Register("h", "x", func() error { called = true; return nil })
	un()
	require.NoError(t, r.Close("h"))
	assert.False(t, called)
	assert.Empty(t, r.Hashes(), "an empty hash is not listed")
}

func TestRegistry_Close_RunsInOrderWithoutHoldingTheLock(t *testing.T) {
	r := session.NewRegistry()
	var order []string
	var un2 func()
	r.Register("h", "first", func() error { order = append(order, "first"); un2(); return nil }) // closer calls unregister
	un2 = r.Register("h", "second", func() error { order = append(order, "second"); return errors.New("bad") })

	err := r.Close("h")
	assert.Equal(t, []string{"first", "second"}, order)
	assert.ErrorContains(t, err, "second")
	assert.ErrorContains(t, err, "bad")
	assert.Zero(t, r.Count("h"))
}

func TestRegistry_RegisterAfterClose_ClosesImmediately(t *testing.T) {
	r := session.NewRegistry()
	require.NoError(t, r.Close("h"))
	called := 0
	un := r.Register("h", "late", func() error { called++; return nil })
	assert.Equal(t, 1, called)
	assert.Zero(t, r.Count("h"))
	un()
	assert.Equal(t, 1, called)

	r.Register("other", "fine", func() error { called++; return nil })
	assert.Equal(t, 1, called, "other sessions are unaffected")
}

func TestRegistry_EndedMemoryIsBounded(t *testing.T) {
	r := session.NewRegistry()
	for i := 0; i < session.EndedCapacity+10; i++ {
		require.NoError(t, r.Close(string(rune('a'+i%26))+string(rune(i))))
	}
	// the very first ended hash has been forgotten, so a late registration is kept
	called := false
	r.Register("a"+string(rune(0)), "x", func() error { called = true; return nil })
	assert.False(t, called)
	assert.Equal(t, 1, r.Count("a"+string(rune(0))))
}
