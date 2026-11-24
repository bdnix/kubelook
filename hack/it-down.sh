#!/usr/bin/env bash
# Removes the integration environment.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
for n in kubelook-it-fakeidp kubelook-it-postgres kubelook-it-vault; do
  remove_container "$n"
done
for c in alpha beta host; do kind delete cluster --name "kubelook-it-$c" || true; done
rm -rf "$ROOT/.it"
echo "IT DOWN"
