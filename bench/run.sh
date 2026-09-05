#!/usr/bin/env bash
# KolkBench runner. One task, one harness, N runs, one JSONL line per run.
#
#   bench/run.sh --harness kolk --task 00-smoke --runs 1 \
#                --base-url http://127.0.0.1:11434/v1 --model qwen2.5-coder:14b
#
# Every run gets a fresh copy of the fixture in a temporary directory, so a task
# can never see what a previous run did. Nothing is scored by reading output:
# verify.sh's exit code is the result.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BENCH="$ROOT/bench"

HARNESS=""; TASK=""; RUNS=5; MODEL=""; BASE_URL=""; OUT="$BENCH/results"; KEEP=0
while [ $# -gt 0 ]; do
  case "$1" in
    --harness) HARNESS="$2"; shift 2 ;;
    --task)    TASK="$2";    shift 2 ;;
    --runs)    RUNS="$2";    shift 2 ;;
    --model)   MODEL="$2";   shift 2 ;;
    --base-url) BASE_URL="$2"; shift 2 ;;
    --out)     OUT="$2";     shift 2 ;;
    --keep)    KEEP=1;       shift ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done
[ -n "$HARNESS" ] || { echo "--harness is required" >&2; exit 2; }
[ -n "$TASK" ] || { echo "--task is required (or 'all')" >&2; exit 2; }

command -v git >/dev/null || { echo "git is required" >&2; exit 2; }

# macOS ships no coreutils timeout. Run in the background, poll, kill the group.
run_with_timeout() {
  local secs="$1" logfile="$2"; shift 2
  ( "$@" >"$logfile" 2>&1 ) &
  local pid=$! waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$waited" -ge "$secs" ]; then
      kill -TERM "$pid" 2>/dev/null
      sleep 2
      kill -KILL "$pid" 2>/dev/null
      wait "$pid" 2>/dev/null
      return 124
    fi
    sleep 1
    waited=$((waited + 1))
  done
  wait "$pid"
}

# Each harness is one function so adding a fourth is a small, visible change.
# The command lines for codex and opencode are marked unverified until D4.4
# actually runs them; they are written from published documentation, not from
# a run, and this file should be corrected the first time they are exercised.
invoke_kolk() {
  local dir="$1" prompt="$2" log="$3" secs="$4"
  run_with_timeout "$secs" "$log" env -C "$dir" \
    "$ROOT/kolk" --base-url "$BASE_URL" -m "$MODEL" -P full-auto -p "$prompt"
}
invoke_codex() {   # UNVERIFIED until D4.4
  local dir="$1" prompt="$2" log="$3" secs="$4"
  run_with_timeout "$secs" "$log" env -C "$dir" \
    codex exec --skip-git-repo-check "$prompt"
}
invoke_opencode() { # UNVERIFIED until D4.4
  local dir="$1" prompt="$2" log="$3" secs="$4"
  run_with_timeout "$secs" "$log" env -C "$dir" \
    opencode run "$prompt"
}

harness_version() {
  case "$HARNESS" in
    kolk) "$ROOT/kolk" help 2>/dev/null | grep -o 'kolk v[^ ]*' | head -1 ;;
    codex) codex --version 2>/dev/null | head -1 ;;
    opencode) opencode --version 2>/dev/null | head -1 ;;
  esac
}

json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))' ; }

task_list() {
  if [ "$TASK" = "all" ]; then
    find "$BENCH/tasks" -mindepth 1 -maxdepth 1 -type d | sort | while read -r d; do basename "$d"; done
  else
    echo "$TASK"
  fi
}

mkdir -p "$OUT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
JSONL="$OUT/${STAMP}-${HARNESS}.jsonl"
VERSION="$(harness_version)"
total=0; passed=0

for task in $(task_list); do
  tdir="$BENCH/tasks/$task"
  [ -d "$tdir" ] || { echo "no such task: $task" >&2; exit 2; }
  prompt="$(grep -m1 '^prompt' "$tdir/task.toml" | sed -E 's/^prompt[[:space:]]*=[[:space:]]*"(.*)"$/\1/')"
  secs="$(grep -m1 '^timeout_seconds' "$tdir/task.toml" | sed -E 's/[^0-9]//g')"
  [ -n "$secs" ] || secs=300

  for run in $(seq 1 "$RUNS"); do
    work="$(mktemp -d)"
    cp -R "$tdir/repo/." "$work/"
    ( cd "$work" && git init -q && git add -A && \
      git -c user.name=kolkbench -c user.email=bench@invalid.example commit -qm baseline )

    log="$OUT/${STAMP}-${HARNESS}-${task}-run${run}.log"
    start=$(date +%s)
    "invoke_$HARNESS" "$work" "$prompt" "$log" "$secs"
    rc=$?
    end=$(date +%s)
    timed_out=false; [ "$rc" = 124 ] && timed_out=true

    ( cd "$work" && git add -A ) >/dev/null 2>&1
    numstat="$(cd "$work" && git diff --cached --numstat | awk '{a+=$1; d+=$2; n++} END {printf "%d %d %d", n+0, a+0, d+0}')"
    files=$(echo "$numstat" | cut -d' ' -f1); ins=$(echo "$numstat" | cut -d' ' -f2); dels=$(echo "$numstat" | cut -d' ' -f3)

    ( cd "$work" && bash "$tdir/verify.sh" ) >"$log.verify" 2>&1
    vrc=$?
    ok=false; [ "$vrc" = 0 ] && { ok=true; passed=$((passed+1)); }
    total=$((total+1))

    printf '{"task":"%s","harness":"%s","harness_version":%s,"model":"%s","run":%d,"passed":%s,"verify_exit":%d,"harness_exit":%d,"timed_out":%s,"wall_seconds":%d,"files_changed":%d,"insertions":%d,"deletions":%d,"transcript":"%s"}\n' \
      "$task" "$HARNESS" "$(printf '%s' "$VERSION" | json_escape)" "$MODEL" "$run" "$ok" "$vrc" "$rc" "$timed_out" \
      "$((end-start))" "$files" "$ins" "$dels" "$(basename "$log")" >>"$JSONL"

    printf '  %-14s %-9s run %s/%s  %s  %ss  %s files\n' \
      "$task" "$HARNESS" "$run" "$RUNS" "$([ "$ok" = true ] && echo PASS || echo fail)" "$((end-start))" "$files"

    if [ "$KEEP" = 1 ]; then echo "    workdir kept: $work"; else rm -rf "$work"; fi
  done
done

echo
echo "  $passed/$total passed  →  $JSONL"
