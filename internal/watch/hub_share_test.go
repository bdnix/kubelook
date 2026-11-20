package watch_test

import (
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
	"github.com/bdnix/kubelook/internal/watch"
)

// pagedPods serves n pods in pages of the requested limit. Every page carries the resourceVersion
// "100" except the pages after the first, which carry "999" so a test can tell them apart.
func pagedPods(n int, calls *atomic.Int32, failSecondPageOnce *atomic.Bool) func(metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return func(o metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		calls.Add(1)
		start := 0
		if o.Continue != "" {
			start, _ = strconv.Atoi(o.Continue)
			if failSecondPageOnce != nil && failSecondPageOnce.CompareAndSwap(true, false) {
				return nil, apierrors.NewResourceExpired("continue token expired")
			}
		}
		end := min(start+int(o.Limit), n)
		list := &unstructured.UnstructuredList{}
		list.SetResourceVersion("100")
		if start > 0 {
			list.SetResourceVersion("999")
		}
		for i := start; i < end; i++ {
			list.Items = append(list.Items, *fakekube.Pod("ns1", fmt.Sprintf("p%04d", i), nil))
		}
		if end < n {
			list.SetContinue(strconv.Itoa(end))
		}
		return list, nil
	}
}

func TestHub_FirstList_IsPagedAndTheWatchStartsFromTheFirstPagesVersion(t *testing.T) {
	e := newHubEnv(t)
	var calls atomic.Int32
	e.spy.pager = pagedPods(1200, &calls, nil)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	snap := testutil.Recv(t, r.snaps)
	e.watcher(t)

	assert.Len(t, snap, 1200, "the snapshot holds every object")
	assert.EqualValues(t, 3, calls.Load(), "1,200 objects in pages of 500 are three List calls")
	require.Len(t, e.spy.lists, 3)
	assert.EqualValues(t, 500, e.spy.lists[0].Limit)
	assert.Empty(t, e.spy.lists[0].Continue)
	assert.Equal(t, "500", e.spy.lists[1].Continue)
	assert.Equal(t, "1000", e.spy.lists[2].Continue)
	assert.EqualValues(t, 500, e.spy.lists[2].Limit)
	require.Len(t, e.spy.watches, 1)
	assert.Equal(t, "100", e.spy.watches[0].ResourceVersion, "the watch starts from the first page's version")
}

func TestHub_ListContinueExpired_RestartsFromTheBeginning(t *testing.T) {
	e := newHubEnv(t)
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	e.spy.pager = pagedPods(1200, &calls, &fail)
	r := newRec()
	e.subscribe(t, sessA, "", r)
	snap := testutil.Recv(t, r.snaps)
	e.watcher(t)

	assert.Len(t, snap, 1200, "objects of the abandoned pages are not counted twice")
	assert.EqualValues(t, 5, calls.Load(), "page 1, the expired continue, then all three pages again")
	assert.Empty(t, e.spy.lists[2].Continue, "the restart begins with no continue token")
	assert.Equal(t, "100", e.spy.watches[0].ResourceVersion)
}

func TestHub_TwoSessionsOfOneUser_ShareOneUpstream(t *testing.T) {
	e := newHubEnv(t)
	r1, r2 := newRec(), newRec()
	e.subscribe(t, sessA, "", r1)
	r1.next(t)
	e.watcher(t)
	e.subscribe(t, sessA2, "", r2)
	assert.Equal(t, "snapshot rv=100 [a b]", r2.next(t))

	lists, watches := e.spy.counts()
	assert.Equal(t, 1, lists)
	assert.Equal(t, 1, watches)
	assert.Equal(t, hubStats(1, 2, 2), e.hub.Stats())
}

func TestHub_OwnerSessionEnds_UpstreamRestartsUnderTheRemainingSessionWithAFreshSnapshot(t *testing.T) {
	e := newHubEnv(t)
	r1, r2 := newRec(), newRec()
	e.subscribe(t, sessA, "", r1)
	r1.next(t)
	w1 := e.watcher(t)
	e.subscribe(t, sessA2, "", r2)
	r2.next(t)

	require.NoError(t, e.closers.Close(sessA.Hash))

	assert.Equal(t, "snapshot rv=100 [a b]", r2.next(t), "the remaining subscriber gets a fresh snapshot")
	e.watcher(t)
	assert.True(t, w1.IsStopped(), "the watch of the ended session is released")
	seen := e.spy.sessionsSeen()
	assert.Equal(t, "sa", seen[0])
	assert.Equal(t, "sa2", seen[len(seen)-1], "the new list and watch use the remaining session's credentials")
	assert.Equal(t, hubStats(1, 1, 2), e.hub.Stats())
}

func TestHub_NonOwnerSessionEnds_UpstreamKeepsRunning(t *testing.T) {
	e := newHubEnv(t)
	r1, r2 := newRec(), newRec()
	e.subscribe(t, sessA, "", r1)
	r1.next(t)
	w := e.watcher(t)
	e.subscribe(t, sessA2, "", r2)
	r2.next(t)

	require.NoError(t, e.closers.Close(sessA2.Hash))

	w.Add(withRV(pod("c"), "101"))
	assert.Equal(t, "ADDED c", r1.next(t))
	assert.False(t, w.IsStopped())
	lists, _ := e.spy.counts()
	assert.Equal(t, 1, lists)
}

func kubeconfigKey(s session.Session) watch.Key {
	return watch.Key{UserID: s.UserID, Cluster: "k:mine/ctx", GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}}
}

func TestHub_KubeconfigCluster_ShareOnlyWithTheSameServerAndCredential(t *testing.T) {
	e := newHubEnv(t)
	cfgs := map[string]*rest.Config{
		"sa":  {Host: "https://one.example", BearerToken: "tok-1"},
		"sa2": {Host: "https://one.example", BearerToken: "tok-1"},
		"sa3": {Host: "https://one.example", BearerToken: "tok-OTHER"},
		"sa4": {Host: "https://two.example", BearerToken: "tok-1"},
	}
	e.spy.restFor = func(s session.Session) *rest.Config { return cfgs[s.Hash] }

	subscribe := func(s session.Session) *rec {
		r := newRec()
		_, err := e.hub.Subscribe(t.Context(), s, kubeconfigKey(s), r)
		require.NoError(t, err)
		r.next(t)
		return r
	}
	subscribe(sessA)
	subscribe(sessA2)
	assert.Equal(t, 1, e.hub.Stats().Watches, "same server and same credential share one upstream")

	subscribe(session.Session{Hash: "sa3", UserID: "u1"})
	assert.Equal(t, 2, e.hub.Stats().Watches, "a different credential under the same label stays apart")

	subscribe(session.Session{Hash: "sa4", UserID: "u1"})
	assert.Equal(t, 3, e.hub.Stats().Watches, "a different server stays apart")
}
