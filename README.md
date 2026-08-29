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
/mode agent   orchestration: plan the work as tasks with real
              dependencies, route each one to a model slot, run the
              independent ones concurrently, then synthesize one answer
```

Code is the default, so plain `kolk` is ready for file and command work. Switch
to chat when you want a tool-free conversation, or agent when a longer task
benefits from decomposition and isolated working contexts.

## The effort dial

```
/effort low | medium | high | max
```

`ultrathink` scales thinking on one vendor's model. Kolkrabbi's effort scales
across providers: each level maps to a model tier you choose, and it also sets
the tool-round limit per turn, the shell timeout, and how many tasks an
orchestrated run may open (low 1, medium 2, high 4, max 6). The older
`quick/standard/deep/ultra` words and the numbers `1..4` are still accepted.

```bash
kolk config set-tier low    google/gemini-2.5-flash   # pennies
kolk config set-tier medium anthropic/claude-sonnet-4.6
kolk config set-tier high   anthropic/claude-opus-4.6  # frontier
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
curl -fsSL https://kolkrabbi.francomichetti.com/install.sh | sh   # macOS and Linux, amd64 and arm64
kolk key sk-or-v1-...                                             # or export OPENROUTER_API_KEY=...
kolk
```

That's the whole setup. Everything else is optional.

The install script picks a writable directory on your `PATH` (override with
`KOLK_INSTALL_DIR`), pins a version with `KOLK_VERSION`, and verifies the download's
SHA-256 against the release's `checksums.txt` before it installs anything.

Two other ways in, if you prefer them:

```bash
go install github.com/onembyte/kolkrabbi/cmd/kolk@latest   # Go 1.25+, two dependencies
git clone https://github.com/onembyte/kolkrabbi && cd kolkrabbi && go build -o kolk ./cmd/kolk
```

`kolk update` replaces the running binary with the current release, verifying its
checksum first. Nothing checks for updates on its own — no background poll, no startup
nudge. kolk contacts the release server when you ask it to and not otherwise.

Every release ships four archives with a `checksums.txt` signed by keyless Cosign. If
you want signature-level assurance rather than checksum-level, `scripts/verify-release.sh
v1.2.3` verifies the signature against the release workflow's identity. One wrinkle worth
knowing: an archive downloaded from the Releases page **in a browser** is quarantined by
macOS and needs `xattr -d com.apple.quarantine kolk` before it will run. The install
script's downloads are not quarantined, so this only bites manual downloads.

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
kolk --permission auto-approve "run the tests and fix failures"   # edits flow, commands still ask
kolk -r                       # resume the most recent session
kolk --base-url http://localhost:11434/v1 -m qwen2.5-coder:14b "..."  # Ollama
kolk stats                    # the dashboard
kolk dash                     # the same numbers as a loopback-only page
kolk sessions                 # list / search / fork / export saved conversations
kolk models claude            # browse models with $/1M pricing
kolk saga "goal"              # the careful-progression loop, gated on your tests
kolk localia                  # what this machine could run locally
kolk serve --addr 127.0.0.1:7777   # stream this session's events to a client
```

In-session: `/mode`, `/effort`, `/model`, `/rate 1-5`, `/diff`, `/changes`,
`/undo`, `/rewind`, `/plan`, `/compact`, `/remember`, `/new`,
`/permissions [ask|auto-approve|full-auto]`, `/help` — `/help` lists all of
them. `/permissions` without an argument lists the three tiers and marks the
active one; `/ask`, `/auto-approve` and `/full-auto` switch straight to one.
`@` completes a file path against the project, and the status line carries
mode, model, effort, context use, and what the session has cost.
In the interactive TUI, ↑ reloads the last message; one Ctrl+C clears only the
composer, while a second consecutive Ctrl+C exits. Single-shot Ctrl+C still
aborts that run.

## Sessions, checkpoints, project memory

- **Sessions** auto-save after every step (atomic writes) to
  `~/.config/kolk/sessions/`; resume with `-r`/`-s <id>`. `-r` resumes the work
  done in *this* directory, and says so when it reaches into another project.
  Interrupted tool calls are repaired on resume so the history stays API-valid.
  `kolk sessions search|rename|fork|export` covers the rest.
- **Context** is measured from provider-reported tokens and shown in the status
  line. A filling session compacts at a turn boundary and says what it gave up;
  `/compact` forces it, `/compact undo` puts the conversation back, and a turn
  refused for length is recovered rather than lost.
- **Checkpoints** snapshot files before every `write_file`/`edit_file`;
  `/changes` lists them, `/diff` shows them as diffs, `/rewind` restores the last
  turn's files and `/undo` takes back the files *and* the conversation
  (repeatable, survives restarts). `bash` changes aren't tracked.
- **Project memory**: `KOLKRABBI.md` or `AGENTS.md` in the working directory
  is added to the system prompt. `/remember` adds one line of personal guidance
  beneath it, without editing a project file.

## Sandbox testing (no network, no key, no cost)

`./scripts/test.sh` runs the complete suite fully offline, including an
end-to-end drive of the code loop against a scripted in-process mock of the OpenRouter API
(`internal/enginetest`) that streams realistically fragmented SSE with usage
chunks. For manual rehearsal:

```bash
go run ./cmd/kolk-mock       # prints its URL; scripted demo session inside
kolk --base-url <url> --permission full-auto "create the hello file"
```

## What the model can do

| Tool | Purpose | Confirmed? | Checkpointed? |
|---|---|---|---|
| `bash` | run a shell command (30s–600s, set by effort) | yes | no |
| `read_file` | read a file with line numbers | no | — |
| `write_file` | create/overwrite a file | yes | yes |
| `edit_file` | unique exact find/replace | yes | yes |
| `list_dir` | list a directory | no | — |

Chat mode carries no tools at all. Code mode and agent subagents share the same
gates.

File paths are confined to the project — the enclosing git repository, or the
working directory when there is none. Reaching outside it asks first, and in
`full-auto` it proceeds but is logged with the path and the reason the model
gave for needing it.

Three permission tiers decide how much happens without a prompt:

| tier | inside the project | shell commands | outside the project |
|---|---|---|---|
| `ask` (default) | asks before writing | asks | asks |
| `auto-approve` | edits without asking | asks | asks |
| `full-auto` | edits without asking | runs | proceeds, and logs it |

**No tier removes the floor.** Credential files (`~/.ssh`, `~/.aws`, `~/.gnupg`,
`credentials.json`), writes into system directories, `sudo`, piping a download
into a shell, and unrecoverable deletes are refused in all three, `full-auto`
included. An agent that cannot refuse anything is not one you can leave
running.

## Architecture

```
cmd/kolk               flags, REPL, subcommands (config/models/sessions/stats…)
cmd/kolkd              headless daemon over the same event protocol
cmd/kolk-mock          standalone mock for manual sandbox runs
protocol/, spec/       the versioned event envelope and its golden frames
internal/provider      streaming SSE client, tool-call reassembly, usage/cost
internal/engine        chat/code/agent modes, effort tiers, orchestration, saga
internal/tools         tool schemas + execution, confirm gating, ckpt hook
internal/session       persistent conversations (atomic JSON), compaction
internal/checkpoint    pre-change snapshots, per-turn rewind
internal/stats         local JSONL store + aggregation (the dashboard)
internal/dash          server-rendered, loopback-only usage dashboard
internal/bus, serve    event bus and the NDJSON / stdio / SSE surfaces
internal/devices       pairing codes and per-device tokens for remote access
internal/local         managed local-model runtime, hardware probe, fit planner
internal/tui, term     persistent composer, status line, terminal facts
internal/redact, secret, keystore   scrubbing and credential storage
internal/enginetest    scripted fake OpenRouter for offline e2e testing
```

Go module path: `github.com/onembyte/kolkrabbi`. Binary: `kolk`.

> This is the prototype layout. The hardened target architecture — one event bus
> with three byte-identical exits, a language-neutral `spec/` contract, and
> desktop/iPad/Android attaching as new directories — is
> [`docs/plan/02-architecture.md`](docs/plan/02-architecture.md); the open plan
> items are in [`PLAN.md`](PLAN.md).

## Roadmap and what kolk will not do

The roadmap is [`PLAN.md`](PLAN.md) and [`CHECKPOINTS.md`](CHECKPOINTS.md) — versioned with the code,
reviewed in a diff, and checked against each other by `make check`. Work goes in phases, not version
numbers: the ordering rule is *finish what is half-built before starting what is unbuilt, correctness
before the surface that displays it, and permissions before autonomy*.

Refusals are worth more than plans when you are deciding whether to use something, so the short list
of what kolk deliberately does **not** do:

- **No telemetry, no analytics, no background version check.** kolk contacts a server when you ask it
  to. `kolk update` checks for a release; nothing else phones anywhere.
- **No hosted service and no cloud sync.** Sessions are files on your disk.
- **No plugins compiled in Go**, and no dynamic loading into the agent's address space.
- **No native mobile apps.** A phone can steer a session over the local network instead.
- **No branch-per-session**, and no pull-request integration beyond GitHub's `gh`.
- **Windows is cross-built and advisory in CI, not supported.** macOS and Linux are.

The reasoning for each, and the condition that would change it, is in
[`docs/plan/23-roadmap-phasing-non-goals.md`](docs/plan/23-roadmap-phasing-non-goals.md).

## Known limitations / next steps

- Ratings inform *you* via the dashboard; auto-routing by rating ("send
  chat turns to my best-rated cheap model") is still ahead.
- `bash` changes aren't checkpointed; a git-stash snapshot per turn would
  cover repos.
- Subagents run concurrently but share the working tree: worktree isolation and
  a dedicated critic are not built yet.
- A session still expects a gateway key even when a subscription plan will
  answer the turns.
- Local models use the Ollama you already have; kolk never installs one. An
  installed-but-idle Ollama lists nothing in `/model` until something runs it.
- No MCP, skills, commands, or hooks yet, and no execution sandbox.
- A remote device can watch a session and answer its permission prompts; it
  cannot yet send a turn.
- Unix-only in practice (bash tool, ANSI colors); Windows is cross-built and
  advisory in CI, not supported.
