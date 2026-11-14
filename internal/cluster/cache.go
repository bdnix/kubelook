package cluster

import (
	"sync"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/vault"
)

// discoveryTTL is how long a discovery result is reused (the spec's 60 s).
const discoveryTTL = 60 * time.Second

type cacheEntry struct {
	clusters   []vault.Cluster
	at         time.Time
	unregister func()
}

// DiscoveryCache keeps, per session, the clusters Vault discovery last found. A result is fresh
// for 60 s and is kept after that as the "last known" answer for when Vault cannot be reached.
// An entry is dropped when the session ends.
type DiscoveryCache struct {
	clock    clock.Clock
	sessions *session.Registry

	mu sync.Mutex
	m  map[string]cacheEntry
}

// NewDiscoveryCache returns an empty cache. sessions (may be nil) is told to drop entries of ended sessions.
func NewDiscoveryCache(c clock.Clock, sessions *session.Registry) *DiscoveryCache {
	return &DiscoveryCache{clock: c, sessions: sessions, m: map[string]cacheEntry{}}
}

// Fresh returns the cached clusters when they are younger than 60 s.
func (c *DiscoveryCache) Fresh(hash string) ([]vault.Cluster, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[hash]
	if !ok || c.clock.Now().Sub(e.at) >= discoveryTTL {
		return nil, false
	}
	return e.clusters, true
}

// Last returns the clusters of the last successful discovery, however old.
func (c *DiscoveryCache) Last(hash string) ([]vault.Cluster, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[hash]
	return e.clusters, ok
}

// Put stores a discovery result.
func (c *DiscoveryCache) Put(hash string, clusters []vault.Cluster) {
	c.mu.Lock()
	e, had := c.m[hash]
	e.clusters, e.at = clusters, c.clock.Now()
	c.m[hash] = e
	c.mu.Unlock()
	if had || c.sessions == nil {
		return
	}
	// Registered outside the lock: for an already ended session the closer runs at once and takes the lock.
	unregister := c.sessions.Register(hash, "discovery-cache", func() error { c.drop(hash); return nil })
	c.mu.Lock()
	if e, ok := c.m[hash]; ok {
		e.unregister = unregister
		c.m[hash] = e
	}
	c.mu.Unlock()
}

// Invalidate forgets the session's entry, including the last known clusters.
func (c *DiscoveryCache) Invalidate(hash string) {
	c.mu.Lock()
	e, ok := c.m[hash]
	delete(c.m, hash)
	c.mu.Unlock()
	if ok && e.unregister != nil {
		e.unregister()
	}
}

func (c *DiscoveryCache) drop(hash string) {
	c.mu.Lock()
	delete(c.m, hash)
	c.mu.Unlock()
}

// Len returns the number of sessions with an entry.
func (c *DiscoveryCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}
