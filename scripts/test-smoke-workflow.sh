#!/usr/bin/env bash
# Static contract for the weekly live smoke workflow.
#
# This is the only workflow that spends real provider quota, so its contract is
# about restraint: it runs on a schedule and by hand, never on a push; it does
# nothing at all when the opt-in secret is absent; and it cannot run from a
# fork. The key it holds is the reason every action here is pinned by digest.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKFLOW="$ROOT/.github/workflows/smoke.yml"
SMOKE="$ROOT/scripts/smoke.sh"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'smoke workflow: %s\n' "$1" >&2; }

contains() {
  local text="$1" label="$2"
  if [ -f "$WORKFLOW" ] && grep -Fq -- "$text" "$WORKFLOW"; then pass; else fail "$label"; fi
}

excludes() {
  local pattern="$1" label="$2"
  if [ -f "$WORKFLOW" ] && ! grep -Eq -- "$pattern" "$WORKFLOW"; then pass; else fail "$label"; fi
}

if [ -f "$WORKFLOW" ]; then pass; else fail ".github/workflows/smoke.yml is missing"; fi
if [ -x "$SMOKE" ]; then pass; else fail "scripts/smoke.sh is missing or not executable"; fi

contains 'name: smoke' "workflow name drifted"
contains 'schedule:' "workflow does not run on a schedule"
contains 'workflow_dispatch:' "workflow cannot be started by hand"
excludes '^ *(push|pull_request):' "workflow spends provider quota on every push"

contains 'permissions: {}' "workflow does not deny permissions by default"
contains 'contents: read' "job does not declare read-only contents"
excludes 'contents: write|id-token: write' "a live-key job asks for write permission"
contains "github.repository == 'onembyte/kolkrabbi'" "workflow can run from a fork"

contains './scripts/smoke.sh --real' "workflow does not drive the real binary"
contains 'OPENROUTER_API_KEY' "workflow does not pass the opt-in key"

# The key must reach the run exactly once, through an env mapping. Every extra
# mention is another place it can be echoed into a public log.
if [ -f "$WORKFLOW" ]; then
  uses=$(grep -c 'secrets.OPENROUTER_API_KEY' "$WORKFLOW")
  if [ "$uses" = 1 ]; then pass; else fail "the secret is referenced $uses times, expected exactly 1"; fi
else
  fail "cannot count secret references: workflow is missing"
fi
excludes 'echo .*OPENROUTER_API_KEY|printf .*OPENROUTER_API_KEY' "workflow prints the key"

# The model is spelled in YAML, so nothing but this check stops it drifting away
# from the catalogue. It must be a free model the offline fallback already
# seeds: the weekly run then exercises exactly what a keyless user first meets.
if [ -f "$WORKFLOW" ]; then
  model=$(grep -oE -- '--model [^ ]+' "$WORKFLOW" | head -1 | awk '{print $2}')
  case "$model" in
    *:free) pass ;;
    "")     fail "workflow does not pin a model, so --real would bill openrouter/auto" ;;
    *)      fail "workflow model $model is not a free model" ;;
  esac
  if [ -n "$model" ] && grep -Fq "\"$model\"" "$ROOT/internal/provider/catalog.go"; then
    pass
  else
    fail "workflow model $model is not seeded in the fallback catalogue"
  fi
else
  fail "cannot check the model: workflow is missing"
  fail "cannot check the catalogue: workflow is missing"
fi

# Pinned by digest, matching the release workflow: this job holds a live key.
contains 'actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6' "checkout action is not pinned by digest"
contains 'actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6' "setup-go action is not pinned by digest"

if [ "$failures" -eq 0 ]; then
  printf 'smoke workflow: %d checks passed\n' "$checks"
else
  printf 'smoke workflow: %d of %d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
