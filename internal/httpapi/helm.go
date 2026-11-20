package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/session"
)

// registerHelm mounts the Helm endpoints. They need the Helm service, the job runner and the audit recorder.
func (d Deps) registerHelm(mux *http.ServeMux) {
	if d.Helm == nil || d.Jobs == nil || d.Audit == nil {
		return
	}
	const base = "/api/v1/clusters/{cluster}/helm"
	mux.HandleFunc("GET "+base+"/releases", d.helmList)
	mux.HandleFunc("GET "+base+"/releases/{namespace}/{name}", d.helmDetail)
	mux.HandleFunc("POST "+base+"/releases/{namespace}/{name}/values/reveal", d.helmReveal)
	mux.HandleFunc("POST "+base+"/preview", d.helmPreview)
	mux.HandleFunc("POST "+base+"/jobs", d.helmStart)
	mux.HandleFunc("GET "+base+"/repositories", d.helmRepos)
	mux.HandleFunc("POST "+base+"/repositories", d.helmAddRepo)
	mux.HandleFunc("DELETE "+base+"/repositories/{name}", d.helmDeleteRepo)
}

// helmCall checks the session and that the cluster is known.
func (d Deps) helmCall(w http.ResponseWriter, r *http.Request) (session.Session, string, bool) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return s, "", false
	}
	cluster := r.PathValue("cluster")
	if _, err := d.Kube.Discovery(r.Context(), s, cluster); err != nil {
		writeError(w, r, d.Log, err)
		return s, "", false
	}
	return s, cluster, true
}

func badRequest(format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, ErrInvalid)...)
}

func validNamespace(ns string) error {
	if len(validation.IsDNS1123Label(ns)) > 0 {
		return badRequest("%q is not a namespace name", ns)
	}
	return nil
}

func (d Deps) helmList(w http.ResponseWriter, r *http.Request) {
	s, cluster, ok := d.helmCall(w, r)
	if !ok {
		return
	}
	ns := r.URL.Query().Get("namespace")
	if ns != "" {
		if err := validNamespace(ns); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
	}
	releases, err := d.Helm.List(r.Context(), s, cluster, ns)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if releases == nil {
		releases = []helm.Release{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
}

func revisionParam(r *http.Request) (int, error) {
	v := r.URL.Query().Get("revision")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, badRequest("%q is not a revision", v)
	}
	return n, nil
}

// helmDetail answers a release without its values: they come only from the audited reveal.
func (d Deps) helmDetail(w http.ResponseWriter, r *http.Request) {
	s, cluster, ok := d.helmCall(w, r)
	if !ok {
		return
	}
	ns := r.PathValue("namespace")
	if err := validNamespace(ns); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	rev, err := revisionParam(r)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	detail, history, err := d.Helm.Detail(r.Context(), s, cluster, ns, r.PathValue("name"), rev)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": detail.Name, "namespace": detail.Namespace, "revision": detail.Revision, "status": detail.Status,
		"chart": detail.Chart, "chartVersion": detail.ChartVersion, "appVersion": detail.AppVersion, "updated": detail.Updated,
		"applyMethod": detail.ApplyMethod, "manifest": detail.Manifest, "notes": detail.Notes, "hooks": detail.Hooks,
		"history": history, "valuesHidden": true,
	})
}

type revealRequest struct {
	Revision int `json:"revision"`
}

func (d Deps) helmReveal(w http.ResponseWriter, r *http.Request) {
	s, cluster, ok := d.helmCall(w, r)
	if !ok {
		return
	}
	var req revealRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if req.Revision < 0 {
		writeError(w, r, d.Log, badRequest("the revision must not be negative"))
		return
	}
	ns := r.PathValue("namespace")
	if err := validNamespace(ns); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	user, computed, err := d.Helm.Reveal(r.Context(), s, cluster, ns, r.PathValue("name"), req.Revision)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"userValues": user, "computedValues": computed})
}

type helmActionRequest struct {
	Action string `json:"action"`
	helm.JobRequest
}

func (d Deps) helmPreview(w http.ResponseWriter, r *http.Request) {
	s, cluster, ok := d.helmCall(w, r)
	if !ok {
		return
	}
	var req helmActionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	current, next, err := d.Helm.Preview(r.Context(), s, cluster, req.Action, req.JobRequest)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"currentManifest": current, "newManifest": next})
}

func (d Deps) helmStart(w http.ResponseWriter, r *http.Request) {
	s, cluster, ok := d.helmCall(w, r)
	if !ok {
		return
	}
	var req helmActionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	id, err := d.Helm.Submit(r.Context(), d.Jobs, s, cluster, req.Action, req.JobRequest)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"jobId": id})
}

func (d Deps) helmRepos(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	repos, err := d.Helm.Repos.List(r.Context(), s)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repositories": repos})
}

type addRepoRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (d Deps) helmAddRepo(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var req addRepoRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if err := d.Helm.Repos.Add(r.Context(), s, req.Name, req.URL, req.Username, req.Password); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name})
}

func (d Deps) helmDeleteRepo(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	if err := d.Helm.Repos.Delete(r.Context(), s, r.PathValue("name")); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

var _ = apperr.ErrInvalid
