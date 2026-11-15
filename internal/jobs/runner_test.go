package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
)

func session1() session.Session { return session.Session{Hash: sess.hash, UserID: sess.user} }

// blocking returns a handler that reports it started and then waits to be released.
func blocking(typ string) (h *fakeHandler, started chan string, release chan struct{}) {
	started, release = make(chan string, 16), make(chan struct{})
	h = &fakeHandler{typ: typ, run: func(ctx context.Context, j *jobs.Job, _ func(jobs.Progress)) error {
		started <- j.ID
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	return h, started, release
}

func TestRunner_Submit_UnknownType_IsInvalid(t *testing.T) {
	w := newWorld(t)
	r := w.runner(t, "r1")
	_, err := r.Submit(t.Context(), session1(), "c1", "nope", nil, nil)
	assert.Error(t, err)
}

func TestRunner_Submit_RecordsTheRowBeforeTheHandlerRuns(t *testing.T) {
	w := newWorld(t)
	var seen store.JobRow
	h := &fakeHandler{typ: "bulk"}
	h.run = func(ctx context.Context, j *jobs.Job, _ func(jobs.Progress)) error {
		row, err := w.jobs.Get(ctx, j.ID)
		require.NoError(t, err)
		seen = row
		return nil
	}
	r := w.runner(t, "r1", h)
	id := w.submit(t, r, "bulk", map[string]string{"k": "v"})
	r.Wait()

	assert.Equal(t, id, seen.ID)
	assert.Equal(t, store.JobRunning, seen.Status)
	assert.Equal(t, "r1", seen.LeaseOwner)
	assert.Equal(t, "u1", seen.UserID)
	assert.Equal(t, "h1", seen.SessionHash)
	assert.Equal(t, "c1", seen.Cluster)
	assert.Equal(t, t0.Add(30*time.Second), seen.LeaseExpiresAt)
	assert.NotContains(t, seen.SealedParams, `"k"`, "parameters are sealed")
	_, err := w.sealer.Open(seen.SealedParams, crypto.AAD(crypto.PurposeJob, id))
	assert.NoError(t, err, "bound to the job ID")
}

func TestRunner_Submit_PassesTargetAndParamsToTheHandler(t *testing.T) {
	w := newWorld(t)
	got := make(chan *jobs.Job, 1)
	h := &fakeHandler{typ: "bulk", run: func(_ context.Context, j *jobs.Job, _ func(jobs.Progress)) error {
		got <- j
		return nil
	}}
	r := w.runner(t, "r1", h)
	w.submit(t, r, "bulk", map[string]string{"k": "v"})
	j := testutil.Recv(t, got)
	assert.JSONEq(t, `{"k":"v"}`, string(j.Params))
	assert.JSONEq(t, `{"name":"n"}`, string(j.Target))
	assert.Equal(t, session.Session{Hash: "h1", UserID: "u1"}, j.Session())
	assert.Equal(t, "c1", j.Cluster)
	r.Wait()
}

func TestRunner_Limits_ExtraJobsStayQueuedAndStartInSubmissionOrder(t *testing.T) {
	w := newWorld(t)
	h, started, release := blocking("bulk")
	r := w.runner(t, "r1", h)
	ids := []string{w.submit(t, r, "bulk", nil), w.submit(t, r, "bulk", nil), w.submit(t, r, "bulk", nil), w.submit(t, r, "bulk", nil)}

	// the first two run side by side, so they may report in either order
	assert.ElementsMatch(t, ids[:2], []string{testutil.Recv(t, started), testutil.Recv(t, started)})
	testutil.NoRecv(t, started)
	assert.Equal(t, store.JobQueued, w.row(t, ids[2]).Status)
	assert.Equal(t, store.JobQueued, w.row(t, ids[3]).Status)

	release <- struct{}{} // one running job finishes
	assert.Equal(t, ids[2], testutil.Recv(t, started), "the next in submission order starts")
	testutil.NoRecv(t, started)
	close(release)
	assert.Equal(t, ids[3], testutil.Recv(t, started))
	r.Wait()
}

func TestRunner_Limits_HelmTypesAllowFiveAtOnce(t *testing.T) {
	w := newWorld(t)
	h, started, release := blocking("helm.install")
	r := w.runner(t, "r1", h)
	for range 6 {
		w.submit(t, r, "helm.install", nil)
	}
	for range 5 {
		testutil.Recv(t, started)
	}
	testutil.NoRecv(t, started)
	close(release)
	testutil.Recv(t, started)
	r.Wait()
}

func TestRunner_Limits_AreConfigurable(t *testing.T) {
	w := newWorld(t)
	h, started, release := blocking("bulk")
	d := w.deps("r1")
	d.Limits = jobs.Limits{Helm: 1, Other: 1}
	r := w.runnerWith(t, d, h)
	w.submit(t, r, "bulk", nil)
	w.submit(t, r, "bulk", nil)
	testutil.Recv(t, started)
	testutil.NoRecv(t, started)
	close(release)
	testutil.Recv(t, started)
	r.Wait()
}

func TestRunner_Outcomes_NilIsSucceeded_ErrorIsFailed_PanicIsFailed(t *testing.T) {
	w := newWorld(t)
	ok := &fakeHandler{typ: "ok"}
	bad := &fakeHandler{typ: "bad", run: func(context.Context, *jobs.Job, func(jobs.Progress)) error { return errors.New("it broke") }}
	boom := &fakeHandler{typ: "boom", run: func(context.Context, *jobs.Job, func(jobs.Progress)) error { panic("kaput") }}
	r := w.runner(t, "r1", ok, bad, boom)
	a, b, c := w.submit(t, r, "ok", nil), w.submit(t, r, "bad", nil), w.submit(t, r, "boom", nil)
	r.Wait()

	ra, rb, rc := w.row(t, a), w.row(t, b), w.row(t, c)
	assert.Equal(t, store.JobSucceeded, ra.Status)
	assert.NotZero(t, ra.FinishedAt)
	assert.Equal(t, store.JobFailed, rb.Status)
	assert.Equal(t, "it broke", rb.Message)
	assert.Equal(t, `["retry"]`, rb.Offers)
	assert.Equal(t, store.JobFailed, rc.Status)
	assert.NotContains(t, rc.Message, "kaput", "panic values are logged, not shown")
	assert.NotEmpty(t, rc.Message)
}

func TestRunner_Report_PersistsProgress(t *testing.T) {
	w := newWorld(t)
	reported := make(chan struct{})
	release := make(chan struct{})
	h := &fakeHandler{typ: "bulk", run: func(_ context.Context, _ *jobs.Job, report func(jobs.Progress)) error {
		report(jobs.Progress{Done: 1, Total: 3, Text: "one of three", Items: []jobs.ItemResult{{Name: "a", Status: "done"}}})
		close(reported)
		<-release
		return nil
	}}
	r := w.runner(t, "r1", h)
	id := w.submit(t, r, "bulk", nil)
	<-reported
	assert.Equal(t, jobs.Progress{Done: 1, Total: 3, Text: "one of three", Items: []jobs.ItemResult{{Name: "a", Status: "done"}}}, progressOf(t, w.row(t, id)))
	close(release)
	r.Wait()
}

func TestRunner_Lease_IsRenewedEveryTenSeconds(t *testing.T) {
	w := newWorld(t)
	h, started, release := blocking("bulk")
	r := w.runner(t, "r1", h)
	w.start(t, r, 0)
	id := w.submit(t, r, "bulk", nil)
	testutil.Recv(t, started)
	assert.Equal(t, t0.Add(30*time.Second), w.row(t, id).LeaseExpiresAt)

	w.pass(t, "renew", 10*time.Second, 1)
	assert.Equal(t, t0.Add(40*time.Second), w.row(t, id).LeaseExpiresAt)
	w.pass(t, "renew", 10*time.Second, 1)
	assert.Equal(t, t0.Add(50*time.Second), w.row(t, id).LeaseExpiresAt)
	close(release)
	r.Wait()
}

func TestRunner_Lease_QueuedJobsAreRenewedToo(t *testing.T) {
	w := newWorld(t)
	h, started, release := blocking("bulk")
	d := w.deps("r1")
	d.Limits = jobs.Limits{Helm: 1, Other: 1}
	r := w.runnerWith(t, d, h)
	w.start(t, r, 0)
	w.submit(t, r, "bulk", nil)
	queued := w.submit(t, r, "bulk", nil)
	testutil.Recv(t, started)
	w.pass(t, "renew", 10*time.Second, 1)
	assert.Equal(t, t0.Add(40*time.Second), w.row(t, queued).LeaseExpiresAt)
	assert.Equal(t, store.JobQueued, w.row(t, queued).Status)
	close(release)
	r.Wait()
}

func TestRunner_Lease_LostCancelsTheHandlerAndKeepsTheStatus(t *testing.T) {
	for name, lose := range map[string]string{
		"row deleted":  "DELETE FROM jobs WHERE id = ?",
		"other owner":  "UPDATE jobs SET lease_owner = 'someone-else' WHERE id = ?",
		"already done": "UPDATE jobs SET status = 'failed' WHERE id = ?",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			h, started, _ := blocking("bulk")
			r := w.runner(t, "r1", h)
			w.start(t, r, 0)
			id := w.submit(t, r, "bulk", nil)
			testutil.Recv(t, started)
			_, err := w.db.Exec(t.Context(), lose, id)
			require.NoError(t, err)

			w.pass(t, "renew", 10*time.Second, 1)
			r.Wait() // the handler saw its context end and returned

			if name == "row deleted" {
				_, err := w.jobs.Get(t.Context(), id)
				assert.ErrorIs(t, err, store.ErrNotFound)
				return
			}
			got := w.row(t, id)
			assert.NotEqual(t, store.JobSucceeded, got.Status)
			assert.NotEqual(t, "context canceled", got.Message, "this replica wrote nothing")
			if name == "other owner" {
				assert.Equal(t, store.JobRunning, got.Status)
				assert.Equal(t, "someone-else", got.LeaseOwner)
			}
		})
	}
}

func TestRunner_Close_CancelsRunningJobsAndLeavesThemRunning(t *testing.T) {
	w := newWorld(t)
	h, started, _ := blocking("bulk")
	r := w.runner(t, "r1", h)
	id := w.submit(t, r, "bulk", nil)
	testutil.Recv(t, started)
	r.Close()
	got := w.row(t, id)
	assert.Equal(t, store.JobRunning, got.Status, "recovery classifies it later")
	assert.Empty(t, got.Message)
}

func TestRunner_Close_LeavesQueuedJobsQueued(t *testing.T) {
	w := newWorld(t)
	h, started, _ := blocking("bulk")
	d := w.deps("r1")
	d.Limits = jobs.Limits{Helm: 1, Other: 1}
	r := w.runnerWith(t, d, h)
	w.submit(t, r, "bulk", nil)
	queued := w.submit(t, r, "bulk", nil)
	testutil.Recv(t, started)
	r.Close()
	assert.Equal(t, store.JobQueued, w.row(t, queued).Status)
	assert.EqualValues(t, 1, h.runs.Load())
}

func TestRunner_Submit_AfterClose_IsRefused(t *testing.T) {
	w := newWorld(t)
	r := w.runner(t, "r1", &fakeHandler{typ: "bulk"})
	r.Close()
	_, err := r.Submit(t.Context(), session1(), "c1", "bulk", nil, nil)
	assert.Error(t, err)
}

func TestRunner_ContextHook_ReachesTheHandler(t *testing.T) {
	type key struct{}
	w := newWorld(t)
	got := make(chan any, 1)
	h := &fakeHandler{typ: "bulk", run: func(ctx context.Context, _ *jobs.Job, _ func(jobs.Progress)) error {
		got <- ctx.Value(key{})
		return nil
	}}
	d := w.deps("r1")
	d.Context = func(ctx context.Context, j *jobs.Job) context.Context { return context.WithValue(ctx, key{}, j.UserID) }
	r := w.runnerWith(t, d, h)
	w.submit(t, r, "bulk", nil)
	assert.Equal(t, "u1", testutil.Recv(t, got))
	r.Wait()
}
