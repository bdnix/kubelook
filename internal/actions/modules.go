package actions

import (
	"encoding/json"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/kube"
)

const (
	kedaPausedAnnotation = "autoscaling.keda.sh/paused"
	esoForceSyncKey      = "force-sync"
)

var (
	scaledObjects   = []schema.GroupKind{gk("keda.sh", "ScaledObject")}
	externalSecrets = []schema.GroupKind{gk("external-secrets.io", "ExternalSecret")}
)

// trivyReports are the report kinds of the Trivy Operator (the group's kinds that end in Report).
var trivyReports = func() []schema.GroupKind {
	names := []string{
		"VulnerabilityReport", "ConfigAuditReport", "ExposedSecretReport", "RbacAssessmentReport", "InfraAssessmentReport",
		"SbomReport", "ClusterVulnerabilityReport", "ClusterConfigAuditReport", "ClusterRbacAssessmentReport",
		"ClusterInfraAssessmentReport", "ClusterSbomReport", "ClusterComplianceReport",
	}
	out := make([]schema.GroupKind, len(names))
	for i, n := range names {
		out[i] = gk("aquasecurity.github.io", n)
	}
	return out
}()

func annotationPatch(key string, value any) []byte {
	b, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]any{key: value}}})
	return b
}

// registerModules adds the actions of the ecosystem modules. Every one is an ordinary mutating call under the
// user's identity and is audited as a module action.
func registerModules(h map[string]handler) {
	h["kedaPause"] = handler{kinds: scaledObjects, verb: audit.VerbModuleAction, run: func(c *call) error {
		return c.patch(types.MergePatchType, annotationPatch(kedaPausedAnnotation, "true"))
	}}
	// a JSON merge patch removes a key only when its value is an explicit null
	h["kedaResume"] = handler{kinds: scaledObjects, verb: audit.VerbModuleAction, run: func(c *call) error {
		return c.patch(types.MergePatchType, annotationPatch(kedaPausedAnnotation, nil))
	}}
	// the operator creates the report again for the kinds whose controller was read (see web/src/kinds/trivy.ts)
	h["trivyRescan"] = handler{kinds: trivyReports, verb: audit.VerbModuleAction, run: func(c *call) error {
		err := c.dyn.Resource(c.gvr).Namespace(c.t.Namespace).Delete(c.ctx, c.t.Name, metav1.DeleteOptions{})
		return kube.MapError(err)
	}}
	h["esoForceSync"] = handler{kinds: externalSecrets, verb: audit.VerbModuleAction, run: func(c *call) error {
		return c.patch(types.MergePatchType, annotationPatch(esoForceSyncKey, strconv.FormatInt(c.svc.d.Clock.Now().Unix(), 10)))
	}}
}
