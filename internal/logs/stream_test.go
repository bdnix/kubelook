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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/logs"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

type frame struct {
	Typ string
	D   map[string]any
}

// sink records the frames of a stream.
type sink struct {
	frames  chan frame
	backlog atomic.Int32
	closed  chan string
}

func newSink() *sink { return &sink{frames: make(chan frame, 256), closed: make(chan string, 4)} }

func (s *sink) Send(_ int, typ string, d any) error {
	b, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.frames <- frame{typ, m}
	return nil
}
func (s *sink) Backlog() int                      { return int(s.backlog.Load()) }
func (s *sink) CloseChannel(_ int, reason string) { s.closed <- reason }

func (s *sink) next(t *testing.T) frame { t.Helper(); return testutil.Recv(t, s.frames) }

func linesOf(f frame) []map[string]any {
	var out []map[string]any
	for _, l := range f.D["lines"].([]any) {
		out = append(out, l.(map[string]any))
	}
	return out
}

// reader is a log stream the test feeds chunk by chunk. A signal on reading means every byte
// fed so far has been consumed and parsed: the reader is blocked in its next Read.
type reader struct {
	rest    []byte
	chunks  chan []byte
	reading chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func newReader() *reader {
	return &reader{chunks: make(chan []byte), reading: make(chan struct{}, 64), closed: make(chan struct{})}
}

func (r *reader) Read(p []byte) (int, error) {
	if len(r.rest) > 0 { // a chunk larger than the caller's buffer is served in pieces
		n := copy(p, r.rest)
		r.rest = r.rest[n:]
		return n, nil
	}
	r.reading <- struct{}{}
	select {
	case c, ok := <-r.chunks:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, c)
		r.rest = c[n:]
		return n, nil
	case <-r.closed:
		return 0, io.ErrClosedPipe
	}
}

func (r *reader) Close() error { r.once.Do(func() { close(r.closed) }); return nil }

// feed hands a chunk over and waits until it has been consumed.
func (r *reader) feed(t *testing.T, chunk string) {
	t.Helper()
	require.NoError(t, sendChunk(r, []byte(chunk)))
	testutil.Recv(t, r.reading)
}

func sendChunk(r *reader, b []byte) error {
	select {
	case r.chunks <- b:
		return nil
	case <-r.closed:
		return errors.New("the log reader was closed")
	}
}

func (r *reader) eof() { close(r.chunks) }

type source struct {
	rd   *reader
	err  error
	opts *corev1.PodLogOptions
	n    atomic.Int32
}

func (s *source) Stream(_ context.Context, _ session.Session, _, _, _ string, o *corev1.PodLogOptions) (io.ReadCloser, error) {
	s.n.Add(1)
	s.opts = o
	if s.err != nil {
		return nil, s.err
	}
	return s.rd, nil
}

type env struct {
	src      *source
	sink     *sink
	clock    *clock.Fake
	auditOut *bytes.Buffer
	deps     logs.Deps
	kept     atomic.Int32
	sess     session.Session
}

func newEnv(t *testing.T, perSecond int) *env {
	t.Helper()
	e := &env{src: &source{rd: newReader()}, sink: newSink(), clock: clock.NewFake(t0), auditOut: &bytes.Buffer{}, sess: session.Session{Hash: "sh", UserID: "u1"}}
	db := storetest.New(t)
	n := 0
	rec := audit.NewRecorder(db, e.clock, e.auditOut, func() string { n++; return fmt.Sprintf("a%d", n) })
	e.deps = logs.Deps{
		Source: e.src, Clock: e.clock, Audit: rec, MaxLinesPerSecond: perSecond,
		KeepAlive: func(context.Context, string) { e.kept.Add(1) },
	}
	return e
}

func (e *env) open(t *testing.T, req map[string]any) (ch interface{ Close() }, err error) {
	t.Helper()
	raw, _ := json.Marshal(req)
	c, err := logs.Open(audit.WithIdentity(t.Context(), audit.Identity{AccountEmail: "ada@corp.example"}), e.deps, e.sink, e.sess, 7, raw)
	if err == nil {
		t.Cleanup(c.Close)
		testutil.Recv(t, e.src.rd.reading) // the reader is waiting for the first bytes
	}
	return c, err
}

var baseReq = map[string]any{"cluster": "c", "namespace": "ns", "pod": "web-1", "container": "app", "follow": true}

func TestStream_LinesAreSplitIntoTimestampAndTextAndFlushedEvery100ms(t *testing.T) {
	e := newEnv(t, 1000)
	_, err := e.open(t, baseReq)
	require.NoError(t, err)
	e.src.rd.feed(t, "2026-11-14T12:00:00.000000001Z hello world\n2026-11-14T12:00:01Z second\n")
	e.clock.Advance(100 * time.Millisecond)
	got := linesOf(e.sink.next(t))
	assert.Equal(t, []map[string]any{
		{"ts": "2026-11-14T12:00:00.000000001Z", "text": "hello world"},
		{"ts": "2026-11-14T12:00:01Z", "text": "second"},
	}, got)
	assert.EqualValues(t, 1, e.kept.Load(), "every tick keeps the session alive")
	e.clock.Advance(100 * time.Millisecond)
	e.clock.Advance(100 * time.Millisecond)
	e.src.rd.feed(t, "2026-11-14T12:00:02Z third\n")
	e.clock.Advance(100 * time.Millisecond)
	assert.Equal(t, "third", linesOf(e.sink.next(t))[0]["text"])
	assert.GreaterOrEqual(t, e.kept.Load(), int32(3))
}

func TestStream_FiveHundredPendingLinesFlushWithoutWaitingForTheTick(t *testing.T) {
	e := newEnv(t, 100000)
	_, err := e.open(t, baseReq)
	require.NoError(t, err)
	e.src.rd.feed(t, strings.Repeat("2026-11-14T12:00:00Z x\n", 500))
	assert.Len(t, linesOf(e.sink.next(t)), 500)
}

func TestStream_RateCap_ExtraLinesAreDroppedAndReported(t *testing.T) {
	e := newEnv(t, 1000)
	_, err := e.open(t, baseReq)
	require.NoError(t, err)
	e.src.rd.feed(t, strings.Repeat("2026-11-14T12:00:00Z x\n", 3000))
	assert.Len(t, linesOf(e.sink.next(t)), 500)
	assert.Len(t, linesOf(e.sink.next(t)), 500)
	e.clock.Advance(100 * time.Millisecond)
	f := e.sink.next(t)
	assert.Equal(t, "dropped", f.Typ)
	assert.EqualValues(t, 2000, f.D["count"])

	e.clock.Advance(time.Second) // the bucket is full again
	e.src.rd.feed(t, strings.Repeat("2026-11-14T12:00:09Z y\n", 5))
	e.clock.Advance(100 * time.Millisecond)
	got := 0
	for got < 5 { // leftover ticks of the earlier Advance may split the delivery
		got += len(linesOf(e.sink.next(t)))
	}
	assert.Equal(t, 5, got)
}

func TestStream_SlowBrowser_BufferHoldsTwoThousandLinesAndDropsTheOldest(t *testing.T) {
	e := newEnv(t, 100000)
	e.sink.backlog.Store(1000) // the outbound queue is busy
	_, err := e.open(t, baseReq)
	require.NoError(t, err)
	var chunk strings.Builder
	for i := range 2500 {
		fmt.Fprintf(&chunk, "2026-11-14T12:00:00Z line%04d\n", i)
	}
	e.src.rd.feed(t, chunk.String())
	select {
	case f := <-e.sink.frames:
		t.Fatalf("nothing is sent while the queue is busy, got %s", f.Typ)
	default:
	}

	e.sink.backlog.Store(0)
	e.clock.Advance(100 * time.Millisecond)
	var first, last string
	total := 0
	for total < 2000 {
		ls := linesOf(e.sink.next(t))
		if total == 0 {
			first = ls[0]["text"].(string)
		}
		last = ls[len(ls)-1]["text"].(string)
		total += len(ls)
	}
	assert.Equal(t, "line0500", first, "the oldest 500 were dropped")
	assert.Equal(t, "line2499", last)
	f := e.sink.next(t)
	assert.Equal(t, "dropped", f.Typ)
	assert.EqualValues(t, 500, f.D["count"])
}

func TestStream_LongLineIsTruncatedAndNonUTF8Replaced(t *testing.T) {
	e := newEnv(t, 1000)
	_, err := e.open(t, baseReq)
	require.NoError(t, err)
	long := "2026-11-14T12:00:00Z " + strings.Repeat("a", 70<<10) + "\n"
	e.src.rd.feed(t, long)
	e.src.rd.feed(t, "2026-11-14T12:00:01Z bad\xff\xfebytes\n")
	e.src.rd.feed(t, "no-space-line\n")
	e.clock.Advance(100 * time.Millisecond)
	ls := linesOf(e.sink.next(t))
	require.Len(t, ls, 3)
	text := ls[0]["text"].(string)
	assert.True(t, strings.HasSuffix(text, "… [truncated]"))
	assert.Less(t, len(text), 65<<10)
	assert.Equal(t, "bad\uFFFDbytes", ls[1]["text"], "a run of invalid bytes becomes one replacement character")
	assert.Equal(t, "", ls[2]["ts"])
	assert.Equal(t, "no-space-line", ls[2]["text"])
}

func TestStream_EOF_NotFollowing_SendsEofOnly(t *testing.T) {
	e := newEnv(t, 1000)
	req := map[string]any{"cluster": "c", "namespace": "ns", "pod": "web-1", "follow": false}
	_, err := e.open(t, req)
	require.NoError(t, err)
	e.src.rd.feed(t, "2026-11-14T12:00:00Z last\n")
	e.src.rd.eof()
	assert.Equal(t, "last", linesOf(e.sink.next(t))[0]["text"], "pending lines are flushed first")
	assert.Equal(t, "eof", e.sink.next(t).Typ)
	select {
	case r := <-e.sink.closed:
		t.Fatalf("the channel stays open, closed: %s", r)
	default:
	}
}

func TestStream_EOF_Following_SendsEofThenCloses(t *testing.T) {
	e := newEnv(t, 1000)
	_, err := e.open(t, baseReq)
	require.NoError(t, err)
	e.src.rd.eof()
	assert.Equal(t, "eof", e.sink.next(t).Typ)
	assert.Equal(t, "container_terminated", testutil.Recv(t, e.sink.closed))
}

func TestStream_ReadErrorSendsAnErrorFrameAndCloses(t *testing.T) {
	e := newEnv(t, 1000)
	e.src.rd = newReader()
	_, err := e.open(t, baseReq)
	require.NoError(t, err)
	// make the next Read fail: close the reader without Close being called by the stream
	_ = e.src.rd.Close()
	f := e.sink.next(t)
	assert.Equal(t, "error", f.Typ)
	assert.Equal(t, "internal", f.D["code"])
	assert.Equal(t, "error", testutil.Recv(t, e.sink.closed))
}

func TestOpen_OptionsMapToPodLogOptionsExactly(t *testing.T) {
	e := newEnv(t, 1000)
	_, err := e.open(t, map[string]any{
		"cluster": "c", "namespace": "ns", "pod": "web-1", "container": "sidecar", "follow": true, "previous": true,
		"tailLines": 200, "sinceSeconds": 60,
	})
	require.NoError(t, err)
	o := e.src.opts
	assert.Equal(t, "sidecar", o.Container)
	assert.True(t, o.Follow)
	assert.True(t, o.Previous)
	assert.True(t, o.Timestamps)
	require.NotNil(t, o.TailLines)
	assert.EqualValues(t, 200, *o.TailLines)
	require.NotNil(t, o.SinceSeconds)
	assert.EqualValues(t, 60, *o.SinceSeconds)
	assert.Nil(t, o.SinceTime)
}

func TestOpen_SinceTimeAndDefaults(t *testing.T) {
	e := newEnv(t, 1000)
	_, err := e.open(t, map[string]any{"cluster": "c", "namespace": "ns", "pod": "p", "sinceTime": "2026-11-14T11:00:00Z"})
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC), e.src.opts.SinceTime.UTC())
	assert.Nil(t, e.src.opts.TailLines)
	assert.Nil(t, e.src.opts.SinceSeconds)
	assert.False(t, e.src.opts.Follow)
	assert.True(t, e.src.opts.Timestamps)
}

func TestOpen_BadRequests(t *testing.T) {
	e := newEnv(t, 1000)
	for name, req := range map[string]map[string]any{
		"no pod":        {"cluster": "c", "namespace": "ns"},
		"no namespace":  {"cluster": "c", "pod": "p"},
		"negative tail": {"cluster": "c", "namespace": "ns", "pod": "p", "tailLines": -1},
		"zero since":    {"cluster": "c", "namespace": "ns", "pod": "p", "sinceSeconds": 0},
		"bad time":      {"cluster": "c", "namespace": "ns", "pod": "p", "sinceTime": "yesterday"},
	} {
		_, err := e.open(t, req)
		require.ErrorIs(t, err, apperr.ErrInvalid, name)
	}
	_, err := logs.Open(t.Context(), e.deps, e.sink, e.sess, 1, json.RawMessage(`"x"`))
	require.ErrorIs(t, err, apperr.ErrInvalid)
	assert.EqualValues(t, 0, e.src.n.Load(), "nothing was opened")
}

func TestOpen_AuditRecordsOpenAndCloseWithoutLogText(t *testing.T) {
	e := newEnv(t, 1000)
	ch, err := e.open(t, map[string]any{
		"cluster": "c", "namespace": "ns", "pod": "web-1", "container": "app", "follow": true, "tailLines": 100,
	})
	require.NoError(t, err)
	e.src.rd.feed(t, "2026-11-14T12:00:00Z super-secret-log-text\n")
	e.clock.Advance(100 * time.Millisecond)
	e.sink.next(t)
	ch.Close()
	ch.Close() // closing twice records once

	out := e.auditOut.String()
	assert.Equal(t, 1, strings.Count(out, `"verb":"log-view-open"`))
	assert.Equal(t, 1, strings.Count(out, `"verb":"log-view-close"`))
	assert.Contains(t, out, `"cluster":"c"`)
	assert.Contains(t, out, `"namespace":"ns"`)
	assert.Contains(t, out, `"name":"web-1"`)
	assert.Contains(t, out, `"container":"app"`)
	assert.Contains(t, out, `"tailLines":100`)
	assert.Contains(t, out, "ada@corp.example")
	assert.NotContains(t, out, "super-secret-log-text")
}

func TestOpen_ClosingTheChannelClosesTheUpstreamStream(t *testing.T) {
	e := newEnv(t, 1000)
	ch, err := e.open(t, baseReq)
	require.NoError(t, err)
	ch.Close()
	select {
	case <-e.src.rd.closed:
	default:
		t.Fatal("the upstream stream was not closed")
	}
}

func TestOpen_AuditFailure_RefusesTheStream(t *testing.T) {
	db := storetest.New(t)
	n := 0
	e := newEnv(t, 1000)
	e.deps.Audit = audit.NewRecorder(db, e.clock, e.auditOut, func() string { n++; return fmt.Sprintf("a%d", n) })
	require.NoError(t, db.Close())
	_, err := e.open(t, baseReq)
	var api *apperr.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, "server_busy", api.Code)
	assert.EqualValues(t, 0, e.src.n.Load(), "no record, no stream")
}

func TestOpen_APIErrorsAreReturnedWithTheirMessage(t *testing.T) {
	e := newEnv(t, 1000)
	e.src.err = apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "web-1")
	_, err := e.open(t, baseReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "web-1")
	out := e.auditOut.String()
	assert.Contains(t, out, `"verb":"log-view-close"`, "a failed open is closed in the audit trail")
	assert.Contains(t, out, `"outcome":"failure"`)
}

func TestNewKubeSource_ReadsThroughTheTypedClient(t *testing.T) {
	fk := newFakeClients()
	rc, err := logs.NewKubeSource(fk).Stream(t.Context(), session.Session{}, "c", "ns", "web-1", &corev1.PodLogOptions{})
	require.NoError(t, err)
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "fake logs", string(b))
	require.NoError(t, rc.Close())

	fk.Known = map[string]bool{}
	_, err = logs.NewKubeSource(fk).Stream(t.Context(), session.Session{}, "c", "ns", "web-1", &corev1.PodLogOptions{})
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}
