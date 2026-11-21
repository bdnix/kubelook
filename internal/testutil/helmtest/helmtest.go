// Package helmtest builds a Helm chart and a Helm 4 configuration over memory storage, for tests.
package helmtest

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	ri "helm.sh/helm/v4/pkg/release"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// Chart is a small chart: a ConfigMap with `replicas`, a Secret, a test hook and notes.
func Chart() *chart.Chart {
	return ChartVersion("web", "1.0.0", "2.0")
}

// ChartVersion is Chart with another name, version and app version.
func ChartVersion(name, version, appVersion string) *chart.Chart {
	file := func(n, data string) *common.File { return &common.File{Name: n, Data: []byte(data)} }
	return &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: chart.APIVersionV2, Name: name, Version: version, AppVersion: appVersion, Type: "application"},
		Values:   map[string]any{"replicas": 1, "message": "hello"},
		Templates: []*common.File{
			file("templates/configmap.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-config\ndata:\n  replicas: {{ .Values.replicas | quote }}\n  message: {{ .Values.message | quote }}\n"),
			file("templates/secret.yaml", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: {{ .Release.Name }}-secret\nstringData:\n  password: hunter2\n"),
			file("templates/test.yaml", "apiVersion: v1\nkind: Pod\nmetadata:\n  name: {{ .Release.Name }}-test\n  annotations:\n    helm.sh/hook: test\nspec:\n  restartPolicy: Never\n  containers:\n    - name: t\n      image: busybox\n"),
			file("templates/NOTES.txt", "Thank you for installing {{ .Chart.Name }}.\n"),
		},
	}
}

// Env is a Helm 4 configuration over memory storage and a client that only prints.
type Env struct {
	Cfg *action.Configuration
	mem *driver.Memory
}

// New returns an Env.
func New(t *testing.T) *Env {
	t.Helper()
	mem := driver.NewMemory()
	cfg := action.NewConfiguration()
	cfg.Releases = storage.Init(mem)
	cfg.KubeClient = &kubefake.PrintingKubeClient{Out: io.Discard}
	cfg.Capabilities = common.DefaultCapabilities
	return &Env{Cfg: cfg, mem: mem}
}

// Open is what an adapter calls to get a configuration for a namespace ("" is all namespaces).
func (e *Env) Open(namespace string) (*action.Configuration, error) {
	e.mem.SetNamespace(namespace)
	return e.Cfg, nil
}

// Install installs the chart through the SDK's own action, so the stored record is what Helm writes.
func (e *Env) Install(t *testing.T, ch *chart.Chart, namespace, name string, values map[string]any) ri.Releaser {
	t.Helper()
	inst := action.NewInstall(e.Cfg)
	inst.ReleaseName = name
	inst.Namespace = namespace
	inst.ServerSideApply = false
	rel, err := inst.Run(ch, values)
	require.NoError(t, err)
	return rel
}

// Upgrade upgrades the release through the SDK's own action.
func (e *Env) Upgrade(t *testing.T, ch *chart.Chart, namespace, name string, values map[string]any) {
	t.Helper()
	e.mem.SetNamespace(namespace)
	up := action.NewUpgrade(e.Cfg)
	up.Namespace = namespace
	up.ServerSideApply = "false"
	_, err := up.Run(name, ch, values)
	require.NoError(t, err)
}

// Save writes the chart as `<name>-<version>.tgz` in dir and returns its path.
func Save(t *testing.T, ch *chart.Chart, dir string) string {
	t.Helper()
	path, err := chartutil.Save(ch, dir)
	require.NoError(t, err)
	return path
}

// Releases is the storage of the Env, for tests that write records the SDK would not (a Helm 3 record).
func (e *Env) Releases() *storage.Storage { return e.Cfg.Releases }
