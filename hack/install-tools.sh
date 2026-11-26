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
HELM_VERSION=v4.3.2
HELM3_VERSION=v3.22.1   # integration tests only: makes and checks releases of Helm 3

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
built "$(have kind version)" "kind ${KIND_VERSION} " || \
  reinstall kind "sigs.k8s.io/kind@${KIND_VERSION}"

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
have_helm3="$("$GOBIN/helm3" version --short 2>/dev/null || true)"
if ! grep -Fqw "${HELM3_VERSION}" <<<"$have_helm3"; then
  curl -fsSLo "$tmp/helm3.tgz" "https://get.helm.sh/helm-${HELM3_VERSION}-${OS}-${ARCH}.tar.gz"
  echo "$(curl -fsSL "https://get.helm.sh/helm-${HELM3_VERSION}-${OS}-${ARCH}.tar.gz.sha256sum" | awk '{print $1}')  $tmp/helm3.tgz" | sha256_check
  mkdir "$tmp/helm3"
  tar -xzf "$tmp/helm3.tgz" -C "$tmp/helm3"
  mv "$tmp/helm3/${OS}-${ARCH}/helm" "$GOBIN/helm3"
fi
echo "TOOLS OK"
