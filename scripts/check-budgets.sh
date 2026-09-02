#!/usr/bin/env bash
# Performance budgets (docs/plan/02-architecture.md §11). These FAIL, never warn.
#
# A budget that warns is a budget that gets ignored for six months and then
# costs a rewrite.
set -euo pipefail
cd "$(dirname "$0")/.."

BIN_HARD=$((20 * 1024 * 1024)) # 20 MB
BIN_SOFT=$((12 * 1024 * 1024)) # 12 MB — warn only
START_HARD_MS=30
START_SOFT_MS=20
TEST_FLOOR=22

out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
status=0

filesize() { # portable stat
  if stat -f%z "$1" >/dev/null 2>&1; then stat -f%z "$1"; else stat -c%s "$1"; fi
}

echo "── binary size ──"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$out/kolk" ./cmd/kolk
size="$(filesize "$out/kolk")"
printf 'kolk: %s bytes (%.2f MB)\n' "$size" "$(echo "$size" | awk '{print $1/1048576}')"
if [ "$size" -gt "$BIN_HARD" ]; then
  echo "::error::kolk exceeds the 20 MB hard budget"; status=1
elif [ "$size" -gt "$BIN_SOFT" ]; then
  echo "::warning::kolk is over the 12 MB soft budget"
fi

echo "── cold start ──"
# `kolk help` reads nothing and opens no session, so this measures process
# startup rather than anything the command chose to do.
#
# It used to be `kolk version`, which stopped being a command on 2026-09-02.
# Dispatch turns an unknown word into a prompt, so this loop quietly started
# sending twenty turns to a real provider per run — the budget's own timing is
# what caught it. Measure a command that exists, and one from the closed
# outside-session set so it cannot quietly stop existing again.
"$out/kolk" help >/dev/null
start="$(date +%s000000000 2>/dev/null || true)"
if command -v python3 >/dev/null; then
  ms="$(python3 - "$out/kolk" <<'PY'
import subprocess, sys, time
b = sys.argv[1]
runs = []
for _ in range(20):
    t = time.perf_counter()
    subprocess.run([b, "help"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    runs.append((time.perf_counter() - t) * 1000)
runs.sort()
print(f"{runs[len(runs)//2]:.1f}")
PY
)"
  echo "cold start p50: ${ms} ms (soft ${START_SOFT_MS}, hard ${START_HARD_MS})"
  over_hard="$(awk -v m="$ms" -v h="$START_HARD_MS" 'BEGIN{print (m>h)?1:0}')"
  over_soft="$(awk -v m="$ms" -v s="$START_SOFT_MS" 'BEGIN{print (m>s)?1:0}')"
  if [ "$over_hard" = 1 ]; then
    echo "::error::cold start ${ms} ms exceeds the ${START_HARD_MS} ms budget"; status=1
  elif [ "$over_soft" = 1 ]; then
    echo "::warning::cold start ${ms} ms is over the ${START_SOFT_MS} ms soft budget"
  fi
else
  echo "python3 not found; skipping the cold-start measurement"
fi

echo "── test-count floor ──"
count="$(go test ./... -count=1 -v 2>/dev/null | grep -c '^=== RUN' || true)"
echo "root module: $count tests (floor $TEST_FLOOR)"
if [ "$count" -lt "$TEST_FLOOR" ]; then
  echo "::error::test count fell below the floor of $TEST_FLOOR (got $count)"; status=1
fi

echo "── third-party modules in the root graph ──"
deps="$(go list -m all | tail -n +2 | wc -l | tr -d ' ')"
echo "root module requires: $deps"
if [ "$deps" -gt 2 ]; then
  echo "::error::more than 2 third-party modules in the root graph ($deps)"; status=1
fi

exit $status
