// Package metrics reads CPU and memory of pods and nodes: the current usage from metrics-server through the
// user's own credentials, and the history from an optional Prometheus per cluster.
package metrics

import (
	"net/http"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/kube"
)

// Container is the usage of one container of a pod.
type Container struct {
	Name          string `json:"name"`
	CPUMillicores int64  `json:"cpuMillicores"`
	MemoryBytes   int64  `json:"memoryBytes"`
}

// Item is the usage of one pod or node.
type Item struct {
	Namespace     string      `json:"namespace,omitempty"`
	Name          string      `json:"name"`
	CPUMillicores int64       `json:"cpuMillicores"`
	MemoryBytes   int64       `json:"memoryBytes"`
	Containers    []Container `json:"containers,omitempty"`
}

// Usage is the answer of the current-usage endpoints. Available is false when the cluster serves no metrics.
type Usage struct {
	Available bool   `json:"available"`
	Items     []Item `json:"items"`
}

// Series is one measure over time: points are [unix seconds, value].
type Series struct {
	Metric string       `json:"metric"` // cpu | memory
	Points [][2]float64 `json:"points"`
}

// History is the answer of the history endpoint. Available is false without a working Prometheus; Message says why.
type History struct {
	Available bool     `json:"available"`
	Series    []Series `json:"series"`
	Message   string   `json:"message,omitempty"`
}

// Deps are the Service's dependencies.
type Deps struct {
	Clients    kube.Clients
	Clock      clock.Clock
	HTTP       *http.Client // nil → a client with a 10 second timeout
	Prometheus config.Prometheus
}

// Service reads metrics.
type Service struct{ d Deps }

// New returns a Service.
func New(d Deps) *Service {
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{d: d}
}
