// Package fakekube is a kube.Clients backed by client-go fakes, for unit tests.
package fakekube

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// Clients implements kube.Clients (and kube.Invalidator) over fakes. The same fakes answer every
// session and cluster, unless Known limits the cluster IDs.
type Clients struct {
	Dyn  *dynamicfake.FakeDynamicClient
	Kube *kubefake.Clientset
	Disc *fakediscovery.FakeDiscovery
	// Known, when not nil, lists the cluster IDs that exist; any other ID answers ErrClusterUnavailable.
	Known map[string]bool

	mu          sync.Mutex
	discCalls   int
	invalidated int
}

var (
	_ kube.Clients     = (*Clients)(nil)
	_ kube.Invalidator = (*Clients)(nil)
)

// ListKinds are the list kinds the dynamic fake can list.
func ListKinds() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "pods"}:                       "PodList",
		{Version: "v1", Resource: "secrets"}:                    "SecretList",
		{Version: "v1", Resource: "configmaps"}:                 "ConfigMapList",
		{Version: "v1", Resource: "namespaces"}:                 "NamespaceList",
		{Version: "v1", Resource: "nodes"}:                      "NodeList",
		{Version: "v1", Resource: "services"}:                   "ServiceList",
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
	}
}

func resourceLists() []*metav1.APIResourceList {
	verbs := metav1.Verbs{"get", "list", "watch", "create", "update", "patch", "delete"}
	return []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{Name: "pods", Namespaced: true, Kind: "Pod", Verbs: verbs},
			{Name: "pods/exec", Namespaced: true, Kind: "Pod", Verbs: metav1.Verbs{"create", "get"}},
			{Name: "pods/log", Namespaced: true, Kind: "Pod", Verbs: metav1.Verbs{"get"}},
			{Name: "secrets", Namespaced: true, Kind: "Secret", Verbs: verbs},
			{Name: "configmaps", Namespaced: true, Kind: "ConfigMap", Verbs: verbs},
			{Name: "services", Namespaced: true, Kind: "Service", Verbs: verbs},
			{Name: "namespaces", Namespaced: false, Kind: "Namespace", Verbs: verbs},
			{Name: "nodes", Namespaced: false, Kind: "Node", Verbs: verbs},
		}},
		{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{
			{Name: "deployments", Namespaced: true, Kind: "Deployment", Verbs: verbs},
		}},
	}
}

// New returns fakes holding the unstructured objects (for the dynamic client).
func New(objects ...runtime.Object) *Clients {
	c := &Clients{
		Dyn:  dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), ListKinds(), objects...),
		Kube: kubefake.NewClientset(),
	}
	c.Disc = c.Kube.Discovery().(*fakediscovery.FakeDiscovery)
	c.Disc.Resources = resourceLists()
	return c
}

func (c *Clients) check(id string) error {
	if c.Known != nil && !c.Known[id] {
		return apperr.ErrClusterUnavailable
	}
	return nil
}

// Dynamic implements kube.Clients.
func (c *Clients) Dynamic(_ context.Context, _ session.Session, id string) (dynamic.Interface, error) {
	return c.Dyn, c.check(id)
}

// Typed implements kube.Clients.
func (c *Clients) Typed(_ context.Context, _ session.Session, id string) (kubernetes.Interface, error) {
	return c.Kube, c.check(id)
}

// REST implements kube.Clients.
func (c *Clients) REST(_ context.Context, _ session.Session, id string) (*rest.Config, error) {
	if err := c.check(id); err != nil {
		return nil, err
	}
	return &rest.Config{Host: "https://fake.example"}, nil
}

// Discovery implements kube.Clients.
func (c *Clients) Discovery(_ context.Context, _ session.Session, id string) (*kube.Discovery, error) {
	if err := c.check(id); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.discCalls++
	c.mu.Unlock()
	return kube.BuildDiscovery(c.Disc)
}

// Invalidate implements kube.Invalidator.
func (c *Clients) Invalidate(session.Session, string) {
	c.mu.Lock()
	c.invalidated++
	c.mu.Unlock()
}

// DiscoveryCalls returns how often Discovery was called.
func (c *Clients) DiscoveryCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.discCalls
}

// Invalidations returns how often Invalidate was called.
func (c *Clients) Invalidations() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.invalidated
}

// Object builds an unstructured object.
func Object(apiVersion, kind, ns, name string, fields map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": map[string]any{"name": name}}}
	if ns != "" {
		u.SetNamespace(ns)
	}
	u.SetUID(uidFor(kind, ns, name))
	for k, v := range fields {
		u.Object[k] = v
	}
	return u
}

func uidFor(kind, ns, name string) types.UID { return types.UID(kind + "/" + ns + "/" + name) }

// Pod builds a Pod.
func Pod(ns, name string, fields map[string]any) *unstructured.Unstructured {
	return Object("v1", "Pod", ns, name, fields)
}

// Secret builds a Secret with the given base64 data values.
func Secret(ns, name string, data map[string]any) *unstructured.Unstructured {
	return Object("v1", "Secret", ns, name, map[string]any{"data": data, "type": string(corev1.SecretTypeOpaque)})
}

// ConfigMap builds a ConfigMap.
func ConfigMap(ns, name string, data map[string]any) *unstructured.Unstructured {
	return Object("v1", "ConfigMap", ns, name, map[string]any{"data": data})
}

// Namespace builds a cluster-scoped Namespace.
func Namespace(name string) *unstructured.Unstructured {
	return Object("v1", "Namespace", "", name, nil)
}
