package helm4

import (
	"context"
	"fmt"
	"net/http"

	"helm.sh/helm/v4/pkg/action"

	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// storageDriver is where Helm keeps releases: Secrets, as Helm 3 and Helm 4 do by default.
// Clusters that keep them in ConfigMaps are not supported.
const storageDriver = "secret"

// Factory makes the Helm driver of a cluster for one user.
type Factory struct {
	Clients kube.Clients
	// RegistryHTTP is the HTTP client of OCI pulls, whose references are the user's own (egress guard); nil → the SDK's.
	RegistryHTTP *http.Client
}

// Driver returns the Helm 4 driver for the cluster. It uses the connection the credential broker made for the
// user, so every call is made as that user. `workDir` is the job's own temporary directory.
func (f *Factory) Driver(ctx context.Context, sess session.Session, clusterID, namespace, workDir string) (helm.Driver, error) {
	restCfg, err := f.Clients.REST(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	open := func(ns string) (*action.Configuration, error) {
		cfg := action.NewConfiguration()
		if err := cfg.Init(NewRESTClientGetter(restCfg, ns), ns, storageDriver); err != nil {
			return nil, fmt.Errorf("opening Helm storage: %w", err)
		}
		return cfg, nil
	}
	_ = namespace // the namespace of a call is passed to List, Get and History; the driver itself is not bound to one
	return New(open).WithWorkDir(workDir).WithRegistryHTTP(f.RegistryHTTP), nil
}
