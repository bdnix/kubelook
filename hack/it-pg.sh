#!/usr/bin/env bash
# The PostgreSQL side of the integration environment. The schema and the roles are applied by the chart's migrate
# hook (hack/it-kubelook.sh); the tests use this script to change the roles and run one-off commands.
#   it-pg.sh roles <retention_days> apply deploy/postgres/roles.sql with that retention period
#   it-pg.sh oneoff <command> [NAME=value ...]   run a one-off container of the image (kubelook:dev), with extra environment
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

case "${1:-}" in
  roles) roles "${2:?retention days}" ;;
  oneoff) shift; oneoff "$@" ;;
  *) echo "usage: it-pg.sh roles <days>|oneoff <command> [NAME=value ...]" >&2; exit 2 ;;
esac
