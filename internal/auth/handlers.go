package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"golang.org/x/oauth2"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/crypto"
)

// Deps are the dependencies of Handlers.
type Deps struct {
	Providers    []Provider
	PublicURL    string
	Sealer       *crypto.Sealer
	Clock        clock.Clock
	Rand         io.Reader
	CookieSecure bool
	// Fail writes an error response (httpapi supplies it; auth cannot import httpapi).
	Fail func(w http.ResponseWriter, r *http.Request, err error)
}

// Handlers serve the sign-in routes.
type Handlers struct {
	d         Deps
	providers map[string]Provider
}

// NewHandlers builds the handlers.
func NewHandlers(d Deps) *Handlers {
	h := &Handlers{d: d, providers: map[string]Provider{}}
	for _, p := range d.Providers {
		h.providers[p.Name()] = p
	}
	d.PublicURL = strings.TrimRight(d.PublicURL, "/")
	h.d = d
	return h
}

// Register adds the routes to mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/providers", h.listProviders)
	mux.HandleFunc("GET /auth/{provider}/login", h.login)
}

func (h *Handlers) names() []string {
	names := make([]string, 0, len(h.providers))
	for n := range h.providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (h *Handlers) listProviders(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string][]string{"providers": h.names()})
}

func (h *Handlers) redirectURL(provider string) string {
	return h.d.PublicURL + "/auth/" + provider + "/callback"
}

func (h *Handlers) lookup(w http.ResponseWriter, r *http.Request) (Provider, bool) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok {
		h.d.Fail(w, r, fmt.Errorf("unknown sign-in provider: %w", apperr.ErrNotFound))
	}
	return p, ok
}

// login starts the flow. It accepts no redirect parameter: after sign-in the user always lands on "/".
func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	p, ok := h.lookup(w, r)
	if !ok {
		return
	}
	if rp, ok := p.(interface{ Ready(context.Context) error }); ok {
		if err := rp.Ready(r.Context()); err != nil {
			h.d.Fail(w, r, fmt.Errorf("sign-in provider %s is not reachable: %w", p.Name(), err))
			return
		}
	}
	state, err := randomToken(h.d.Rand)
	if err != nil {
		h.d.Fail(w, r, err)
		return
	}
	verifier, err := randomToken(h.d.Rand)
	if err != nil {
		h.d.Fail(w, r, err)
		return
	}
	plain, err := marshalState(oauthState{State: state, Verifier: verifier, CreatedAt: h.d.Clock.Now()})
	if err != nil {
		h.d.Fail(w, r, fmt.Errorf("encoding state: %w", err))
		return
	}
	sealed, err := h.d.Sealer.Seal(plain, crypto.AAD(crypto.PurposeOAuthState, p.Name()))
	if err != nil {
		h.d.Fail(w, r, fmt.Errorf("sealing state: %w", err))
		return
	}
	//nolint:gosec // Secure follows server.publicURL on purpose (A8): plain HTTP is only for local development
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: sealed, Path: cookiePath, MaxAge: int(stateTTL.Seconds()),
		HttpOnly: true, Secure: h.d.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	cfg := p.OAuth2(h.redirectURL(p.Name()))
	http.Redirect(w, r, cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// parseCallback validates the callback against the state cookie, clears the cookie and
// returns the authorization code and the PKCE verifier.
func (h *Handlers) parseCallback(w http.ResponseWriter, r *http.Request, provider string) (code, verifier string, err error) {
	c, cerr := r.Cookie(cookieName)
	if cerr != nil {
		return "", "", fmt.Errorf("no state cookie: %w", ErrState)
	}
	h.clearCookie(w)
	plain, oerr := h.d.Sealer.Open(c.Value, crypto.AAD(crypto.PurposeOAuthState, provider))
	if oerr != nil {
		return "", "", fmt.Errorf("opening state cookie: %w", ErrState)
	}
	st, uerr := unmarshalState(plain)
	if uerr != nil {
		return "", "", fmt.Errorf("decoding state cookie: %w", ErrState)
	}
	if h.d.Clock.Now().Sub(st.CreatedAt) > stateTTL {
		return "", "", fmt.Errorf("state cookie expired: %w", ErrState)
	}
	q := r.URL.Query()
	if st.State == "" || !equal(q.Get("state"), st.State) {
		return "", "", fmt.Errorf("state mismatch: %w", ErrState)
	}
	if q.Get("error") != "" {
		return "", "", fmt.Errorf("provider returned %q: %w", q.Get("error"), ErrDenied)
	}
	code = q.Get("code")
	if code == "" {
		return "", "", fmt.Errorf("no authorization code: %w", ErrState)
	}
	return code, st.Verifier, nil
}

func (h *Handlers) clearCookie(w http.ResponseWriter) {
	//nolint:gosec // see login
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: cookiePath, MaxAge: -1,
		HttpOnly: true, Secure: h.d.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}
