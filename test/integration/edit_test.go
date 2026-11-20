//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

func cmPath(ns, name, query string) string {
	return fmt.Sprintf("/api/v1/clusters/v:alpha/resources/core/v1/configmaps/%s?namespace=%s%s", name, ns, query)
}

func getCM(t *testing.T, c *it.Client, ns, name string) map[string]any {
	t.Helper()
	var obj map[string]any
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, cmPath(ns, name, ""), nil, &obj))
	return obj
}

func setData(obj map[string]any, key, value string) {
	obj["data"] = map[string]any{key: value}
}

func TestEdit_DryRunDoesNotChangeTheObject_AndARealPutDoesAsFieldManagerKubelook(t *testing.T) {
	c := devopsClient(t, "edit-put")
	ns := it.Namespace(t, "alpha")
	it.Kubectl(t, "alpha", "-n", ns, "create", "configmap", "cm1", "--from-literal=a=1")

	obj := getCM(t, c, ns, "cm1")
	setData(obj, "a", "2")
	var dry struct {
		Live   map[string]any `json:"live"`
		Result map[string]any `json:"result"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodPut, cmPath(ns, "cm1", "&dryRun=true"), obj, &dry))
	assert.NotEmpty(t, dry.Live)
	assert.Equal(t, "2", dry.Result["data"].(map[string]any)["a"])
	assert.Equal(t, "1", it.Kubectl(t, "alpha", "-n", ns, "get", "configmap", "cm1", "-o", "jsonpath={.data.a}"), "a dry run changes nothing")

	require.Equal(t, http.StatusOK, c.JSON(http.MethodPut, cmPath(ns, "cm1", ""), obj, nil))
	assert.Equal(t, "2", it.Kubectl(t, "alpha", "-n", ns, "get", "configmap", "cm1", "-o", "jsonpath={.data.a}"))
	managers := it.Kubectl(t, "alpha", "-n", ns, "get", "configmap", "cm1", "--show-managed-fields", "-o", "jsonpath={.metadata.managedFields[*].manager}")
	assert.Contains(t, managers, "kubelook")
}

func TestEdit_AStaleResourceVersion_IsAConflict(t *testing.T) {
	c := devopsClient(t, "edit-conflict")
	ns := it.Namespace(t, "alpha")
	it.Kubectl(t, "alpha", "-n", ns, "create", "configmap", "cm1", "--from-literal=a=1")
	stale := getCM(t, c, ns, "cm1")
	it.Kubectl(t, "alpha", "-n", ns, "patch", "configmap", "cm1", "-p", `{"data":{"a":"changed elsewhere"}}`)

	setData(stale, "a", "mine")
	status, _ := c.Do(http.MethodPut, cmPath(ns, "cm1", ""), mustJSON(t, stale), nil)
	assert.Equal(t, http.StatusConflict, status)
}

func TestEdit_ApplyOfTwoDocuments_CreatesBothObjects(t *testing.T) {
	c := devopsClient(t, "edit-apply")
	ns := it.Namespace(t, "alpha")
	doc := func(name string) string {
		return fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: %s\ndata:\n  k: v\n", name, ns)
	}
	var res struct {
		Results []struct {
			Name, Action, Error string
		} `json:"results"`
	}
	status := c.JSON(http.MethodPost, "/api/v1/clusters/v:alpha/apply", map[string]any{"documents": []string{doc("one"), doc("two")}}, &res)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "one\ntwo", strings.TrimSpace(it.Kubectl(t, "alpha", "-n", ns, "get", "configmaps", "one", "two", "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")))
	for _, r := range res.Results {
		assert.Empty(t, r.Error)
	}
}

// TestEdit_ConflictRateOnABusyObject records how often a save of a Deployment is refused with 409 while it
// rolls out (spec "to confirm in the spike"). It asserts nothing about the rate: the count goes to the log and
// to .it/findings/yaml-save-conflicts.txt, and docs/spike-findings.md quotes it.
func TestEdit_ConflictRateOnABusyObject_IsRecordedNotAsserted(t *testing.T) {
	c := devopsClient(t, "edit-busy")
	ns := deploy(t, "busy")
	it.Kubectl(t, "alpha", "-n", ns, "scale", "deployment/busy", "--replicas=3")
	it.Kubectl(t, "alpha", "-n", ns, "set", "env", "deployment/busy", "IT_ROLLOUT=1") // the rollout runs while we save

	path := func(q string) string {
		return "/api/v1/clusters/v:alpha/resources/apps/v1/deployments/busy?namespace=" + ns + q
	}
	conflicts, saved := 0, 0
	for i := range 20 {
		var obj map[string]any
		require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, path(""), nil, &obj))
		md, _ := obj["metadata"].(map[string]any)
		ann, _ := md["annotations"].(map[string]any)
		if ann == nil {
			ann = map[string]any{}
		}
		ann["it/save"] = fmt.Sprint(i)
		md["annotations"] = ann
		switch status := c.JSON(http.MethodPut, path(""), obj, nil); status {
		case http.StatusOK:
			saved++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("save %d answered %d", i, status)
		}
	}
	line := fmt.Sprintf("20 saves of a Deployment during a rollout: %d saved, %d refused with 409", saved, conflicts)
	t.Log(line)
	dir := filepath.Join(it.Root(), ".it", "findings")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "yaml-save-conflicts.txt"), []byte(line+"\n"), 0o600))
}
