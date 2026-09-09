#!/usr/bin/env bash
# Run the tests for EVERY module in the repo.
#
# Use this, never a bare `go test ./...`. A nested module's tests are invisible
# to it: `go test ./...` in the root prints ok and exits 0 while a nested module
# containing an unconditional t.Fatal is simply never run. From the day
# tools/go.mod exists, bare ./... is a lie, and this script is the truth.
#
# One run per module, not two (OPTIMIZATION_PLAN.md O9). The run is verbose and
# tee'd to a log; the test count is grepped out of that log instead of paid for
# with a second run, and the module's real exit status is recovered through
# ${PIPESTATUS[0]} rather than tee's.
#
# The root module's log lands at ${KOLK_TEST_LOG:-<temp>} so that
# scripts/check-budgets.sh can read the test-count floor and the sandbox
# overhead line out of this run instead of taking a third one. `make check`
# and CI set the variable; `make test` on its own still works without it.
set -euo pipefail
cd "$(dirname "$0")/.."

logdir="$(mktemp -d)"
trap 'rm -rf "$logdir"' EXIT

root_log="${KOLK_TEST_LOG:-$logdir/root.log}"
mkdir -p "$(dirname "$root_log")"

fail=0
total=0
n=0

while IFS= read -r gomod; do
  dir="$(dirname "$gomod")"
  echo "── ${dir#./} ──"
  if [ "$dir" = "." ]; then
    log="$root_log"
  else
    n=$(( n + 1 ))
    log="$logdir/module-$n.log"
  fi

  set +e
  (cd "$dir" && go test -count=1 -v "$@" ./...) 2>&1 | tee "$log" >/dev/null
  rc=${PIPESTATUS[0]}
  set -e

  count="$(grep -c '^=== RUN' "$log" || true)"
  echo "   tests: $count"
  total=$(( total + count ))

  if [ "$rc" -ne 0 ]; then
    fail=1
    # -v output for a whole module is megabytes, so only the failures are
    # lifted out of the log; the log itself is the temp file above.
    echo "   FAILED (exit $rc) — failures from $log:"
    grep -E '^(\s*--- FAIL|FAIL|panic:|\s*--- SKIP: .*build)' "$log" | head -40 | sed 's/^/   /'
  fi
# bench/tasks/*/repo are benchmark fixtures. Most of them fail on purpose --
# that is the whole point of a task -- so they are not this repository's tests.
# bench/validate-tasks.sh is what checks them.
done < <(find . -name go.mod -not -path './.git/*' -not -path '*/node_modules/*' -not -path './bench/*' | sort)

echo "── total: $total tests across all modules ──"
exit $fail
