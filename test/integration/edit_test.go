//go:build integration

package integration

import (
	"fmt"
	"net/http"
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
