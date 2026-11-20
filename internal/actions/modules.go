package actions

import (
	"encoding/json"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/audit"
)

const (
	kedaPausedAnnotation = "autoscaling.keda.sh/paused"
	esoForceSyncKey      = "force-sync"
)

var (
	scaledObjects   = []schema.GroupKind{gk("keda.sh", "ScaledObject")}
	externalSecrets = []schema.GroupKind{gk("external-secrets.io", "ExternalSecret")}
)

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
	h["esoForceSync"] = handler{kinds: externalSecrets, verb: audit.VerbModuleAction, run: func(c *call) error {
		return c.patch(types.MergePatchType, annotationPatch(esoForceSyncKey, strconv.FormatInt(c.svc.d.Clock.Now().Unix(), 10)))
	}}
}
