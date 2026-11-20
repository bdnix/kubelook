# Helm 3 release records

`web.v1.json` and `web.v2.json` are the two `v1/Secret` objects that the **Helm 3.22.1** Secrets storage driver
writes for release `web` in namespace `demo` (type `helm.sh/release.v1`, labels `owner=helm`, `name`, `status`,
`version`, the gzip + base64 release in `data.release`): revision 1 `superseded`, revision 2 `deployed`. User values
are `replicas: 2`; the manifest has a ConfigMap and a Secret.

They are generated once, outside this module, because `helm.sh/helm/v3` must not appear in `go.mod`. To regenerate:

```sh
mkdir h3gen && cd h3gen
cat > go.mod <<'GOMOD'
module h3gen

go 1.27.4
GOMOD
# save the program below as main.go
go get helm.sh/helm/v3@v3.22.1 k8s.io/client-go/kubernetes/fake
go mod tidy
mkdir out && go run . out
cp out/sh.helm.release.v1.web.v1.json <repo>/internal/helm/helm4/testdata/helm3/web.v1.json
cp out/sh.helm.release.v1.web.v2.json <repo>/internal/helm/helm4/testdata/helm3/web.v2.json
```

```go
// Throw-away program: writes two release records exactly as Helm 3.22.1's Secrets storage driver does.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	htime "helm.sh/helm/v3/pkg/time"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func chartAt(version, appVersion string) *chart.Chart {
	f := func(n, d string) *chart.File { return &chart.File{Name: n, Data: []byte(d)} }
	return &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "web", Version: version, AppVersion: appVersion, Type: "application"},
		Values:   map[string]any{"replicas": 1, "message": "hello"},
		Templates: []*chart.File{
			f("templates/configmap.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-config\n"),
			f("templates/secret.yaml", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: {{ .Release.Name }}-secret\nstringData:\n  password: hunter2\n"),
			f("templates/NOTES.txt", "Thank you for installing {{ .Chart.Name }}.\n"),
		},
	}
}

const manifest = "---\n# Source: web/templates/configmap.yaml\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web-config\n---\n# Source: web/templates/secret.yaml\napiVersion: v1\nkind: Secret\nmetadata:\n  name: web-secret\nstringData:\n  password: hunter2\n"

func main() {
	client := fake.NewSimpleClientset()
	secrets := driver.NewSecrets(client.CoreV1().Secrets("demo"))
	t0 := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	revs := []struct {
		v      int
		status release.Status
		ch     *chart.Chart
		cfg    map[string]any
		desc   string
	}{
		{1, release.StatusSuperseded, chartAt("1.0.0", "2.0"), map[string]any{"replicas": 2}, "Install complete"},
		{2, release.StatusDeployed, chartAt("1.1.0", "2.1"), map[string]any{"replicas": 2}, "Upgrade complete"},
	}
	for i, r := range revs {
		when := t0.Add(time.Duration(i) * time.Hour)
		rel := &release.Release{
			Name: "web", Namespace: "demo", Version: r.v, Chart: r.ch, Config: r.cfg, Manifest: manifest,
			Info: &release.Info{FirstDeployed: htime.Time{Time: t0}, LastDeployed: htime.Time{Time: when}, Status: r.status, Description: r.desc, Notes: "Thank you for installing web.\n"},
		}
		if err := secrets.Create(fmt.Sprintf("sh.helm.release.v1.web.v%d", r.v), rel); err != nil {
			panic(err)
		}
	}
	list, err := client.CoreV1().Secrets("demo").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, s := range list.Items {
		s.ManagedFields = nil
		s.CreationTimestamp = metav1.NewTime(t0)
		s.ResourceVersion = ""
		s.APIVersion, s.Kind = "v1", "Secret"
		b, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(os.Args[1]+"/"+s.Name+".json", append(b, '\n'), 0o644); err != nil {
			panic(err)
		}
	}
}
```
