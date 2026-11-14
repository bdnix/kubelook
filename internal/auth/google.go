package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/bdnix/kubelook/internal/clock"
)

// GoogleConfig configures the Google provider.
type GoogleConfig struct {
	ClientID       string
	ClientSecret   string
	IssuerURL      string
	AllowedDomains []string
	// Restrict limits sign-in to AllowedDomains (auth.restrictSignIn).
	Restrict bool
}

type google struct {
	cfg   GoogleConfig
	clock clock.Clock
	hc    *http.Client

	mu       sync.Mutex
	provider *oidc.Provider // set by the first successful discovery
}

// NewGoogle returns the Google provider. Discovery happens on first use and is retried until it
// succeeds, so the server starts even when the issuer is briefly unreachable.
func NewGoogle(_ context.Context, cfg GoogleConfig, c clock.Clock, hc *http.Client) (Provider, error) {
	if cfg.ClientID == "" || cfg.IssuerURL == "" {
		return nil, errors.New("google: client ID and issuer URL are required")
	}
	return &google{cfg: cfg, clock: c, hc: hc}, nil
}

func (g *google) Name() string { return "google" }

// discover returns the discovered provider, performing discovery when it has not succeeded yet.
func (g *google) discover(ctx context.Context) (*oidc.Provider, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.provider != nil {
		return g.provider, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, g.hc), g.cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("google discovery: %w", err)
	}
	g.provider = p
	return p, nil
}

// Ready reports whether discovery has succeeded, attempting it when needed.
func (g *google) Ready(ctx context.Context) error {
	_, err := g.discover(ctx)
	return err
}

func (g *google) OAuth2(redirectURL string) *oauth2.Config {
	cfg := &oauth2.Config{
		ClientID:     g.cfg.ClientID,
		ClientSecret: g.cfg.ClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	if p, err := g.discover(context.Background()); err == nil {
		cfg.Endpoint = p.Endpoint()
	}
	return cfg
}

func (g *google) Identify(ctx context.Context, tok *oauth2.Token) (Identity, error) {
	p, err := g.discover(ctx)
	if err != nil {
		return Identity{}, err
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return Identity{}, errors.New("google: token response has no id_token")
	}
	verifier := p.Verifier(&oidc.Config{ClientID: g.cfg.ClientID, Now: g.clock.Now})
	idt, err := verifier.Verify(oidc.ClientContext(ctx, g.hc), raw)
	if err != nil {
		return Identity{}, fmt.Errorf("google: verifying id_token: %w", err)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HD            string `json:"hd"`
		Name          string `json:"name"`
	}
	if err := idt.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("google: reading claims: %w", err)
	}
	if g.cfg.Restrict {
		if len(g.cfg.AllowedDomains) == 0 {
			return Identity{}, fmt.Errorf("google: sign-in is restricted but no list is set: %w", ErrNotAllowed)
		}
		if !claims.EmailVerified {
			return Identity{}, fmt.Errorf("google: email is not verified: %w", ErrNotAllowed)
		}
		if !allowed(g.cfg.AllowedDomains, claims.HD) {
			return Identity{}, fmt.Errorf("google: hosted domain %q is not on the list: %w", claims.HD, ErrNotAllowed)
		}
	}
	return Identity{Provider: "google", Subject: idt.Subject, Email: claims.Email, Name: claims.Name}, nil
}

// allowed reports whether hd is one of domains, ignoring case. An empty hd never matches.
func allowed(domains []string, hd string) bool {
	if hd == "" {
		return false
	}
	for _, d := range domains {
		if strings.EqualFold(d, hd) {
			return true
		}
	}
	return false
}
