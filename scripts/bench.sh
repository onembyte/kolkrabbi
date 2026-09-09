#!/usr/bin/env bash
# The O0 baseline of OPTIMIZATION_PLAN.md: the five benchmarks every later
# leaf quotes a before/after pair from.
#
# The first run writes bench/baseline.txt and stops. Every later run writes
# bench/current.txt and, if benchstat happens to be installed, prints the
# delta. benchstat is NOT a dependency -- this repo has two and adds none --
# so without it the two files are left side by side to read or diff by hand.
#
# Rerun a baseline deliberately (after a machine change, or when a leaf is
# accepted and its numbers become the new floor): BENCH_BASELINE=1 scripts/bench.sh
set -euo pipefail
cd "$(dirname "$0")/.."

BENCHTIME="${BENCHTIME:-1s}"
COUNT="${BENCH_COUNT:-3}"
out="bench/baseline.txt"
mode="baseline"
if [ -f "$out" ] && [ "${BENCH_BASELINE:-0}" != "1" ]; then
  out="bench/current.txt"
  mode="current"
fi

# One line per package: the -bench regex is exact so a slow exploratory
# benchmark added later (BenchmarkPublishTurn) does not silently join the
# baseline and change what the numbers mean.
targets=(
  "./internal/bus|^BenchmarkPublish$"
  "./internal/provider|^BenchmarkReadStream$"
  "./internal/session|^(BenchmarkSave|BenchmarkLatestForDir)$"
  "./internal/stats|^BenchmarkRatingsByModel$"
)

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

{
  echo "# kolkrabbi O0 $mode — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# commit: $(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)$( [ -n "$(git status --porcelain 2>/dev/null)" ] && echo '-dirty')"
  echo "# go: $(go version)"
  echo "# flags: -count=$COUNT -benchmem -benchtime=$BENCHTIME"
  echo
} >"$tmp"

for target in "${targets[@]}"; do
  pkg="${target%%|*}"
  pattern="${target#*|}" # shortest match: the pattern itself contains |
  echo "── ${pkg#./} ${pattern} ──" >&2
  go test -run '^$' -bench "$pattern" -benchmem -count="$COUNT" -benchtime="$BENCHTIME" "$pkg" | tee -a "$tmp"
done

mkdir -p bench
mv "$tmp" "$out"
trap - EXIT
echo "── wrote $out ──" >&2

if [ "$mode" = "current" ]; then
  if command -v benchstat >/dev/null 2>&1; then
    echo "── benchstat bench/baseline.txt → bench/current.txt ──" >&2
    benchstat bench/baseline.txt bench/current.txt
  else
    echo "benchstat is not installed (it is not a dependency); compare bench/baseline.txt and bench/current.txt by hand" >&2
  fi
fi
