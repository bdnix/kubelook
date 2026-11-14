// Package auth implements sign-in with external OAuth providers: the provider-independent
// authorization-code flow with PKCE and a sealed state cookie.
package auth

import (
	"context"
	"errors"

	"golang.org/x/oauth2"
)

// Identity is who a provider says the user is.
type Identity struct{ Provider, Subject, Email, Name string }

// Provider is one sign-in method (Google, GitHub).
type Provider interface {
	Name() string // "google" | "github"
	OAuth2(redirectURL string) *oauth2.Config
	// Identify exchanges nothing; it receives the token from the exchange and returns the identity
	// or ErrNotAllowed when the allowlist refuses it.
	Identify(ctx context.Context, tok *oauth2.Token) (Identity, error)
}

// Errors of the sign-in flow.
var (
	ErrState      = errors.New("auth: sign-in state is missing, expired or does not match")
	ErrDenied     = errors.New("auth: sign-in was denied at the provider")
	ErrNotAllowed = errors.New("auth: this account is not allowed to sign in")
)
