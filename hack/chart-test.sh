#!/usr/bin/env bash
# Tests of the Helm chart (PLAN.md Step 8.2): lint, render and assert, with no cluster needed.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
CHART="$ROOT/deploy/helm/kubelook"
CI="$CHART/ci"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

fail() { echo "CHART FAIL: $*" >&2; exit 1; }

# render <values file> [helm args...]: the manifests of the chart as release "kubelook" in namespace "kubelook".
render() { local f="$1"; shift; helm template kubelook "$CHART" -n kubelook -f "$f" "$@"; }

# docs <Kind>: from the manifests on stdin, only the documents of that kind.
docs() { awk -v k="$1" 'BEGIN { RS = "\n---\n" } $0 ~ ("(^|\n)kind: " k "(\n|$)") { print $0; print "\n---" }'; }

# count <Kind>: how many documents of that kind.
count() { docs "$1" | grep -c '^kind: ' || true; }

# has <file> <extended regex> <message>
has() { grep -Eq -- "$2" "$1" || fail "$3 (missing: $2)"; }
lacks() { ! grep -Eq -- "$2" "$1" || fail "$3 (found: $2)"; }

[ -f "$CHART/Chart.yaml" ] || fail "no chart at deploy/helm/kubelook"

# The roles script in the chart is a copy of the one the database owner runs by hand.
diff -u "$ROOT/deploy/postgres/roles.sql" "$CHART/files/roles.sql" >/dev/null \
  || fail "deploy/helm/kubelook/files/roles.sql differs from deploy/postgres/roles.sql"

for f in "$CI"/*.yaml; do
  helm lint "$CHART" -f "$f" >/dev/null || { helm lint "$CHART" -f "$f" >&2; fail "helm lint $f"; }
  render "$f" > "$TMP/$(basename "$f")" || fail "helm template $f"
done

# --- SQLite
S="$TMP/sqlite-values.yaml"
docs Deployment < "$S" > "$TMP/s-deploy"
[ "$(count Deployment < "$S")" = 1 ] || fail "sqlite: expected one Deployment"
has "$TMP/s-deploy" 'type: Recreate' "sqlite: strategy Recreate"
has "$TMP/s-deploy" 'replicas: 1$' "sqlite: one replica"
has "$TMP/s-deploy" 'mountPath: /data$' "sqlite: the volume is mounted where the default sqlitePath is"
[ "$(count PersistentVolumeClaim < "$S")" = 1 ] || fail "sqlite: expected a PersistentVolumeClaim"
[ "$(count Job < "$S")" = 0 ] || fail "sqlite: no migrate Job"
[ "$(count CronJob < "$S")" = 0 ] || fail "sqlite: no CronJob when retentionDays is 0"
[ "$(count PodDisruptionBudget < "$S")" = 0 ] || fail "sqlite: no PodDisruptionBudget for one replica"
# The debug image is pinned to a digest by hack/pin-netshoot.sh (Step 8.1).
has "$S" 'image: "?ghcr\.io/bdnix/kubelook-netshoot@sha256:[0-9a-f]{64}"?$' "the default debug.image is pinned to a digest"

# --- PostgreSQL
P="$TMP/postgres-values.yaml"
docs Deployment < "$P" > "$TMP/p-deploy"
docs Job < "$P" > "$TMP/p-job"
docs CronJob < "$P" > "$TMP/p-cron"
has "$TMP/p-deploy" 'replicas: 2$' "postgres: two replicas"
has "$TMP/p-deploy" 'type: RollingUpdate' "postgres: RollingUpdate"
has "$TMP/p-deploy" 'name: KUBELOOK_DATABASE_URL' "postgres: the Deployment gets the application URL"
[ "$(count PodDisruptionBudget < "$P")" = 1 ] || fail "postgres: expected a PodDisruptionBudget"
[ "$(count PersistentVolumeClaim < "$P")" = 0 ] || fail "postgres: no PersistentVolumeClaim"
has "$TMP/p-job" 'helm.sh/hook: pre-install,pre-upgrade' "postgres: migrate hook"
has "$TMP/p-job" 'helm.sh/hook-delete-policy' "postgres: migrate hook delete policy"
has "$TMP/p-job" 'name: KUBELOOK_MIGRATE_DATABASE_URL' "postgres: the migrate Job gets the migration URL"
lacks "$TMP/p-job" 'psql' "postgres: roles are applied only when database.rolesSecret is set"
has "$TMP/p-cron" 'name: KUBELOOK_AUDIT_CLEANUP_DATABASE_URL' "postgres: the CronJob gets the cleanup URL"
lacks "$TMP/p-cron" 'name: KUBELOOK_DATABASE_URL' "postgres: the CronJob must not get the application URL"

# --- Full values: every setting reaches the configuration the application loads.
F="$TMP/full-values.yaml"
docs ConfigMap < "$F" | awk '
  /^  config.yaml: \|/ { on = 1; next }
  on && /^    / { sub(/^    /, ""); print; next }
  on { exit }' > "$TMP/rendered-config.yaml"
[ -s "$TMP/rendered-config.yaml" ] || fail "full: no config.yaml in the ConfigMap"
(cd "$ROOT" && KUBELOOK_TEST_CHART_CONFIG="$TMP/rendered-config.yaml" go test -count=1 ./internal/config -run TestLoad_RenderedChartConfig) \
  || fail "full: the rendered configuration does not load to the values that were set"
docs Job < "$F" > "$TMP/f-job"
has "$TMP/f-job" 'postgres:18\.6-trixie' "full: roles are applied with psql of postgres:18.6-trixie"
has "$TMP/f-job" 'retention_days=90' "full: retention_days equals audit.retentionDays"
has "$F" 'GOMEMLIMIT' "full: GOMEMLIMIT"
has "$F" 'value: 400MiB' "full: goMemLimit"
has "$F" 'hostnames:' "full: hostAliases"
has "$F" 'kind: Ingress' "full: Ingress"
has "$F" 'kind: NetworkPolicy' "full: NetworkPolicy"
has "$F" 'sessionAffinity: ClientIP' "full: sessionAffinity"
has "$F" 'name: KUBELOOK_GOOGLE_CLIENT_SECRET' "full: Google secret reference"
has "$F" 'name: KUBELOOK_GITHUB_CLIENT_SECRET' "full: GitHub secret reference"
has "$F" 'example.org/kubelook:1\.2\.3' "full: image"

# --- Schema failures: each of these must make the install fail.
expect_fail() {
  local why="$1" base="$2"; shift 2
  if helm template kubelook "$CHART" -n kubelook -f "$base" "$@" >/dev/null 2>&1; then fail "schema: accepted ($why)"; fi
}
expect_fail "empty encryption.keySecret" "$CI/sqlite-values.yaml" --set-string encryption.keySecret=
expect_fail "replicaCount > 1 with sqlite" "$CI/sqlite-values.yaml" --set replicaCount=2
expect_fail "postgres without database.existingSecret" "$CI/postgres-values.yaml" --set-string database.existingSecret=
expect_fail "helm.version 3" "$CI/sqlite-values.yaml" --set helm.version=3
expect_fail "retention with postgres and no cleanup credentials" "$CI/postgres-values.yaml" --set-string audit.cleanup.credentialsSecret=

# --- Every rendered manifest
for f in "$TMP"/*-values.yaml; do
  lacks "$f" 'automountServiceAccountToken: true' "$(basename "$f"): the token of the service account is not mounted"
  lacks "$f" 'hostPath' "$(basename "$f"): no hostPath"
  lacks "$f" 'privileged: true' "$(basename "$f"): no privileged container"
  lacks "$f" 'allowPrivilegeEscalation: true' "$(basename "$f"): no privilege escalation"
done
has "$TMP/s-deploy" 'automountServiceAccountToken: false' "the Deployment does not mount the service account token"
has "$TMP/s-deploy" 'readOnlyRootFilesystem: true' "read-only root filesystem"
has "$TMP/s-deploy" 'runAsUser: 65532' "runs as 65532"
has "$TMP/s-deploy" 'runAsNonRoot: true' "runs as non-root"
has "$TMP/s-deploy" 'path: /healthz' "liveness probe"
has "$TMP/s-deploy" 'path: /readyz' "readiness probe"
has "$TMP/s-deploy" 'checksum/config:' "the pod restarts when the configuration changes"

echo "CHART OK"
