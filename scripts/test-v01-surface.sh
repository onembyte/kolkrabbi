#!/usr/bin/env bash
# Public mode contract for the first working deploy.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
README="$ROOT/README.md"
CLI="$ROOT/internal/cli"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'mode surface: %s\n' "$1" >&2; }

contains() {
  local file="$1" text="$2" label="$3"
  if grep -Fq -- "$text" "$file"; then pass; else fail "$label"; fi
}

excludes() {
  local file="$1" pattern="$2" label="$3"
  if ! grep -Eiq -- "$pattern" "$file"; then pass; else fail "$label"; fi
}

contains "$README" '## The three modes' "README does not define the three-mode release"
contains "$README" 'kolk                          # interactive, code mode' "README does not state that code is the default"
contains "$README" '/mode agent' "README does not document agent mode"
contains "$README" '/auto-approve [on|off]' "README does not document the explicit auto-approve command"
excludes "$README" 'parallel subagents|subagents in parallel|at once' "README inaccurately claims parallel orchestration"
contains "$CLI/flags.go" '<chat|code|agent>' "CLI mode flag does not name exactly chat, code, and agent"
contains "$CLI/flags.go" 'agent = orchestrated' "CLI mode flag does not explain agent mode"
contains "$CLI/cli.go" 'kolk — chat / code / agent in one CLI' "top-level help does not name the three-mode release"
contains "$CLI/cli.go" 'In agent mode, effort also scales orchestration width.' "top-level help does not explain agent effort"
contains "$CLI/slash.go" '/auto-approve [on|off]' "in-session help does not list explicit auto-approval"
contains "$CLI/cli.go" '{"update", "", "install the latest verified release"' "top-level update command is missing"
contains "$CLI/slash.go" '/update        install the latest verified release' "in-session update command is missing"
contains "$ROOT/internal/engine/agent.go" 'var Modes = []string{ModeChat, ModeCode, ModeAgent}' "engine registry does not expose exactly three modes"

if [ "$failures" -ne 0 ]; then
  printf 'mode surface: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'mode surface: %d checks passed\n' "$checks"
