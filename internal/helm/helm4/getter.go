package helm4

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// restClientGetter gives the Helm SDK the connection of one cluster, built from the rest config the
// credential broker produced for the user. It never reads a kubeconfig file.
type restClientGetter struct {
	cfg       *rest.Config
	namespace string
}

var _ genericclioptions.RESTClientGetter = (*restClientGetter)(nil)

// NewRESTClientGetter returns the getter Helm needs, for the given connection and default namespace.
func NewRESTClientGetter(cfg *rest.Config, namespace string) genericclioptions.RESTClientGetter {
	return &restClientGetter{cfg: rest.CopyConfig(cfg), namespace: namespace}
}

func (g *restClientGetter) ToRESTConfig() (*rest.Config, error) { return rest.CopyConfig(g.cfg), nil }

func (g *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	c, err := discovery.NewDiscoveryClientForConfig(rest.CopyConfig(g.cfg))
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(c), nil
}

func (g *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	c, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(c), nil
}

func (g *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewDefaultClientConfig(*clientcmdapi.NewConfig(),
		&clientcmd.ConfigOverrides{Context: clientcmdapi.Context{Namespace: g.namespace}})
}
