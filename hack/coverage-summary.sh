#!/usr/bin/env bash
# Usage: coverage-summary.sh be|fe — prints the coverage numbers as Markdown for the job summary.
set -euo pipefail
echo "## Unit-test coverage (gate: 90%)"
echo
case "${1:-}" in
be)
  echo "### Backend"
  echo '```'
  grep -E 'Total (coverage threshold|test coverage)' go-coverage.txt || echo "no Go coverage output"
  echo '```'
  ;;
fe)
  echo "### Frontend"
  echo '```'
  if [ -f web/coverage/coverage-summary.json ]; then
    node -e 'const t=require("./web/coverage/coverage-summary.json").total;for(const k of ["lines","statements","functions","branches"])console.log(k.padEnd(11),t[k].pct+"%")'
  else
    echo "no web coverage output"
  fi
  echo '```'
  ;;
*)
  echo "usage: $0 be|fe" >&2
  exit 2
  ;;
esac
