#!/usr/bin/env bash
# A task is only worth running if its pass condition discriminates. For each
# task this checks two things in a throwaway copy of the fixture:
#
#   1. verify.sh FAILS on the untouched fixture   (else the task starts solved)
#   2. verify.sh PASSES after oracle.sh is applied (else the task is unsolvable)
#
# Run this before tagging the task set. A task that fails either check is a bug
# in the benchmark, not a hard task.
set -uo pipefail
BENCH="$(cd "$(dirname "$0")" && pwd)"
fail=0; n=0

for tdir in "$BENCH"/tasks/*/; do
  task="$(basename "$tdir")"
  [ -f "$tdir/oracle.sh" ] || { printf '  %-22s SKIP (no oracle)\n' "$task"; continue; }
  n=$((n + 1))

  before="$(mktemp -d)"; cp -R "$tdir/repo/." "$before/"
  ( cd "$before" && bash "$tdir/verify.sh" ) >/dev/null 2>&1
  unsolved=$?
  rm -rf "$before"

  after="$(mktemp -d)"; cp -R "$tdir/repo/." "$after/"
  ( cd "$after" && bash "$tdir/oracle.sh" ) >/dev/null 2>&1
  orc=$?
  ( cd "$after" && bash "$tdir/verify.sh" ) >/dev/null 2>&1
  solved=$?
  rm -rf "$after"

  if [ "$unsolved" = 0 ]; then
    printf '  %-22s BROKEN  verify.sh passes on the untouched fixture\n' "$task"; fail=1
  elif [ "$orc" != 0 ]; then
    printf '  %-22s BROKEN  oracle.sh itself failed (exit %s)\n' "$task" "$orc"; fail=1
  elif [ "$solved" != 0 ]; then
    printf '  %-22s BROKEN  verify.sh still fails after the oracle fix\n' "$task"; fail=1
  else
    printf '  %-22s ok      fails before, passes after\n' "$task"
  fi
done

echo
if [ "$fail" != 0 ]; then echo "  task validation FAILED"; exit 1; fi
echo "  $n tasks validated"
