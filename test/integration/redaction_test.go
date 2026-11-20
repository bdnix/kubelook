//go:build integration

package integration

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

// secretPlaintext is the password in test/integration/testdata/manifests/base.yaml.
const secretPlaintext = "s3cret-it-value"

func TestRedaction_SecretsAreMaskedEverywhereButTheAuditedReveal(t *testing.T) {
	c := devopsClient(t, "redaction")
	secretPath := "/api/v1/clusters/v:alpha/resources/core/v1/secrets"

	// list and get
	for _, path := range []string{secretPath + "?namespace=demo", secretPath + "/credentials?namespace=demo"} {
		status, body := c.Do(http.MethodGet, path, nil, nil)
		require.Equal(t, http.StatusOK, status)
		assert.NotContains(t, string(body), secretPlaintext)
		assert.NotContains(t, string(body), base64.StdEncoding.EncodeToString([]byte(secretPlaintext)))
		assert.Contains(t, string(body), "********")
	}

	// the editor's dry run
	var obj map[string]any
	require.Equal(t, http.StatusOK, c.JSON(http.MethodGet, secretPath+"/credentials?namespace=demo", nil, &obj))
	status, body := c.Do(http.MethodPut, secretPath+"/credentials?namespace=demo&dryRun=true", mustJSON(t, obj), nil)
	assert.NotContains(t, string(body), secretPlaintext, "status %d", status)

	// watch
	ch := c.WS().Open("watch.open", map[string]any{"cluster": "v:alpha", "group": "core", "version": "v1", "resource": "secrets", "namespace": "demo"})
	assert.NotContains(t, string(ch.Next("snapshot", 30*time.Second)), secretPlaintext)

	// reveal returns the real value and is audited
	var revealed struct {
		Data map[string]string `json:"data"`
	}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodPost, "/api/v1/clusters/v:alpha/secrets/reveal", map[string]any{"namespace": "demo", "name": "credentials", "keys": []string{"password"}}, &revealed))
	raw, err := base64.StdEncoding.DecodeString(revealed.Data["password"])
	require.NoError(t, err)
	assert.Equal(t, secretPlaintext, string(raw))
	it.Eventually(t, 20*time.Second, func() bool {
		for _, l := range it.AuditLines(t, "kubelook-it-app") {
			if l.Verb == "secret-reveal" && l.Account["email"] == "redaction@corp.example" {
				assert.NotContains(t, l.Raw, secretPlaintext)
				return true
			}
		}
		return false
	}, "secret-reveal is audited")
}

func TestRedaction_ATLSSecretYieldsCertificateMetadataAndNoKeyMaterial(t *testing.T) {
	c := devopsClient(t, "redaction-tls")
	ns := it.Namespace(t, "alpha")
	dir := t.TempDir()
	openssl := func(args ...string) { runIn(t, dir, "openssl", args...) }
	openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", "tls.key", "-out", "tls.crt", "-subj", "/CN=it.example", "-days", "2")
	it.Kubectl(t, "alpha", "-n", ns, "create", "secret", "tls", "cert", "--cert="+dir+"/tls.crt", "--key="+dir+"/tls.key")

	status, body := c.Do(http.MethodPost, "/api/v1/clusters/v:alpha/secrets/certificates", mustJSON(t, map[string]string{"namespace": ns, "name": "cert"}), nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "it.example")
	assert.NotContains(t, string(body), "PRIVATE KEY")
	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Contains(t, out, "certificates")
}
