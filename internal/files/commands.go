// Package files is the pod file browser: list, read, save, upload, download and delete files in a container
// through exec. Every command is an argument vector (never a shell line); nothing is written to the server's disk.
package files

import (
	"fmt"
	"path"
	"strings"

	"github.com/bdnix/kubelook/internal/apperr"
)

// Frame sizes and limits.
const (
	// ChunkBytes is the size of one transfer chunk before base64, so a frame stays under the 1 MiB limit.
	ChunkBytes = 48 << 10
	// ReadLimit is how much of a file the editor loads.
	ReadLimit = 1 << 20
	// binaryProbe is how many leading bytes are searched for NUL.
	binaryProbe = 8000
	// stderrLimit caps the stderr kept for an error message.
	stderrLimit = 4 << 10
	// listLimit caps the output of ls.
	listLimit = 8 << 20
)

// cleanPath checks and cleans an absolute path. NUL bytes and relative paths are refused.
func cleanPath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("the path contains a NUL byte: %w", apperr.ErrInvalid)
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("the path must be absolute: %w", apperr.ErrInvalid)
	}
	return path.Clean(p), nil
}

// leafName checks the name of a file to create inside a directory.
func leafName(n string) error {
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\x00") {
		return fmt.Errorf("the file name is not valid: %w", apperr.ErrInvalid)
	}
	return nil
}

func listArgv(p string) []string { return []string{"ls", "-lA", "--", p} }
func statArgv(p string) []string { return []string{"ls", "-ld", "--", p} }
func readArgv(p string) []string { return []string{"head", "-c", "1048577", "--", p} }
func saveArgv(p string) []string { return []string{"tee", "--", p} }

func deleteArgv(p string, recursive bool) ([]string, error) {
	if p == "/" {
		return nil, fmt.Errorf("the root directory cannot be deleted: %w", apperr.ErrInvalid)
	}
	if recursive {
		return []string{"rm", "-rf", "--", p}, nil
	}
	return []string{"rm", "-f", "--", p}, nil
}

func catArgv(p string) []string { return []string{"cat", "--", p} }

func tarCreateArgv(dir, base string) []string {
	return []string{"tar", "cf", "-", "-C", dir, "--", base}
}

func tarExtractArgv(dir string) []string { return []string{"tar", "xf", "-", "-C", dir} }

var tarProbeArgv = []string{"tar", "--version"}
