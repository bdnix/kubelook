package crypto_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/testutil"
)

func sealer(t *testing.T, key byte) *crypto.Sealer {
	t.Helper()
	s, err := crypto.NewSealer(bytes.Repeat([]byte{key}, 32), testutil.Rand("seed"))
	require.NoError(t, err)
	return s
}

func TestNewSealer_RejectsKeysThatAreNot32Bytes(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33, 64} {
		_, err := crypto.NewSealer(make([]byte, n), testutil.Rand("x"))
		assert.Error(t, err, n)
	}
}

func TestKeyID_IsFirst16HexOfSHA256(t *testing.T) {
	s := sealer(t, 0)
	assert.Equal(t, "66687aadf862bd77", s.KeyID())
}

func TestSealOpen_RoundTrips(t *testing.T) {
	s := sealer(t, 1)
	aad := crypto.AAD("saved-credential", "user-1")
	for name, pt := range map[string][]byte{"empty": {}, "1MiB": bytes.Repeat([]byte("x"), 1<<20)} {
		env, err := s.Seal(pt, aad)
		require.NoError(t, err, name)
		got, err := s.Open(env, aad)
		require.NoError(t, err, name)
		assert.Equal(t, len(pt), len(got), name)
		assert.True(t, bytes.Equal(pt, got), name)
	}
}

func TestSeal_EnvelopeFormat(t *testing.T) {
	s := sealer(t, 1)
	env, err := s.Seal([]byte("hello"), nil)
	require.NoError(t, err)
	parts := strings.Split(env, ".")
	require.Len(t, parts, 4)
	assert.Equal(t, "v1", parts[0])
	assert.Equal(t, s.KeyID(), parts[1])
	nonce, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	assert.Len(t, nonce, 12)
	ct, err := base64.RawURLEncoding.DecodeString(parts[3])
	require.NoError(t, err)
	assert.Len(t, ct, len("hello")+16)
}

func TestSeal_SamePlaintextTwice_DiffersByNonce(t *testing.T) {
	s := sealer(t, 1)
	a, _ := s.Seal([]byte("same"), nil)
	b, _ := s.Seal([]byte("same"), nil)
	assert.NotEqual(t, a, b)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy exhausted") }

func TestSeal_RandomSourceFails_ReturnsError(t *testing.T) {
	s, err := crypto.NewSealer(make([]byte, 32), failingReader{})
	require.NoError(t, err)
	_, err = s.Seal([]byte("x"), nil)
	assert.ErrorContains(t, err, "entropy exhausted")
}

func TestOpen_WrongAAD_ErrDecrypt(t *testing.T) {
	s := sealer(t, 1)
	env, _ := s.Seal([]byte("x"), crypto.AAD("job", "1"))
	_, err := s.Open(env, crypto.AAD("job", "2"))
	assert.ErrorIs(t, err, crypto.ErrDecrypt)
}

func TestOpen_OtherKey_ErrUnknownKey(t *testing.T) {
	env, _ := sealer(t, 1).Seal([]byte("x"), nil)
	_, err := sealer(t, 2).Open(env, nil)
	assert.ErrorIs(t, err, crypto.ErrUnknownKey)
}

func TestOpen_TamperedCiphertext_ErrDecrypt(t *testing.T) {
	s := sealer(t, 1)
	env, _ := s.Seal([]byte("secret"), nil)
	parts := strings.Split(env, ".")
	ct, _ := base64.RawURLEncoding.DecodeString(parts[3])
	ct[0] ^= 0xff
	parts[3] = base64.RawURLEncoding.EncodeToString(ct)
	_, err := s.Open(strings.Join(parts, "."), nil)
	assert.ErrorIs(t, err, crypto.ErrDecrypt)
}

func TestOpen_Malformed_ErrMalformed(t *testing.T) {
	s := sealer(t, 1)
	env, _ := s.Seal([]byte("x"), nil)
	p := strings.Split(env, ".")
	short := base64.RawURLEncoding.EncodeToString([]byte("short"))
	tests := map[string]string{
		"empty":         "",
		"three parts":   strings.Join(p[:3], "."),
		"five parts":    env + ".x",
		"version v2":    "v2." + strings.Join(p[1:], "."),
		"bad nonce b64": strings.Join([]string{p[0], p[1], "!!!", p[3]}, "."),
		"bad ct b64":    strings.Join([]string{p[0], p[1], p[2], "!!!"}, "."),
		"short nonce":   strings.Join([]string{p[0], p[1], short, p[3]}, "."),
		"short ct":      strings.Join([]string{p[0], p[1], p[2], short[:2]}, "."),
	}
	for name, e := range tests {
		_, err := s.Open(e, nil)
		assert.ErrorIs(t, err, crypto.ErrMalformed, name)
	}
}

func TestAAD_JoinsPurposeAndBinding(t *testing.T) {
	assert.Equal(t, []byte("saved-credential|user-1"), crypto.AAD("saved-credential", "user-1"))
	assert.Equal(t, "saved-credential", crypto.PurposeSavedCredential)
	assert.Equal(t, "session-credential", crypto.PurposeSessionCredential)
	assert.Equal(t, "job", crypto.PurposeJob)
	assert.Equal(t, "oauth-state", crypto.PurposeOAuthState)
}

func TestAAD_PurposeContainingPipe_Panics(t *testing.T) {
	assert.Panics(t, func() { crypto.AAD("a|b", "c") })
}
