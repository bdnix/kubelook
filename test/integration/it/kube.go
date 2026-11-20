//go:build integration

package it

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

// Namespace creates a namespace "it-<random>" in the cluster ("alpha" or "beta") and deletes it
// when the test ends, so tests do not depend on each other.
func Namespace(t testing.TB, cluster string) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	ns := "it-" + hex.EncodeToString(b)
	Kubectl(t, cluster, "create", "namespace", ns)
	t.Cleanup(func() {
		Kubectl(t, cluster, "delete", "namespace", ns, "--wait=false", "--ignore-not-found")
	})
	return ns
}
