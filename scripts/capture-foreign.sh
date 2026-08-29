#!/usr/bin/env bash
# capture-foreign.sh — capture one vendor agent-CLI stream, redact it, and
# record the argv that produced it.
#
# Fixtures under spec/testdata/foreign/ are the only offline evidence of what
# the vendors actually send. Two defects in the committed set are the reason
# this script exists rather than a note someone types afterwards:
#
#   A1  The README said claude-tool-use.ndjson was captured with
#       --allowedTools "Write". The captured tool_use block runs Bash. A
#       fixture whose provenance is misdescribed cannot anchor a regression
#       test, so argv is written verbatim into a sidecar .cmd file, by the
#       thing that ran it, at the moment it ran.
#
#   A3  The redaction replaced a newline inside tool_result.content with U+240A
#       SYMBOL FOR LINE FEED. A test asserting that would enshrine an artifact
#       of the cleaning as though it were vendor behaviour. Redaction here runs
#       through jq, which decodes and re-encodes JSON strings: control
#       characters survive as control characters. Never redact these files with
#       sed or tr — a line-oriented tool cannot see a \n inside a JSON string
#       without mangling it.
#
# The vendor runs in a scratch directory, never the repository: raw frames echo
# cwd, and a capture taken inside the checkout bakes the maintainer's path into
# the fixture before redaction gets a chance.
#
# Usage:
#   ./scripts/capture-foreign.sh <name> -- <vendor argv...>
#   ./scripts/capture-foreign.sh <name> --from-file <raw.ndjson>
#
# Examples:
#   ./scripts/capture-foreign.sh claude-isolated.ndjson -- \
#     claude -p "Reply with exactly: ok" --verbose --output-format stream-json \
#       --safe-mode --setting-sources "" --permission-mode acceptEdits
#
#   ./scripts/capture-foreign.sh codex-plain.jsonl -- \
#     codex exec --json --skip-git-repo-check "Reply with exactly: ok"
#
# --from-file re-runs redaction over an already-captured raw stream. It is how
# the redactor itself is exercised without spending a vendor turn, and how a
# capture is re-cleaned when the rules change.
#
# Nothing here is destructive: an existing fixture is refused, not overwritten.
set -uo pipefail

die() { printf 'capture-foreign: %s\n' "$1" >&2; exit 1; }

command -v jq >/dev/null 2>&1 || die "jq is required; it is what keeps control characters intact"

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd) || die "cannot locate the repository root"
dest_dir="$repo_root/spec/testdata/foreign"
[ -d "$dest_dir" ] || die "no $dest_dir — is this the kolkrabbi checkout?"

[ $# -ge 2 ] || die "usage: capture-foreign.sh <name> -- <vendor argv...>"
name=$1; shift
case "$name" in
  */*) die "name must be a bare filename, not a path: $name" ;;
  "")  die "name is empty" ;;
esac

dest="$dest_dir/$name"
cmd_file="$dest.cmd"
[ -e "$dest" ] && die "$name already exists; move it aside rather than overwriting evidence"

from_file=""
if [ "${1:-}" = "--from-file" ]; then
  shift
  [ $# -eq 1 ] || die "--from-file takes exactly one path"
  from_file=$1
  [ -r "$from_file" ] || die "cannot read $from_file"
elif [ "${1:-}" = "--" ]; then
  shift
  [ $# -ge 1 ] || die "no vendor command after --"
else
  die "expected -- or --from-file after the name"
fi

work=$(mktemp -d) || die "cannot create a scratch directory"
trap 'rm -rf "$work"' EXIT
raw="$work/raw"

if [ -n "$from_file" ]; then
  cp "$from_file" "$raw" || die "cannot copy $from_file"
  printf '# re-redacted from %s, argv unknown\n' "$from_file" > "$work/cmd"
else
  # Verbatim argv, one element per line, so an argument containing spaces or
  # an empty string (--setting-sources "") is unambiguous on the way back out.
  # This is the record A1 exists for; it is written before the run, so a
  # command that fails still leaves its provenance behind.
  : > "$work/cmd"
  for arg in "$@"; do printf '%s\n' "$arg" >> "$work/cmd"; done

  printf 'capturing in %s\n' "$work"
  ( cd "$work" && "$@" ) > "$raw" 2> "$work/stderr"
  status=$?
  printf 'exit=%d\n' "$status"
  printf '# exit=%d\n' "$status" >> "$work/cmd"
  if [ -s "$work/stderr" ]; then
    printf 'stderr (diagnostics, never the stream):\n'
    sed 's/^/  /' "$work/stderr"
  fi
  [ -s "$raw" ] || die "the vendor produced no stdout; nothing to redact"
fi

# Non-JSON lines are real vendor output on a shimmed machine (a version manager
# announcing itself). They are dropped from the fixture rather than kept, since
# a fixture is a frame stream — but they are reported, because their existence
# is a fact about the vendor that a translator has to handle.
dropped=$(grep -cv '^[[:space:]]*{' "$raw" 2>/dev/null || true)
[ "${dropped:-0}" -gt 0 ] && printf 'note: dropped %s non-JSON line(s) from the stream\n' "$dropped"

# Redaction. Every rule replaces an identifying VALUE; no rule touches a field
# name, a frame type, or the order of anything. UUIDs are mapped to stable
# fakes by order of first appearance, so a re-capture of the same shape
# produces the same fixture and the diff stays readable.
grep '^[[:space:]]*{' "$raw" | jq -s -c --arg home "$HOME" --arg work "$work" '
  def is_uuid: type == "string" and test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$");

  . as $frames
  | [ $frames | .. | select(is_uuid) ] as $found
  | ($found | unique_by(.) ) as $_unused
  | ( reduce $found[] as $u ({seen: [], map: {}};
        if (.seen | index($u)) then . else
          .map[$u] = ("00000000-0000-4000-8000-" + ((.seen | length) + 1 | tostring | ("000000000000" + .) | .[-12:]))
          | .seen += [$u]
        end) | .map ) as $uuids

  | $frames
  | map(
      walk(
        if is_uuid then $uuids[.]
        elif type == "string" then
            gsub($work; "/work")
          | gsub($home; "/home/user")
        elif type == "object" then
            ( if has("cwd") then .cwd = "/work" else . end )
          | ( if has("hook_name") then .hook_name = "example-hook" else . end )
          | ( if has("plugins") then .plugins = [{name: "example-plugin"}] else . end )
          | ( if has("mcp_servers") then .mcp_servers = [{name: "example-server", status: "connected"}] else . end )
          | ( if has("tools") and (.tools | type) == "array" then .tools = ["Bash","Read","Write"] else . end )
          | ( if has("slash_commands") and (.slash_commands | type) == "array" then .slash_commands = ["compact","clear"] else . end )
          | ( if has("agents") and (.agents | type) == "array" then .agents = ["general-purpose"] else . end )
          | ( if has("skills") and (.skills | type) == "array" then .skills = [] else . end )
          | ( if has("output") and (.output | type) == "string" then .output = "example hook output" else . end )
          | ( if has("stdout") and (.stdout | type) == "string" then .stdout = "example stdout" else . end )
          | ( if has("stderr") and (.stderr | type) == "string" then .stderr = "example stderr" else . end )
        else . end
      )
    )
  | .[]
' > "$work/redacted" || die "redaction failed"

[ -s "$work/redacted" ] || die "redaction produced nothing"

# Refuse to ship a fixture that still names this machine. Cheap, and the exact
# mistake redaction exists to prevent.
if grep -qF "$HOME" "$work/redacted" || grep -qF "$work" "$work/redacted"; then
  die "the redacted stream still contains this machine's paths; not writing it"
fi

cp "$work/redacted" "$dest" || die "cannot write $dest"
cp "$work/cmd" "$cmd_file" || die "cannot write $cmd_file"

printf '\nwrote %s (%s frames)\n' "$dest" "$(wc -l < "$dest" | tr -d ' ')"
printf 'wrote %s\n' "$cmd_file"
printf '\nRead it before committing. Redaction is mechanical; deciding what is\n'
printf 'identifying is not, and this script has never seen your account.\n'
