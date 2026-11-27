package resourcemap

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/watch"
	"github.com/bdnix/kubelook/internal/ws"
)

// debounce is how long changes are collected before the graph is sent again.
const debounce = time.Second

// Source is the watch hub.
type Source interface {
	Subscribe(ctx context.Context, sess session.Session, k watch.Key, s watch.Subscriber) (unsubscribe func(), err error)
}

// Access answers whether the user may do something (`kube.Access`).
type Access interface {
	Can(ctx context.Context, sess session.Session, cluster string, c kube.Check) (allowed bool, reason string, err error)
}

// Conn is the part of the WebSocket connection the channel uses (`*ws.Conn`).
type Conn interface {
	Send(ch int, typ string, d any) error
	// SendDroppable queues a frame a slow browser may skip: each graph replaces the whole map, so an older one is worth nothing.
	SendDroppable(ch int, typ string, d any)
	Session() session.Session
	Hidden() bool
	CloseChannel(ch int, reason string)
	OnVisibility(fn func(hidden bool)) (cancel func())
}

// Deps are the dependencies of the map channel.
type Deps struct {
	Source  Source
	Clients kube.Clients
	Access  Access
	Clock   clock.Clock
}

// FocusRef is the object whose connected component a map shows.
type FocusRef struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type openRequest struct {
	Cluster    string    `json:"cluster"`
	Namespaces []string  `json:"namespaces"`
	Focus      *FocusRef `json:"focus"`
}

// Factory returns the `map.open` factory for the WebSocket mux.
func Factory(d Deps) ws.Factory {
	return func(ctx context.Context, c *ws.Conn, ch int, raw json.RawMessage) (ws.Channel, error) {
		return Open(ctx, d, c, ch, raw)
	}
}

type channel struct {
	d       Deps
	conn    Conn
	ch      int
	focus   string
	selects map[string]bool

	mu       sync.Mutex
	caches   map[watch.Key]map[string]*unstructured.Unstructured
	pending  int
	sent     bool
	timer    clock.Timer
	closed   bool
	omitted  []string
	leftOut  []string
	unsubs   []func()
	cancelVs func()
}

// Open starts a map: it subscribes to the hub for each kind the user can list, as the user.
func Open(ctx context.Context, d Deps, conn Conn, ch int, raw json.RawMessage) (ws.Channel, error) {
	var req openRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Cluster == "" {
		return nil, fmt.Errorf("map.open needs {cluster, namespaces}: %w", apperr.ErrInvalid)
	}
	sess := conn.Session()
	disc, err := d.Clients.Discovery(ctx, sess, req.Cluster)
	if err != nil {
		return nil, err
	}
	served := map[Kind]schema.GroupVersionResource{}
	var kinds []Kind
	for _, k := range Kinds {
		if gvr, err := disc.Resolve(k.Group, k.Resource); err == nil {
			served[k] = gvr
			kinds = append(kinds, k)
		}
	}
	c := &channel{d: d, conn: conn, ch: ch, caches: map[watch.Key]map[string]*unstructured.Unstructured{}, selects: map[string]bool{}}
	for _, ns := range req.Namespaces {
		c.selects[ns] = true
	}
	if req.Focus != nil {
		c.focus = nodeID(req.Focus.Kind, req.Focus.Namespace, req.Focus.Name)
	}
	listable, all, err := c.permissions(ctx, sess, req, kinds)
	if err != nil {
		return nil, err
	}
	for _, k := range kinds {
		if !slices.Contains(listable, k) {
			c.omitted = append(c.omitted, k.Kind)
		}
	}
	var plan []Watch
	if len(req.Namespaces) == 0 {
		for _, k := range listable {
			if all[k] {
				plan = append(plan, Watch{Kind: k})
			}
		}
	} else {
		plan, c.leftOut = PlanWatches(listable, req.Namespaces, func(k Kind) bool { return all[k] })
	}
	c.pending = len(plan)
	for _, w := range plan {
		key := watch.Key{UserID: sess.UserID, Cluster: req.Cluster, GVR: served[w.Kind], Namespace: w.Namespace}
		c.caches[key] = map[string]*unstructured.Unstructured{}
		unsub, err := d.Source.Subscribe(ctx, sess, key, &kindSub{c: c, key: key, kind: w.Kind})
		if err != nil {
			c.Close()
			return nil, err
		}
		c.unsubs = append(c.unsubs, unsub)
	}
	c.cancelVs = conn.OnVisibility(func(bool) {})
	if len(plan) == 0 {
		c.flush()
	}
	return c, nil
}

// permissions says which kinds the user may list at all (cluster-wide, or in the first selected namespace) and which
// cluster-wide.
func (c *channel) permissions(ctx context.Context, sess session.Session, req openRequest, kinds []Kind) (listable []Kind, all map[Kind]bool, err error) {
	all = map[Kind]bool{}
	for _, k := range kinds {
		ok, _, err := c.d.Access.Can(ctx, sess, req.Cluster, kube.Check{Verb: "list", Group: k.Group, Resource: k.Resource})
		if err != nil {
			return nil, nil, err
		}
		all[k] = ok
		if !ok && len(req.Namespaces) > 0 {
			ok, _, err = c.d.Access.Can(ctx, sess, req.Cluster, kube.Check{Verb: "list", Group: k.Group, Resource: k.Resource, Namespace: req.Namespaces[0]})
			if err != nil {
				return nil, nil, err
			}
		}
		if ok {
			listable = append(listable, k)
		}
	}
	return listable, all, nil
}

func objectKey(o *unstructured.Unstructured) string { return o.GetNamespace() + "/" + o.GetName() }

// kindSub is the subscriber of one watch.
type kindSub struct {
	c    *channel
	key  watch.Key
	kind Kind
}

func (s *kindSub) stamp(o *unstructured.Unstructured) *unstructured.Unstructured {
	if o.GetKind() != "" {
		return o
	}
	c := o.DeepCopy()
	c.SetKind(s.kind.Kind)
	c.SetAPIVersion(schema.GroupVersion{Group: s.key.GVR.Group, Version: s.key.GVR.Version}.String())
	return c
}

func (s *kindSub) Snapshot(items []*unstructured.Unstructured, _ string) {
	c := s.c
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	cache := map[string]*unstructured.Unstructured{}
	for _, o := range items {
		cache[objectKey(o)] = s.stamp(o)
	}
	first := c.pending > 0
	c.caches[s.key] = cache
	if first {
		c.pending--
	}
	ready := c.pending == 0
	sent := c.sent
	c.mu.Unlock()
	switch {
	case ready && !sent:
		c.flush()
	case ready:
		c.schedule()
	}
}

func (s *kindSub) Event(typ string, o *unstructured.Unstructured) {
	c := s.c
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if typ == "DELETED" {
		delete(c.caches[s.key], objectKey(o))
	} else {
		c.caches[s.key][objectKey(o)] = s.stamp(o)
	}
	c.mu.Unlock()
	c.schedule()
}

func (s *kindSub) Paused() {}

func (s *kindSub) Failed(code, message string) {
	_ = s.c.conn.Send(s.c.ch, "error", map[string]string{"code": code, "message": message})
	s.c.conn.CloseChannel(s.c.ch, "error")
}

func (s *kindSub) Active() bool { return !s.c.conn.Hidden() }

// schedule sends the graph after the debounce unless a send is already due. Nothing is sent before every list has arrived.
func (c *channel) schedule() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.timer != nil || c.pending > 0 {
		return
	}
	c.timer = c.d.Clock.AfterFunc(debounce, c.flush)
}

// flush builds the graph from what the caches hold and sends it.
func (c *channel) flush() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.timer = nil
	var objects []*unstructured.Unstructured
	for _, cache := range c.caches {
		for _, o := range cache {
			if len(c.selects) > 0 && o.GetNamespace() != "" && !c.selects[o.GetNamespace()] {
				continue
			}
			objects = append(objects, o)
		}
	}
	g := Build(objects)
	if c.focus != "" {
		g = Focus(g, c.focus)
	}
	g.OmittedKinds = append([]string{}, c.omitted...)
	g.LeftOutNamespaces = append([]string(nil), c.leftOut...)
	c.sent = true
	c.mu.Unlock()
	c.conn.SendDroppable(c.ch, "graph", g)
}

// Handle refuses every op: a map channel is driven by the server.
func (c *channel) Handle(_ context.Context, op string, _ json.RawMessage) error {
	return fmt.Errorf("a map channel takes no op %q: %w", op, apperr.ErrInvalid)
}

// Close detaches from the hub.
func (c *channel) Close() {
	c.mu.Lock()
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
	}
	unsubs := c.unsubs
	c.unsubs = nil
	c.mu.Unlock()
	for _, u := range unsubs {
		u()
	}
	if c.cancelVs != nil {
		c.cancelVs()
	}
}
