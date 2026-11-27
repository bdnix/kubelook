//go:build integration && itpostgres

package integration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

// The PostgreSQL tests address the two replicas of the release one by one: it.Replica1URL and it.Replica2URL. They
// share one database and one encryption key; a client signed in on one reaches the other with Client.On.

const pgHost = "127.0.0.1:15432"

// pgConn connects to the integration database as a role.
func pgConn(t *testing.T, user, password string) *pgx.Conn {
	t.Helper()
	// not t.Context(): the connections are also made in cleanups, when the context of the test is already cancelled
	conn, err := pgx.Connect(context.Background(), "postgres://"+user+":"+password+"@"+pgHost+"/kubelook")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func pgOwner(t *testing.T) *pgx.Conn   { return pgConn(t, "postgres", "postgres") }
func pgApp(t *testing.T) *pgx.Conn     { return pgConn(t, "kubelook_app", "app") }
func pgCleanup(t *testing.T) *pgx.Conn { return pgConn(t, "kubelook_cleanup", "cleanup") }

// isPermissionDenied is true for PostgreSQL's "insufficient privilege" error (SQLSTATE 42501).
func isPermissionDenied(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "42501"
}

// insertAudit inserts an audit record with a time (milliseconds) as the database owner, who is not bound by the row policies.
func insertAudit(t *testing.T, owner *pgx.Conn, id string, at time.Time) {
	t.Helper()
	_, err := owner.Exec(t.Context(), `INSERT INTO audit_events (id, time, account_provider, account_subject, account_email,
vault_entity_id, vault_display_name, kube_context, kube_user, cluster, namespace, verb, resource_kind, resource_name, outcome, details)
VALUES ($1, $2, '', '', '', '', '', '', '', '', '', 'it-probe', '', '', 'success', '{}')`, id, at.UnixMilli())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pgOwner(t).Exec(context.Background(), `DELETE FROM audit_events WHERE id = $1`, id) })
}

func auditRowExists(t *testing.T, owner *pgx.Conn, id string) bool {
	t.Helper()
	var n int
	require.NoError(t, owner.QueryRow(t.Context(), `SELECT count(*) FROM audit_events WHERE id = $1`, id).Scan(&n))
	return n == 1
}

// pgSignedIn signs a fresh account in on replica 1, adds bob's Vault token and opens alpha with the devops role.
func pgSignedIn(t *testing.T, sub string) *it.Client {
	t.Helper()
	c := it.NewClient(t)
	c.Base = it.Replica1URL
	c.Origin = it.BaseURL() // the public URL of both replicas: their WebSocket upgrades accept only that origin
	final := c.SignInGoogle(it.Account{Sub: sub, Email: sub + "@corp.example", HD: "corp.example"})
	require.Empty(t, final.Query().Get("signin_error"), "signing in as %s: %s", sub, final)
	status, _, apiErr := addToken(t, c, "BOB_TOKEN")
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	status, _, apiErr = open(t, c, "v:alpha", "devops-role")
	require.Equal(t, http.StatusOK, status, "opening alpha: %+v", apiErr)
	return c
}

// restoreReplicas waits for the Deployment to replace a stopped or killed replica, numbers it again and waits until
// every address answers.
func restoreReplicas(t *testing.T) {
	t.Helper()
	it.Kubelook(t, "expose")
}

func TestPostgres_ASessionAndAnOpenedCluster_AreSharedByBothReplicas(t *testing.T) {
	c1 := pgSignedIn(t, "pg-shared")
	c2 := c1.On(it.Replica2URL)

	var me struct {
		User struct{ Email string } `json:"user"`
	}
	require.Equal(t, http.StatusOK, c2.JSON(http.MethodGet, "/api/v1/me", nil, &me), "the session created on replica 1 is valid on replica 2")
	assert.Equal(t, "pg-shared@corp.example", me.User.Email)

	status, items := listItems(t, c2, "v:alpha", "core", "v1", "pods", "demo")
	require.Equal(t, http.StatusOK, status, "the cluster opened on replica 1 lists pods through replica 2 without being opened again")
	assert.NotEmpty(t, items)
}

func TestPostgres_LogoutOnOneReplica_ClosesTheWebSocketOfTheOther(t *testing.T) {
	c1 := pgSignedIn(t, "pg-logout")
	c2 := c1.On(it.Replica2URL)
	w := c2.WS()
	ch := w.Open("watch.open", watchConfigMaps("v:alpha", "demo"))
	ch.Next("snapshot", 30*time.Second)

	require.Equal(t, http.StatusNoContent, c1.JSON(http.MethodPost, "/api/v1/auth/logout", nil, nil))
	assert.Equal(t, 4401, w.Closed(15*time.Second), "replica 2 notices within the 5-second watchdog period (A13)")
}

func TestPostgres_StoppingAReplica_TheSessionContinuesOnTheOtherAndAWatchDeliversASnapshot(t *testing.T) {
	c1 := pgSignedIn(t, "pg-stop")
	c2 := c1.On(it.Replica2URL)
	it.Kubelook(t, "stop", "1")
	t.Cleanup(func() { restoreReplicas(t) })

	require.Equal(t, http.StatusOK, c2.JSON(http.MethodGet, "/api/v1/me", nil, nil), "the session continues on replica 2")
	w := c2.WS()
	ch := w.Open("watch.open", watchConfigMaps("v:alpha", "demo"))
	var snap snapshotFrame
	require.NoError(t, json.Unmarshal(ch.Next("snapshot", 30*time.Second), &snap), "a watch opened on replica 2 delivers a snapshot")
	assert.NotEmpty(t, snap.Items)
}

func TestPostgres_ADrainJobOfAKilledReplica_IsInterruptedWithARetryOfferSeenFromTheOther(t *testing.T) {
	c1 := pgSignedIn(t, "pg-job")
	c2 := c1.On(it.Replica2URL)
	node := it.Kubectl(t, "alpha", "get", "nodes", "-o", "jsonpath={.items[0].metadata.name}")
	t.Cleanup(func() {
		restoreReplicas(t)
		it.Kubectl(t, "alpha", "uncordon", node)
		for _, d := range []string{"logger", "shell"} {
			it.Kubectl(t, "alpha", "-n", "demo", "rollout", "status", "deployment/"+d, "--timeout=180s")
		}
	})

	// The PodDisruptionBudget of "web" (maxUnavailable: 0) keeps the drain from ever finishing.
	var started struct {
		JobID string `json:"jobId"`
	}
	require.Equal(t, http.StatusOK, c1.JSON(http.MethodPost, "/api/v1/clusters/v:alpha/nodes/"+node+"/drain", map[string]any{}, &started))
	require.NotEmpty(t, started.JobID)
	var job struct {
		Status string   `json:"status"`
		Offers []string `json:"offers"`
	}
	it.Eventually(t, 60*time.Second, func() bool {
		c1.JSON(http.MethodGet, "/api/v1/jobs/"+started.JobID, nil, &job)
		return job.Status == "running"
	}, "the drain job runs")

	it.Kubelook(t, "kill", "1")
	it.Eventually(t, 90*time.Second, func() bool {
		job.Status, job.Offers = "", nil
		c2.JSON(http.MethodGet, "/api/v1/jobs/"+started.JobID, nil, &job)
		return job.Status == "interrupted"
	}, "replica 2 sees the job as interrupted within 90 seconds")
	assert.Contains(t, job.Offers, "retry")
}

func TestPostgres_AuditRecords_CannotBeReadChangedOrDeletedByTheApplicationRole(t *testing.T) {
	owner := pgOwner(t)
	app := pgApp(t)
	_, err := app.Exec(t.Context(), `UPDATE audit_events SET verb = 'tampered'`)
	assert.True(t, isPermissionDenied(err), "UPDATE: %v", err)
	_, err = app.Exec(t.Context(), `DELETE FROM audit_events`)
	assert.True(t, isPermissionDenied(err), "DELETE: %v", err)
	var n int
	err = app.QueryRow(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&n)
	assert.True(t, isPermissionDenied(err), "SELECT: %v", err)

	// it can still write: signing in has written records through it
	var total int
	require.NoError(t, owner.QueryRow(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&total))
	assert.Positive(t, total, "the replicas wrote audit records through the application role")
}

func TestPostgres_AuditCleanup_RoleDeletesOnlyRecordsOlderThanTheRetentionPeriod(t *testing.T) {
	owner := pgOwner(t)
	now := time.Now()
	recent := now.Add(-time.Hour)
	old := now.Add(-48 * time.Hour)
	insertAudit(t, owner, "it-recent", recent)
	insertAudit(t, owner, "it-old", old)

	cleanup := pgCleanup(t)
	// the role may read and filter on the time column only (PostgreSQL needs SELECT on the columns of a WHERE)
	tag, err := cleanup.Exec(t.Context(), `DELETE FROM audit_events WHERE time = $1`, recent.UnixMilli())
	require.NoError(t, err)
	assert.Zero(t, tag.RowsAffected(), "a record newer than one day is not deleted")
	_, err = cleanup.Exec(t.Context(), `UPDATE audit_events SET verb = 'tampered' WHERE time = $1`, recent.UnixMilli())
	assert.True(t, isPermissionDenied(err), "UPDATE: %v", err)

	out := it.PG(t, "oneoff", "audit-cleanup", "KUBELOOK_AUDIT_CLEANUP_DATABASE_URL=postgres://kubelook_cleanup:cleanup@kubelook-it-postgres:5432/kubelook")
	assert.Contains(t, out, "deleted 1 records")
	assert.False(t, auditRowExists(t, owner, "it-old"))
	assert.True(t, auditRowExists(t, owner, "it-recent"))
}

func TestPostgres_RolesScript_CoversALaterMigrationAndChangesTheRetentionPeriod(t *testing.T) {
	owner := pgOwner(t)
	_, err := owner.Exec(t.Context(), `CREATE TABLE it_probe (a int)`) // stands in for a table added by a future migration
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pgOwner(t).Exec(context.Background(), `DROP TABLE IF EXISTS it_probe`)
		it.PG(t, "roles", "1")
	})
	it.PG(t, "roles", "1")

	app := pgApp(t)
	_, err = app.Exec(t.Context(), `INSERT INTO it_probe VALUES (1)`)
	require.NoError(t, err, "the application role has rights on the new table after the script ran again")
	var a int
	require.NoError(t, app.QueryRow(t.Context(), `SELECT a FROM it_probe`).Scan(&a))
	var n int
	assert.True(t, isPermissionDenied(app.QueryRow(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&n)), "still no SELECT on the audit table")
	assert.True(t, isPermissionDenied(pgCleanup(t).QueryRow(t.Context(), `SELECT a FROM it_probe`).Scan(&a)), "the cleanup role has no rights on the new table")

	// retention 0 means keep forever: the cleanup role then sees and deletes nothing
	old := time.Now().Add(-48 * time.Hour)
	insertAudit(t, owner, "it-keep", old)
	it.PG(t, "roles", "0")
	tag, err := pgCleanup(t).Exec(t.Context(), `DELETE FROM audit_events WHERE time = $1`, old.UnixMilli())
	require.NoError(t, err)
	assert.Zero(t, tag.RowsAffected())
	out := it.PG(t, "oneoff", "audit-cleanup", "KUBELOOK_AUDIT_CLEANUP_DATABASE_URL=postgres://kubelook_cleanup:cleanup@kubelook-it-postgres:5432/kubelook")
	assert.Contains(t, out, "deleted 0 records")
	assert.True(t, auditRowExists(t, owner, "it-keep"))
}

// This test replaces the key of both replicas.
func TestPostgres_ChangingTheEncryptionKey_SignsUsersOutAndDiscardsSavedCopiesWithoutLeakingAKey(t *testing.T) {
	c := pgSignedIn(t, "pg-key")
	sealed := c.AddVaultToken(it.Token("BOB_TOKEN"))
	oldKey := it.AppKey()

	raw := make([]byte, 32)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	newKey := base64.StdEncoding.EncodeToString(raw)
	// The new key stays: later tests sign in afresh, and hack/it-kubelook.sh install (make it-up) applies the key of
	// .it/app.env again.
	t.Log(it.Kubelook(t, "rekey", newKey))
	start := time.Now()

	// The spec: active-session credentials become unreadable, so every signed-in user is signed out. The first request
	// that reads the session's credential ends the session; after that the cookie is refused.
	it.Eventually(t, 30*time.Second, func() bool {
		return c.JSON(http.MethodGet, "/api/v1/clusters", nil, nil) == http.StatusUnauthorized
	}, "the old session is ended once the key changed")
	t.Logf("the old session was ended %s after the rekey returned", time.Since(start).Round(time.Second))
	assert.Equal(t, http.StatusUnauthorized, c.JSON(http.MethodGet, "/api/v1/me", nil, nil))

	again := it.NewClient(t)
	again.Base = it.Replica1URL
	again.Origin = it.BaseURL()
	final := again.SignInGoogle(it.Account{Sub: "pg-key", Email: "pg-key@corp.example", HD: "corp.example"})
	require.Empty(t, final.Query().Get("signin_error"), "%s", final)
	var res struct {
		Results []struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"results"`
	}
	require.Equal(t, http.StatusOK, again.JSON(http.MethodPost, "/api/v1/credentials/restore", map[string]any{"items": []map[string]string{{"sealed": sealed}}}, &res))
	require.Len(t, res.Results, 1)
	assert.Equal(t, "key_changed", res.Results[0].Status)
	assert.Contains(t, res.Results[0].Message, "encryption key was changed")

	var found bool
	for _, l := range it.AuditLines(t) {
		if l.Verb != "key-changed" {
			continue
		}
		found = true
		saved, _ := l.Details["savedKeyId"].(string)
		current, _ := l.Details["currentKeyId"].(string)
		assert.NotEmpty(t, saved)
		assert.NotEmpty(t, current)
		assert.NotEqual(t, saved, current, "two different key IDs")
		assert.NotContains(t, l.Raw, oldKey)
		assert.NotContains(t, l.Raw, newKey)
	}
	assert.True(t, found, "the audit output has a key-changed line")
	assert.False(t, strings.Contains(it.Logs(t), newKey), "the log never contains the key")
}
