package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/bdnix/kubelook/internal/auth"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/testutil/fakeoidc"
)

const googleClientID = "google-client"

type googleEnv struct {
	idp   *fakeoidc.Issuer
	clock *clock.Fake
}

func newGoogleEnv(t *testing.T) *googleEnv {
	t.Helper()
	fake := clock.NewFake(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	idp := fakeoidc.New(fake.Now, googleClientID)
	t.Cleanup(idp.Close)
	return &googleEnv{idp: idp, clock: fake}
}

func (e *googleEnv) provider(t *testing.T, restrict bool, domains ...string) auth.Provider {
	t.Helper()
	p, err := auth.NewGoogle(context.Background(), auth.GoogleConfig{
		ClientID: googleClientID, ClientSecret: "secret", IssuerURL: e.idp.URL(),
		AllowedDomains: domains, Restrict: restrict,
	}, e.clock, e.idp.Client())
	require.NoError(t, err)
	return p
}

func tokenWith(idToken string) *oauth2.Token {
	return (&oauth2.Token{AccessToken: "a"}).WithExtra(map[string]any{"id_token": idToken})
}

func corpClaims() fakeoidc.Claims {
	return fakeoidc.Claims{Sub: "123", Email: "a@corp.example", EmailVerified: true, HD: "corp.example", Name: "Ada"}
}

func TestGoogle_AllowedDomain_ReturnsIdentity(t *testing.T) {
	e := newGoogleEnv(t)
	p := e.provider(t, true, "corp.example")
	id, err := p.Identify(context.Background(), tokenWith(e.idp.IDToken(corpClaims())))
	require.NoError(t, err)
	assert.Equal(t, auth.Identity{Provider: "google", Subject: "123", Email: "a@corp.example", Name: "Ada"}, id)
}

func TestGoogle_Name(t *testing.T) {
	e := newGoogleEnv(t)
	assert.Equal(t, "google", e.provider(t, false).Name())
}

func TestGoogle_Refused_ErrNotAllowed(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*fakeoidc.Claims)
		restrict bool
		domains  []string
	}{
		{"personal account without hd", func(c *fakeoidc.Claims) { c.HD = "" }, true, []string{"corp.example"}},
		{"other hosted domain", func(c *fakeoidc.Claims) { c.HD = "other.example" }, true, []string{"corp.example"}},
		{"unverified email", func(c *fakeoidc.Claims) { c.EmailVerified = false }, true, []string{"corp.example"}},
		{"empty allowlist", func(*fakeoidc.Claims) {}, true, nil},
		{"email domain is not used", func(c *fakeoidc.Claims) { c.HD = ""; c.Email = "a@corp.example" }, true, []string{"corp.example"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newGoogleEnv(t)
			p := e.provider(t, tc.restrict, tc.domains...)
			c := corpClaims()
			tc.mutate(&c)
			_, err := p.Identify(context.Background(), tokenWith(e.idp.IDToken(c)))
			require.ErrorIs(t, err, auth.ErrNotAllowed)
		})
	}
}

func TestGoogle_EmptyAllowlist_MessageSaysNoListIsSet(t *testing.T) {
	e := newGoogleEnv(t)
	p := e.provider(t, true)
	_, err := p.Identify(context.Background(), tokenWith(e.idp.IDToken(corpClaims())))
	require.ErrorIs(t, err, auth.ErrNotAllowed)
	assert.Contains(t, err.Error(), "no list is set")
}

func TestGoogle_RestrictOff_AcceptsAnyAccount(t *testing.T) {
	e := newGoogleEnv(t)
	p := e.provider(t, false)
	c := corpClaims()
	c.HD = ""
	c.Email = "me@gmail.example"
	id, err := p.Identify(context.Background(), tokenWith(e.idp.IDToken(c)))
	require.NoError(t, err)
	assert.Equal(t, "me@gmail.example", id.Email)
}

func TestGoogle_DomainComparisonIsCaseInsensitive(t *testing.T) {
	e := newGoogleEnv(t)
	p := e.provider(t, true, "Corp.Example")
	c := corpClaims()
	c.HD = "CORP.example"
	_, err := p.Identify(context.Background(), tokenWith(e.idp.IDToken(c)))
	require.NoError(t, err)
}

func TestGoogle_InvalidIDToken_ErrorNotErrNotAllowed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fakeoidc.Claims)
		bad    bool
	}{
		{"wrong audience", func(c *fakeoidc.Claims) { c.Aud = "someone-else" }, false},
		{"wrong issuer", func(c *fakeoidc.Claims) { c.Iss = "https://evil.example" }, false},
		{"expired", func(c *fakeoidc.Claims) { c.Exp = time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC).Unix() }, false},
		{"bad signature", func(*fakeoidc.Claims) {}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newGoogleEnv(t)
			p := e.provider(t, true, "corp.example")
			c := corpClaims()
			tc.mutate(&c)
			idp := e.idp
			var raw string
			if tc.bad {
				idp.BadSignature = true
				raw = rawFromTokenEndpoint(t, e, p)
			} else {
				raw = idp.IDToken(c)
			}
			_, err := p.Identify(context.Background(), tokenWith(raw))
			require.Error(t, err)
			assert.NotErrorIs(t, err, auth.ErrNotAllowed)
		})
	}
}

// rawFromTokenEndpoint runs a real code exchange so the issuer signs with the wrong key.
func rawFromTokenEndpoint(t *testing.T, e *googleEnv, p auth.Provider) string {
	t.Helper()
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, e.idp.Client())
	tok, err := p.OAuth2("https://kubelook.example.com/auth/google/callback").Exchange(ctx, "code")
	require.NoError(t, err)
	raw, _ := tok.Extra("id_token").(string)
	require.NotEmpty(t, raw)
	return raw
}

func TestGoogle_TokenWithoutIDToken_Error(t *testing.T) {
	e := newGoogleEnv(t)
	p := e.provider(t, true, "corp.example")
	_, err := p.Identify(context.Background(), &oauth2.Token{AccessToken: "a"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, auth.ErrNotAllowed)
}

func TestGoogle_OAuth2ConfigAndPKCEExchange(t *testing.T) {
	e := newGoogleEnv(t)
	e.idp.Claims = corpClaims()
	p := e.provider(t, true, "corp.example")
	cfg := p.OAuth2("https://kubelook.example.com/auth/google/callback")
	assert.Equal(t, googleClientID, cfg.ClientID)
	assert.Equal(t, "secret", cfg.ClientSecret)
	assert.Equal(t, []string{"openid", "email", "profile"}, cfg.Scopes)
	assert.Equal(t, e.idp.URL()+"/authorize", cfg.Endpoint.AuthURL)

	verifier := "a-verifier-of-sufficient-length-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	e.idp.Challenge = base64.RawURLEncoding.EncodeToString(sum[:])

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, e.idp.Client())
	_, err := cfg.Exchange(ctx, "code", oauth2.VerifierOption("wrong-verifier-wrong-verifier-wrong-verifier"))
	require.Error(t, err, "token endpoint must reject a verifier that does not match the challenge")

	tok, err := cfg.Exchange(ctx, "code", oauth2.VerifierOption(verifier))
	require.NoError(t, err)
	id, err := p.Identify(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, "123", id.Subject)
}

func TestGoogle_IssuerDownAtConstruction_WorksOnceItIsUp(t *testing.T) {
	e := newGoogleEnv(t)
	e.idp.SetDown(true)
	p := e.provider(t, true, "corp.example") // must not fail: discovery is lazy

	r, ok := p.(interface{ Ready(context.Context) error })
	require.True(t, ok, "a lazily constructed provider reports readiness")
	require.Error(t, r.Ready(context.Background()))

	e.idp.SetDown(false)
	require.NoError(t, r.Ready(context.Background()))
	cfg := p.OAuth2("https://kubelook.example.com/auth/google/callback")
	assert.Equal(t, e.idp.URL()+"/authorize", cfg.Endpoint.AuthURL)
	id, err := p.Identify(context.Background(), tokenWith(e.idp.IDToken(corpClaims())))
	require.NoError(t, err)
	assert.Equal(t, "123", id.Subject)
}

func TestGoogle_IdentifyWhileIssuerDown_Error(t *testing.T) {
	e := newGoogleEnv(t)
	e.idp.SetDown(true)
	p := e.provider(t, true, "corp.example")
	_, err := p.Identify(context.Background(), tokenWith("x.y.z"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, auth.ErrNotAllowed)
}

func TestNewGoogle_MissingClientIDOrIssuer_Error(t *testing.T) {
	for _, cfg := range []auth.GoogleConfig{{IssuerURL: "https://i"}, {ClientID: "c"}} {
		_, err := auth.NewGoogle(context.Background(), cfg, clock.Real(), nil)
		require.Error(t, err)
	}
}
