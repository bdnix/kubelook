package helm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
)

var repoName = regexp.MustCompile(`^[a-z0-9][-a-z0-9_.]{0,62}$`)

// RepoInfo is a repository as the UI sees it: never with its password.
type RepoInfo struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Scope   string `json:"scope"` // org | session
	HasAuth bool   `json:"hasAuth"`
}

// Repos are the chart repositories a user can install from: the ones of the deployment settings
// (`helm.repositories`) and the ones the user added to this session. A session repository is sealed, bound to
// the session and gone when the session ends.
type Repos struct {
	Org      []config.HelmRepo
	Sessions *store.Sessions
	Sealer   *crypto.Sealer
	Clock    clock.Clock
}

type repoSecret struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func (r *Repos) org(name string) (config.HelmRepo, bool) {
	for _, o := range r.Org {
		if o.Name == name {
			return o, true
		}
	}
	return config.HelmRepo{}, false
}

func (r *Repos) open(sess session.Session, sealed string) (repoSecret, error) {
	raw, err := r.Sealer.Open(sealed, crypto.AAD(crypto.PurposeSessionCredential, sess.Hash))
	if err != nil {
		return repoSecret{}, fmt.Errorf("opening a repository entry: %w", err)
	}
	var s repoSecret
	if err := json.Unmarshal(raw, &s); err != nil {
		return repoSecret{}, fmt.Errorf("reading a repository entry: %w", err)
	}
	return s, nil
}

// List returns the organisation's repositories, then the session's, each sorted by name.
func (r *Repos) List(ctx context.Context, sess session.Session) ([]RepoInfo, error) {
	out := make([]RepoInfo, 0, len(r.Org))
	for _, o := range r.Org {
		out = append(out, RepoInfo{Name: o.Name, URL: o.URL, Scope: "org"})
	}
	slices.SortFunc(out, func(a, b RepoInfo) int { return strings.Compare(a.Name, b.Name) })
	rows, err := r.Sessions.ListCredentials(ctx, sess.Hash, store.CredHelmRepo)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		s, err := r.open(sess, row.Sealed)
		if err != nil {
			return nil, err
		}
		out = append(out, RepoInfo{Name: row.Ref, URL: s.URL, Scope: "session", HasAuth: s.Username != ""})
	}
	return out, nil
}

// Add stores a repository for the session.
func (r *Repos) Add(ctx context.Context, sess session.Session, name, address, username, password string) error {
	if !repoName.MatchString(name) {
		return fmt.Errorf("%q is not a valid repository name: %w", name, apperr.ErrInvalid)
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("the repository address must be an http or https URL: %w", apperr.ErrInvalid)
	}
	if _, taken := r.org(name); taken {
		return fmt.Errorf("the name %q is used by a repository of the organisation: %w", name, apperr.ErrConflict)
	}
	raw, err := json.Marshal(repoSecret{URL: address, Username: username, Password: password}) //nolint:gosec // the result is sealed before it is stored
	if err != nil {
		return err
	}
	sealed, err := r.Sealer.Seal(raw, crypto.AAD(crypto.PurposeSessionCredential, sess.Hash))
	if err != nil {
		return fmt.Errorf("sealing a repository entry: %w", err)
	}
	return r.Sessions.PutCredential(ctx, sess.Hash, store.CredentialRow{Kind: store.CredHelmRepo, Ref: name, Sealed: sealed}, r.Clock.Now())
}

// Delete removes a session repository. Repositories of the organisation cannot be removed here.
func (r *Repos) Delete(ctx context.Context, sess session.Session, name string) error {
	if _, ok := r.org(name); ok {
		return fmt.Errorf("the repository %q is set by the organisation: %w", name, apperr.ErrForbidden)
	}
	if _, err := r.Sessions.GetCredential(ctx, sess.Hash, store.CredHelmRepo, name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no repository %q: %w", name, apperr.ErrNotFound)
		}
		return err
	}
	return r.Sessions.DeleteCredential(ctx, sess.Hash, store.CredHelmRepo, name)
}

// Resolve returns a repository with its credentials, for a download.
func (r *Repos) Resolve(ctx context.Context, sess session.Session, name string) (Repository, error) {
	if o, ok := r.org(name); ok {
		return Repository{Name: o.Name, URL: o.URL}, nil
	}
	row, err := r.Sessions.GetCredential(ctx, sess.Hash, store.CredHelmRepo, name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Repository{}, fmt.Errorf("no repository %q: %w", name, apperr.ErrNotFound)
		}
		return Repository{}, err
	}
	s, err := r.open(sess, row.Sealed)
	if err != nil {
		return Repository{}, err
	}
	return Repository{Name: name, URL: s.URL, Username: s.Username, Password: s.Password}, nil
}
