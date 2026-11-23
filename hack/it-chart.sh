#!/usr/bin/env bash
# Installs the Helm chart into the alpha kind cluster of the integration environment and makes the console
# reachable at http://localhost:18090 (PLAN.md Step 8.3). Run after `make it-up`.
#
#   hack/it-chart.sh            install (or upgrade) the chart, then start the port-forward
#   hack/it-chart.sh forward    (re)start the port-forward only: it ends when the pod it forwards to is replaced
#
# kubectl port-forward is test tooling on this side; the product has no port-forward feature.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
mkdir -p "$ROOT/.it"
KC="$ROOT/.it/kubeconfig-alpha-host.yaml"
K() { kubectl --kubeconfig "$KC" "$@"; }

stop_forward() {
  if [ -f "$ROOT/.it/pf.pid" ]; then kill "$(cat "$ROOT/.it/pf.pid")" 2>/dev/null || true; rm -f "$ROOT/.it/pf.pid"; fi
}

forward() {
  stop_forward
  K -n kubelook rollout status deployment/kubelook --timeout=180s >/dev/null
  nohup setsid kubectl --kubeconfig "$KC" -n kubelook port-forward svc/kubelook 18090:80 >"$ROOT/.it/pf.log" 2>&1 &
  echo $! > "$ROOT/.it/pf.pid"
  wait_http http://127.0.0.1:18090/readyz 60
}

if [ "${1:-}" = forward ]; then forward; echo "FORWARD OK"; exit 0; fi

ip() { docker inspect -f '{{.NetworkSettings.Networks.kind.IPAddress}}' "$1"; }

kind load docker-image kubelook:dev --name kubelook-it-alpha >/dev/null
K create namespace kubelook --dry-run=client -o yaml | K apply -f - >/dev/null
[ -f "$ROOT/.it/chart.env" ] || { echo "CHART_KEY=$(head -c 32 /dev/urandom | base64)" > "$ROOT/.it/chart.env"; chmod 600 "$ROOT/.it/chart.env"; }
# shellcheck disable=SC1091
source "$ROOT/.it/chart.env"
K -n kubelook create secret generic kubelook-key --from-literal=key="$CHART_KEY" --dry-run=client -o yaml | K apply -f - >/dev/null
K -n kubelook create secret generic kubelook-oauth --from-literal=google=it-secret --from-literal=github=it-secret \
  --dry-run=client -o yaml | K apply -f - >/dev/null

# The same sign-in and Vault settings as test/integration/config/sqlite.yaml, and the banner TestSmoke expects.
cat > "$ROOT/.it/chart-values.yaml" <<VALUES
image: {repository: kubelook, tag: dev, pullPolicy: Never}
encryption: {keySecret: kubelook-key}
server: {publicURL: "http://localhost:18090"}
auth:
  restrictSignIn: true
  google:
    clientId: kubelook-it
    clientSecretRef: {name: kubelook-oauth, key: google}
    allowedDomains: [corp.example]
    issuerURL: "http://kubelook-it-fakeidp:8300/oidc"
  github:
    clientId: kubelook-it
    clientSecretRef: {name: kubelook-oauth, key: github}
    allowedOrgs: [acme]
    baseURL: "http://kubelook-it-fakeidp:8300/github"
    apiBaseURL: "http://kubelook-it-fakeidp:8300/github/api"
vault:
  address: "http://kubelook-it-vault:8200"
  candidateRoles: [devops-role, viewer-role]
  tokenTTL: 10m
  remintBefore: 9m50s
debug: {image: "busybox:1.37", shell: /bin/sh}
fileBrowser: {maxTransferBytes: 1048576}
maintenance: {banner: "integration test banner"}
helm: {version: 4, maxChartBytes: 1048576}
hostAliases:
  - {ip: "$(ip kubelook-it-vault)", hostnames: [kubelook-it-vault]}
  - {ip: "$(ip kubelook-it-fakeidp)", hostnames: [kubelook-it-fakeidp]}
  - {ip: "$(ip kubelook-it-alpha-control-plane)", hostnames: [kubelook-it-alpha-control-plane]}
  - {ip: "$(ip kubelook-it-beta-control-plane)", hostnames: [kubelook-it-beta-control-plane]}
VALUES

helm --kubeconfig "$KC" upgrade --install kubelook "$ROOT/deploy/helm/kubelook" -n kubelook \
  -f "$ROOT/.it/chart-values.yaml" --wait --timeout 180s
forward
echo "CHART UP"
