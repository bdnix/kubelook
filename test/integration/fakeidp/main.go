// Command fakeidp is the identity stand-in of the integration environment (assumption A23).
// It plays Google (OpenID Connect) and GitHub (OAuth 2.0) and signs in whichever account
// the test announced with POST /admin/next. It auto-approves every authorization.
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
)

const (
	clientID     = "kubelook-it"
	clientSecret = "it-secret"
	keyID        = "it-key-1"
)

// account is what POST /admin/next announces.
type account struct {
	Sub           string   `json:"sub"`
	Email         string   `json:"email"`
	Name          string   `json:"name"`
	HD            string   `json:"hd"`
	EmailVerified *bool    `json:"email_verified"`
	GitHubID      int64    `json:"github_id"`
	GitHubOrgs    []string `json:"github_orgs"`
	Org403        bool     `json:"org_403"`
}

type grant struct {
	acct      account
	challenge string
}

type idp struct {
	clock    clock.Clock
	external string // scheme and host the clients use, e.g. http://kubelook-it-fakeidp:8300
	key      *rsa.PrivateKey

	mu     sync.Mutex
	next   account
	codes  map[string]grant   // authorization code → grant
	tokens map[string]account // GitHub access token → account
}

func newIDP(external string, clk clock.Clock) *idp {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return &idp{
		clock:    clk,
		external: strings.TrimRight(external, "/"),
		key:      key,
		codes:    map[string]grant{},
		tokens:   map[string]account{},
	}
}

func (p *idp) issuer() string { return p.external + "/oidc" }

func (p *idp) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /admin/next", p.adminNext)
	mux.HandleFunc("GET /oidc/.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /oidc/keys", p.keys)
	mux.HandleFunc("GET /oidc/authorize", p.authorize)
	mux.HandleFunc("POST /oidc/token", p.oidcToken)
	mux.HandleFunc("GET /github/login/oauth/authorize", p.authorize)
	mux.HandleFunc("POST /github/login/oauth/access_token", p.githubToken)
	mux.HandleFunc("GET /github/api/user", p.githubUser)
	mux.HandleFunc("GET /github/api/user/memberships/orgs/{org}", p.githubMembership)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (p *idp) adminNext(w http.ResponseWriter, r *http.Request) {
	var a account
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.next = a
	p.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (p *idp) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                p.issuer(),
		"authorization_endpoint":                p.issuer() + "/authorize",
		"token_endpoint":                        p.issuer() + "/token",
		"jwks_uri":                              p.issuer() + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *idp) keys(w http.ResponseWriter, _ *http.Request) {
	pub := p.key.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": keyID,
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

// authorize approves at once: it redirects to redirect_uri with a fresh code and the caller's state.
func (p *idp) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme == "" || redirect.Host == "" {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	code := randomHex(16)
	p.mu.Lock()
	p.codes[code] = grant{acct: p.next, challenge: q.Get("code_challenge")}
	p.mu.Unlock()
	rq := redirect.Query()
	rq.Set("code", code)
	rq.Set("state", q.Get("state"))
	redirect.RawQuery = rq.Encode()
	//nolint:gosec // G710: the stand-in redirects wherever the test client asked, by design
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

// redeem checks the client and the PKCE verifier and consumes the code.
func (p *idp) redeem(r *http.Request) (account, int, string) {
	if err := r.ParseForm(); err != nil {
		return account{}, http.StatusBadRequest, "bad form"
	}
	if r.PostForm.Get("client_id") != clientID ||
		subtle.ConstantTimeCompare([]byte(r.PostForm.Get("client_secret")), []byte(clientSecret)) != 1 {
		return account{}, http.StatusUnauthorized, "invalid_client"
	}
	p.mu.Lock()
	g, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.mu.Unlock()
	if !ok {
		return account{}, http.StatusBadRequest, "invalid_grant"
	}
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		return account{}, http.StatusBadRequest, "invalid_grant: PKCE verifier does not match"
	}
	return g.acct, http.StatusOK, ""
}

func (p *idp) oidcToken(w http.ResponseWriter, r *http.Request) {
	a, status, msg := p.redeem(r)
	if status != http.StatusOK {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	now := p.clock.Now()
	verified := true
	if a.EmailVerified != nil {
		verified = *a.EmailVerified
	}
	claims := map[string]any{
		"iss": p.issuer(), "aud": clientID, "sub": a.Sub, "email": a.Email, "name": a.Name,
		"email_verified": verified, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	}
	if a.HD != "" {
		claims["hd"] = a.HD
	}
	idToken, err := p.sign(claims)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": randomHex(16), "token_type": "Bearer", "expires_in": 300, "id_token": idToken,
	})
}

func (p *idp) sign(claims map[string]any) (string, error) {
	enc := func(v any) (string, error) {
		b, err := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b), err
	}
	h, err := enc(map[string]string{"alg": "RS256", "typ": "JWT", "kid": keyID})
	if err != nil {
		return "", err
	}
	c, err := enc(claims)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(h + "." + c))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return h + "." + c + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (p *idp) githubToken(w http.ResponseWriter, r *http.Request) {
	a, status, msg := p.redeem(r)
	if status != http.StatusOK {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	tok := "gho_" + randomHex(16)
	p.mu.Lock()
	p.tokens[tok] = a
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"access_token": tok, "token_type": "bearer", "scope": "read:user,user:email,read:org"})
}

func (p *idp) bearer(r *http.Request) (account, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.tokens[tok]
	return a, ok
}

func (p *idp) githubUser(w http.ResponseWriter, r *http.Request) {
	a, ok := p.bearer(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": a.GitHubID, "login": "user-" + a.Sub, "name": a.Name, "email": a.Email,
	})
}

func (p *idp) githubMembership(w http.ResponseWriter, r *http.Request) {
	a, ok := p.bearer(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	if a.Org403 {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Organization restricts OAuth app access"})
		return
	}
	org := r.PathValue("org")
	for _, o := range a.GitHubOrgs {
		if o == org {
			writeJSON(w, http.StatusOK, map[string]string{"state": "active"})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func main() {
	external := os.Getenv("FAKEIDP_EXTERNAL_URL")
	if external == "" {
		external = "http://kubelook-it-fakeidp:8300"
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv := &http.Server{Addr: ":8300", Handler: newIDP(external, clock.Real()).handler(), ReadHeaderTimeout: 10 * time.Second}
	logger.Info("fakeidp listening", "addr", srv.Addr, "issuer", external+"/oidc")
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("fakeidp stopped", "err", err)
		os.Exit(1)
	}
}
