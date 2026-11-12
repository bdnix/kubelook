#!/usr/bin/env bash
set -euo pipefail
echo "## Unit-test coverage (gate: 90%)"
echo
echo "### Go"
echo '```'
grep -E 'Total (coverage threshold|test coverage)' go-coverage.txt || echo "no Go coverage output"
echo '```'
echo "### Web"
echo '```'
if [ -f web/coverage/coverage-summary.json ]; then
  node -e 'const t=require("./web/coverage/coverage-summary.json").total;for(const k of ["lines","statements","functions","branches"])console.log(k.padEnd(11),t[k].pct+"%")'
else
  echo "no web coverage output"
fi
echo '```'
