//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

// moduleStates returns GET /clusters/{id}/modules as a map of module name to "active" flag.
func moduleStates(t *testing.T, c *it.Client, id string) map[string]bool {
	t.Helper()
	var out struct {
		Modules []struct {
			Name   string `json:"name"`
			Active bool   `json:"active"`
		} `json:"modules"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, "/api/v1/clusters/"+it.ClusterPath(id)+"/modules", nil, &out))
	states := map[string]bool{}
	for _, m := range out.Modules {
		states[m.Name] = m.Active
	}
	return states
}

func TestModules_AlphaServesTheEcosystemCRDsAndBetaDoesNot(t *testing.T) {
	c := devopsClient(t, "modules-detect")

	alpha := moduleStates(t, c, "v:alpha")
	for _, name := range []string{"keda", "externalSecrets", "certManager", "vaultSecrets", "karpenter", "trivy", "flux"} {
		assert.True(t, alpha[name], "module %s is active on alpha", name)
	}
	assert.False(t, alpha["gatewayApi"], "the Gateway API CRDs are not installed on alpha")

	beta := moduleStates(t, c, "v:beta")
	for _, name := range []string{"keda", "externalSecrets", "certManager", "vaultSecrets", "karpenter", "trivy", "flux", "gatewayApi"} {
		assert.False(t, beta[name], "module %s is inactive on beta", name)
	}
	assert.True(t, beta["permissions"], "the permissions module needs only RBAC, which every cluster serves")
}

func customTarget(group, version, resource, ns, name string) map[string]string {
	return map[string]string{"group": group, "version": version, "resource": resource, "namespace": ns, "name": name}
}

// applyOnAlpha creates an object from a manifest with the admin kubeconfig of the test cluster.
func applyOnAlpha(t *testing.T, manifest string) {
	t.Helper()
	path := t.TempDir() + "/object.yaml"
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0o600))
	it.Kubectl(t, "alpha", "create", "-f", path)
}

func annotation(t *testing.T, ns, kind, name, key string) string {
	t.Helper()
	return it.Kubectl(t, "alpha", "-n", ns, "get", kind, name, "-o",
		"jsonpath={.metadata.annotations."+strings.ReplaceAll(key, ".", `\.`)+"}")
}

func TestModules_FluxReconcileSuspendAndResume(t *testing.T) {
	c := devopsClient(t, "modules-flux")
	ns := it.Namespace(t, "alpha")
	applyOnAlpha(t, fmt.Sprintf(`apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata: {name: apps, namespace: %s}
spec:
  interval: 10m
  path: ./apps
  prune: true
  sourceRef: {kind: GitRepository, name: repo}
`, ns))
	tgt := customTarget("kustomize.toolkit.fluxcd.io", "v1", "kustomizations", ns, "apps")

	status, apiErr := doAction(t, c, "fluxReconcile", tgt, nil)
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.NotEmpty(t, annotation(t, ns, "kustomizations.kustomize.toolkit.fluxcd.io", "apps", "reconcile.fluxcd.io/requestedAt"))

	status, apiErr = doAction(t, c, "fluxSuspend", tgt, nil)
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.Equal(t, "true", it.Kubectl(t, "alpha", "-n", ns, "get", "kustomizations.kustomize.toolkit.fluxcd.io", "apps", "-o", "jsonpath={.spec.suspend}"))

	status, apiErr = doAction(t, c, "fluxReconcile", tgt, nil)
	assert.Equal(t, http.StatusConflict, status, "a suspended object is not reconciled: %+v", apiErr)

	status, apiErr = doAction(t, c, "fluxResume", tgt, nil)
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.Equal(t, "false", it.Kubectl(t, "alpha", "-n", ns, "get", "kustomizations.kustomize.toolkit.fluxcd.io", "apps", "-o", "jsonpath={.spec.suspend}"))
}

func TestModules_KedaPauseAndResumeToggleTheAnnotation(t *testing.T) {
	c := devopsClient(t, "modules-keda")
	ns := it.Namespace(t, "alpha")
	applyOnAlpha(t, fmt.Sprintf(`apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata: {name: worker, namespace: %s}
spec:
  scaleTargetRef: {name: worker}
  triggers: [{type: cron, metadata: {timezone: UTC, start: "0 6 * * *", end: "0 20 * * *", desiredReplicas: "1"}}]
`, ns))
	tgt := customTarget("keda.sh", "v1alpha1", "scaledobjects", ns, "worker")
	const kind = "scaledobjects.keda.sh"
	const key = "autoscaling.keda.sh/paused"

	status, apiErr := doAction(t, c, "kedaPause", tgt, nil)
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.Equal(t, "true", annotation(t, ns, kind, "worker", key))

	status, apiErr = doAction(t, c, "kedaResume", tgt, nil)
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.Empty(t, annotation(t, ns, kind, "worker", key), "resume removes the annotation")
}

func TestModules_ExternalSecretsForceSyncSetsTheAnnotation(t *testing.T) {
	c := devopsClient(t, "modules-eso")
	ns := it.Namespace(t, "alpha")
	applyOnAlpha(t, fmt.Sprintf(`apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata: {name: db, namespace: %s}
spec:
  secretStoreRef: {name: store, kind: SecretStore}
  target: {name: db}
`, ns))
	status, apiErr := doAction(t, c, "esoForceSync", customTarget("external-secrets.io", "v1", "externalsecrets", ns, "db"), nil)
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	assert.NotEmpty(t, annotation(t, ns, "externalsecrets.external-secrets.io", "db", "force-sync"))
}

func TestModules_TrivyExposedSecretReportIsReturnedWithoutTheMatchedText(t *testing.T) {
	c := devopsClient(t, "modules-trivy")
	ns := it.Namespace(t, "alpha")
	applyOnAlpha(t, fmt.Sprintf(`apiVersion: aquasecurity.github.io/v1alpha1
kind: ExposedSecretReport
metadata: {name: replicaset-web, namespace: %s}
report:
  summary: {criticalCount: 1}
  secrets:
    - {ruleID: aws-access-key-id, category: AWS, severity: CRITICAL, title: AWS Access Key ID, target: /app/.env, match: "AWS_KEY=AKIAIOSFODNN7EXAMPLE"}
`, ns))
	var obj struct {
		Report struct {
			Secrets []map[string]any `json:"secrets"`
		} `json:"report"`
	}
	path := "/api/v1/clusters/v:alpha/resources/aquasecurity.github.io/v1alpha1/exposedsecretreports/replicaset-web?namespace=" + ns
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, path, nil, &obj))
	require.Len(t, obj.Report.Secrets, 1)
	assert.Equal(t, "aws-access-key-id", obj.Report.Secrets[0]["ruleID"], "the finding stays")
	assert.NotContains(t, obj.Report.Secrets[0], "match", "the text that matched is removed on the server")

	status, data := c.Do(http.MethodGet, path, nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.NotContains(t, string(data), "AKIAIOSFODNN7EXAMPLE")
}

func TestModules_CRDBrowser_CustomKindIsDiscoveredCreatedListedAndDescribedByItsCRD(t *testing.T) {
	c := devopsClient(t, "modules-crd")
	ns := it.Namespace(t, "alpha")

	var disc struct {
		Resources []struct {
			Group    string `json:"group"`
			Version  string `json:"version"`
			Resource string `json:"resource"`
			Kind     string `json:"kind"`
		} `json:"resources"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, "/api/v1/clusters/v:alpha/discovery", nil, &disc))
	found := false
	for _, r := range disc.Resources {
		if r.Group == "example.kubelook.dev" && r.Resource == "widgets" && r.Kind == "Widget" && r.Version == "v1" {
			found = true
		}
	}
	require.True(t, found, "discovery lists widgets.example.kubelook.dev")

	widget := map[string]any{
		"apiVersion": "example.kubelook.dev/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "w1", "namespace": ns},
		"spec":     map[string]any{"size": 3, "color": "green"},
	}
	var apiErr it.APIError
	status := c.JSON(http.MethodPost, "/api/v1/clusters/v:alpha/resources/example.kubelook.dev/v1/widgets?namespace="+ns, widget, &apiErr)
	require.Equal(t, http.StatusCreated, status, "%+v", apiErr)

	status, items := listItems(t, c, "v:alpha", "example.kubelook.dev", "v1", "widgets", ns)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, items, 1)
	assert.Equal(t, "w1", objName(items[0]))

	var crd struct {
		Spec struct {
			Versions []struct {
				Name    string `json:"name"`
				Columns []struct {
					Name     string `json:"name"`
					JSONPath string `json:"jsonPath"`
				} `json:"additionalPrinterColumns"`
			} `json:"versions"`
		} `json:"spec"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet,
		"/api/v1/clusters/v:alpha/resources/apiextensions.k8s.io/v1/customresourcedefinitions/widgets.example.kubelook.dev", nil, &crd))
	require.Len(t, crd.Spec.Versions, 1)
	cols := map[string]string{}
	for _, col := range crd.Spec.Versions[0].Columns {
		cols[col.Name] = col.JSONPath
	}
	assert.Equal(t, map[string]string{"Size": ".spec.size", "Color": ".spec.color"}, cols)
}
