#!/usr/bin/env bash
# Kubelook in the integration environment: installed from its Helm chart into the host cluster, reachable at
# http://localhost:18080 (all replicas), :18081 (replica 1) and, with two replicas, :18082 (replica 2).
# IT_DATABASE (hack/it-lib.sh) chooses: sqlite (default) is one replica on SQLite; postgres is two replicas on PostgreSQL.
#
#   it-kubelook.sh install          install or upgrade the release, then expose
#   it-kubelook.sh expose           (re)label the pods as replica 1 and 2 and wait until all three addresses answer
#   it-kubelook.sh kill <1|2>       delete that replica's pod at once, without a graceful shutdown
#   it-kubelook.sh stop <1|2>       delete that replica's pod with a graceful shutdown
#   it-kubelook.sh rekey [key]      restart both replicas with another encryption key (default: the one of .it/app.env)
#   it-kubelook.sh uninstall        helm uninstall
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
DATABASE="$(it_database)"
if [ "$DATABASE" = postgres ]; then REPLICAS=2; else REPLICAS=1; fi
mkdir -p "$ROOT/.it"
KC="$ROOT/.it/kubeconfig-host-host.yaml"
NS=kubelook
K() { kubectl --kubeconfig "$KC" -n "$NS" "$@"; }
PODS='app.kubernetes.io/name=kubelook,app.kubernetes.io/instance=kubelook,!app.kubernetes.io/component'
DB=kubelook-it-postgres

[ -f "$ROOT/.it/app.env" ] || { echo "KUBELOOK_ENCRYPTION_KEY=$(head -c 32 /dev/urandom | base64)" > "$ROOT/.it/app.env"; chmod 600 "$ROOT/.it/app.env"; }
# shellcheck disable=SC1091
source "$ROOT/.it/app.env"

# step <text>: a timestamped line, so a slow step shows in the output the tests log.
step() { echo "$(date -u +%T) $*"; }
secret() { local name="$1"; shift; K create secret generic "$name" "$@" --dry-run=client -o yaml | K apply -f - >/dev/null; }
ip() { docker inspect -f '{{.NetworkSettings.Networks.kind.IPAddress}}' "$1"; }

install() {
  kind load docker-image kubelook:dev --name kubelook-it-host >/dev/null
  K get namespace "$NS" >/dev/null 2>&1 || kubectl --kubeconfig "$KC" create namespace "$NS" >/dev/null
  secret kubelook-key --from-literal=key="$KUBELOOK_ENCRYPTION_KEY"
  secret kubelook-oauth --from-literal=google=it-secret --from-literal=github=it-secret
  local database_values audit_values db_alias=""
  if [ "$DATABASE" = postgres ]; then
    secret kubelook-db --from-literal=url="postgres://kubelook_app:app@$DB:5432/kubelook"
    secret kubelook-db-migrate --from-literal=url="postgres://postgres:postgres@$DB:5432/kubelook"
    secret kubelook-db-roles --from-literal=appPassword=app --from-literal=cleanupPassword=cleanup
    secret kubelook-db-cleanup --from-literal=url="postgres://kubelook_cleanup:cleanup@$DB:5432/kubelook"
    database_values='database:
  driver: postgres
  existingSecret: kubelook-db
  migrationSecret: kubelook-db-migrate
  rolesSecret: kubelook-db-roles'
    audit_values='audit:
  retentionDays: 1
  cleanup: {credentialsSecret: kubelook-db-cleanup}'
    db_alias="  - {ip: \"$(ip $DB)\", hostnames: [$DB]}"
  else
    database_values='database: {driver: sqlite}'
    audit_values='audit: {retentionDays: 0}'
  fi
  cat > "$ROOT/.it/kubelook-values.yaml" <<VALUES
image: {repository: kubelook, tag: dev, pullPolicy: Never}
replicaCount: $REPLICAS
encryption: {keySecret: kubelook-key}
server: {publicURL: "http://localhost:18080"}
$database_values
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
$audit_values
maintenance: {banner: "integration test banner"}
helm: {version: 4, maxChartBytes: 1048576}
hostAliases:
  - {ip: "$(ip kubelook-it-vault)", hostnames: [kubelook-it-vault]}
  - {ip: "$(ip kubelook-it-fakeidp)", hostnames: [kubelook-it-fakeidp]}
$db_alias
  - {ip: "$(ip kubelook-it-alpha-control-plane)", hostnames: [kubelook-it-alpha-control-plane]}
  - {ip: "$(ip kubelook-it-beta-control-plane)", hostnames: [kubelook-it-beta-control-plane]}
VALUES
  step "helm upgrade --install (waits for the migrate hook and the rollout)"
  helm --kubeconfig "$KC" upgrade --install kubelook "$ROOT/deploy/helm/kubelook" -n "$NS" \
    -f "$ROOT/.it/kubelook-values.yaml" "$@" --wait --timeout 300s >/dev/null
  step "helm done"
  expose
}

# expose gives each running pod a replica number (keeping the numbers pods already have) and points a NodePort
# Service at each number, plus one at both.
expose() {
  step "rollout status"
  K rollout status deployment/kubelook --timeout=300s >/dev/null
  step "rollout done; waiting for $REPLICAS live pod(s)"
  local n pod num rows
  # live = Running and not being deleted; columns: name, replica number (or <none>), deletion time (or <none>)
  live() { K get pods -l "$PODS" --field-selector=status.phase=Running --no-headers \
             -o custom-columns=NAME:.metadata.name,N:.metadata.labels.it-replica,DEL:.metadata.deletionTimestamp | awk '$3 == "<none>"'; }
  for _ in $(seq 1 120); do [ "$(live | wc -l | tr -d ' ')" = "$REPLICAS" ] && break; sleep 1; done
  rows="$(live)"
  [ "$(wc -l <<<"$rows" | tr -d ' ')" = "$REPLICAS" ] || { echo "expected $REPLICAS running Kubelook pod(s), got:" >&2; K get pods -o wide >&2; exit 1; }
  local used
  used=" $(awk '$2 != "<none>" {printf "%s ", $2}' <<<"$rows")"
  while read -r pod num _; do
    [ "$num" = "<none>" ] || continue
    for n in $(seq 1 "$REPLICAS"); do
      if [[ "$used" != *" $n "* ]]; then
        K label pod "$pod" "it-replica=$n" --overwrite >/dev/null
        used="$used$n "
        break
      fi
    done
  done <<<"$rows"
  step "pods numbered; applying the Services"
  K apply -f - >/dev/null <<SERVICES
apiVersion: v1
kind: Service
metadata: {name: kubelook-it}
spec:
  type: NodePort
  selector: {app.kubernetes.io/name: kubelook, app.kubernetes.io/instance: kubelook}
  ports: [{port: 80, targetPort: http, nodePort: 30080}]
---
apiVersion: v1
kind: Service
metadata: {name: kubelook-it-1}
spec:
  type: NodePort
  selector: {app.kubernetes.io/name: kubelook, app.kubernetes.io/instance: kubelook, it-replica: "1"}
  ports: [{port: 80, targetPort: http, nodePort: 30081}]
SERVICES
  local ports=(18080 18081)
  if [ "$REPLICAS" = 2 ]; then
    K apply -f - >/dev/null <<SERVICES
apiVersion: v1
kind: Service
metadata: {name: kubelook-it-2}
spec:
  type: NodePort
  selector: {app.kubernetes.io/name: kubelook, app.kubernetes.io/instance: kubelook, it-replica: "2"}
  ports: [{port: 80, targetPort: http, nodePort: 30082}]
SERVICES
    ports+=(18082)
  fi
  for n in "${ports[@]}"; do wait_http "http://127.0.0.1:$n/readyz" 90; step "$n answers"; done
  step "all ${#ports[@]} addresses answer"
}

delete_replica() {
  local n="${1:?replica 1 or 2}"; shift
  K delete pod -l "it-replica=$n" "$@" >/dev/null
}

case "${1:-}" in
  install) shift; install "$@" ;;
  expose) expose ;;
  kill) delete_replica "${2:-}" --grace-period=0 --force --wait=false ;;
  stop) delete_replica "${2:-}" --wait=true ;;
  rekey)
    step "rekey: new key Secret, rollout restart"
    secret kubelook-key --from-literal=key="${2:-$KUBELOOK_ENCRYPTION_KEY}"
    K rollout restart deployment/kubelook >/dev/null
    K delete services kubelook-it-1 kubelook-it-2 --ignore-not-found >/dev/null
    expose
    ;;
  uninstall)
    step "helm uninstall --wait"
    helm --kubeconfig "$KC" uninstall kubelook -n "$NS" --wait --timeout 300s >/dev/null
    step "uninstalled"
    ;;
  *) echo "usage: it-kubelook.sh install|expose|kill <n>|stop <n>|rekey [key]|uninstall" >&2; exit 2 ;;
esac
