package modules_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/modules"
)

// res is a resource the cluster serves in its preferred version.
func res(group, version, kind string) kube.Resource {
	return kube.Resource{
		Group: group, Version: version, Kind: kind, Resource: fmt.Sprintf("%ss", lower(kind)),
		Namespaced: true, Verbs: []string{"get", "list"}, Preferred: true, ServedVersions: []string{version},
	}
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func disc(rs ...kube.Resource) *kube.Discovery { return &kube.Discovery{Resources: rs} }

func find(t *testing.T, got []modules.Status, name string) modules.Status {
	t.Helper()
	for _, s := range got {
		if s.Name == name {
			return s
		}
	}
	require.FailNow(t, "module not in the result: "+name)
	return modules.Status{}
}

func kindNames(s modules.Status) []string {
	var out []string
	for _, k := range s.Kinds {
		out = append(out, k.Group+"/"+k.Kind)
	}
	return out
}

func TestDetect_NothingServed_OnlyPermissionsIsActive(t *testing.T) {
	got := modules.Detect(disc(res("", "v1", "Pod")), nil)
	var active []string
	for _, s := range got {
		if s.Active {
			active = append(active, s.Name)
		}
	}
	assert.Equal(t, []string{"permissions"}, active)
	assert.Empty(t, find(t, got, "permissions").Kinds, "claims only served kinds")
}

func TestDetect_ResultIsSortedByName(t *testing.T) {
	got := modules.Detect(disc(), nil)
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"certManager", "externalSecrets", "flux", "gatewayApi", "karpenter", "keda", "permissions", "trivy", "vaultSecrets"}, names)
	assert.IsType(t, []modules.Kind{}, got[0].Kinds)
	assert.NotNil(t, got[0].Kinds, "an empty list is [] in JSON, not null")
}

func TestDetect_EachModuleIsActiveWhenItsCRDIsServed(t *testing.T) {
	tests := []struct {
		module string
		served []kube.Resource
		kinds  []string
	}{
		{"keda", []kube.Resource{res("keda.sh", "v1alpha1", "ScaledObject"), res("keda.sh", "v1alpha1", "ScaledJob"), res("keda.sh", "v1alpha1", "TriggerAuthentication")}, []string{"keda.sh/ScaledObject"}},
		{"externalSecrets", []kube.Resource{res("external-secrets.io", "v1", "ExternalSecret"), res("external-secrets.io", "v1", "SecretStore"), res("external-secrets.io", "v1", "ClusterSecretStore"), res("external-secrets.io", "v1", "PushSecret")}, []string{"external-secrets.io/ClusterSecretStore", "external-secrets.io/ExternalSecret", "external-secrets.io/SecretStore"}},
		{"vaultSecrets", []kube.Resource{res("ricoberger.de", "v1alpha1", "VaultSecret")}, []string{"ricoberger.de/VaultSecret"}},
		{"certManager", []kube.Resource{res("cert-manager.io", "v1", "Certificate"), res("cert-manager.io", "v1", "Issuer"), res("cert-manager.io", "v1", "ClusterIssuer"), res("cert-manager.io", "v1", "CertificateRequest"), res("acme.cert-manager.io", "v1", "Order")}, []string{"cert-manager.io/Certificate", "cert-manager.io/CertificateRequest", "cert-manager.io/ClusterIssuer", "cert-manager.io/Issuer"}},
		{"gatewayApi", []kube.Resource{res("gateway.networking.k8s.io", "v1", "HTTPRoute"), res("gateway.networking.k8s.io", "v1", "Gateway"), res("gateway.networking.x-k8s.io", "v1alpha1", "XListenerSet")}, []string{"gateway.networking.k8s.io/Gateway", "gateway.networking.k8s.io/HTTPRoute", "gateway.networking.x-k8s.io/XListenerSet"}},
		{"trivy", []kube.Resource{res("aquasecurity.github.io", "v1alpha1", "VulnerabilityReport"), res("aquasecurity.github.io", "v1alpha1", "ConfigAuditReport"), res("aquasecurity.github.io", "v1alpha1", "ClusterPolicy")}, []string{"aquasecurity.github.io/ConfigAuditReport", "aquasecurity.github.io/VulnerabilityReport"}},
		{"permissions", []kube.Resource{res("kyverno.io", "v1", "ClusterPolicy"), res("kyverno.io", "v1", "Policy"), res("kyverno.io", "v2", "PolicyException"), res("wgpolicyk8s.io", "v1alpha2", "PolicyReport"), res("wgpolicyk8s.io", "v1alpha2", "ClusterPolicyReport"), res("constraints.gatekeeper.sh", "v1beta1", "K8sRequiredLabels")}, []string{"constraints.gatekeeper.sh/K8sRequiredLabels", "kyverno.io/ClusterPolicy", "kyverno.io/Policy", "wgpolicyk8s.io/ClusterPolicyReport", "wgpolicyk8s.io/PolicyReport"}},
	}
	for _, tc := range tests {
		t.Run(tc.module, func(t *testing.T) {
			got := find(t, modules.Detect(disc(tc.served...), nil), tc.module)
			assert.True(t, got.Active)
			assert.Empty(t, got.Reason)
			assert.Equal(t, tc.kinds, kindNames(got))
		})
	}
}

func TestDetect_ModuleWithoutItsCRD_IsInactiveAndClaimsNothing(t *testing.T) {
	got := find(t, modules.Detect(disc(res("keda.sh", "v1alpha1", "ScaledJob")), nil), "keda")
	assert.False(t, got.Active)
	assert.Equal(t, "not_served", got.Reason)
	assert.Empty(t, got.Kinds, "ScaledJob is not claimed, so it stays under Custom Resources")
}

func TestDetect_Disabled_IsInactiveAndItsKindsAreNotClaimed(t *testing.T) {
	cfg := map[string]config.Module{"keda": {Enabled: "false"}}
	got := find(t, modules.Detect(disc(res("keda.sh", "v1alpha1", "ScaledObject")), cfg), "keda")
	assert.False(t, got.Active)
	assert.Equal(t, "disabled", got.Reason)
	assert.Empty(t, got.Kinds)
}

func TestDetect_AutoOrMissingConfigIsEnabled(t *testing.T) {
	cfg := map[string]config.Module{"keda": {Enabled: "auto"}}
	assert.True(t, find(t, modules.Detect(disc(res("keda.sh", "v1alpha1", "ScaledObject")), cfg), "keda").Active)
}

func TestDetect_DisabledPermissionsClaimsNothing(t *testing.T) {
	cfg := map[string]config.Module{"permissions": {Enabled: "false"}}
	got := find(t, modules.Detect(disc(res("kyverno.io", "v1", "Policy")), cfg), "permissions")
	assert.False(t, got.Active)
	assert.Empty(t, got.Kinds)
}

func TestDetect_Karpenter(t *testing.T) {
	v1 := []kube.Resource{
		res("karpenter.sh", "v1", "NodePool"), res("karpenter.sh", "v1", "NodeClaim"),
		res("karpenter.k8s.aws", "v1", "EC2NodeClass"), res("karpenter.azure.com", "v1beta1", "AKSNodeClass"),
		res("karpenter.k8s.aws", "v1", "SomethingElse"),
	}
	got := find(t, modules.Detect(disc(v1...), nil), "karpenter")
	assert.True(t, got.Active)
	assert.Equal(t, []string{"karpenter.azure.com/AKSNodeClass", "karpenter.k8s.aws/EC2NodeClass", "karpenter.sh/NodeClaim", "karpenter.sh/NodePool"}, kindNames(got))

	t.Run("NodeOverlay only where v1alpha1 is served", func(t *testing.T) {
		with := append(append([]kube.Resource{}, v1...), res("karpenter.sh", "v1alpha1", "NodeOverlay"))
		assert.Contains(t, kindNames(find(t, modules.Detect(disc(with...), nil), "karpenter")), "karpenter.sh/NodeOverlay")
		beta := append(append([]kube.Resource{}, v1...), res("karpenter.sh", "v1beta1", "NodeOverlay"))
		assert.NotContains(t, kindNames(find(t, modules.Detect(disc(beta...), nil), "karpenter")), "karpenter.sh/NodeOverlay")
	})

	t.Run("Karpenter 0.x serves only v1beta1: inactive, kinds unclaimed", func(t *testing.T) {
		old := disc(res("karpenter.sh", "v1beta1", "NodePool"), res("karpenter.k8s.aws", "v1beta1", "EC2NodeClass"))
		s := find(t, modules.Detect(old, nil), "karpenter")
		assert.False(t, s.Active)
		assert.Equal(t, "not_served", s.Reason)
		assert.Empty(t, s.Kinds)
	})

	t.Run("v1 served next to v1beta1", func(t *testing.T) {
		np := res("karpenter.sh", "v1", "NodePool")
		np.ServedVersions = []string{"v1", "v1beta1"}
		assert.True(t, find(t, modules.Detect(disc(np), nil), "karpenter").Active)
	})
}

func TestDetect_FluxIsActiveWhenAnyFluxGroupIsServed(t *testing.T) {
	got := find(t, modules.Detect(disc(
		res("kustomize.toolkit.fluxcd.io", "v1", "Kustomization"),
		res("kustomize.toolkit.fluxcd.io", "v1", "Other"),
		res("source.toolkit.fluxcd.io", "v1", "GitRepository"),
		res("fluxcd.controlplane.io", "v1", "FluxInstance"),
	), nil), "flux")
	assert.True(t, got.Active)
	assert.Equal(t, []string{"fluxcd.controlplane.io/FluxInstance", "kustomize.toolkit.fluxcd.io/Kustomization", "source.toolkit.fluxcd.io/GitRepository"}, kindNames(got))
	assert.False(t, find(t, modules.Detect(disc(res("", "v1", "Pod")), nil), "flux").Active)
}

func TestDetect_FluxIgnoresAConfigEntry(t *testing.T) {
	cfg := map[string]config.Module{"flux": {Enabled: "false"}}
	assert.True(t, find(t, modules.Detect(disc(res("helm.toolkit.fluxcd.io", "v2", "HelmRelease")), cfg), "flux").Active)
}

func TestDetect_ClaimedKindsCarryTheServedVersion(t *testing.T) {
	r := res("keda.sh", "v1alpha1", "ScaledObject")
	r.Resource = "scaledobjects"
	r.Namespaced = true
	got := find(t, modules.Detect(disc(r), nil), "keda")
	require.Len(t, got.Kinds, 1)
	assert.Equal(t, modules.Kind{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects", Kind: "ScaledObject", Namespaced: true}, got.Kinds[0])
}

func TestDetect_OnlyPreferredVersionsAreClaimed(t *testing.T) {
	old := res("keda.sh", "v1alpha1", "ScaledObject")
	old.Preferred = false
	got := find(t, modules.Detect(disc(old), nil), "keda")
	assert.False(t, got.Active)
}
