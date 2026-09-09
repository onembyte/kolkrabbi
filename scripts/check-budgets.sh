#!/usr/bin/env bash
# Performance budgets (docs/plan/02-architecture.md §11). These FAIL, never warn.
#
# A budget that warns is a budget that gets ignored for six months and then
# costs a rewrite.
set -euo pipefail
cd "$(dirname "$0")/.."

# Binary size is a RATCHET, not a soft line (OPTIMIZATION_PLAN.md O12). The
# 12 MB "soft budget" only ever warned, so 9 MB of growth would have been
# invisible. BIN_BASELINE is the measured stripped `make build` size; the gate
# is baseline + 10 %, so ordinary drift passes and a step change fails. Raising
# BIN_BASELINE is allowed, in a commit whose message says what the bytes bought
# and with docs/build-log.md's size map re-run. BIN_CEILING is absolute: no
# commit message buys past it.
#
# Measured 2026-09-09 on darwin/arm64, go1.26.4:
#   CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o kolk ./cmd/kolk
#   9,507,938 bytes = 9.07 MB
BIN_BASELINE=9507938             # 9.07 MB, measured 2026-09-09
BIN_HARD=$(( BIN_BASELINE * 110 / 100 ))
BIN_CEILING=$((20 * 1024 * 1024)) # 20 MB — the absolute ceiling
START_HARD_MS=30
START_SOFT_MS=20
# 90 % of the 3,575 `=== RUN` lines the root module ran on 2026-09-09. The old
# floor of 22 could not trip: it would have taken deleting 99 % of the suite.
# Bump it with each release, the same way BIN_BASELINE ratchets.
TEST_FLOOR=3217

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
printf 'ratchet: %s bytes (baseline %s + 10%%), ceiling %s bytes\n' "$BIN_HARD" "$BIN_BASELINE" "$BIN_CEILING"
if [ "$size" -gt "$BIN_CEILING" ]; then
  echo "::error::kolk exceeds the 20 MB absolute ceiling"; status=1
elif [ "$size" -gt "$BIN_HARD" ]; then
  echo "::error::kolk is $size bytes, past the ratchet of $BIN_HARD (baseline $BIN_BASELINE + 10%). Either give the bytes back, or raise BIN_BASELINE in a commit that says what they bought."; status=1
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
# One verbose run serves two budgets: the count below, and the sandbox
# wrapper's per-command overhead, which the shell package measures against the
# same 20/30 ms lines as cold start and prints as a single greppable line.
#
# `make check` and CI's budgets job hand us the log scripts/test.sh already
# wrote (KOLK_TEST_LOG), so the suite runs once per runner instead of twice
# here and twice there. The log has to be newer than go.sum, or it describes a
# tree that no longer exists; `make budgets` on its own has no log and takes
# the run itself.
if [ -n "${KOLK_TEST_LOG:-}" ] && [ -s "$KOLK_TEST_LOG" ] && [ "$KOLK_TEST_LOG" -nt go.sum ]; then
  echo "reusing $KOLK_TEST_LOG from scripts/test.sh"
  cp "$KOLK_TEST_LOG" "$out/tests.log"
else
  go test ./... -count=1 -v >"$out/tests.log" 2>/dev/null || true
fi
count="$(grep -c '^=== RUN' "$out/tests.log" || true)"
echo "root module: $count tests (floor $TEST_FLOOR)"
if [ "$count" -lt "$TEST_FLOOR" ]; then
  echo "::error::test count fell below the floor of $TEST_FLOOR (got $count)"; status=1
fi

echo "── sandbox overhead ──"
# The test itself fails past the hard line (which `make test` already
# enforces); this lifts the number into the budgets log beside cold start so
# both are read from one place, and treats a missing line as a finding rather
# than silence -- a skipped measurement on the runner that is meant to take it
# would otherwise vanish without a trace.
overhead="$(grep -o 'sandbox overhead p50: .*' "$out/tests.log" | head -1 || true)"
if [ -n "$overhead" ]; then
  echo "$overhead"
  # The test only fails at three times the hard line, because a shared runner's
  # timing is noise; this job runs where the number is trusted, so here the
  # hard line is an error and the soft line a warning, like cold start.
  ohms="$(echo "$overhead" | sed -E 's/^sandbox overhead p50: ([0-9.]+) ms.*/\1/')"
  if [ "$(awk -v m="$ohms" -v h="$START_HARD_MS" 'BEGIN{print (m>h)?1:0}')" = 1 ]; then
    echo "::error::sandbox overhead ${ohms} ms exceeds the ${START_HARD_MS} ms budget"; status=1
  elif [ "$(awk -v m="$ohms" -v s="$START_SOFT_MS" 'BEGIN{print (m>s)?1:0}')" = 1 ]; then
    echo "::warning::sandbox overhead ${ohms} ms is over the ${START_SOFT_MS} ms soft budget"
  fi
else
  echo "::error::the sandbox overhead measurement did not run on this runner"; status=1
fi

echo "── third-party modules in the root graph ──"
deps="$(go list -m all | tail -n +2 | wc -l | tr -d ' ')"
echo "root module requires: $deps"
if [ "$deps" -gt 2 ]; then
  echo "::error::more than 2 third-party modules in the root graph ($deps)"; status=1
fi

exit $status
