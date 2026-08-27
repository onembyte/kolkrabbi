#!/usr/bin/env bash
# Every action every workflow runs must be pinned by commit SHA.
#
# A tag is a moving pointer that the action's owner can repoint at any time, so
# `@v5` means "whatever that account publishes next" — which is a credential
# decision, not a version one. These jobs check out the repository, and one of
# them (smoke) holds a live API key.
#
# The rule is mechanical: a 40-character hex SHA, followed by a comment naming
# the human-readable version, so a reader can see what the digest is supposed to
# be and `gh api repos/<action>/git/ref/tags/<tag>` can confirm it.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'workflow pins: %s\n' "$1" >&2; }

shopt -s nullglob
workflows=("$ROOT"/.github/workflows/*.yml)
if [ "${#workflows[@]}" -gt 0 ]; then pass; else fail "no workflows found — the extraction broke, not the pins"; fi

for workflow in "${workflows[@]}"; do
  name="$(basename "$workflow")"
  while IFS= read -r line; do
    ref="${line#*uses: }"
    ref="${ref%% *}"
    case "$ref" in
      ./*|docker://*) pass; continue ;;  # a local action is this repository
    esac
    digest="${ref##*@}"
    if [ ${#digest} -eq 40 ] && [[ "$digest" =~ ^[0-9a-f]{40}$ ]]; then pass; else
      fail "$name uses $ref, which is a moving tag rather than a commit SHA"
      continue
    fi
    if printf '%s' "$line" | grep -q '# v'; then pass; else
      fail "$name pins $ref with no '# vN' comment saying which version it is"
    fi
  done < <(grep -E '^\s*(-\s*)?uses:' "$workflow")
done

if [ "$failures" -eq 0 ]; then
  printf 'workflow pins: %d checks passed\n' "$checks"
else
  printf 'workflow pins: %d of %d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
