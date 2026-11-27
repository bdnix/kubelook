#!/usr/bin/env bash
# Installs the tools only the integration environment and the chart tests use (kind, kubectl, Helm 4, Helm 3) into bin/.
# A developer who only builds, lints and runs unit tests never needs them: `make tools` and `make verify` do not call this.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export GOBIN="$ROOT/bin"
# Build kind with the same Go as the module.
export GOTOOLCHAIN="go$(sed -n 's/^go //p' "$ROOT/go.mod")"
mkdir -p "$GOBIN"
OS="$(go env GOOS)"; ARCH="$(go env GOARCH)"

# sha256 check that works with GNU coreutils and with stock macOS.
sha256_check() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum -c -; else shasum -a 256 -c -; fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

KIND_VERSION=v0.33.0
KUBECTL_VERSION=v1.37.2
HELM_VERSION=v4.3.2
HELM3_VERSION=v3.22.1   # makes and checks releases of Helm 3 in one integration test

GO_VERSION="${GOTOOLCHAIN}"
have() { "$GOBIN/$1" "${@:2}" 2>/dev/null || true; }
built() { grep -Fq "$2" <<<"$1" && grep -Fq "$GO_VERSION" <<<"$1"; }
reinstall() { rm -f "$GOBIN/$1"; go install "$2"; }
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
echo "IT TOOLS OK"
