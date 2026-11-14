package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/execs"
	"github.com/bdnix/kubelook/internal/testutil"
)

func withDebugger(t *testing.T, nodeShell bool) *writeEnv {
	t.Helper()
	e := newWriteEnv(t)
	on := nodeShell
	e.deps.Debugger = execs.NewDebugger(execs.DebuggerDeps{
		Clients: e.fakes, Clock: e.fake, Audit: e.deps.Audit, Rand: testutil.Rand("http"),
		Debug: config.Debug{Image: "debug:1", Shell: "/bin/bash"}, NodeShell: config.NodeShell{Enabled: &on, Image: "ns:1"},
	})
	return e
}

func TestDebug_PodEndpointValidatesAndAnswers(t *testing.T) {
	e := withDebugger(t, false)
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodPost, "/api/v1/clusters/v:prod/debug/pod", "application/json", `{"pod":"x"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodPost, "/api/v1/clusters/v:prod/debug/pod", "application/json", `{"bad":1}`).Code)
	rec := e.sendAs(http.MethodPost, "/api/v1/clusters/v:prod/debug/pod", "application/json", `{"namespace":"ns","pod":"missing","target":"app"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDebug_NodeShellDisabledIs404AndEnabledDeletesOnlyItsOwn(t *testing.T) {
	e := withDebugger(t, false)
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, "/api/v1/clusters/v:prod/debug/node", "application/json", `{"node":"n1"}`).Code)
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodDelete, "/api/v1/clusters/v:prod/debug/node/kubelook-node-shell-aaaaa", "", "").Code)

	e = withDebugger(t, true)
	_, err := e.fakes.Kube.CoreV1().Pods("default").Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "kubelook-node-shell-aaaaa", Namespace: "default", Labels: map[string]string{"app.kubernetes.io/managed-by": "kubelook"},
	}}, metav1.CreateOptions{})
	require.NoError(t, err)
	rec := e.sendAs(http.MethodDelete, "/api/v1/clusters/v:prod/debug/node/kubelook-node-shell-aaaaa", "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodPost, "/api/v1/clusters/v:prod/debug/node", "application/json", `{"node":""}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodPost, "/api/v1/clusters/v:prod/debug/node", "application/json", `{`).Code)
}

func TestDebug_NotMountedWithoutADebugger(t *testing.T) {
	e := newWriteEnv(t)
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodPost, "/api/v1/clusters/v:prod/debug/pod", "application/json", `{}`).Code)
}
