package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/session"
)

// registerLogs mounts the log download record endpoint. Without an audit recorder nothing is mounted.
func (d Deps) registerLogs(mux *http.ServeMux) {
	if d.Audit == nil {
		return
	}
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/logs/downloads", d.logDownload)
}

type logDownloadRequest struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Format    string `json:"format"`
	From      string `json:"from"`
	To        string `json:"to"`
	Lines     int    `json:"lines"`
}

// logDownload records that the browser is about to save a log (text or NDJSON). The browser saves only after
// this answers 204; the record says where and how much, never what.
func (d Deps) logDownload(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var req logDownloadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if req.Namespace == "" || req.Pod == "" || (req.Format != "text" && req.Format != "ndjson") || req.Lines < 0 {
		writeError(w, r, d.Log, fmt.Errorf("a namespace, a pod, the format text or ndjson and a line count are required: %w", ErrInvalid))
		return
	}
	for _, ts := range []string{req.From, req.To} {
		if ts == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
			writeError(w, r, d.Log, fmt.Errorf("from and to must be RFC 3339 times: %w", ErrInvalid))
			return
		}
	}
	cluster := r.PathValue("cluster")
	if _, err := d.Kube.Discovery(r.Context(), s, cluster); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	details := map[string]any{"container": req.Container, "format": req.Format, "lines": req.Lines}
	if req.From != "" {
		details["from"] = req.From
	}
	if req.To != "" {
		details["to"] = req.To
	}
	err := d.Audit.Record(r.Context(), audit.Event{
		Cluster: cluster, Namespace: req.Namespace, Verb: audit.VerbLogDownload, ResourceKind: "Pod", ResourceName: req.Pod,
		Outcome: "success", Details: details,
	})
	if err != nil {
		d.Log.Error("audit record could not be written", "verb", audit.VerbLogDownload, "error", err.Error())
		writeError(w, r, d.Log, errNotRecorded)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
