//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

// The audit records are also written to standard output as JSON, which is where this test reads them.
func TestAudit_EveryMutationHasARequestedAndAnOutcomeRecordWithTheAccountAndTheVaultIdentity(t *testing.T) {
	c := devopsClient(t, "audit-complete")
	ns := deploy(t, "audited")
	dep := target("deployments", "apps", ns, "audited")
	status, apiErr := doAction(t, c, "scale", dep, map[string]int{"replicas": 2})
	require.Equal(t, http.StatusOK, status, "%+v", apiErr)
	it.Kubectl(t, "alpha", "-n", ns, "create", "configmap", "audit-cm", "--from-literal=a=1")
	var obj map[string]any
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, cmPath(ns, "audit-cm", ""), nil, &obj))
	setData(obj, "a", "2")
	require.Equal(t, http.StatusOK, c.JSON(http.MethodPut, cmPath(ns, "audit-cm", ""), obj, nil))

	it.Eventually(t, 20*time.Second, func() bool {
		seen := map[string]map[string]bool{}
		for _, l := range it.AuditLines(t, "kubelook-it-app") {
			if l.Account["email"] != "audit-complete@corp.example" || l.Verb == "vault-credential-issued" || l.Verb == "sign-in" {
				continue
			}
			if seen[l.Verb] == nil {
				seen[l.Verb] = map[string]bool{}
			}
			seen[l.Verb][l.Outcome] = true
			assert.NotEmpty(t, l.Account["provider"], "the account provider is filled in")
			assert.NotEmpty(t, l.Vault["displayName"], "the Vault identity is filled in")
		}
		for _, verb := range []string{"scale", "update"} {
			if !seen[verb]["requested"] || (!seen[verb]["success"] && !seen[verb]["failure"]) {
				return false
			}
		}
		return true
	}, "scale and update each have a requested record and an outcome record")
}
