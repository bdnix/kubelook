package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	cookieName = "kubelook_oauth"
	cookiePath = "/auth"
	stateTTL   = 10 * time.Minute
	randBytes  = 32
)

// oauthState is what the sealed cookie carries between login and callback.
type oauthState struct {
	State     string    `json:"state"`
	Verifier  string    `json:"verifier"`
	CreatedAt time.Time `json:"createdAt"`
}

// randomToken returns 32 random bytes, base64url without padding.
func randomToken(r io.Reader) (string, error) {
	b := make([]byte, randBytes)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func marshalState(s oauthState) ([]byte, error) { return json.Marshal(s) }

func unmarshalState(b []byte) (oauthState, error) {
	var s oauthState
	err := json.Unmarshal(b, &s)
	return s, err
}

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
