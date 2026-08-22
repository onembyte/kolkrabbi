# Research: the local model-efficiency dashboard (Braintrust → local equivalent)

Date: 2026-08-22. Method: direct WebFetch of primary docs (Braintrust, Langfuse, OTel semconv,
OpenRouter, SQLite driver repos). Feeds PLAN.md items 12 and 17.

## 1. Braintrust concept map → local kolk equivalent
(Glossary: https://braintrust.dev/docs/reference/glossary.md; SQL fields: https://braintrust.dev/docs/reference/sql/index.md)
- **Trace/span** (`span_attributes.type` = llm|tool|task|function|eval|score;
  `metrics.{prompt_tokens,completion_tokens,cost,start,end,latency}`) → kolk
  `sessions → turns → spans`. Their Claude Code plugin models exactly session-root → turn →
  tool-call via hooks (https://braintrust.dev/docs/integrations/developer-tools/claude-code.md).
- **Scores/scorers** (code 0–1 with name/metadata; LLM-judge with
  `{{input}}/{{output}}/{{expected}}/{{thread}}`, choice_scores map, use_cot, `__pass_threshold`)
  → `scores` table; `/rate` is a human-review score. Autoevals list:
  https://github.com/braintrustdata/autoevals.
- **Online scoring** (project rules, sampling %, trace/span scope, 30 s idle timeout, results as
  child score spans) → optional post-turn LLM-judge job with sampling; run async, never on the
  hot path.
- **Human review** (categorical/continuous/free-form → writes `expected`; keyboard `r`) →
  `/rate` + optional `/why` comment; keep it.
- **Experiments / compare** (immutable snapshot; baseline = most recent on same git branch; score
  delta column; comparison key = input; diff mode; group by metadata; trials) → "replay": re-run
  a stored turn's prompt against model B, diff outputs, compare score/cost/latency.
- **Playgrounds** (multi-task side-by-side, grades Improvement/Regression/Tradeoff/Tie) → same
  replay feature, keep minimal.
- **Monitor dashboards** (count, latency, tokens, cost, TTFT, scores; time series/top list/big
  number; click → filtered logs) → v1 charts.
- **Datasets, Topics (LLM clustering of logs), Loop (NL agent), BTQL/SQL, RBAC, retention,
  Pro $249/mo** → enterprise/team noise for a solo dev; the only cheap analog is "ask your own
  model over the SQLite file."

## 2. Borrowed ideas, ranked
1. **Non-overlapping usage buckets + price snapshots** (Langfuse `usage_details/cost_details`,
   ingested-cost beats inferred; model regex + pricing tiers):
   https://langfuse.com/docs/observability/features/token-and-cost-tracking
2. **Implicit signals that already exist in coding agents**: Claude Code OTel
   `claude_code.code_edit_tool.decision` (accept/reject), `tool_result.success/duration_ms`,
   `api_request.effort`, `lines_of_code.count`, "suggestion accept rate":
   https://code.claude.com/docs/en/monitoring-usage, https://code.claude.com/docs/en/analytics
3. **Score shape**: Langfuse `{name,value,string_value,data_type NUMERIC|CATEGORICAL|BOOLEAN|TEXT,
   source API|ANNOTATION|EVAL,trace_id|observation_id|session_id,comment}`:
   https://langfuse.com/docs/evaluation/evaluation-methods/custom-scores
4. **ccusage** 5-hour blocks, `--breakdown` per model, message-id dedup, LiteLLM pricing, JSON
   out, MCP server: https://github.com/ryoppippi/ccusage; pricing file
   https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json
5. **promptfoo** matrix (prompt × provider × test; tokens/latency/cost/tokens-per-sec; local
   SQLite): https://www.promptfoo.dev/docs/usage/web-ui/
6. **tokscale** TUI tabs (Overview/Models/Daily/Hourly/Agents) + heatmap; OpenCode already moved
   sessions into SQLite (`~/.local/share/opencode/opencode.db`):
   https://github.com/junhoyeo/tokscale; `opencode stats --days --models --project`:
   https://opencode.ai/docs/cli/
7. Claude Code `/insights` → local HTML report at `~/.claude/usage-data/report.html`
   (static-export precedent): https://code.claude.com/docs/en/costs
8. OpenLIT "OpenGround" side-by-side model compare with a pricing file:
   https://github.com/openlit/openlit. Phoenix: single container, SQLite default
   (https://arize.com/docs/phoenix/self-hosting/configuration). Langfuse/Opik/Laminar/Helicone
   all need ClickHouse+Postgres(+Redis/S3) — too heavy to embed; borrow schema, not stack.

## 3. OTel GenAI conventions (2026)
- Moved to https://github.com/open-telemetry/semantic-conventions-genai; spans/agent-spans/
  events/metrics all still **Status: Development** (not stable). Span name
  `"{gen_ai.operation.name} {gen_ai.request.model}"`; required `gen_ai.operation.name` (chat,
  execute_tool, invoke_agent, plan…), `gen_ai.provider.name` (no `openrouter` enum seen);
  recommended `gen_ai.usage.{input_tokens,output_tokens,cache_read.input_tokens,
  cache_creation.input_tokens,reasoning.output_tokens}`, `gen_ai.response.model`,
  `finish_reasons`, `time_to_first_chunk`; opt-in `gen_ai.input.messages/output.messages/
  system_instructions`; `gen_ai.tool.{name,call.id,call.arguments,call.result}`,
  `gen_ai.agent.{id,name}`, `gen_ai.conversation.id`; eval event
  `gen_ai.evaluation.{name,score.value,score.label,explanation}`.
- Recommendation: **name kolk columns after these attributes** (input_tokens, output_tokens,
  cache_read_tokens, operation_name, provider_name, conversation_id) — Braintrust
  (https://braintrust.dev/docs/integrations/sdk-integrations/opentelemetry.md) and Langfuse
  (https://langfuse.com/integrations/native/opentelemetry) both ingest exactly these, so a later
  `kolk export --otlp` is a column→attribute map. Go semconv v1.37.0 already exposes
  `GenAIRequestModel`, `GenAIUsageInputTokens`, etc.
  (https://pkg.go.dev/go.opentelemetry.io/otel/semconv/v1.37.0).

## 4. Storage + schema
- Driver: **modernc.org/sqlite** (v1.57.0, SQLite 3.53.3, BSD, cgo-free,
  `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`; pin `modernc.org/libc` same version).
  Bench (2026-03, https://github.com/cvilsmeier/go-sqlite-bench): modernc ~1.6× slower than
  mattn on bulk inserts, comparable on queries — irrelevant at kolk's ~10³ rows/day. mattn needs
  gcc/cross-compile pain; zombiezen (v1.4.2, no database/sql, has `sqlitemigration`) is a fine
  alternative; ncruces wasm = more memory. WAL: single writer, readers never block
  (https://www.sqlite.org/wal.html). Keep `stats.jsonl` as an append-only raw log
  (import/re-derive), SQLite as the queryable store.
- Tables (all times ms-epoch, ids ULID):
  - `sessions(id, started_at, ended_at, mode chat|code|agent, cwd, git_branch, goal TEXT,
    tags JSON, kolk_version)`
  - `turns(id, session_id, idx, user_prompt_hash, started_at, ended_at,
    outcome accepted|rewound|retried|abandoned, rating INT, rating_comment)`
  - `spans(id, session_id, turn_id, parent_id, kind llm_call|tool_call|subagent,
    operation_name, provider_name, request_model, response_model, role planner|worker|judge,
    effort, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    reasoning_tokens, cost_usd, cost_source=openrouter|price_table, started_at, ended_at,
    ttfc_ms, finish_reason, error_type, tool_name, tool_ok, gen_id)`
  - `scores(id, target_kind session|turn|span, target_id, name, value REAL, label, data_type,
    source human|judge|implicit, judge_model, explanation, created_at)`
  - `price_snapshots(model, provider, prompt_usd_per_tok, completion_usd_per_tok, cache_read,
    cache_write, fetched_at)` (OpenRouter returns `usage.cost`, `cached_tokens`,
    `reasoning_tokens` in every response; `GET /api/v1/generation?id=` gives USD `total_cost`,
    `native_tokens_*`, `latency`: https://openrouter.ai/docs/use-cases/usage-accounting,
    https://openrouter.ai/docs/api-reference/get-a-generation)
  - `scorer_configs(id, name, kind code|judge, prompt, choice_scores JSON, sampling_pct,
    enabled)`; `session_scorers(session_id, scorer_id)`
- "Session-by-session analysis configured by user" = per-session **goal + tag set + chosen
  scorer set + optional budget**; views then slice by tag/mode/role/effort/model: rating per $,
  cost per accepted edit, p50/p95 latency, effort-tier deltas, A/B replay of a turn across two
  models.

## 5. Delivery + v1 views
- `kolk dash`: embedded SPA (`//go:embed` + `http.FS`), localhost, JSON endpoints over SQLite.
  Charts: **uPlot 21.9 KB gz** (canvas, time-series/bars, no pie) vs Chart.js 68 KB, Observable
  Plot 128 KB, ECharts 368 KB (bundlephobia). Use uPlot + hand SVG bars, or pure SVG. TUI table
  for quick `kolk stats`; `kolk dash --export report.html` for static sharing.
- Top 5: (1) model leaderboard: rating/$, $/accepted turn, p50/p95 ms; (2) cost & tokens over
  time by model (stacked); (3) per-session drill-down timeline (turn → spans, tool errors);
  (4) effort-tier vs rating/cost scatter; (5) A/B replay diff.

## 6. Phases
- MVP: SQLite + importer from JSONL, spans/turns/scores, OpenRouter cost, leaderboard + timeline,
  `/rate`. Next: implicit signals (rewind/retry/tests), price snapshots, judge scorers with
  sampling. Later: replay A/B, OTLP export, session clustering.

## 7. Gotchas
- Privacy: don't store prompts/outputs by default (OTel makes them opt-in; Claude Code
  transcripts are plaintext and leak secrets); store hashes + opt-in.
- Growth: ~0.5–1 KB/span → tens of MB/year; WAL `-wal/-shm` files, checkpoint on exit, `VACUUM`
  rarely; no network FS.
- Migrations: `PRAGMA user_version` + embedded SQL steps; keep raw JSONL to rebuild.
- Cost drift: snapshot prices; prefer provider-reported cost.
- Judge cost/bias: sample, pick cheap judge, calibrate against `/rate`.

**Unverified**: OpenRouter `/models` pricing field names (404 during fetch — but the live pull on
2026-08-21 showed `pricing.prompt/completion` strings, see PLAN.md item 8), Weave cost API,
Opik online-rule details, modernc binary-size impact, that `GenAIOperationName` exists in Go
semconv.
