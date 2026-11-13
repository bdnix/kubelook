package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/session"
)

type meResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// me answers who is signed in. Step 2.4 extends the payload; until sign-in exists
// (no sessions wired) the answer is always 401.
func (d Deps) me(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok || d.Users == nil {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	u, err := d.Users.Get(r.Context(), s.UserID)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, meResponse{ID: u.ID, Email: u.Email, Name: u.Name})
}
