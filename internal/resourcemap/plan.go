package resourcemap

// MaxWatches is the most upstream watches one map may hold: the user has 50 in all, and the map shares them with
// the open lists.
const MaxWatches = 24

// Kind is a kind the map reads.
type Kind struct{ Group, Kind, Resource string }

// Watch is one watch the map needs: a kind in one namespace, or in all of them ("").
type Watch struct {
	Kind      Kind
	Namespace string
}

// PlanWatches decides which watches a map of the namespaces needs, never more than MaxWatches.
//
// While kinds × namespaces fits, each kind is watched per namespace. Otherwise each kind the user may list across all
// namespaces gets one all-namespaces watch (the graph is then filtered to the selected namespaces), and a kind the user
// may not list cluster-wide shares what is left of the budget: it is watched in the first namespaces that fit, and
// the namespaces dropped are returned.
func PlanWatches(kinds []Kind, namespaces []string, canListAll func(Kind) bool) (watches []Watch, leftOut []string) {
	if len(kinds) == 0 || len(namespaces) == 0 {
		return nil, nil
	}
	if len(kinds)*len(namespaces) <= MaxWatches {
		for _, ns := range namespaces {
			for _, k := range kinds {
				watches = append(watches, Watch{Kind: k, Namespace: ns})
			}
		}
		return watches, nil
	}
	var scoped []Kind
	for _, k := range kinds {
		if canListAll(k) {
			if len(watches) < MaxWatches {
				watches = append(watches, Watch{Kind: k})
			}
		} else {
			scoped = append(scoped, k)
		}
	}
	if len(scoped) == 0 {
		return watches, nil
	}
	perKind := (MaxWatches - len(watches)) / len(scoped)
	kept := min(perKind, len(namespaces))
	for _, k := range scoped {
		for _, ns := range namespaces[:kept] {
			watches = append(watches, Watch{Kind: k, Namespace: ns})
		}
	}
	return watches, append([]string(nil), namespaces[kept:]...)
}
