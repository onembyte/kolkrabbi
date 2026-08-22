# Kolkrabbi

**Chat, code, and orchestrated agents in one fast CLI — any model, any
provider, with a 100% local rating dashboard.**

*Kolkrabbi* is Icelandic for octopus — *kol* ("coal") + *krabbi* ("crab").
Fitting: roughly two-thirds of an octopus's neurons live in its arms, each
one sensing and acting semi-independently while a central brain coordinates.
That's the architecture — a central orchestrator, isolated subagents,
many arms reaching into many model providers at once.

Binary name: **`kolk`**.

Think Claude Code, but: any model on [OpenRouter](https://openrouter.ai) (or
any OpenAI-compatible endpoint — LiteLLM, Ollama, vLLM), three modes instead
of one, an effort dial that scales *which model* and *how much orchestration*
instead of just thinking tokens, and every call tracked locally so you learn
which models actually earn their cost.

Go, zero external dependencies, single ~5MB static binary, ~2ms startup.

## The three modes

```
/mode chat    plain conversation, no tools — cheap and instant
/mode code    the classic agentic loop: read/write/edit files, run commands,
              iterate until done (Claude-Code style)
/mode agent   orchestrated: a planner decomposes your request, isolated
              subagents execute each task with their own context, and a
              synthesis step writes the final answer
```

In agent mode the main conversation only ever records *your request → the
final answer* — all the subagent work happens in isolated contexts, so long
orchestrated turns don't bloat your session history.

## The effort dial

```
/effort quick | standard | deep | ultra
```

Claude Code's `ultrathink` scales thinking on one Anthropic model. Kolkrabbi's
effort scales across providers: each level maps to a model tier you choose,
and in agent mode it also scales orchestration width (2 → 6 subagent tasks).

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
anthropic/claude-sonnet-4.6         42     181203     $1.24   2100ms    4.6★  code:38 agent:4
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
kolk config set-key sk-or-v1-...         # or export OPENROUTER_API_KEY=...
kolk
```

That's the whole setup. Everything else is optional.

## Usage

```bash
kolk                          # interactive, code mode
kolk --mode chat              # start in chat
kolk --mode agent -e ultra "refactor the auth package and add tests"
kolk -y "run the tests and fix failures"    # auto-approve tool actions
kolk -r                       # resume the most recent session
kolk --base-url http://localhost:11434/v1 -m qwen2.5-coder:14b "..."  # Ollama
kolk stats                    # the dashboard
kolk sessions                 # list / resume / delete saved conversations
kolk models claude            # browse models with $/1M pricing
```

In-session: `/mode`, `/effort`, `/model`, `/rate 1-5`, `/changes`, `/rewind`,
`/new`, `/yolo`, `/help`. Ctrl+C interrupts the current turn only.

## Sessions, checkpoints, project memory

- **Sessions** auto-save after every step (atomic writes) to
  `~/.config/kolk/sessions/`; resume with `-r`/`-s <id>`. Interrupted tool
  calls are repaired on resume so the history stays API-valid.
- **Checkpoints** snapshot files before every `write_file`/`edit_file`;
  `/changes` lists them, `/rewind` restores the last turn's files (repeatable,
  survives restarts). `bash` changes aren't tracked. An orchestrated turn
  rewinds as one unit.
- **Project memory**: `KOLKRABBI.md` or `AGENTS.md` in the working directory
  is added to the system prompt, like CLAUDE.md.

## Sandbox testing (no network, no key, no cost)

`go test ./...` runs 22 tests fully offline, including end-to-end drives of
the code loop *and* the full orchestrator (plan → subagents → synthesis)
against a scripted in-process mock of the OpenRouter API
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

Chat mode carries no tools at all. Confirmation prompts gate every
side-effecting action unless `-y`/`/yolo`.

## Architecture

```
cmd/kolk               flags, REPL, subcommands (config/models/sessions/stats)
cmd/kolk-mock          standalone mock for manual sandbox runs
internal/provider      streaming SSE client, tool-call reassembly, usage/cost
internal/engine        modes, effort tiers, the code loop, the orchestrator
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

- Subagents run sequentially; parallel execution is the natural upgrade once
  confirmation UX for concurrent tool calls is designed (yolo mode first).
- No context compaction yet: very long sessions eventually hit token limits.
- Ratings inform *you* via the dashboard; auto-routing by rating ("send
  chat turns to my best-rated cheap model") is the phase-3 flywheel.
- `bash` changes aren't checkpointed; a git-stash snapshot per turn would
  cover repos.
- Unix-only in practice (bash tool, ANSI colors). Single-line REPL input.
