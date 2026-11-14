package fakekube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

// Channel numbers of the v5.channel.k8s.io protocol.
const (
	chStdin  = 0
	chStdout = 1
	chStderr = 2
	chError  = 3
	chResize = 4
)

// ExecServer answers the pods/exec subresource over a WebSocket (v5.channel.k8s.io), for tests of the
// executor. Bytes on stdin are echoed on stdout; the line "exit\n" ends the command.
type ExecServer struct {
	*httptest.Server
	// FailStart, when not empty, is sent as the Failure status of a command that could not start.
	FailStart string
	// ExitCode is the exit code reported when the command ends: 0 is Success.
	ExitCode int

	mu      sync.Mutex
	queries []url.Values
	paths   []string
	resizes []string
}

// NewExecServer starts the server.
func NewExecServer() *ExecServer {
	s := &ExecServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Queries returns the query of every exec request, in order.
func (s *ExecServer) Queries() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.queries...)
}

// Paths returns the path of every exec request.
func (s *ExecServer) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// Resizes returns the resize messages received, as JSON text.
func (s *ExecServer) Resizes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.resizes...)
}

func (s *ExecServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.queries = append(s.queries, r.URL.Query())
	s.paths = append(s.paths, r.URL.Path)
	s.mu.Unlock()
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"v5.channel.k8s.io"}})
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	ctx := r.Context()
	if s.FailStart != "" {
		s.status(ctx, c, failure(s.FailStart, 0))
		_ = c.Close(websocket.StatusNormalClosure, "")
		return
	}
	for {
		_, data, err := c.Read(ctx)
		if err != nil || len(data) == 0 {
			return
		}
		switch data[0] {
		case chStdin:
			_ = c.Write(ctx, websocket.MessageBinary, append([]byte{chStdout}, data[1:]...))
			if strings.TrimSpace(string(data[1:])) == "exit" {
				s.status(ctx, c, s.exitStatus())
				_ = c.Close(websocket.StatusNormalClosure, "")
				return
			}
		case chResize:
			s.mu.Lock()
			s.resizes = append(s.resizes, string(data[1:]))
			s.mu.Unlock()
		}
	}
}

func (s *ExecServer) exitStatus() map[string]any {
	if s.ExitCode == 0 {
		return map[string]any{"metadata": map[string]any{}, "status": "Success"}
	}
	return failure("command terminated with non-zero exit code", s.ExitCode)
}

func failure(message string, code int) map[string]any {
	st := map[string]any{"metadata": map[string]any{}, "status": "Failure", "message": message, "reason": "InternalError"}
	if code != 0 {
		st["reason"] = "NonZeroExitCode"
		st["details"] = map[string]any{"causes": []any{map[string]any{"reason": "ExitCode", "message": itoa(code)}}}
	}
	return st
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func (s *ExecServer) status(ctx context.Context, c *websocket.Conn, st map[string]any) {
	b, _ := json.Marshal(st)
	_ = c.Write(ctx, websocket.MessageBinary, append([]byte{chError}, b...))
}
