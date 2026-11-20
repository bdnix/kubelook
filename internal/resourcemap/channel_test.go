package resourcemap_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/resourcemap"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
	"github.com/bdnix/kubelook/internal/watch"
)

var (
	t0   = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	sess = session.Session{Hash: "h", UserID: "u1"}
)

// source is a hub: it remembers every subscription and lets the test play the upstream.
type source struct {
	mu   sync.Mutex
	subs map[watch.Key]watch.Subscriber
	off  int
	err  error
}

func (s *source) Subscribe(_ context.Context, _ session.Session, k watch.Key, sub watch.Subscriber) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.subs == nil {
		s.subs = map[watch.Key]watch.Subscriber{}
	}
	s.subs[k] = sub
	return func() { s.mu.Lock(); s.off++; s.mu.Unlock() }, nil
}

func (s *source) keys() []watch.Key {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []watch.Key
	for k := range s.subs {
		out = append(out, k)
	}
	return out
}

func (s *source) forResource(resource, namespace string) watch.Subscriber {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.subs {
		if k.GVR.Resource == resource && k.Namespace == namespace {
			return v
		}
	}
	return nil
}

type frame struct {
	ch  int
	typ string
	d   any
}

type conn struct {
	mu     sync.Mutex
	frames []frame
	closed []int
	hidden bool
	vis    []func(bool)
}

func (c *conn) Send(ch int, typ string, d any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frames = append(c.frames, frame{ch, typ, d})
	return nil
}
func (c *conn) Session() session.Session { return sess }
func (c *conn) Hidden() bool             { return c.hidden }
func (c *conn) CloseChannel(ch int, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = append(c.closed, ch)
}
func (c *conn) OnVisibility(fn func(bool)) func() { c.vis = append(c.vis, fn); return func() {} }

func (c *conn) graphs() []resourcemap.Graph {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []resourcemap.Graph
	for _, f := range c.frames {
		if f.typ == "graph" {
			b, _ := json.Marshal(f.d)
			var g resourcemap.Graph
			_ = json.Unmarshal(b, &g)
			out = append(out, g)
		}
	}
	return out
}

type access struct{ deny func(resource, namespace string) bool }

func (a access) Can(_ context.Context, _ session.Session, _ string, c kube.Check) (bool, string, error) {
	if a.deny != nil && a.deny(c.Resource, c.Namespace) {
		return false, "no", nil
	}
	return true, "", nil
}

type rig struct {
	src  *source
	conn *conn
	clk  *clock.Fake
	deps resourcemap.Deps
}

func newRig(t *testing.T, deny func(resource, namespace string) bool) *rig {
	t.Helper()
	fk := fakekube.New()
	fk.AddResources(
		&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", Namespaced: true, Verbs: metav1.Verbs{"list", "watch"}}}},
		&metav1.APIResourceList{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{
			{Name: "replicasets", Kind: "ReplicaSet", Namespaced: true, Verbs: metav1.Verbs{"list", "watch"}},
			{Name: "statefulsets", Kind: "StatefulSet", Namespaced: true, Verbs: metav1.Verbs{"list", "watch"}},
			{Name: "daemonsets", Kind: "DaemonSet", Namespaced: true, Verbs: metav1.Verbs{"list", "watch"}},
		}},
		&metav1.APIResourceList{GroupVersion: "networking.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "ingresses", Kind: "Ingress", Namespaced: true, Verbs: metav1.Verbs{"list", "watch"}}}},
	)
	r := &rig{src: &source{}, conn: &conn{}, clk: clock.NewFake(t0)}
	r.deps = resourcemap.Deps{Source: r.src, Clients: fk, Access: access{deny}, Clock: r.clk}
	return r
}

func (r *rig) open(t *testing.T, req map[string]any) func() {
	t.Helper()
	raw, _ := json.Marshal(req)
	ch, err := resourcemap.Open(t.Context(), r.deps, r.conn, 7, raw)
	require.NoError(t, err)
	return ch.Close
}

func (r *rig) snapshot(resource, namespace string, items ...*unstructured.Unstructured) {
	sub := r.src.forResource(resource, namespace)
	if sub == nil {
		panic("no subscription for " + resource)
	}
	sub.Snapshot(items, "1")
}

// every watch answers with an empty list, so the graph is sent
func (r *rig) settle(namespace string, except ...string) {
	skip := map[string]bool{}
	for _, e := range except {
		skip[e] = true
	}
	for _, k := range r.src.keys() {
		if !skip[k.GVR.Resource] && k.Namespace == namespace {
			r.src.mu.Lock()
			sub := r.src.subs[k]
			r.src.mu.Unlock()
			sub.Snapshot(nil, "1")
		}
	}
}

func TestOpen_WatchesEveryServedKindOfTheNamespaceAsTheUser(t *testing.T) {
	r := newRig(t, nil)
	r.open(t, map[string]any{"cluster": "c", "namespaces": []string{"shop"}})
	var got []string
	for _, k := range r.src.keys() {
		assert.Equal(t, "u1", k.UserID)
		assert.Equal(t, "c", k.Cluster)
		assert.Equal(t, "shop", k.Namespace)
		got = append(got, k.GVR.Resource)
	}
	assert.ElementsMatch(t, []string{"pods", "configmaps", "secrets", "persistentvolumeclaims", "services", "replicasets", "deployments", "statefulsets", "daemonsets", "ingresses"}, got, "the routes are not served here")
}

func TestOpen_SendsTheGraphWhenEveryListHasArrived_AndAfterEachDebouncedChange(t *testing.T) {
	r := newRig(t, nil)
	r.open(t, map[string]any{"cluster": "c", "namespaces": []string{"shop"}})
	r.settle("shop", "pods")
	assert.Empty(t, r.conn.graphs(), "one list is still missing")
	r.snapshot("pods", "shop", o("v1", "Pod", "shop", "web-1", map[string]any{"status": map[string]any{"phase": "Running"}}))
	graphs := r.conn.graphs()
	require.Len(t, graphs, 1, "sent when the last list arrived")
	assert.Equal(t, []string{"Pod/shop/web-1"}, ids(graphs[0]))

	sub := r.src.forResource("pods", "shop")
	sub.Event("ADDED", o("v1", "Pod", "shop", "web-2", nil))
	sub.Event("ADDED", o("v1", "Pod", "shop", "web-3", nil))
	sub.Event("DELETED", o("v1", "Pod", "shop", "web-1", nil))
	assert.Len(t, r.conn.graphs(), 1, "changes wait for the debounce")
	r.clk.Advance(time.Second)
	graphs = r.conn.graphs()
	require.Len(t, graphs, 2, "one frame for the three changes")
	assert.Equal(t, []string{"Pod/shop/web-2", "Pod/shop/web-3"}, ids(graphs[1]))
}

func TestOpen_AKindTheUserCannotListIsLeftOutAndNamed(t *testing.T) {
	r := newRig(t, func(resource, _ string) bool { return resource == "secrets" })
	r.open(t, map[string]any{"cluster": "c", "namespaces": []string{"shop"}})
	assert.Nil(t, r.src.forResource("secrets", "shop"), "no watch for what the user cannot list")
	r.settle("shop")
	g := r.conn.graphs()[0]
	assert.Equal(t, []string{"Secret"}, g.OmittedKinds)
}

func TestOpen_FilterObjectsToTheSelectedNamespaces_WhenWatchingAllNamespaces(t *testing.T) {
	r := newRig(t, nil)
	r.open(t, map[string]any{"cluster": "c", "namespaces": []string{"a", "b", "c"}})
	for _, k := range r.src.keys() {
		assert.Empty(t, k.Namespace, "three namespaces take the all-namespaces watches")
	}
	r.settle("", "pods")
	r.snapshot("pods", "", o("v1", "Pod", "a", "in", nil), o("v1", "Pod", "z", "out", nil))
	assert.Equal(t, []string{"Pod/a/in"}, ids(r.conn.graphs()[0]))
}

func TestOpen_NamespacesThatDoNotFitAreReported(t *testing.T) {
	r := newRig(t, func(resource, namespace string) bool { return resource == "configmaps" && namespace == "" })
	var many []string
	for i := 0; i < 20; i++ {
		many = append(many, string(rune('a'+i))+"-ns")
	}
	r.open(t, map[string]any{"cluster": "c", "namespaces": many})
	r.settle("", "pods")
	r.settle("a-ns")
	r.snapshot("pods", "", o("v1", "Pod", "a-ns", "p", nil))
	g := r.conn.graphs()[0]
	assert.NotEmpty(t, g.LeftOutNamespaces)
	assert.LessOrEqual(t, len(r.src.keys()), resourcemap.MaxWatches)
}

func TestOpen_FocusKeepsTheConnectedComponent(t *testing.T) {
	r := newRig(t, nil)
	r.open(t, map[string]any{"cluster": "c", "namespaces": []string{"shop"}, "focus": map[string]any{"group": "apps", "kind": "Deployment", "namespace": "shop", "name": "web"}})
	r.settle("shop", "pods", "deployments", "replicasets")
	r.snapshot("replicasets", "shop", owned(o("apps/v1", "ReplicaSet", "shop", "web-abc", nil), "Deployment", "web"))
	r.snapshot("deployments", "shop", o("apps/v1", "Deployment", "shop", "web", nil))
	r.snapshot("pods", "shop", owned(o("v1", "Pod", "shop", "web-abc-1", nil), "ReplicaSet", "web-abc"), o("v1", "Pod", "shop", "lonely", nil))
	g := r.conn.graphs()[0]
	assert.Equal(t, []string{"Deployment/shop/web", "Pod/shop/web-abc-1"}, ids(g))
}

func TestOpen_ClosingDetachesFromTheHub(t *testing.T) {
	r := newRig(t, nil)
	closeCh := r.open(t, map[string]any{"cluster": "c", "namespaces": []string{"shop"}})
	n := len(r.src.keys())
	closeCh()
	assert.Equal(t, n, r.src.off)
	r.settle("shop")
	r.clk.Advance(2 * time.Second)
	assert.Empty(t, r.conn.graphs(), "a closed channel sends nothing")
}

func TestOpen_Errors(t *testing.T) {
	r := newRig(t, nil)
	_, err := resourcemap.Open(t.Context(), r.deps, r.conn, 1, json.RawMessage(`nonsense`))
	assert.Error(t, err)
	_, err = resourcemap.Open(t.Context(), r.deps, r.conn, 1, json.RawMessage(`{"namespaces":["a"]}`))
	assert.Error(t, err, "no cluster")
	r.src.err = assert.AnError
	_, err = resourcemap.Open(t.Context(), r.deps, r.conn, 1, json.RawMessage(`{"cluster":"c","namespaces":["a"]}`))
	assert.Error(t, err, "a refused watch fails the open")
}

func TestOpen_AFailedWatchClosesTheChannelWithAnError(t *testing.T) {
	r := newRig(t, nil)
	r.open(t, map[string]any{"cluster": "c", "namespaces": []string{"shop"}})
	r.src.forResource("pods", "shop").Failed("forbidden", "no access")
	assert.Equal(t, []int{7}, r.conn.closed)
	assert.Equal(t, "error", r.conn.frames[len(r.conn.frames)-1].typ)
}

func TestChannel_TakesNoOpsFromTheBrowserAndFollowsTheTabsVisibility(t *testing.T) {
	r := newRig(t, nil)
	raw, _ := json.Marshal(map[string]any{"cluster": "c", "namespaces": []string{"shop"}})
	ch, err := resourcemap.Open(t.Context(), r.deps, r.conn, 7, raw)
	require.NoError(t, err)
	assert.Error(t, ch.Handle(t.Context(), "anything", nil))
	sub := r.src.forResource("pods", "shop")
	assert.True(t, sub.Active())
	r.conn.hidden = true
	assert.False(t, sub.Active())
	sub.Paused()
	assert.Empty(t, r.conn.graphs())
}
