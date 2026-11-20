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

// This file sorts last on purpose: draining the only node of beta restarts every pod there.
func TestDrain_TheSingleNodeOfBeta_IsCordonedAndItsPodsAreEvicted(t *testing.T) {
	c := devopsClient(t, "drain")
	ns := it.Namespace(t, "beta")
	it.Kubectl(t, "beta", "-n", ns, "create", "deployment", "victim", "--image=registry.k8s.io/pause:3.10", "--replicas=1")
	it.Kubectl(t, "beta", "-n", ns, "rollout", "status", "deployment/victim", "--timeout=120s")
	before := it.Kubectl(t, "beta", "-n", ns, "get", "pods", "-l", "app=victim", "-o", "jsonpath={.items[0].metadata.uid}")
	node := it.Kubectl(t, "beta", "get", "nodes", "-o", "jsonpath={.items[0].metadata.name}")
	t.Cleanup(func() {
		it.Kubectl(t, "beta", "uncordon", node)
		it.Kubectl(t, "beta", "-n", "demo", "rollout", "status", "deployment/shell", "--timeout=180s")
	})

	var started struct {
		JobID string `json:"jobId"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodPost, "/api/v1/clusters/v:beta/nodes/"+node+"/drain", map[string]any{}, &started))
	require.NotEmpty(t, started.JobID)

	var job struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	it.Eventually(t, 5*time.Minute, func() bool {
		c.JSON(http.MethodGet, "/api/v1/jobs/"+started.JobID, nil, &job)
		return job.Status == "succeeded" || job.Status == "failed" || job.Status == "interrupted"
	}, "the drain job finishes")
	assert.Equal(t, "succeeded", job.Status, job.Message)

	assert.Equal(t, "true", it.Kubectl(t, "beta", "get", "node", node, "-o", "jsonpath={.spec.unschedulable}"), "the node is cordoned")
	after := it.Kubectl(t, "beta", "-n", ns, "get", "pods", "-l", "app=victim", "-o", "jsonpath={.items[*].metadata.uid}")
	assert.NotContains(t, after, before, "the original pod was evicted")
}
