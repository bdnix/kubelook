package vault_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/testutil/fakevault"
	"github.com/bdnix/kubelook/internal/vault"
)

// memAPI is an in-memory vault.API.
type memAPI struct {
	mounts     map[string]string
	caps       map[string][]string
	mountsErr  error
	capsCalled [][]string
}

func (m *memAPI) LookupSelf(context.Context, string) (vault.TokenInfo, error) {
	return vault.TokenInfo{}, nil
}
func (m *memAPI) RenewSelf(context.Context, string) (time.Duration, error) { return 0, nil }
func (m *memAPI) Mounts(context.Context, string) (map[string]string, error) {
	return m.mounts, m.mountsErr
}
func (m *memAPI) Capabilities(_ context.Context, _ string, paths []string) (map[string][]string, error) {
	m.capsCalled = append(m.capsCalled, paths)
	out := map[string][]string{}
	for _, p := range paths {
		if c, ok := m.caps[p]; ok {
			out[p] = c
		}
	}
	return out, nil
}
func (m *memAPI) ClusterConfig(context.Context, string, string) (vault.ClusterConfig, error) {
	return vault.ClusterConfig{}, nil
}
func (m *memAPI) Creds(context.Context, string, string, string, vault.CredsRequest) (vault.Creds, error) {
	return vault.Creds{}, nil
}

var roles = []string{"devops-role", "platform-hub-role"}

func discover(t *testing.T, api vault.API) ([]vault.Cluster, error) {
	t.Helper()
	return vault.Discover(t.Context(), api, "tok", "kubernetes/", roles)
}

func prodAPI(caps map[string][]string) *memAPI {
	return &memAPI{mounts: map[string]string{"kubernetes/prod/": "kubernetes"}, caps: caps}
}

func TestDiscover_Cases(t *testing.T) {
	cfg := "kubernetes/prod/config"
	dev := "kubernetes/prod/creds/devops-role"
	hub := "kubernetes/prod/creds/platform-hub-role"
	cases := []struct {
		name string
		caps map[string][]string
		want []vault.Cluster
	}{
		{"read config + update on first role", map[string][]string{cfg: {"read"}, dev: {"update"}},
			[]vault.Cluster{{Name: "prod", Mount: "kubernetes/prod", Roles: []string{"devops-role"}}}},
		{"both roles allowed, configured order", map[string][]string{cfg: {"read"}, dev: {"update"}, hub: {"update"}},
			[]vault.Cluster{{Name: "prod", Mount: "kubernetes/prod", Roles: []string{"devops-role", "platform-hub-role"}}}},
		{"only second role", map[string][]string{cfg: {"read"}, hub: {"update"}},
			[]vault.Cluster{{Name: "prod", Mount: "kubernetes/prod", Roles: []string{"platform-hub-role"}}}},
		{"create only on creds", map[string][]string{cfg: {"read"}, dev: {"create"}}, nil},
		{"config denied", map[string][]string{cfg: {"deny"}, dev: {"update"}}, nil},
		{"no capabilities on config", map[string][]string{cfg: {}, dev: {"update"}}, nil},
		{"root implies everything", map[string][]string{cfg: {"root"}, dev: {"root"}},
			[]vault.Cluster{{Name: "prod", Mount: "kubernetes/prod", Roles: []string{"devops-role"}}}},
		{"deny wins over update", map[string][]string{cfg: {"read"}, dev: {"deny", "update"}}, nil},
		{"sudo and list only", map[string][]string{cfg: {"sudo", "list"}, dev: {"sudo", "list"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := discover(t, prodAPI(tc.caps))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDiscover_IgnoresOtherMounts(t *testing.T) {
	api := &memAPI{
		mounts: map[string]string{
			"kubernetes/prod/": "kubernetes", "kubernetes/kv/": "kv", "k8s/other/": "kubernetes", "secret/": "kv",
		},
		caps: map[string][]string{
			"kubernetes/prod/config": {"read"}, "kubernetes/prod/creds/devops-role": {"update"},
			"k8s/other/config": {"read"}, "k8s/other/creds/devops-role": {"update"},
			"kubernetes/kv/config": {"read"}, "kubernetes/kv/creds/devops-role": {"update"},
		},
	}
	got, err := discover(t, api)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "prod", got[0].Name)
	for _, call := range api.capsCalled {
		for _, p := range call {
			assert.NotContains(t, p, "k8s/")
			assert.NotContains(t, p, "kubernetes/kv/")
		}
	}
}

func TestDiscover_NestedName(t *testing.T) {
	api := &memAPI{
		mounts: map[string]string{"kubernetes/eu/prod/": "kubernetes"},
		caps:   map[string][]string{"kubernetes/eu/prod/config": {"read"}, "kubernetes/eu/prod/creds/devops-role": {"update"}},
	}
	got, err := discover(t, api)
	require.NoError(t, err)
	assert.Equal(t, []vault.Cluster{{Name: "eu/prod", Mount: "kubernetes/eu/prod", Roles: []string{"devops-role"}}}, got)
}

func TestDiscover_NoCandidateRoles(t *testing.T) {
	_, err := vault.Discover(t.Context(), prodAPI(nil), "tok", "kubernetes/", nil)
	require.ErrorIs(t, err, vault.ErrNoCandidateRoles)
}

func TestDiscover_NoMounts_EmptyResultNoCapabilityCall(t *testing.T) {
	api := &memAPI{mounts: map[string]string{"secret/": "kv"}}
	got, err := discover(t, api)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Empty(t, api.capsCalled)
}

func TestDiscover_250Mounts_OneMountsCall_SortedResult(t *testing.T) {
	f := fakevault.New()
	defer f.Close()
	caps := map[string][]string{}
	for i := range 250 {
		name := fmt.Sprintf("c%03d", 249-i) // registered in reverse to prove sorting
		f.AddMount("kubernetes/"+name+"/", "kubernetes")
		caps["kubernetes/"+name+"/config"] = []string{"read"}
		caps["kubernetes/"+name+"/creds/devops-role"] = []string{"update"}
	}
	f.AddToken("tok", fakevault.Token{Caps: caps})

	got, err := vault.Discover(t.Context(), vault.NewClient(f.URL(), f.Client()), "tok", "kubernetes/", roles)
	require.NoError(t, err)
	require.Len(t, got, 250)
	assert.Equal(t, "c000", got[0].Name)
	assert.Equal(t, "c249", got[249].Name)
	assert.Equal(t, 1, f.Calls("mounts"))
	assert.Equal(t, 8, f.Calls("capabilities"), "ceil(250*3/100)")
}

func TestDiscover_VaultUnreachable_WrapsErrUnreachable(t *testing.T) {
	f := fakevault.New()
	defer f.Close()
	f.HangUp(true)
	_, err := vault.Discover(t.Context(), vault.NewClient(f.URL(), f.Client()), "tok", "kubernetes/", roles)
	require.ErrorIs(t, err, vault.ErrUnreachable)
}

func TestDiscover_CapabilitiesFail_ErrorReturned(t *testing.T) {
	api := &failingCaps{memAPI: *prodAPI(nil)}
	_, err := discover(t, api)
	require.Error(t, err)
}

type failingCaps struct{ memAPI }

func (f *failingCaps) Capabilities(context.Context, string, []string) (map[string][]string, error) {
	return nil, fmt.Errorf("boom")
}
