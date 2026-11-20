package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
)

const issuer = "http://kubelook-it-fakeidp:8300/oidc"

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(newIDP("http://kubelook-it-fakeidp:8300", clock.Real()).handler())
	t.Cleanup(s.Close)
	return s
}

func postJSON(t *testing.T, s *httptest.Server, path, body string) {
	t.Helper()
	resp, err := http.Post(s.URL+path, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorize drives the authorize endpoint and returns the code and state it redirects with.
func authorize(t *testing.T, s *httptest.Server, path, verifier string) (code, state string) {
	t.Helper()
	q := url.Values{
		"client_id": {"kubelook-it"}, "redirect_uri": {"http://localhost:18080/cb"}, "state": {"st-1"},
		"response_type": {"code"}, "code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"},
	}
	resp, err := noRedirect().Get(s.URL + path + "?" + q.Encode())
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "localhost:18080", loc.Host)
	return loc.Query().Get("code"), loc.Query().Get("state")
}

func exchange(t *testing.T, s *httptest.Server, path, code, verifier string) (status int, out map[string]any) {
	t.Helper()
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier},
		"client_id": {"kubelook-it"}, "client_secret": {"it-secret"}, "redirect_uri": {"http://localhost:18080/cb"},
	}
	req, err := http.NewRequest(http.MethodPost, s.URL+path, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func decodeJWT(t *testing.T, raw string) (header, claims map[string]any, signingInput string, sig []byte) {
	t.Helper()
	parts := strings.Split(raw, ".")
	require.Len(t, parts, 3)
	for i, dst := range []*map[string]any{&header, &claims} {
		b, err := base64.RawURLEncoding.DecodeString(parts[i])
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(b, dst))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	return header, claims, parts[0] + "." + parts[1], sig
}

func fetchKey(t *testing.T, s *httptest.Server, kid string) *rsa.PublicKey {
	t.Helper()
	resp, err := http.Get(s.URL + "/oidc/keys")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var set struct {
		Keys []struct{ Kid, Kty, Alg, N, E string } `json:"keys"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&set))
	for _, k := range set.Keys {
		if k.Kid != kid {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		require.NoError(t, err)
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		require.NoError(t, err)
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	t.Fatalf("no key %q", kid)
	return nil
}

func TestHealthz_Returns200(t *testing.T) {
	s := newServer(t)
	resp, err := http.Get(s.URL + "/healthz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDiscovery_HasIssuerAndEndpoints(t *testing.T) {
	s := newServer(t)
	resp, err := http.Get(s.URL + "/oidc/.well-known/openid-configuration")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var doc map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))
	assert.Equal(t, issuer, doc["issuer"])
	assert.Equal(t, issuer+"/authorize", doc["authorization_endpoint"])
	assert.Equal(t, issuer+"/token", doc["token_endpoint"])
	assert.Equal(t, issuer+"/keys", doc["jwks_uri"])
	assert.Contains(t, doc["id_token_signing_alg_values_supported"], "RS256")
}

func TestOIDC_CodeAndPKCE_YieldsSignedIDToken(t *testing.T) {
	s := newServer(t)
	postJSON(t, s, "/admin/next", `{"sub":"42","email":"a@corp.example","name":"Ada","hd":"corp.example","email_verified":true}`)
	code, state := authorize(t, s, "/oidc/authorize", "verifier-1")
	assert.Equal(t, "st-1", state)

	status, out := exchange(t, s, "/oidc/token", code, "verifier-1")
	require.Equal(t, http.StatusOK, status)
	raw, _ := out["id_token"].(string)
	header, claims, signingInput, sig := decodeJWT(t, raw)
	assert.Equal(t, "RS256", header["alg"])
	assert.Equal(t, issuer, claims["iss"])
	assert.Equal(t, "kubelook-it", claims["aud"])
	assert.Equal(t, "42", claims["sub"])
	assert.Equal(t, "a@corp.example", claims["email"])
	assert.Equal(t, true, claims["email_verified"])
	assert.Equal(t, "corp.example", claims["hd"])
	assert.Greater(t, claims["exp"].(float64), claims["iat"].(float64))

	pub := fetchKey(t, s, header["kid"].(string))
	sum := sha256.Sum256([]byte(signingInput))
	require.NoError(t, rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig))
}

func TestOIDC_WrongVerifier_Is400(t *testing.T) {
	s := newServer(t)
	postJSON(t, s, "/admin/next", `{"sub":"1","email":"a@corp.example"}`)
	code, _ := authorize(t, s, "/oidc/authorize", "right")
	status, _ := exchange(t, s, "/oidc/token", code, "wrong")
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestOIDC_WrongClientSecret_Is401(t *testing.T) {
	s := newServer(t)
	postJSON(t, s, "/admin/next", `{"sub":"1","email":"a@corp.example"}`)
	code, _ := authorize(t, s, "/oidc/authorize", "v")
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {"v"}, "client_id": {"kubelook-it"}, "client_secret": {"nope"}}
	resp, err := http.PostForm(s.URL+"/oidc/token", form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestOIDC_HdAbsentUnlessSet(t *testing.T) {
	s := newServer(t)
	postJSON(t, s, "/admin/next", `{"sub":"1","email":"a@corp.example"}`)
	code, _ := authorize(t, s, "/oidc/authorize", "v")
	_, out := exchange(t, s, "/oidc/token", code, "v")
	_, claims, _, _ := decodeJWT(t, out["id_token"].(string))
	_, has := claims["hd"]
	assert.False(t, has)
	assert.Equal(t, true, claims["email_verified"], "email_verified defaults to true")
}

func TestOIDC_EmailVerifiedFalse_IsCarried(t *testing.T) {
	s := newServer(t)
	postJSON(t, s, "/admin/next", `{"sub":"1","email":"a@corp.example","hd":"corp.example","email_verified":false}`)
	code, _ := authorize(t, s, "/oidc/authorize", "v")
	_, out := exchange(t, s, "/oidc/token", code, "v")
	_, claims, _, _ := decodeJWT(t, out["id_token"].(string))
	assert.Equal(t, false, claims["email_verified"])
}

func TestOIDC_CodeIsSingleUse(t *testing.T) {
	s := newServer(t)
	postJSON(t, s, "/admin/next", `{"sub":"1","email":"a@corp.example"}`)
	code, _ := authorize(t, s, "/oidc/authorize", "v")
	first, _ := exchange(t, s, "/oidc/token", code, "v")
	second, _ := exchange(t, s, "/oidc/token", code, "v")
	assert.Equal(t, http.StatusOK, first)
	assert.Equal(t, http.StatusBadRequest, second)
}

func githubGet(t *testing.T, s *httptest.Server, path, token string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func githubToken(t *testing.T, s *httptest.Server, next string) string {
	t.Helper()
	postJSON(t, s, "/admin/next", next)
	code, _ := authorize(t, s, "/github/login/oauth/authorize", "v")
	status, out := exchange(t, s, "/github/login/oauth/access_token", code, "v")
	require.Equal(t, http.StatusOK, status)
	tok, _ := out["access_token"].(string)
	require.NotEmpty(t, tok)
	return tok
}

func TestGitHub_UserAndMemberships(t *testing.T) {
	s := newServer(t)
	tok := githubToken(t, s, `{"sub":"7","github_id":7,"email":"g@x.example","name":"Gee","github_orgs":["acme"]}`)

	status, user := githubGet(t, s, "/github/api/user", tok)
	assert.Equal(t, http.StatusOK, status)
	assert.EqualValues(t, 7, user["id"])
	assert.Equal(t, "g@x.example", user["email"])
	assert.Equal(t, "Gee", user["name"])

	status, m := githubGet(t, s, "/github/api/user/memberships/orgs/acme", tok)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "active", m["state"])

	status, _ = githubGet(t, s, "/github/api/user/memberships/orgs/other", tok)
	assert.Equal(t, http.StatusNotFound, status)
}

func TestGitHub_Org403_AnswersForbiddenForEveryOrg(t *testing.T) {
	s := newServer(t)
	tok := githubToken(t, s, `{"sub":"8","github_id":8,"github_orgs":["acme"],"org_403":true}`)
	status, _ := githubGet(t, s, "/github/api/user/memberships/orgs/acme", tok)
	assert.Equal(t, http.StatusForbidden, status)
}

func TestGitHub_WrongVerifier_Is400(t *testing.T) {
	s := newServer(t)
	postJSON(t, s, "/admin/next", `{"sub":"9","github_id":9}`)
	code, _ := authorize(t, s, "/github/login/oauth/authorize", "right")
	status, _ := exchange(t, s, "/github/login/oauth/access_token", code, "wrong")
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestGitHub_UnknownToken_Is401(t *testing.T) {
	s := newServer(t)
	status, _ := githubGet(t, s, "/github/api/user", "nope")
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestCharts_ServesFilesOfTheChartsDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.yaml"), []byte("apiVersion: v1\n"), 0o600))
	p := newIDP("http://kubelook-it-fakeidp:8300", clock.Real())
	p.charts = dir
	s := httptest.NewServer(p.handler())
	t.Cleanup(s.Close)

	resp, err := http.Get(s.URL + "/charts/index.yaml")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "apiVersion: v1\n", string(body))

	missing, err := http.Get(s.URL + "/charts/none.tgz")
	require.NoError(t, err)
	defer func() { _ = missing.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, missing.StatusCode)
}
