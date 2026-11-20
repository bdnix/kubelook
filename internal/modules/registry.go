// Package modules decides which ecosystem modules a cluster supports and which kinds each module claims.
// Modules read and change objects only through the generic resource API and the actions endpoint; none has a
// Kubernetes client of its own.
package modules

import (
	"slices"
	"strings"

	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/kube"
)

// Reasons a module is inactive.
const (
	ReasonDisabled  = "disabled"
	ReasonNotServed = "not_served"
)

// Kind is a kind a module claims, in the version the cluster serves.
type Kind struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
}

// Status is what the cluster supports of one module.
type Status struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Reason string `json:"reason,omitempty"`
	// Kinds are claimed only while the module is active; the other kinds stay under Custom Resources.
	Kinds []Kind `json:"kinds"`
}

// definition is one row of the module table: the single source of truth.
type definition struct {
	name string
	// configurable is false for modules without a `modules.<name>.enabled` key.
	configurable bool
	// active decides from what the cluster serves; nil means always active.
	active func(s served) bool
	// claims says whether the module claims a served kind.
	claims func(s served, r kube.Resource) bool
}

// served is the preferred resources of a cluster.
type served struct{ resources []kube.Resource }

func (s served) has(group, kind string) bool {
	return slices.ContainsFunc(s.resources, func(r kube.Resource) bool { return r.Group == group && r.Kind == kind })
}

func (s served) hasGroup(groups ...string) bool {
	return slices.ContainsFunc(s.resources, func(r kube.Resource) bool { return slices.Contains(groups, r.Group) })
}

// hasVersion reports whether the cluster serves the kind in the version, whether or not it is the preferred one.
func (s served) hasVersion(group, kind, version string) bool {
	return slices.ContainsFunc(s.resources, func(r kube.Resource) bool {
		return r.Group == group && r.Kind == kind && slices.Contains(r.ServedVersions, version)
	})
}

func kinds(group string, names ...string) func(served, kube.Resource) bool {
	return func(_ served, r kube.Resource) bool { return r.Group == group && slices.Contains(names, r.Kind) }
}

// fluxKinds is the spec's Flux table.
var fluxKinds = map[string][]string{
	"fluxcd.controlplane.io":         {"FluxInstance", "FluxReport", "ResourceSet", "ResourceSetInputProvider"},
	"source.toolkit.fluxcd.io":       {"GitRepository", "OCIRepository", "HelmRepository", "HelmChart", "Bucket", "ExternalArtifact"},
	"source.extensions.fluxcd.io":    {"ArtifactGenerator"},
	"kustomize.toolkit.fluxcd.io":    {"Kustomization"},
	"helm.toolkit.fluxcd.io":         {"HelmRelease"},
	"image.toolkit.fluxcd.io":        {"ImageRepository", "ImagePolicy", "ImageUpdateAutomation"},
	"notification.toolkit.fluxcd.io": {"Provider", "Alert", "Receiver"},
}

func fluxGroups() []string {
	groups := make([]string, 0, len(fluxKinds))
	for g := range fluxKinds {
		groups = append(groups, g)
	}
	slices.Sort(groups)
	return groups
}

var definitions = []definition{
	{
		name:   "flux",
		active: func(s served) bool { return s.hasGroup(fluxGroups()...) },
		claims: func(_ served, r kube.Resource) bool { return slices.Contains(fluxKinds[r.Group], r.Kind) },
	},
	{
		name:         "karpenter",
		configurable: true,
		active:       func(s served) bool { return s.hasVersion("karpenter.sh", "NodePool", "v1") },
		claims: func(s served, r kube.Resource) bool {
			switch {
			case r.Group == "karpenter.sh":
				return r.Kind == "NodePool" || r.Kind == "NodeClaim" ||
					(r.Kind == "NodeOverlay" && s.hasVersion("karpenter.sh", "NodeOverlay", "v1alpha1"))
			case strings.HasPrefix(r.Group, "karpenter."):
				return slices.Contains([]string{"EC2NodeClass", "AKSNodeClass", "GCENodeClass", "GCECustomMachineType"}, r.Kind)
			}
			return false
		},
	},
	{name: "keda", configurable: true, active: func(s served) bool { return s.has("keda.sh", "ScaledObject") }, claims: kinds("keda.sh", "ScaledObject")},
	{
		name: "externalSecrets", configurable: true,
		active: func(s served) bool { return s.has("external-secrets.io", "ExternalSecret") },
		claims: kinds("external-secrets.io", "ExternalSecret", "SecretStore", "ClusterSecretStore"),
	},
	{name: "vaultSecrets", configurable: true, active: func(s served) bool { return s.has("ricoberger.de", "VaultSecret") }, claims: kinds("ricoberger.de", "VaultSecret")},
	{
		name: "certManager", configurable: true,
		active: func(s served) bool { return s.has("cert-manager.io", "Certificate") },
		claims: kinds("cert-manager.io", "Certificate", "Issuer", "ClusterIssuer", "CertificateRequest"),
	},
	{
		name: "gatewayApi", configurable: true,
		active: func(s served) bool { return s.hasGroup("gateway.networking.k8s.io") },
		claims: func(_ served, r kube.Resource) bool {
			return r.Group == "gateway.networking.k8s.io" || r.Group == "gateway.networking.x-k8s.io"
		},
	},
	{
		name: "trivy", configurable: true,
		active: func(s served) bool { return s.hasGroup("aquasecurity.github.io") },
		claims: func(_ served, r kube.Resource) bool {
			return r.Group == "aquasecurity.github.io" && strings.HasSuffix(r.Kind, "Report")
		},
	},
	{
		name: "permissions", configurable: true,
		claims: func(s served, r kube.Resource) bool {
			return kinds("kyverno.io", "ClusterPolicy", "Policy")(s, r) ||
				kinds("wgpolicyk8s.io", "PolicyReport", "ClusterPolicyReport")(s, r) ||
				r.Group == "constraints.gatekeeper.sh"
		},
	},
}

// Detect reports each module for a cluster, sorted by name. A module is active when the cluster serves what it
// needs and the configuration does not turn it off; only an active module claims kinds.
func Detect(d *kube.Discovery, cfg map[string]config.Module) []Status {
	s := served{}
	for _, r := range d.Resources {
		if r.Preferred {
			s.resources = append(s.resources, r)
		}
	}
	out := make([]Status, 0, len(definitions))
	for _, def := range definitions {
		st := Status{Name: def.name, Kinds: []Kind{}}
		switch {
		case def.configurable && cfg[def.name].Enabled == "false":
			st.Reason = ReasonDisabled
		case def.active != nil && !def.active(s):
			st.Reason = ReasonNotServed
		default:
			st.Active = true
			for _, r := range s.resources {
				if def.claims(s, r) {
					st.Kinds = append(st.Kinds, Kind{Group: r.Group, Version: r.Version, Resource: r.Resource, Kind: r.Kind, Namespaced: r.Namespaced})
				}
			}
			slices.SortFunc(st.Kinds, func(a, b Kind) int {
				return strings.Compare(a.Group+"/"+a.Kind, b.Group+"/"+b.Kind)
			})
		}
		out = append(out, st)
	}
	slices.SortFunc(out, func(a, b Status) int { return strings.Compare(a.Name, b.Name) })
	return out
}
