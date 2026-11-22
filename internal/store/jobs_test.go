package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

func newJobs(t *testing.T) *store.Jobs {
	t.Helper()
	return store.NewJobs(storetest.New(t))
}

func jobRow(id, user string, created time.Time) store.JobRow {
	return store.JobRow{
		ID: id, UserID: user, SessionHash: "h", Cluster: "c1", Type: "bulk", Target: `{"name":"n"}`, SealedParams: "sealed",
		Status: store.JobQueued, Progress: "{}", Offers: "[]", LeaseOwner: "r1", LeaseExpiresAt: created.Add(30 * time.Second),
		CreatedAt: created, UpdatedAt: created,
	}
}

func TestJobs_InsertThenGet_RoundTrips(t *testing.T) {
	j := newJobs(t)
	row := jobRow("j1", "u1", t0)
	require.NoError(t, j.Insert(t.Context(), row))
	got, err := j.Get(t.Context(), "j1")
	require.NoError(t, err)
	assert.Equal(t, row, got)
}

func TestJobs_Get_Unknown_ErrNotFound(t *testing.T) {
	_, err := newJobs(t).Get(t.Context(), "nope")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestJobs_List_OnlyTheUsersJobs_NewestFirst_ActiveFilter_Limit(t *testing.T) {
	ctx := t.Context()
	j := newJobs(t)
	for i, id := range []string{"a", "b", "c"} {
		require.NoError(t, j.Insert(ctx, jobRow(id, "u1", t0.Add(time.Duration(i)*time.Minute))))
	}
	require.NoError(t, j.Insert(ctx, jobRow("x", "u2", t0)))
	ok, err := j.Start(ctx, "b", "r1", t0.Add(time.Hour), t0)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = j.Finish(ctx, "b", "r1", store.JobSucceeded, "done", "[]", t0.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, ok)

	ids := func(active *bool, limit int) []string {
		rows, err := j.List(ctx, "u1", active, limit)
		require.NoError(t, err)
		var out []string
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}
	yes, no := true, false
	assert.Equal(t, []string{"c", "b", "a"}, ids(nil, 100))
	assert.Equal(t, []string{"c", "a"}, ids(&yes, 100))
	assert.Equal(t, []string{"b"}, ids(&no, 100))
	assert.Equal(t, []string{"c"}, ids(nil, 1))
}

func TestJobs_Transitions_AreCompareAndSwap(t *testing.T) {
	ctx := t.Context()
	j := newJobs(t)
	require.NoError(t, j.Insert(ctx, jobRow("j", "u1", t0)))

	ok, err := j.Start(ctx, "j", "other", t0.Add(time.Minute), t0)
	require.NoError(t, err)
	assert.False(t, ok, "only the lease owner starts a job")
	ok, err = j.Finish(ctx, "j", "r1", store.JobSucceeded, "", "[]", t0)
	require.NoError(t, err)
	assert.False(t, ok, "a queued job cannot finish")

	ok, err = j.Start(ctx, "j", "r1", t0.Add(30*time.Second), t0.Add(time.Second))
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = j.Start(ctx, "j", "r1", t0.Add(30*time.Second), t0.Add(time.Second))
	require.NoError(t, err)
	assert.False(t, ok, "a running job cannot start again")

	ok, err = j.SetProgress(ctx, "j", "r1", `{"done":1}`, t0.Add(2*time.Second))
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = j.SetProgress(ctx, "j", "other", `{"done":2}`, t0)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = j.Finish(ctx, "j", "other", store.JobFailed, "x", "[]", t0)
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = j.Finish(ctx, "j", "r1", store.JobFailed, "boom", `["retry"]`, t0.Add(3*time.Second))
	require.NoError(t, err)
	assert.True(t, ok)

	got, err := j.Get(ctx, "j")
	require.NoError(t, err)
	assert.Equal(t, store.JobFailed, got.Status)
	assert.Equal(t, `{"done":1}`, got.Progress)
	assert.Equal(t, "boom", got.Message)
	assert.Equal(t, `["retry"]`, got.Offers)
	assert.Equal(t, t0.Add(3*time.Second), got.FinishedAt)

	ok, err = j.Renew(ctx, "j", "r1", t0.Add(time.Hour), t0)
	require.NoError(t, err)
	assert.False(t, ok, "a finished job has no lease to renew")
}

func TestJobs_Renew_ExtendsTheLease_OnlyForTheOwner(t *testing.T) {
	ctx := t.Context()
	j := newJobs(t)
	require.NoError(t, j.Insert(ctx, jobRow("j", "u1", t0)))
	ok, err := j.Renew(ctx, "j", "r1", t0.Add(time.Minute), t0.Add(10*time.Second))
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = j.Renew(ctx, "j", "other", t0.Add(time.Hour), t0)
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = j.Renew(ctx, "gone", "r1", t0.Add(time.Hour), t0)
	require.NoError(t, err)
	assert.False(t, ok)
	got, err := j.Get(ctx, "j")
	require.NoError(t, err)
	assert.Equal(t, t0.Add(time.Minute), got.LeaseExpiresAt)
}

func TestJobs_ExpiredStealClassify_OnlyOneReplicaWins(t *testing.T) {
	ctx := t.Context()
	j := newJobs(t)
	require.NoError(t, j.Insert(ctx, jobRow("j", "u1", t0)))
	require.NoError(t, j.Insert(ctx, jobRow("fresh", "u1", t0.Add(time.Hour))))
	_, err := j.Start(ctx, "j", "r1", t0.Add(30*time.Second), t0)
	require.NoError(t, err)

	now := t0.Add(time.Minute)
	expired, err := j.Expired(ctx, now)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	assert.Equal(t, "j", expired[0].ID)

	won, err := j.Steal(ctx, "j", expired[0].LeaseExpiresAt, "r2", now.Add(30*time.Second), now)
	require.NoError(t, err)
	assert.True(t, won)
	lost, err := j.Steal(ctx, "j", expired[0].LeaseExpiresAt, "r3", now.Add(30*time.Second), now)
	require.NoError(t, err)
	assert.False(t, lost, "the second scanner sees a changed lease")

	ok, err := j.Classify(ctx, "j", "r2", now.Add(30*time.Second), store.JobInterrupted, "stopped", `["retry"]`, now)
	require.NoError(t, err)
	assert.True(t, ok)
	got, err := j.Get(ctx, "j")
	require.NoError(t, err)
	assert.Equal(t, store.JobInterrupted, got.Status)
	assert.Equal(t, "stopped", got.Message)
	assert.Equal(t, now, got.FinishedAt)

	ok, err = j.Classify(ctx, "j", "r2", now.Add(30*time.Second), store.JobFailed, "again", "[]", now)
	require.NoError(t, err)
	assert.False(t, ok, "a classified job is not classified twice")
}

func TestJobs_DeleteForSession_RemovesNothingButIsNotNeeded(t *testing.T) {
	// jobs outlive their session: the row keeps the session hash without a foreign key
	db := storetest.New(t)
	_, err := store.NewUsers(db, clock.NewFake(t0), func() string { return "u1" }).Upsert(t.Context(), "google", "s", "e@x", "n")
	require.NoError(t, err)
	j := store.NewJobs(db)
	require.NoError(t, j.Insert(t.Context(), jobRow("j", "u1", t0)))
	require.NoError(t, store.NewSessions(db).Delete(t.Context(), "h"))
	_, err = j.Get(t.Context(), "j")
	assert.NoError(t, err)
}

func TestJobs_CountByTypeAndStatus_GroupsEveryUsersJobs(t *testing.T) {
	ctx := t.Context()
	j := newJobs(t)
	got, err := j.CountByTypeAndStatus(ctx)
	require.NoError(t, err)
	assert.Empty(t, got)

	for i, id := range []string{"a", "b", "c"} {
		require.NoError(t, j.Insert(ctx, jobRow(id, "u1", t0.Add(time.Duration(i)*time.Minute)))) // type "bulk", queued
	}
	other := jobRow("d", "u2", t0)
	other.Type = "helm.install"
	require.NoError(t, j.Insert(ctx, other))
	ok, err := j.Start(ctx, "b", "r1", t0.Add(time.Hour), t0)
	require.NoError(t, err)
	require.True(t, ok)

	got, err = j.CountByTypeAndStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, []store.JobCount{
		{Type: "bulk", Status: "queued", Count: 2},
		{Type: "bulk", Status: "running", Count: 1},
		{Type: "helm.install", Status: "queued", Count: 1},
	}, got)
}
