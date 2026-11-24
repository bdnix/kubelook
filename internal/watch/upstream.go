package watch

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	k8swatch "k8s.io/apimachinery/pkg/watch"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/redact"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/ws"
)

const (
	firstBackoff = time.Second
	maxBackoff   = 30 * time.Second
	// healthyAfter is how long a watch must have lived for its end to count as normal (the API
	// server closes idle watches after a few minutes) rather than as a failure.
	healthyAfter = 30 * time.Second
	commandQueue = 1024
)

type cmdKind int

const (
	cmdAdd         cmdKind = iota // attach a subscriber
	cmdRemove                     // detach a subscriber
	cmdRecheck                    // a subscriber's Active() changed
	cmdPause                      // tell everyone to reopen later, then stop
	cmdStop                       // stop silently
	cmdDropSession                // a session ended: forget its subscribers, restart if it owned the credentials
)

type command struct {
	kind cmdKind
	sub  *subscription
	hash string // cmdDropSession
}

type subscription struct {
	s    Subscriber
	sess session.Session
}

// upstream is one list-then-watch loop. Its goroutine owns the cache, the watch and the timers;
// everything else talks to it through commands.
type upstream struct {
	h    *Hub
	sess session.Session // the session whose credentials the list and watch use
	key  Key
	ukey ukey
	cmds chan command
	done chan struct{}

	subs, cached atomic.Int64

	mu      sync.Mutex // guards sublist and stopped
	sublist []*subscription
	stopped bool

	// owned by run
	cache     map[types.UID]*unstructured.Unstructured
	rv        string
	ready     bool
	needList  bool
	w         k8swatch.Interface
	watchedAt time.Time
	gotEvent  bool
	bo        *backoff
	retryT    clock.Timer
	retryC    <-chan time.Time
	idleT     clock.Timer
	idleC     <-chan time.Time
}

func newUpstream(h *Hub, sess session.Session, k ukey) *upstream {
	return &upstream{
		h: h, sess: sess, key: k.Key, ukey: k, cmds: make(chan command, commandQueue), done: make(chan struct{}),
		cache: map[types.UID]*unstructured.Unstructured{}, needList: true, bo: newBackoff(firstBackoff, maxBackoff),
	}
}

// send hands a command to the upstream's goroutine. It reports false when the upstream has stopped.
func (u *upstream) send(c command) bool {
	u.mu.Lock()
	if u.stopped {
		u.mu.Unlock()
		return false
	}
	select {
	case u.cmds <- c:
		u.mu.Unlock()
		return true
	default:
	}
	u.mu.Unlock()
	select {
	case u.cmds <- c:
		return true
	case <-u.done:
		return false
	}
}

func (u *upstream) subscribers() []*subscription {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]*subscription(nil), u.sublist...)
}

// onlyHidden reports whether no subscriber looks at this watch right now.
func (u *upstream) onlyHidden() bool {
	for _, s := range u.subscribers() {
		if s.s.Active() {
			return false
		}
	}
	return true
}

func (u *upstream) run(ctx context.Context) {
	defer u.h.wg.Done()
	defer u.finish()
	for {
		if u.drainCommands(ctx) {
			return
		}
		for u.w == nil && u.retryC == nil { // a gone watch relists at once
			if stop := u.establish(ctx); stop {
				return
			}
		}
		var events <-chan k8swatch.Event
		if u.w != nil {
			events = u.w.ResultChan()
		}
		select {
		case <-ctx.Done():
			return
		case c := <-u.cmds:
			if u.handle(ctx, c) {
				return
			}
		case <-u.retryC:
			u.retryT, u.retryC = nil, nil
		case <-u.idleC:
			u.idleT, u.idleC = nil, nil
			if !u.anyActive() {
				for _, s := range u.subscribers() {
					s.s.Paused()
				}
				return
			}
		case ev, ok := <-events:
			if u.onEvent(ev, ok) {
				return
			}
		}
	}
}

// drainCommands runs the commands that are already queued. It reports whether to stop.
func (u *upstream) drainCommands(ctx context.Context) bool {
	for {
		select {
		case c := <-u.cmds:
			if u.handle(ctx, c) {
				return true
			}
		default:
			return false
		}
	}
}

// finish deregisters the upstream, stops its timers and watch and answers commands left in the queue.
func (u *upstream) finish() {
	u.h.remove(u)
	u.stopWatch()
	u.stopRetry()
	u.stopIdle()
	u.mu.Lock()
	u.stopped = true
	u.mu.Unlock()
	for {
		select {
		case c := <-u.cmds:
			if c.kind == cmdAdd {
				c.sub.s.Paused() // arrived as the watch ended: the browser reopens it
			}
			continue
		default:
		}
		break
	}
	close(u.done)
}

func (u *upstream) stopWatch() {
	if u.w != nil {
		u.w.Stop()
		u.w = nil
	}
}

func (u *upstream) stopRetry() {
	if u.retryT != nil {
		u.retryT.Stop()
		u.retryT, u.retryC = nil, nil
	}
}

func (u *upstream) stopIdle() {
	if u.idleT != nil {
		u.idleT.Stop()
		u.idleT, u.idleC = nil, nil
	}
}

func (u *upstream) scheduleRetry() {
	u.retryT = u.h.d.Clock.NewTimer(u.bo.next())
	u.retryC = u.retryT.C()
}

// permanent reports errors that retrying cannot fix.
func permanent(err error) bool {
	return errors.Is(err, apperr.ErrForbidden) || errors.Is(err, apperr.ErrNotFound) || errors.Is(err, apperr.ErrInvalid)
}

// errCredentialRefused is a 401 from the API server: the credential expired or was replaced. It is not
// permanent: the next attempt gets a fresh client, whose credential the broker re-mints (spec: watches resume).
var errCredentialRefused = errors.New("the cluster refused the credential; retrying with a fresh one")

// mapErr maps a Kubernetes error for the hub. A 401 is retried; everything else maps as for any request.
func mapErr(err error) error {
	if apierrors.IsUnauthorized(err) {
		return errCredentialRefused
	}
	return kube.MapError(err)
}

func isGone(err error) bool { return apierrors.IsGone(err) || apierrors.IsResourceExpired(err) }

func (u *upstream) failAll(err error) {
	body := ws.ErrorBody(err)
	for _, s := range u.subscribers() {
		s.s.Failed(body["code"], body["message"])
	}
}

// establish lists (when needed) and starts the watch. It reports whether the upstream must stop.
func (u *upstream) establish(ctx context.Context) (stop bool) {
	if u.needList {
		if err := u.list(ctx); err != nil {
			return u.failed(ctx, err)
		}
		u.needList = false
	}
	dyn, err := u.h.d.Clients.Dynamic(ctx, u.sess, u.key.Cluster)
	if err != nil {
		return u.failed(ctx, mapErr(err))
	}
	opts := metav1.ListOptions{ResourceVersion: u.rv, AllowWatchBookmarks: true, FieldSelector: u.key.FieldSelector}
	res := dyn.Resource(u.key.GVR)
	var w k8swatch.Interface
	if u.key.Namespace != "" {
		w, err = res.Namespace(u.key.Namespace).Watch(ctx, opts)
	} else {
		w, err = res.Watch(ctx, opts)
	}
	if err != nil {
		if isGone(err) {
			u.needList = true
			return false
		}
		return u.failed(ctx, mapErr(err))
	}
	u.w, u.watchedAt, u.gotEvent = w, u.h.d.Clock.Now(), false
	return false
}

// failed handles a failed list or watch: permanent errors end the upstream, others back off.
func (u *upstream) failed(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	if permanent(err) {
		u.failAll(err)
		return true
	}
	u.h.d.Log.Warn("watch upstream failed", "cluster", u.key.Cluster, "resource", u.key.GVR.Resource, "error", err.Error())
	u.scheduleRetry()
	return false
}

// listPageSize is the page size of the first list (spec "Replica memory").
const listPageSize = 500

// maxListRestarts bounds how often an expired continue token restarts a list.
const maxListRestarts = 3

// list reads the current state page by page, replaces the cache and sends a snapshot to every
// subscriber. Each page is redacted and cached before the next is requested, so the unredacted full
// list is never held at once. The watch starts from the version of the first page.
func (u *upstream) list(ctx context.Context) error {
	dyn, err := u.h.d.Clients.Dynamic(ctx, u.sess, u.key.Cluster)
	if err != nil {
		return mapErr(err)
	}
	res := dyn.Resource(u.key.GVR)
	fresh := map[types.UID]*unstructured.Unstructured{}
	rv, cont, restarts := "", "", 0
	for {
		opts := metav1.ListOptions{FieldSelector: u.key.FieldSelector, Limit: listPageSize, Continue: cont}
		var page *unstructured.UnstructuredList
		if u.key.Namespace != "" {
			page, err = res.Namespace(u.key.Namespace).List(ctx, opts)
		} else {
			page, err = res.List(ctx, opts)
		}
		if err != nil {
			if cont != "" && isGone(err) && restarts < maxListRestarts {
				// the continue token expired: start again from the beginning
				fresh, rv, cont, restarts = map[types.UID]*unstructured.Unstructured{}, "", "", restarts+1
				continue
			}
			return mapErr(err)
		}
		if cont == "" {
			rv = page.GetResourceVersion()
		}
		items := redact.List(page.Items)
		for i := range items {
			fresh[items[i].GetUID()] = &items[i]
		}
		if cont = page.GetContinue(); cont == "" {
			break
		}
	}
	u.cache = fresh
	u.rv, u.ready = rv, true
	u.cached.Store(int64(len(u.cache)))
	snap := u.snapshot()
	for _, s := range u.subscribers() {
		s.s.Snapshot(snap, u.rv)
	}
	return nil
}

func (u *upstream) snapshot() []*unstructured.Unstructured {
	items := make([]*unstructured.Unstructured, 0, len(u.cache))
	for _, o := range u.cache {
		items = append(items, o)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].GetNamespace() != items[j].GetNamespace() {
			return items[i].GetNamespace() < items[j].GetNamespace()
		}
		return items[i].GetName() < items[j].GetName()
	})
	return items
}

// onEvent processes one watch event (ok is false when the watch ended). It reports whether to stop.
func (u *upstream) onEvent(ev k8swatch.Event, ok bool) bool {
	if !ok {
		u.stopWatch()
		if u.gotEvent || u.h.d.Clock.Now().Sub(u.watchedAt) >= healthyAfter {
			u.bo.reset() // an ordinary end of a healthy watch: reconnect at once
		} else {
			u.scheduleRetry()
		}
		return false
	}
	switch ev.Type {
	case k8swatch.Bookmark:
		if o, isObj := ev.Object.(*unstructured.Unstructured); isObj {
			u.rv = o.GetResourceVersion()
		}
	case k8swatch.Added, k8swatch.Modified, k8swatch.Deleted:
		u.gotEvent = true
		u.bo.reset()
		if o, isObj := ev.Object.(*unstructured.Unstructured); isObj {
			u.apply(ev.Type, o)
		}
	case k8swatch.Error:
		u.stopWatch()
		err := apierrors.FromObject(ev.Object)
		switch {
		case isGone(err):
			u.needList = true
		case permanent(mapErr(err)):
			u.failAll(mapErr(err))
			return true
		default:
			u.scheduleRetry()
		}
	}
	return false
}

func (u *upstream) apply(typ k8swatch.EventType, o *unstructured.Unstructured) {
	u.rv = o.GetResourceVersion()
	if !redact.Apply(o) {
		return
	}
	switch typ {
	case k8swatch.Deleted:
		delete(u.cache, o.GetUID())
	default:
		u.cache[o.GetUID()] = o
	}
	u.cached.Store(int64(len(u.cache)))
	for _, s := range u.subscribers() {
		s.s.Event(string(typ), o)
	}
}

// handle runs one command. It reports whether to stop.
func (u *upstream) handle(_ context.Context, c command) bool {
	switch c.kind {
	case cmdAdd:
		u.mu.Lock()
		u.sublist = append(u.sublist, c.sub)
		u.subs.Store(int64(len(u.sublist)))
		u.mu.Unlock()
		if u.ready {
			c.sub.s.Snapshot(u.snapshot(), u.rv)
		}
		u.evalIdle()
	case cmdRemove:
		u.mu.Lock()
		for i, s := range u.sublist {
			if s == c.sub {
				u.sublist = append(u.sublist[:i], u.sublist[i+1:]...)
				break
			}
		}
		u.subs.Store(int64(len(u.sublist)))
		u.mu.Unlock()
		u.evalIdle()
	case cmdRecheck:
		u.evalIdle()
	case cmdPause:
		for _, s := range u.subscribers() {
			s.s.Paused()
		}
		return true
	case cmdStop:
		return true
	case cmdDropSession:
		return u.dropSession(c.hash)
	}
	return false
}

// dropSession forgets the subscribers of an ended session. It reports whether the upstream must
// stop (nobody is left). When the ended session owned the credentials, the upstream restarts under
// a remaining session: a fresh list, so its subscribers get a fresh snapshot.
func (u *upstream) dropSession(hash string) bool {
	u.mu.Lock()
	kept := u.sublist[:0:0]
	for _, s := range u.sublist {
		if s.sess.Hash != hash {
			kept = append(kept, s)
		}
	}
	u.sublist = kept
	u.subs.Store(int64(len(kept)))
	u.mu.Unlock()
	if len(kept) == 0 {
		return true
	}
	if u.sess.Hash == hash {
		u.sess = kept[0].sess
		u.stopWatch()
		u.stopRetry()
		u.bo.reset()
		u.needList = true
	}
	u.evalIdle()
	return false
}

func (u *upstream) anyActive() bool { return !u.onlyHidden() && len(u.subscribers()) > 0 }

// evalIdle starts the idle timer when nobody is looking and cancels it when somebody is.
func (u *upstream) evalIdle() {
	if u.anyActive() {
		u.stopIdle()
		return
	}
	if u.idleC != nil {
		return
	}
	secs := DefaultIdleSeconds
	if u.h.d.IdleSeconds != nil {
		if n := u.h.d.IdleSeconds(context.Background(), u.key.UserID); n > 0 {
			secs = n
		}
	}
	u.idleT = u.h.d.Clock.NewTimer(time.Duration(secs) * time.Second)
	u.idleC = u.idleT.C()
}
