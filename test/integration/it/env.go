//go:build integration

// Package it is the small client library of the integration tests: the environment, a Kubelook
// API client with a cookie jar, a WebSocket client, and polling helpers.
package it

import (
	"bufio"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// BaseURL is the address of the Kubelook container with SQLite.
func BaseURL() string {
	if v := os.Getenv("KUBELOOK_IT_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:18080"
}

var (
	rootOnce sync.Once
	rootDir  string
)

// Root is the repository root (the directory with go.mod above the working directory).
func Root() string {
	rootOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				rootDir = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				panic("go.mod not found above the working directory")
			}
			dir = parent
		}
	})
	return rootDir
}

// Token returns a Vault token written by hack/vault-setup.sh, e.g. Token("ALICE_TOKEN").
func Token(name string) string {
	f, err := os.Open(filepath.Join(Root(), ".it", "tokens.env"))
	if err != nil {
		panic("reading .it/tokens.env (run make it-up): " + err.Error())
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok && k == name {
			return v
		}
	}
	panic("token " + name + " not in .it/tokens.env")
}

func run(t testing.TB, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // G204: test helper running the pinned tools
	cmd.Dir = Root()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Kubectl runs bin/kubectl against a cluster ("alpha" or "beta") with the host kubeconfig.
func Kubectl(t testing.TB, cluster string, args ...string) string {
	t.Helper()
	all := append([]string{"--kubeconfig", filepath.Join(Root(), ".it", "kubeconfig-"+cluster+"-host.yaml")}, args...)
	return run(t, filepath.Join(Root(), "bin", "kubectl"), all...)
}

// Vault runs the vault CLI inside the Vault container as the root token.
func Vault(t testing.TB, args ...string) string {
	t.Helper()
	all := append([]string{"exec", "-e", "VAULT_ADDR=http://127.0.0.1:8200", "-e", "VAULT_TOKEN=root", "kubelook-it-vault", "vault"}, args...)
	return run(t, "docker", all...)
}

// Docker runs the docker CLI.
func Docker(t testing.TB, args ...string) string {
	t.Helper()
	return run(t, "docker", args...)
}

// TryDocker runs the docker CLI and ignores a failure (cleanups that may already have happened).
func TryDocker(args ...string) {
	cmd := exec.CommandContext(context.Background(), "docker", args...) //nolint:gosec // G204: test helper
	_ = cmd.Run()
}

// ClusterPath is the percent-encoded cluster ID for a URL path ("k:a/b" contains a slash).
func ClusterPath(id string) string { return url.PathEscape(id) }
