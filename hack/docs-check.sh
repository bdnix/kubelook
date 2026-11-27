#!/usr/bin/env bash
# Checks the documentation (docs/plans/20261126-021417-plan.md Step 8.5): every configuration key and environment variable is in
# docs/configuration.md, every relative Markdown link points to a file, the required documents exist, and the
# README is no longer "under construction". Prints DOCS OK.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"
fails=0
fail() { echo "DOCS FAIL: $*" >&2; fails=$((fails + 1)); }

# Required documents.
for f in docs/deployment.md docs/configuration.md docs/vault-policy.md docs/postgres.md docs/docker-run.md docs/operations.md \
         docs/security-review.md docs/releasing.md docs/ci.md docs/spike-findings.md README.md; do
  [ -f "$f" ] || fail "missing $f"
done
[ -f docs/configuration.md ] || { echo "DOCS FAIL: cannot go on without docs/configuration.md" >&2; exit 1; }

# Every key of the configuration file (printed by TestPrintKeys) is in docs/configuration.md, in backticks.
keys="$(go test ./internal/config -run TestPrintKeys -v -count=1 | sed -n 's/.*KEY \(.*\)$/\1/p')"
[ -n "$keys" ] || fail "TestPrintKeys printed no keys"
while IFS= read -r k; do
  grep -qF "\`$k\`" docs/configuration.md || fail "docs/configuration.md does not document the key \`$k\`"
done <<< "$keys"

# Every environment variable that the code reads.
vars="$(grep -rhoE 'KUBELOOK_[A-Z_]+' --include='*.go' --exclude='*_test.go' cmd internal | sort -u)"
while IFS= read -r v; do
  [ -z "$v" ] || grep -qF "\`$v\`" docs/configuration.md || fail "docs/configuration.md does not document the variable \`$v\`"
done <<< "$vars"

# Relative links. A link is [text](target) where the target is not a URL or an anchor of the same file.
files=()
while IFS= read -r f; do files+=("$f"); done < <(ls README.md docs/*.md deploy/helm/kubelook/README.md 2>/dev/null)
for f in "${files[@]}"; do
  dir="$(dirname "$f")"
  # Links inside code fences are examples, not links.
  while IFS= read -r target; do
    case "$target" in http://*|https://*|mailto:*|\#*|"") continue ;; esac
    path="${target%%#*}"
    [ -e "$dir/$path" ] || fail "$f links to $target, which does not exist"
  done < <(awk '/^```/ { fence = !fence; next } !fence' "$f" | grep -oE '\]\([^)]+\)' | sed -E 's/^\]\(//; s/\)$//; s/ .*//')
done

! grep -qi 'under construction' README.md || fail 'README.md still says "Under construction"'

[ "$fails" = 0 ] || { echo "$fails problem(s)" >&2; exit 1; }
echo "DOCS OK"
