package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bdnix/kubelook/internal/apperr"
)

const (
	maxMinutes   = 1440
	maxPoints    = 240
	minStep      = 15
	maxResponse  = 4 << 20
	unavailable  = "Prometheus did not answer."
	podCPU       = `sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod="%s",container!=""}[5m]))`
	podMemory    = `sum(container_memory_working_set_bytes{namespace="%s",pod="%s",container!=""})`
	nodeCPU      = `sum(rate(node_cpu_seconds_total{mode!="idle",instance=~"%s.*"}[5m]))`
	nodeMemory   = `node_memory_MemTotal_bytes{instance=~"%[1]s.*"} - node_memory_MemAvailable_bytes{instance=~"%[1]s.*"}`
	statusOK     = "success"
	rangeQueries = "/api/v1/query_range"
)

// escape makes a value safe inside a double-quoted PromQL string: users never write PromQL, they only supply names.
func escape(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

type rangeResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Values [][2]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// History returns the CPU and memory of a pod or node over the last minutes. The queries are fixed templates;
// only the escaped names are filled in. Without a working Prometheus for the cluster it is unavailable.
func (s *Service) History(ctx context.Context, cluster, kind, namespace, name string, minutes int) (History, error) {
	if kind != "pod" && kind != "node" {
		return History{}, fmt.Errorf("kind must be pod or node: %w", apperr.ErrInvalid)
	}
	if name == "" || (kind == "pod" && namespace == "") {
		return History{}, fmt.Errorf("a name (and a namespace for a pod) is needed: %w", apperr.ErrInvalid)
	}
	if minutes < 1 || minutes > maxMinutes {
		return History{}, fmt.Errorf("minutes must be between 1 and %d: %w", maxMinutes, apperr.ErrInvalid)
	}
	cfg, ok := s.d.Prometheus.Clusters[cluster]
	if !ok || cfg.URL == "" {
		return History{Series: []Series{}}, nil
	}
	ns, n := escape(namespace), escape(name)
	queries := [2]string{fmt.Sprintf(nodeCPU, n), fmt.Sprintf(nodeMemory, n)}
	if kind == "pod" {
		queries = [2]string{fmt.Sprintf(podCPU, ns, n), fmt.Sprintf(podMemory, ns, n)}
	}
	end := s.d.Clock.Now().UTC()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	step := max(minStep, (minutes*60+maxPoints-1)/maxPoints)
	out := History{Available: true, Series: make([]Series, 0, 2)}
	for i, metric := range []string{"cpu", "memory"} {
		points, msg := s.queryRange(ctx, cfg.URL, queries[i], start, end, step)
		if msg != "" {
			return History{Series: []Series{}, Message: msg}, nil
		}
		out.Series = append(out.Series, Series{Metric: metric, Points: points})
	}
	return out, nil
}

func (s *Service) queryRange(ctx context.Context, base, query string, start, end time.Time, step int) ([][2]float64, string) {
	q := url.Values{
		"query": {query}, "start": {start.Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}, "step": {strconv.Itoa(step) + "s"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+rangeQueries+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return nil, unavailable
	}
	res, err := s.d.HTTP.Do(req)
	if err != nil {
		return nil, unavailable
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return nil, unavailable
	}
	var parsed rangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Sprintf("Prometheus answered HTTP %d with something that is not JSON.", res.StatusCode)
	}
	if res.StatusCode/100 != 2 || parsed.Status != statusOK {
		if parsed.Error != "" {
			return nil, "Prometheus reported an error: " + parsed.Error
		}
		return nil, fmt.Sprintf("Prometheus answered HTTP %d.", res.StatusCode)
	}
	points := [][2]float64{}
	if len(parsed.Data.Result) > 0 {
		for _, v := range parsed.Data.Result[0].Values {
			var ts float64
			var raw string
			if json.Unmarshal(v[0], &ts) != nil || json.Unmarshal(v[1], &raw) != nil {
				continue
			}
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				points = append(points, [2]float64{ts, f})
			}
		}
	}
	return points, ""
}
