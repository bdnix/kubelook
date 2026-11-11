#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export GOBIN="$ROOT/bin"
mkdir -p "$GOBIN"
OS="$(go env GOOS)"; ARCH="$(go env GOARCH)"

GOLANGCI_VERSION=v2.15.0
GOVULNCHECK_VERSION=v1.9.0
KIND_VERSION=v0.33.0
KUBECTL_VERSION=v1.37.2
HELM_VERSION=v4.3.1

go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
go install "sigs.k8s.io/kind@${KIND_VERSION}"

if [ ! -x "$GOBIN/kubectl" ] || ! "$GOBIN/kubectl" version --client 2>/dev/null | grep -q "${KUBECTL_VERSION}"; then
  curl -fsSLo "$GOBIN/kubectl" "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl"
  echo "$(curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl.sha256")  $GOBIN/kubectl" | sha256sum -c -
  chmod +x "$GOBIN/kubectl"
fi

if [ ! -x "$GOBIN/helm" ] || ! "$GOBIN/helm" version --short 2>/dev/null | grep -q "${HELM_VERSION}"; then
  tmp="$(mktemp -d)"
  curl -fsSLo "$tmp/helm.tgz" "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH}.tar.gz"
  echo "$(curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH}.tar.gz.sha256sum" | awk '{print $1}')  $tmp/helm.tgz" | sha256sum -c -
  tar -xzf "$tmp/helm.tgz" -C "$tmp"
  mv "$tmp/${OS}-${ARCH}/helm" "$GOBIN/helm"
  rm -rf "$tmp"
fi
echo "TOOLS OK"
