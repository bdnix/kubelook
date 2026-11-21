#!/usr/bin/env bash
# Runs the watch-hub load test and appends the result table to docs/load-test.md.
# Parameters (environment): LOAD_PODS (1000), LOAD_SESSIONS (200), LOAD_EVENTS_PER_SECOND (50), LOAD_SECONDS (60).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export LOAD_COMMIT="${LOAD_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
export LOAD_DOCS="${LOAD_DOCS:-$ROOT/docs/load-test.md}"
make load
