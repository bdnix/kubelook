//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebug_ADebugContainerOnAPodWithoutAShellOpensAShellAndIsReused(t *testing.T) {
	c := devopsClient(t, "debug-container")
	pod := podOf(t, "alpha", "web") // the pause image has no shell
	var first, second struct {
		Container string `json:"container"`
		Reused    bool   `json:"reused"`
	}
	body := map[string]string{"namespace": "demo", "pod": pod}
	require.Equal(t, http.StatusOK, c.JSON(http.MethodPost, "/api/v1/clusters/v:alpha/debug/pod", body, &first))
	require.NotEmpty(t, first.Container)
	assert.False(t, first.Reused)

	ch := c.WS().Open("exec.open", map[string]any{"cluster": "v:alpha", "namespace": "demo", "pod": pod, "container": first.Container, "cols": 80, "rows": 24})
	ch.Send("exec.stdin", stdin("echo kubelook-$((6*7))\n"))
	readUntil(t, ch, "kubelook-42", 60*time.Second)

	require.Equal(t, http.StatusOK, c.JSON(http.MethodPost, "/api/v1/clusters/v:alpha/debug/pod", body, &second))
	assert.True(t, second.Reused, "a second request reuses the debug container")
	assert.Equal(t, first.Container, second.Container)
}
