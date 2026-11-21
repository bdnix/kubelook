#!/usr/bin/env bash
# Builds the chart repository of the integration environment: the packaged demo charts and index.yaml in
# .it/charts/, which the identity stand-in serves under /charts. See PLAN.md Step 7.8.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
OUT="$ROOT/.it/charts"
rm -rf "$OUT"; mkdir -p "$OUT"

helm package "$ROOT/test/integration/testdata/charts/demo" -d "$OUT" >/dev/null

# demo-big: the demo chart under another name plus 2 MiB of random bytes, which do not compress, so the
# archive is larger than the 1 MiB limit of the test configuration.
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
cp -r "$ROOT/test/integration/testdata/charts/demo" "$tmp/demo-big"
sed -i 's/^name: demo$/name: demo-big/' "$tmp/demo-big/Chart.yaml"
mkdir -p "$tmp/demo-big/files"
head -c 2097152 /dev/urandom > "$tmp/demo-big/files/blob.bin"
helm package "$tmp/demo-big" -d "$OUT" >/dev/null

helm repo index "$OUT" --url http://kubelook-it-fakeidp:8300/charts
chmod -R a+rX "$OUT"
echo "CHARTS OK"
