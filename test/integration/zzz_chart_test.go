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

// Every test of this package runs against Kubelook installed from its Helm chart (hack/it-kubelook.sh). This one
// checks the chart's own life cycle on that release: an upgrade rolls the console out with the new configuration,
// and an uninstall leaves no pod. The release is gone afterwards, so it must run last: Go runs a package's test files
// in name order, and this file name sorts after every other (zz_drain_test.go included). CI runs the browser suite
// before the Go suite, and make it-up installs the release again before another run.
func TestChart_UpgradeRollsOutTheNewConfigurationAndUninstallLeavesNoPods(t *testing.T) {
	t.Log(it.Kubelook(t, "install", "--set", "maintenance.banner=upgraded"))
	c := signedIn(t, "chart-after-upgrade")
	var me struct {
		MaintenanceBanner string `json:"maintenanceBanner"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, "/api/v1/me", nil, &me))
	assert.Equal(t, "upgraded", me.MaintenanceBanner)

	t.Log(it.Kubelook(t, "uninstall"))
	start := time.Now()
	it.Eventually(t, 120*time.Second, func() bool {
		return it.Kubectl(t, "host", "-n", "kubelook", "get", "pods", "-l", "app.kubernetes.io/instance=kubelook", "-o", "name") == ""
	}, "no pod of the release is left")
	t.Logf("the last pod was gone %s after the uninstall returned", time.Since(start).Round(time.Second))
}
