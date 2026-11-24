//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

// Every test of this package runs against Kubelook installed from its Helm chart (hack/it-kubelook.sh). This one,
// last in the package, checks the chart's own life cycle on that release: an upgrade rolls the console out with the
// new configuration, and an uninstall leaves no pod. It installs the release again at the end, so the environment
// stays usable (for the browser tests, and for running the suite again).
func TestChart_UpgradeRollsOutTheNewConfigurationAndUninstallLeavesNoPods(t *testing.T) {
	t.Cleanup(func() { it.Kubelook(t, "install") })

	it.Kubelook(t, "install", "--set", "maintenance.banner=upgraded")
	c := signedIn(t, "chart-after-upgrade")
	var me struct {
		MaintenanceBanner string `json:"maintenanceBanner"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, "/api/v1/me", nil, &me))
	assert.Equal(t, "upgraded", me.MaintenanceBanner)

	it.Kubelook(t, "uninstall")
	it.Eventually(t, 120*time.Second, func() bool {
		return it.Kubectl(t, "host", "-n", "kubelook", "get", "pods", "-l", "app.kubernetes.io/instance=kubelook", "-o", "name") == ""
	}, "no pod of the release is left")
}
