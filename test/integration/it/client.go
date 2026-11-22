//go:build integration

package it

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Account is what the identity stand-in signs in next (POST /admin/next).
type Account struct {
	Sub           string   `json:"sub"`
	Email         string   `json:"email"`
	Name          string   `json:"name,omitempty"`
	HD            string   `json:"hd,omitempty"`
	EmailVerified *bool    `json:"email_verified,omitempty"`
	GitHubID      int64    `json:"github_id,omitempty"`
	GitHubOrgs    []string `json:"github_orgs,omitempty"`
	Org403        bool     `json:"org_403,omitempty"`
}

// FakeIDPURL is where the host reaches the identity stand-in; inside Docker it is
// kubelook-it-fakeidp:8300, and Dial maps that name to the published port.
const FakeIDPURL = "http://kubelook-it-fakeidp:8300"

// Client is an HTTP client with a cookie jar that behaves like one browser tab.
type Client struct {
	t    testing.TB
	HC   *http.Client
	CSRF string // set by SignIn* when the session is valid
	// Base is the address requests go to; empty means BaseURL(). Origin is what a WebSocket announces as its
	// origin; empty means Base. Both differ from the default only for a Client made by On.
	Base   string
	Origin string

	mu         sync.Mutex
	setCookies []*http.Cookie // every Set-Cookie the server sent, in order, for attribute checks
}

// recorder keeps the Set-Cookie headers of every response, including redirects the client follows.
type recorder struct {
	next http.RoundTripper
	c    *Client
}

func (r recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err == nil {
		r.c.mu.Lock()
		r.c.setCookies = append(r.c.setCookies, resp.Cookies()...)
		r.c.mu.Unlock()
	}
	return resp, err
}

// SetCookie returns the last Set-Cookie with that name the server sent, or nil.
func (c *Client) SetCookie(name string) *http.Cookie {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.setCookies) - 1; i >= 0; i-- {
		if c.setCookies[i].Name == name {
			return c.setCookies[i]
		}
	}
	return nil
}

// base is the address this client talks to.
func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return BaseURL()
}

// origin is the Origin header of this client's WebSocket upgrades.
func (c *Client) origin() string {
	if c.Origin != "" {
		return c.Origin
	}
	return c.base()
}

// On returns a client with the same cookies and CSRF token that talks to another replica. Its WebSocket
// upgrades still announce this client's origin: the replicas stand behind one public name in production,
// so each of them accepts only that one.
func (c *Client) On(base string) *Client {
	return &Client{t: c.t, HC: c.HC, CSRF: c.CSRF, Base: strings.TrimRight(base, "/"), Origin: c.origin()}
}

// NoFollow returns a client that shares the cookie jar and transport but does not follow redirects.
func (c *Client) NoFollow() *http.Client {
	hc := *c.HC
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &hc
}

// NewClient returns a client with its own cookie jar and no session.
func NewClient(t testing.TB) *Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasPrefix(addr, "kubelook-it-fakeidp:") {
				addr = "127.0.0.1:" + strings.TrimPrefix(addr, "kubelook-it-fakeidp:")
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	t.Cleanup(tr.CloseIdleConnections)
	c := &Client{t: t}
	c.HC = &http.Client{Jar: jar, Transport: recorder{next: tr, c: c}, Timeout: 60 * time.Second}
	return c
}

// Announce tells the identity stand-in which account signs in next.
func (c *Client) Announce(acct Account) {
	c.t.Helper()
	body, err := json.Marshal(acct)
	if err != nil {
		c.t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, FakeIDPURL+"/admin/next", bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HC.Do(req)
	if err != nil {
		c.t.Fatalf("announcing the account: %v", err)
	}
	_ = resp.Body.Close()
}

// signIn follows the redirects of /auth/<provider>/login and returns the URL it ends on.
func (c *Client) signIn(provider string, acct Account) *url.URL {
	c.t.Helper()
	c.Announce(acct)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.base()+"/auth/"+provider+"/login", nil)
	if err != nil {
		c.t.Fatal(err)
	}
	resp, err := c.HC.Do(req)
	if err != nil {
		c.t.Fatalf("signing in with %s: %v", provider, err)
	}
	_ = resp.Body.Close()
	c.loadCSRF()
	return resp.Request.URL
}

// SignInGoogle signs in through the stand-in's OpenID Connect flow and returns the final URL:
// "/" on success, "/?signin_error=<reason>" when Kubelook refuses the account.
func (c *Client) SignInGoogle(acct Account) *url.URL { return c.signIn("google", acct) }

// SignInGitHub is SignInGoogle for the GitHub flow.
func (c *Client) SignInGitHub(acct Account) *url.URL { return c.signIn("github", acct) }

// loadCSRF reads /api/v1/me; when the session is valid it keeps the CSRF token.
func (c *Client) loadCSRF() {
	c.t.Helper()
	var me struct {
		CSRFToken string `json:"csrfToken"`
	}
	c.CSRF = ""
	if status := c.JSON(http.MethodGet, "/api/v1/me", nil, &me); status == http.StatusOK {
		c.CSRF = me.CSRFToken
	}
}

// Do sends a request with the CSRF token (when known) and returns status and body.
func (c *Client) Do(method, path string, body []byte, headers map[string]string) (int, []byte) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, c.base()+path, bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if len(body) > 0 && headers["Content-Type"] == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.CSRF != "" {
		req.Header.Set("X-CSRF-Token", c.CSRF)
	}
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.HC.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("reading %s %s: %v", method, path, err)
	}
	return resp.StatusCode, data
}

// JSON sends in (when not nil) as JSON and decodes a JSON answer into out (when not nil).
// Error answers decode into out as well, so a test can read {"error":{...}} through its own type.
func (c *Client) JSON(method, path string, in, out any) int {
	c.t.Helper()
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			c.t.Fatal(err)
		}
	}
	status, data := c.Do(method, path, body, nil)
	if out != nil && len(data) > 0 {
		_ = json.Unmarshal(data, out)
	}
	return status
}

// APIError is the standard error body.
type APIError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// AddVaultToken stores a Vault token as the session's credential and returns the sealed copy.
func (c *Client) AddVaultToken(token string) (sealed string) {
	c.t.Helper()
	var out struct {
		Sealed string `json:"sealed"`
	}
	if status := c.JSON(http.MethodPost, "/api/v1/credentials/vault", map[string]string{"token": token}, &out); status != http.StatusOK {
		c.t.Fatalf("adding the Vault token: status %d", status)
	}
	return out.Sealed
}

// UploadKubeconfig uploads a kubeconfig under a label and returns the sealed copy.
func (c *Client) UploadKubeconfig(label string, content []byte) (sealed string) {
	c.t.Helper()
	var out struct {
		Sealed string `json:"sealed"`
	}
	in := map[string]string{"label": label, "content": string(content)}
	if status := c.JSON(http.MethodPost, "/api/v1/credentials/kubeconfig", in, &out); status != http.StatusOK {
		c.t.Fatalf("uploading the kubeconfig: status %d", status)
	}
	return out.Sealed
}

// Multipart builds a multipart/form-data body with one file part.
func Multipart(field, filename string, content []byte) (body []byte, contentType string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile(field, filename)
	_, _ = part.Write(content)
	_ = w.Close()
	return buf.Bytes(), w.FormDataContentType()
}
