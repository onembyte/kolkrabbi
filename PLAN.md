# Kolkrabbi — plan-hardening checklist

Kolkrabbi (`kolk`) is a fast, lightweight terminal agent: **chat / code / agent** modes, an
**effort dial**, **one key → many models** (OpenRouter first), and a **100% local** dashboard
that tells you which model earns its cost on *your* tasks. CLI first; desktop later; iPad after.

This file is the **index of open plan items**. Each item is sized to be hardened in one focused
`/loop` session: resolve the decisions, write the detail into `docs/plan/NN-slug.md`
(template: `docs/plan/README.md`), tick the box here and add a one-line decision summary.
Research inputs gathered on 2026-08-21 live in `docs/research/`.

**How to run a hardening loop** (copy, edit the item number):

```
/loop harden item 10 of PLAN.md: read the item and its inputs in docs/research/, resolve every
"Decide" bullet (research the web when needed, cite sources), write the result as
docs/plan/10-saga-loop.md using docs/plan/README.md's template, then tick the item in PLAN.md
with a one-line decision. Each iteration: make progress on ONE open bullet, update the doc,
stop when all bullets are closed and the "Hardened when" criteria hold.
```

Legend: `[ ]` open · `[~]` in progress · `[x]` hardened (doc linked).

---

## 0. Ground truth — what exists today (verified 2026-08-21)

Extracted from `kolkrabbi.tar` into this directory. Go 1.22 module `kolkrabbi` (built here with
Go 1.26.4), **zero external dependencies**, ~3.4k LOC, builds to a 6.1 MB static binary that
starts in ~10 ms per invocation (fork+exec+run, measured over 20 runs on an M-series Mac),
`go vet` clean, **22 tests pass** (offline, against an in-process scripted OpenRouter mock).

| Area | Prototype has | Gap vs. the vision |
|---|---|---|
| Modes | `chat` (no tools), `code` (tool loop), `agent` (plan → sequential subagents → synthesis) | parallel subagents, per-task model routing, mode auto-suggest |
| Effort | `quick/standard/deep/ultra` → model tier map + orchestration width (2–6 tasks) | per-mode efforts, provider `reasoning.effort`, tool/round budgets |
| Providers | OpenRouter + any OpenAI-compatible `--base-url` (Ollama/LiteLLM/vLLM) | OAuth login, subscription backends (Claude Max), fallbacks/retries |
| Tools | `bash`, `read_file`, `write_file`, `edit_file`, `list_dir`; confirm gating; `-y` yolo | grep/glob, web, MCP, permission rules, sandboxing |
| Sessions | JSON per session, atomic saves, resume `-r`/`-s`, dangling-tool-call repair | compaction, search, fork, SQLite |
| Checkpoints | pre-write snapshots, `/changes`, `/rewind` per turn | bash-made changes, conversation rewind |
| Stats | `stats.jsonl` per call (model/mode/effort/role/tokens/cost/ms) + `/rate 1-5`; `kolk stats` table | real dashboard, scorers, A/B, session-level analysis |
| UI | single-line ANSI REPL, slash commands, cost footer | TUI, multiline, markdown/diff rendering, status line |
| Commands | `config`, `models`, `sessions`, `stats`; slash `/mode /effort /model /rate /yolo /new /session /changes /rewind` | `model`, `effort`, `loop`→`saga`, `login`, `dash`… |

Things the prototype gets right that we keep: stdlib-only core, streaming SSE client with tool-call
reassembly, OpenRouter `usage.include` for exact cost, project memory via `KOLKRABBI.md`/`AGENTS.md`,
offline e2e testing via `internal/mockrouter`.

---

## A. Foundations

### [ ] 1. Identity, repository & release skeleton
**Scope:** names, GitHub repo, module path, license, versioning, CI skeleton.
**Today:** README says *kolkrabbi* = Icelandic "octopus" (kol+krabbi); binary `kolk`; module `kolkrabbi`.
**Decide:**
- Name is `kolkrabbi` (your message said "lolkrabbi" — assumed a typo; confirm). Binary stays `kolk`.
- GitHub: `onembyte/kolkrabbi` (the `gh` account on this machine) or an org? Public from day one?
- Module path `github.com/<owner>/kolkrabbi`; package layout `cmd/kolk`, `internal/...`, later `pkg/core` for reuse by desktop/iPad.
- License (MIT vs Apache-2.0 — Apache gives a patent grant; MIT is simpler). Contributor policy (solo for now).
- Versioning & release: semver `v0.x`, GitHub Actions (build/test/lint on push; goreleaser on tag), targets darwin/linux (amd64+arm64) first, windows later.
- Naming theme to reuse or not: octopus "arms" = subagents, "ink" = logs, etc. (keep it light).
**Hardened when:** repo exists with CI green on the current prototype, `go install github.com/<owner>/kolkrabbi/cmd/kolk@latest` works, README reflects the vision.
**Inputs:** —

### [x] 2. Language & architecture — **hardened → [`docs/plan/02-architecture.md`](docs/plan/02-architecture.md)**
**Decision:** Go stays; one module `github.com/onembyte/kolkrabbi` with `cmd/kolk` + `internal/*`, a language-neutral `spec/` contract with a public `protocol/` binding, one event bus with three byte-identical exits (stdout NDJSON / child stdio / HTTP+SSE), layering enforced by `internal/arch` tests; desktop, iPad and Android each attach as a new directory. Nested modules (`desktop/`, `bind/`, `tools/`) can import the parent's `internal/` while foreign repos cannot — **verified empirically 2026-08-22**. 16-step migration keeps all 22 tests green with no red build window.
**Scope:** language verdict, long-term architecture that survives desktop + iPad, dependency policy, performance budgets.
**Today:** Go, stdlib only, hand-rolled REPL, ~2 ms startup, 6 MB binary.
**Decide:**
- Go vs Rust vs TypeScript/Bun for *this* product (single static binary, startup, TUI quality, streaming/concurrency, contributor pool, mobile/desktop reuse). Working assumption: **Go stays** — research concurs with caveats (tree-sitter/LSP later, pin `charm.land`, Windows later); see `docs/research/platform-strategy.md` for the full verdict.
- Architecture: `core` (engine: providers, modes, tools, sessions, stats) as an importable Go library + an optional local daemon (`kolk serve`) exposing a versioned JSON-RPC / HTTP+SSE API; thin frontends: CLI/TUI now, desktop webview later, iPad client later. Define the protocol *before* the TUI rewrite so the CLI is just another client.
- Dependency policy: stay stdlib-only where it matters (engine), allow vetted deps in the UI layer (Bubble Tea v2 / Lip Gloss / Glamour?) and storage (pure-Go SQLite?). Explicit "no cgo" rule? (cross-compilation + iPad later).
- OS support: macOS + Linux first; Windows = later but don't paint into a corner (`bash` tool → shell abstraction, ANSI handling, paths).
- Performance budgets: cold start < 30 ms, binary < 20 MB, idle RSS < 50 MB, first token overhead < 50 ms above raw API latency.
- Concurrency model for parallel subagents + streaming UI (goroutines + channels; cancellation via context everywhere).
**Hardened when:** architecture doc with module map, protocol sketch, dependency rules and budgets; a tiny proof (core package compiled separately from the CLI).
**Inputs:** `docs/research/platform-strategy.md`

### [ ] 3. Provider layer — one key, many models (and any OpenAI-compatible endpoint)
**Scope:** the abstraction every mode talks to; OpenRouter as primary; local/self-hosted; optional direct providers.
**Today:** `internal/api` streaming client (chat/completions, tools, usage+cost), `ListModels`, `--base-url` override.
**Decide:**
- Provider interface: stream, tools (parallel tool calls), reasoning params, prompt caching hints, usage/cost, model capabilities (ctx, pricing, supported params, reasoning, vision), errors (429/402/5xx/timeouts) → typed.
- OpenRouter specifics to support: `models` fallback array, provider routing (`sort: throughput|latency|price`, `:nitro`, `:floor`, `:free`), `reasoning` unified param, `reasoning_details` passthrough for tool loops, `usage.include`, app attribution headers, `/models` catalog cache (TTL + `kolk models --refresh`), `/key` credits check, data-policy filters.
- Second gateway as alternative one-key backend? (Vercel AI Gateway, LiteLLM proxy) — or just "any OpenAI-compatible base URL".
- Direct provider keys (Anthropic/OpenAI/Google native APIs) — in scope, or OpenRouter-only + OpenAI-compatible? (Native APIs unlock prompt caching control and Responses API features but multiply code paths.)
- Local models: Ollama / LM Studio / vLLM presets (`kolk config provider ollama`), tool-calling caveats for small models.
- Retries/backoff policy, request timeouts, streaming keep-alives, idempotency on resume, free-model rate-limit handling (rotate to another free model on 429?). *Research:* branch on OpenRouter's typed `error_type`, honor Retry-After on 429/503, and handle mid-stream `finish_reason:"error"` SSE events + preserve `reasoning_details` across tool calls — two known prototype gaps.
- Where provider-specific quirks live (per-model "profiles": max output tokens, supports tools/structured output, reasoning style).
**Hardened when:** interface spec + OpenRouter adapter spec + error/retry matrix + model-catalog cache design; test plan against `mockrouter`.
**Inputs:** `docs/research/openrouter.md`

### [ ] 4. Subscription logins — Claude Max via Claude Code (and the Codex / Gemini analogues)
**Scope:** "instead of an Anthropic API key, use my Claude Max plan". Must be done in the shape the vendor permits.
**Today:** nothing.
**Answered by research** (`docs/research/subscription-auth.md`, vendor docs quoted, 2026-08-22):
(i) token reuse — **prohibited**; (ii) Agent SDK offered on subscription login — **prohibited
unless previously approved**; (iii) spawning the user's own unmodified, self-logged-in
`claude -p --output-format stream-json` — **permitted with conditions** (binary unmodified, no
credential touching, no resale, "Claude Agent" branding, risk note); (iv) Codex/ChatGPT login —
gray, verify OpenAI terms; (v) Gemini login — OAuth reuse **explicitly prohibited** → API-key
backend only.
**Still to decide:**
- If (iii) is the path: `kolk login claude` = detect the `claude` binary + login state; a "claude-code" provider that maps kolk modes/efforts to `claude -p` flags (`--permission-mode`, `--allowedTools`, `--append-system-prompt`, `--model`, effort); who runs the tools (Claude Code runs its own tools — so in that backend kolk is a frontend + recorder); what the dashboard can still capture (usage from stream-json result events).
- Product stance for the prohibited shapes (token reuse, Gemini OAuth): document clearly, offer the API-key path, keep adapters pluggable in case policy changes; decide whether to build the Codex adapter before or after verifying OpenAI's terms.
- Terms/risk note in README (account-suspension risk language) and a per-backend capability matrix (tools, streaming, cost reporting, context size).
**Hardened when:** a written allowed/gray/prohibited table with citations, a chosen shape, a flag-mapping spec, and a fallback plan.
**Inputs:** `docs/research/subscription-auth.md`

### [ ] 5. Auth, keys & secrets
**Scope:** how keys get in, where they live, how they never leak.
**Today:** `kolk config set-key` → `~/.config/kolk/config.json` (0600) or `OPENROUTER_API_KEY`.
**Decide:**
- "Login with OpenRouter" via OAuth PKCE (localhost callback) vs. paste-key; both? Default for first run. *Research:* PKCE flow is confirmed CLI-friendly — `openrouter.ai/auth` + `POST /api/v1/auth/keys`, localhost callback on any port, headless code-paste variant, codes single-use/10 min.
- Storage: OS keychain (macOS Keychain / Secret Service / Windows Credential Manager) vs. 0600 file; env overrides; per-profile keys; multiple providers.
- Redaction: never write keys to sessions/stats/logs; mask in `config show`; scrub tool output that echoes env.
- Multi-profile / multi-account (work vs personal keys) and project-level overrides.
**Hardened when:** key lifecycle spec (add/rotate/remove/where), threat list, UX for first run.
**Inputs:** `docs/research/openrouter.md` (OAuth PKCE section)

## B. Core UX

### [ ] 6. The three modes — chat / code / agent
**Scope:** exact semantics of each mode, switching by prompt, per-mode defaults.
**Today:** `/mode chat|code|agent`; chat = no tools; code = tool loop; agent = orchestrated. System prompt per mode; project memory appended.
**Decide:**
- Boundaries: what chat can do (read-only tools? web? none), what code adds, what agent adds. Can a mode be a *policy* over the same engine (tools set + orchestration + defaults) rather than three code paths?
- Switching "by prompt": `/chat`, `/code`, `/agent` one-word shortcuts (+ `/mode`), keybinding to cycle (Shift+Tab style), flag `--mode`, and optional auto-suggest ("this looks like a coding task — switch? [y/N]") driven by the fast lane (item 8). Never auto-switch silently.
- Per-mode defaults: model/tier map, effort, yolo policy, tool set, system prompt; persisted per project (`.kolk/`) and per user.
- Mode-specific memory: `KOLKRABBI.md` + `AGENTS.md` (standard) — keep both; precedence and size limits.
- Visual identity per mode (prompt label, color) and how mode shows in the status line.
**Hardened when:** a spec table (mode × tools × defaults × prompt × switching), and the "modes as policy" architecture decision.
**Inputs:** `docs/research/ecosystem.md`

### [ ] 7. The effort dial — fully configurable, including inside code mode
**Scope:** what "effort" means in kolk and how the user tunes it.
**Today:** `quick/standard/deep/ultra` → optional model tier map (`config set-tier`) + subagent count in agent mode. Unset tiers fall back to the session model.
**Decide:**
- Names: keep `quick/standard/deep/ultra` or align with Claude Code's `low/medium/high/max` (you use that muscle memory) + numeric aliases `/effort 3`. Recommendation: align with Claude Code names, keep 4 levels.
- What each level controls, per mode: model tier, provider `reasoning.effort`/thinking budget, max tool rounds, subagent width, verification passes (e.g. `max` runs a critic), context budget, timeouts.
- Fully configurable: per-user and per-project tier maps; per-mode overrides (`code.effort.high.model = …`); live `/effort` in any mode (code mode included) with immediate model re-resolution and a visible "effort → model" echo.
- Fresh-install defaults built from **free** models (item 8) so effort works with zero config.
- Escalation policy: automatic bump of effort when a step fails N times (opt-in), and de-escalation for tiny tasks.
- *Research reference:* Codex couples `/model` with `model_reasoning_effort` (minimal…xhigh); Amp names its modes low/medium/high/ultra; Hermes sets `reasoning_effort` per model slot — supports "effort = tier map + per-slot reasoning effort".
**Hardened when:** effort matrix (level × mode × knob) + config schema + UX transcript examples.
**Inputs:** `docs/research/openrouter.md` (reasoning param), `docs/research/ecosystem.md`

### [ ] 8. Model selection, routing, the fast lane, and free-model defaults
**Scope:** picking models by hand and automatically; tiny-task fast models; the fresh-install catalog.
**Today:** `-m`/`/model <id>`, `kolk models [filter]` (ctx + $/1M), default `openrouter/auto`. Live `/models` today: 422 models, 23 free (`:free`), almost all with tools + reasoning; routers `openrouter/auto`, `openrouter/free`.
**Decide:**
- `/model` UX: fuzzy picker with recent/favorites/free filter, aliases (`sonnet`, `gpt`, `gemini`, `glm`…), show ctx/price/speed; `kolk model <id>` as the top-level command.
- Fresh start = **all free OpenRouter models preloaded**: compute at first run from `/models` (pricing 0 + tools + reasoning), rank by capability (params/ctx/recency), map to effort tiers automatically, refresh on a TTL, fallback `openrouter/free`; handle free rate limits (per-minute/day) by rotating or queueing, with a clear message.
- The **fast lane**: a dedicated "tiny tasks" model (titles, summaries, intent/mode suggestion, commit messages, compaction) chosen by throughput/latency (OpenRouter `sort: throughput`/`:nitro`, endpoints stats, rankings page) — free by default, paid upgrade optional. *Research:* the endpoints API gives `throughput_last_30m`/`latency_last_30m`/uptime + pricing per endpoint, so this is fully automatable; combine with Hermes-style named slots (item 14).
- Per-role models in agent mode: planner / subagent / synthesizer / critic can differ (item 14).
- Auto-routing flywheel: later, suggest/route by your own dashboard ratings ("your best-rated cheap model for chat"). Never silently change a pinned model.
- Model profiles: per-model caps (max output, supports structured output, vision) and the quirks list.
**Hardened when:** selection UX spec, default-catalog algorithm (with today's live list as fixture), fast-lane policy, rate-limit strategy.
**Inputs:** `docs/research/openrouter.md`, `docs/research/orcli.md`

### [ ] 9. Command surface — few, obvious, typeable
**Scope:** the top-level commands and slash commands; what stays out; the reserve list.
**Today:** top-level `config models sessions stats`; slash `/mode /effort /model /rate /yolo /new /session /changes /rewind /help /exit`; flags `-m --mode -e -y -r -s --base-url -p`.
**Decide:**
- Core verbs (your list): `model`, `effort`, `config`, the loop (item 10) — plus what *must* exist: `login`, `sessions`, `stats`/`dash`, `models`, `help`, `version`, `doctor`, `update`. Everything else waits.
- Parity rule: every top-level verb has a slash twin inside the REPL (`kolk model x` ≡ `/model x`), same argument grammar.
- Non-interactive: `kolk -p "…"` / `--json` / `--output stream-json` for scripting and for the future desktop/iPad client; exit codes.
- Shell completions (bash/zsh/fish), `kolk help` as the single source of truth (generated from command defs).
- The **reserve list** (checkpoint for later): `mcp`, `skills`/`commands`, `hooks`, `worktree`, `export`, `compact`, `undo`, `diff`, `cost`, `profile`, `theme`, `serve`.
- Naming guardrails: one word, lowercase, ≤ 6 letters, no synonyms for the same thing.
**Hardened when:** a command table (verb, args, slash twin, flag, since-version) and the reserve list.
**Inputs:** `docs/research/ecosystem.md`

### [ ] 10. The careful-progression loop (working name: `saga`)
**Scope:** your favourite Claude Code `/loop`, redesigned as *careful progression, update by update*, with an easy-to-type name.
**Today:** nothing.
**Decide:**
- Name: `saga` (Old Norse: a long tale advanced chapter by chapter — each iteration is a "chapter" with a report; 4 letters) vs `vard` (vörðr, "warden") vs `careful`/`steady`/`bulletproof`. Recommendation: **`saga`**, progress file `SAGA.md`, iterations = chapters.
- Semantics: plan → do one bounded step → verify (tests/build/lint/self-check) → checkpoint → report a human-readable chapter → decide continue/stop. Self-paced by default, optional interval; explicit stop conditions (done-criteria met, max chapters, max $ budget, max time, N consecutive no-progress chapters).
- Progress artifact: `SAGA.md` (goal, acceptance criteria, chapter log, open risks) that survives restarts and is the resume point; `kolk saga --resume`.
- Safety: per-chapter checkpoint + `/rewind` of a chapter; tool allow-list for unattended runs; optional git worktree/branch isolation; never `yolo` beyond the declared scope; notification on stop (terminal bell / macOS notification / webhook later).
- Effort interplay: escalate effort when stuck, drop for mechanical chapters; per-chapter model routing via item 14.
- How it differs from agent mode (saga = longitudinal, one goal over many chapters; agent = one request fanned out to subagents) and whether saga can use agent mode inside a chapter.
- *Research blueprint:* Hermes `/goal` = judge loop + completion contract + shell **quality gates** + turn budget + a `wait` verdict that parks on background processes; its `/loop` = fixed or self-paced (1→15 min backoff) with `--times/--until/max_ticks`; Ralph hygiene = one task per iteration + progress file + commit-on-green. Decide which of these saga absorbs vs. exposes as separate primitives (`saga` vs plain `loop` vs cron). Pitfall: inject loop wakeups as user turns, not system-prompt edits (prompt-cache).
- UX: `kolk saga "goal"` / `/saga "goal"`; `kolk saga status|stop|resume`; live view of chapter progress; cost so far.
**Hardened when:** state machine + `SAGA.md` format + stop conditions + CLI spec + 2 worked examples.
**Inputs:** `docs/research/ecosystem.md` (loop/autonomy patterns)

### [ ] 11. REPL / TUI & input
**Scope:** how it feels to type in kolk; rendering; keybindings.
**Today:** single-line stdin REPL, ANSI colors, streaming plain text, cost footer, Ctrl+C = interrupt turn.
**Decide:**
- Hand-rolled ANSI vs Bubble Tea v2 (+ Lip Gloss, Glamour for markdown) vs tview; impact on startup, binary size, Windows, and the "thin client" architecture (item 2). The TUI must be *one* frontend over the core protocol. *Research:* Bubble Tea v2.0.9 / Lip Gloss v2 / Glamour v2 are stable (`charm.land/*`); Crush's `internal/ui/AGENTS.md` playbook (single top-level model, cached message items, per-tool renderers, dialog overlay stack, stable-prefix markdown re-render, unified/split diff auto ≥140 cols) is the reference.
- Input: multiline (Shift+Enter / backslash / heredoc), history, `@file` mentions with completion, paste handling, `/` command completion.
- Output: streaming markdown, code blocks with syntax highlight, diff view for edits (before confirm), tool-call collapsibles, reasoning/thinking display toggle.
- Keys: Ctrl+C (interrupt turn) vs Esc, Ctrl+D exit, Shift+Tab cycle mode/permission, Ctrl+O verbose, Ctrl+R history search.
- Status line: mode · effort → model · session · context % · cost this session; themes / `NO_COLOR`; minimal "quiet" mode for `-p`.
- Confirm prompts: allow once / allow for session / always for this tool+pattern / deny; show diffs/commands in full.
**Hardened when:** TUI framework decision with a spike, keymap table, render spec, confirm UX.
**Inputs:** `docs/research/ecosystem.md`, `docs/research/platform-strategy.md`

### [ ] 12. Sessions, context & memory
**Scope:** persistence, resume, compaction, memory files.
**Today:** JSON sessions, atomic save, `-r`/`-s`, list/rm/clear, title from first input, dangling tool-call repair.
**Decide:**
- Storage: JSON files vs SQLite (shared with the dashboard, item 17); migration path; one DB for everything?
- Features: `sessions` list/search/rename/fork/export (md/json), per-project session scoping (cwd-aware resume), auto-title via the fast lane.
- Context management: token accounting per model, auto-compaction at threshold with a summary strategy (keep plan, decisions, file list; drop tool noise), `/compact` manual, tool-output truncation rules, conversation rewind (`/undo` last turn incl. messages).
- Memory layers: project (`KOLKRABBI.md`/`AGENTS.md`), user-level (`~/.config/kolk/memory.md`?), and "save this" command; size caps; what the agent may write back.
**Hardened when:** storage decision, compaction algorithm, memory precedence, session command spec.
**Inputs:** `docs/research/dashboard.md`, `docs/research/ecosystem.md`

### [ ] 13. Tools, permissions & sandboxing
**Scope:** what the model can do to your machine and how that is controlled.
**Today:** 5 tools; confirm on bash/write/edit; `-y` yolo; checkpoints on write/edit only; 120 s bash timeout; 12 k char output cap.
**Decide:**
- Tool set v1: add `grep`/`glob` (ripgrep-like in Go), `multi_edit`/patch, `web_fetch`, `web_search` (OpenRouter `:online` or a plugin), background `bash` with streaming output, `read` with ranges; later MCP tools (item 16).
- Permission model: rules (`allow bash(git *)`, `deny write(~/.ssh/*)`), scopes (once / session / project / always), yolo with guardrails, dangerous-command heuristics, path jail to the project root by default, network toggle. *Research:* OpenCode's allow/ask/deny globs with last-match-wins + a `doom_loop` rule; Hermes's hardline blocklist that survives `/yolo` and auto-deny inside subagents (avoids approval deadlock) — adopt all three.
- Sandboxing options: none (default, ask), macOS `sandbox-exec`/seatbelt profile, Linux bubblewrap/landlock, Docker/devcontainer execution; how sandbox relates to yolo (yolo inside sandbox = safe default for saga).
- Checkpoints for bash-made changes: git-based snapshot per turn when inside a repo; otherwise warn.
- Tool output handling: truncation, large-file paging, binary detection, secrets redaction.
**Hardened when:** tool catalog (schema + risk + confirm + checkpoint), permission-rule grammar, sandbox matrix per OS.
**Inputs:** `docs/research/ecosystem.md`

### [ ] 14. Agent mode — orchestration & per-task model routing ("Hermes-style, but multi-model")
**Scope:** planner → subagents → synthesis done well; different models for different tasks; what to borrow from Hermes Agent.
**Today:** planner (strict-JSON task list) → sequential subagents (isolated contexts, ≤ 12 rounds) → synthesis; width by effort; main session only stores request → answer.
**Decide:**
- Parallel subagents: concurrency, shared confirm UX (queue prompts; yolo-in-sandbox default), per-subagent streaming panes vs a log.
- Per-task routing: a task classifier (fast lane) tags tasks (`edit`, `test`, `research`, `explain`, `design`, `boilerplate`) → model per tag from config + your dashboard ratings; cost-aware; always overridable.
- Roles & models: planner, subagent(s), synthesizer, critic/verifier (optional, effort-gated); retries with escalation; budget caps per run.
- Isolation: context isolation (done), plus optional file isolation (git worktree per subagent) and merge/review step.
- Hermes ideas in/out of scope: skills/tool registries, long-term memory, messaging gateways, scheduled tasks — decide explicitly. *Research:* Hermes `delegation.*` defaults worth copying (max_concurrent_children 3, spawn depth 1, max_iterations 50, optional worktree isolation, summary-only return, children get parent toolsets minus delegate/memory/messaging); category-based routing (oh-my-openagent) as the routing mechanism; Mixture-of-Agents as a possible `ultra` preset.
- Failure handling: a failed subagent's partial results, timeouts, and how the synthesis reports uncertainty.
**Hardened when:** orchestration state machine, routing table format, concurrency/confirm design, cost-cap rules.
**Inputs:** `docs/research/ecosystem.md` (Hermes section)

### [ ] 15. Code mode specifics (Claude Code-style loop, done right)
**Scope:** the coding experience: editing quality, feedback loops, repo awareness, plan-first.
**Today:** read/write/edit/list/bash with unique-match edits; checkpoints; project memory.
**Decide:**
- Repo awareness: git status/diff in context, `.gitignore`-aware listing/search, file tree summary, language detection.
- Editing: diff preview before confirm, multi-hunk edits, whitespace-tolerant matching, create-vs-overwrite guard, formatter hooks after edit (gofmt/prettier).
- Feedback loops: detect test/build commands per language, "run tests after edits" policy per effort, lint integration.
- Plan mode: `/plan` (read-only exploration → plan → approve → execute), and how it relates to effort `max`.
- Conversation + file rewind together (`/undo`), `/diff` of the session's changes, commit helper (`/commit` drafts a message via the fast lane).
- Effort switching inside code mode (explicit requirement): `/effort` mid-task re-resolves the model for the next call; show it.
- Later: LSP/tree-sitter-powered symbols, test-failure parsing.
**Hardened when:** code-mode feature list by version, edit-tool spec, plan-mode flow.
**Inputs:** `docs/research/ecosystem.md`

### [ ] 16. Extensibility — MCP, skills/commands, hooks
**Scope:** how users add capabilities without forking.
**Today:** none.
**Decide:**
- MCP client (stdio + HTTP): `kolk mcp add/list/rm`, tool namespacing, permission rules per server, timeouts. Priority vs. v1?
- Skills/commands: Markdown-defined slash commands (`.kolk/commands/*.md`, `~/.config/kolk/commands/`), argument substitution, compatibility with Claude Code's `.claude/commands` format (import?).
- Hooks: pre/post tool-call shell hooks (lint after edit, notify on stop), config format, safety.
- Plugin boundary: keep the core small; plugins = MCP + markdown + hooks, no dynamic Go loading.
**Hardened when:** which of the three ship in v0.x, the file formats, and the permission story for third-party tools.
**Inputs:** `docs/research/ecosystem.md`

## C. Data

### [ ] 17. The local dashboard — model efficiency, session by session (Braintrust, but local and yours)
**Scope:** what we record, how we score, what we show, where it lives. 100% local, zero telemetry.
**Today:** `stats.jsonl` (per call: model, mode, effort, role, tokens, cost, ms, tool_calls) + `/rate` rows; `kolk stats` per-model table (calls, tokens, cost, avg ms, rating, modes).
**Decide:**
- Data model: SQLite (pure-Go driver, WAL) vs JSONL + derived views; tables sessions / turns / spans (llm_call, tool_call, subagent) / scores / tags; mirror OpenTelemetry GenAI attributes so OTLP export is trivial later; price snapshots per model over time. *Research recommendation:* `modernc.org/sqlite` + keep `stats.jsonl` as raw log; start from the 7-table schema in `docs/research/dashboard.md` §4 — confirm or amend it.
- Signals to capture automatically: tokens (incl. cached), cost, latency (TTFT + total), tool calls/errors, rounds per turn, rewinds (negative), tests passed after edit (positive), user re-asks, interruptions.
- Scoring: manual `/rate`, implicit signals, optional LLM-judge via the fast lane (off by default; cost shown), custom scorers per session.
- "Session by session, depending on the user's needs": per-session goal/tags, choose which scorers apply, compare models on the same prompt (A/B / `kolk compare`), per-mode/effort/role breakdowns, cost per accepted edit, rating per $, latency percentiles, trend over time.
- Delivery: `kolk stats` (table, now) → `kolk dash` (embedded web UI on localhost, single binary, light charting) → export CSV/JSON/OTLP; TUI view optional.
- Operations: retention/pruning, DB size, schema migrations, privacy guarantees written down (nothing leaves the machine; redaction of prompts optional).
**Hardened when:** schema DDL, signal list, v1 views (top 5), dashboard delivery decision, migration from `stats.jsonl`.
**Inputs:** `docs/research/dashboard.md`

### [ ] 18. Config system
**Scope:** formats, locations, precedence, UX.
**Today:** `~/.config/kolk/config.json` {api_key, model, base_url, tiers}; env overrides; `kolk config set-* | show`.
**Decide:**
- Format: JSON (now) vs TOML/YAML/JSON5 (comments!) — and a schema with validation + `kolk config doctor`.
- Layers & precedence: flags > env > project `.kolk/config.*` > user > defaults; per-mode and per-profile sections; secrets separated from config (item 5).
- UX: `kolk config get/set/unset/edit/path/show`, dotted keys (`code.effort.high.model`), `/config` in-REPL read-only view.
- Migration of the prototype's file; versioned config with auto-upgrade.
**Hardened when:** config schema document + precedence table + migration note.
**Inputs:** `docs/research/ecosystem.md`

## D. Platform & delivery

### [ ] 19. Desktop & iPad path (decided early, built later)
**Scope:** don't block the future; pick the shape now, build after CLI v1.
**Today:** nothing (CLI only).
**Decide:**
- Desktop: Wails v3 (Go-native webview; **in Beta**, beta.12 2026-08-21) vs Tauri v2 + Go sidecar vs Electron + sidecar; what desktop adds (dashboard, session browser, multiple parallel sessions, notifications). Shared protocol = the daemon API from item 2; defer the shell choice until that protocol exists.
- iPad: iPadOS apps can't spawn shells/toolchains → code mode on iPad means **remote execution** (kolk daemon on a Mac/Linux box over Tailscale/SSH) or chat-only local; Swift client vs gomobile-bound core; App Store constraints. Pragmatic v1: run kolk on your Mac, use it from iPad via a terminal app (Blink/Termius + mosh) + the web dashboard over Tailscale.
- Protocol requirements these impose on the core *now*: streaming events, session multiplexing, auth on localhost, versioning.
**Hardened when:** a one-page platform strategy with the chosen stacks, what ships when, and the protocol constraints list.
**Inputs:** `docs/research/platform-strategy.md`

### [ ] 20. Distribution, updates & CI
**Scope:** how people get and update kolk.
**Decide:**
- Install paths: `curl | sh` script, Homebrew tap, `go install`, GitHub Releases (goreleaser), later scoop/winget/AUR; checksums/signing; macOS notarization (needed once there's a desktop app; CLI maybe).
- `kolk update` self-update vs package managers only; version check (opt-in, local-only otherwise).
- CI: test matrix (macOS/Linux), lint (golangci-lint), release on tag, size/startup budget checks, weekly live smoke test against free models (opt-in secret).
**Hardened when:** release pipeline spec + install docs + budget checks in CI.
**Inputs:** —

### [ ] 21. Quality, testing & security
**Scope:** how we stay correct as it grows.
**Today:** 22 offline tests incl. e2e via `mockrouter`; SSE fragmentation covered.
**Decide:**
- Test pyramid: unit, `mockrouter` e2e per mode/effort/saga, golden output tests for the TUI, fuzzing the SSE/tool-arg parsers, property tests for edit tool; optional live tests.
- Error UX matrix: 401/402/404/408/429/5xx, network drop mid-stream, model lacking tool support, context overflow — each with a clear message and a next action.
- Observability for us: `--debug` log file with redaction, `kolk doctor` (keys, network, model access, terminal caps).
- Security review checklist: secrets, command injection via tool args, path traversal, prompt injection from files/web, supply chain (deps policy, pinned actions).
**Hardened when:** test plan + error matrix + security checklist committed.
**Inputs:** —

### [ ] 22. Onboarding & docs
**Scope:** the first 60 seconds and the reference.
**Decide:**
- First run: no key → offer "login with OpenRouter" or paste; free models preloaded and mapped to efforts; pick a default mode; show `/help` once; a 3-line "how to switch mode/effort/model".
- Docs: README (vision + quickstart), `docs/` (commands, config, modes, effort, saga, dashboard, providers, subscriptions policy), demo GIFs (vhs), CHANGELOG.
- Built-in help: `kolk help <topic>` generated from the command table; `/help` contextual per mode.
**Hardened when:** onboarding flow script + docs outline + README rewrite.
**Inputs:** `docs/research/orcli.md`, `docs/research/ecosystem.md`

### [ ] 23. Roadmap, phasing & explicit non-goals
**Scope:** order of work and what we refuse to do (for now).
**Decide:**
- Phases (proposal): **v0.1** polish prototype → module path/CI, `model`/`effort` verbs, free-model defaults, effort per mode, stats kept; **v0.2** TUI + multiline, sessions/compaction, `saga`; **v0.3** dashboard (SQLite + `kolk dash`), MCP, parallel agent mode + routing; **v0.4** subscription backends if permitted, sandboxing; **v1.0** daemon API frozen, desktop; **later** iPad.
- Definition of done per phase; what's measured (startup, binary size, test count, cost per task on the dashboard).
- Non-goals for now: Windows polish, plugins in Go, cloud sync, telemetry of any kind, a hosted service.
**Hardened when:** phased roadmap with DoD and the non-goals list, reflected in README and GitHub milestones.
**Inputs:** all of the above

---

## Appendix A — research inputs (2026-08-21)

Filled in from the research agents; full reports in `docs/research/`.

- `docs/research/orcli.md` — review of github.com/Theanlegendary/orcli (read over HTTP only; nothing was cloned or executed locally) + similar OpenRouter CLIs.
  Takeaways: it is a 1.3k-line single-file Python hobby script (0★, 3 commits, no tests) — not a
  reference architecture, but four ideas are worth taking: (1) free-tier fallback chain that
  auto-hops to the next free model on 429/503 with a per-turn "tried" set and ✗ markers in the
  picker; (2) `/auth/key` credit check at startup + `/credits`; (3) a degeneration/loop detector
  that aborts a repeating stream and retries on another model; (4) `/retry` = resend on the next
  model/effort notch. Anti-patterns to avoid: executing fenced code blocks from chat with a
  default-yes prompt, writing keys into `~/.zshrc` + 0644 JSON, scraping `<think>` tags instead
  of the `reasoning` field. Better references found: charmbracelet/crush (Go, 27.6k★),
  charmbracelet/mods, sigoden/aichat, simonw/llm (SQLite call log — dashboard data-model
  reference), lwlee2608/tokentop (Go OpenRouter usage TUI).
- `docs/research/ecosystem.md` — OpenCode, Crush, Goose, Hermes Agent, Codex CLI, Aider, Gemini CLI, Cline, Kilo, Amp…: features worth borrowing, loop/autonomy patterns.
  Takeaways (top of the ranked list): (1) **named model slots** — Hermes configures
  `auxiliary.<task>.{provider,model,reasoning_effort}` per task (vision, compression, approval,
  title, judge…) and `delegation.*` for subagents; Crush has large/small roles; this is exactly
  our "multiple models depending on task" — make slots a first-class config concept that the
  effort dial fills with defaults. (2) **category delegation** — subagents ask for a category
  (quick/deep/visual…), a router picks the model. (3) **autonomy = three primitives** — Hermes
  splits it into `/goal` (judge-driven completion contract + shell quality gates + budget +
  wait-on-background-PIDs), `/loop` (timer or self-paced with backoff, `--times/--until`,
  `max_ticks`), and cron; Ralph hygiene on top (one task per iteration, progress file,
  commit-on-green) — a straight blueprint for `saga`. (4) permissions as allow/ask/deny globs,
  last-match-wins, once/session/always, `--yolo` that still respects a hardline blocklist, and a
  doom-loop rule. (5) Crush proves the Go TUI stack is ready: Bubble Tea v2.0.9 stable +
  Lip Gloss v2 + Glamour v2 (`charm.land/*`), stable-prefix markdown streaming, unified/split
  diffs, and its provider layer (`fantasy`) + auto-updated model DB (`catwalk`) are reusable.
  Pitfalls recorded: MCP schema bloat, loops without caps, git-dependent undo outside repos,
  system-prompt mutation killing prompt caches, weak models failing at exact-match edits.
- `docs/research/subscription-auth.md` — what Anthropic/OpenAI/Google permit for subscription-backed third-party CLIs (as of Aug 2026).
  Takeaways (from vendor primary docs, quoted inside): extracting/intermediating Claude OAuth
  tokens and offering claude.ai login in a third-party product are **explicitly prohibited**
  ("unless previously approved"), but Anthropic's own docs bless the shape kolk wants: an end
  user signing in to the **unmodified** Claude Code binary with their own subscription, and
  other languages driving the agent loop **"as a subprocess with `-p` and
  `--output-format json`"**. So: `kolk login claude` = detect the user's own `claude` install,
  have them log in through Anthropic's flow, then spawn `claude -p --output-format stream-json`
  as an "external agent CLI" backend — kolk never touches tokens, and brands it "Claude Agent",
  never "Claude Code". Gemini CLI's ToS *explicitly* forbids third-party reuse of its OAuth
  ("using OpenClaw with Gemini CLI OAuth is a violation") → Gemini backend is API-key only.
  OpenAI: OpenCode publicly ships ChatGPT sign-in and Codex offers `exec`/app-server surfaces —
  gray, verify OpenAI terms before building. Full verdict table + conditions in the file.
- `docs/research/openrouter.md` — free tier limits, routing primitives, reasoning param, cost/usage endpoints, OAuth PKCE, rankings/benchmarks.
  Takeaways: free = 20 req/min and 50/day (<$10 lifetime credits) or 1000/day (≥$10) — surface
  this in onboarding/doctor. The per-model **endpoints API** exposes `throughput_last_30m`,
  `latency_last_30m`, `uptime_last_5m/30m/1d` + per-endpoint pricing (incl. cache reads) → the
  fast lane and "cheapest capable" picks are fully programmable; `:nitro` = sort throughput,
  `:floor` = sort price; `openrouter/auto` classifies into ~30 task categories with session
  stickiness. Unified `reasoning.effort` (none…xhigh/max) maps cleanly onto our effort dial, and
  **`reasoning_details` must be preserved verbatim across tool calls** (prototype gap). Exact
  cost comes in-stream via `usage.include`; cached tokens in `prompt_tokens_details`. OAuth PKCE
  is CLI-friendly (localhost callback on any port, headless code-paste variant) → "login with
  OpenRouter" beats paste-a-key for onboarding. Mid-stream failures arrive as
  `finish_reason:"error"` SSE events with a typed `error_type` vocabulary (second prototype
  gap); 429/503 carry Retry-After. Attribution now documents `HTTP-Referer` (required) +
  `X-OpenRouter-Title` (prototype sends legacy `X-Title`).
- `docs/research/dashboard.md` — Braintrust concept map → local equivalent; SQLite schema proposal; OTel GenAI conventions.
  Takeaways: Braintrust's useful core for a solo dev is traces/spans + scores (human, code,
  LLM-judge with sampling) + experiment/compare + monitor charts; datasets/topics/RBAC/BTQL are
  team noise. Recommended storage: **SQLite via pure-Go `modernc.org/sqlite`** (WAL, cgo-free;
  ~1.6× slower than mattn on bulk inserts — irrelevant at our volume), keep `stats.jsonl` as the
  append-only raw log. Proposed tables: `sessions / turns / spans / scores / price_snapshots /
  scorer_configs / session_scorers`, with column names mirroring **OTel GenAI** attributes
  (`input_tokens`, `cache_read_tokens`, `operation_name`, `provider_name`, `conversation_id`) so
  `kolk export --otlp` is a column→attribute map later (conventions still "Development" status in
  2026). Implicit signals to steal from Claude Code's own OTel metrics: edit accept/reject, tool
  success/duration, effort per request. Delivery: `kolk dash` = embedded SPA + **uPlot (22 KB gz)**
  on localhost; top-5 v1 views: model leaderboard (rating/$, $/accepted turn, p50/p95), cost &
  tokens over time by model, session drill-down timeline, effort-tier vs rating/cost scatter, A/B
  replay diff. Privacy default: store prompt hashes, not prompts (opt-in). Precedents: ccusage,
  tokscale, promptfoo, OpenCode's SQLite move, Claude Code `/insights` static HTML report.
- `docs/research/platform-strategy.md` — Go verdict, core/daemon/frontends, desktop (Wails/Tauri) and iPad (remote execution) paths.
  Takeaways: **Go — yes** (measured: 6.1 MB binary, ~10 ms startup; Bubble Tea v2 stack is
  stable and proven by Crush; OpenAI-compatible HTTP+SSE needs no SDK ecosystem; pure-Go SQLite
  exists). Caveats: tree-sitter/LSP later, pin `charm.land` versions, Windows later.
  Architecture: core library + `kolk serve` daemon (versioned JSON event stream over HTTP+SSE/WS,
  localhost token auth) with TUI as client #1 — desktop and iPad become clients, not rewrites.
  Desktop: Wails v3 is **Beta** (beta.12 on 2026-08-21) — decide Wails vs Tauri-sidecar only
  when the daemon protocol exists. iPad: App Review 2.5.2 rules out on-device toolchains → code
  mode on iPad = remote execution against the daemon; pragmatic v0 = Blink/mosh + `kolk dash`
  over Tailscale; gomobile (experimental) optional, never critical path.

## Appendix B — assumptions to confirm

- "lolkrabbi" in your message was a typo for **kolkrabbi** (tarball, directory and README all say kolkrabbi).
- GitHub owner = `onembyte` (the account `gh` is logged into here); nothing has been created or `git init`-ed yet — item 1 does that when you say go.
- Free-model defaults are computed live from OpenRouter (the list churns), not hard-coded.
- The subscription-auth findings rest on vendor docs fetched 2026-08-22 (quoted in
  `docs/research/subscription-auth.md`); news coverage of the Jan-2026 OpenCode enforcement was
  not separately fetched (session's web-search budget was exhausted) — the primary docs are
  explicit enough, but re-verify before implementing item 4.
