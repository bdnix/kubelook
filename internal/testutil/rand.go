package testutil

import (
	"crypto/sha256"
	"encoding/binary"
	"io"
)

type counterReader struct {
	seed []byte
	n    uint64
	buf  []byte
}

// Rand returns a deterministic io.Reader: a SHA-256 counter stream seeded by seed.
func Rand(seed string) io.Reader { return &counterReader{seed: []byte(seed)} }

func (r *counterReader) Read(p []byte) (int, error) {
	for len(r.buf) < len(p) {
		var c [8]byte
		binary.BigEndian.PutUint64(c[:], r.n)
		r.n++
		sum := sha256.Sum256(append(append([]byte(nil), r.seed...), c[:]...))
		r.buf = append(r.buf, sum[:]...)
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}
