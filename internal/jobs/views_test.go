package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/jobs"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
)

func TestRunner_Retry_CreatesANewJobWithTheSameParamsUnderTheCallersSession(t *testing.T) {
	w := newWorld(t)
	w.sessions = newSessions("h1", "h2")
	got := make(chan *jobs.Job, 4)
	fail := true
	h := &fakeHandler{typ: "bulk", run: func(_ context.Context, j *jobs.Job, _ func(jobs.Progress)) error {
		got <- j
		if fail {
			return errors.New("first attempt fails")
		}
		return nil
	}}
	r := w.runner(t, "r1", h)
	orig := w.submit(t, r, "bulk", map[string]string{"k": "v"})
	r.Wait()
	<-got
	require.Equal(t, store.JobFailed, w.row(t, orig).Status)

	fail = false
	newID, err := r.Retry(t.Context(), session.Session{Hash: "h2", UserID: "u1"}, orig)
	require.NoError(t, err)
	r.Wait()
	assert.NotEqual(t, orig, newID)
	j := <-got
	assert.Equal(t, newID, j.ID)
	assert.Equal(t, "h2", j.SessionHash, "the caller's current session")
	assert.JSONEq(t, `{"k":"v"}`, string(j.Params))
	assert.JSONEq(t, `{"name":"n"}`, string(j.Target))
	assert.Equal(t, "c1", j.Cluster)
	assert.Equal(t, store.JobSucceeded, w.row(t, newID).Status)
	assert.Equal(t, store.JobFailed, w.row(t, orig).Status, "the old job is untouched")
}

func TestRunner_Retry_OtherUsersGetNotFound_RunningJobsConflict(t *testing.T) {
	w := newWorld(t)
	h, started, release := blocking("bulk")
	r := w.runner(t, "r1", h)
	id := w.submit(t, r, "bulk", nil)
	<-started

	_, err := r.Retry(t.Context(), session.Session{Hash: "x", UserID: "u2"}, id)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = r.Retry(t.Context(), session.Session{Hash: "h1", UserID: "u1"}, id)
	assert.ErrorIs(t, err, apperr.ErrConflict, "only failed or interrupted jobs are retried")
	_, err = r.Retry(t.Context(), session1(), "missing")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	close(release)
	r.Wait()
}

func TestRunner_Get_OnlyTheOwnerSeesTheJob(t *testing.T) {
	w := newWorld(t)
	h := &fakeHandler{typ: "bulk", run: func(_ context.Context, _ *jobs.Job, report func(jobs.Progress)) error {
		report(jobs.Progress{Done: 2, Total: 2, Text: "ok"})
		return nil
	}}
	r := w.runner(t, "r1", h)
	id := w.submit(t, r, "bulk", map[string]string{"secret": "x"})
	r.Wait()

	got, err := r.Get(t.Context(), "u1", id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, jobs.Succeeded, got.Status)
	assert.Equal(t, jobs.Progress{Done: 2, Total: 2, Text: "ok"}, got.Progress)
	assert.Equal(t, []string{}, got.Offers)
	assert.Equal(t, t0, got.CreatedAt)
	assert.Empty(t, got.Params, "parameters are not part of the view")

	_, err = r.Get(t.Context(), "u2", id)
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = r.Get(t.Context(), "u1", "missing")
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestRunner_List_NewestFirstWithActiveFilter(t *testing.T) {
	w := newWorld(t)
	h, started, release := blocking("bulk")
	ok := &fakeHandler{typ: "ok"}
	r := w.runner(t, "r1", h, ok)
	done := w.submit(t, r, "ok", nil)
	r.Wait()
	w.clock.Advance(time.Minute)
	running := w.submit(t, r, "bulk", nil)
	<-started

	ids := func(active *bool) []string {
		list, err := r.List(t.Context(), "u1", active)
		require.NoError(t, err)
		var out []string
		for _, j := range list {
			out = append(out, j.ID)
		}
		return out
	}
	yes, no := true, false
	assert.Equal(t, []string{running, done}, ids(nil))
	assert.Equal(t, []string{running}, ids(&yes))
	assert.Equal(t, []string{done}, ids(&no))
	list, err := r.List(t.Context(), "u2", nil)
	require.NoError(t, err)
	assert.Empty(t, list)
	close(release)
	r.Wait()
}
