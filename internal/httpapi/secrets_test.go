package httpapi_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

const (
	revealPath = "/api/v1/clusters/v:prod/secrets/reveal"
	certsPath  = "/api/v1/clusters/v:prod/secrets/certificates"
)

func TestReveal_ReturnsRequestedKeysAuditedFirstAndNotCached(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"db","keys":["password"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"data":{"password":"c2VjcmV0"}}`, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	lines := e.auditLines(t)
	require.Len(t, lines, 2)
	assert.Equal(t, "secret-reveal", lines[0]["verb"])
	assert.Equal(t, "requested", lines[0]["outcome"])
	assert.Equal(t, map[string]any{"keys": []any{"password"}}, map[string]any{"keys": lines[0]["details"].(map[string]any)["keys"]})
	assert.NotContains(t, e.auditOut.String(), "c2VjcmV0")
	assert.NotContains(t, e.logs.String(), "c2VjcmV0")
}

func TestReveal_AllKeysWhenNoneAreNamed(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"db"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":{"password":"c2VjcmV0"}}`, rec.Body.String())
	assert.Equal(t, "all", e.auditLines(t)[0]["details"].(map[string]any)["keys"])
}

func TestReveal_Errors(t *testing.T) {
	e := newWriteEnv(t)
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"db","keys":["nope"]}`).Code)
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"sh.helm.release.v1.web.v1"}`).Code)
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"missing"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodPost, revealPath, "application/json", `{"name":"db"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"db","extra":1}`).Code)
}

func TestReveal_AuditFailure_NoRead(t *testing.T) {
	e := newWriteEnv(t)
	breakAudit(t, e)
	reads := 0
	e.fakes.Dyn.PrependReactor("get", "secrets", func(clienttesting.Action) (bool, runtime.Object, error) { reads++; return false, nil, nil })
	rec := e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"db"}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Zero(t, reads)
	assert.NotContains(t, rec.Body.String(), "c2VjcmV0")
}

func TestReveal_ForbiddenByTheClusterShowsTheMessage(t *testing.T) {
	e := newWriteEnv(t)
	e.fakes.Dyn.PrependReactor("get", "secrets", forbidden("secrets is forbidden"))
	rec := e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"db"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "secrets is forbidden")
}

func TestCertificates_AuditedAndMetadataOnly(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPost, certsPath, "application/json", `{"namespace":"ns1","name":"db"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"certificates":[]}`, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, []string{"certificate-view:requested", "certificate-view:success"}, e.verbsAndOutcomes(t))
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, certsPath, "application/json", `{"namespace":"ns1","name":"sh.helm.release.v1.web.v1"}`).Code)
}

func TestSecrets_NotMountedWithoutAudit(t *testing.T) {
	e := newWriteEnv(t)
	e.deps.Audit = nil
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, revealPath, "application/json", `{"namespace":"ns1","name":"db"}`).Code)
}

func TestCertificates_ListsTheCertificatesOfASecretWithTheRemainingLifetime(t *testing.T) {
	e := newWriteEnv(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "web.example"}, NotBefore: t0.Add(-time.Hour), NotAfter: t0.Add(48 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	require.NoError(t, err)
	crt := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, e.fakes.Dyn.Tracker().Add(fakekube.Secret("ns1", "tls", map[string]any{"tls.crt": base64.StdEncoding.EncodeToString(crt)})))
	rec := e.sendAs(http.MethodPost, certsPath, "application/json", `{"namespace":"ns1","name":"tls"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Certificates []struct {
			Key, Subject     string
			ExpiresInSeconds int64
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Certificates, 1)
	assert.Equal(t, "tls.crt", got.Certificates[0].Key)
	assert.Equal(t, "CN=web.example", got.Certificates[0].Subject)
	assert.EqualValues(t, 48*3600, got.Certificates[0].ExpiresInSeconds)
}
