#!/usr/bin/env bash
# Removes the integration environment. See PLAN.md Step 7.1.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
[ -f "$ROOT/.it/pf.pid" ] && kill "$(cat "$ROOT/.it/pf.pid")" 2>/dev/null || true   # the port-forward of hack/it-chart.sh
for n in kubelook-it-app kubelook-it-pg-1 kubelook-it-pg-2 kubelook-it-fakeidp kubelook-it-postgres kubelook-it-vault; do
  remove_container "$n"
done
for c in alpha beta; do kind delete cluster --name "kubelook-it-$c" || true; done
rm -rf "$ROOT/.it"
echo "IT DOWN"
