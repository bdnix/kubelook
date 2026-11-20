package resourcemap_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/bdnix/kubelook/internal/resourcemap"
)

func o(apiVersion, kind, ns, name string, fields map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": map[string]any{"name": name}}}
	if ns != "" {
		u.SetNamespace(ns)
	}
	for k, v := range fields {
		u.Object[k] = v
	}
	return u
}

func owned(u *unstructured.Unstructured, kind, name string) *unstructured.Unstructured {
	apiVersion := "apps/v1"
	u.SetOwnerReferences(nil)
	u.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"apiVersion": apiVersion, "kind": kind, "name": name, "controller": true}}
	return u
}

func labelled(u *unstructured.Unstructured, labels map[string]string) *unstructured.Unstructured {
	u.SetLabels(labels)
	return u
}

func edges(g resourcemap.Graph) []string {
	var out []string
	for _, e := range g.Edges {
		out = append(out, e.From+" -> "+e.To+" ("+e.Type+")")
	}
	return out
}

func ids(g resourcemap.Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		out = append(out, n.ID)
	}
	return out
}

func container(extra map[string]any) map[string]any {
	c := map[string]any{"name": "app", "image": "x"}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

func pod(name string, spec map[string]any) *unstructured.Unstructured {
	return o("v1", "Pod", "ns", name, map[string]any{"spec": spec, "status": map[string]any{"phase": "Running"}})
}

func TestBuild_PodToConfigMapsSecretsAndClaims(t *testing.T) {
	p := pod("web-1", map[string]any{
		"containers": []any{container(map[string]any{
			"envFrom": []any{map[string]any{"configMapRef": map[string]any{"name": "cfg-env"}}, map[string]any{"secretRef": map[string]any{"name": "sec-env"}}},
			"env": []any{
				map[string]any{"name": "A", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "sec-key", "key": "k"}}},
				map[string]any{"name": "B", "valueFrom": map[string]any{"configMapKeyRef": map[string]any{"name": "cfg-key", "key": "k"}}},
			},
		})},
		"initContainers": []any{container(map[string]any{"envFrom": []any{map[string]any{"configMapRef": map[string]any{"name": "cfg-init"}}}})},
		"volumes": []any{
			map[string]any{"name": "v1", "configMap": map[string]any{"name": "cfg-vol"}},
			map[string]any{"name": "v2", "secret": map[string]any{"secretName": "sec-vol"}},
			map[string]any{"name": "v3", "persistentVolumeClaim": map[string]any{"claimName": "data"}},
			map[string]any{"name": "v4", "projected": map[string]any{"sources": []any{map[string]any{"configMap": map[string]any{"name": "cfg-proj"}}}}},
		},
	})
	var objs []*unstructured.Unstructured
	objs = append(objs, p)
	for _, n := range []string{"cfg-env", "cfg-key", "cfg-init", "cfg-vol", "cfg-proj"} {
		objs = append(objs, o("v1", "ConfigMap", "ns", n, map[string]any{"data": map[string]any{"password": "TOP-SECRET-CONFIG"}}))
	}
	for _, n := range []string{"sec-env", "sec-key", "sec-vol"} {
		objs = append(objs, o("v1", "Secret", "ns", n, map[string]any{"data": map[string]any{"password": "VE9QLVNFQ1JFVA=="}}))
	}
	objs = append(objs, o("v1", "PersistentVolumeClaim", "ns", "data", nil))
	g := resourcemap.Build(objs)

	assert.Equal(t, []string{
		"Pod/ns/web-1 -> ConfigMap/ns/cfg-env (uses)",
		"Pod/ns/web-1 -> ConfigMap/ns/cfg-init (uses)",
		"Pod/ns/web-1 -> ConfigMap/ns/cfg-vol (uses)",
		"Pod/ns/web-1 -> PersistentVolumeClaim/ns/data (uses)",
		"Pod/ns/web-1 -> Secret/ns/sec-env (uses)",
		"Pod/ns/web-1 -> Secret/ns/sec-key (uses)",
		"Pod/ns/web-1 -> Secret/ns/sec-vol (uses)",
	}, edges(g), "configMapKeyRef and projected volumes give no link")
	assert.NotContains(t, ids(g), "ConfigMap/ns/cfg-key")
	assert.NotContains(t, ids(g), "ConfigMap/ns/cfg-proj")
}

func TestBuild_NodesCarryNamesOnly_NoDataOfConfigMapsOrSecrets(t *testing.T) {
	p := pod("web-1", map[string]any{"volumes": []any{map[string]any{"name": "v", "secret": map[string]any{"secretName": "s"}}, map[string]any{"name": "c", "configMap": map[string]any{"name": "c"}}}})
	g := resourcemap.Build([]*unstructured.Unstructured{
		p,
		o("v1", "Secret", "ns", "s", map[string]any{"data": map[string]any{"password": "VE9QLVNFQ1JFVA=="}, "stringData": map[string]any{"k": "TOP-SECRET-STRING"}}),
		o("v1", "ConfigMap", "ns", "c", map[string]any{"data": map[string]any{"key": "TOP-SECRET-CONFIG"}}),
	})
	b, err := json.Marshal(g)
	require.NoError(t, err)
	out := string(b)
	for _, forbidden := range []string{`"data"`, "TOP-SECRET", "VE9QLVNFQ1JFVA", "stringData"} {
		assert.NotContains(t, out, forbidden)
	}
	assert.Contains(t, out, `"name":"s"`)
}

func TestBuild_WorkloadsToPods_ViaOwnersAndReplicaSets(t *testing.T) {
	dep := o("apps/v1", "Deployment", "ns", "web", nil)
	rs := owned(o("apps/v1", "ReplicaSet", "ns", "web-abc", nil), "Deployment", "web")
	p1 := owned(pod("web-abc-1", map[string]any{}), "ReplicaSet", "web-abc")
	sts := o("apps/v1", "StatefulSet", "ns", "db", nil)
	p2 := owned(pod("db-0", map[string]any{}), "StatefulSet", "db")
	ds := o("apps/v1", "DaemonSet", "ns", "agent", nil)
	p3 := owned(pod("agent-x", map[string]any{}), "DaemonSet", "agent")
	stray := owned(pod("stray", map[string]any{}), "ReplicaSet", "unknown-rs")
	g := resourcemap.Build([]*unstructured.Unstructured{dep, rs, p1, sts, p2, ds, p3, stray})
	assert.Equal(t, []string{
		"DaemonSet/ns/agent -> Pod/ns/agent-x (owns)",
		"Deployment/ns/web -> Pod/ns/web-abc-1 (owns)",
		"StatefulSet/ns/db -> Pod/ns/db-0 (owns)",
	}, edges(g))
	assert.NotContains(t, ids(g), "ReplicaSet/ns/web-abc", "ReplicaSets are not drawn")
}

func TestBuild_HelmReleaseFromTheWorkloadsAnnotationsAndLabel(t *testing.T) {
	dep := o("apps/v1", "Deployment", "ns", "web", nil)
	dep.SetAnnotations(map[string]string{"meta.helm.sh/release-name": "shop", "meta.helm.sh/release-namespace": "ns-rel"})
	labelled(dep, map[string]string{"app.kubernetes.io/managed-by": "Helm"})
	other := o("apps/v1", "Deployment", "ns", "plain", nil)
	other.SetAnnotations(map[string]string{"meta.helm.sh/release-name": "x", "meta.helm.sh/release-namespace": "ns"})
	g := resourcemap.Build([]*unstructured.Unstructured{dep, other})
	assert.Equal(t, []string{"Deployment/ns/web -> HelmRelease/ns-rel/shop (helm)"}, edges(g), "only workloads managed by Helm")
	var release resourcemap.Node
	for _, n := range g.Nodes {
		if n.Kind == "HelmRelease" {
			release = n
		}
	}
	assert.Equal(t, "shop", release.Name)
	assert.Empty(t, release.Status, "a release has no status on the map")
}

func TestBuild_ServiceToPodsBySelector(t *testing.T) {
	svc := o("v1", "Service", "ns", "web", map[string]any{"spec": map[string]any{"selector": map[string]any{"app": "web"}}})
	headless := o("v1", "Service", "ns", "noselector", map[string]any{"spec": map[string]any{}})
	match := labelled(pod("a", map[string]any{}), map[string]string{"app": "web", "extra": "1"})
	miss := labelled(pod("b", map[string]any{}), map[string]string{"app": "other"})
	elsewhere := labelled(o("v1", "Pod", "other-ns", "c", nil), map[string]string{"app": "web"})
	g := resourcemap.Build([]*unstructured.Unstructured{svc, headless, match, miss, elsewhere})
	assert.Equal(t, []string{"Service/ns/web -> Pod/ns/a (selects)"}, edges(g))
}

func TestBuild_IngressToServicesAndTLSSecrets_NotTheDefaultBackend(t *testing.T) {
	ing := o("networking.k8s.io/v1", "Ingress", "ns", "site", map[string]any{"spec": map[string]any{
		"defaultBackend": map[string]any{"service": map[string]any{"name": "fallback"}},
		"tls":            []any{map[string]any{"secretName": "tls-cert"}},
		"rules": []any{map[string]any{"http": map[string]any{"paths": []any{
			map[string]any{"backend": map[string]any{"service": map[string]any{"name": "web"}}},
			map[string]any{"backend": map[string]any{"service": map[string]any{"name": "api"}}},
		}}}},
	}})
	g := resourcemap.Build([]*unstructured.Unstructured{
		ing, o("v1", "Service", "ns", "web", nil), o("v1", "Service", "ns", "api", nil), o("v1", "Service", "ns", "fallback", nil), o("v1", "Secret", "ns", "tls-cert", nil),
	})
	assert.Equal(t, []string{
		"Ingress/ns/site -> Secret/ns/tls-cert (tls)",
		"Ingress/ns/site -> Service/ns/api (routes)",
		"Ingress/ns/site -> Service/ns/web (routes)",
	}, edges(g))
}

func TestBuild_RoutesToTheirBackendServices(t *testing.T) {
	route := func(kind, name string) *unstructured.Unstructured {
		return o("gateway.networking.k8s.io/v1", kind, "ns", name, map[string]any{"spec": map[string]any{"rules": []any{map[string]any{"backendRefs": []any{
			map[string]any{"name": "web"},
			map[string]any{"name": "api", "namespace": "ns2", "kind": "Service"},
			map[string]any{"name": "weird", "kind": "Other"},
		}}}}})
	}
	g := resourcemap.Build([]*unstructured.Unstructured{
		route("HTTPRoute", "r1"), route("GRPCRoute", "r2"),
		o("v1", "Service", "ns", "web", nil), o("v1", "Service", "ns2", "api", nil), o("v1", "Service", "ns", "weird", nil),
	})
	assert.Equal(t, []string{
		"GRPCRoute/ns/r2 -> Service/ns/web (routes)",
		"GRPCRoute/ns/r2 -> Service/ns2/api (routes)",
		"HTTPRoute/ns/r1 -> Service/ns/web (routes)",
		"HTTPRoute/ns/r1 -> Service/ns2/api (routes)",
	}, edges(g))
}

func TestBuild_PodNodesCarryThePhase_OthersNone(t *testing.T) {
	failed := o("v1", "Pod", "ns", "bad", map[string]any{"status": map[string]any{"phase": "Failed"}})
	g := resourcemap.Build([]*unstructured.Unstructured{failed, o("v1", "Service", "ns", "svc", nil)})
	status := map[string]string{}
	for _, n := range g.Nodes {
		status[n.Kind] = n.Status
	}
	assert.Equal(t, "Failed", status["Pod"])
	assert.Empty(t, status["Service"])
}

func TestBuild_IsDeterministic_AndIgnoresWhatItDoesNotKnow(t *testing.T) {
	a := pod("a", map[string]any{"volumes": []any{map[string]any{"name": "v", "configMap": map[string]any{"name": "c"}}}})
	b := pod("b", map[string]any{"volumes": []any{map[string]any{"name": "v", "configMap": map[string]any{"name": "c"}}}})
	c := o("v1", "ConfigMap", "ns", "c", nil)
	noise := o("batch/v1", "Job", "ns", "job", nil)
	first := resourcemap.Build([]*unstructured.Unstructured{a, b, c, noise})
	second := resourcemap.Build([]*unstructured.Unstructured{noise, c, b, a})
	assert.Equal(t, first, second)
	assert.NotContains(t, strings.Join(ids(first), ","), "Job")
	assert.Equal(t, []string{"ConfigMap/ns/c", "Pod/ns/a", "Pod/ns/b"}, ids(first))
	assert.Equal(t, resourcemap.Graph{Nodes: []resourcemap.Node{}, Edges: []resourcemap.Edge{}, OmittedKinds: []string{}}, resourcemap.Build(nil))
}

func TestFocus_KeepsTheConnectedComponentOfTheFocusedObject(t *testing.T) {
	dep := o("apps/v1", "Deployment", "ns", "web", nil)
	rs := owned(o("apps/v1", "ReplicaSet", "ns", "web-abc", nil), "Deployment", "web")
	p := owned(pod("web-abc-1", map[string]any{"volumes": []any{map[string]any{"name": "v", "configMap": map[string]any{"name": "c"}}}}), "ReplicaSet", "web-abc")
	cm := o("v1", "ConfigMap", "ns", "c", nil)
	other := pod("lonely", map[string]any{})
	g := resourcemap.Build([]*unstructured.Unstructured{dep, rs, p, cm, other})
	focused := resourcemap.Focus(g, "Deployment/ns/web")
	assert.Equal(t, []string{"ConfigMap/ns/c", "Deployment/ns/web", "Pod/ns/web-abc-1"}, ids(focused))
	assert.Len(t, focused.Edges, 2)
	assert.Equal(t, resourcemap.Focus(g, "Pod/ns/nope").Nodes, []resourcemap.Node{}, "an object that is not on the map gives an empty graph")
}
