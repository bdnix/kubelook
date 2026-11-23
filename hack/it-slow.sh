#!/usr/bin/env bash
# Starts kubelook-it-slow (port 18083), the container of the nightly credential-expiry test. Its credentials are
# re-minted only at the last instant (vault.remintBefore: 1ns), so they can expire. See PLAN.md Step 7.11.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
# shellcheck disable=SC1091
source "$ROOT/.it/app.env"
remove_container kubelook-it-slow
docker run -d --name kubelook-it-slow --network kind -p 127.0.0.1:18083:8080 \
  -v "$ROOT/test/integration/config/slow.yaml:/etc/kubelook/config.yaml:ro" --tmpfs /data:uid=65532,gid=65532 --tmpfs /tmp \
  -e KUBELOOK_ENCRYPTION_KEY="$KUBELOOK_ENCRYPTION_KEY" -e KUBELOOK_GOOGLE_CLIENT_SECRET=it-secret -e KUBELOOK_GITHUB_CLIENT_SECRET=it-secret \
  kubelook:dev serve >/dev/null
wait_http http://127.0.0.1:18083/readyz 60
