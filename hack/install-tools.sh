#!/usr/bin/env bash
# Installs the developer tools built from Go source (golangci-lint, govulncheck) into bin/. Nothing else: kind, kubectl and
# Helm belong to the integration environment (hack/install-it-tools.sh).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export GOBIN="$ROOT/bin"
# Build the tools with the same Go as the module so lint can load its code.
export GOTOOLCHAIN="go$(sed -n 's/^go //p' "$ROOT/go.mod")"
mkdir -p "$GOBIN"

GOLANGCI_VERSION=v2.15.0
GOVULNCHECK_VERSION=v1.9.0

# A Go tool is built again only when bin/ does not hold it at this version, built with this Go (its version output
# names both), so a cached bin/ makes this step take seconds.
GO_VERSION="${GOTOOLCHAIN}"
have() { "$GOBIN/$1" "${@:2}" 2>/dev/null || true; }
built() { grep -Fq "$2" <<<"$1" && grep -Fq "$GO_VERSION" <<<"$1"; }
# reinstall <name> <package>: replaces whatever bin/<name> holds (go install refuses to overwrite a non-binary).
reinstall() { rm -f "$GOBIN/$1"; go install "$2"; }
built "$(have golangci-lint version)" "version ${GOLANGCI_VERSION#v} " || \
  reinstall golangci-lint "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
built "$(have govulncheck -version)" "govulncheck@${GOVULNCHECK_VERSION}" || \
  reinstall govulncheck "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
echo "TOOLS OK"
