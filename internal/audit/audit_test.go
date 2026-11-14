package audit_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
)

var t0 = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

type env struct {
	db   *store.DB
	rec  *audit.Recorder
	out  *bytes.Buffer
	fake *clock.Fake
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := storetest.New(t)
	out := &bytes.Buffer{}
	fake := clock.NewFake(t0)
	n := 0
	var mu sync.Mutex
	newID := func() string { mu.Lock(); defer mu.Unlock(); n++; return "id-" + strconv.Itoa(n) }
	return env{db: db, rec: audit.NewRecorder(db, fake, out, newID), out: out, fake: fake}
}

func (e env) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(e.out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m), l)
		out = append(out, m)
	}
	return out
}

type row struct {
	ID, Provider, Subject, Email, EntityID, DisplayName, KubeCtx, KubeUser string
	Cluster, Namespace, Verb, Kind, Name, Outcome, Details                 string
	Time                                                                   int64
}

func (e env) rows(t *testing.T) []row {
	t.Helper()
	rs, err := e.db.Query(t.Context(), `SELECT id, time, account_provider, account_subject, account_email, vault_entity_id,
vault_display_name, kube_context, kube_user, cluster, namespace, verb, resource_kind, resource_name, outcome, details
FROM audit_events ORDER BY time, id`)
	require.NoError(t, err)
	defer func() { _ = rs.Close() }()
	var out []row
	for rs.Next() {
		var r row
		require.NoError(t, rs.Scan(&r.ID, &r.Time, &r.Provider, &r.Subject, &r.Email, &r.EntityID, &r.DisplayName,
			&r.KubeCtx, &r.KubeUser, &r.Cluster, &r.Namespace, &r.Verb, &r.Kind, &r.Name, &r.Outcome, &r.Details))
		out = append(out, r)
	}
	require.NoError(t, rs.Err())
	return out
}

var identity = audit.Identity{
	AccountProvider: "google", AccountSubject: "sub-1", AccountEmail: "a@example.com",
	VaultEntityID: "ent-1", VaultDisplayName: "ann",
	KubeContext: "ctx", KubeUser: "ann@k",
}

var event = audit.Event{
	Cluster: "v:prod", Namespace: "web", Verb: audit.VerbDelete, ResourceKind: "Pod", ResourceName: "p1",
	Outcome: "success", Details: map[string]any{"reason": "cleanup", "count": 2},
}

func TestRecord_InsertsRowAndWritesOneJSONLine(t *testing.T) {
	e := newEnv(t)
	ctx := audit.WithIdentity(t.Context(), identity)
	require.NoError(t, e.rec.Record(ctx, event))

	rows := e.rows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, row{
		ID: "id-1", Time: t0.UnixMilli(), Provider: "google", Subject: "sub-1", Email: "a@example.com",
		EntityID: "ent-1", DisplayName: "ann", KubeCtx: "ctx", KubeUser: "ann@k",
		Cluster: "v:prod", Namespace: "web", Verb: "delete", Kind: "Pod", Name: "p1", Outcome: "success",
		Details: `{"count":2,"reason":"cleanup"}`,
	}, rows[0])

	lines := e.lines(t)
	require.Len(t, lines, 1)
	l := lines[0]
	assert.Equal(t, true, l["audit"])
	assert.Equal(t, true, l["recorded"])
	ts, err := time.Parse(time.RFC3339, l["time"].(string))
	require.NoError(t, err)
	assert.True(t, ts.Equal(t0))
	assert.Equal(t, map[string]any{"provider": "google", "subject": "sub-1", "email": "a@example.com"}, l["account"])
	assert.Equal(t, map[string]any{"entityId": "ent-1", "displayName": "ann"}, l["vault"])
	assert.Equal(t, map[string]any{"context": "ctx", "user": "ann@k"}, l["kube"])
	assert.Equal(t, "v:prod", l["cluster"])
	assert.Equal(t, "web", l["namespace"])
	assert.Equal(t, "delete", l["verb"])
	assert.Equal(t, map[string]any{"kind": "Pod", "name": "p1"}, l["resource"])
	assert.Equal(t, "success", l["outcome"])
	assert.Equal(t, map[string]any{"reason": "cleanup", "count": float64(2)}, l["details"])
	assert.Equal(t, 1, strings.Count(e.out.String(), "\n"))
}

func TestRecord_NoIdentityInContext_UsesEmptyStrings(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.rec.Record(t.Context(), audit.Event{Verb: audit.VerbSignIn, Outcome: "failure"}))
	r := e.rows(t)[0]
	assert.Empty(t, r.Provider+r.Subject+r.Email+r.EntityID+r.DisplayName+r.KubeCtx+r.KubeUser)
}

func TestRecord_NilDetails_StoredAsEmptyObject(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.rec.Record(t.Context(), audit.Event{Verb: audit.VerbSignOut, Outcome: "success"}))
	assert.Equal(t, "{}", e.rows(t)[0].Details)
	assert.Equal(t, map[string]any{}, e.lines(t)[0]["details"])
}

func TestRecord_DatabaseFailure_ErrNotRecordedButLineStillWritten(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.db.Close())
	err := e.rec.Record(t.Context(), event)
	require.ErrorIs(t, err, audit.ErrNotRecorded)
	lines := e.lines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, false, lines[0]["recorded"])
	assert.Equal(t, "delete", lines[0]["verb"])
}

func TestRecord_ForbiddenDetailKeys_Rejected(t *testing.T) {
	for _, key := range []string{"token", "password", "secret", "content", "value", "Token", "SECRET"} {
		e := newEnv(t)
		err := e.rec.Record(t.Context(), audit.Event{Verb: "x", Outcome: "success", Details: map[string]any{key: "s3cret"}})
		assert.ErrorIs(t, err, audit.ErrForbiddenDetail, key)
		assert.Empty(t, e.rows(t), key)
		assert.Empty(t, e.out.String(), key)
	}
}

func TestBegin_RecordsRequestedAndDoneRecordsOutcomeWithSameRequestID(t *testing.T) {
	e := newEnv(t)
	ctx := audit.WithIdentity(t.Context(), identity)
	ev := event
	ev.Details = map[string]any{"reason": "x"}

	p, err := e.rec.Begin(ctx, ev)
	require.NoError(t, err)
	e.fake.Advance(time.Second)
	p.Done(ctx, nil)

	p2, err := e.rec.Begin(ctx, ev)
	require.NoError(t, err)
	p2.Done(ctx, errors.New("boom"))

	rows := e.rows(t)
	require.Len(t, rows, 4)
	details := func(i int) map[string]any {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(rows[i].Details), &m))
		return m
	}
	byOutcome := map[string][]map[string]any{}
	for i, r := range rows {
		byOutcome[r.Outcome] = append(byOutcome[r.Outcome], details(i))
	}
	require.Len(t, byOutcome["requested"], 2)
	require.Len(t, byOutcome["success"], 1)
	require.Len(t, byOutcome["failure"], 1)

	// the first pair shares a request ID; so does the second; the two pairs differ
	reqs := []string{byOutcome["requested"][0]["requestId"].(string), byOutcome["requested"][1]["requestId"].(string)}
	assert.NotEqual(t, reqs[0], reqs[1])
	assert.Contains(t, reqs, byOutcome["success"][0]["requestId"])
	assert.Contains(t, reqs, byOutcome["failure"][0]["requestId"])
	assert.Equal(t, "boom", byOutcome["failure"][0]["error"])
	assert.Equal(t, "x", byOutcome["success"][0]["reason"])
	assert.NotContains(t, byOutcome["success"][0], "error")
	assert.Equal(t, "Pod", rows[0].Kind)
	assert.Equal(t, "ent-1", rows[0].EntityID)
}

func TestBegin_DatabaseFailure_ReturnsErrorSoCallerMustNotAct(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.db.Close())
	p, err := e.rec.Begin(t.Context(), event)
	require.ErrorIs(t, err, audit.ErrNotRecorded)
	assert.Nil(t, p)
}

func TestBegin_ForbiddenDetail_ReturnsError(t *testing.T) {
	e := newEnv(t)
	p, err := e.rec.Begin(t.Context(), audit.Event{Verb: "x", Details: map[string]any{"password": "p"}})
	require.ErrorIs(t, err, audit.ErrForbiddenDetail)
	assert.Nil(t, p)
}

func TestDone_DatabaseFailure_DoesNotPanicAndStillWritesLine(t *testing.T) {
	e := newEnv(t)
	p, err := e.rec.Begin(t.Context(), event)
	require.NoError(t, err)
	require.NoError(t, e.db.Close())
	p.Done(t.Context(), nil)
	lines := e.lines(t)
	require.Len(t, lines, 2)
	assert.Equal(t, false, lines[1]["recorded"])
}

func TestRecord_ConcurrentCalls_NeverInterleaveLines(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, e.rec.Record(t.Context(), event))
		}()
	}
	wg.Wait()
	assert.Len(t, e.lines(t), 20)
	assert.Len(t, e.rows(t), 20)
}

func TestVerbConstants(t *testing.T) {
	assert.Equal(t, "sign-in", audit.VerbSignIn)
	assert.Equal(t, "vault-credential-issued", audit.VerbVaultCredentialIssued)
	assert.Equal(t, "key-changed", audit.VerbKeyChanged)
}

func TestUserIdentity_TakesProviderSubjectAndEmail(t *testing.T) {
	id := audit.UserIdentity(store.User{Provider: "github", Subject: "42", Email: "o@x.example", Name: "Octo"})
	assert.Equal(t, audit.Identity{AccountProvider: "github", AccountSubject: "42", AccountEmail: "o@x.example"}, id)
}

func TestIdentityFrom_NothingAttached_Zero(t *testing.T) {
	assert.Equal(t, audit.Identity{}, audit.IdentityFrom(t.Context()))
	ctx := audit.WithIdentity(t.Context(), audit.Identity{AccountEmail: "e"})
	assert.Equal(t, "e", audit.IdentityFrom(ctx).AccountEmail)
}

func TestPending_AnnotateAddsADetailToTheFinalRecordOnly(t *testing.T) {
	e := newEnv(t)
	p, err := e.rec.Begin(t.Context(), audit.Event{Verb: "file-view", Details: map[string]any{"path": "/etc/hosts"}})
	require.NoError(t, err)
	p.Annotate("size", 42)
	p.Done(t.Context(), nil)
	lines := e.lines(t)
	require.Len(t, lines, 2)
	assert.NotContains(t, lines[0]["details"], "size")
	assert.EqualValues(t, 42, lines[1]["details"].(map[string]any)["size"])
	assert.Equal(t, "/etc/hosts", lines[1]["details"].(map[string]any)["path"])
}
