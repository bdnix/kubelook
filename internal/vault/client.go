// Package vault is a small client for the Vault endpoints Kubelook uses, and the discovery of
// which clusters a Vault token can open.
package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxResponseBytes = 4 << 20
	maxCapPaths      = 100
)

// Errors returned by the client. They wrap Vault's own message where there is one.
var (
	ErrPermissionDenied = errors.New("vault: permission denied")
	ErrNotFound         = errors.New("vault: not found")
	ErrUnreachable      = errors.New("vault: unreachable")
)

// ResponseError is Vault answering a request with an error list (HTTP 400, or 500 when a
// secrets engine fails). It is an answer, not an outage.
type ResponseError struct {
	Status   int
	Messages []string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("vault: HTTP %d: %s", e.Status, strings.Join(e.Messages, "; "))
}

// TokenInfo is what lookup-self says about a token.
type TokenInfo struct {
	TTL         time.Duration
	Renewable   bool
	Type        string // "service" | "batch"
	Policies    []string
	EntityID    string
	DisplayName string
	ExpireTime  time.Time // zero when the token never expires
}

// NeverExpires reports whether the token has no TTL.
func (t TokenInfo) NeverExpires() bool { return t.TTL == 0 }

// ClusterConfig is the connection data of a Kubernetes secrets mount.
type ClusterConfig struct{ Host, CACert string }

// CredsRequest asks a Kubernetes secrets mount for a short-lived service account token.
type CredsRequest struct {
	Namespace          string
	ClusterRoleBinding bool
	TTL                time.Duration
}

// Creds is a minted service account token.
type Creds struct {
	Token, ServiceAccount, Namespace, LeaseID string
	LeaseDuration                             time.Duration
}

// API is the set of Vault calls Kubelook makes. Every call carries the caller's token.
type API interface {
	LookupSelf(ctx context.Context, token string) (TokenInfo, error)
	RenewSelf(ctx context.Context, token string) (time.Duration, error)
	Mounts(ctx context.Context, token string) (map[string]string, error)
	Capabilities(ctx context.Context, token string, paths []string) (map[string][]string, error)
	ClusterConfig(ctx context.Context, token, mount string) (ClusterConfig, error)
	Creds(ctx context.Context, token, mount, role string, req CredsRequest) (Creds, error)
}

// Client implements API over HTTP.
type Client struct {
	address string
	hc      *http.Client
}

// NewClient returns a Client for the Vault at address. hc has a 15 s timeout in production.
func NewClient(address string, hc *http.Client) *Client {
	return &Client{address: strings.TrimRight(address, "/"), hc: hc}
}

// do sends one request. in is JSON-encoded as the body when non-nil; the answer is decoded into out when non-nil.
func (c *Client) do(ctx context.Context, method, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encoding vault request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	//nolint:gosec // G704: the address is operator configuration; the path is built from fixed segments and mount names
	req, err := http.NewRequestWithContext(ctx, method, c.address+"/v1/"+path, body)
	if err != nil {
		return redact(fmt.Errorf("building vault request: %w", err), token)
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("X-Vault-Request", "true")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req) //nolint:gosec // G704: see above
	if err != nil {
		if ctx.Err() != nil {
			return redact(fmt.Errorf("vault request: %w", ctx.Err()), token)
		}
		return redact(fmt.Errorf("%w: %w", ErrUnreachable, err), token)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return redact(fmt.Errorf("reading vault response: %w", ctx.Err()), token)
		}
		return redact(fmt.Errorf("%w: reading response: %w", ErrUnreachable, err), token)
	}
	if len(raw) > maxResponseBytes {
		return errors.New("vault: response too large")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decoding vault response: %w", err)
		}
		return nil
	}
	return statusError(resp.StatusCode, raw, token)
}

// statusError maps an unsuccessful answer to a typed error. Vault's messages are kept, minus the token.
func statusError(status int, raw []byte, token string) error {
	var e struct {
		Errors []string `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	msgs := make([]string, len(e.Errors))
	for i, m := range e.Errors {
		msgs[i] = strings.ReplaceAll(m, token, "[redacted]")
	}
	first := ""
	if len(msgs) > 0 {
		first = msgs[0]
	}
	switch {
	case status == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrPermissionDenied, first)
	case status == http.StatusNotFound:
		return ErrNotFound
	case status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout:
		return fmt.Errorf("%w: HTTP %d", ErrUnreachable, status)
	case status == http.StatusBadRequest || (status == http.StatusInternalServerError && len(msgs) > 0):
		return &ResponseError{Status: status, Messages: msgs}
	}
	return fmt.Errorf("vault: unexpected HTTP status %d", status)
}

// redact removes the token from an error's text, keeping the error chain.
func redact(err error, token string) error {
	if token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return &redacted{msg: strings.ReplaceAll(err.Error(), token, "[redacted]"), err: err}
}

type redacted struct {
	msg string
	err error
}

func (r *redacted) Error() string { return r.msg }
func (r *redacted) Unwrap() error { return r.err }

func seconds(n float64) time.Duration { return time.Duration(n * float64(time.Second)) }

// LookupSelf describes the token.
func (c *Client) LookupSelf(ctx context.Context, token string) (TokenInfo, error) {
	var out struct {
		Data struct {
			TTL         float64  `json:"ttl"`
			Renewable   bool     `json:"renewable"`
			Type        string   `json:"type"`
			Policies    []string `json:"policies"`
			EntityID    string   `json:"entity_id"`
			DisplayName string   `json:"display_name"`
			ExpireTime  *string  `json:"expire_time"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "auth/token/lookup-self", token, nil, &out); err != nil {
		return TokenInfo{}, err
	}
	d := out.Data
	info := TokenInfo{
		TTL: seconds(d.TTL), Renewable: d.Renewable, Type: d.Type, Policies: d.Policies,
		EntityID: d.EntityID, DisplayName: d.DisplayName,
	}
	if d.ExpireTime != nil && *d.ExpireTime != "" {
		t, err := time.Parse(time.RFC3339Nano, *d.ExpireTime)
		if err != nil {
			return TokenInfo{}, fmt.Errorf("parsing token expire_time: %w", err)
		}
		info.ExpireTime = t
	}
	return info, nil
}

// RenewSelf renews the token and returns its new TTL.
func (c *Client) RenewSelf(ctx context.Context, token string) (time.Duration, error) {
	var out struct {
		Auth struct {
			LeaseDuration float64 `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := c.do(ctx, http.MethodPost, "auth/token/renew-self", token, struct{}{}, &out); err != nil {
		return 0, err
	}
	return seconds(out.Auth.LeaseDuration), nil
}

// Mounts returns the secrets mounts visible to the token: path (with trailing slash) → type.
func (c *Client) Mounts(ctx context.Context, token string) (map[string]string, error) {
	var out struct {
		Data struct {
			Secret map[string]struct {
				Type string `json:"type"`
			} `json:"secret"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "sys/internal/ui/mounts", token, nil, &out); err != nil {
		return nil, err
	}
	mounts := make(map[string]string, len(out.Data.Secret))
	for p, m := range out.Data.Secret {
		mounts[p] = m.Type
	}
	return mounts, nil
}

// Capabilities returns the token's capabilities on each path (no leading slash, no "v1/").
// More than 100 paths are split into several calls and merged.
func (c *Client) Capabilities(ctx context.Context, token string, paths []string) (map[string][]string, error) {
	result := make(map[string][]string, len(paths))
	for start := 0; start < len(paths); start += maxCapPaths {
		chunk := paths[start:min(start+maxCapPaths, len(paths))]
		var out struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := c.do(ctx, http.MethodPost, "sys/capabilities-self", token, map[string][]string{"paths": chunk}, &out); err != nil {
			return nil, err
		}
		for _, p := range chunk {
			raw, ok := out.Data[p]
			if !ok {
				continue
			}
			var caps []string
			if err := json.Unmarshal(raw, &caps); err != nil {
				return nil, fmt.Errorf("decoding capabilities of %q: %w", p, err)
			}
			result[p] = caps
		}
	}
	return result, nil
}

// ClusterConfig reads the connection data of a Kubernetes mount (path without trailing slash).
// A mount that was never configured (HTTP 404) yields the zero value and no error.
func (c *Client) ClusterConfig(ctx context.Context, token, mount string) (ClusterConfig, error) {
	var out struct {
		Data struct {
			Host   string `json:"kubernetes_host"`
			CACert string `json:"kubernetes_ca_cert"`
		} `json:"data"`
	}
	err := c.do(ctx, http.MethodGet, mount+"/config", token, nil, &out)
	if errors.Is(err, ErrNotFound) {
		return ClusterConfig{}, nil
	}
	if err != nil {
		return ClusterConfig{}, err
	}
	return ClusterConfig{Host: out.Data.Host, CACert: out.Data.CACert}, nil
}

// Creds mints a service account token from the mount (path without trailing slash) for the role.
func (c *Client) Creds(ctx context.Context, token, mount, role string, req CredsRequest) (Creds, error) {
	in := map[string]any{
		"kubernetes_namespace": req.Namespace,
		"cluster_role_binding": req.ClusterRoleBinding,
		"ttl":                  req.TTL.String(),
	}
	var out struct {
		LeaseID       string  `json:"lease_id"`
		LeaseDuration float64 `json:"lease_duration"`
		Data          struct {
			Token          string `json:"service_account_token"`
			ServiceAccount string `json:"service_account_name"`
			Namespace      string `json:"service_account_namespace"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, mount+"/creds/"+role, token, in, &out); err != nil {
		return Creds{}, err
	}
	return Creds{
		Token: out.Data.Token, ServiceAccount: out.Data.ServiceAccount, Namespace: out.Data.Namespace,
		LeaseID: out.LeaseID, LeaseDuration: seconds(out.LeaseDuration),
	}, nil
}

var _ API = (*Client)(nil)
