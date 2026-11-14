package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// fakeHandler is a hand-written Handler whose behaviour the test sets.
type fakeHandler struct {
	typ     string
	run     func(ctx context.Context, j *jobs.Job, report func(jobs.Progress)) error
	recover func(ctx context.Context, j *jobs.Job) (jobs.Status, string, []string)
	runs    atomic.Int32
	recovs  atomic.Int32
}

func (h *fakeHandler) Type() string { return h.typ }

func (h *fakeHandler) Run(ctx context.Context, j *jobs.Job, report func(jobs.Progress)) error {
	h.runs.Add(1)
	if h.run == nil {
		return nil
	}
	return h.run(ctx, j, report)
}

func (h *fakeHandler) Recover(ctx context.Context, j *jobs.Job) (jobs.Status, string, []string) {
	h.recovs.Add(1)
	if h.recover == nil {
		return jobs.Interrupted, "recovered", []string{"retry"}
	}
	return h.recover(ctx, j)
}

// fakeSessions is the set of session hashes that still exist.
type fakeSessions struct {
	mu   sync.Mutex
	live map[string]bool
}

func newSessions(hashes ...string) *fakeSessions {
	s := &fakeSessions{live: map[string]bool{}}
	for _, h := range hashes {
		s.live[h] = true
	}
	return s
}

func (s *fakeSessions) Exists(_ context.Context, hashes []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, h := range hashes {
		if s.live[h] {
			out = append(out, h)
		}
	}
	return out, nil
}

func (s *fakeSessions) end(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.live, hash)
}

// world is a database, a clock and sessions shared by one or more runners (replicas).
type world struct {
	db       *store.DB
	jobs     *store.Jobs
	clock    *clock.Fake
	sealer   *crypto.Sealer
	sessions *fakeSessions
	passes   chan string
	ids      atomic.Int32
}

func newWorld(t *testing.T) *world {
	t.Helper()
	sealer, err := crypto.NewSealer(bytes.Repeat([]byte{7}, 32), testutil.Rand("jobs"))
	require.NoError(t, err)
	db := storetest.New(t)
	return &world{db: db, jobs: store.NewJobs(db), clock: clock.NewFake(t0), sealer: sealer,
		sessions: newSessions("h1"), passes: make(chan string, 256)}
}

func (w *world) deps(replica string) jobs.Deps {
	return jobs.Deps{
		Store: w.jobs, Clock: w.clock, Sealer: w.sealer, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReplicaID: replica, NewID: func() string { return fmt.Sprintf("job-%d", w.ids.Add(1)) },
		Sessions: w.sessions, OnPass: func(p string) { w.passes <- p },
	}
}

func (w *world) runner(t *testing.T, replica string, hs ...*fakeHandler) *jobs.Runner {
	t.Helper()
	return w.runnerWith(t, w.deps(replica), hs...)
}

func (w *world) runnerWith(t *testing.T, d jobs.Deps, hs ...*fakeHandler) *jobs.Runner {
	t.Helper()
	r := jobs.NewRunner(d)
	for _, h := range hs {
		r.Register(h)
	}
	t.Cleanup(r.Close)
	return r
}

// start runs the renewal and recovery loops until the test ends and waits until both tickers exist.
func (w *world) start(t *testing.T, r *jobs.Runner, already int) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); r.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	w.clock.BlockUntil(already + 2)
}

// pass advances the clock and waits for n passes of the named kind.
func (w *world) pass(t *testing.T, kind string, d time.Duration, n int) {
	t.Helper()
	w.clock.Advance(d)
	for range n {
		w.recvPass(t, kind)
	}
}

// recvPass waits for one pass of the named kind and skips the others.
func (w *world) recvPass(t *testing.T, kind string) {
	t.Helper()
	for testutil.Recv(t, w.passes) != kind {
	}
}

var sess = struct{ hash, user string }{"h1", "u1"}

func (w *world) submit(t *testing.T, r *jobs.Runner, typ string, params any) string {
	t.Helper()
	id, err := r.Submit(t.Context(), session1(), "c1", typ, map[string]string{"name": "n"}, params)
	require.NoError(t, err)
	return id
}

func (w *world) row(t *testing.T, id string) store.JobRow {
	t.Helper()
	row, err := w.jobs.Get(t.Context(), id)
	require.NoError(t, err)
	return row
}

func progressOf(t *testing.T, row store.JobRow) jobs.Progress {
	t.Helper()
	var p jobs.Progress
	require.NoError(t, json.Unmarshal([]byte(row.Progress), &p))
	return p
}
