# spec/testdata/foreign — real vendor agent-CLI output

Input fixtures for the `internal/provider/agentcli` adapters (PLAN.md item 4), which spawn the
user's **own, already-logged-in** vendor binary. They exist so `translate_test.go` can replay real
vendor output **offline, forever, with no vendor binary and no account** — see
`docs/plan/02-architecture.md` §2 and §6.

> These vendor-native fixtures predate migration step 6 because capturing them requires a logged-in
> binary that happened to be available. A6.1 has since added the Kolkrabbi envelope foundation;
> these files remain inert adapter inputs and are deliberately not Kolkrabbi protocol frames.

## What is here

| File | Captured with | Shape it proves |
|---|---|---|
| `claude-plain.ndjson` | `claude -p "Reply with exactly: ok" --output-format stream-json --verbose` | 12 frames: 4×`hook_started`, 4×`hook_response`, `system/init`, `assistant`(text), `rate_limit_event`, `result/success` |
| `claude-tool-use.ndjson` | same, plus `--allowedTools "Write"`, prompting a file write | 14 frames: the full tool round trip — `assistant`(`tool_use`) → `user`(`tool_result`) → `assistant`(text) → `result` |
| `codex-plain.jsonl` | `codex exec --json --skip-git-repo-check "Reply with exactly: ok"` | 4 frames: `thread.started`(handle) → `turn.started` → `item.completed`(`agent_message`) → `turn.completed`(`usage`) |
| `codex-tool-use.jsonl` | same, with `-s workspace-write`, prompting a file write | 12 frames: interleaved `item.started`/`item.completed` for `file_change` and `command_execution` (with `aggregated_output`, `exit_code`) and `agent_message` prose between them |
| `codex-error.jsonl` | `codex exec --json --skip-git-repo-check -m gpt-4.1 "…"` | 5 frames: exit 1 — an `item.completed`(`type:"error"`) warning, then top-level `error` and `turn.failed` whose `message` is a **JSON-encoded string** carrying OpenAI's real error |

Captured 2026-08-22 with **Claude Code 2.1.240**, `apiKeySource: "none"` (i.e. subscription login,
which is the mode kolk's adapter targets), model `claude-opus-5`. The codex fixtures were captured
2026-08-28 with **codex-cli 0.149.1**, `codex login status` → `Logged in using ChatGPT`.

Three codex facts verified on the capturing machine that the shape tables alone do not carry:

1. **stdout is not pure JSONL.** A tool-shim line (`mise …`) precedes the first frame when the
   binary is reached through a version-manager shim, and `codex exec` also prints prose to stderr
   ("Reading additional input from stdin…"). A codex translator **skips lines that do not parse as
   JSON objects** and treats stderr as diagnostics, never as the stream.
2. **The plan catalog's codex rows were stale.** `gpt-4.1` errors with "Model metadata for `gpt-4.1`
   not found" and this account (ChatGPT) refuses it outright; the current ids are `gpt-5.6-*`
   (`sol`/`luna`/`terra`/`pro`) and `gpt-5.3-codex`. `model_reasoning_effort` accepts
   `minimal|low|medium|high|xhigh` via `-c` (verified with `gpt-5.6-sol` + `xhigh`).
3. **`codex exec resume <thread_id>`** re-opens the thread verbatim (`thread.started` echoes the
   same id, exit 0) — that is the resume handle, the codex analogue of Claude's `--resume`.

## What the adapter must learn from these

1. **Frames arrive before `system/init`.** Hook frames precede it when the user has `SessionStart`
   hooks. An adapter that assumes `init` is frame #1 breaks on a real machine.
2. **`rate_limit_event` exists** and is neither an error nor content — it must be tolerated.
3. **`result` carries the accounting kolk's dashboard needs** (item 17): `total_cost_usd`, `usage`,
   `modelUsage`, `ttft_ms`, `duration_api_ms`, `duration_ms`, `num_turns`, `stop_reason`,
   `permission_denials`, `session_id`. Per the vendor docs these costs are **client-side estimates**,
   so record them as `cost_source: "vendor_estimate"`, never as authoritative billing.
4. **Tool results come back as a `user` frame** whose content is a `tool_result` block — the vendor
   runs its own tools. kolk does not execute them and must not try to.
5. **`system/init` advertises capability** (`tools`, `slash_commands`, `agents`, `skills`, `model`,
   `permissionMode`, `apiKeySource`) — this is the adapter's capability probe, so no guessing.

## Redaction

Captured in a scratch directory, then redacted — these are committed to a repo, and raw frames leak
the capturing machine. Replaced: `cwd`→`/work`; every UUID→stable fakes; `$HOME`→`/home/user`;
`plugins`/`mcp_servers`→generic two-entry examples; `hook_name` and all hook `output`/`stdout`/
`stderr`→placeholders; and `tools`/`slash_commands`/`agents`/`skills` →a generic built-in subset
(the real arrays enumerate everything the user has installed, including private project agents).
**Frame shape, ordering and every field name are untouched** — only identifying values changed.

Re-capture with `scripts/capture-foreign.sh` (arrives with the adapter) and re-run the redaction;
never commit a raw capture.
