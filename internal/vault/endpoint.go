package vault

import (
	"net/http"
	"strings"
)

// Endpoint names the Vault operation a request performs, for metrics and traces. The names are a
// fixed vocabulary: mount paths and role names are never part of them.
func Endpoint(r *http.Request) string {
	path, ok := strings.CutPrefix(r.URL.Path, "/v1/")
	if !ok {
		return "other"
	}
	switch path {
	case "auth/token/lookup-self":
		return "token_lookup"
	case "auth/token/renew-self":
		return "token_renew"
	case "sys/internal/ui/mounts":
		return "mounts"
	case "sys/capabilities-self":
		return "capabilities"
	}
	switch {
	case strings.HasSuffix(path, "/config"):
		return "cluster_config"
	case strings.Contains(path, "/creds/"):
		return "creds"
	}
	return "other"
}
