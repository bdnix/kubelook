// Package watch keeps one upstream watch per user, cluster, kind and namespace and fans it out to
// that user's views. Objects are redacted before they are cached, so Secret values never sit in memory.
package watch

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

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

// Key identifies one upstream watch: one per user, cluster, kind, namespace and selector. Sessions
// of the same user share it (see ukey for kubeconfig clusters).
type Key struct {
	UserID, Cluster string
	GVR             schema.GroupVersionResource
	Namespace       string // "" means all namespaces
	FieldSelector   string // "" means every object; the details drawer watches one object by name
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

// ukey is what the hub indexes upstreams by: the Key, plus for a kubeconfig cluster a fingerprint of
// the server and the credential. Two sessions of one user may hold different kubeconfig files under
// one label, so they share an upstream only when both resolve the cluster ID to the same server and
// the same credential. Vault clusters are the user's own and are shared without the check.
type ukey struct {
	Key
	credential string
}

// kubeconfigPrefix starts the ID of every cluster that comes from an uploaded kubeconfig.
const kubeconfigPrefix = "k:"

// Hub owns the upstream watches of one replica.
type Hub struct {
	d      Deps
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	ups      map[ukey]*upstream
	order    []*upstream       // creation order, for evicting the oldest
	fpKey    []byte            // random key of the credential fingerprints; never leaves memory
	sessions map[string]string // session hash → user ID, for the sessions whose end the hub listens to
	closed   bool
}

// NewHub returns a hub. Close it to stop every upstream.
func NewHub(d Deps) *Hub {
	if d.MaxPerUser <= 0 {
		d.MaxPerUser = DefaultMaxPerUser
	}
	ctx, cancel := context.WithCancel(context.Background())
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &Hub{d: d, fpKey: key, ctx: ctx, cancel: cancel, ups: map[ukey]*upstream{}, sessions: map[string]string{}}
}

var errTooManyWatches = &apperr.APIError{Status: http.StatusTooManyRequests, Code: "too_many_watches",
	Message: "You have too many live views open. Close some tabs or lists and try again."}

// Subscribe attaches s to the upstream for k, creating it when this is the first subscriber. The
// subscriber receives a snapshot first, then events. unsubscribe detaches it.
func (h *Hub) Subscribe(ctx context.Context, sess session.Session, k Key, s Subscriber) (unsubscribe func(), err error) {
	uk, err := h.ukeyFor(ctx, sess, k)
	if err != nil {
		return nil, err
	}
	for {
		sub := &subscription{s: s, sess: sess}
		u, added, err := h.upstreamFor(sess, uk, sub)
		if err != nil {
			return nil, err
		}
		h.listenForEnd(sess)
		if added || u.send(command{kind: cmdAdd, sub: sub}) {
			return func() { u.send(command{kind: cmdRemove, sub: sub}) }, nil
		}
		// the upstream stopped between lookup and add: look it up again
	}
}

// ukeyFor adds the credential fingerprint for a kubeconfig cluster.
func (h *Hub) ukeyFor(ctx context.Context, sess session.Session, k Key) (ukey, error) {
	if !strings.HasPrefix(k.Cluster, kubeconfigPrefix) {
		return ukey{Key: k}, nil
	}
	cfg, err := h.d.Clients.REST(ctx, sess, k.Cluster)
	if err != nil {
		return ukey{}, kube.MapError(err)
	}
	return ukey{Key: k, credential: h.fingerprint(cfg)}, nil
}

// fingerprint is an HMAC-SHA-256, keyed with a random per-hub key, over the server and every credential field of a REST config. It stays in
// memory and is only compared, never shown or stored.
func (h *Hub) fingerprint(cfg *rest.Config) string {
	sum := hmac.New(sha256.New, h.fpKey)
	for _, part := range [][]byte{
		[]byte(cfg.Host), []byte(cfg.BearerToken), []byte(cfg.Username), []byte(cfg.Password),
		cfg.CertData, cfg.KeyData, cfg.CAData,
	} {
		_, _ = sum.Write(part)
		_, _ = sum.Write([]byte{0}) // a separator, so "ab"+"c" differs from "a"+"bc"
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// ActivityChanged tells the hub that a subscriber's Active() changed, so idle timers are re-evaluated.
func (h *Hub) ActivityChanged(sess session.Session, k Key) {
	uk, err := h.ukeyFor(h.ctx, sess, k)
	if err != nil {
		return
	}
	h.mu.Lock()
	u := h.ups[uk]
	h.mu.Unlock()
	if u != nil {
		u.send(command{kind: cmdRecheck})
	}
}

// upstreamFor finds or creates the upstream. A new upstream starts with sub already queued, so that
// its first list cannot fail before the subscriber is there to hear about it.
func (h *Hub) upstreamFor(sess session.Session, k ukey, sub *subscription) (u *upstream, added bool, err error) {
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
	h.wg.Add(1)
	go u.run(h.ctx)
	h.mu.Unlock()
	return u, true, nil
}

// listenForEnd registers, once per session, what to do when that session ends.
func (h *Hub) listenForEnd(sess session.Session) {
	if h.d.Closers == nil {
		return
	}
	h.mu.Lock()
	_, known := h.sessions[sess.Hash]
	h.sessions[sess.Hash] = sess.UserID
	h.mu.Unlock()
	if !known {
		// Outside the lock: for an ended session the closer runs at once and takes it.
		h.d.Closers.Register(sess.Hash, "watch-hub", func() error { h.stopSession(sess.Hash); return nil })
	}
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
	if h.ups[u.ukey] == u {
		delete(h.ups, u.ukey)
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

// stopSession drops the ended session from the user's upstreams. An upstream with no subscriber
// left stops; one that ran with the ended session's credentials restarts under a remaining session.
func (h *Hub) stopSession(hash string) {
	h.mu.Lock()
	userID := h.sessions[hash]
	delete(h.sessions, hash)
	var mine []*upstream
	for _, u := range h.order {
		if u.key.UserID == userID {
			mine = append(mine, u)
		}
	}
	h.mu.Unlock()
	for _, u := range mine {
		u.send(command{kind: cmdDropSession, hash: hash})
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

// WatchesPerUser returns, for every user that holds any upstream, how many it holds; the counts are sorted.
func (h *Hub) WatchesPerUser() []int {
	h.mu.Lock()
	perUser := map[string]int{}
	for _, u := range h.order {
		perUser[u.key.UserID]++
	}
	h.mu.Unlock()
	out := make([]int, 0, len(perUser))
	for _, n := range perUser {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// Close stops every upstream and waits for them.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.cancel()
	h.wg.Wait()
}
