#!/usr/bin/env bash
# The plan is a claim about the code, and this checks the claim's own bookkeeping.
#
# PLAN.md's tick marks and docs/plan/ are two records of the same fact, and this
# repository has already been bitten by them disagreeing: an audit on 2026-08-27
# found the phase table four phases out of date, still calling built phases
# "queued". A tick that has no document, or a document that says "hardened" for
# an item nobody ticked, is the same class of rot one step earlier.
#
#   [x] item  ⇔  docs/plan/NN-*.md exists AND says "Status: hardened"
#   [~] item  ⇒  if it has a doc, that doc does not claim to be hardened
#   every doc ⇒  an item with that number exists
#   every ticked item's link  ⇒  resolves to a file that is there
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PLAN="$ROOT/PLAN.md"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'plan: %s\n' "$1" >&2; }

doc_for() { # item number, with or without a leading zero
  local n="$1"
  ls "$ROOT/docs/plan/$n-"*.md "$ROOT/docs/plan/0$n-"*.md 2>/dev/null | head -1
}

if [ -f "$PLAN" ]; then pass; else fail "PLAN.md is missing"; exit 1; fi

# Every marked item, against its document.
while read -r mark number; do
  doc="$(doc_for "$number")"
  case "$mark" in
    x)
      if [ -z "$doc" ]; then
        fail "item $number is ticked but has no docs/plan/ document"
        continue
      fi
      if head -5 "$doc" | grep -q '^Status: hardened'; then pass; else
        fail "item $number is ticked but $(basename "$doc") does not say 'Status: hardened'"
      fi
      ;;
    '~')
      # A part-done item is not required to have a document: item 1 records its
      # decisions inline in PLAN.md and predates the docs/plan convention. What
      # it may not do is have a document that claims to be finished.
      if [ -n "$doc" ] && head -5 "$doc" | grep -q '^Status: hardened'; then
        fail "item $number is marked part-done but $(basename "$doc") claims to be hardened"
      else
        pass
      fi
      ;;
  esac
done < <(grep -oE '^### \[[x~ ]\] [0-9]+\.' "$PLAN" | sed -E 's/^### \[(.)\] ([0-9]+)\./\1 \2/' | grep -v '^ ')

# Every document, against the plan.
for doc in "$ROOT"/docs/plan/[0-9]*.md; do
  number="$(basename "$doc" | cut -d- -f1 | sed 's/^0*//')"
  if grep -qE "^### \[[x~ ]\] $number\." "$PLAN"; then pass; else
    fail "$(basename "$doc") documents item $number, which is not in PLAN.md"
  fi
done

# Every link a ticked item makes to its own document has to resolve. A dead link
# in the one line that says "this is decided" is worse than no link.
while read -r target; do
  if [ -f "$ROOT/$target" ]; then pass; else fail "PLAN.md links to $target, which does not exist"; fi
done < <(grep -oE '\(docs/plan/[0-9][^)]*\.md\)' "$PLAN" | tr -d '()' | sort -u)

if [ "$failures" -eq 0 ]; then
  printf 'plan: %d checks passed\n' "$checks"
else
  printf 'plan: %d of %d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
