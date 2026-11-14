package certs_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/certs"
)

var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func makeCert(t *testing.T, cn string, serial int64, notAfter time.Time, isCA bool, dns ...string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn, Organization: []string{"Acme"}},
		NotBefore: now.Add(-24 * time.Hour), NotAfter: notAfter, DNSNames: dns, IsCA: isCA, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func TestParse_ChainsInOrderAcrossKeysWithMetadataOnly(t *testing.T) {
	leaf, key := makeCert(t, "web.example", 42, now.Add(10*24*time.Hour), false, "web.example", "www.example")
	ca, _ := makeCert(t, "Root CA", 7, now.Add(3650*24*time.Hour), true)
	data := map[string][]byte{
		"tls.crt":   append(append([]byte{}, leaf...), ca...),
		"tls.key":   key,
		"note.txt":  []byte("not pem"),
		"binary":    {0xff, 0xfe, 0x00},
		"ca.crt":    ca,
		"mixed.pem": append(append([]byte("junk before\n"), key...), leaf...),
	}
	got := certs.Parse(data, now)
	var order []string
	for _, c := range got {
		order = append(order, c.Key+"#"+strings.TrimSpace(c.Subject))
	}
	assert.Equal(t, []string{
		"ca.crt#CN=Root CA,O=Acme",
		"mixed.pem#CN=web.example,O=Acme",
		"tls.crt#CN=web.example,O=Acme",
		"tls.crt#CN=Root CA,O=Acme",
	}, order)
	chain := got[2:]
	assert.Equal(t, 0, chain[0].Index)
	assert.Equal(t, 1, chain[1].Index)
	assert.Equal(t, "2a", chain[0].Serial)
	assert.Equal(t, []string{"web.example", "www.example"}, chain[0].DNSNames)
	assert.False(t, chain[0].IsCA)
	assert.True(t, chain[1].IsCA)
	assert.EqualValues(t, 10*24*3600, chain[0].ExpiresInSeconds)
	assert.Equal(t, now.Add(10*24*time.Hour), chain[0].NotAfter)
}

func TestParse_NeverReturnsPrivateKeyMaterial(t *testing.T) {
	leaf, key := makeCert(t, "web", 1, now.Add(time.Hour), false)
	got := certs.Parse(map[string][]byte{"tls.crt": leaf, "tls.key": key}, now)
	require.Len(t, got, 1)
	for _, c := range got {
		assert.NotContains(t, c.Subject+c.Issuer+c.Serial+c.Error, "PRIVATE")
	}
	for _, e := range got {
		assert.Empty(t, e.Error)
	}
	// the key's bytes appear nowhere in the output
	block, _ := pem.Decode(key)
	require.NotNil(t, block)
	assert.NotContains(t, strings.Join([]string{got[0].Subject, got[0].Issuer, got[0].Serial}, ""), string(block.Bytes[:8]))
}

func TestParse_ExpiredIsNegativeAndMalformedHasAnError(t *testing.T) {
	old, _ := makeCert(t, "old", 1, now.Add(-2*time.Hour), false)
	bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})
	got := certs.Parse(map[string][]byte{"a": old, "b": bad}, now)
	require.Len(t, got, 2)
	assert.EqualValues(t, -7200, got[0].ExpiresInSeconds)
	assert.Equal(t, "b", got[1].Key)
	assert.NotEmpty(t, got[1].Error)
	assert.Empty(t, got[1].Subject)
}

func TestParse_NoCertificates(t *testing.T) {
	assert.Empty(t, certs.Parse(map[string][]byte{"a": []byte("x")}, now))
	assert.Empty(t, certs.Parse(nil, now))
}
