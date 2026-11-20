//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

func target(resource, group, ns, name string) map[string]string {
	return map[string]string{"group": group, "version": "v1", "resource": resource, "namespace": ns, "name": name}
}

func doAction(t *testing.T, c *it.Client, action string, tgt map[string]string, params any) (int, it.APIError) {
	t.Helper()
	var apiErr it.APIError
	status := c.JSON(http.MethodPost, "/api/v1/clusters/v:alpha/actions", map[string]any{"action": action, "target": tgt, "params": params}, &apiErr)
	return status, apiErr
}

// deploy creates a one-replica Deployment of a pause container in a fresh namespace.
func deploy(t *testing.T, name string) string {
	t.Helper()
	ns := it.Namespace(t, "alpha")
	it.Kubectl(t, "alpha", "-n", ns, "create", "deployment", name, "--image=registry.k8s.io/pause:3.10", "--replicas=1")
	it.Kubectl(t, "alpha", "-n", ns, "rollout", "status", "deployment/"+name, "--timeout=120s")
	return ns
}

func TestActions_ScaleRestartAndRollback(t *testing.T) {
	c := devopsClient(t, "actions-workload")
	ns := deploy(t, "app")
	dep := target("deployments", "apps", ns, "app")

	status, apiErr := doAction(t, c, "scale", dep, map[string]int{"replicas": 3})
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	it.Eventually(t, 60*time.Second, func() bool {
		return it.Kubectl(t, "alpha", "-n", ns, "get", "deployment", "app", "-o", "jsonpath={.spec.replicas}") == "3"
	}, "the deployment has 3 replicas")

	status, apiErr = doAction(t, c, "restart", dep, nil)
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	annotation := it.Kubectl(t, "alpha", "-n", ns, "get", "deployment", "app", "-o", `jsonpath={.spec.template.metadata.annotations}`)
	assert.Contains(t, annotation, "restartedAt")

	// change the image (revision 2 or later), then roll back to revision 1
	it.Kubectl(t, "alpha", "-n", ns, "set", "image", "deployment/app", "pause=busybox:1.37")
	it.Kubectl(t, "alpha", "-n", ns, "rollout", "status", "deployment/app", "--timeout=120s")
	status, apiErr = doAction(t, c, "rollback", dep, map[string]int{"revision": 1})
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	it.Eventually(t, 60*time.Second, func() bool {
		return strings.Contains(it.Kubectl(t, "alpha", "-n", ns, "get", "deployment", "app", "-o", "jsonpath={.spec.template.spec.containers[0].image}"), "pause")
	}, "the image is back to the first revision's")
}

func TestActions_EvictingAPodProtectedByAZeroBudget_IsAConflictWithThePDBMessage(t *testing.T) {
	c := devopsClient(t, "actions-evict")
	pod := podOf(t, "alpha", "web")
	status, apiErr := doAction(t, c, "evict", target("pods", "", "demo", pod), nil)
	assert.Equal(t, http.StatusConflict, status, "%+v", apiErr)
	assert.Contains(t, strings.ToLower(apiErr.Error.Message), "disruption budget")
}
