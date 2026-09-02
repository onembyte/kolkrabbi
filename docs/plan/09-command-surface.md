# 9. Command surface — few, obvious, typeable

Status: hardened on 2026-08-26 · supersedes: — · PLAN.md item 9

## Decision (the short version)

**Kolkrabbi's command surface enforces strict top-level and in-session parity: every top-level verb has an exact slash twin (`kolk <verb> [args]` $\equiv$ `/<verb> [args]`) sharing identical syntax, parsing, and semantics.** Command names are governed by three rigid guardrails: (1) strictly one word, (2) all lowercase, (3) $\le 6$ characters in length. A unified Command Registry in `internal/cli` serves as the single source of truth for CLI flag dispatch, REPL slash routing, interactive TUI autocomplete, `--help` generation, and shell completion generation.

For automated scripting, CI workflows, and future desktop/iPad clients, non-interactive execution (`-p "prompt"`, `cat input | kolk`, `--output stream-json`) streams machine-parseable NDJSON events without TUI chrome, terminal controls, or interactive prompts, exiting with deterministic UNIX status codes (0 ok, 1 general error, 2 usage, 3 budget exceeded, 130 interrupt). All future capabilities are held in an explicit **Reserve List** so core verbs remain uncluttered.

---

## Spec

### 0. ★ North star compliance

#### 0.1 The napkin test
```console
# CLI usage:
$ kolk key sk-or-v1-…
$ kolk model sonnet
$ kolk effort 3

# REPL usage (exact identical grammar):
kolk-code> /key sk-or-v1-…
kolk-code> /model sonnet
kolk-code> /effort 3
```

#### 0.2 North star rules compliance

| North star rule | How Item 9 complies | Enforced by |
|---|---|---|
| **1. Zero-config is the product** | Commands operate with sensible defaults when called without arguments (`kolk`, `kolk model`, `kolk effort`, `kolk stats`). No required initialization commands. | `TestZeroArgCommandDefaults` |
| **2. Every default computed, not asked** | `kolk` with no verb immediately starts a working code session. Flag parsing uses non-interactive fail-fast validation. | `TestBareKolkEntersSession` |
| **3. One install command, static binary** | Self-generating completions (Bash, Zsh, Fish) and help text are embedded in the binary without external man-page or toolchain dependencies. | `TestEmbeddedCompletionGeneration` |
| **4. One key command** | `kolk key <key>` and `/key <key>` accept any supported provider key, inferring the provider shape automatically. | `TestKeyCommandParity` |
| **5. Complexity ships off, discoverable later** | Advanced tools (MCP, hooks, profiles, worktree) remain on the reserve list. v0.1 ships only core verbs. | `TestCommandTableMatchesSpec` |
| **6. Simple to type beats simple to explain** | Verbs are $\le 6$ letters, with five recorded exceptions (see below). Common actions have single-character flags (`-m`, `-e`, `-p`, `-r`); `-y` was removed by E13.2. | `TestCommandNameLengthGuardrail` |

---

### 1. The Parity Rule & Command Registry

#### 1.1 The Parity Rule
> Every top-level verb that makes sense interactively MUST exist as an identical slash twin inside the REPL and TUI composer, accepting the same positional arguments and flags.

#### 1.2 The Core Verbs Table

| Verb | Length | Arguments | Slash Twin | CLI Flag Twin | Summary | Since |
|---|:---:|---|:---:|:---:|---|:---:|
| `key` | 3 | `<api-key> \| - \| <provider> <key>` | `/key` | — | Configure provider API key | v0.1 |
| `model` | 5 | `[id \| alias]` | `/model` | `-m, --model` | Switch or list model catalog | v0.1 |
| `effort`| 6 | `[low\|medium\|high\|max\|1..4]` | `/effort` | `-e, --effort`| Set reasoning and tool budget | v0.1 |
| `mode` | 4 | `[chat\|code]` | `/mode` | `--mode` | Switch operational mode | v0.1 |
| `config`| 6 | `[get\|set\|unset\|show] [k] [v]` | `/config` | — | Read and write persistent settings | v0.1 |
| `login` | 5 | `[provider]` | `/login` | — | OAuth login or native agent auth | v0.1 |
| `update`| 6 | `[--check]` | `/update` | — | Check or install latest binary | v1.1 |
| `stats` | 5 | `[--json]` | `/stats` | — | View local token and cost metrics | v0.1 |
| `dash` | 4 | `[--port <p>]` | `/dash` | — | Launch local efficiency dashboard | v0.3 |
| `doctor`| 6 | `[--verbose]` | `/doctor` | — | Diagnose keys, network, and tools | v0.2 |
| `help` | 4 | `[command]` | `/help` | `-h, --help` | Show command reference | v0.1 |
| `exit` | 4 | `[code]` | `/exit` | — | Terminate session (REPL alias: `/quit`)| v0.1 |

*Note: Session-only navigation commands (`/new`, `/clear`, `/rewind`, `/diff`, `/changes`) exist in the REPL where state history is active.*

`/saga` is an inline workflow marker in a normal request, not a standalone CLI verb and not a
status/stop/resume command family. Its progress and cost are shown in the running TUI log.

---

### 2. Naming Guardrails

To prevent command sprawl and cognitive clutter, all commands must satisfy:
1. **Single word**: No hyphenated or camelCase command verbs (`kolk set-key` $\to$ `kolk key`, `kolk set-tier` $\to$ `kolk effort`).
2. **All lowercase**: Case-insensitive in REPL, lowercased in shell.
3. **$\le 6$ characters**: Short and effortless to type in the terminal (`key`, `mode`, `model`, `effort`, `config`, `login`, `update`, `stats`, `dash`, `saga`, `doctor`, `help`, `exit`).
4. **No synonyms**: Exactly one canonical verb per concept (`stats` is the CLI table, `dash` is the interactive UI; `/exit` is primary, `/quit` is a silent alias).

---

### 3. Non-interactive & Scripting Surface

When stdin/stdout is not a TTY, or when invoked with `-p / --prompt`:

#### 3.1 Invocation forms
```console
# 1. Single-shot prompt
$ kolk -p "explain the bug in internal/shell"

# 2. Piped input (reads prompt from stdin)
$ git diff | kolk -p "review this diff for race conditions"

# 3. Stream JSON for machine integration
$ kolk -p "refactor auth" --output stream-json
```

#### 3.2 Machine-readable output formats
- `--json`: Emits a single JSON summary object on completion:
  ```json
  {"session_id":"s_...","model":"...","content":"...","tokens":{"prompt":120,"completion":45},"cost_usd":0.0012}
  ```
- `--output stream-json`: Emits newline-delimited JSON (NDJSON) events identical to the `protocol/` wire format (`message.delta`, `tool.invocation`, `tool.result`, `turn.complete`).

#### 3.3 Exit codes

| Exit Code | Constant | Meaning |
|:---:|---|---|
| `0` | `ExitOK` | Successful turn completion |
| `1` | `ExitError` | Runtime error, provider failure, or tool execution failure |
| `2` | `ExitUsage` | Invalid flag, unrecognized command, or bad argument grammar |
| `3` | `ExitBudget` | Cost budget, token limit, or loop iteration ceiling reached |
| `130` | `ExitInterrupt`| Process cancelled via SIGINT (Ctrl+C) |

---

### 4. Shell completions

Kolkrabbi embeds completion scripts for Bash, Zsh, and Fish:
```console
$ kolk completion bash > /etc/bash_completion.d/kolk
$ kolk completion zsh  > ~/.zfunc/_kolk
$ kolk completion fish > ~/.config/fish/completions/kolk.fish
```
Completions dynamically provide:
- Command verbs (`key`, `model`, `effort`, `config`, `stats`, etc.)
- Model aliases and local cached model IDs for `kolk model <TAB>`
- Effort levels (`low`, `medium`, `high`, `max`, `1`, `2`, `3`, `4`) for `kolk effort <TAB>`
- Config keys for `kolk config get/set <TAB>`

---

### 5. The Reserve List

Future capabilities must not invent one-off top-level commands without architectural review. The following names are explicitly reserved for upcoming phases:

| Name | Owning Item | Target Release | Purpose |
|---|:---:|:---:|---|
| `mcp` | Item 16 | v0.3 | MCP server lifecycle (`kolk mcp add/list/rm`) |
| `skills`| Item 16 | v0.3 | Markdown-defined user prompts & tools |
| `hooks` | Item 16 | v0.3 | Pre/post tool shell hook configurations |
| `worktree`| Item 14| v0.3 | Git worktree isolation for parallel subagents |
| `compact` | Item 12| v0.2 | Manual context compaction command |
| `diff` | Item 15 | v0.2 | View session file modifications |
| `undo` | Item 12 | v0.2 | Undo turn changes and session history |
| `cost` | Item 17 | v0.2 | Print detailed session expenditure breakdown |
| `theme` | Item 11 | v0.3 | Terminal color scheme picker |
| `serve` | Item 2 | v0.4 | Background daemon / headless event server |

---


### Amendment, 2026-08-27 — the six-letter rule has five exceptions

An audit compared `commandTable()` against this rule and found five shipped verbs breaking it:
`completion`, `localia`, `pmodels`, `sessions`, `version`.

`TestCommandNameLengthGuardrail`, named above as the enforcement, could not have caught them: it
checked a hardcoded list of thirteen names rather than the table, so it asserted `len("key") > 6` —
decidable when it was written and unable to fail afterwards. Three of those thirteen (`login`,
`doctor`, `exit`) are not commands at all. The test now reads the table.

The five are recorded as exceptions rather than renamed, because they are published: `kolk sessions`
and `kolk version` shipped in v1.2.1. Renaming them is a deprecation with a cost to users, and that
is the owner's call, not a tidy-up. Each exception carries its reason in `longVerbs`, a new long verb
fails the test, and a second test rejects an exemption for a command that no longer exists.

**Open for the owner:** shorten some of these on the next major, or accept that the rule is "short
unless the shorter name reads worse". `version` in particular is what every other CLI calls it, and
muscle memory is a stronger argument than a character count.

## Rationale

1. **Muscle memory unification**: Switching between the shell and the REPL should not require translating verbs (e.g. remembering whether it's `kolk set-model` vs `/model`). One verb behaves identically in both.
2. **Terminal typing speed**: Requiring long verbs like `configuration` or `orchestration` slows developers down. Short, memorable $\le 6$ character verbs make Kolkrabbi feel nimble and pleasant.
3. **Robust automated pipelines**: Providing deterministic exit codes and standard `stream-json` makes Kolkrabbi trivially scriptable in git hooks, CI pipelines, and external editors without terminal scraping.

---

## Alternatives rejected

- **Nested subcommand trees (e.g. `kolk agent mode set code`)**: Rejected as bloated git-style complexity. Kolkrabbi's surface is flat, concise, and direct.
- **Interactive wizard on bare `kolk`**: Rejected; bare `kolk` must immediately open an interactive prompt in code mode without questions.
- **Multiple synonyms for verbs**: Rejected to keep documentation, muscle memory, and test suites unambiguous.

---

## Risks & open questions

- **Risk: Ambiguity between prompts and commands**: A user running `kolk update the dependencies` might accidentally trigger the `update` command.
  *Mitigation*: The command dispatcher strictly matches command verbs only when positional argument grammar matches. Furthermore, `update` takes no arguments; any following words route to a prompt turn.
- **Risk: Terminal raw mode conflicts in piped execution**:
  *Mitigation*: Piped execution disables raw mode, ANSI styling, and interactive loops completely, streaming plain text or NDJSON directly to stdout.

---

## Sources

- `docs/research/ecosystem.md`: CLI ergonomics in Claude Code, Codex, Aider, and OpenCode.
- `docs/plan/02-architecture.md`: Protocol contracts and streaming exits.
- `docs/plan/06-modes.md`: Mode switching and default code behavior.
- `docs/plan/07-effort-dial.md`: Effort commands and numeric aliases.
- `docs/plan/18-config.md`: Config key grammar and CLI mapping.

---

## Checkpoint breakdown

Implementation of Item 9 is organized into 4 atomic checkpoints:

- **C9.1 Unified Command Table & Parity Engine**: Refactor `internal/cli` to use one single command table for both shell dispatch and REPL slash routing.
- **C9.2 Short Verbs & Grammar Simplification**: Migrate legacy commands (`set-model`, `set-tier`) into unified short verbs (`model`, `effort`, `key`).
- **C9.3 Non-Interactive Scripting & Stream-JSON**: Implement `--output stream-json` and strict UNIX exit codes (0, 1, 2, 3, 130).
- **C9.4 Shell Completion Generator**: Add `kolk completion <bash|zsh|fish>` generating dynamic autocompletions from the unified command table.
