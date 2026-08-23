#!/usr/bin/env bash
# Public product contract for the first working deploy.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
README="$ROOT/README.md"
CLI="$ROOT/internal/cli"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'v0.1 surface: %s\n' "$1" >&2; }

contains() {
  local file="$1" text="$2" label="$3"
  if grep -Fq -- "$text" "$file"; then pass; else fail "$label"; fi
}

excludes() {
  local file="$1" pattern="$2" label="$3"
  if ! grep -Eiq -- "$pattern" "$file"; then pass; else fail "$label"; fi
}

contains "$README" '## The two modes' "README does not define the two-mode release"
contains "$README" 'kolk                          # interactive, code mode' "README does not state that code is the default"
excludes "$README" 'three modes|/mode agent|--mode agent|agent mode|orchestrated agents' "README advertises the unreleased agent mode"
contains "$CLI/flags.go" '<chat|code>' "CLI mode flag does not name exactly chat and code"
excludes "$CLI/flags.go" 'chat\|code\|agent|agent = orchestrated' "CLI mode flag advertises the unreleased agent mode"
contains "$CLI/cli.go" 'kolk — chat / code in one CLI' "top-level help does not name the two-mode release"
excludes "$CLI/cli.go" 'chat / code / agent|In agent mode' "top-level help advertises the unreleased agent mode"

if [ "$failures" -ne 0 ]; then
  printf 'v0.1 surface: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'v0.1 surface: %d checks passed\n' "$checks"
