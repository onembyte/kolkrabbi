#!/usr/bin/env bash
# Require the protocol changelog whenever a committed spec/ tree changes.
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  printf 'usage: %s <base-treeish> [head-treeish]\n' "${0##*/}" >&2
  exit 2
fi

repo="${KOLK_SPEC_REPO:-$(cd "$(dirname "$0")/.." && pwd)}"
base="$1"
head="${2:-HEAD}"

if ! git -C "$repo" rev-parse --verify --quiet "${base}^{tree}" >/dev/null; then
  printf 'spec change guard: invalid base treeish %q\n' "$base" >&2
  exit 2
fi
if ! git -C "$repo" rev-parse --verify --quiet "${head}^{tree}" >/dev/null; then
  printf 'spec change guard: invalid head treeish %q\n' "$head" >&2
  exit 2
fi
if [ "$(git -C "$repo" cat-file -t "${head}:spec/CHANGELOG.md" 2>/dev/null || true)" != "blob" ]; then
  printf 'spec change guard: required spec/CHANGELOG.md is missing from %q\n' "$head" >&2
  exit 1
fi

contract_status=0
git -C "$repo" diff --quiet "$base" "$head" -- spec || contract_status=$?
case "$contract_status" in
  0)
    printf 'spec change guard: no contract changes\n'
    exit 0
    ;;
  1) ;;
  *)
    printf 'spec change guard: unable to compare contract trees\n' >&2
    exit 2
    ;;
esac

changelog_status=0
git -C "$repo" diff --quiet "$base" "$head" -- spec/CHANGELOG.md || changelog_status=$?
case "$changelog_status" in
  1)
    printf 'spec change guard: changelog present\n'
    ;;
  0)
    printf 'spec change guard: spec/ changed without spec/CHANGELOG.md\n' >&2
    exit 1
    ;;
  *)
    printf 'spec change guard: unable to compare protocol changelog\n' >&2
    exit 2
    ;;
esac
