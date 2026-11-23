//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

// TestChart_InstalledConsoleWorks_UpgradesAndUninstalls runs against the console that hack/it-chart.sh installed from
// the Helm chart (KUBELOOK_IT_BASE_URL points at it). It is the last test of the package, because it uninstalls the
// release; it is skipped when the variable is unset, which is the case in `make it`.
func TestChart_InstalledConsoleWorks_UpgradesAndUninstalls(t *testing.T) {
	if os.Getenv("KUBELOOK_IT_BASE_URL") == "" {
		t.Skip("KUBELOOK_IT_BASE_URL is not set: run hack/it-chart.sh first")
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the console's log:\n%s", it.Kubectl(t, "alpha", "-n", "kubelook", "logs", "deployment/kubelook", "--tail=80"))
		}
	})

	// The Vault round trip: sign in, add alice's token, see v:alpha, open it, list pods, receive a log line.
	c := signedIn(t, "chart-alice")
	status, _, apiErr := addToken(t, c, "ALICE_TOKEN")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	got := vaultClusters(t, c)
	require.Len(t, got, 1, "clusters: %+v", got)
	require.Equal(t, "v:alpha", got[0].ID)
	status, _, apiErr = open(t, c, "v:alpha", "devops-role")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	status, pods := listItems(t, c, "v:alpha", "core", "v1", "pods", "demo")
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, strings.Join(names(pods), " "), "logger")

	pod := podOf(t, "alpha", "logger")
	ch := c.WS().Open("logs.open", map[string]any{"cluster": "v:alpha", "namespace": "demo", "pod": pod, "follow": true})
	var lines linesFrame
	require.NoError(t, json.Unmarshal(ch.Next("lines", 30*time.Second), &lines))
	require.NotEmpty(t, lines.Lines)

	// An upgrade rolls the console out with the new configuration. The port-forward ended with the old pod.
	root := it.Root()
	kubeconfig := filepath.Join(root, ".it", "kubeconfig-alpha-host.yaml")
	helm := filepath.Join(root, "bin", "helm")
	runIn(t, root, helm, "--kubeconfig", kubeconfig, "upgrade", "kubelook", "deploy/helm/kubelook", "-n", "kubelook",
		"--reuse-values", "--set", "maintenance.banner=upgraded", "--wait", "--timeout", "180s")
	runIn(t, root, filepath.Join(root, "hack", "it-chart.sh"), "forward")
	after := signedIn(t, "chart-after-upgrade")
	var me struct {
		MaintenanceBanner string `json:"maintenanceBanner"`
	}
	require.Equal(t, http.StatusOK, after.JSON(http.MethodGet, "/api/v1/me", nil, &me))
	assert.Equal(t, "upgraded", me.MaintenanceBanner)

	// An uninstall leaves no pods.
	runIn(t, root, helm, "--kubeconfig", kubeconfig, "uninstall", "kubelook", "-n", "kubelook", "--wait", "--timeout", "180s")
	it.Eventually(t, 60*time.Second, func() bool {
		return it.Kubectl(t, "alpha", "-n", "kubelook", "get", "pods", "-o", "name") == ""
	}, "no pod of the release is left")
}
