package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

const actionsPath = "/api/v1/clusters/v:prod/actions"

func TestActions_ScaleIsAuditedAndAnswersOK(t *testing.T) {
	e := newWriteEnv(t)
	e.fakes.Dyn.PrependReactor("patch", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, fakekube.Object("apps/v1", "Deployment", "ns", "web", nil), nil
	})
	rec := e.sendAs(http.MethodPost, actionsPath, "application/json",
		`{"action":"scale","target":{"group":"apps","version":"v1","resource":"deployments","namespace":"ns","name":"web"},"params":{"replicas":2}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
	assert.Equal(t, []string{"scale:requested", "scale:success"}, e.verbsAndOutcomes(t))
}

func TestActions_Errors(t *testing.T) {
	e := newWriteEnv(t)
	rec := e.sendAs(http.MethodPost, actionsPath, "application/json", `{"action":"explode","target":{"version":"v1","resource":"pods","name":"x"}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodPost, actionsPath, "application/json", `{"bogus":1}`).Code)
	e.deps.Audit = nil
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, actionsPath, "application/json", `{}`).Code)
}
