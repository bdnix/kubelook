// Package fakevault is an httptest Vault double for unit tests. It implements only the six
// endpoints Kubelook uses, with Vault's JSON shapes, and can be made unreachable or failing.
package fakevault

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Token describes one valid token.
type Token struct {
	TTLSeconds  int
	Renewable   bool
	Type        string // "service" | "batch"
	Policies    []string
	EntityID    string
	DisplayName string
	ExpireTime  string // RFC 3339; empty → null
	// Caps maps a path (no leading slash) to its capabilities for this token.
	Caps map[string][]string
}

// ClusterConfig is the data of GET <mount>/config.
type ClusterConfig struct{ Host, CACert string }

// Creds is the answer of POST <mount>/creds/<role>.
type Creds struct {
	Token, ServiceAccount, Namespace, LeaseID string
	LeaseDurationSeconds                      int
}

// Server is the fake. Configure it before the first request; the switches may change at any time.
type Server struct {
	srv *httptest.Server

	mu       sync.Mutex
	tokens   map[string]Token
	mounts   map[string]string        // "kubernetes/prod/" → type
	configs  map[string]ClusterConfig // "kubernetes/prod" → config; missing → 404
	creds    map[string]Creds         // "kubernetes/prod/creds/role" → creds
	credsErr map[string]string        // same key → error message answered with HTTP 500
	counts   map[string]int
	capCalls [][]string
	credsReq []map[string]any
	forced   int
	hangUp   bool
	padBytes int
}

// New starts the fake. Close it with Close.
func New() *Server {
	s := &Server{
		tokens: map[string]Token{}, mounts: map[string]string{}, configs: map[string]ClusterConfig{},
		creds: map[string]Creds{}, credsErr: map[string]string{}, counts: map[string]int{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// URL is the address of the fake.
func (s *Server) URL() string { return s.srv.URL }

// Client returns an HTTP client for the fake.
func (s *Server) Client() *http.Client { return s.srv.Client() }

// Close stops the server.
func (s *Server) Close() { s.srv.Close() }

// AddToken registers a valid token.
func (s *Server) AddToken(value string, t Token) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.Type == "" {
		t.Type = "service"
	}
	s.tokens[value] = t
}

// AddMount registers a secrets mount; path ends with "/" as in Vault's answers.
func (s *Server) AddMount(path, typ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mounts[path] = typ
}

// SetConfig sets the cluster config of a mount (path without trailing slash).
func (s *Server) SetConfig(mount string, c ClusterConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[mount] = c
}

// SetCreds sets the answer for creds on mount and role.
func (s *Server) SetCreds(mount, role string, c Creds) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds[mount+"/creds/"+role] = c
}

// SetCredsError makes creds on mount and role fail with HTTP 500 and this message.
func (s *Server) SetCredsError(mount, role, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credsErr[mount+"/creds/"+role] = msg
}

// ForceStatus makes every request answer with this status and an empty errors list (0 = off).
func (s *Server) ForceStatus(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forced = code
}

// HangUp makes the server close connections without answering (an unreachable Vault).
func (s *Server) HangUp(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hangUp = on
}

// PadResponses appends n bytes of padding to every successful JSON answer (for size-limit tests).
func (s *Server) PadResponses(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.padBytes = n
}

// Calls returns how often an endpoint was called: "lookup-self", "renew-self", "mounts", "capabilities", "config", "creds".
func (s *Server) Calls(endpoint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[endpoint]
}

// CapabilityRequests returns the paths of every capabilities-self call, in order.
func (s *Server) CapabilityRequests() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.capCalls...)
}

// CredsRequests returns the decoded body of every creds call, in order.
func (s *Server) CredsRequests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.credsReq...)
}

func writeJSON(w http.ResponseWriter, status int, v any, pad int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
	if pad > 0 {
		_, _ = w.Write([]byte(strings.Repeat(" ", pad)))
	}
}

func writeErrors(w http.ResponseWriter, status int, msgs ...string) {
	writeJSON(w, status, map[string]any{"errors": msgs}, 0)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hangUp {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
		return
	}
	if s.forced != 0 {
		writeErrors(w, s.forced)
		return
	}
	if r.Header.Get("X-Vault-Request") != "true" {
		writeErrors(w, http.StatusBadRequest, "missing X-Vault-Request")
		return
	}
	tokenValue := r.Header.Get("X-Vault-Token")
	tok, ok := s.tokens[tokenValue]
	if !ok {
		writeErrors(w, http.StatusForbidden, "permission denied")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	switch {
	case r.Method == http.MethodGet && path == "auth/token/lookup-self":
		s.counts["lookup-self"]++
		var expire any
		if tok.ExpireTime != "" {
			expire = tok.ExpireTime
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"ttl": tok.TTLSeconds, "renewable": tok.Renewable, "type": tok.Type, "policies": tok.Policies,
			"entity_id": tok.EntityID, "display_name": tok.DisplayName, "expire_time": expire,
		}}, s.padBytes)
	case r.Method == http.MethodPost && path == "auth/token/renew-self":
		s.counts["renew-self"]++
		writeJSON(w, http.StatusOK, map[string]any{"auth": map[string]any{"lease_duration": tok.TTLSeconds, "renewable": tok.Renewable}}, s.padBytes)
	case r.Method == http.MethodGet && path == "sys/internal/ui/mounts":
		s.counts["mounts"]++
		secret := map[string]any{}
		for p, typ := range s.mounts {
			secret[p] = map[string]any{"type": typ}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"secret": secret, "auth": map[string]any{}}}, s.padBytes)
	case r.Method == http.MethodPost && path == "sys/capabilities-self":
		s.counts["capabilities"]++
		var in struct {
			Paths []string `json:"paths"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.capCalls = append(s.capCalls, in.Paths)
		data := map[string]any{}
		for _, p := range in.Paths {
			caps := tok.Caps[p]
			if caps == nil {
				caps = []string{"deny"}
			}
			data[p] = caps
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data}, s.padBytes)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/config"):
		s.counts["config"]++
		c, found := s.configs[strings.TrimSuffix(path, "/config")]
		if !found {
			writeErrors(w, http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"kubernetes_host": c.Host, "kubernetes_ca_cert": c.CACert}}, s.padBytes)
	case r.Method == http.MethodPost && strings.Contains(path, "/creds/"):
		s.counts["creds"]++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.credsReq = append(s.credsReq, body)
		if msg, failing := s.credsErr[path]; failing {
			writeErrors(w, http.StatusInternalServerError, msg)
			return
		}
		c, found := s.creds[path]
		if !found {
			writeErrors(w, http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"lease_id": c.LeaseID, "lease_duration": c.LeaseDurationSeconds, "data": map[string]any{
			"service_account_token": c.Token, "service_account_name": c.ServiceAccount, "service_account_namespace": c.Namespace,
		}}, s.padBytes)
	default:
		writeErrors(w, http.StatusNotFound)
	}
}
