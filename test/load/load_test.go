//go:build load

// Package load measures the watch hub at the scale the spec proposes: memory per watch, the time from an event
// to a client, the time to the first snapshot, and refusals. It uses real time and a real server on loopback; it
// is a measurement, not a unit test, and it never runs in the ci workflow.
package load

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/app"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
)

// parameters are read from the environment so that hack/load.sh and the nightly run can change them.
type parameters struct {
	Pods, Sessions, EventsPerSecond int
	Duration                        time.Duration
}

func envInt(t *testing.T, name string, def int) int {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	require.NoError(t, err, name)
	require.Positive(t, n, name)
	return n
}

func readParameters(t *testing.T) parameters {
	t.Helper()
	return parameters{
		Pods: envInt(t, "LOAD_PODS", 1000), Sessions: envInt(t, "LOAD_SESSIONS", 200),
		EventsPerSecond: envInt(t, "LOAD_EVENTS_PER_SECOND", 50), Duration: time.Duration(envInt(t, "LOAD_SECONDS", 60)) * time.Second,
	}
}

// liveHeap is the live heap after a full collection.
func liveHeap() uint64 {
	runtime.GC()
	s := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return s[0].Value.Uint64()
}

type user struct {
	cookie, csrf string
}

// result is what one client saw.
type result struct {
	firstSnapshot time.Duration // from the open request to the last snapshot frame
	latencies     []time.Duration
	events        int
	refusal       string // the error code of a refused open, if any
	err           error
}

func TestLoad(t *testing.T) {
	p := readParameters(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	api := newFakeAPIServer(p.Pods, p.EventsPerSecond)
	defer api.Close()

	// A real Kubelook server on a loopback port, with its own SQLite database.
	dbPath := filepath.Join(t.TempDir(), "load.db")
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("server:\n  publicURL: http://localhost\n  listenAddress: 127.0.0.1:0\ndatabase:\n  sqlitePath: "+dbPath+"\n"), 0o600))
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	cfg, err := config.Load(cfgPath, func(k string) string {
		if k == "KUBELOOK_ENCRYPTION_KEY" {
			return key
		}
		return ""
	})
	require.NoError(t, err)
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ready := make(chan app.Ready, 1)
	served := make(chan error, 1)
	go func() {
		served <- app.Serve(ctx, cfg, app.ServeOptions{Listener: ln, Stdout: io.Discard, LogLevel: slog.LevelError, OnReady: func(r app.Ready) { ready <- r }})
	}()
	select {
	case <-ready:
	case err := <-served:
		t.Fatalf("the server stopped before it was ready: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not become ready")
	}
	base := "http://" + ln.Addr().String()

	users := signInUsers(t, ctx, dbPath, p.Sessions)
	kubeconfig := kubeconfigFor(api)
	uploadKubeconfigs(t, ctx, base, users, kubeconfig)

	before := liveHeap()
	results := make([]result, len(users))
	var snapshots atomic.Int64
	allSnapshots := make(chan struct{})
	var once sync.Once
	runCtx, stop := context.WithTimeout(ctx, p.Duration+2*time.Minute)
	defer stop()
	var done sync.WaitGroup
	for i := range users {
		done.Add(1)
		go func() {
			defer done.Done()
			var counted bool
			markOpened := func() {
				if !counted {
					counted = true
					if snapshots.Add(1) == int64(len(users)) {
						once.Do(func() { close(allSnapshots) })
					}
				}
			}
			defer markOpened()
			results[i] = watchPods(runCtx, base, users[i], markOpened)
		}()
	}
	select {
	case <-allSnapshots:
	case <-time.After(2 * time.Minute):
		t.Log("not every client had its snapshot after two minutes")
	}
	after := liveHeap()

	// Let the events flow for the measured time, then stop the clients.
	time.Sleep(p.Duration)
	stop()
	done.Wait()
	cancel()
	select {
	case err := <-served:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Log("the server did not stop in 30 s")
	}

	report(t, p, results, before, after, api.Watches())
}

// signInUsers creates one user and one session per client in the server's database, the way sign-in would.
func signInUsers(t *testing.T, ctx context.Context, dbPath string, n int) []user {
	t.Helper()
	db, err := store.Open(ctx, store.Options{Driver: "sqlite", SQLitePath: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	users := store.NewUsers(db, clock.Real(), store.NewID)
	mgr := session.NewManager(session.Deps{
		Store: store.NewSessions(db), Clock: clock.Real(), Rand: rand.Reader, Log: slog.New(slog.DiscardHandler),
		IdleTimeout: time.Hour, MaxLifetime: 24 * time.Hour,
	})
	out := make([]user, n)
	for i := range out {
		u, err := users.Upsert(ctx, "google", fmt.Sprintf("load-%d", i), fmt.Sprintf("load-%d@example.org", i), fmt.Sprintf("Load %d", i))
		require.NoError(t, err)
		id, s, err := mgr.Create(ctx, u.ID)
		require.NoError(t, err)
		out[i] = user{cookie: "kubelook_session=" + id, csrf: s.CSRFToken}
	}
	return out
}

const clusterID = "k:load/load"

func kubeconfigFor(api *fakeAPIServer) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: load
    cluster:
      server: %s
      certificate-authority-data: %s
users:
  - name: load
    user:
      token: load-token
contexts:
  - name: load
    context: {cluster: load, user: load}
current-context: load
`, api.URL(), base64.StdEncoding.EncodeToString(api.CAPEM()))
}

func uploadKubeconfigs(t *testing.T, ctx context.Context, base string, users []user, kubeconfig string) {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	var failed atomic.Value
	for _, u := range users {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			body, _ := json.Marshal(map[string]string{"label": "load", "content": kubeconfig})
			for _, call := range []struct{ path, body string }{{"/api/v1/credentials/kubeconfig", string(body)}, {"/api/v1/clusters/" + url.PathEscape(clusterID) + "/open", ""}} {
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+call.path, strings.NewReader(call.body))
				if err != nil {
					failed.Store(err)
					return
				}
				req.Header.Set("Cookie", u.cookie)
				req.Header.Set("X-CSRF-Token", u.csrf)
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					failed.Store(err)
					return
				}
				b, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					failed.Store(fmt.Errorf("%s: status %d: %s", call.path, resp.StatusCode, b))
					return
				}
			}
		}()
	}
	wg.Wait()
	if err, _ := failed.Load().(error); err != nil {
		t.Fatalf("preparing the clients: %v", err)
	}
}

type frame struct {
	Ch int    `json:"ch"`
	T  string `json:"t"`
	D  struct {
		More   bool   `json:"more"`
		Code   string `json:"code"`
		Object struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"object"`
	} `json:"d"`
}

// watchPods is one browser tab: it opens a pods watch and reads until ctx is done.
func watchPods(ctx context.Context, base string, u user, snapshotted func()) (res result) {
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {u.cookie}}}) //nolint:bodyclose // a WebSocket handshake response has no body
	if err != nil {
		res.err = err
		return res
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(64 << 20)
	open, _ := json.Marshal(map[string]any{"op": "watch.open", "ch": 1, "d": map[string]string{
		"cluster": clusterID, "group": "core", "version": "v1", "resource": "pods", "namespace": "",
	}})
	start := time.Now()
	if err := c.Write(ctx, websocket.MessageText, open); err != nil {
		res.err = err
		return res
	}
	snap := false
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				res.err = err
			}
			return res
		}
		var f frame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.T {
		case "error":
			res.refusal = f.D.Code
			return res
		case "snapshot":
			if !f.D.More && !snap {
				snap = true
				res.firstSnapshot = time.Since(start)
				snapshotted()
			}
		case "event":
			res.events++
			if ns, err := strconv.ParseInt(f.D.Object.Metadata.Annotations[emittedAnnotation], 10, 64); err == nil {
				res.latencies = append(res.latencies, time.Since(time.Unix(0, ns)))
			}
		}
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, int(float64(len(sorted))*p))]
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000) }

// report prints the results and, when LOAD_DOCS names a file, appends them to it.
func report(t *testing.T, p parameters, results []result, before, after uint64, upstreamWatches int) {
	t.Helper()
	var latencies, firsts []time.Duration
	events, refused, failed := 0, 0, 0
	for _, r := range results {
		latencies = append(latencies, r.latencies...)
		if r.firstSnapshot > 0 {
			firsts = append(firsts, r.firstSnapshot)
		}
		events += r.events
		if r.refusal != "" {
			refused++
		}
		if r.err != nil {
			failed++
			t.Logf("client error: %v", r.err)
		}
	}
	slices.Sort(latencies)
	slices.Sort(firsts)
	perWatch := int64(after) - int64(before)
	if len(results) > 0 {
		perWatch /= int64(len(results))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "### %s, commit %s\n\n", time.Now().UTC().Format("2006-01-02 15:04 MST"), envOr("LOAD_COMMIT", "unknown"))
	fmt.Fprintf(&b, "Parameters: %d pods, %d sessions (one user and one watch each), %d events per second per watch, %s.\n\n",
		p.Pods, p.Sessions, p.EventsPerSecond, p.Duration)
	b.WriteString("| Measure | Result |\n| --- | --- |\n")
	fmt.Fprintf(&b, "| Live heap growth per watch (whole process, includes the load generator) | %.1f KiB |\n", float64(perWatch)/1024)
	fmt.Fprintf(&b, "| Time to the complete first snapshot, p50 / p95 / p99 | %s / %s / %s |\n", ms(percentile(firsts, .5)), ms(percentile(firsts, .95)), ms(percentile(firsts, .99)))
	fmt.Fprintf(&b, "| Event emitted → received by the client, p50 / p95 / p99 | %s / %s / %s |\n", ms(percentile(latencies, .5)), ms(percentile(latencies, .95)), ms(percentile(latencies, .99)))
	fmt.Fprintf(&b, "| Events received (latency samples) | %d (%d) |\n", events, len(latencies))
	fmt.Fprintf(&b, "| Upstream watches opened on the fake API server | %d |\n", upstreamWatches)
	fmt.Fprintf(&b, "| Refused opens | %d |\n| Clients that failed | %d |\n\n", refused, failed)
	out := b.String()
	fmt.Println(out)

	if path := os.Getenv("LOAD_DOCS"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644) //nolint:gosec // a path the person running the test names
		require.NoError(t, err)
		_, err = f.WriteString(out)
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
	require.Zero(t, failed, "clients failed")
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
