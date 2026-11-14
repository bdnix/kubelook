package files

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/bdnix/kubelook/internal/apperr"
)

// ErrUnsafeArchive is returned for a tar entry that could write outside its directory.
var ErrUnsafeArchive = errors.New("the archive holds an entry that would escape its directory")

// validateTarHeader refuses entries with absolute names, `..` elements, or links that leave the archive. When
// root is not empty, every entry must lie inside it.
func validateTarHeader(h *tar.Header, root string) error {
	name := h.Name
	if strings.HasPrefix(name, "/") || hasDotDot(name) {
		return fmt.Errorf("%w: %q", ErrUnsafeArchive, name)
	}
	clean := path.Clean(name)
	if root != "" && clean != root && !strings.HasPrefix(clean, root+"/") {
		return fmt.Errorf("%w: %q is outside %q", ErrUnsafeArchive, name, root)
	}
	switch h.Typeflag {
	case tar.TypeSymlink:
		if strings.HasPrefix(h.Linkname, "/") || escapes(path.Join(path.Dir(clean), h.Linkname)) { //nolint:gosec // G305: this is the check itself
			return fmt.Errorf("%w: link %q", ErrUnsafeArchive, name)
		}
	case tar.TypeLink:
		if strings.HasPrefix(h.Linkname, "/") || escapes(path.Clean(h.Linkname)) {
			return fmt.Errorf("%w: link %q", ErrUnsafeArchive, name)
		}
	}
	return nil
}

func hasDotDot(p string) bool {
	for _, el := range strings.Split(p, "/") {
		if el == ".." {
			return true
		}
	}
	return false
}

func escapes(p string) bool { return p == ".." || strings.HasPrefix(p, "../") }

// checkTar reads a whole tar stream and validates every header. It reads the bodies but keeps nothing.
func checkTar(r io.Reader, root string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("the archive is not a valid tar file: %w", apperr.ErrInvalid)
		}
		if err := validateTarHeader(h, root); err != nil {
			return err
		}
	}
}

// tarGuard validates a tar stream while it is forwarded: write the stream into it, then Wait.
type tarGuard struct {
	pw   *io.PipeWriter
	done chan error
}

func newTarGuard(root string, onUnsafe func()) *tarGuard {
	pr, pw := io.Pipe()
	g := &tarGuard{pw: pw, done: make(chan error, 1)}
	go func() {
		err := checkTar(pr, root)
		if err != nil {
			onUnsafe()
		}
		_, _ = io.Copy(io.Discard, pr) // never block the writer
		g.done <- err
	}()
	return g
}

func (g *tarGuard) Write(p []byte) (int, error) {
	// A guard that gave up has closed the pipe's reader side; the forward must not fail because of it.
	n, err := g.pw.Write(p)
	if err != nil {
		return len(p), nil //nolint:nilerr // the verdict is read from Wait
	}
	return n, nil
}

// Wait ends the stream and returns the verdict.
func (g *tarGuard) Wait() error {
	_ = g.pw.Close()
	return <-g.done
}

// singleFileTar builds a tar archive that holds one file.
func singleFileTar(name string, data []byte) ([]byte, error) {
	var b strings.Builder
	w := tar.NewWriter(&b)
	if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return nil, fmt.Errorf("building the archive: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("building the archive: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("building the archive: %w", err)
	}
	return []byte(b.String()), nil
}
