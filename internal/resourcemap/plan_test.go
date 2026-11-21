package resourcemap_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/resourcemap"
)

func kinds(n int) []resourcemap.Kind {
	out := make([]resourcemap.Kind, n)
	for i := range out {
		out[i] = resourcemap.Kind{Group: "g", Kind: fmt.Sprintf("K%d", i), Resource: fmt.Sprintf("k%ds", i)}
	}
	return out
}

func namespaces(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("ns%d", i)
	}
	return out
}

func allowAll(resourcemap.Kind) bool { return true }

func count(ws []resourcemap.Watch, namespace string) int {
	n := 0
	for _, w := range ws {
		if w.Namespace == namespace {
			n++
		}
	}
	return n
}

func TestPlanWatches_OneNamespace_OneWatchPerKind(t *testing.T) {
	ws, left := resourcemap.PlanWatches(kinds(11), namespaces(1), allowAll)
	assert.Len(t, ws, 11)
	assert.Empty(t, left)
	assert.Equal(t, 11, count(ws, "ns0"))
}

func TestPlanWatches_TwoNamespaces_PerNamespace(t *testing.T) {
	ws, left := resourcemap.PlanWatches(kinds(11), namespaces(2), allowAll)
	assert.Len(t, ws, 22)
	assert.Empty(t, left)
}

func TestPlanWatches_ThreeNamespaces_OneAllNamespacesWatchPerKind(t *testing.T) {
	ws, left := resourcemap.PlanWatches(kinds(11), namespaces(3), allowAll)
	assert.Len(t, ws, 11)
	assert.Equal(t, 11, count(ws, ""))
	assert.Empty(t, left)
}

func TestPlanWatches_AKindThatCannotBeListedClusterWide_FallsBackToNamespaces(t *testing.T) {
	ks := kinds(11)
	cannot := func(k resourcemap.Kind) bool { return k.Kind != "K3" }
	ws, left := resourcemap.PlanWatches(ks, namespaces(3), cannot)
	assert.LessOrEqual(t, len(ws), 24)
	assert.Len(t, ws, 10+3)
	assert.Empty(t, left)
	for _, w := range ws {
		if w.Kind.Kind == "K3" {
			assert.NotEmpty(t, w.Namespace)
		} else {
			assert.Empty(t, w.Namespace)
		}
	}
}

func TestPlanWatches_NeverMoreThan24_AndTheNamespacesDroppedAreReported(t *testing.T) {
	ks := kinds(11)
	cannot := func(k resourcemap.Kind) bool { return k.Kind != "K3" }
	ws, left := resourcemap.PlanWatches(ks, namespaces(30), cannot)
	assert.LessOrEqual(t, len(ws), 24)
	assert.Equal(t, 24-10, count(ws, "ns0")+count(ws, "ns1")+count(ws, "ns2")+count(ws, "ns3")+count(ws, "ns4")+count(ws, "ns5")+count(ws, "ns6")+count(ws, "ns7")+count(ws, "ns8")+count(ws, "ns9")+count(ws, "ns10")+count(ws, "ns11")+count(ws, "ns12")+count(ws, "ns13"))
	assert.Len(t, left, 30-14)
	assert.Equal(t, "ns14", left[0], "the first namespaces are kept, the rest are left out")

	two := func(k resourcemap.Kind) bool { return k.Kind != "K3" && k.Kind != "K4" }
	ws, left = resourcemap.PlanWatches(ks, namespaces(30), two)
	assert.LessOrEqual(t, len(ws), 24)
	assert.Len(t, left, 30-7, "14 watches left for two kinds: 7 namespaces each")
}

func TestPlanWatches_NoNamespaces_NoWatches(t *testing.T) {
	ws, left := resourcemap.PlanWatches(kinds(11), nil, allowAll)
	assert.Empty(t, ws)
	assert.Empty(t, left)
}

func TestPlanWatches_ManyKindsAndNamespaces_StaysWithinTheBudget(t *testing.T) {
	for _, nk := range []int{1, 5, 11, 12, 20, 24} {
		for _, nn := range []int{1, 2, 3, 10, 50} {
			for _, listable := range []bool{true, false} {
				ws, _ := resourcemap.PlanWatches(kinds(nk), namespaces(nn), func(resourcemap.Kind) bool { return listable })
				assert.LessOrEqual(t, len(ws), 24, "kinds=%d namespaces=%d listable=%v", nk, nn, listable)
			}
		}
	}
}
