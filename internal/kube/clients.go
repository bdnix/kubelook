// Package kube talks to Kubernetes clusters with the credentials of one signed-in user: client
// factories, discovery, generic reads and permission checks. Nothing here is shared between users.
package kube

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
)

// discoveryTTL is how long a cluster's discovery result is reused.
const discoveryTTL = 60 * time.Second

// Clients gives access to one cluster with one session's credentials.
type Clients interface {
	Dynamic(ctx context.Context, sess session.Session, clusterID string) (dynamic.Interface, error)
	Typed(ctx context.Context, sess session.Session, clusterID string) (kubernetes.Interface, error)
	Discovery(ctx context.Context, sess session.Session, clusterID string) (*Discovery, error)
	REST(ctx context.Context, sess session.Session, clusterID string) (*rest.Config, error)
}

// Invalidator is implemented by Clients that cache discovery.
type Invalidator interface {
	// Invalidate forces the next Discovery call to ask the cluster again (after a CRD changed).
	Invalidate(sess session.Session, clusterID string)
}

// ConfigSource provides REST configurations; the cluster opener implements it.
type ConfigSource interface {
	RESTConfig(ctx context.Context, sess session.Session, clusterID string) (*rest.Config, error)
}

// Built is what a REST configuration turns into.
type Built struct {
	Dynamic   dynamic.Interface
	Typed     kubernetes.Interface
	Discovery discovery.DiscoveryInterface
}

// Deps are the dependencies of the client factory.
type Deps struct {
	Config  ConfigSource
	Clock   clock.Clock
	Closers *session.Registry
	Build   func(*rest.Config) (Built, error) // nil → real client-go clients
}

type cached struct {
	cfg    *rest.Config
	built  Built
	disco  *Discovery
	discAt time.Time
}

type factory struct {
	d  Deps
	mu sync.Mutex
	m  map[string]*cached
}

// NewClients returns the client factory.
func NewClients(d Deps) Clients {
	if d.Build == nil {
		d.Build = buildReal
	}
	return &factory{d: d, m: map[string]*cached{}}
}

func buildReal(cfg *rest.Config) (Built, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return Built{}, fmt.Errorf("building dynamic client: %w", err)
	}
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return Built{}, fmt.Errorf("building typed client: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return Built{}, fmt.Errorf("building discovery client: %w", err)
	}
	return Built{Dynamic: dyn, Typed: typed, Discovery: disc}, nil
}

func key(sess session.Session, id string) string { return sess.Hash + "\x00" + id }

func (f *factory) get(ctx context.Context, sess session.Session, id string) (*cached, error) {
	k := key(sess, id)
	f.mu.Lock()
	c, ok := f.m[k]
	f.mu.Unlock()
	if ok {
		return c, nil
	}
	cfg, err := f.d.Config.RESTConfig(ctx, sess, id)
	if err != nil {
		return nil, err
	}
	built, err := f.d.Build(cfg)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	if c, ok := f.m[k]; ok {
		f.mu.Unlock()
		return c, nil // another caller built it first
	}
	c = &cached{cfg: cfg, built: built}
	f.m[k] = c
	f.mu.Unlock()
	if f.d.Closers != nil {
		// Registered outside the lock: for an ended session the closer runs at once and takes it.
		f.d.Closers.Register(sess.Hash, id, func() error { f.drop(k); return nil })
	}
	return c, nil
}

func (f *factory) drop(k string) {
	f.mu.Lock()
	delete(f.m, k)
	f.mu.Unlock()
}

// Len returns the number of cached clients (for tests and metrics).
func (f *factory) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.m)
}

func (f *factory) Dynamic(ctx context.Context, sess session.Session, id string) (dynamic.Interface, error) {
	c, err := f.get(ctx, sess, id)
	if err != nil {
		return nil, err
	}
	return c.built.Dynamic, nil
}

func (f *factory) Typed(ctx context.Context, sess session.Session, id string) (kubernetes.Interface, error) {
	c, err := f.get(ctx, sess, id)
	if err != nil {
		return nil, err
	}
	return c.built.Typed, nil
}

func (f *factory) REST(ctx context.Context, sess session.Session, id string) (*rest.Config, error) {
	c, err := f.get(ctx, sess, id)
	if err != nil {
		return nil, err
	}
	return rest.CopyConfig(c.cfg), nil
}

func (f *factory) Discovery(ctx context.Context, sess session.Session, id string) (*Discovery, error) {
	c, err := f.get(ctx, sess, id)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	if c.disco != nil && f.d.Clock.Now().Sub(c.discAt) < discoveryTTL {
		d := c.disco
		f.mu.Unlock()
		return d, nil
	}
	f.mu.Unlock()
	d, err := BuildDiscovery(c.built.Discovery)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	c.disco, c.discAt = d, f.d.Clock.Now()
	f.mu.Unlock()
	return d, nil
}

// Invalidate implements Invalidator.
func (f *factory) Invalidate(sess session.Session, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.m[key(sess, id)]; ok {
		c.disco = nil
	}
}
