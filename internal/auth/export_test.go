package auth

import "net/http"

// ParseCallback exposes parseCallback to the black-box tests.
func ParseCallback(h *Handlers, w http.ResponseWriter, r *http.Request, provider string) (string, string, error) {
	return h.parseCallback(w, r, provider)
}
