package vault_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/vault"
)

func TestEndpoint_NamesTheOperationNeverTheMountOrRole(t *testing.T) {
	cases := map[string]string{
		"/v1/auth/token/lookup-self":               "token_lookup",
		"/v1/auth/token/renew-self":                "token_renew",
		"/v1/sys/internal/ui/mounts":               "mounts",
		"/v1/sys/capabilities-self":                "capabilities",
		"/v1/kubernetes-prod/config":               "cluster_config",
		"/v1/team/payments/k8s/config":             "cluster_config",
		"/v1/kubernetes-prod/creds/payments-admin": "creds",
		"/v1/team/payments/k8s/creds/alice-role":   "creds",
		"/v1/something/else":                       "other",
		"/elsewhere":                               "other",
	}
	for path, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "https://vault.example"+path+"?x=secret", nil)
		got := vault.Endpoint(req)
		assert.Equal(t, want, got, path)
		assert.NotContains(t, got, "payments")
	}
}
