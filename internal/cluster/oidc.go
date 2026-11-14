package cluster

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kubeconfig"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
)

// refreshMargin is how long before expiry an ID token is replaced.
const refreshMargin = 30 * time.Second

var errOIDCRejected = &apperr.APIError{Status: http.StatusUnauthorized, Code: "unauthenticated",
	Message: "Your identity provider no longer accepts the saved login of this kubeconfig. Upload a fresh kubeconfig."}

// kubeState is the in-memory state of an opened kubeconfig cluster.
type kubeState struct {
	file    *kubeconfig.File
	label   string
	context string
	oidc    bool
	idToken string
	idExp   time.Time
}

// kubeSource connects with the credentials in an uploaded kubeconfig.
type kubeSource struct{ o *Opener }

func (s *kubeSource) open(ctx context.Context, sess session.Session, ref Ref, _ string) (*entry, error) {
	files, err := s.o.d.Credentials.loadKubeconfigs(ctx, sess)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.Label != ref.Label {
			continue
		}
		cfg, err := f.RESTConfig(ref.Context)
		if err != nil {
			return nil, err
		}
		ks := &kubeState{file: f, label: f.Label, context: ref.Context}
		e := &entry{kube: ks, server: cfg.Host}
		if oc, ok := f.OIDC(ref.Context); ok {
			ks.oidc, ks.idToken, ks.idExp = true, oc.IDToken, jwtExpiry(oc.IDToken)
			e.expires = ks.idExp
		}
		return e, nil
	}
	return nil, apperr.ErrClusterUnavailable
}

func (s *kubeSource) state(e *entry) kubeState {
	s.o.mu.Lock()
	defer s.o.mu.Unlock()
	return *e.kube
}

func (s *kubeSource) restConfig(e *entry) (*rest.Config, error) {
	ks := s.state(e)
	cfg, err := ks.file.RESTConfig(ks.context)
	if err != nil {
		return nil, err
	}
	if ks.oidc {
		cfg.BearerToken = "" // attached per request, refreshed when needed
	}
	return cfg, nil
}

func (s *kubeSource) due(e *entry) bool {
	ks := s.state(e)
	return ks.oidc && ks.idExp.Sub(s.o.d.Clock.Now()) <= s.o.d.Config.Vault.RemintBefore.D()
}

func (s *kubeSource) token(ctx context.Context, e *entry) (string, error) {
	ks := s.state(e)
	if !ks.oidc {
		return "", nil
	}
	if ks.idToken != "" && ks.idExp.Sub(s.o.d.Clock.Now()) > refreshMargin {
		return ks.idToken, nil
	}
	if err := s.o.flight.do(entryKey(e.sess.Hash, e.ref.ID), func() error { return s.refresh(ctx, e) }); err != nil {
		return "", err
	}
	return s.state(e).idToken, nil
}

func (s *kubeSource) refresh(ctx context.Context, e *entry) error {
	o := s.o
	ks := s.state(e)
	if ks.idToken != "" && ks.idExp.Sub(o.d.Clock.Now()) > refreshMargin {
		return nil
	}
	oc, _ := ks.file.OIDC(ks.context)
	idToken, newRefresh, err := s.exchange(ctx, oc)
	if errors.Is(err, errOIDCRejected) {
		o.setDisconnected(e)
	}
	if err != nil {
		return err
	}
	content, err := ks.file.SetOIDCTokens(ks.context, idToken, newRefresh)
	if err != nil {
		return err
	}
	sealed, err := o.sealSession(e.sess, content)
	if err != nil {
		return err
	}
	if err := o.d.Sessions.PutCredential(ctx, e.sess.Hash, store.CredentialRow{Kind: kindKubeconfig, Ref: ks.label, Sealed: sealed}, o.d.Clock.Now()); err != nil {
		return err
	}
	if newRefresh != "" && newRefresh != oc.RefreshToken {
		if err := s.queueCopy(ctx, e.sess, ks.label, content); err != nil {
			return err
		}
	}
	o.mu.Lock()
	e.kube.idToken, e.kube.idExp = idToken, jwtExpiry(idToken)
	e.expires = e.kube.idExp
	o.mu.Unlock()
	return nil
}

// queueCopy keeps a sealed browser copy of the updated kubeconfig until the browser collects it.
func (s *kubeSource) queueCopy(ctx context.Context, sess session.Session, label string, content []byte) error {
	sealed, err := s.o.d.Credentials.sealForBrowser(sess.UserID, browserCopy{
		Kind: kindKubeconfig, Label: label, Content: string(content), SavedAt: s.o.d.Clock.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return s.o.d.Sessions.PutCredential(ctx, sess.Hash, store.CredentialRow{Kind: kindPendingCopy, Ref: label, Sealed: sealed}, s.o.d.Clock.Now())
}

// exchange trades the refresh token for a new ID token (and possibly a rotated refresh token).
func (s *kubeSource) exchange(ctx context.Context, oc kubeconfig.OIDCSettings) (idToken, refreshToken string, err error) {
	hc, err := s.httpClient(oc)
	if err != nil {
		return "", "", err
	}
	endpoint, err := tokenEndpoint(ctx, hc, oc.IssuerURL)
	if err != nil {
		return "", "", err
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {oc.RefreshToken}, "client_id": {oc.ClientID}}
	if oc.ClientSecret != "" {
		form.Set("client_secret", oc.ClientSecret)
	}
	var out struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
	}
	status, err := postForm(ctx, hc, endpoint, form, &out)
	switch {
	case err != nil:
		return "", "", fmt.Errorf("the identity provider is unreachable: %w", apperr.ErrBusy)
	case status == http.StatusBadRequest || status == http.StatusUnauthorized:
		return "", "", errOIDCRejected
	case status != http.StatusOK || out.IDToken == "":
		return "", "", fmt.Errorf("the identity provider answered HTTP %d: %w", status, apperr.ErrBusy)
	}
	return out.IDToken, out.RefreshToken, nil
}

func (s *kubeSource) httpClient(oc kubeconfig.OIDCSettings) (*http.Client, error) {
	if len(oc.IDPCAData) == 0 {
		return s.o.d.HTTPClient, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(oc.IDPCAData) {
		return nil, fmt.Errorf("idp-certificate-authority-data holds no certificate: %w", apperr.ErrInvalid)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig.RootCAs = pool
	return &http.Client{Transport: tr, Timeout: s.o.d.HTTPClient.Timeout}, nil
}

func tokenEndpoint(ctx context.Context, hc *http.Client, issuer string) (string, error) {
	u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("building discovery request: %w", err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("the identity provider is unreachable: %w", apperr.ErrBusy)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc) != nil || doc.TokenEndpoint == "" {
		return "", fmt.Errorf("the identity provider has no usable discovery document: %w", apperr.ErrBusy)
	}
	return doc.TokenEndpoint, nil
}

func postForm(ctx context.Context, hc *http.Client, endpoint string, form url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("calling token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decoding token response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// jwtExpiry reads the exp claim of a JWT without verifying it. It only decides when to refresh;
// it is never trusted for anything else. A token that cannot be read counts as expired (zero time).
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(b, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}
