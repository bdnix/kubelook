// Package testutil holds helpers shared by tests. It is excluded from coverage.
package testutil

import (
	"testing"
	"time"
)

// Recv returns the next value from ch or fails the test after 5 seconds.
// The wait is a failure bound only; a passing test never waits for it.
func Recv[T any](t testing.TB, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for channel")
		panic("unreachable")
	}
}

// NoRecv fails if ch has a value ready right now.
func NoRecv[T any](t testing.TB, ch <-chan T) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected value on channel: %v", v)
	default:
	}
}
