package helm4_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/helm/helm4"
)

// helm3Adapter reads two records exactly as the Helm 3.22.1 Secrets driver wrote them (testdata/helm3/README.md),
// through the Helm 4 Secrets driver.
func helm3Adapter(t *testing.T) *helm4.Adapter {
	t.Helper()
	client := fake.NewSimpleClientset()
	for _, name := range []string{"web.v1.json", "web.v2.json"} {
		raw, err := os.ReadFile(filepath.Join("testdata", "helm3", name))
		require.NoError(t, err)
		var s corev1.Secret
		require.NoError(t, json.Unmarshal(raw, &s))
		require.Equal(t, corev1.SecretType("helm.sh/release.v1"), s.Type, name)
		_, err = client.CoreV1().Secrets(s.Namespace).Create(context.Background(), &s, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	return helm4.New(func(ns string) (*action.Configuration, error) {
		cfg := action.NewConfiguration()
		cfg.KubeClient = &kubefake.PrintingKubeClient{Out: io.Discard}
		cfg.Releases = storage.Init(driver.NewSecrets(client.CoreV1().Secrets(ns)))
		return cfg, nil
	})
}

func TestHelm3Records_List(t *testing.T) {
	got, err := helm3Adapter(t).List(context.Background(), "demo")
	require.NoError(t, err)
	require.Len(t, got, 1)
	r := got[0]
	assert.Equal(t, "web", r.Name)
	assert.Equal(t, 2, r.Revision)
	assert.Equal(t, "deployed", r.Status)
	assert.Equal(t, "web", r.Chart)
	assert.Equal(t, "1.1.0", r.ChartVersion)
	assert.Equal(t, "2.1", r.AppVersion)
}

func TestHelm3Records_Get(t *testing.T) {
	d, err := helm3Adapter(t).Get(context.Background(), "demo", "web", 0)
	require.NoError(t, err)
	assert.Equal(t, "replicas: 2\n", d.UserValues)
	assert.Contains(t, d.ComputedValues, "message: hello")
	assert.Contains(t, d.Manifest, "kind: ConfigMap")
	assert.Contains(t, d.Manifest, "kind: Secret")
	assert.NotContains(t, d.Manifest, "hunter2", "Secrets are masked")
	assert.Equal(t, "Thank you for installing web.\n", d.Notes)
	assert.Empty(t, d.Hooks)

	old, err := helm3Adapter(t).Get(context.Background(), "demo", "web", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, old.Revision)
	assert.Equal(t, "1.0.0", old.ChartVersion)
	assert.Equal(t, "superseded", old.Status)
}

func TestHelm3Records_History(t *testing.T) {
	got, err := helm3Adapter(t).History(context.Background(), "demo", "web")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []int{2, 1}, []int{got[0].Revision, got[1].Revision})
	assert.Equal(t, "Upgrade complete", got[0].Description)
}

func TestHelm3Records_AreClientSideApply(t *testing.T) {
	got, err := helm3Adapter(t).List(context.Background(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "csa", got[0].ApplyMethod, "a record without an apply method is client-side apply")
}

var _ helm.Driver = (*helm4.Adapter)(nil)
