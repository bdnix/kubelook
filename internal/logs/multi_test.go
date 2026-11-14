package logs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/logs"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/ws"
)

// multiSource serves one reader per pod/container for live streams and a canned log for history requests.
type multiSource struct {
	mu      sync.Mutex
	readers map[string]*reader
	history map[string]string
	opens   []*corev1.PodLogOptions
	failFor map[string]error
}

func newMultiSource() *multiSource {
	return &multiSource{readers: map[string]*reader{}, history: map[string]string{}, failFor: map[string]error{}}
}

func (m *multiSource) reader(pod, container string) *reader {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := pod + "/" + container
	if m.readers[k] == nil {
		m.readers[k] = newReader()
	}
	return m.readers[k]
}

func (m *multiSource) Stream(_ context.Context, _ session.Session, _, _, pod string, o *corev1.PodLogOptions) (io.ReadCloser, error) {
	k := pod + "/" + o.Container
	m.mu.Lock()
	m.opens = append(m.opens, o.DeepCopy())
	err := m.failFor[k]
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !o.Follow {
		m.mu.Lock()
		defer m.mu.Unlock()
		return io.NopCloser(strings.NewReader(m.history[k])), nil
	}
	return m.reader(pod, o.Container), nil
}

func (m *multiSource) lastOpen() *corev1.PodLogOptions {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.opens[len(m.opens)-1]
}

func (m *multiSource) openCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.opens)
}

type fakeResolver struct {
	targets []logs.Target
	err     error
	got     logs.SourceSpec
	ns      string
}

func (f *fakeResolver) Resolve(_ context.Context, _ session.Session, _, ns string, spec logs.SourceSpec) ([]logs.Target, error) {
	f.got, f.ns = spec, ns
	return f.targets, f.err
}

type multiEnv struct {
	src      *multiSource
	res      *fakeResolver
	sink     *sink
	clock    *clock.Fake
	auditOut *bytes.Buffer
	deps     logs.Deps
	sess     session.Session
}

func newMultiEnv(t *testing.T, perSecond int, targets ...logs.Target) *multiEnv {
	t.Helper()
	e := &multiEnv{src: newMultiSource(), res: &fakeResolver{targets: targets}, sink: newSink(), clock: clock.NewFake(t0),
		auditOut: &bytes.Buffer{}, sess: session.Session{Hash: "sh", UserID: "u1"}}
	n := 0
	rec := audit.NewRecorder(storetest.New(t), e.clock, e.auditOut, func() string { n++; return fmt.Sprintf("a%d", n) })
	e.deps = logs.Deps{Source: e.src, Resolver: e.res, Clock: e.clock, Audit: rec, MaxLinesPerSecond: perSecond, HistoryMaxLines: 1000}
	return e
}

func (e *multiEnv) open(t *testing.T, req map[string]any) (ws.Channel, error) {
	t.Helper()
	raw, _ := json.Marshal(req)
	c, err := logs.Open(audit.WithIdentity(t.Context(), audit.Identity{AccountEmail: "ada@corp.example"}), e.deps, e.sink, e.sess, 7, raw)
	if err == nil {
		t.Cleanup(c.Close)
	}
	return c, err
}

func multiReq(sources map[string]any, extra map[string]any) map[string]any {
	req := map[string]any{"cluster": "c", "namespace": "ns", "follow": true, "sources": sources}
	for k, v := range extra {
		req[k] = v
	}
	return req
}

var (
	webApp  = logs.Target{Pod: "web-1", Container: "app"}
	webSide = logs.Target{Pod: "web-1", Container: "sidecar"}
	apiApp  = logs.Target{Pod: "api-1", Container: "app"}
)

func TestMulti_AllContainersOfAPod_OneStreamEach_LinesTaggedAndMergedInArrivalOrder(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp, webSide)
	_, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "allContainers": true}, nil))
	require.NoError(t, err)
	assert.Equal(t, logs.SourceSpec{Pod: "web-1", AllContainers: true}, e.res.got)
	assert.Equal(t, "ns", e.res.ns)
	assert.Equal(t, 2, e.src.openCount())

	e.src.reader("web-1", "app").feed(t, "2026-11-14T12:00:00Z from app\n")
	e.src.reader("web-1", "sidecar").feed(t, "2026-11-14T12:00:01Z from sidecar\n")
	e.src.reader("web-1", "app").feed(t, "2026-11-14T12:00:02Z app again\n")
	e.clock.Advance(100 * time.Millisecond)
	assert.Equal(t, []map[string]any{
		{"pod": "web-1", "container": "app", "ts": "2026-11-14T12:00:00Z", "text": "from app"},
		{"pod": "web-1", "container": "sidecar", "ts": "2026-11-14T12:00:01Z", "text": "from sidecar"},
		{"pod": "web-1", "container": "app", "ts": "2026-11-14T12:00:02Z", "text": "app again"},
	}, linesOf(e.sink.next(t)))
}

func TestMulti_SelectorAndWorkloadAreHandedToTheResolver_PodsAreFixedAtOpen(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp, apiApp)
	_, err := e.open(t, multiReq(map[string]any{"selector": "app=web"}, nil))
	require.NoError(t, err)
	assert.Equal(t, logs.SourceSpec{Selector: "app=web"}, e.res.got)
	assert.Equal(t, 2, e.src.openCount())

	e2 := newMultiEnv(t, 1000, webApp)
	_, err = e2.open(t, multiReq(map[string]any{"workload": map[string]any{"group": "apps", "kind": "Deployment", "name": "web"}}, nil))
	require.NoError(t, err)
	assert.Equal(t, logs.SourceSpec{Workload: &logs.WorkloadRef{Group: "apps", Kind: "Deployment", Name: "web"}}, e2.res.got)
}

func TestMulti_APodAndContainerNeedNoResolver(t *testing.T) {
	e := newMultiEnv(t, 1000)
	e.deps.Resolver = nil
	_, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "container": "app"}, nil))
	require.NoError(t, err)
	assert.Equal(t, 1, e.src.openCount())
	assert.Equal(t, "app", e.src.lastOpen().Container)
	e.src.reader("web-1", "app").feed(t, "2026-11-14T12:00:00Z x\n")
	e.clock.Advance(100 * time.Millisecond)
	assert.Equal(t, "web-1", linesOf(e.sink.next(t))[0]["pod"], "lines carry their source once sources are given")
}

func TestMulti_AtMostTwentyStreams(t *testing.T) {
	var many []logs.Target
	for i := range 21 {
		many = append(many, logs.Target{Pod: fmt.Sprintf("p%d", i), Container: "app"})
	}
	e := newMultiEnv(t, 1000, many...)
	_, err := e.open(t, multiReq(map[string]any{"selector": "app=web"}, nil))
	require.ErrorIs(t, err, apperr.ErrInvalid)
	assert.Contains(t, err.Error(), "20")
	assert.Equal(t, 0, e.src.openCount(), "nothing was opened")

	e = newMultiEnv(t, 1000, many[:20]...)
	_, err = e.open(t, multiReq(map[string]any{"selector": "app=web"}, nil))
	require.NoError(t, err)
	assert.Equal(t, 20, e.src.openCount())
}

func TestMulti_NothingMatches_IsAnError(t *testing.T) {
	e := newMultiEnv(t, 1000)
	_, err := e.open(t, multiReq(map[string]any{"selector": "app=none"}, nil))
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestMulti_ResolverErrorsAreReturned(t *testing.T) {
	e := newMultiEnv(t, 1000)
	e.res.err = fmt.Errorf("no such workload: %w", apperr.ErrNotFound)
	_, err := e.open(t, multiReq(map[string]any{"workload": map[string]any{"kind": "Deployment", "name": "x"}}, nil))
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestMulti_BadSources(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	for name, src := range map[string]map[string]any{
		"empty":                    {},
		"pod and selector":         {"pod": "p", "selector": "a=b"},
		"selector and workload":    {"selector": "a=b", "workload": map[string]any{"kind": "Deployment", "name": "x"}},
		"allContainers with other": {"selector": "a=b", "allContainers": true},
		"workload without name":    {"workload": map[string]any{"kind": "Deployment"}},
	} {
		_, err := e.open(t, multiReq(src, nil))
		require.ErrorIs(t, err, apperr.ErrInvalid, name)
	}
	assert.Equal(t, 0, e.src.openCount())
}

func TestMulti_OneSourceEnding_OthersContinue_ChannelClosesWhenAllEnded(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp, apiApp)
	_, err := e.open(t, multiReq(map[string]any{"selector": "app=web"}, nil))
	require.NoError(t, err)
	e.src.reader("web-1", "app").eof()
	f := e.sink.next(t)
	assert.Equal(t, "eof", f.Typ)
	assert.Equal(t, "web-1", f.D["pod"])
	assert.Equal(t, "app", f.D["container"])
	select {
	case r := <-e.sink.closed:
		t.Fatalf("the channel stays open while another source runs, closed: %s", r)
	default:
	}
	e.src.reader("api-1", "app").feed(t, "2026-11-14T12:00:00Z still here\n")
	e.clock.Advance(100 * time.Millisecond)
	assert.Equal(t, "still here", linesOf(e.sink.next(t))[0]["text"])
	e.src.reader("api-1", "app").eof()
	assert.Equal(t, "eof", e.sink.next(t).Typ)
	assert.Equal(t, "container_terminated", testutil.Recv(t, e.sink.closed))
}

func TestMulti_ASourceThatCannotBeOpened_IsReported_TheOthersRun(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp, webSide)
	e.src.failFor["web-1/sidecar"] = errors.New("container sidecar is waiting to start")
	_, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "allContainers": true}, nil))
	require.NoError(t, err)
	f := e.sink.next(t)
	assert.Equal(t, "source_error", f.Typ)
	assert.Equal(t, "sidecar", f.D["container"])
	assert.Contains(t, f.D["message"], "waiting to start")
	e.src.reader("web-1", "app").feed(t, "2026-11-14T12:00:00Z ok\n")
	e.clock.Advance(100 * time.Millisecond)
	assert.Equal(t, "ok", linesOf(e.sink.next(t))[0]["text"])
}

func TestMulti_EverySourceFailing_FailsTheOpen(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp, webSide)
	e.src.failFor["web-1/app"] = errors.New("boom")
	e.src.failFor["web-1/sidecar"] = errors.New("boom")
	_, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "allContainers": true}, nil))
	require.Error(t, err)
	assert.Contains(t, e.auditOut.String(), `"verb":"log-view-close"`)
}

func TestMulti_AuditIsOneOpenAndOneCloseRecord_WithTheNumberOfStreams(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp, webSide)
	ch, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "allContainers": true}, nil))
	require.NoError(t, err)
	e.src.reader("web-1", "app").feed(t, "2026-11-14T12:00:00Z super-secret\n")
	ch.Close()
	out := e.auditOut.String()
	assert.Equal(t, 1, strings.Count(out, `"verb":"log-view-open"`))
	assert.Equal(t, 1, strings.Count(out, `"verb":"log-view-close"`))
	assert.Contains(t, out, `"streams":2`)
	assert.NotContains(t, out, "super-secret")
}

func TestMulti_ClosingTheChannelClosesEveryUpstreamStream(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp, webSide)
	ch, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "allContainers": true}, nil))
	require.NoError(t, err)
	e.src.reader("web-1", "app")
	e.src.reader("web-1", "sidecar")
	ch.Close()
	for _, c := range []string{"app", "sidecar"} {
		select {
		case <-e.src.reader("web-1", c).closed:
		default:
			t.Fatalf("stream %s was not closed", c)
		}
	}
}

func TestFilter_Text_OnlyMatchingLinesAreSent_FilteredLinesAreNotDropped(t *testing.T) {
	e := newMultiEnv(t, 2, webApp) // two lines per second: filtered lines must not use the budget
	_, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "container": "app"}, map[string]any{"filter": map[string]any{"text": "ERROR"}}))
	require.NoError(t, err)
	var feed strings.Builder
	for i := range 20 {
		fmt.Fprintf(&feed, "2026-11-14T12:00:00Z info line %d\n", i)
	}
	feed.WriteString("2026-11-14T12:00:01Z an ERROR happened\n")
	feed.WriteString("2026-11-14T12:00:02Z another error here\n")
	e.src.reader("web-1", "app").feed(t, feed.String())
	e.clock.Advance(100 * time.Millisecond)
	f := e.sink.next(t)
	require.Equal(t, "lines", f.Typ)
	got := linesOf(f)
	require.Len(t, got, 2)
	assert.Equal(t, "an ERROR happened", got[0]["text"])
	assert.Equal(t, "another error here", got[1]["text"], "the text filter ignores case")
	e.clock.Advance(100 * time.Millisecond)
	select {
	case d := <-e.sink.frames:
		t.Fatalf("no dropped report expected, got %s %v", d.Typ, d.D)
	default:
	}
}

func TestFilter_Regex_UsesRE2(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	_, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "container": "app"}, map[string]any{"filter": map[string]any{"regex": `status=(5\d\d)`}}))
	require.NoError(t, err)
	e.src.reader("web-1", "app").feed(t, "2026-11-14T12:00:00Z status=200\n2026-11-14T12:00:01Z status=503 upstream\n")
	e.clock.Advance(100 * time.Millisecond)
	got := linesOf(e.sink.next(t))
	require.Len(t, got, 1)
	assert.Equal(t, "status=503 upstream", got[0]["text"])
}

func TestFilter_Invalid(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	src := map[string]any{"pod": "web-1", "container": "app"}
	for name, f := range map[string]map[string]any{
		"bad regex":          {"regex": "("},
		"backreference":      {"regex": `(a)\1`}, // not RE2
		"too long":           {"regex": strings.Repeat("a", 257)},
		"text too long":      {"text": strings.Repeat("a", 257)},
		"text and regex":     {"text": "a", "regex": "b"},
		"neither":            {},
		"unknown field only": {"glob": "*"},
	} {
		_, err := e.open(t, multiReq(src, map[string]any{"filter": f}))
		require.ErrorIs(t, err, apperr.ErrInvalid, name)
	}
	assert.Equal(t, 0, e.src.openCount())
	_, err := e.open(t, multiReq(src, map[string]any{"filter": map[string]any{"regex": strings.Repeat("a", 256)}}))
	require.NoError(t, err, "256 characters are allowed")
}

func openHistory(t *testing.T, e *multiEnv, targets ...logs.Target) ws.Channel {
	t.Helper()
	ch, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "container": "app"}, map[string]any{"tailLines": 3}))
	require.NoError(t, err)
	e.src.reader("web-1", "app").feed(t, "2026-11-14T12:00:02Z l2\n2026-11-14T12:00:03Z l3\n2026-11-14T12:00:04Z l4\n")
	e.clock.Advance(100 * time.Millisecond)
	assert.Len(t, linesOf(e.sink.next(t)), 3)
	return ch
}

const fullLog = "2026-11-14T12:00:00Z l0\n2026-11-14T12:00:01Z l1\n2026-11-14T12:00:02Z l2\n2026-11-14T12:00:03Z l3\n2026-11-14T12:00:04Z l4\n"

func history(t *testing.T, ch ws.Channel, before string, n int) error {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"source": map[string]any{"pod": "web-1", "container": "app"}, "before": before, "lines": n})
	return ch.Handle(t.Context(), "logs.history", raw)
}

func TestHistory_RequestsTailOfLoadedPlusN_ReturnsOnlyOlderLines(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	ch := openHistory(t, e)
	e.src.history["web-1/app"] = fullLog

	require.NoError(t, history(t, ch, "2026-11-14T12:00:02Z", 2))
	o := e.src.lastOpen()
	assert.False(t, o.Follow)
	assert.True(t, o.Timestamps)
	require.NotNil(t, o.TailLines)
	assert.EqualValues(t, 5, *o.TailLines, "three lines are loaded, two more are wanted")
	f := e.sink.next(t)
	assert.Equal(t, "history", f.Typ)
	assert.Equal(t, "web-1", f.D["pod"])
	assert.Equal(t, "app", f.D["container"])
	got := linesOf(f)
	require.Len(t, got, 2)
	assert.Equal(t, "l0", got[0]["text"])
	assert.Equal(t, "l1", got[1]["text"], "oldest first")
}

func TestHistory_TheStartOfTheLogEndsIt(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	ch := openHistory(t, e)
	e.src.history["web-1/app"] = fullLog
	require.NoError(t, history(t, ch, "2026-11-14T12:00:02Z", 2))
	e.sink.next(t)
	// five lines are loaded now; the log has only five
	require.NoError(t, history(t, ch, "2026-11-14T12:00:00Z", 2))
	f := e.sink.next(t)
	assert.Equal(t, "historyEnd", f.Typ)
	assert.Equal(t, "start", f.D["reason"])
	assert.EqualValues(t, 7, *e.src.lastOpen().TailLines)
}

func TestHistory_ShortAnswerEndsItAfterTheLines(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	ch := openHistory(t, e)
	e.src.history["web-1/app"] = "2026-11-14T12:00:01Z l1\n2026-11-14T12:00:02Z l2\n2026-11-14T12:00:03Z l3\n2026-11-14T12:00:04Z l4\n"
	require.NoError(t, history(t, ch, "2026-11-14T12:00:02Z", 5))
	f := e.sink.next(t)
	assert.Equal(t, "history", f.Typ)
	assert.Len(t, linesOf(f), 1)
	end := e.sink.next(t)
	assert.Equal(t, "historyEnd", end.Typ)
	assert.Equal(t, "start", end.D["reason"])
}

func TestHistory_TotalLoadedIsCapped(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	e.deps.HistoryMaxLines = 5
	ch := openHistory(t, e)
	e.src.history["web-1/app"] = fullLog
	require.NoError(t, history(t, ch, "2026-11-14T12:00:02Z", 10))
	assert.EqualValues(t, 5, *e.src.lastOpen().TailLines, "asks for no more than the cap")
	f := e.sink.next(t)
	assert.Equal(t, "history", f.Typ)
	assert.Len(t, linesOf(f), 2)
	end := e.sink.next(t)
	assert.Equal(t, "historyEnd", end.Typ)
	assert.Equal(t, "cap", end.D["reason"])

	opens := e.src.openCount()
	require.NoError(t, history(t, ch, "2026-11-14T12:00:00Z", 10))
	assert.Equal(t, "cap", e.sink.next(t).D["reason"])
	assert.Equal(t, opens, e.src.openCount(), "nothing more is requested once the cap is reached")
}

func TestHistory_AppliesTheServerFilter(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	ch, err := e.open(t, multiReq(map[string]any{"pod": "web-1", "container": "app"}, map[string]any{"filter": map[string]any{"text": "keep"}}))
	require.NoError(t, err)
	e.src.history["web-1/app"] = "2026-11-14T12:00:00Z keep a\n2026-11-14T12:00:01Z drop b\n2026-11-14T12:00:02Z keep c\n"
	require.NoError(t, history(t, ch, "2026-11-14T12:00:03Z", 3))
	got := linesOf(e.sink.next(t))
	require.Len(t, got, 2)
	assert.Equal(t, "keep a", got[0]["text"])
	assert.Equal(t, "keep c", got[1]["text"])
}

func TestHistory_BadRequests(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	ch := openHistory(t, e)
	for name, d := range map[string]map[string]any{
		"unknown source": {"source": map[string]any{"pod": "other", "container": "app"}, "before": "2026-11-14T12:00:02Z", "lines": 5},
		"bad time":       {"source": map[string]any{"pod": "web-1", "container": "app"}, "before": "yesterday", "lines": 5},
		"zero lines":     {"source": map[string]any{"pod": "web-1", "container": "app"}, "before": "2026-11-14T12:00:02Z", "lines": 0},
		"too many lines": {"source": map[string]any{"pod": "web-1", "container": "app"}, "before": "2026-11-14T12:00:02Z", "lines": 100001},
	} {
		raw, _ := json.Marshal(d)
		require.ErrorIs(t, ch.Handle(t.Context(), "logs.history", raw), apperr.ErrInvalid, name)
	}
	require.ErrorIs(t, ch.Handle(t.Context(), "logs.history", json.RawMessage(`"x"`)), apperr.ErrInvalid)
	require.ErrorIs(t, ch.Handle(t.Context(), "bogus", nil), apperr.ErrInvalid)
}

func TestHistory_ASourceThatFails_ReturnsTheError(t *testing.T) {
	e := newMultiEnv(t, 1000, webApp)
	ch := openHistory(t, e)
	e.src.failFor["web-1/app"] = errors.New("pod is gone")
	require.Error(t, history(t, ch, "2026-11-14T12:00:02Z", 2))
}
