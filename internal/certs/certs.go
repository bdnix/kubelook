// Package certs reads the certificates in a Secret's values. Only metadata leaves it: no key, no raw bytes.
package certs

import (
	"crypto/x509"
	"encoding/pem"
	"sort"
	"time"
)

// Entry describes one certificate (or one that could not be read).
type Entry struct {
	Key              string    `json:"key"`
	Index            int       `json:"index"`
	Subject          string    `json:"subject"`
	Issuer           string    `json:"issuer"`
	Serial           string    `json:"serial"`
	NotBefore        time.Time `json:"notBefore"`
	NotAfter         time.Time `json:"notAfter"`
	DNSNames         []string  `json:"dnsNames"`
	IsCA             bool      `json:"isCA"`
	ExpiresInSeconds int64     `json:"expiresInSeconds"`
	Error            string    `json:"error,omitempty"`
}

// Parse finds every PEM CERTIFICATE block of every value, in key order and, within a value, in file order.
// Other PEM blocks (private keys) and values that are not PEM are skipped without being looked at.
func Parse(data map[string][]byte, now time.Time) []Entry {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []Entry{}
	for _, k := range keys {
		rest := data[k]
		index := 0
		for {
			var block *pem.Block
			block, rest = pem.Decode(rest)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" {
				continue
			}
			e := Entry{Key: k, Index: index}
			index++
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				e.Error = "The certificate could not be read: " + err.Error()
				out = append(out, e)
				continue
			}
			e.Subject, e.Issuer = cert.Subject.String(), cert.Issuer.String()
			e.Serial = cert.SerialNumber.Text(16)
			e.NotBefore, e.NotAfter = cert.NotBefore, cert.NotAfter
			e.DNSNames = append([]string{}, cert.DNSNames...)
			e.IsCA = cert.IsCA
			e.ExpiresInSeconds = int64(cert.NotAfter.Sub(now) / time.Second)
			out = append(out, e)
		}
	}
	return out
}
