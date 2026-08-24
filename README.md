# Kolkrabbi

**Chat, code, and ordered agents in one fast CLI — any model, any provider,
with a 100% local rating dashboard.**

*Kolkrabbi* is Icelandic for octopus — *kol* ("coal") + *krabbi* ("crab").
Fitting: roughly two-thirds of an octopus's neurons live in its arms. Many
arms, one small terminal, and many model providers within reach.

Binary name: **`kolk`**.

Think Claude Code, but: any model on [OpenRouter](https://openrouter.ai) (or
any OpenAI-compatible endpoint — LiteLLM, Ollama, vLLM), separate chat, code,
and agent modes, an effort dial that selects *which model* and agent task width
instead of just thinking tokens, and every call tracked locally so you learn
which models actually earn their cost.

Go, zero external dependencies, single ~5MB static binary, ~2ms startup.

## The three modes

```
/mode chat    plain conversation, no tools — cheap and instant
/mode code    the coding loop: read/write/edit files, run commands,
              iterate until done (Claude-Code style)
/mode agent   ordered orchestration: plan the work, run isolated
              subagents one by one, then synthesize one answer
```

Code is the default, so plain `kolk` is ready for file and command work. Switch
to chat when you want a tool-free conversation, or agent when a longer task
benefits from decomposition and isolated working contexts.

## The effort dial

```
/effort quick | standard | deep | ultra
```

Claude Code's `ultrathink` scales thinking on one Anthropic model. Kolkrabbi's
effort scales across providers: each level maps to a model tier you choose. In
agent mode it also caps orchestration width: quick 2 tasks, standard 3, deep 4,
and ultra 6.

```bash
kolk config set-tier quick    google/gemini-2.5-flash   # pennies
kolk config set-tier standard anthropic/claude-sonnet-4.6
kolk config set-tier deep     anthropic/claude-opus-4.6  # frontier
```

Zero-config still works: unset tiers fall back to the session model, so
tiers are a pure optimization, never a requirement.

## The local dashboard

Every model call is appended to `~/.config/kolk/stats.jsonl` — plain JSONL,
no database, no telemetry, nothing ever leaves your machine. Rate turns as
you go with `/rate 1-5`, then:

```
$ kolk stats
MODEL                            CALLS     TOKENS      COST     AVG  RATING  MODES
anthropic/claude-sonnet-4.6         42     181203     $1.24   2100ms    4.6★  code:42
google/gemini-2.5-flash             67      88410     $0.04    390ms    4.1★  chat:67
deepseek/deepseek-chat              12      31877     $0.01    720ms    3.5★  chat:12
TOTAL                              121     301490     $1.29
```

Per-turn cost/latency footers keep it visible in the moment; the dashboard
accumulates the judgment over time: which model is worth what, in *your*
hands, on *your* tasks. Costs are exact on OpenRouter (reported by the API),
token-based elsewhere.

## Install & setup

```bash
go build -ldflags="-s -w" -o kolk .      # Go 1.22+, no dependencies
kolk key sk-or-v1-...                    # or export OPENROUTER_API_KEY=...
kolk
```

That's the whole setup. Everything else is optional.

### Start free automatically

For every new session without an explicit model, kolk asks OpenRouter's live catalog
for zero-cost, tool-capable models and prefers the strongest coding-oriented option.
If the catalog is unavailable, it uses OpenRouter's guaranteed zero-cost
`openrouter/free` router. A saved model, `--model`, or a genuinely custom effort-tier
map still wins because those are explicit user choices.

Earlier builds documented an all-tier `stealth/ox-alpha` preset as free. That model is
no longer guaranteed to cost zero, so kolk recognizes that exact old preset and uses
live free-model discovery instead. `kolk models` lists current models with context size
and $/1M pricing when you want to make a deliberate override.

## Usage

```bash
kolk                          # interactive, code mode
kolk --mode chat              # start in chat
kolk --mode agent "plan, implement, and verify this change"
kolk -y "run the tests and fix failures"    # auto-approve tool actions
kolk -r                       # resume the most recent session
kolk --base-url http://localhost:11434/v1 -m qwen2.5-coder:14b "..."  # Ollama
kolk stats                    # the dashboard
kolk sessions                 # list / resume / delete saved conversations
kolk models claude            # browse models with $/1M pricing
```

In-session: `/mode`, `/effort`, `/model`, `/rate 1-5`, `/changes`, `/rewind`,
`/new`, `/auto-approve [on|off]`, `/yolo`, `/help`. `/auto-approve` without an
argument enables it for the current session; `/yolo` remains the quick toggle.
In the interactive TUI, ↑ reloads the last message; one Ctrl+C clears only the
composer, while a second consecutive Ctrl+C exits. Single-shot Ctrl+C still
aborts that run.

## Sessions, checkpoints, project memory

- **Sessions** auto-save after every step (atomic writes) to
  `~/.config/kolk/sessions/`; resume with `-r`/`-s <id>`. Interrupted tool
  calls are repaired on resume so the history stays API-valid.
- **Checkpoints** snapshot files before every `write_file`/`edit_file`;
  `/changes` lists them, `/rewind` restores the last turn's files (repeatable,
  survives restarts). `bash` changes aren't tracked.
- **Project memory**: `KOLKRABBI.md` or `AGENTS.md` in the working directory
  is added to the system prompt, like CLAUDE.md.

## Sandbox testing (no network, no key, no cost)

`./scripts/test.sh` runs the complete suite fully offline, including an
end-to-end drive of the code loop against a scripted in-process mock of the OpenRouter API
(`internal/enginetest`) that streams realistically fragmented SSE with usage
chunks. For manual rehearsal:

```bash
go run ./cmd/kolk-mock       # prints its URL; scripted demo session inside
kolk --base-url <url> -y "create the hello file"
```

## What the model can do

| Tool | Purpose | Confirmed? | Checkpointed? |
|---|---|---|---|
| `bash` | run a shell command (120s timeout) | yes | no |
| `read_file` | read a file with line numbers | no | — |
| `write_file` | create/overwrite a file | yes | yes |
| `edit_file` | unique exact find/replace | yes | yes |
| `list_dir` | list a directory | no | — |

Chat mode carries no tools at all. Code mode and agent subagents use the same
tool and confirmation gates; every side-effecting action requires approval
unless `-y`, `/auto-approve on`, or `/yolo` is active.

## Architecture

```
cmd/kolk               flags, REPL, subcommands (config/models/sessions/stats)
cmd/kolk-mock          standalone mock for manual sandbox runs
internal/provider      streaming SSE client, tool-call reassembly, usage/cost
internal/engine        chat/code/agent modes, effort tiers, and orchestration
internal/tools         tool schemas + execution, confirm gating, ckpt hook
internal/session       persistent conversations (atomic JSON)
internal/checkpoint    pre-change snapshots, per-turn rewind
internal/stats         local JSONL store + aggregation (the dashboard)
internal/enginetest    scripted fake OpenRouter for offline e2e testing
```

Go module path: `github.com/onembyte/kolkrabbi`. Binary: `kolk`.

> This is the prototype layout. The hardened target architecture — one event bus
> with three byte-identical exits, a language-neutral `spec/` contract, and
> desktop/iPad/Android attaching as new directories — is
> [`docs/plan/02-architecture.md`](docs/plan/02-architecture.md); the open plan
> items are in [`PLAN.md`](PLAN.md).

## Known limitations / next steps

- No context compaction yet: very long sessions eventually hit token limits.
- Ratings inform *you* via the dashboard; auto-routing by rating ("send
  chat turns to my best-rated cheap model") is the phase-3 flywheel.
- `bash` changes aren't checkpointed; a git-stash snapshot per turn would
  cover repos.
- Agent-mode subagents currently run in a fixed order; concurrency is future
  work.
- Unix-only in practice (bash tool, ANSI colors). Single-line REPL input.
