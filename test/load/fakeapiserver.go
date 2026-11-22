//go:build load

package load

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"time"
)

// emittedAnnotation carries the time an event was emitted, in Unix nanoseconds, so a client can measure how long
// the event took to reach it.
const emittedAnnotation = "load.kubelook.io/emitted"

// fakeAPIServer is a Kubernetes API server that serves one resource, pods: a list of n generated pods and, for
// every watch, a stream of events at a fixed rate. Every watch is a separate stream, as every user holds their own.
type fakeAPIServer struct {
	srv        *httptest.Server
	pods       int
	eventEvery time.Duration
	rv         atomic.Int64
	watches    atomic.Int64
}

func newFakeAPIServer(pods, eventsPerSecond int) *fakeAPIServer {
	f := &fakeAPIServer{pods: pods, eventEvery: time.Second / time.Duration(max(eventsPerSecond, 1))}
	f.rv.Store(1000)
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	return f
}

func (f *fakeAPIServer) URL() string { return f.srv.URL }
func (f *fakeAPIServer) Close()      { f.srv.Close() }

// CAPEM is the certificate a kubeconfig must trust to reach the server.
func (f *fakeAPIServer) CAPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw})
}

// Watches is the number of watch streams opened so far.
func (f *fakeAPIServer) Watches() int { return int(f.watches.Load()) }

func (f *fakeAPIServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/pods" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		return
	}
	if r.URL.Query().Get("watch") == "true" || r.URL.Query().Get("watch") == "1" {
		f.watch(w, r)
		return
	}
	items := make([]map[string]any, f.pods)
	for i := range items {
		items[i] = f.pod(i, "1000", "")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"apiVersion": "v1", "kind": "PodList", "metadata": map[string]any{"resourceVersion": "1000"}, "items": items,
	})
}

func (f *fakeAPIServer) pod(i int, rv, emitted string) map[string]any {
	meta := map[string]any{
		"name": "pod-" + strconv.Itoa(i), "namespace": "ns-" + strconv.Itoa(i%20), "uid": "uid-" + strconv.Itoa(i),
		"resourceVersion": rv, "creationTimestamp": "2026-11-21T10:00:00Z",
		"labels": map[string]any{"app": "app-" + strconv.Itoa(i%50)},
	}
	if emitted != "" {
		meta["annotations"] = map[string]any{emittedAnnotation: emitted}
	}
	return map[string]any{
		"apiVersion": "v1", "kind": "Pod", "metadata": meta,
		"spec":   map[string]any{"containers": []any{map[string]any{"name": "app", "image": "registry.example.org/app:1.0"}}, "nodeName": "node-" + strconv.Itoa(i%10)},
		"status": map[string]any{"phase": "Running", "podIP": "10.0.0." + strconv.Itoa(i%250)},
	}
}

// watch streams one MODIFIED event per tick, until the client goes away.
func (f *fakeAPIServer) watch(w http.ResponseWriter, r *http.Request) {
	f.watches.Add(1)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	enc := json.NewEncoder(w)
	tick := time.NewTicker(f.eventEvery)
	defer tick.Stop()
	for n := 0; ; n++ {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
		rv := strconv.FormatInt(f.rv.Add(1), 10)
		ev := map[string]any{"type": "MODIFIED", "object": f.pod(n%f.pods, rv, strconv.FormatInt(time.Now().UnixNano(), 10))}
		if err := enc.Encode(ev); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}
