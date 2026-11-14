package cluster_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/cluster"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakevault"
	"github.com/bdnix/kubelook/internal/vault"
)

const (
	vaultTok     = "hvs.PLAINTEXT-VAULT-TOKEN"
	keyChangedTx = "Kubelook's encryption key was changed, so your saved Vault token and kubeconfigs can't be used anymore. Please paste your token and upload your kubeconfigs again."
)

type env struct {
	c        *cluster.Credentials
	vault    *fakevault.Server
	db       *store.DB
	clock    *clock.Fake
	sealer   *crypto.Sealer
	sessions *session.Manager
	sess     session.Session
	user     store.User
	logs     *bytes.Buffer
	auditOut *bytes.Buffer
	cfg      *config.Config
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{logs: &bytes.Buffer{}, auditOut: &bytes.Buffer{}}
	e.clock = clock.NewFake(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	e.vault = fakevault.New()
	t.Cleanup(e.vault.Close)
	e.vault.SetNow(e.clock.Now)
	e.vault.AddToken(vaultTok, fakevault.Token{
		TTLSeconds: 1800, Renewable: true, EntityID: "ent-1", DisplayName: "oidc-ada", Policies: []string{"default"},
		Caps: map[string][]string{"kubernetes/prod/config": {"read"}, "kubernetes/prod/creds/devops-role": {"update"}},
	})
	e.vault.AddMount("kubernetes/prod/", "kubernetes")

	e.db = storetest.New(t)
	n := 0
	newID := func() string { n++; return fmt.Sprintf("id-%d", n) }
	var err error
	e.user, err = store.NewUsers(e.db, e.clock, newID).Upsert(t.Context(), "google", "sub-1", "ada@corp.example", "Ada")
	require.NoError(t, err)
	log := slog.New(slog.NewJSONHandler(e.logs, nil))
	e.sessions = session.NewManager(session.Deps{
		Store: store.NewSessions(e.db), Clock: e.clock, Rand: testutil.Rand("sessions"), Log: log,
		IdleTimeout: 30 * time.Minute, MaxLifetime: 12 * time.Hour,
	})
	_, e.sess, err = e.sessions.Create(t.Context(), e.user.ID)
	require.NoError(t, err)
	e.sealer, err = crypto.NewSealer(make([]byte, 32), testutil.Rand("seal"))
	require.NoError(t, err)
	e.cfg = &config.Config{Vault: config.Vault{
		Address: e.vault.URL(), KubernetesMountPrefix: "kubernetes/", CandidateRoles: []string{"devops-role"},
		ExpiryWarning: config.Duration(30 * time.Minute),
	}}
	e.c = cluster.NewCredentials(cluster.Deps{
		Sessions: store.NewSessions(e.db), Sealer: e.sealer, Vault: e.vaultClient(), Clock: e.clock, Config: e.cfg,
		Audit: audit.NewRecorder(e.db, e.clock, e.auditOut, newID), Registry: e.sessions.Registry(), Ender: e.sessions, Log: log,
	})
	return e
}

func (e *env) vaultClient() *vault.Client { return vault.NewClient(e.vault.URL(), e.vault.Client()) }

func (e *env) openSaved(t *testing.T, sealed string) map[string]any {
	t.Helper()
	pt, err := e.sealer.Open(sealed, crypto.AAD(crypto.PurposeSavedCredential, e.user.ID))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(pt, &m))
	return m
}

func (e *env) count(t *testing.T, kind string) int {
	t.Helper()
	var n int
	require.NoError(t, e.db.QueryRow(t.Context(), "SELECT COUNT(*) FROM session_credentials WHERE session_hash = ? AND kind = ?", e.sess.Hash, kind).Scan(&n))
	return n
}

func (e *env) add(t *testing.T) string {
	t.Helper()
	sealed, _, err := e.c.AddVaultToken(t.Context(), e.sess, vaultTok)
	require.NoError(t, err)
	return sealed
}

func apiErr(t *testing.T, err error) *apperr.APIError {
	t.Helper()
	var a *apperr.APIError
	require.ErrorAs(t, err, &a)
	return a
}

func TestAdd_ValidToken_StoresSessionCredentialAndReturnsBrowserCopy(t *testing.T) {
	e := newEnv(t)
	sealed, st, err := e.c.AddVaultToken(t.Context(), e.sess, vaultTok)
	require.NoError(t, err)

	got := e.openSaved(t, sealed)
	assert.Equal(t, "vault_token", got["kind"])
	assert.Equal(t, vaultTok, got["token"])
	assert.NotEmpty(t, got["savedAt"])

	assert.Equal(t, "oidc-ada", st.DisplayName)
	assert.Equal(t, "ent-1", st.EntityID)
	assert.Equal(t, 1800, st.TTLSeconds)
	require.NotNil(t, st.ExpiresAt)
	assert.Equal(t, e.clock.Now().Add(30*time.Minute), *st.ExpiresAt)
	assert.True(t, st.Renewable)
	assert.True(t, st.CanRenew)
	assert.False(t, st.NeverExpires)
	assert.False(t, st.Warning, "1800 s is not below the 1800 s warning threshold")
	assert.False(t, st.Expired)

	row, err := store.NewSessions(e.db).GetCredential(t.Context(), e.sess.Hash, "vault_token", "")
	require.NoError(t, err)
	assert.NotContains(t, row.Sealed, vaultTok)
	_, err = e.sealer.Open(row.Sealed, crypto.AAD(crypto.PurposeSessionCredential, e.sess.Hash))
	require.NoError(t, err)
	assert.Equal(t, e.clock.Now().Add(30*time.Minute), row.ExpiresAt)
}

func TestAdd_RejectedToken_BadRequest(t *testing.T) {
	e := newEnv(t)
	_, _, err := e.c.AddVaultToken(t.Context(), e.sess, "wrong")
	a := apiErr(t, err)
	assert.Equal(t, 400, a.Status)
	assert.Equal(t, "bad_request", a.Code)
	assert.Contains(t, a.Message, "Vault rejected this token")
	assert.Equal(t, 0, e.count(t, "vault_token"))
}

func TestAdd_VaultDown_ServiceUnavailable(t *testing.T) {
	e := newEnv(t)
	e.vault.HangUp(true)
	_, _, err := e.c.AddVaultToken(t.Context(), e.sess, vaultTok)
	a := apiErr(t, err)
	assert.Equal(t, 503, a.Status)
	assert.Equal(t, "vault_unreachable", a.Code)
}

func TestAdd_PlaintextTokenNeverInLogsAuditOrErrors(t *testing.T) {
	e := newEnv(t)
	sealed := e.add(t)
	_, _, err := e.c.AddVaultToken(t.Context(), e.sess, vaultTok+"-bad")
	require.Error(t, err)
	e.c.Restore(t.Context(), e.sess, []string{sealed, "garbage"})
	_, err = e.c.RenewVaultToken(t.Context(), e.sess)
	require.NoError(t, err)
	assert.NotContains(t, e.logs.String(), vaultTok)
	assert.NotContains(t, e.auditOut.String(), vaultTok)
	assert.NotContains(t, err2s(err), vaultTok)
}

func err2s(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestRestore_OK_StoresTokenAndReturnsStatusAndClusters(t *testing.T) {
	e := newEnv(t)
	sealed := e.add(t)
	require.NoError(t, e.c.RemoveVaultToken(t.Context(), e.sess))
	require.Equal(t, 0, e.count(t, "vault_token"))

	res := e.c.Restore(t.Context(), e.sess, []string{sealed})
	require.Len(t, res, 1)
	assert.Equal(t, "ok", res[0].Status)
	assert.Equal(t, "vault_token", res[0].Kind)
	assert.Equal(t, "oidc-ada", res[0].Label)
	require.NotNil(t, res[0].Vault)
	assert.Equal(t, "ent-1", res[0].Vault.EntityID)
	require.Len(t, res[0].Clusters, 1)
	assert.Equal(t, "prod", res[0].Clusters[0].Name)
	assert.Equal(t, 1, e.count(t, "vault_token"))
}

func TestRestore_CopyOfAnotherUser_WrongUser_NothingStored(t *testing.T) {
	e := newEnv(t)
	foreign, err := e.sealer.Seal([]byte(`{"kind":"vault_token","token":"`+vaultTok+`","savedAt":"2026-11-14T12:00:00Z"}`), crypto.AAD(crypto.PurposeSavedCredential, "someone-else"))
	require.NoError(t, err)
	res := e.c.Restore(t.Context(), e.sess, []string{foreign})
	assert.Equal(t, "wrong_user", res[0].Status)
	assert.Equal(t, 0, e.count(t, "vault_token"))
}

func TestRestore_CopyFromAnotherKey_KeyChanged_OneAuditRecordWithBothKeyIDs(t *testing.T) {
	e := newEnv(t)
	other, err := crypto.NewSealer(bytes.Repeat([]byte{7}, 32), testutil.Rand("other"))
	require.NoError(t, err)
	var items []string
	for range 2 {
		sealed, err := other.Seal([]byte(`{}`), crypto.AAD(crypto.PurposeSavedCredential, e.user.ID))
		require.NoError(t, err)
		items = append(items, sealed)
	}
	res := e.c.Restore(t.Context(), e.sess, items)
	for _, r := range res {
		assert.Equal(t, "key_changed", r.Status)
		assert.Equal(t, keyChangedTx, r.Message)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(e.auditOut.String()), "\n") {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		lines = append(lines, m)
	}
	require.Len(t, lines, 1, "one record per restore call, not per item")
	assert.Equal(t, "key-changed", lines[0]["verb"])
	details := lines[0]["details"].(map[string]any)
	assert.Equal(t, e.sealer.KeyID(), details["currentKeyId"])
	assert.Equal(t, "4bb06f8e4e3a7715", details["savedKeyId"]) // hex(sha256(32 × 0x07))[:16]
}

func TestRestore_ExpiredGarbageAndUnsupported(t *testing.T) {
	e := newEnv(t)
	sealed := e.add(t)
	require.NoError(t, e.c.RemoveVaultToken(t.Context(), e.sess))

	expired, err := e.sealer.Seal([]byte(`{"kind":"vault_token","token":"gone","savedAt":"2026-11-14T12:00:00Z"}`), crypto.AAD(crypto.PurposeSavedCredential, e.user.ID))
	require.NoError(t, err)
	unsupported, err := e.sealer.Seal([]byte(`{"kind":"mystery"}`), crypto.AAD(crypto.PurposeSavedCredential, e.user.ID))
	require.NoError(t, err)
	notJSON, err := e.sealer.Seal([]byte(`not json`), crypto.AAD(crypto.PurposeSavedCredential, e.user.ID))
	require.NoError(t, err)

	res := e.c.Restore(t.Context(), e.sess, []string{expired, "garbage", unsupported, notJSON, sealed})
	require.Len(t, res, 5)
	assert.Equal(t, "expired", res[0].Status)
	assert.Contains(t, res[0].Message, "paste a new")
	assert.Equal(t, "invalid", res[1].Status)
	assert.Equal(t, "invalid", res[2].Status)
	assert.Equal(t, "invalid", res[3].Status)
	assert.Equal(t, "ok", res[4].Status)
}

func TestRestore_VaultDown_UnreachableAndTokenKept(t *testing.T) {
	e := newEnv(t)
	sealed := e.add(t)
	require.NoError(t, e.c.RemoveVaultToken(t.Context(), e.sess))
	e.vault.HangUp(true)

	res := e.c.Restore(t.Context(), e.sess, []string{sealed})
	assert.Equal(t, "vault_unreachable", res[0].Status)
	assert.Equal(t, 1, e.count(t, "vault_token"), "kept so discovery can be retried")

	e.vault.HangUp(false)
	tok, info, err := e.c.VaultToken(t.Context(), e.sess)
	require.NoError(t, err)
	assert.Equal(t, vaultTok, tok)
	assert.Equal(t, "ent-1", info.EntityID)
}

func TestRestore_DiscoveryUnreachable_TokenKept(t *testing.T) {
	e := newEnv(t)
	sealed := e.add(t)
	require.NoError(t, e.c.RemoveVaultToken(t.Context(), e.sess))
	failing := &discoveryDown{Client: e.vaultClient()}
	e.c = cluster.NewCredentials(cluster.Deps{
		Sessions: store.NewSessions(e.db), Sealer: e.sealer, Vault: failing, Clock: e.clock, Config: e.cfg,
		Audit: audit.NewRecorder(e.db, e.clock, e.auditOut, func() string { return "a" }), Registry: e.sessions.Registry(), Ender: e.sessions,
		Log: slog.New(slog.NewJSONHandler(e.logs, nil)),
	})
	res := e.c.Restore(t.Context(), e.sess, []string{sealed})
	assert.Equal(t, "vault_unreachable", res[0].Status)
	assert.Equal(t, 1, e.count(t, "vault_token"))
}

func TestRenew_Renewable_CallsRenewSelfAndReflectsNewTTL(t *testing.T) {
	e := newEnv(t)
	e.vault.AddToken("short", fakevault.Token{TTLSeconds: 600, Renewable: true, RenewTTLSeconds: 1800, EntityID: "e", DisplayName: "d"})
	_, _, err := e.c.AddVaultToken(t.Context(), e.sess, "short")
	require.NoError(t, err)

	st, err := e.c.RenewVaultToken(t.Context(), e.sess)
	require.NoError(t, err)
	assert.Equal(t, 1, e.vault.Calls("renew-self"))
	assert.Equal(t, 1800, st.TTLSeconds)
	assert.True(t, st.CanRenew)
}

func TestRenew_TTLNotIncreased_MaxReached_CanRenewFalse(t *testing.T) {
	e := newEnv(t)
	e.vault.AddToken("capped", fakevault.Token{TTLSeconds: 1800, Renewable: true, RenewTTLSeconds: 1800, EntityID: "e", DisplayName: "d"})
	_, _, err := e.c.AddVaultToken(t.Context(), e.sess, "capped")
	require.NoError(t, err)

	st, err := e.c.RenewVaultToken(t.Context(), e.sess)
	require.NoError(t, err)
	assert.False(t, st.CanRenew)

	again, err := e.c.VaultStatus(t.Context(), e.sess)
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.False(t, again.CanRenew, "remembered across requests")
}

func TestRenew_BatchOrNotRenewable_Conflict(t *testing.T) {
	for name, tok := range map[string]fakevault.Token{
		"batch":         {TTLSeconds: 600, Renewable: true, Type: "batch", EntityID: "e", DisplayName: "d"},
		"not renewable": {TTLSeconds: 600, Renewable: false, EntityID: "e", DisplayName: "d"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.vault.AddToken("t", tok)
			_, st, err := e.c.AddVaultToken(t.Context(), e.sess, "t")
			require.NoError(t, err)
			assert.False(t, st.CanRenew)
			_, err = e.c.RenewVaultToken(t.Context(), e.sess)
			require.ErrorIs(t, err, apperr.ErrConflict)
			assert.Equal(t, 0, e.vault.Calls("renew-self"))
		})
	}
}

func TestRenew_NoVaultToken_NotFound(t *testing.T) {
	e := newEnv(t)
	_, err := e.c.RenewVaultToken(t.Context(), e.sess)
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestRenew_ExpiredToken_Conflict(t *testing.T) {
	e := newEnv(t)
	e.add(t)
	e.vault.Forget(vaultTok)
	_, err := e.c.RenewVaultToken(t.Context(), e.sess)
	require.ErrorIs(t, err, apperr.ErrConflict)
}

func TestVaultToken_StatusAndDiscoveryNeverRenew(t *testing.T) {
	e := newEnv(t)
	e.add(t)
	_, _, err := e.c.VaultToken(t.Context(), e.sess)
	require.NoError(t, err)
	_, err = e.c.VaultStatus(t.Context(), e.sess)
	require.NoError(t, err)
	e.c.Restore(t.Context(), e.sess, []string{e.add(t)})
	assert.Equal(t, 0, e.vault.Calls("renew-self"))
}

func TestStatus_WarningNeverExpiresAndTTLFromClock(t *testing.T) {
	e := newEnv(t)
	e.vault.AddToken("t", fakevault.Token{TTLSeconds: 1200, Renewable: true, EntityID: "e", DisplayName: "d"})
	_, st, err := e.c.AddVaultToken(t.Context(), e.sess, "t")
	require.NoError(t, err)
	assert.True(t, st.Warning, "0 < ttl < expiryWarning")

	e.clock.Advance(5 * time.Minute)
	e.vault.SetTTL("t", 1200) // the number Vault reports is unchanged; the expiry is what counts
	got, err := e.c.VaultStatus(t.Context(), e.sess)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1200, got.TTLSeconds, "lookup-self now reports 1200 again with a fresh expire_time")

	e.vault.AddToken("root", fakevault.Token{TTLSeconds: 0, Renewable: false, EntityID: "r", DisplayName: "root"})
	_, st, err = e.c.AddVaultToken(t.Context(), e.sess, "root")
	require.NoError(t, err)
	assert.True(t, st.NeverExpires)
	assert.False(t, st.Warning)
	assert.False(t, st.CanRenew)
	assert.Nil(t, st.ExpiresAt)
}

func TestStatus_TTLComputedFromExpireTimeAndClock(t *testing.T) {
	e := newEnv(t)
	e.vault.AddToken("fixed", fakevault.Token{
		TTLSeconds: 9999, Renewable: true, EntityID: "e", DisplayName: "d",
		ExpireTime: e.clock.Now().Add(10 * time.Minute).Format(time.RFC3339),
	})
	_, st, err := e.c.AddVaultToken(t.Context(), e.sess, "fixed")
	require.NoError(t, err)
	assert.Equal(t, 600, st.TTLSeconds, "from expire_time and the clock, not the stale ttl number")
	assert.True(t, st.Warning)

	e.clock.Advance(11 * time.Minute)
	got, err := e.c.VaultStatus(t.Context(), e.sess)
	require.NoError(t, err)
	assert.True(t, got.Expired)
	assert.Equal(t, 0, got.TTLSeconds)
	assert.False(t, got.Warning)
}

func TestStatus_NoToken_Nil(t *testing.T) {
	e := newEnv(t)
	st, err := e.c.VaultStatus(t.Context(), e.sess)
	require.NoError(t, err)
	assert.Nil(t, st)
}

func TestStatus_VaultRejectsStoredToken_ExpiredStatus(t *testing.T) {
	e := newEnv(t)
	e.add(t)
	e.vault.Forget(vaultTok)
	st, err := e.c.VaultStatus(t.Context(), e.sess)
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.True(t, st.Expired)
	assert.Equal(t, "oidc-ada", st.DisplayName)
}

func TestRemove_DeletesTokenAndVaultClusterCredentialsAndClosesVaultStreams(t *testing.T) {
	e := newEnv(t)
	e.add(t)
	ss := store.NewSessions(e.db)
	for _, c := range []store.CredentialRow{
		{Kind: "cluster_cred", Ref: "v:prod"}, {Kind: "cluster_role", Ref: "v:prod"},
		{Kind: "cluster_cred", Ref: "k:local/dev"}, {Kind: "kubeconfig", Ref: "local"},
	} {
		c.Sealed = "x"
		require.NoError(t, ss.PutCredential(t.Context(), e.sess.Hash, c, e.clock.Now()))
	}
	closedV, closedK := 0, 0
	e.sessions.Registry().Register(e.sess.Hash, "v:prod/logs", func() error { closedV++; return nil })
	e.sessions.Registry().Register(e.sess.Hash, "k:local/dev/logs", func() error { closedK++; return nil })

	require.NoError(t, e.c.RemoveVaultToken(t.Context(), e.sess))
	assert.Equal(t, 0, e.count(t, "vault_token"))
	assert.Equal(t, 1, e.count(t, "cluster_cred"), "only the kubeconfig cluster's credential stays")
	assert.Equal(t, 0, e.count(t, "cluster_role"))
	assert.Equal(t, 1, e.count(t, "kubeconfig"))
	assert.Equal(t, 1, closedV)
	assert.Equal(t, 0, closedK)
}

func TestRemove_NothingStored_NoError(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.c.RemoveVaultToken(t.Context(), e.sess))
}

func TestVaultIdentity_FromTheStoredCredential(t *testing.T) {
	e := newEnv(t)
	entity, name := e.c.VaultIdentity(t.Context(), e.sess)
	assert.Empty(t, entity)
	assert.Empty(t, name)

	e.add(t)
	entity, name = e.c.VaultIdentity(t.Context(), e.sess)
	assert.Equal(t, "ent-1", entity)
	assert.Equal(t, "oidc-ada", name)
}

func TestVaultToken_SessionCredentialSealedWithAnotherKey_EndsSessionAnd401KeyChanged(t *testing.T) {
	e := newEnv(t)
	other, err := crypto.NewSealer(bytes.Repeat([]byte{9}, 32), testutil.Rand("o"))
	require.NoError(t, err)
	sealed, err := other.Seal([]byte(`{"token":"x"}`), crypto.AAD(crypto.PurposeSessionCredential, e.sess.Hash))
	require.NoError(t, err)
	require.NoError(t, store.NewSessions(e.db).PutCredential(t.Context(), e.sess.Hash,
		store.CredentialRow{Kind: "vault_token", Sealed: sealed}, e.clock.Now()))

	_, _, err = e.c.VaultToken(t.Context(), e.sess)
	a := apiErr(t, err)
	assert.Equal(t, 401, a.Status)
	assert.Equal(t, "key_changed", a.Code)
	_, gerr := store.NewSessions(e.db).Get(t.Context(), e.sess.Hash)
	require.ErrorIs(t, gerr, store.ErrNotFound, "the session was ended")
}

func TestVaultToken_CorruptSessionCredential_Error(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, store.NewSessions(e.db).PutCredential(t.Context(), e.sess.Hash,
		store.CredentialRow{Kind: "vault_token", Sealed: "garbage"}, e.clock.Now()))
	_, _, err := e.c.VaultToken(t.Context(), e.sess)
	require.Error(t, err)
	var a *apperr.APIError
	assert.NotErrorAs(t, err, &a, "corruption is an internal error, not a key change")
}

// discoveryDown answers the token calls normally and fails Mounts as unreachable.
type discoveryDown struct{ *vault.Client }

func (d *discoveryDown) Mounts(context.Context, string) (map[string]string, error) {
	return nil, fmt.Errorf("listing: %w", vault.ErrUnreachable)
}

func TestAdd_VaultAnswersWithAnError_GenericErrorNotATokenRejection(t *testing.T) {
	e := newEnv(t)
	e.vault.ForceStatus(400)
	_, _, err := e.c.AddVaultToken(t.Context(), e.sess, vaultTok)
	require.Error(t, err)
	var a *apperr.APIError
	assert.NotErrorAs(t, err, &a)

	res := e.c.Restore(t.Context(), e.sess, []string{e.sealedCopy(t)})
	assert.Equal(t, "invalid", res[0].Status)
}

func (e *env) sealedCopy(t *testing.T) string {
	t.Helper()
	s, err := e.sealer.Seal([]byte(`{"kind":"vault_token","token":"`+vaultTok+`","savedAt":"2026-11-14T12:00:00Z"}`), crypto.AAD(crypto.PurposeSavedCredential, e.user.ID))
	require.NoError(t, err)
	return s
}

func TestRestore_DiscoveryFailsForAnotherReason_Invalid(t *testing.T) {
	e := newEnv(t)
	e.c = cluster.NewCredentials(cluster.Deps{
		Sessions: store.NewSessions(e.db), Sealer: e.sealer, Vault: &discoveryBroken{Client: e.vaultClient()}, Clock: e.clock, Config: e.cfg,
		Audit: audit.NewRecorder(e.db, e.clock, e.auditOut, func() string { return "a" }), Registry: e.sessions.Registry(), Ender: e.sessions,
		Log: slog.New(slog.NewJSONHandler(e.logs, nil)),
	})
	res := e.c.Restore(t.Context(), e.sess, []string{e.sealedCopy(t)})
	assert.Equal(t, "invalid", res[0].Status)
	assert.Contains(t, e.logs.String(), "discovering clusters")
}

type discoveryBroken struct{ *vault.Client }

func (d *discoveryBroken) Mounts(context.Context, string) (map[string]string, error) {
	return nil, fmt.Errorf("boom")
}

const tokenKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: prod-cluster
  cluster: {server: "https://api.prod.example:6443", certificate-authority-data: Y2EtZGF0YQ==}
users:
- name: ada
  user: {token: static-token}
contexts:
- name: ctx1
  context: {cluster: prod-cluster, user: ada}
- name: ctx2
  context: {cluster: prod-cluster, user: ada}
`

const execKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "https://api.example:6443"}
users:
- name: eks-user
  user:
    exec: {apiVersion: client.authentication.k8s.io/v1beta1, command: aws}
contexts:
- name: ctx
  context: {cluster: c, user: eks-user}
`

func TestKubeconfig_Add_StoresSessionCredentialAndReturnsBrowserCopy(t *testing.T) {
	e := newEnv(t)
	sealed, clusters, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", tokenKubeconfig)
	require.NoError(t, err)

	got := e.openSaved(t, sealed)
	assert.Equal(t, "kubeconfig", got["kind"])
	assert.Equal(t, "work", got["label"])
	assert.Equal(t, tokenKubeconfig, got["content"])

	require.Len(t, clusters, 2)
	assert.Equal(t, cluster.ClusterInfo{
		ID: "k:work/ctx1", Name: "prod-cluster", Context: "ctx1", User: "ada", Server: "https://api.prod.example:6443",
	}, clusters[0])
	assert.Equal(t, "k:work/ctx2", clusters[1].ID)

	row, err := store.NewSessions(e.db).GetCredential(t.Context(), e.sess.Hash, "kubeconfig", "work")
	require.NoError(t, err)
	assert.NotContains(t, row.Sealed, "static-token")
	pt, err := e.sealer.Open(row.Sealed, crypto.AAD(crypto.PurposeSessionCredential, e.sess.Hash))
	require.NoError(t, err)
	assert.Equal(t, tokenKubeconfig, string(pt))
	assert.NotContains(t, e.logs.String(), "static-token")
	assert.NotContains(t, e.auditOut.String(), "static-token")
}

func TestKubeconfig_Add_Rejected_NothingStored(t *testing.T) {
	e := newEnv(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", execKubeconfig)
	require.ErrorIs(t, err, apperr.ErrInvalid)
	assert.Contains(t, err.Error(), "exec credential plugins are not supported")
	assert.Equal(t, 0, e.count(t, "kubeconfig"))

	_, _, err = e.c.AddKubeconfig(t.Context(), e.sess, "a/b", tokenKubeconfig)
	require.ErrorIs(t, err, apperr.ErrInvalid)
}

func TestKubeconfig_Add_ExistingLabel_ReplacesAndDropsItsClusterCredentialsAndConnections(t *testing.T) {
	e := newEnv(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", tokenKubeconfig)
	require.NoError(t, err)
	ss := store.NewSessions(e.db)
	require.NoError(t, ss.PutCredential(t.Context(), e.sess.Hash, store.CredentialRow{Kind: "cluster_cred", Ref: "k:work/ctx1", Sealed: "x"}, e.clock.Now()))
	require.NoError(t, ss.PutCredential(t.Context(), e.sess.Hash, store.CredentialRow{Kind: "cluster_cred", Ref: "k:other/ctx1", Sealed: "x"}, e.clock.Now()))
	closed := 0
	e.sessions.Registry().Register(e.sess.Hash, "k:work/ctx1/logs", func() error { closed++; return nil })

	_, _, err = e.c.AddKubeconfig(t.Context(), e.sess, "work", strings.ReplaceAll(tokenKubeconfig, "ctx2", "ctx3"))
	require.NoError(t, err)
	assert.Equal(t, 1, e.count(t, "kubeconfig"))
	assert.Equal(t, 1, e.count(t, "cluster_cred"), "only the other label's credential stays")
	assert.Equal(t, 1, closed)
}

func TestKubeconfig_Remove_DeletesCredentialClusterRowsAndClosesConnections(t *testing.T) {
	e := newEnv(t)
	for _, l := range []string{"work", "home"} {
		_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, l, tokenKubeconfig)
		require.NoError(t, err)
	}
	ss := store.NewSessions(e.db)
	for _, ref := range []string{"k:work/ctx1", "k:home/ctx1", "v:prod"} {
		require.NoError(t, ss.PutCredential(t.Context(), e.sess.Hash, store.CredentialRow{Kind: "cluster_cred", Ref: ref, Sealed: "x"}, e.clock.Now()))
	}
	closedWork, closedHome := 0, 0
	e.sessions.Registry().Register(e.sess.Hash, "k:work/ctx1/shell", func() error { closedWork++; return nil })
	e.sessions.Registry().Register(e.sess.Hash, "k:home/ctx1/shell", func() error { closedHome++; return nil })

	require.NoError(t, e.c.RemoveKubeconfig(t.Context(), e.sess, "work"))
	assert.Equal(t, 1, e.count(t, "kubeconfig"))
	assert.Equal(t, 2, e.count(t, "cluster_cred"))
	assert.Equal(t, 1, closedWork)
	assert.Equal(t, 0, closedHome)
	require.NoError(t, e.c.RemoveKubeconfig(t.Context(), e.sess, "unknown"), "removing what is not there is fine")
}

func TestKubeconfig_Restore_ReparsesTheCopy(t *testing.T) {
	e := newEnv(t)
	sealed, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", tokenKubeconfig)
	require.NoError(t, err)
	require.NoError(t, e.c.RemoveKubeconfig(t.Context(), e.sess, "work"))

	res := e.c.Restore(t.Context(), e.sess, []string{sealed})
	assert.Equal(t, "ok", res[0].Status)
	assert.Equal(t, "kubeconfig", res[0].Kind)
	assert.Equal(t, "work", res[0].Label)
	require.Len(t, res[0].Clusters, 2)
	assert.Equal(t, "k:work/ctx1", res[0].Clusters[0].ID)
	assert.Equal(t, 1, e.count(t, "kubeconfig"))
}

func TestKubeconfig_Restore_CopyThatNoLongerParses_InvalidWithMessage(t *testing.T) {
	e := newEnv(t)
	bad, err := e.sealer.Seal([]byte(`{"kind":"kubeconfig","label":"work","content":`+jsonString(execKubeconfig)+`,"savedAt":"2026-11-14T12:00:00Z"}`),
		crypto.AAD(crypto.PurposeSavedCredential, e.user.ID))
	require.NoError(t, err)
	res := e.c.Restore(t.Context(), e.sess, []string{bad})
	assert.Equal(t, "invalid", res[0].Status)
	assert.Contains(t, res[0].Message, "exec credential plugins are not supported")
	assert.Equal(t, 0, e.count(t, "kubeconfig"))
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestKubeconfig_Disabled_AddIsNotFoundAndRestoreInvalid(t *testing.T) {
	e := newEnv(t)
	sealed, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "work", tokenKubeconfig)
	require.NoError(t, err)
	no := false
	e.cfg.Kubeconfig.Enabled = &no

	_, _, err = e.c.AddKubeconfig(t.Context(), e.sess, "again", tokenKubeconfig)
	require.ErrorIs(t, err, apperr.ErrNotFound)
	res := e.c.Restore(t.Context(), e.sess, []string{sealed})
	assert.Equal(t, "invalid", res[0].Status)
	assert.Contains(t, res[0].Message, "disabled")
}
