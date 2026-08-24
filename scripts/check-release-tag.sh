#!/usr/bin/env bash
# Reject anything that is not a v-prefixed Semantic Version before publishing.
set -euo pipefail

tag="${1:-${GITHUB_REF_NAME:-}}"
pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?(\+([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$'

if [[ ! "$tag" =~ $pattern ]]; then
  printf 'release tag must be v-prefixed SemVer (example: v1.1.0); got %q\n' "$tag" >&2
  exit 2
fi

prerelease="${BASH_REMATCH[5]:-}"
if [ -n "$prerelease" ]; then
  IFS='.' read -r -a identifiers <<<"$prerelease"
  for identifier in "${identifiers[@]}"; do
    if [[ "$identifier" =~ ^0[0-9]+$ ]]; then
      printf 'numeric prerelease identifiers cannot have leading zeroes; got %q\n' "$tag" >&2
      exit 2
    fi
  done
fi

printf 'release tag: %s\n' "$tag"
