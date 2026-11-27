#!/usr/bin/env bash
# Tests of hack/ci-select-postgres.sh: which changed files make CI run the PostgreSQL and two-replica integration tests.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fails=0
# expect <true|false> <file>...: the script's answer for a change of exactly these files.
expect() {
  local want="$1"; shift
  local got
  got="$(printf '%s\n' "$@" | "$ROOT/hack/ci-select-postgres.sh")"
  if [ "$got" != "$want" ]; then echo "FAIL: [$*] want $want, got $got" >&2; fails=$((fails + 1)); fi
}

# what is shared between replicas, or deployed with them
expect true internal/store/db.go
expect true internal/store/migrations/0003_audit.sql
expect true internal/session/manager.go
expect true internal/watch/hub.go
expect true internal/ws/mux.go
expect true internal/jobs/runner.go
expect true internal/audit/audit.go
expect true internal/config/config.go
expect true internal/app/serve.go
expect true deploy/helm/kubelook/templates/deployment.yaml
expect true deploy/postgres/roles.sql
expect true test/integration/postgres_test.go
expect true hack/it-up.sh
expect true hack/it-kubelook.sh
expect true hack/it-pg.sh
expect true hack/ci-select-postgres.sh
expect true .github/workflows/ci.yml
expect true .github/actions/integration/action.yml
expect true Dockerfile
expect true go.mod
expect true go.sum
# one such file among others is enough
expect true README.md web/src/App.tsx internal/store/db.go
# everything else
expect false web/src/features/resources/ResourceListPage.tsx
expect false web/package-lock.json
expect false internal/kube/discovery.go
expect false internal/logs/stream.go
expect false internal/resourcemap/channel.go
expect false docs/ci.md
expect false README.md
expect false PLAN.md
expect false .github/workflows/netshoot.yml
# no change at all
expect false

if [ "$fails" -ne 0 ]; then echo "CI SELECT FAIL: $fails case(s)" >&2; exit 1; fi
echo "CI SELECT OK"
