# Research: the terminal AI agent ecosystem (mid-2026) — what to borrow for kolk

Date: 2026-08-22. Method: WebFetch of official docs + `gh api` on repos (WebSearch budget ran out
mid-task, so everything is from primary docs/repos). ★ = GitHub stars that day. Feeds PLAN.md
items 6, 7, 8, 9, 10, 11, 13, 14, 15, 16, 18, 22.

## A) Snapshots

- **Hermes Agent** (NousResearch, Python/MIT, 234k★; https://github.com/NousResearch/hermes-agent,
  docs https://hermes-agent.nousresearch.com/docs/). One `AIAgent` loop (`run_agent.py`) behind
  three API modes (chat_completions / codex_responses / anthropic_messages); ~70 self-registering
  tools in ~28 toolsets; default CLI is prompt_toolkit, `hermes --tui` is React+Ink talking
  JSON-RPC to a Python `tui_gateway`. One gateway process serves 25+ chat platforms with shared
  SQLite+FTS5 sessions, cron (per-job model pin → `cron.model` → default), DM pairing.
  **Model routing**: `/model provider:model`, 50+ providers, `fallback_providers` chain,
  OpenRouter `provider_routing` (sort price/throughput, `:nitro`/`:floor`, Pareto code router),
  **per-task `auxiliary.<task>.{provider,model,reasoning_effort}` slots** (vision, compression,
  approval, title_generation, web_extract, goal_judge, background_review…),
  `delegation.{model,provider,max_concurrent_children=3,max_spawn_depth=1,max_iterations=50,
  worktree_isolation}` (subagents: fresh context, parent toolsets minus
  delegate/clarify/memory/send_message/cronjob, summary-only return), Mixture-of-Agents virtual
  provider (reference models → aggregator). Memory: char-capped MEMORY.md/USER.md frozen per
  session + `memory` tool + background review on a cheaper model; skills = agentskills.io
  SKILL.md with 3-level progressive disclosure, agent-created skills + curator. Approvals:
  `approvals.mode` smart(aux-LLM risk)/manual/off, hardline blocklist survives `/yolo`; 7
  terminal backends. Autonomy: `/goal` (judge loop, completion contract, shell **quality gates**,
  20-turn budget, `wait` verdict parks on background PIDs), `/loop` (fixed or self-paced 1→15 min
  backoff, `--times/--until`, `LOOP_COMPLETE`, `loops.max_ticks=100`), `/heartbeat`, cron,
  kanban. Opt-in shadow-git checkpoints (`/rollback`). Status bar: model, ctx-fill bar, est.
  cost, compression count, bg tasks; `/usage /insights /context`; `hermes chat -q`.
- **Crush** (Go, 27.6k★; https://github.com/charmbracelet/crush). Bubble Tea **v2.0.9 stable**,
  Lip Gloss v2.0.5, Glamour v2, Bubbles v2 + `ultraviolet` screen buffer (import path
  `charm.land/*`). Providers via **fantasy**
  (openai/anthropic/google/azure/bedrock/openrouter/vercel/openaicompat) + **catwalk** model DB
  (auto-updated). `internal/ui/AGENTS.md`: single top-level Bubble Tea model, imperative
  sub-components, cached message items, per-tool renderers, dialog overlay stack;
  `streaming_markdown.go` re-renders only the trailing unstable markdown; diffview
  unified/split (auto ≥140 cols). Permission dialog = allow / allow-for-session / deny with
  diff preview; `permissions allow/deny`, `--yolo`; hooks `PreToolUse` (Claude-compatible);
  `large`/`small` model roles (`crush run -q -m --small-model -C`); coder + read-only task agent;
  skills (agentskills, `user-invocable`); `crush stats` HTML token/cost report; ctrl+l model
  picker, ctrl+p palette, reasoning-effort dialog.
- **OpenCode** (now **anomalyco/opencode**, TS, 200k★; https://opencode.ai/docs). Agents
  build/plan (Tab), subagents general/explore/scout via `@`, per-agent model/permission;
  permissions allow/ask/deny with globs, last-match-wins, `doom_loop` rule, `--auto`, TUI
  once/always/reject; `/undo`/`/redo` git-backed; leader `ctrl+x`; ctrl+t cycles reasoning
  variants; `opencode run --format json`, `opencode stats --days --tools --models`; Zen gateway;
  md commands with `$ARGUMENTS`, `` !`cmd` ``, `@file`.
- **Goose** (Rust, now aaif-goose under Linux Foundation AAIF, 53k★; https://goose-docs.ai).
  `/mode auto|approve|smart_approve|chat`, per-tool always/ask/never; `goose run -t/-i
  --max-turns --no-session`, recipes, `goose schedule`; subagents (parallel/sequential, 5-min
  timeout, only in auto mode); `GOOSE_PLANNER_MODEL` for `/plan` (lead/worker env vars
  **unverified**, page 404); auto-compact 80%; Open-Plugins hooks.
- **Codex CLI** (Rust, 111k★; https://learn.chatgpt.com/docs/…). `approval_policy`
  untrusted/on-request/never, `sandbox_mode` read-only/workspace-write/danger-full-access,
  `model_reasoning_effort` minimal…xhigh, `/model` sets model+effort; `/plan`, **`/goal`**
  (persistent goal with token budget; app-server `thread/goal/*`), `/review`, `/fork`, `/btw`,
  `/ps`; subagents as TOML (`model`, `model_reasoning_effort`, `sandbox_mode`), built-ins
  default/explorer/worker, `agents.max_concurrent_threads_per_session`,
  `default_subagent_model`; `codex exec --json --output-schema --output-last-message`.
- **Gemini CLI** (TS, 106k★): Shift+Tab Default→Auto-Edit→Plan, `/rewind` (Esc Esc;
  conversation and/or code), shadow-git checkpoints, "auto" Pro/Flash model routing + fallback +
  experimental local Gemma router, subagents `@name`, headless `--output-format stream-json`,
  exit codes 0/1/42/53.
- **Aider** (Python, 48k★, last release Aug 2025 — stale): architect+editor two-model mode,
  `/weak-model`, `/reasoning-effort`, git-commit `/undo`.
- **Cline CLI** (66k★): `-p` plan-first, `--auto-approve`, `--json` NDJSON, `--thinking`, `-t`
  timeout, `cline schedule/kanban`. **Kilo CLI**: OpenCode fork, 500+ models via gateway,
  `kilo run --auto` exit 0/124/1, `/stats`. **Amp** (closed): modes low/medium/high/**ultra**,
  auto model routing, oracle second-opinion model, `-x`, `--stream-json`, no prompts by default,
  self-scheduling agents.
- **pi** (earendil-works/pi, TS, 95k★): 4 tools, no MCP/subagents/plan/permissions built in
  (extensions), JSONL session tree `/tree /fork /clone`, steering vs follow-up queue, footer
  ↑↓ R W CH cost ctx, `-p/--mode json/--mode rpc`.
- **Claude Code**: `/loop [interval] [prompt]` (self-paced when interval omitted, alias
  `/proactive`), `/effort low…xhigh/max/ultracode/auto`, `/rewind`, `/fork`, `/batch` (parallel
  worktree subagents), Ralph plugin = Stop hook re-feeds prompt with
  `--max-iterations/--completion-promise`.
- **Newcomers**: OpenClaw (387k★, assistant gateway), OpenAI Symphony (Elixir, spec-driven
  autonomous runs from Linear issues), oh-my-openagent (68k★; **category-based routing**
  quick/deep/ultrabrain/visual-engineering → model, ultrawork, Team Mode, hashline edits), Gas
  Town (Go). Qwen Code/Kimi/Mistral Vibe/Copilot CLI/Warp not examined.

## B) Ideas for kolk (ranked, value/effort)

1. **Named model slots** (Hermes `auxiliary.*`, Crush large/small, Codex subagent defaults):
   orchestrator/worker/explore/judge/compressor/title/vision, each = OpenRouter model + effort;
   the effort dial sets slot defaults. High/med.
2. **Category delegation** (OmO/Codex): subagents request a category, router picks model.
   High/low.
3. **Loop = three primitives** (Hermes): judge-driven `/goal` with completion contract + shell
   gates + budget + wait-on-PID; timer/self-paced `/loop` with `--times/--until/max_ticks`; cron
   outside session. Add Ralph hygiene: one task/iteration, progress file, commit-on-green,
   checkpoint each iteration. High/med.
4. **Permissions**: allow/ask/deny globs, last-match-wins, once/session/always prompt with diff,
   `--yolo` + hardline blocklist, doom-loop rule, read-only plan agent. Med/low.
5. **Shadow-git checkpoints → /undo /redo /rewind** (Hermes/Gemini/OpenCode). Med/med.
6. **TUI from Crush's playbook** + fantasy/catwalk (or openaicompat for OpenRouter);
   stable-prefix glamour streaming; unified/split diff; reasoning dialog; ctrl+l/ctrl+p.
   High/low.
7. **Footer + `kolk stats`**: tokens ↑↓ cache-hit, cost, ctx bar, compression count, bg tasks →
   feeds local dashboard. Med/low.
8. **`kolk run -p --format json`** NDJSON events, exit codes, `--output-schema`, `--continue`.
   Med/low.
9. Input UX: `@file`, `!shell`, Shift+Tab mode/thinking, Ctrl+G editor, steering/follow-up
   queue, Esc Esc rewind, Ctrl+B background. Med/low.
10. Skills (agentskills, progressive disclosure), md commands, Claude-compatible PreToolUse
    hooks, Hermes `tool_search` bridge for MCP bloat. Med/med.
11. Compression on cheaper slot, 50–80% thresholds, protect first/last N, pressure notices.
    Med/low.
12. Fallback chain + OpenRouter `provider_routing`/Pareto; MoA preset as "ultra". High/low.

## C) Pitfalls

- MCP/tool schemas devour context (Hermes tool_search, Goose <25 tools).
- Loops without caps/judge fail-open + budget; placeholder implementations; parallel builds.
- Prompt fatigue vs yolo without blocklist; subagent approval deadlock (Hermes auto-denies in
  children).
- Git-dependent undo breaks outside repos.
- Mid-session system-prompt mutation kills prompt cache — inject loop wakeups as user turns.
- Weak models fail at edits: architect/editor split or hashline anchors.
- pi's warning: too many baked-in features; keep core small.

## D) Sources
hermes-agent.nousresearch.com/docs (architecture, providers, configuration, delegation, goals,
loops, heartbeat, security, memory, skills, cron, checkpoints, mixture-of-agents, tool-search) ·
github.com/charmbracelet/{crush,fantasy,catwalk,bubbletea,ultraviolet} (go.mod,
internal/ui/AGENTS.md, permission.go, streaming_markdown.go, stats.go, docs/hooks) ·
opencode.ai/docs/{agents,permissions,keybinds,cli,tui,models,commands,share} · goose-docs.ai +
aaif-goose/goose docs · learn.chatgpt.com/docs/{codex/cli,developer-commands,
non-interactive-mode,config-file/config-reference,agent-configuration/subagents} +
codex-rs/tui/src/slash_command.rs, app-server/README · google-gemini/gemini-cli/docs (commands,
model-routing, plan-mode, rewind, checkpointing, subagents, headless) ·
aider.chat/docs/usage/{commands,modes} · docs.cline.bot/cline-cli/overview · kilo.ai/docs/cli ·
ampcode.com/manual · github.com/earendil-works/pi · code.claude.com/docs/en/{commands,
interactive-mode} · anthropics/claude-code ralph-wiggum README · ghuntley.com/ralph · READMEs:
openclaw/openclaw, openai/symphony, code-yeongyu/oh-my-openagent, gastownhall/gastown.
