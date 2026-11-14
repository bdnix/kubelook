package watch_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
	"github.com/bdnix/kubelook/internal/watch"
)

var (
	t0      = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	sessA   = session.Session{Hash: "sa", UserID: "u1"}
	sessB   = session.Session{Hash: "sb", UserID: "u2"}
)

func keyFor(s session.Session, ns string) watch.Key {
	return watch.Key{SessionHash: s.Hash, UserID: s.UserID, Cluster: "c", GVR: podsGVR, Namespace: ns}
}

// rec is a subscriber that reports every call on a channel.
type rec struct {
	frames chan string
	active atomic.Bool
	snaps  chan []string
}

func newRec() *rec {
	r := &rec{frames: make(chan string, 64), snaps: make(chan []string, 16)}
	r.active.Store(true)
	return r
}

func nameList(items []*unstructured.Unstructured) []string {
	out := []string{}
	for _, o := range items {
		out = append(out, o.GetName())
	}
	return out
}

func (r *rec) Snapshot(items []*unstructured.Unstructured, rv string) {
	r.snaps <- nameList(items)
	r.frames <- fmt.Sprintf("snapshot rv=%s %v", rv, nameList(items))
}
func (r *rec) Event(typ string, o *unstructured.Unstructured) {
	r.frames <- fmt.Sprintf("%s %s", typ, o.GetName())
}
func (r *rec) Paused()                  { r.frames <- "paused" }
func (r *rec) Failed(code, _ string)    { r.frames <- "failed " + code }
func (r *rec) Active() bool             { return r.active.Load() }
func (r *rec) next(t *testing.T) string { t.Helper(); return testutil.Recv(t, r.frames) }

// spyClients wraps the fakes, counts lists and watches and hands the fake watchers to the test.
type spyClients struct {
	*fakekube.Clients
	mu       sync.Mutex
	lists    []metav1.ListOptions
	watches  []metav1.ListOptions
	watchers chan *k8swatch.FakeWatcher
}

func (s *spyClients) Dynamic(ctx context.Context, sess session.Session, id string) (dynamic.Interface, error) {
	d, err := s.Clients.Dynamic(ctx, sess, id)
	return spyDyn{d, s}, err
}

func (s *spyClients) counts() (lists, watches int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lists), len(s.watches)
}

type spyDyn struct {
	dynamic.Interface
	s *spyClients
}

func (d spyDyn) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return spyRes{d.Interface.Resource(gvr), d.s}
}

type spyRes struct {
	dynamic.NamespaceableResourceInterface
	s *spyClients
}

func (r spyRes) List(ctx context.Context, o metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	r.s.mu.Lock()
	r.s.lists = append(r.s.lists, o)
	r.s.mu.Unlock()
	return r.NamespaceableResourceInterface.List(ctx, o)
}

func (r spyRes) Watch(ctx context.Context, o metav1.ListOptions) (k8swatch.Interface, error) {
	r.s.mu.Lock()
	r.s.watches = append(r.s.watches, o)
	r.s.mu.Unlock()
	return r.NamespaceableResourceInterface.Watch(ctx, o)
}

func (r spyRes) Namespace(ns string) dynamic.ResourceInterface {
	return spyNs{r.NamespaceableResourceInterface.Namespace(ns), r.s}
}

type spyNs struct {
	dynamic.ResourceInterface
	s *spyClients
}

func (r spyNs) List(ctx context.Context, o metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	r.s.mu.Lock()
	r.s.lists = append(r.s.lists, o)
	r.s.mu.Unlock()
	return r.ResourceInterface.List(ctx, o)
}

func (r spyNs) Watch(ctx context.Context, o metav1.ListOptions) (k8swatch.Interface, error) {
	r.s.mu.Lock()
	r.s.watches = append(r.s.watches, o)
	r.s.mu.Unlock()
	return r.ResourceInterface.Watch(ctx, o)
}

type hubEnv struct {
	hub     *watch.Hub
	spy     *spyClients
	clock   *clock.Fake
	closers *session.Registry
	// state served by the fake list
	mu        sync.Mutex
	items     []*unstructured.Unstructured
	listRV    string
	listErr   error
	watchErr  func() error
	idleSecs  int
	maxPerUsr int
}

func pod(name string) *unstructured.Unstructured { return fakekube.Pod("ns1", name, nil) }

func newHubEnv(t *testing.T, mutate ...func(*hubEnv)) *hubEnv {
	t.Helper()
	e := &hubEnv{clock: clock.NewFake(t0), closers: session.NewRegistry(), listRV: "100", items: []*unstructured.Unstructured{pod("a"), pod("b")}}
	for _, m := range mutate {
		m(e)
	}
	e.spy = &spyClients{Clients: fakekube.New(), watchers: make(chan *k8swatch.FakeWatcher, 16)}
	e.spy.Dyn.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.listErr != nil {
			return true, nil, e.listErr
		}
		items := make([]any, len(e.items))
		for i, o := range e.items {
			items[i] = o.DeepCopy().Object
		}
		return true, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "PodList", "metadata": map[string]any{"resourceVersion": e.listRV}, "items": items,
		}}, nil
	})
	e.spy.Dyn.PrependWatchReactor("pods", func(clienttesting.Action) (bool, k8swatch.Interface, error) {
		e.mu.Lock()
		fn := e.watchErr
		e.mu.Unlock()
		if fn != nil {
			if err := fn(); err != nil {
				return true, nil, err
			}
		}
		w := k8swatch.NewFake()
		e.spy.watchers <- w
		return true, w, nil
	})
	e.hub = watch.NewHub(watch.Deps{
		Clients: e.spy, Clock: e.clock, Log: slog.New(slog.DiscardHandler), Closers: e.closers, MaxPerUser: e.maxPerUsr,
		IdleSeconds: func(context.Context, string) int { return e.idleSecs },
	})
	t.Cleanup(e.hub.Close)
	return e
}

func (e *hubEnv) subscribe(t *testing.T, s session.Session, ns string, r *rec) func() {
	t.Helper()
	unsub, err := e.hub.Subscribe(t.Context(), s, keyFor(s, ns), r)
	require.NoError(t, err)
	return unsub
}

func (e *hubEnv) watcher(t *testing.T) *k8swatch.FakeWatcher { return testutil.Recv(t, e.spy.watchers) }

func withRV(o *unstructured.Unstructured, rv string) *unstructured.Unstructured {
	o = o.DeepCopy()
	o.SetResourceVersion(rv)
	return o
}

func TestHub_FirstSubscriber_OneListThenOneWatchFromItsResourceVersion(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	assert.Equal(t, "snapshot rv=100 [a b]", r.next(t))
	e.watcher(t)
	lists, watches := e.spy.counts()
	assert.Equal(t, 1, lists)
	assert.Equal(t, 1, watches)
	assert.Empty(t, e.spy.lists[0].ResourceVersion, "a fresh list, not served from the watch cache")
	assert.Equal(t, "100", e.spy.watches[0].ResourceVersion)
	assert.True(t, e.spy.watches[0].AllowWatchBookmarks)
}

func TestHub_NamespaceKeyListsOneNamespace(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	e.subscribe(t, sessA, "ns1", r)
	assert.Equal(t, "snapshot rv=100 [a b]", r.next(t))
	e.watcher(t)
	lists, watches := e.spy.counts()
	assert.Equal(t, 1, lists)
	assert.Equal(t, 1, watches)
}

func TestHub_SecondSubscriberSharesTheUpstreamAndGetsTheCachedState(t *testing.T) {
	e := newHubEnv(t)
	r1 := newRec()
	e.subscribe(t, sessA, "", r1)
	r1.next(t)
	w := e.watcher(t)
	w.Add(withRV(pod("c"), "101"))
	w.Modify(withRV(pod("a"), "102"))
	assert.Equal(t, "ADDED c", r1.next(t))
	assert.Equal(t, "MODIFIED a", r1.next(t))

	r2 := newRec()
	e.subscribe(t, sessA, "", r2)
	assert.Equal(t, "snapshot rv=102 [a b c]", r2.next(t))
	lists, watches := e.spy.counts()
	assert.Equal(t, 1, lists)
	assert.Equal(t, 1, watches)

	w.Delete(withRV(pod("b"), "103"))
	assert.Equal(t, "DELETED b", r1.next(t))
	assert.Equal(t, "DELETED b", r2.next(t))
	r3 := newRec()
	e.subscribe(t, sessA, "", r3)
	assert.Equal(t, "snapshot rv=103 [a c]", r3.next(t))
	assert.Equal(t, hubStats(1, 3, 2), e.hub.Stats())
}

func hubStats(watches, subs, cached int) watch.Stats {
	return watch.Stats{Watches: watches, Subscribers: subs, CachedObjects: cached}
}

func TestHub_AnotherUserWithTheSameKeyGetsItsOwnUpstream(t *testing.T) {
	e := newHubEnv(t)
	ra, rb := newRec(), newRec()
	e.subscribe(t, sessA, "", ra)
	e.subscribe(t, sessB, "", rb)
	ra.next(t)
	rb.next(t)
	e.watcher(t)
	e.watcher(t)
	lists, watches := e.spy.counts()
	assert.Equal(t, 2, lists)
	assert.Equal(t, 2, watches)
	assert.Equal(t, 2, e.hub.Stats().Watches)
}

func TestHub_Events_AreRedactedBeforeCachingAndHelmReleasesAreHidden(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	r.next(t)
	w := e.watcher(t)

	sec := withRV(fakekube.Secret("ns1", "db", map[string]any{"password": "c2VjcmV0"}), "101")
	helm := withRV(fakekube.Secret("ns1", "sh.helm.release.v1.web.v1", map[string]any{"release": "aGVsbQ=="}), "102")
	w.Add(sec)
	assert.Equal(t, "ADDED db", r.next(t))
	w.Add(helm)
	w.Add(withRV(pod("z"), "103"))
	assert.Equal(t, "ADDED z", r.next(t), "the Helm release produced no event")

	r2 := newRec()
	e.subscribe(t, sessA, "", r2)
	snap := testutil.Recv(t, r2.snaps)
	assert.Equal(t, []string{"a", "b", "db", "z"}, snap)
	assert.Equal(t, "********", sec.Object["data"].(map[string]any)["password"], "masked in place before it was cached")
}

func TestHub_Bookmark_UpdatesTheResourceVersionAndIsNotForwarded(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	r.next(t)
	w := e.watcher(t)
	w.Action(k8swatch.Bookmark, withRV(pod("ignored"), "150"))
	w.Add(withRV(pod("c"), "151"))
	assert.Equal(t, "ADDED c", r.next(t), "the bookmark produced no frame")
	w.Stop()
	e.watcher(t) // the hub reconnects
	assert.Equal(t, "151", e.spy.watches[1].ResourceVersion)
}

func TestHub_UpstreamClosed_ResumesFromTheLastVersionWithoutANewSnapshot(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	r.next(t)
	w := e.watcher(t)
	w.Add(withRV(pod("c"), "101"))
	assert.Equal(t, "ADDED c", r.next(t))
	w.Stop() // a network blip after a healthy stretch

	w2 := e.watcher(t)
	lists, watches := e.spy.counts()
	assert.Equal(t, 1, lists)
	assert.Equal(t, 2, watches)
	assert.Equal(t, "101", e.spy.watches[1].ResourceVersion)
	w2.Add(withRV(pod("d"), "102"))
	assert.Equal(t, "ADDED d", r.next(t), "no snapshot in between")
}

func TestHub_GoneEvent_RelistsAndSendsAFreshSnapshotToEverySubscriber(t *testing.T) {
	e := newHubEnv(t)
	r1, r2 := newRec(), newRec()
	e.subscribe(t, sessA, "", r1)
	e.subscribe(t, sessA, "", r2)
	r1.next(t)
	r2.next(t)
	w := e.watcher(t)

	e.mu.Lock()
	e.items, e.listRV = []*unstructured.Unstructured{pod("x"), pod("y")}, "200"
	e.mu.Unlock()
	w.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 410, Reason: metav1.StatusReasonExpired, Message: "too old"})
	assert.Equal(t, "snapshot rv=200 [x y]", r1.next(t))
	assert.Equal(t, "snapshot rv=200 [x y]", r2.next(t))
	e.watcher(t)
	lists, watches := e.spy.counts()
	assert.Equal(t, 2, lists)
	assert.Equal(t, 2, watches)
	assert.Equal(t, "200", e.spy.watches[1].ResourceVersion)
}

func TestHub_WatchReturnsGone_RelistsToo(t *testing.T) {
	var calls atomic.Int32
	e := newHubEnv(t, func(e *hubEnv) {
		e.watchErr = func() error {
			if calls.Add(1) == 1 {
				return apierrors.NewResourceExpired("too old")
			}
			return nil
		}
	})
	r := newRec()
	e.subscribe(t, sessA, "", r)
	assert.Equal(t, "snapshot rv=100 [a b]", r.next(t))
	assert.Equal(t, "snapshot rv=100 [a b]", r.next(t), "the second list answers the gone watch")
	e.watcher(t)
	lists, _ := e.spy.counts()
	assert.Equal(t, 2, lists)
}

func TestHub_RepeatedFailuresBackOffOnTheFakeClock(t *testing.T) {
	var attempts atomic.Int32
	attempted := make(chan struct{}, 32)
	e := newHubEnv(t, func(e *hubEnv) {
		e.watchErr = func() error {
			attempts.Add(1)
			attempted <- struct{}{}
			return errors.New("connection refused")
		}
	})
	r := newRec()
	e.subscribe(t, sessA, "", r)
	r.next(t)
	testutil.Recv(t, attempted)
	for _, wait := range []time.Duration{1, 2, 4, 8, 16, 30, 30} {
		d := wait * time.Second
		e.clock.BlockUntil(1)
		e.clock.Advance(d - time.Millisecond)
		select {
		case <-attempted:
			t.Fatalf("retried before %v", d)
		default:
		}
		e.clock.Advance(time.Millisecond)
		testutil.Recv(t, attempted)
	}
	assert.EqualValues(t, 8, attempts.Load())
}

func TestHub_ForbiddenList_FailsTheSubscribersAndDoesNotRetry(t *testing.T) {
	e := newHubEnv(t, func(e *hubEnv) {
		e.listErr = apierrors.NewForbidden(podsGVR.GroupResource(), "", errors.New("RBAC"))
	})
	r := newRec()
	e.subscribe(t, sessA, "", r)
	assert.Equal(t, "failed forbidden", r.next(t))
	e.clock.Advance(5 * time.Minute)
	lists, watches := e.spy.counts()
	assert.Equal(t, 1, lists)
	assert.Equal(t, 0, watches)
	assert.Equal(t, 0, e.hub.Stats().Watches)

	// a later subscriber starts over
	e.mu.Lock()
	e.listErr = nil
	e.mu.Unlock()
	r2 := newRec()
	e.subscribe(t, sessA, "", r2)
	assert.Equal(t, "snapshot rv=100 [a b]", r2.next(t))
}

func TestHub_Idle_LastActiveSubscriberLeaves_StopsAfterTheUsersIdleTime(t *testing.T) {
	e := newHubEnv(t)
	active, hidden := newRec(), newRec()
	hidden.active.Store(false)
	unsub := e.subscribe(t, sessA, "", active)
	e.subscribe(t, sessA, "", hidden)
	active.next(t)
	hidden.next(t)
	w := e.watcher(t)

	unsub()
	e.clock.BlockUntil(1)
	e.clock.Advance(29 * time.Second)
	assert.Equal(t, 1, e.hub.Stats().Watches)
	e.clock.Advance(time.Second)
	assert.Equal(t, "paused", hidden.next(t))
	assert.True(t, w.IsStopped())
	e.hub.Close()
	assert.Equal(t, 0, e.hub.Stats().Watches)
}

func TestHub_Idle_ReturningBeforeTheTimeoutCancelsTheStop(t *testing.T) {
	e := newHubEnv(t, func(e *hubEnv) { e.idleSecs = 10 })
	r := newRec()
	e.subscribe(t, sessA, "", r)
	r.next(t)
	e.watcher(t)

	r.active.Store(false)
	e.hub.ActivityChanged(keyFor(sessA, ""))
	e.clock.BlockUntil(1)
	e.clock.Advance(9 * time.Second)
	r.active.Store(true)
	e.hub.ActivityChanged(keyFor(sessA, ""))
	e.clock.Advance(5 * time.Second) // the cancelled timer would have fired by now
	e.clock.Advance(20 * time.Second)

	// still alive: an event gets through
	assert.Equal(t, 1, e.hub.Stats().Watches)
	r2 := newRec()
	e.subscribe(t, sessA, "", r2)
	assert.Equal(t, "snapshot rv=100 [a b]", r2.next(t))
}

func TestHub_Idle_NoSubscribersLeft_StopsSilently(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	unsub := e.subscribe(t, sessA, "", r)
	r.next(t)
	w := e.watcher(t)
	unsub()
	e.clock.BlockUntil(1)
	e.clock.Advance(30 * time.Second)
	e.hub.Close() // waits for the upstream, which has already stopped or stops now
	assert.True(t, w.IsStopped())
	select {
	case f := <-r.frames:
		t.Fatalf("unexpected frame %q", f)
	default:
	}
}

func TestHub_Limit_TheOldestHiddenWatchIsPausedForTheNewOne(t *testing.T) {
	e := newHubEnv(t, func(e *hubEnv) { e.maxPerUsr = 2 })
	hiddenA, hiddenB := newRec(), newRec()
	hiddenA.active.Store(false)
	hiddenB.active.Store(false)
	e.subscribe(t, sessA, "ns-a", hiddenA)
	e.subscribe(t, sessA, "ns-b", hiddenB)
	hiddenA.next(t)
	hiddenB.next(t)
	e.watcher(t)
	e.watcher(t)

	third := newRec()
	e.subscribe(t, sessA, "ns-c", third)
	assert.Equal(t, "paused", hiddenA.next(t), "the oldest watch with only hidden subscribers is closed")
	assert.Equal(t, "snapshot rv=100 [a b]", third.next(t))
	assert.Equal(t, 2, e.hub.Stats().Watches)
}

func TestHub_Limit_NoHiddenWatch_TheOpenFails(t *testing.T) {
	e := newHubEnv(t, func(e *hubEnv) { e.maxPerUsr = 1 })
	first := newRec()
	e.subscribe(t, sessA, "ns-a", first)
	first.next(t)
	_, err := e.hub.Subscribe(t.Context(), sessA, keyFor(sessA, "ns-b"), newRec())
	var api *apperr.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, "too_many_watches", api.Code)

	// another user is not affected
	other := newRec()
	e.subscribe(t, sessB, "ns-a", other)
	other.next(t)
}

func TestHub_SessionEnd_StopsItsUpstreams(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	r.next(t)
	w := e.watcher(t)
	other := newRec()
	e.subscribe(t, sessB, "", other)
	other.next(t)
	wb := e.watcher(t)

	require.NoError(t, e.closers.Close(sessA.Hash))
	// the stop is asynchronous: wait for the watcher to be released
	for !w.IsStopped() {
		e.hub.Stats()
	}
	assert.False(t, wb.IsStopped())
	assert.Equal(t, 1, e.hub.Stats().Watches)
}

func TestHub_Close_RefusesNewSubscribers(t *testing.T) {
	e := newHubEnv(t)
	e.hub.Close()
	_, err := e.hub.Subscribe(t.Context(), sessA, keyFor(sessA, ""), newRec())
	require.ErrorIs(t, err, apperr.ErrBusy)
}

func TestHub_ActivityChanged_UnknownKeyIsIgnored(t *testing.T) {
	e := newHubEnv(t)
	e.hub.ActivityChanged(keyFor(sessA, "nope"))
}

func TestHub_WatchErrorEventThatIsNotGone_BacksOffAndReconnects(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	r.next(t)
	w := e.watcher(t)
	w.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 500, Reason: metav1.StatusReasonInternalError, Message: "etcd hiccup"})
	e.clock.BlockUntil(1)
	e.clock.Advance(time.Second)
	e.watcher(t)
	lists, _ := e.spy.counts()
	assert.Equal(t, 1, lists, "no relist for an ordinary error")
}

func TestHub_WatchErrorEventForbidden_FailsPermanently(t *testing.T) {
	e := newHubEnv(t)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	r.next(t)
	w := e.watcher(t)
	w.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 403, Reason: metav1.StatusReasonForbidden, Message: "no"})
	assert.Equal(t, "failed forbidden", r.next(t))
}

func TestHub_ListFailureThatIsTransient_RetriesAfterTheBackoff(t *testing.T) {
	e := newHubEnv(t, func(e *hubEnv) { e.listErr = errors.New("timeout") })
	r := newRec()
	e.subscribe(t, sessA, "", r)
	e.clock.BlockUntil(1)
	e.mu.Lock()
	e.listErr = nil
	e.mu.Unlock()
	e.clock.Advance(time.Second)
	assert.Equal(t, "snapshot rv=100 [a b]", r.next(t))
}
