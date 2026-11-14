package httpapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

type writeEnv struct {
	*kubeEnv
	auditOut *bytes.Buffer
}

func newWriteEnv(t *testing.T) *writeEnv {
	t.Helper()
	e := &writeEnv{kubeEnv: newKubeEnv(t), auditOut: &bytes.Buffer{}}
	n := 0
	e.deps.Audit = audit.NewRecorder(e.db, e.fake, e.auditOut, func() string { n++; return fmt.Sprintf("audit-%d", n) })
	_ = e.fakes.Dyn.Tracker().Add(withRV(fakekube.ConfigMap("ns1", "cm", map[string]any{"a": "1"}), "5"))
	_ = e.fakes.Dyn.Tracker().Add(fakekube.Pod("ns1", "web-1", nil))
	_ = e.fakes.Dyn.Tracker().Add(fakekube.Secret("ns1", "db", map[string]any{"password": "c2VjcmV0"}))
	_ = e.fakes.Dyn.Tracker().Add(fakekube.Secret("ns1", "sh.helm.release.v1.web.v1", map[string]any{"release": "aGVsbQ=="}))
	return e
}

// sendAs sends a request with a content type and the session cookie and CSRF token.
func (e *writeEnv) sendAs(method, path, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "kubelook_session", Value: e.id})
	req.Header.Set("X-CSRF-Token", e.csrf)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	httpapi.New(e.deps).ServeHTTP(rec, req)
	return rec
}

func (e *writeEnv) auditLines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(e.auditOut.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m), l)
		out = append(out, m)
	}
	return out
}

func (e *writeEnv) verbsAndOutcomes(t *testing.T) []string {
	var out []string
	for _, l := range e.auditLines(t) {
		out = append(out, l["verb"].(string)+":"+l["outcome"].(string))
	}
	return out
}

const cmBase = "/api/v1/clusters/v:prod/resources/core/v1/configmaps"

func cmBody(name, rv, a string) string {
	meta := fmt.Sprintf(`"name":%q,"namespace":"ns1"`, name)
	if rv != "" {
		meta += fmt.Sprintf(`,"resourceVersion":%q`, rv)
	}
	return fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{%s},"data":{"a":%q}}`, meta, a)
}

func TestPut_Success_IsAuditedBeforeAndAfter(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPut, cmBase+"/cm?namespace=ns1", "application/json", cmBody("cm", "9", "2"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"a":"2"`)
	assert.Equal(t, []string{"update:requested", "update:success"}, e.verbsAndOutcomes(t))
	first := e.auditLines(t)[0]
	assert.Equal(t, "v:prod", first["cluster"])
	assert.Equal(t, "ns1", first["namespace"])
	assert.Equal(t, map[string]any{"kind": "configmaps", "name": "cm"}, first["resource"])
}

func TestPut_Validation(t *testing.T) {
	e := newWriteEnv(t)
	tests := []struct{ name, path, body, want string }{
		{"no resourceVersion", "/cm?namespace=ns1", cmBody("cm", "", "2"), "resourceVersion is required"},
		{"name differs", "/cm?namespace=ns1", cmBody("other", "9", "2"), "name"},
		{"namespace differs", "/cm?namespace=ns2", cmBody("cm", "9", "2"), "namespace"},
		{"kind differs", "/cm?namespace=ns1", `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"cm","namespace":"ns1","resourceVersion":"9"}}`, "kind"},
		{"not json", "/cm?namespace=ns1", `{`, "JSON"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.sendAs(http.MethodPut, cmBase+tc.path, "application/json", tc.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Equal(t, "bad_request", errorCode(t, rec))
			assert.Contains(t, rec.Body.String(), tc.want)
		})
	}
	assert.Empty(t, e.auditLines(t), "rejected requests are not actions")
}

func TestPut_ConflictAndAdmission(t *testing.T) {
	e := newWriteEnv(t)
	gr := schema.GroupResource{Resource: "configmaps"}
	e.fakes.Dyn.PrependReactor("update", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(gr, "cm", errors.New("stale"))
	})
	rec := e.sendAs(http.MethodPut, cmBase+"/cm?namespace=ns1", "application/json", cmBody("cm", "9", "2"))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "conflict", errorCode(t, rec))
	assert.Contains(t, rec.Body.String(), "The object changed since you loaded it.")
	assert.Equal(t, []string{"update:requested", "update:failure"}, e.verbsAndOutcomes(t))

	e.fakes.Dyn.PrependReactor("update", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewBadRequest(`admission webhook "policy" denied the request: no latest tags`)
	})
	rec = e.sendAs(http.MethodPut, cmBase+"/cm?namespace=ns1", "application/json", cmBody("cm", "9", "2"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "unprocessable", errorCode(t, rec))
	assert.Contains(t, rec.Body.String(), "no latest tags")

	e.fakes.Dyn.PrependReactor("update", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(gr, "cm", errors.New("webhook says no"))
	})
	rec = e.sendAs(http.MethodPut, cmBase+"/cm?namespace=ns1", "application/json", cmBody("cm", "9", "2"))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "webhook says no")
}

func TestPut_DryRun_ReturnsLiveAndResultWithoutAudit(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPut, cmBase+"/cm?namespace=ns1&dryRun=true", "application/json", cmBody("cm", "9", "2"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Live, Result struct{ Data map[string]string }
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "1", got.Live.Data["a"])
	assert.Equal(t, "2", got.Result.Data["a"])
	assert.Empty(t, e.auditLines(t))
}

func TestPut_DryRunUnsupported_422(t *testing.T) {
	e := newWriteEnv(t)
	e.fakes.Dyn.PrependReactor("update", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewBadRequest("the resource does not support dry run")
	})
	rec := e.sendAs(http.MethodPut, cmBase+"/cm?namespace=ns1&dryRun=true", "application/json", cmBody("cm", "9", "2"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "dry_run_unsupported", errorCode(t, rec))
}

func TestPut_Secret_MasksRestoredAndResponseRedacted(t *testing.T) {
	e := newWriteEnv(t)
	body := `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"db","namespace":"ns1","resourceVersion":"3"},"data":{"password":"********"}}`
	rec := e.sendAs(http.MethodPut, "/api/v1/clusters/v:prod/resources/core/v1/secrets/db?namespace=ns1", "application/json", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "c2VjcmV0")
	assert.Contains(t, rec.Body.String(), "********")

	helm := `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"sh.helm.release.v1.web.v1","namespace":"ns1","resourceVersion":"3"},"data":{"release":"eA=="}}`
	rec = e.sendAs(http.MethodPut, "/api/v1/clusters/v:prod/resources/core/v1/secrets/sh.helm.release.v1.web.v1?namespace=ns1", "application/json", helm)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAuditFailure_RefusesTheActionBeforeTheAPICall(t *testing.T) {
	e := newWriteEnv(t)
	dead := storetest.New(t)
	e.deps.Audit = audit.NewRecorder(dead, e.fake, e.auditOut, func() string { return "x" })
	require.NoError(t, dead.Close())
	calls := 0
	e.fakes.Dyn.PrependReactor("*", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
		calls++
		return false, nil, nil
	})
	rec := e.sendAs(http.MethodPut, cmBase+"/cm?namespace=ns1", "application/json", cmBody("cm", "9", "2"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal", errorCode(t, rec))
	assert.Contains(t, rec.Body.String(), "The action was refused because it could not be recorded.")
	assert.Zero(t, calls)
}

func TestPost_CreatesAndAudits(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPost, cmBase+"?namespace=ns1", "application/json", cmBody("fresh", "", "x"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"create:requested", "create:success"}, e.verbsAndOutcomes(t))

	rec = e.sendAs(http.MethodPost, cmBase+"?namespace=ns1", "application/json", cmBody("cm", "", "x"))
	assert.Equal(t, http.StatusConflict, rec.Code, "already exists")

	rec = e.sendAs(http.MethodPost, cmBase+"?namespace=ns1&dryRun=true", "application/json", cmBody("dry", "", "x"))
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Len(t, e.auditLines(t), 4, "the dry run added no record")

	rec = e.sendAs(http.MethodPost, cmBase+"?namespace=ns1", "application/json", `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"namespace":"ns1"}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "name is required")
}

func TestPatch_MergeAndStrategic(t *testing.T) {
	e := newWriteEnv(t)
	var got []string
	e.fakes.Dyn.PrependReactor("patch", "configmaps", func(a clienttesting.Action) (bool, runtime.Object, error) {
		got = append(got, string(a.(clienttesting.PatchAction).GetPatchType()))
		return true, fakekube.ConfigMap("ns1", "cm", nil), nil // the fake cannot apply strategic patches itself
	})
	for _, ct := range []string{"application/merge-patch+json", "application/strategic-merge-patch+json"} {
		rec := e.sendAs(http.MethodPatch, cmBase+"/cm?namespace=ns1", ct, `{"data":{"a":"3"}}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	assert.Equal(t, []string{"application/merge-patch+json", "application/strategic-merge-patch+json"}, got)
	assert.Equal(t, []string{"patch:requested", "patch:success", "patch:requested", "patch:success"}, e.verbsAndOutcomes(t))

	rec := e.sendAs(http.MethodPatch, cmBase+"/cm?namespace=ns1", "application/json", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = e.sendAs(http.MethodPatch, cmBase+"/cm?namespace=ns1", "application/merge-patch+json", strings.Repeat("a", 1<<20+1))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestDelete_PolicyAuditAndMissing(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodDelete, "/api/v1/clusters/v:prod/resources/core/v1/pods/web-1?namespace=ns1&propagationPolicy=Foreground", "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"delete:requested", "delete:success"}, e.verbsAndOutcomes(t))

	rec = e.sendAs(http.MethodDelete, "/api/v1/clusters/v:prod/resources/core/v1/pods/web-1?namespace=ns1", "", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = e.sendAs(http.MethodDelete, "/api/v1/clusters/v:prod/resources/core/v1/pods/web-1?namespace=ns1&propagationPolicy=Sideways", "", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWrites_NeedCSRFAndAuditWiring(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.doWithSession(http.MethodDelete, "/api/v1/clusters/v:prod/resources/core/v1/pods/web-1?namespace=ns1", e.id, "")
	assert.Equal(t, "csrf", errorCode(t, rec))
	e.deps.Audit = nil
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodDelete, "/api/v1/clusters/v:prod/resources/core/v1/pods/web-1?namespace=ns1", "", "").Code,
		"without an audit recorder the mutation routes are not mounted")
}

const applyPath = "/api/v1/clusters/v:prod/apply"

func applyBody(t *testing.T, dryRun bool, docs ...string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"documents": docs, "dryRun": dryRun})
	require.NoError(t, err)
	return string(b)
}

func TestApply_CreatesUpdatesAndContinuesAfterErrors(t *testing.T) {
	e := newWriteEnv(t)
	docs := []string{
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: brand-new\n  namespace: ns1\ndata:\n  a: \"1\"\n",
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n  namespace: ns1\ndata:\n  a: \"9\"\n---\n",
		"apiVersion: example.com/v1\nkind: Gadget\nmetadata:\n  name: g\n",
		"",
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: [\n",
	}
	rec := e.sendAs(http.MethodPost, applyPath, "application/json", applyBody(t, false, docs...))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Results []struct {
			Index                 int
			Kind, Namespace, Name string
			Action, Error         string
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Results, 4, "the empty document is skipped")
	assert.Equal(t, "created", got.Results[0].Action)
	assert.Equal(t, "brand-new", got.Results[0].Name)
	assert.Equal(t, "updated", got.Results[1].Action)
	assert.Equal(t, 1, got.Results[1].Index)
	assert.Contains(t, got.Results[2].Error, "Gadget")
	assert.NotEmpty(t, got.Results[3].Error)
	assert.Equal(t, []string{"create:requested", "create:success", "update:requested", "update:success"}, e.verbsAndOutcomes(t))
}

func TestApply_DryRunWritesNoAuditAndLimits(t *testing.T) {
	e := newWriteEnv(t)
	doc := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: dry\n  namespace: ns1\n"
	rec := e.sendAs(http.MethodPost, applyPath, "application/json", applyBody(t, true, doc))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"action":"created"`)
	assert.Empty(t, e.auditLines(t))

	many := make([]string, 101)
	for i := range many {
		many[i] = doc
	}
	assert.Equal(t, http.StatusRequestEntityTooLarge, e.sendAs(http.MethodPost, applyPath, "application/json", applyBody(t, false, many...)).Code)
	big := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: big\n  namespace: ns1\n# " + strings.Repeat("x", 600<<10) + "\n"
	assert.Equal(t, http.StatusRequestEntityTooLarge, e.sendAs(http.MethodPost, applyPath, "application/json", applyBody(t, false, big, big)).Code)
}

func TestApply_DefaultsTheNamespaceForNamespacedKinds(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPost, applyPath, "application/json", applyBody(t, false, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: nons\n"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"namespace":"default"`)
}

func withRV(o *unstructured.Unstructured, rv string) *unstructured.Unstructured {
	o.SetResourceVersion(rv)
	return o
}
