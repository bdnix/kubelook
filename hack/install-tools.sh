#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export GOBIN="$ROOT/bin"
# Build the tools with the same Go as the module so lint can load its code.
export GOTOOLCHAIN="go$(sed -n 's/^go //p' "$ROOT/go.mod")"
mkdir -p "$GOBIN"
OS="$(go env GOOS)"; ARCH="$(go env GOARCH)"

# sha256 check that works with GNU coreutils and with stock macOS.
sha256_check() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum -c -; else shasum -a 256 -c -; fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

GOLANGCI_VERSION=v2.15.0
GOVULNCHECK_VERSION=v1.9.0
KIND_VERSION=v0.33.0
KUBECTL_VERSION=v1.37.2
HELM_VERSION=v4.3.1

go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
go install "sigs.k8s.io/kind@${KIND_VERSION}"

have_kubectl="$("$GOBIN/kubectl" version --client 2>/dev/null || true)"
if ! grep -Fqw "${KUBECTL_VERSION}" <<<"$have_kubectl"; then
  curl -fsSLo "$tmp/kubectl" "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl"
  echo "$(curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl.sha256")  $tmp/kubectl" | sha256_check
  chmod +x "$tmp/kubectl"
  mv "$tmp/kubectl" "$GOBIN/kubectl"
fi

have_helm="$("$GOBIN/helm" version --short 2>/dev/null || true)"
if ! grep -Fqw "${HELM_VERSION}" <<<"$have_helm"; then
  curl -fsSLo "$tmp/helm.tgz" "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH}.tar.gz"
  echo "$(curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH}.tar.gz.sha256sum" | awk '{print $1}')  $tmp/helm.tgz" | sha256_check
  tar -xzf "$tmp/helm.tgz" -C "$tmp"
  mv "$tmp/${OS}-${ARCH}/helm" "$GOBIN/helm"
fi
echo "TOOLS OK"
