// Package crypto seals secrets with AES-256-GCM into self-describing envelopes.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Errors returned by Open.
var (
	ErrUnknownKey = errors.New("crypto: sealed with a different key")
	ErrDecrypt    = errors.New("crypto: decryption failed")
	ErrMalformed  = errors.New("crypto: malformed envelope")
)

// Purposes bind a sealed value to the kind of thing it protects.
//
//nolint:gosec // G101 matches the names; these are labels, not credentials
const (
	PurposeSavedCredential   = "saved-credential"   // binding: user ID
	PurposeSessionCredential = "session-credential" // binding: session hash
	PurposeJob               = "job"                // binding: job ID
	PurposeOAuthState        = "oauth-state"        // binding: provider name
)

const (
	version   = "v1"
	nonceSize = 12
)

// Sealer seals and opens envelopes with one key.
type Sealer struct {
	aead  cipher.AEAD
	keyID string
	rand  io.Reader
}

// NewSealer returns a Sealer for a 32-byte key. rand supplies nonces.
func NewSealer(key []byte, rand io.Reader) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}
	sum := sha256.Sum256(key)
	return &Sealer{aead: aead, keyID: hex.EncodeToString(sum[:])[:16], rand: rand}, nil
}

// KeyID identifies the key without revealing it.
func (s *Sealer) KeyID() string { return s.keyID }

// Seal encrypts plaintext, authenticating aad, and returns
// v1.<keyID>.<nonce b64url>.<ciphertext b64url>.
func (s *Sealer) Seal(plaintext, aad []byte) (string, error) {
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(s.rand, nonce); err != nil {
		return "", fmt.Errorf("reading nonce: %w", err)
	}
	ct := s.aead.Seal(nil, nonce, plaintext, aad)
	enc := base64.RawURLEncoding
	return strings.Join([]string{version, s.keyID, enc.EncodeToString(nonce), enc.EncodeToString(ct)}, "."), nil
}

// Open verifies and decrypts an envelope produced by Seal with the same aad.
func (s *Sealer) Open(envelope string, aad []byte) ([]byte, error) {
	keyID, nonce, ct, err := parseEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	if keyID != s.keyID {
		return nil, ErrUnknownKey
	}
	pt, err := s.aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func parseEnvelope(envelope string) (keyID string, nonce, ct []byte, err error) {
	parts := strings.Split(envelope, ".")
	if len(parts) != 4 || parts[0] != version {
		return "", nil, nil, ErrMalformed
	}
	enc := base64.RawURLEncoding
	if nonce, err = enc.DecodeString(parts[2]); err != nil || len(nonce) != nonceSize {
		return "", nil, nil, ErrMalformed
	}
	if ct, err = enc.DecodeString(parts[3]); err != nil || len(ct) < 16 {
		return "", nil, nil, ErrMalformed
	}
	return parts[1], nonce, ct, nil
}

// AAD builds the associated data that binds a sealed value to its purpose and owner.
// A purpose containing "|" is a programming error and panics.
func AAD(purpose, binding string) []byte {
	if strings.Contains(purpose, "|") {
		panic("crypto: purpose must not contain '|'")
	}
	return []byte(purpose + "|" + binding)
}
