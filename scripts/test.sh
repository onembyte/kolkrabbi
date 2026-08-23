#!/usr/bin/env bash
# Run the tests for EVERY module in the repo.
#
# Use this, never a bare `go test ./...`. A nested module's tests are invisible
# to it: `go test ./...` in the root prints ok and exits 0 while a nested module
# containing an unconditional t.Fatal is simply never run. From the day
# tools/go.mod exists, bare ./... is a lie, and this script is the truth.
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
total=0

while IFS= read -r gomod; do
  dir="$(dirname "$gomod")"
  echo "── ${dir#./} ──"
  if ! (cd "$dir" && go test "$@" ./...); then
    fail=1
  fi
  count="$( (cd "$dir" && go test -count=1 -v ./... 2>/dev/null || true) | grep -c '^=== RUN' || true)"
  echo "   tests: $count"
  total=$(( total + count ))
done < <(find . -name go.mod -not -path './.git/*' -not -path '*/node_modules/*' | sort)

echo "── total: $total tests across all modules ──"
exit $fail
