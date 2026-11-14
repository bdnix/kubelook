package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
)

// GitHubConfig configures the GitHub provider.
type GitHubConfig struct {
	ClientID     string
	ClientSecret string
	BaseURL      string // e.g. https://github.com
	APIBaseURL   string // e.g. https://api.github.com
	AllowedOrgs  []string
	// Restrict limits sign-in to members of AllowedOrgs (auth.restrictSignIn).
	Restrict bool
}

type github struct {
	cfg GitHubConfig
	hc  *http.Client
}

// NewGitHub returns the GitHub provider.
func NewGitHub(cfg GitHubConfig, hc *http.Client) Provider {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.APIBaseURL = strings.TrimRight(cfg.APIBaseURL, "/")
	return &github{cfg: cfg, hc: hc}
}

func (g *github) Name() string { return "github" }

func (g *github) OAuth2(redirectURL string) *oauth2.Config {
	scopes := []string{"read:user", "user:email"}
	if g.cfg.Restrict {
		scopes = append(scopes, "read:org")
	}
	return &oauth2.Config{
		ClientID:     g.cfg.ClientID,
		ClientSecret: g.cfg.ClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  g.cfg.BaseURL + "/login/oauth/authorize",
			TokenURL: g.cfg.BaseURL + "/login/oauth/access_token",
		},
	}
}

func (g *github) Identify(ctx context.Context, tok *oauth2.Token) (Identity, error) {
	var user struct {
		ID    int64   `json:"id"`
		Name  *string `json:"name"`
		Email *string `json:"email"`
	}
	status, err := g.getJSON(ctx, g.cfg.APIBaseURL+"/user", tok.AccessToken, &user)
	if err != nil {
		return Identity{}, fmt.Errorf("github: reading user: %w", err)
	}
	if status != http.StatusOK {
		return Identity{}, fmt.Errorf("github: reading user: unexpected status %d", status)
	}
	if g.cfg.Restrict {
		if err := g.checkOrgs(ctx, tok.AccessToken); err != nil {
			return Identity{}, err
		}
	}
	id := Identity{Provider: "github", Subject: strconv.FormatInt(user.ID, 10)}
	if user.Email != nil {
		id.Email = *user.Email
	}
	if user.Name != nil {
		id.Name = *user.Name
	}
	return id, nil
}

// checkOrgs allows the user when they are an active member of at least one listed organization.
func (g *github) checkOrgs(ctx context.Context, token string) error {
	if len(g.cfg.AllowedOrgs) == 0 {
		return fmt.Errorf("github: sign-in is restricted but no list is set: %w", ErrNotAllowed)
	}
	for _, org := range g.cfg.AllowedOrgs {
		var m struct {
			State string `json:"state"`
		}
		status, err := g.getJSON(ctx, g.cfg.APIBaseURL+"/user/memberships/orgs/"+url.PathEscape(org), token, &m)
		switch {
		case status == http.StatusForbidden:
			return fmt.Errorf("github: organization %q restricts OAuth apps; an organization owner must approve this OAuth app: %w", org, ErrNotAllowed)
		case status == http.StatusNotFound:
			continue
		case err != nil:
			return fmt.Errorf("github: reading membership of %q: %w", org, err)
		case status != http.StatusOK:
			return fmt.Errorf("github: reading membership of %q: unexpected status %d", org, status)
		case m.State == "active":
			return nil
		}
	}
	return fmt.Errorf("github: not an active member of an allowed organization: %w", ErrNotAllowed)
}

// getJSON GETs url with the token and decodes a JSON body into v when the status is 200.
func (g *github) getJSON(ctx context.Context, u, token string, v any) (status int, err error) {
	//nolint:gosec // G704: u is the operator-configured GitHub API base URL plus a fixed path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := g.hc.Do(req) //nolint:gosec // G704: see above
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return resp.StatusCode, errors.New("decoding response: " + err.Error())
	}
	return resp.StatusCode, nil
}
