package cluster_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/cluster"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakevault"
)

// twoClusters lets the Vault token reach kubernetes/a and kubernetes/b.
func (e *env) twoClusters() {
	caps := map[string][]string{}
	for _, n := range []string{"b", "a"} { // registered in reverse to prove sorting
		e.vault.AddMount("kubernetes/"+n+"/", "kubernetes")
		caps["kubernetes/"+n+"/config"] = []string{"read"}
		caps["kubernetes/"+n+"/creds/devops-role"] = []string{"update"}
	}
	e.vault.AddToken(vaultTok, fakevault.Token{TTLSeconds: 1800, Renewable: true, RenewTTLSeconds: 3600, EntityID: "ent-1", DisplayName: "oidc-ada", Caps: caps})
}

func idsOf(s cluster.Source) []string {
	var ids []string
	for _, c := range s.Clusters {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestList_NoCredentials_NoSources(t *testing.T) {
	e := newEnv(t)
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestList_VaultToken_OneSourceWithSortedClustersAndRoles(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "vault", got[0].Type)
	assert.Equal(t, "ok", got[0].Status)
	assert.Equal(t, []string{"v:a", "v:b"}, idsOf(got[0]))
	assert.Equal(t, cluster.Entry{ID: "v:a", Name: "a", Roles: []string{"devops-role"}, State: "closed"}, got[0].Clusters[0])
}

func TestList_Kubeconfig_SourcePerLabelWithContexts(t *testing.T) {
	e := newEnv(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "prod", tokenKubeconfig)
	require.NoError(t, err)
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "kubeconfig", got[0].Type)
	assert.Equal(t, "prod", got[0].Label)
	assert.Equal(t, "ok", got[0].Status)
	assert.Equal(t, []string{"k:prod/ctx1", "k:prod/ctx2"}, idsOf(got[0]))
	assert.Equal(t, "https://api.prod.example:6443", got[0].Clusters[0].Server)
	assert.Equal(t, "closed", got[0].Clusters[0].State)
}

func TestList_Both_VaultFirstThenKubeconfigsByLabel(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	for _, l := range []string{"zeta", "alpha"} {
		_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, l, tokenKubeconfig)
		require.NoError(t, err)
	}
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "vault", got[0].Type)
	assert.Equal(t, "alpha", got[1].Label)
	assert.Equal(t, "zeta", got[2].Label)
}

func TestList_DiscoveryIsCachedForAMinute_AndNeverFetchesConfigOrCreds(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	list := func() {
		_, err := e.reg.List(t.Context(), e.sess)
		require.NoError(t, err)
	}
	list()
	list()
	assert.Equal(t, 1, e.vault.Calls("mounts"))
	e.clock.Advance(59 * time.Second)
	list()
	assert.Equal(t, 1, e.vault.Calls("mounts"))
	e.clock.Advance(2 * time.Second)
	list()
	assert.Equal(t, 2, e.vault.Calls("mounts"))
	assert.Equal(t, 0, e.vault.Calls("config"), "credentials are fetched lazily")
	assert.Equal(t, 0, e.vault.Calls("creds"))
}

func TestList_AddingOrRenewingTheTokenInvalidatesTheCache(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	list := func() {
		_, err := e.reg.List(t.Context(), e.sess)
		require.NoError(t, err)
	}
	list()
	require.Equal(t, 1, e.vault.Calls("mounts"))
	e.add(t)
	list()
	assert.Equal(t, 2, e.vault.Calls("mounts"), "adding a token")
	_, err := e.c.RenewVaultToken(t.Context(), e.sess)
	require.NoError(t, err)
	list()
	assert.Equal(t, 3, e.vault.Calls("mounts"), "renewing the token")
	require.NoError(t, e.c.RemoveVaultToken(t.Context(), e.sess))
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	assert.Empty(t, got, "removing the token")
}

func TestList_VaultUnreachable_KeepsTheLastKnownClusters(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	_, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	_, _, err = e.c.AddKubeconfig(t.Context(), e.sess, "prod", tokenKubeconfig)
	require.NoError(t, err)

	e.clock.Advance(2 * time.Minute) // cache stale; the token itself is still valid (30 min)
	e.vault.HangUp(true)
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "unreachable", got[0].Status)
	assert.Equal(t, []string{"v:a", "v:b"}, idsOf(got[0]), "from the last successful discovery")
	assert.Equal(t, "ok", got[1].Status, "kubeconfig sources are unaffected")
}

func TestList_VaultUnreachableFromTheStart_NoClusters(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	e.vault.HangUp(true)
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "unreachable", got[0].Status)
	assert.Empty(t, got[0].Clusters)
}

func TestList_VaultTokenExpired_StatusExpiredAndClustersDisconnected(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	_, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	calls := e.vault.Calls("mounts")

	e.clock.Advance(31 * time.Minute)
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "expired", got[0].Status)
	require.Len(t, got[0].Clusters, 2)
	for _, c := range got[0].Clusters {
		assert.Equal(t, "disconnected", c.State)
	}
	assert.Equal(t, calls, e.vault.Calls("mounts"), "no Vault call for a token known to be expired")
}

func TestList_VaultRevokesTheToken_StatusExpired(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	e.vault.Forget(vaultTok)
	got, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "expired", got[0].Status)
}

func TestList_EndingTheSessionDropsItsCacheEntry(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	_, err := e.reg.List(t.Context(), e.sess)
	require.NoError(t, err)
	assert.Equal(t, 1, e.cache.Len())
	require.NoError(t, e.sessions.End(t.Context(), e.sess.Hash))
	assert.Equal(t, 0, e.cache.Len())
}

func TestResolve_VaultAndKubeconfigClusters(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "prod", tokenKubeconfig)
	require.NoError(t, err)

	ref, err := e.reg.Resolve(t.Context(), e.sess, "v:b")
	require.NoError(t, err)
	assert.Equal(t, cluster.Ref{ID: "v:b", Source: "vault", Mount: "kubernetes/b", Roles: []string{"devops-role"}}, ref)

	ref, err = e.reg.Resolve(t.Context(), e.sess, "k:prod/ctx2")
	require.NoError(t, err)
	assert.Equal(t, cluster.Ref{ID: "k:prod/ctx2", Source: "kubeconfig", Label: "prod", Context: "ctx2"}, ref)
}

func TestResolve_UnknownClusters_AreAllNotFoundWithTheSameMessage(t *testing.T) {
	e := newEnv(t)
	e.twoClusters()
	e.add(t)
	_, _, err := e.c.AddKubeconfig(t.Context(), e.sess, "prod", tokenKubeconfig)
	require.NoError(t, err)

	// a second session of another user with a kubeconfig of its own
	other, err := store.NewUsers(e.db, e.clock, func() string { return "u-other" }).Upsert(t.Context(), "google", "sub-2", "x@corp.example", "X")
	require.NoError(t, err)
	_, otherSess, err := e.sessions.Create(t.Context(), other.ID)
	require.NoError(t, err)
	_, _, err = e.c.AddKubeconfig(t.Context(), otherSess, "private", tokenKubeconfig)
	require.NoError(t, err)

	for _, id := range []string{"v:nope", "k:prod/nope", "k:nope/ctx1", "k:private/ctx1", "garbage", "", "v:", "k:"} {
		_, err := e.reg.Resolve(t.Context(), e.sess, id)
		require.ErrorIs(t, err, apperr.ErrNotFound, id)
		require.ErrorIs(t, err, apperr.ErrClusterUnavailable, id)
		assert.Equal(t, "This cluster isn't available in your session. If it comes from a kubeconfig, upload that kubeconfig first.", err.Error(), id)
	}
}

func TestResolve_VaultClusterWithoutAToken_NotFound(t *testing.T) {
	e := newEnv(t)
	_, err := e.reg.Resolve(t.Context(), e.sess, "v:a")
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestList_KubeconfigSealedWithAnotherKey_EndsSessionAnd401(t *testing.T) {
	e := newEnv(t)
	other, err := crypto.NewSealer(bytes.Repeat([]byte{9}, 32), testutil.Rand("o"))
	require.NoError(t, err)
	sealed, err := other.Seal([]byte(tokenKubeconfig), crypto.AAD(crypto.PurposeSessionCredential, e.sess.Hash))
	require.NoError(t, err)
	require.NoError(t, store.NewSessions(e.db).PutCredential(t.Context(), e.sess.Hash,
		store.CredentialRow{Kind: "kubeconfig", Ref: "work", Sealed: sealed}, e.clock.Now()))
	_, err = e.reg.List(t.Context(), e.sess)
	a := apiErr(t, err)
	assert.Equal(t, "key_changed", a.Code)
	_, gerr := store.NewSessions(e.db).Get(t.Context(), e.sess.Hash)
	require.ErrorIs(t, gerr, store.ErrNotFound)
}
