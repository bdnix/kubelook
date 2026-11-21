package helm

import (
	"helm.sh/helm/v4/pkg/chart/v2/loader"
)

// chartVersionOf reads the version of a chart archive; "" when it cannot be read.
func chartVersionOf(path string) string {
	ch, err := loader.Load(path)
	if err != nil || ch.Metadata == nil {
		return ""
	}
	return ch.Metadata.Version
}
