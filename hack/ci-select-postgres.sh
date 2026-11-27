#!/usr/bin/env bash
# Reads the names of the changed files on standard input and prints "true" when the PostgreSQL and two-replica integration
# tests have to run for this change, "false" when SQLite on one replica is enough. CI calls it for pull requests and the
# merge queue (docs/ci.md); pushes to main and release pull requests run the tests without asking.
#
# A change selects them when it touches what the replicas share (the store and its migrations, sessions, the watch hub,
# the WebSocket layer, jobs, audit, configuration, the wiring in internal/app), how Kubelook is deployed (the chart, the
# PostgreSQL roles, the image, the dependencies), the integration environment itself or this pipeline.
set -euo pipefail
selected=false
while IFS= read -r f; do
  case "$f" in
    internal/store/*|internal/session/*|internal/watch/*|internal/ws/*|internal/jobs/*|internal/audit/*|internal/config/*|internal/app/*) selected=true ;;
    deploy/*|Dockerfile|go.mod|go.sum) selected=true ;;
    test/integration/*|hack/it-*.sh|hack/vault-setup.sh|hack/ci-select-postgres.sh) selected=true ;;
    .github/workflows/ci.yml|.github/actions/*) selected=true ;;
  esac
done
echo "$selected"
