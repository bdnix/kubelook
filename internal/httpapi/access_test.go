package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/httpapi"
)

func (e *kubeEnv) send(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "kubelook_session", Value: e.id})
	req.Header.Set("X-CSRF-Token", e.csrf)
	rec := httptest.NewRecorder()
	httpapi.New(e.deps).ServeHTTP(rec, req)
	return rec
}

func allowOnly(e *kubeEnv, resource string) {
	e.fakes.Kube.PrependReactor("create", "selfsubjectaccessreviews", func(a clienttesting.Action) (bool, runtime.Object, error) {
		r := a.(clienttesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: r.Spec.ResourceAttributes.Resource == resource}}, nil
	})
}

func TestAccess_ResultsInOrder(t *testing.T) {
	e := newKubeEnv(t)
	allowOnly(e, "nodes")
	rec := e.send(http.MethodPost, "/api/v1/clusters/v:prod/access",
		`{"checks":[{"verb":"list","group":"core","resource":"pods"},{"verb":"list","group":"","resource":"nodes"}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Results []struct {
			Allowed bool
			Reason  string
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Results, 2)
	assert.False(t, got.Results[0].Allowed)
	assert.Equal(t, "missing permission: list pods", got.Results[0].Reason)
	assert.True(t, got.Results[1].Allowed)
}

func TestAccess_Validation(t *testing.T) {
	e := newKubeEnv(t)
	many := `{"checks":[` + strings.Repeat(`{"verb":"get","resource":"pods"},`, 100) + `{"verb":"get","resource":"pods"}]}`
	for name, body := range map[string]string{
		"too many": many, "no verb": `{"checks":[{"resource":"pods"}]}`, "no resource": `{"checks":[{"verb":"get"}]}`, "not json": `x`,
	} {
		rec := e.send(http.MethodPost, "/api/v1/clusters/v:prod/access", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
	}
	assert.Equal(t, http.StatusOK, e.send(http.MethodPost, "/api/v1/clusters/v:prod/access", `{"checks":[]}`).Code)
}

func TestAccess_UnknownClusterAndCSRF(t *testing.T) {
	e := newKubeEnv(t)
	rec := e.send(http.MethodPost, "/api/v1/clusters/v:nope/access", `{"checks":[{"verb":"get","resource":"pods"}]}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = e.doWithSession(http.MethodPost, "/api/v1/clusters/v:prod/access", e.id, "")
	assert.Equal(t, "csrf", errorCode(t, rec))
}
