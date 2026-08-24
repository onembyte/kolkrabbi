#!/usr/bin/env bash
# Black-box and workflow contract for the protocol changelog guard.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GUARD="$ROOT/scripts/check-spec-change.sh"
WORKFLOW="$ROOT/.github/workflows/spec.yml"
MAKEFILE="$ROOT/Makefile"
CI="$ROOT/.github/workflows/ci.yml"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'spec guard: %s\n' "$1" >&2; }

contains() {
  local file="$1" text="$2" label="$3"
  if [ -f "$file" ] && grep -Fq -- "$text" "$file"; then pass; else fail "$label"; fi
}

excludes() {
  local file="$1" pattern="$2" label="$3"
  if [ -f "$file" ] && ! grep -Eq -- "$pattern" "$file"; then pass; else fail "$label"; fi
}

if [ -f "$GUARD" ]; then pass; else fail "scripts/check-spec-change.sh is missing"; fi
if [ -f "$WORKFLOW" ]; then pass; else fail ".github/workflows/spec.yml is missing"; fi
contains "$MAKEFILE" 'spec: ## language-neutral protocol contract and changelog guard tests' \
  "Makefile has no named spec target"
contains "$MAKEFILE" './scripts/test-spec-change.sh' "make spec does not run the guard matrix"
contains "$CI" 'run: make spec' "ordinary CI does not run the named spec gate"
contains "$WORKFLOW" 'name: spec' "spec workflow name drifted"
contains "$WORKFLOW" 'paths:' "spec workflow is not path-filtered"
contains "$WORKFLOW" '- "spec/**"' "spec workflow does not watch spec/**"
contains "$WORKFLOW" 'fetch-depth: 0' "spec workflow cannot compare full Git history"
contains "$WORKFLOW" 'persist-credentials: false' "spec workflow retains checkout credentials"
contains "$WORKFLOW" 'run: make spec' "spec workflow does not run the named spec gate"
dollar='$'
contains "$WORKFLOW" "./scripts/check-spec-change.sh \"${dollar}SPEC_BASE\" \"${dollar}GITHUB_SHA\"" \
  "spec workflow does not compare its event base to the checked-out head"
contains "$WORKFLOW" 'actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6' \
  "spec workflow checkout action is not pinned"
contains "$WORKFLOW" 'actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6' \
  "spec workflow setup-go action is not pinned"
contains "$WORKFLOW" 'contents: read' "spec workflow lacks read-only contents permission"
excludes "$WORKFLOW" 'contents: write|id-token: write|pull-requests: write' \
  "spec workflow requests write permission"

run_guard() {
  local want_status="$1" want_text="$2" label="$3" repo="$4" base="$5" head="$6"
  local output status
  output="$(KOLK_SPEC_REPO="$repo" bash "$GUARD" "$base" "$head" 2>&1)"
  status=$?
  if [ "$status" -ne "$want_status" ]; then
    fail "$label returned $status, want $want_status: $output"
    return
  fi
  if [[ "$output" != *"$want_text"* ]]; then
    fail "$label output %q does not contain $want_text"
    return
  fi
  pass
}

if [ -f "$GUARD" ]; then
  if bash -n "$GUARD"; then pass; else fail "spec changelog guard fails bash -n"; fi

  scratch="$(mktemp -d "${TMPDIR:-/tmp}/kolk-spec-guard.XXXXXX")"
  trap 'rm -rf "$scratch"' EXIT
  repo="$scratch/repo"
  mkdir -p "$repo/spec" "$repo/docs"
  git -C "$repo" init -q
  git -C "$repo" config user.name "Kolk Spec Test"
  git -C "$repo" config user.email "spec-test@invalid.example"
  printf '# contract changes\n' >"$repo/spec/CHANGELOG.md"
  printf '0\n' >"$repo/spec/VERSION"
  git -C "$repo" add spec
  git -C "$repo" commit -qm initial
  initial="$(git -C "$repo" rev-parse HEAD)"

  run_guard 0 'no contract changes' "equal trees" "$repo" "$initial" "$initial"

  printf 'outside contract\n' >"$repo/docs/note.md"
  git -C "$repo" add docs/note.md
  git -C "$repo" commit -qm docs
  docs_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 0 'no contract changes' "non-spec change" "$repo" "$initial" "$docs_head"

  printf '1\n' >"$repo/spec/VERSION"
  git -C "$repo" add spec/VERSION
  git -C "$repo" commit -qm 'change version without changelog'
  modified_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 1 'without spec/CHANGELOG.md' "modified spec without changelog" \
    "$repo" "$docs_head" "$modified_head"

  printf '%s\n' '- change version' >>"$repo/spec/CHANGELOG.md"
  git -C "$repo" add spec/CHANGELOG.md
  git -C "$repo" commit -qm 'document version change'
  documented_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 0 'changelog present' "modified spec with changelog" \
    "$repo" "$docs_head" "$documented_head"

  printf '{}\n' >"$repo/spec/added.json"
  git -C "$repo" add spec/added.json
  git -C "$repo" commit -qm 'add contract without changelog'
  added_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 1 'without spec/CHANGELOG.md' "added spec without changelog" \
    "$repo" "$documented_head" "$added_head"

  printf '%s\n' '- add contract' >>"$repo/spec/CHANGELOG.md"
  git -C "$repo" add spec/CHANGELOG.md
  git -C "$repo" commit -qm 'document added contract'
  added_documented_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 0 'changelog present' "added spec with changelog" \
    "$repo" "$documented_head" "$added_documented_head"

  git -C "$repo" rm -q spec/added.json
  git -C "$repo" commit -qm 'delete contract without changelog'
  deleted_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 1 'without spec/CHANGELOG.md' "deleted spec without changelog" \
    "$repo" "$added_documented_head" "$deleted_head"

  printf '%s\n' '- remove contract' >>"$repo/spec/CHANGELOG.md"
  git -C "$repo" add spec/CHANGELOG.md
  git -C "$repo" commit -qm 'document deleted contract'
  deleted_documented_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 0 'changelog present' "deleted spec with changelog" \
    "$repo" "$added_documented_head" "$deleted_documented_head"

  printf '%s\n' '- notes only' >>"$repo/spec/CHANGELOG.md"
  git -C "$repo" add spec/CHANGELOG.md
  git -C "$repo" commit -qm 'changelog only'
  changelog_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 0 'changelog present' "changelog-only change" \
    "$repo" "$deleted_documented_head" "$changelog_head"

  printf 'dirty but uncommitted\n' >>"$repo/spec/VERSION"
  run_guard 0 'no contract changes' "working-tree noise" "$repo" "$changelog_head" "$changelog_head"

  git -C "$repo" rm -q spec/CHANGELOG.md
  git -C "$repo" add spec/VERSION
  git -C "$repo" commit -qm 'remove required changelog'
  missing_head="$(git -C "$repo" rev-parse HEAD)"
  run_guard 1 'required spec/CHANGELOG.md is missing' "missing changelog" \
    "$repo" "$changelog_head" "$missing_head"
  run_guard 2 'invalid base treeish' "invalid base" "$repo" does-not-exist "$missing_head"
fi

if [ "$failures" -ne 0 ]; then
  printf 'spec guard: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'spec guard: %d checks passed\n' "$checks"
