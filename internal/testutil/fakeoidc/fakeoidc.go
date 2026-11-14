// Package fakeoidc is an httptest OpenID Connect issuer for unit tests: a discovery document,
// a JWKS, and a token endpoint that returns an RS256 ID token with configurable claims and
// checks the PKCE verifier.
package fakeoidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// Claims are the ID-token claims. Zero values are omitted, except Exp and Iat which the
// issuer fills in from its clock when zero.
type Claims struct {
	Iss           string `json:"iss,omitempty"`
	Aud           string `json:"aud,omitempty"`
	Sub           string `json:"sub,omitempty"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified"`
	HD            string `json:"hd,omitempty"`
	Name          string `json:"name,omitempty"`
	Exp           int64  `json:"exp,omitempty"`
	Iat           int64  `json:"iat,omitempty"`
}

// Issuer is the fake identity provider.
type Issuer struct {
	// Now supplies the issue time (the test's fake clock).
	Now func() time.Time
	// ClientID is the audience put in tokens whose Aud is empty.
	ClientID string
	// Claims is the template for the next ID token. Iss defaults to the issuer URL.
	Claims Claims
	// NoIDToken makes the token endpoint answer without an id_token.
	NoIDToken bool
	// BadSignature signs the next ID token with a key that is not in the JWKS.
	BadSignature bool
	// RefreshToken, when set, is the only refresh token the refresh_token grant accepts.
	RefreshToken string
	// NewRefreshToken, when set, is returned as the rotated refresh token by the refresh_token grant.
	NewRefreshToken string
	// Challenge, when set, is the PKCE S256 challenge the token endpoint must see proved.
	Challenge string

	srv      *httptest.Server
	key      *rsa.PrivateKey
	otherKey *rsa.PrivateKey
	mu       sync.Mutex
	down     bool
	refreshN int
}

// Refreshes returns how many refresh_token grants were served.
func (i *Issuer) Refreshes() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.refreshN
}

// New starts an issuer. Close it with Close.
func New(now func() time.Time, clientID string) *Issuer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	i := &Issuer{Now: now, ClientID: clientID, key: key, otherKey: other}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", i.discovery)
	mux.HandleFunc("GET /keys", i.keys)
	mux.HandleFunc("POST /token", i.token)
	i.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i.mu.Lock()
		down := i.down
		i.mu.Unlock()
		if down {
			http.Error(w, "issuer is down", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return i
}

// URL is the issuer URL.
func (i *Issuer) URL() string { return i.srv.URL }

// Client returns an HTTP client for the issuer.
func (i *Issuer) Client() *http.Client { return i.srv.Client() }

// Close stops the server.
func (i *Issuer) Close() { i.srv.Close() }

// SetDown makes every request fail with 503 (true) or work again (false).
func (i *Issuer) SetDown(down bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.down = down
}

func (i *Issuer) discovery(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                i.srv.URL,
		"authorization_endpoint":                i.srv.URL + "/authorize",
		"token_endpoint":                        i.srv.URL + "/token",
		"jwks_uri":                              i.srv.URL + "/keys",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (i *Issuer) keys(w http.ResponseWriter, _ *http.Request) {
	pub := i.key.PublicKey
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "k1",
		"n": b64(pub.N.Bytes()),
		"e": b64(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	i.mu.Lock()
	challenge, noID, bad, claims := i.Challenge, i.NoIDToken, i.BadSignature, i.Claims
	wantRefresh, newRefresh := i.RefreshToken, i.NewRefreshToken
	refreshing := r.PostForm.Get("grant_type") == "refresh_token"
	if refreshing {
		i.refreshN++
	}
	i.mu.Unlock()
	if refreshing {
		if wantRefresh != "" && r.PostForm.Get("refresh_token") != wantRefresh {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		resp := map[string]any{"access_token": "access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": i.sign(claims, bad)}
		if newRefresh != "" {
			resp["refresh_token"] = newRefresh
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	if challenge != "" {
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if b64(sum[:]) != challenge {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
	}
	resp := map[string]any{"access_token": "access-token", "token_type": "Bearer", "expires_in": 3600}
	if !noID {
		resp["id_token"] = i.sign(claims, bad)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// IDToken returns a signed ID token for c, for tests that call Identify directly.
func (i *Issuer) IDToken(c Claims) string { return i.sign(c, false) }

func (i *Issuer) sign(c Claims, bad bool) string {
	if c.Iss == "" {
		c.Iss = i.srv.URL
	}
	if c.Aud == "" {
		c.Aud = i.ClientID
	}
	now := i.Now()
	if c.Iat == 0 {
		c.Iat = now.Unix()
	}
	if c.Exp == 0 {
		c.Exp = now.Add(time.Hour).Unix()
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	payload, _ := json.Marshal(c)
	signing := b64(header) + "." + b64(payload)
	key := i.key
	if bad {
		key = i.otherKey
	}
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return signing + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
