// Package resourcemap builds the graph of objects and the links between them that the Resource Map shows. It works
// on the objects the watch hub holds and puts only names, kinds and links into the graph: nothing a ConfigMap or a
// Secret holds ever reaches the browser.
package resourcemap

import (
	"cmp"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

// Kinds are the kinds the map reads: what the rows of the spec's table need, and ReplicaSets so a Deployment can
// reach its pods.
var Kinds = []Kind{
	{"", "Pod", "pods"},
	{"", "ConfigMap", "configmaps"},
	{"", "Secret", "secrets"},
	{"", "PersistentVolumeClaim", "persistentvolumeclaims"},
	{"", "Service", "services"},
	{"apps", "ReplicaSet", "replicasets"},
	{"apps", "Deployment", "deployments"},
	{"apps", "StatefulSet", "statefulsets"},
	{"apps", "DaemonSet", "daemonsets"},
	{"networking.k8s.io", "Ingress", "ingresses"},
	{"gateway.networking.k8s.io", "HTTPRoute", "httproutes"},
	{"gateway.networking.k8s.io", "GRPCRoute", "grpcroutes"},
}

// Node is an object on the map. The ID is `Kind/namespace/name`.
type Node struct {
	ID        string `json:"id"`
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Status is the phase of a pod; other kinds have none.
	Status string `json:"status,omitempty"`
}

// Edge links two nodes. Type is uses, owns, selects, routes, tls or helm.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// Graph is what the browser draws.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
	// OmittedKinds are the kinds the user may not list: the map is drawn without them.
	OmittedKinds []string `json:"omittedKinds"`
	// LeftOutNamespaces are the selected namespaces a map of this size could not watch.
	LeftOutNamespaces []string `json:"leftOutNamespaces,omitempty"`
}

func nodeID(kind, namespace, name string) string { return kind + "/" + namespace + "/" + name }

type obj = unstructured.Unstructured

type builder struct {
	byID   map[string]*obj
	nodes  map[string]Node
	edges  map[Edge]bool
	owners map[string]*obj // by `Kind/ns/name`
}

func strAt(o *obj, fields ...string) string {
	v, _, _ := unstructured.NestedString(o.Object, fields...)
	return v
}

func listAt(o *obj, fields ...string) []any {
	v, _, _ := unstructured.NestedSlice(o.Object, fields...)
	return v
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func (b *builder) add(o *obj) Node {
	id := nodeID(o.GetKind(), o.GetNamespace(), o.GetName())
	n := Node{ID: id, Kind: o.GetKind(), Namespace: o.GetNamespace(), Name: o.GetName()}
	if i := strings.Index(o.GetAPIVersion(), "/"); i >= 0 {
		n.Group = o.GetAPIVersion()[:i]
	}
	if o.GetKind() == "Pod" {
		n.Status = strAt(o, "status", "phase")
	}
	b.nodes[id] = n
	return n
}

func (b *builder) link(from, to, typ string) { b.edges[Edge{From: from, To: to, Type: typ}] = true }

// linkTo adds the edge only when the target object is in the input.
func (b *builder) linkTo(from string, kind, namespace, name, typ string) {
	id := nodeID(kind, namespace, name)
	if _, ok := b.byID[id]; ok {
		b.link(from, id, typ)
	}
}

func controllerOf(o *obj) (kind, name string) {
	for _, r := range o.GetOwnerReferences() {
		if r.Controller != nil && *r.Controller {
			return r.Kind, r.Name
		}
	}
	return "", ""
}

func podRefs(p *obj) (configMaps, secrets, claims []string) {
	var containers []any
	containers = append(containers, listAt(p, "spec", "containers")...)
	containers = append(containers, listAt(p, "spec", "initContainers")...)
	for _, c := range containers {
		cm := asMap(c)
		for _, e := range asAnySlice(cm["envFrom"]) {
			em := asMap(e)
			if n, _ := asMap(em["configMapRef"])["name"].(string); n != "" {
				configMaps = append(configMaps, n)
			}
			if n, _ := asMap(em["secretRef"])["name"].(string); n != "" {
				secrets = append(secrets, n)
			}
		}
		for _, e := range asAnySlice(cm["env"]) {
			// a configMapKeyRef gives no link, as the spec says; a secretKeyRef does
			if n, _ := asMap(asMap(asMap(e)["valueFrom"])["secretKeyRef"])["name"].(string); n != "" {
				secrets = append(secrets, n)
			}
		}
	}
	for _, v := range listAt(p, "spec", "volumes") {
		vm := asMap(v)
		if n, _ := asMap(vm["configMap"])["name"].(string); n != "" {
			configMaps = append(configMaps, n)
		}
		if n, _ := asMap(vm["secret"])["secretName"].(string); n != "" {
			secrets = append(secrets, n)
		}
		if n, _ := asMap(vm["persistentVolumeClaim"])["claimName"].(string); n != "" {
			claims = append(claims, n)
		}
	}
	return configMaps, secrets, claims
}

func asAnySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func (b *builder) pod(p *obj) {
	id := nodeID("Pod", p.GetNamespace(), p.GetName())
	cms, secrets, claims := podRefs(p)
	for _, n := range cms {
		b.linkTo(id, "ConfigMap", p.GetNamespace(), n, "uses")
	}
	for _, n := range secrets {
		b.linkTo(id, "Secret", p.GetNamespace(), n, "uses")
	}
	for _, n := range claims {
		b.linkTo(id, "PersistentVolumeClaim", p.GetNamespace(), n, "uses")
	}
	// the workload that owns the pod, through a ReplicaSet for a Deployment
	kind, name := controllerOf(p)
	if kind == "ReplicaSet" {
		rs, ok := b.owners[nodeID("ReplicaSet", p.GetNamespace(), name)]
		if !ok {
			return
		}
		kind, name = controllerOf(rs)
	}
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet":
		if _, ok := b.byID[nodeID(kind, p.GetNamespace(), name)]; ok {
			b.link(nodeID(kind, p.GetNamespace(), name), id, "owns")
		}
	}
}

func (b *builder) helm(w *obj) {
	name := w.GetAnnotations()["meta.helm.sh/release-name"]
	namespace := w.GetAnnotations()["meta.helm.sh/release-namespace"]
	if w.GetLabels()["app.kubernetes.io/managed-by"] != "Helm" || name == "" || namespace == "" {
		return
	}
	id := nodeID("HelmRelease", namespace, name)
	b.nodes[id] = Node{ID: id, Group: "helm.sh", Kind: "HelmRelease", Namespace: namespace, Name: name}
	b.link(nodeID(w.GetKind(), w.GetNamespace(), w.GetName()), id, "helm")
}

func (b *builder) service(s *obj, pods []*obj) {
	sel, _, _ := unstructured.NestedStringMap(s.Object, "spec", "selector")
	if len(sel) == 0 {
		return
	}
	set := labels.SelectorFromSet(sel)
	id := nodeID("Service", s.GetNamespace(), s.GetName())
	for _, p := range pods {
		if p.GetNamespace() == s.GetNamespace() && set.Matches(labels.Set(p.GetLabels())) {
			b.link(id, nodeID("Pod", p.GetNamespace(), p.GetName()), "selects")
		}
	}
}

func (b *builder) ingress(i *obj) {
	id := nodeID("Ingress", i.GetNamespace(), i.GetName())
	for _, r := range listAt(i, "spec", "rules") {
		for _, p := range asAnySlice(asMap(asMap(r)["http"])["paths"]) {
			if n, _ := asMap(asMap(asMap(p)["backend"])["service"])["name"].(string); n != "" {
				b.linkTo(id, "Service", i.GetNamespace(), n, "routes")
			}
		}
	}
	for _, t := range listAt(i, "spec", "tls") {
		if n, _ := asMap(t)["secretName"].(string); n != "" {
			b.linkTo(id, "Secret", i.GetNamespace(), n, "tls")
		}
	}
}

func (b *builder) route(r *obj) {
	id := nodeID(r.GetKind(), r.GetNamespace(), r.GetName())
	for _, rule := range listAt(r, "spec", "rules") {
		for _, ref := range asAnySlice(asMap(rule)["backendRefs"]) {
			m := asMap(ref)
			if k, _ := m["kind"].(string); k != "" && k != "Service" {
				continue
			}
			name, _ := m["name"].(string)
			ns, _ := m["namespace"].(string)
			if ns == "" {
				ns = r.GetNamespace()
			}
			b.linkTo(id, "Service", ns, name, "routes")
		}
	}
}

// Build draws the graph of the objects. Objects of kinds the map does not know are ignored. The result is sorted,
// so the same objects always give the same graph.
func Build(objects []*unstructured.Unstructured) Graph {
	b := &builder{byID: map[string]*obj{}, nodes: map[string]Node{}, edges: map[Edge]bool{}, owners: map[string]*obj{}}
	known := map[string]bool{}
	for _, k := range Kinds {
		known[k.Kind] = true
	}
	var pods []*obj
	for _, o := range objects {
		if !known[o.GetKind()] {
			continue
		}
		b.byID[nodeID(o.GetKind(), o.GetNamespace(), o.GetName())] = o
		if o.GetKind() == "ReplicaSet" {
			b.owners[nodeID("ReplicaSet", o.GetNamespace(), o.GetName())] = o
			continue // ReplicaSets are not drawn
		}
		b.add(o)
		if o.GetKind() == "Pod" {
			pods = append(pods, o)
		}
	}
	for _, o := range b.byID {
		switch o.GetKind() {
		case "Pod":
			b.pod(o)
		case "Deployment", "StatefulSet", "DaemonSet":
			b.helm(o)
		case "Service":
			b.service(o, pods)
		case "Ingress":
			b.ingress(o)
		case "HTTPRoute", "GRPCRoute":
			b.route(o)
		}
	}
	return finish(b)
}

// usedOnly are the kinds that appear on the map only when something links to them: a cluster holds hundreds of
// ConfigMaps and Secrets that no pod uses.
var usedOnly = map[string]bool{"ConfigMap": true, "Secret": true, "PersistentVolumeClaim": true}

func finish(b *builder) Graph {
	linked := map[string]bool{}
	for e := range b.edges {
		linked[e.From], linked[e.To] = true, true
	}
	for id, n := range b.nodes {
		if usedOnly[n.Kind] && !linked[id] {
			delete(b.nodes, id)
		}
	}
	g := Graph{Nodes: make([]Node, 0, len(b.nodes)), Edges: make([]Edge, 0, len(b.edges)), OmittedKinds: []string{}}
	for _, n := range b.nodes {
		g.Nodes = append(g.Nodes, n)
	}
	slices.SortFunc(g.Nodes, func(a, c Node) int { return cmp.Compare(a.ID, c.ID) })
	for e := range b.edges {
		g.Edges = append(g.Edges, e)
	}
	slices.SortFunc(g.Edges, func(a, c Edge) int {
		return cmp.Or(cmp.Compare(a.From, c.From), cmp.Compare(a.To, c.To), cmp.Compare(a.Type, c.Type))
	})
	return g
}

// Focus keeps the connected component of the object with the id: the object, everything linked to it, and so on.
// An object that is not on the map gives an empty graph.
func Focus(g Graph, id string) Graph {
	adj := map[string][]string{}
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	seen := map[string]bool{}
	if slices.ContainsFunc(g.Nodes, func(n Node) bool { return n.ID == id }) {
		queue := []string{id}
		seen[id] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, next := range adj[cur] {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
	}
	out := Graph{Nodes: []Node{}, Edges: []Edge{}, OmittedKinds: g.OmittedKinds, LeftOutNamespaces: g.LeftOutNamespaces}
	for _, n := range g.Nodes {
		if seen[n.ID] {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range g.Edges {
		if seen[e.From] && seen[e.To] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}
