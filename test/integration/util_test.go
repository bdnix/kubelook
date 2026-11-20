//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/bdnix/kubelook/test/integration/it"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func unmarshal(data []byte, v any) { _ = json.Unmarshal(data, v) }

// podOf returns the name of a running pod of a demo workload, e.g. podOf(t, "alpha", "logger").
func podOf(t *testing.T, cluster, app string) string {
	t.Helper()
	return it.Kubectl(t, cluster, "-n", "demo", "get", "pods", "-l", "app="+app, "--field-selector=status.phase=Running",
		"-o", "jsonpath={.items[0].metadata.name}")
}

// runIn runs a command in a directory and fails the test when it fails.
func runIn(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // G204: test helper
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
