#!/usr/bin/env bash
# Starts the Kubelook containers of the integration environment. See PLAN.md Step 7.3.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
mkdir -p "$ROOT/.it"

if [ ! -f "$ROOT/.it/app.env" ]; then
  echo "KUBELOOK_ENCRYPTION_KEY=$(head -c 32 /dev/urandom | base64)" > "$ROOT/.it/app.env"
  chmod 600 "$ROOT/.it/app.env"
fi
# shellcheck disable=SC1091
source "$ROOT/.it/app.env"

# Always recreate: the image may have changed since the last run.
remove_container kubelook-it-app
docker run -d --name kubelook-it-app --network kind -p 127.0.0.1:18080:8080 \
  -v "$ROOT/test/integration/config/sqlite.yaml:/etc/kubelook/config.yaml:ro" --tmpfs /data:uid=65532,gid=65532 --tmpfs /tmp \
  -e KUBELOOK_ENCRYPTION_KEY="$KUBELOOK_ENCRYPTION_KEY" -e KUBELOOK_GOOGLE_CLIENT_SECRET=it-secret -e KUBELOOK_GITHUB_CLIENT_SECRET=it-secret \
  kubelook:dev serve >/dev/null
wait_http http://127.0.0.1:18080/readyz 60
echo "APPS OK"
