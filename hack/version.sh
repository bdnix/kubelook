#!/usr/bin/env bash
# Prints the version in the VERSION file after checking its form: MAJOR.MINOR.PATCH with an optional -rc.N suffix,
# without a leading "v", on exactly one line. Exits 1 otherwise.
#
#   hack/version.sh               prints the version
#   hack/version.sh --prerelease  prints true when the version has the -rc.N suffix, false otherwise
#
# VERSION_FILE overrides the file (the tests use it).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FILE="${VERSION_FILE:-$ROOT/VERSION}"

mode=version
case "${1:-}" in
  "") ;;
  --prerelease) mode=prerelease ;;
  *) echo "usage: version.sh [--prerelease]" >&2; exit 2 ;;
esac

[ -f "$FILE" ] || { echo "version: $FILE does not exist" >&2; exit 1; }
[ "$(grep -c '' "$FILE")" = 1 ] || { echo "version: $FILE must hold exactly one line" >&2; exit 1; }
version="$(cat "$FILE")"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]] \
  || { echo "version: '$version' does not match MAJOR.MINOR.PATCH or MAJOR.MINOR.PATCH-rc.N" >&2; exit 1; }

if [ "$mode" = prerelease ]; then
  [[ "$version" == *-rc.* ]] && echo true || echo false
else
  echo "$version"
fi
