package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
)

// orphan inserts a row left behind by a replica that died.
func (w *world) orphan(t *testing.T, id, status, session string) {
	t.Helper()
	sealed, err := w.sealer.Seal([]byte(`{"p":1}`), []byte("job|"+id))
	require.NoError(t, err)
	require.NoError(t, w.jobs.Insert(t.Context(), store.JobRow{
		ID: id, UserID: "u1", SessionHash: session, Cluster: "c1", Type: "bulk", Target: `{"name":"n"}`, SealedParams: sealed,
		Status: status, Progress: "{}", Offers: "[]", LeaseOwner: "dead", LeaseExpiresAt: t0.Add(5 * time.Second),
		CreatedAt: t0, UpdatedAt: t0,
	}))
}

func TestRecovery_RunningJobOfADeadReplica_IsClassifiedByTheHandler(t *testing.T) {
	w := newWorld(t)
	h := &fakeHandler{typ: "bulk", recover: func(_ context.Context, j *jobs.Job) (jobs.Status, string, []string) {
		assert.Equal(t, "orphan", j.ID)
		assert.JSONEq(t, `{"p":1}`, string(j.Params))
		return jobs.Interrupted, "Drain stopped part-way.", []string{"retry"}
	}}
	r := w.runner(t, "r2", h)
	w.orphan(t, "orphan", store.JobRunning, "h1")
	w.start(t, r, 0)

	w.pass(t, "scan", 15*time.Second, 1)
	got := w.row(t, "orphan")
	assert.Equal(t, store.JobInterrupted, got.Status)
	assert.Equal(t, "Drain stopped part-way.", got.Message)
	assert.Equal(t, `["retry"]`, got.Offers)
	assert.EqualValues(t, 1, h.recovs.Load())
	assert.EqualValues(t, 0, h.runs.Load(), "interrupted jobs are never re-run")
}

func TestRecovery_HandlerMaySayTheJobSucceeded(t *testing.T) {
	w := newWorld(t)
	h := &fakeHandler{typ: "bulk", recover: func(context.Context, *jobs.Job) (jobs.Status, string, []string) {
		return jobs.Succeeded, "All pods were evicted.", nil
	}}
	r := w.runner(t, "r2", h)
	w.orphan(t, "orphan", store.JobRunning, "h1")
	w.start(t, r, 0)
	w.pass(t, "scan", 15*time.Second, 1)
	got := w.row(t, "orphan")
	assert.Equal(t, store.JobSucceeded, got.Status)
	assert.Equal(t, "[]", got.Offers)
}

func TestRecovery_QueuedJobWithExpiredLease_IsInterrupted(t *testing.T) {
	w := newWorld(t)
	h := &fakeHandler{typ: "bulk"}
	r := w.runner(t, "r2", h)
	w.orphan(t, "orphan", store.JobQueued, "h1")
	w.start(t, r, 0)
	w.pass(t, "scan", 15*time.Second, 1)
	got := w.row(t, "orphan")
	assert.Equal(t, store.JobInterrupted, got.Status)
	assert.Equal(t, "The server stopped before this job started.", got.Message)
	assert.Equal(t, `["retry"]`, got.Offers)
	assert.EqualValues(t, 0, h.recovs.Load()+h.runs.Load())
}

func TestRecovery_SessionGone_IsInterruptedWithoutAskingTheHandler(t *testing.T) {
	w := newWorld(t)
	h := &fakeHandler{typ: "bulk"}
	r := w.runner(t, "r2", h)
	w.orphan(t, "orphan", store.JobRunning, "gone")
	w.start(t, r, 0)
	w.pass(t, "scan", 15*time.Second, 1)
	got := w.row(t, "orphan")
	assert.Equal(t, store.JobInterrupted, got.Status)
	assert.Equal(t, "The job's session ended, so its result could not be checked.", got.Message)
	assert.EqualValues(t, 0, h.recovs.Load())
}

func TestRecovery_UnknownType_IsInterrupted(t *testing.T) {
	w := newWorld(t)
	r := w.runner(t, "r2")
	w.orphan(t, "orphan", store.JobRunning, "h1")
	w.start(t, r, 0)
	w.pass(t, "scan", 15*time.Second, 1)
	assert.Equal(t, store.JobInterrupted, w.row(t, "orphan").Status)
}

func TestRecovery_LiveJobsAndFreshLeasesAreLeftAlone(t *testing.T) {
	w := newWorld(t)
	h, started, release := blocking("bulk")
	r := w.runner(t, "r1", h)
	w.start(t, r, 0)
	id := w.submit(t, r, "bulk", nil)
	testutil.Recv(t, started)
	w.pass(t, "scan", 15*time.Second, 1)
	assert.Equal(t, store.JobRunning, w.row(t, id).Status)
	assert.EqualValues(t, 0, h.recovs.Load())
	close(release)
	r.Wait()
}

func TestRecovery_ARunnerThatDiedDuringARun_IsClassifiedByAnotherAndNeverRerun(t *testing.T) {
	w := newWorld(t)
	h1, started, _ := blocking("bulk")
	a := w.runner(t, "r1", h1)
	id := w.submit(t, a, "bulk", nil)
	testutil.Recv(t, started)
	a.Close() // the replica goes away; the row stays running with a lease that will expire

	h2 := &fakeHandler{typ: "bulk"}
	b := w.runner(t, "r2", h2)
	w.start(t, b, 0)
	w.pass(t, "scan", 15*time.Second, 1) // lease (30 s) has not expired yet
	assert.Equal(t, store.JobRunning, w.row(t, id).Status)
	w.pass(t, "scan", 15*time.Second, 1)
	w.pass(t, "scan", 15*time.Second, 1)
	assert.Equal(t, store.JobInterrupted, w.row(t, id).Status)
	assert.EqualValues(t, 1, h1.runs.Load())
	assert.EqualValues(t, 0, h2.runs.Load())
	assert.EqualValues(t, 1, h2.recovs.Load())
}

func TestRecovery_TwoReplicasScanningAtOnce_ClassifyOnce(t *testing.T) {
	w := newWorld(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	mk := func() *fakeHandler {
		return &fakeHandler{typ: "bulk", recover: func(context.Context, *jobs.Job) (jobs.Status, string, []string) {
			entered <- struct{}{}
			<-release
			return jobs.Interrupted, "classified", nil
		}}
	}
	h1, h2 := mk(), mk()
	a, b := w.runner(t, "r1", h1), w.runner(t, "r2", h2)
	w.orphan(t, "orphan", store.JobRunning, "h1")
	w.start(t, a, 0)
	w.start(t, b, 2)

	w.clock.Advance(15 * time.Second)
	testutil.Recv(t, entered) // one replica won the claim and is classifying
	// the other finishes its pass without classifying
	w.recvPass(t, "scan")
	close(release)
	w.recvPass(t, "scan")
	assert.EqualValues(t, 1, h1.recovs.Load()+h2.recovs.Load())
	assert.Equal(t, "classified", w.row(t, "orphan").Message)
}
