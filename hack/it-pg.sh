#!/usr/bin/env bash
# The PostgreSQL side of the integration environment. See PLAN.md Step 7.9.
#   it-pg.sh schema                 migrate the database and apply deploy/postgres/roles.sql (retention 1 day), twice
#   it-pg.sh roles <retention_days> apply deploy/postgres/roles.sql with that retention period
#   it-pg.sh replicas [key]         (re)start kubelook-it-pg-1 and -pg-2 (ports 18081, 18082); the key defaults to .it/app.env
#   it-pg.sh oneoff <command> [NAME=value ...]   run a one-off container of the image, with extra environment
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
# shellcheck disable=SC1091
source "$ROOT/.it/app.env"
DB=kubelook-it-postgres
APP_URL="postgres://kubelook_app:app@$DB:5432/kubelook"

# base_env <key>: the environment every Kubelook container of this database needs (the configuration loader
# validates it for every command).
base_env() {
  echo -e "-e\nKUBELOOK_ENCRYPTION_KEY=$1\n-e\nKUBELOOK_DATABASE_URL=$APP_URL\n-e\nKUBELOOK_GOOGLE_CLIENT_SECRET=it-secret\n-e\nKUBELOOK_GITHUB_CLIENT_SECRET=it-secret"
}

roles() {
  docker exec -i -e PGOPTIONS="-c client_min_messages=warning" "$DB" psql -v ON_ERROR_STOP=1 -q -U postgres -d kubelook \
    -v app_password=app -v cleanup_password=cleanup -v "retention_days=$1" < "$ROOT/deploy/postgres/roles.sql"
}

oneoff() {
  local cmd="$1"; shift
  local extra=()
  for kv in "$@"; do extra+=(-e "$kv"); done
  mapfile -t env < <(base_env "$KUBELOOK_ENCRYPTION_KEY")
  docker run --rm --network kind -v "$ROOT/test/integration/config/postgres.yaml:/etc/kubelook/config.yaml:ro" --tmpfs /tmp \
    "${env[@]}" "${extra[@]}" kubelook:dev "$cmd"
}

replicas() {
  local key="${1:-$KUBELOOK_ENCRYPTION_KEY}" n port
  mapfile -t env < <(base_env "$key")
  for n in 1 2; do
    port=$((18080 + n))
    remove_container "kubelook-it-pg-$n"
    docker run -d --name "kubelook-it-pg-$n" --network kind -p "127.0.0.1:$port:8080" \
      -v "$ROOT/test/integration/config/postgres.yaml:/etc/kubelook/config.yaml:ro" --tmpfs /tmp \
      "${env[@]}" kubelook:dev serve >/dev/null
  done
  for n in 1 2; do wait_http "http://127.0.0.1:$((18080 + n))/readyz" 60; done
}

case "${1:-}" in
  schema)
    oneoff migrate KUBELOOK_MIGRATE_DATABASE_URL="postgres://postgres:postgres@$DB:5432/kubelook"
    roles 1; roles 1   # twice, to prove roles.sql is idempotent
    ;;
  roles) roles "${2:?retention days}" ;;
  replicas) replicas "${2:-}" ;;
  oneoff) shift; oneoff "$@" ;;
  *) echo "usage: it-pg.sh schema|roles <days>|replicas [key]|oneoff <command> [NAME=value ...]" >&2; exit 2 ;;
esac
