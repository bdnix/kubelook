//go:build integration

package it

import (
	"testing"
	"time"
)

// Eventually polls cond every 250 ms until it is true or d has passed, then fails with msg.
// It is the only way the integration tests wait for a condition.
func Eventually(t testing.TB, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s: %s", d, msg)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
