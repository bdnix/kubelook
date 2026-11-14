// Package watch keeps one upstream watch per user, cluster, kind and namespace and fans it out to
// that user's views. Objects are redacted before they are cached, so Secret values never sit in memory.
package watch

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// Defaults.
const (
	DefaultMaxPerUser  = 50
	DefaultIdleSeconds = 30
)

// Key identifies one upstream watch. Nothing is shared between sessions.
type Key struct {
	SessionHash, UserID, Cluster string
	GVR                          schema.GroupVersionResource
	Namespace                    string // "" means all namespaces
	FieldSelector                string // "" means every object; the details drawer watches one object by name
}

// Subscriber receives what an upstream sees. Its methods run on the upstream's goroutine and
// must not block.
type Subscriber interface {
	Snapshot(items []*unstructured.Unstructured, rv string)
	Event(typ string, obj *unstructured.Unstructured)
	Paused()
	Failed(code, message string)
	Active() bool // false while the tab is hidden
}

// Stats are the numbers the metrics need.
type Stats struct {
	Watches, Subscribers, CachedObjects int
}

// Deps are the dependencies of Hub.
type Deps struct {
	Clients     kube.Clients
	Clock       clock.Clock
	Log         *slog.Logger
	IdleSeconds func(ctx context.Context, userID string) int // the user's WatchIdleSeconds; nil or ≤ 0 → 30
	MaxPerUser  int                                          // ≤ 0 → 50
	Closers     *session.Registry                            // nil → upstreams are not tied to the session
}

// Hub owns the upstream watches of one replica.
type Hub struct {
	d      Deps
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	ups      map[Key]*upstream
	order    []*upstream // creation order, for evicting the oldest
	sessions map[string]bool
	closed   bool
}

// NewHub returns a hub. Close it to stop every upstream.
func NewHub(d Deps) *Hub {
	if d.MaxPerUser <= 0 {
		d.MaxPerUser = DefaultMaxPerUser
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{d: d, ctx: ctx, cancel: cancel, ups: map[Key]*upstream{}, sessions: map[string]bool{}}
}

var errTooManyWatches = &apperr.APIError{Status: http.StatusTooManyRequests, Code: "too_many_watches",
	Message: "You have too many live views open. Close some tabs or lists and try again."}

// Subscribe attaches s to the upstream for k, creating it when this is the first subscriber. The
// subscriber receives a snapshot first, then events. unsubscribe detaches it.
func (h *Hub) Subscribe(_ context.Context, sess session.Session, k Key, s Subscriber) (unsubscribe func(), err error) {
	for {
		sub := &subscription{s: s}
		u, added, err := h.upstreamFor(sess, k, sub)
		if err != nil {
			return nil, err
		}
		if added || u.send(command{kind: cmdAdd, sub: sub}) {
			return func() { u.send(command{kind: cmdRemove, sub: sub}) }, nil
		}
		// the upstream stopped between lookup and add: look it up again
	}
}

// ActivityChanged tells the hub that a subscriber's Active() changed, so idle timers are re-evaluated.
func (h *Hub) ActivityChanged(k Key) {
	h.mu.Lock()
	u := h.ups[k]
	h.mu.Unlock()
	if u != nil {
		u.send(command{kind: cmdRecheck})
	}
}

// upstreamFor finds or creates the upstream. A new upstream starts with sub already queued, so that
// its first list cannot fail before the subscriber is there to hear about it.
func (h *Hub) upstreamFor(sess session.Session, k Key, sub *subscription) (u *upstream, added bool, err error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, false, fmt.Errorf("the server is shutting down: %w", apperr.ErrBusy)
	}
	if u, ok := h.ups[k]; ok {
		h.mu.Unlock()
		return u, false, nil
	}
	if err := h.makeRoom(k.UserID); err != nil {
		h.mu.Unlock()
		return nil, false, err
	}
	u = newUpstream(h, sess, k)
	u.cmds <- command{kind: cmdAdd, sub: sub}
	h.ups[k] = u
	h.order = append(h.order, u)
	first := !h.sessions[k.SessionHash]
	h.sessions[k.SessionHash] = true
	h.wg.Add(1)
	go u.run(h.ctx)
	h.mu.Unlock()
	if first && h.d.Closers != nil {
		// Outside the lock: for an ended session the closer runs at once and takes it.
		h.d.Closers.Register(k.SessionHash, "watch-hub", func() error { h.stopSession(k.SessionHash); return nil })
	}
	return u, true, nil
}

// makeRoom frees a slot for a new watch of the user: the oldest watch that only hidden tabs look at
// is paused. With none, the user is at the limit. Callers hold h.mu.
func (h *Hub) makeRoom(userID string) error {
	count := 0
	var victim *upstream
	for _, u := range h.order {
		if u.key.UserID != userID {
			continue
		}
		count++
		if victim == nil && u.onlyHidden() {
			victim = u
		}
	}
	if count < h.d.MaxPerUser {
		return nil
	}
	if victim == nil {
		return errTooManyWatches
	}
	victim.send(command{kind: cmdPause})
	h.removeLocked(victim) // takes effect now so the new watch fits
	return nil
}

// removeLocked forgets the upstream. Callers hold h.mu.
func (h *Hub) removeLocked(u *upstream) {
	if h.ups[u.key] == u {
		delete(h.ups, u.key)
	}
	for i, o := range h.order {
		if o == u {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
}

func (h *Hub) remove(u *upstream) {
	h.mu.Lock()
	h.removeLocked(u)
	h.mu.Unlock()
}

func (h *Hub) stopSession(hash string) {
	h.mu.Lock()
	delete(h.sessions, hash)
	var stop []*upstream
	for _, u := range h.order {
		if u.key.SessionHash == hash {
			stop = append(stop, u)
		}
	}
	for _, u := range stop {
		h.removeLocked(u)
	}
	h.mu.Unlock()
	for _, u := range stop {
		u.send(command{kind: cmdStop})
	}
}

// Stats counts the hub's current watches, subscribers and cached objects.
func (h *Hub) Stats() Stats {
	h.mu.Lock()
	ups := make([]*upstream, len(h.order))
	copy(ups, h.order)
	h.mu.Unlock()
	var st Stats
	for _, u := range ups {
		st.Watches++
		st.Subscribers += int(u.subs.Load())
		st.CachedObjects += int(u.cached.Load())
	}
	return st
}

// Close stops every upstream and waits for them.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.cancel()
	h.wg.Wait()
}
