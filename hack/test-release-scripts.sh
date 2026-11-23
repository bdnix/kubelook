#!/usr/bin/env bash
# Tests of the release scripts (PLAN.md Step 8.4): hack/version.sh. Prints "RELEASE SCRIPTS OK".
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
fails=0
fail() { echo "FAIL: $*" >&2; fails=$((fails + 1)); }

# version <content-printf-format> [args...]: runs hack/version.sh on a VERSION file with that content.
version() { printf "$1" > "$TMP/VERSION"; shift; VERSION_FILE="$TMP/VERSION" "$ROOT/hack/version.sh" "$@"; }

for v in 0.1.0 0.1.0-rc.1 12.30.456 1.0.0-rc.12; do
  out="$(version "$v\n" 2>&1)" || fail "$v was refused"
  [ "$out" = "$v" ] || fail "$v printed '$out'"
done
for v in v0.1.0 0.1 0.1.0-beta 0.1.0-rc 0.1.0-rc.1.2 ' 0.1.0' '0.1.0 ' 01.2.3x; do
  if version "$v\n" >/dev/null 2>&1; then fail "'$v' was accepted"; fi
done
if version '' >/dev/null 2>&1; then fail "an empty file was accepted"; fi
if version '0.1.0\n0.1.1\n' >/dev/null 2>&1; then fail "two lines were accepted"; fi
if version '0.1.0\n\n' >/dev/null 2>&1; then fail "a trailing empty line was accepted"; fi
if VERSION_FILE="$TMP/missing" "$ROOT/hack/version.sh" >/dev/null 2>&1; then fail "a missing file was accepted"; fi

[ "$(version '0.1.0-rc.1\n' --prerelease)" = true ] || fail "--prerelease is not true for 0.1.0-rc.1"
[ "$(version '0.1.0\n' --prerelease)" = false ] || fail "--prerelease is not false for 0.1.0"
if version 'bad\n' --prerelease >/dev/null 2>&1; then fail "--prerelease accepted a malformed version"; fi
if version '0.1.0\n' --unknown >/dev/null 2>&1; then fail "an unknown option was accepted"; fi

# The committed VERSION file is valid.
"$ROOT/hack/version.sh" >/dev/null || fail "the VERSION file of the repository is malformed"

[ "$fails" = 0 ] || { echo "$fails failure(s)" >&2; exit 1; }
echo "RELEASE SCRIPTS OK"
