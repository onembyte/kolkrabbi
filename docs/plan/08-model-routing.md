# 8. Model selection, routing, the fast lane, and free-model defaults

Status: hardened on 2026-08-26 · supersedes: — · PLAN.md item 8

## Decision (the short version)

**Model selection in Kolkrabbi operates on a strict zero-config, free-first principle while honoring explicit user pins absolutely.** On a fresh installation with zero configuration, Kolkrabbi discovers and selects the highest-intelligence, tool-capable free coding model from OpenRouter's live catalog, with a bulletproof fallback to `openrouter/free`. The `/model` surface provides both a categorized catalog browser (bare `/model`) and instantaneous switching (`/model <id|alias>`) with popular vendor aliases (`sonnet`, `haiku`, `flash`, `deepseek`, `o3-mini`, `free`).

For background bookkeeping (session titles, diff summaries, commit messages, and prompt compaction), Kolkrabbi defines a dedicated **Fast Lane** (`slot.fast`) that routes auxiliary tasks to high-throughput, low-latency zero-cost endpoints (`:nitro` or light free models) so that frontier reasoning models never spend tokens or latency on metadata. When using free models, transient HTTP 429 rate limits automatically rotate through a per-turn verified candidate set before failing; however, **any model explicitly selected by the user is pinned and is never silently rotated or changed.**

---

## Spec

### 0. ★ North star compliance

#### 0.1 The napkin test
```console
# 1. Zero-config startup: dynamically selects verified free model
$ kolk
# In-memory discovery selects highest-ranked free model with tools
kolk-code> /model
active: cohere/north-mini-code:free (free · 200k ctx · tools)

available models:
  [free]
  * cohere/north-mini-code:free     free · 200k ctx · tools · reasoning
    meta-llama/llama-3.3-70b-instruct:free free · 131k ctx · tools
    google/gemini-2.0-flash-exp:free free · 1048k ctx · tools · vision
  [popular]
    anthropic/claude-3-7-sonnet     $3.00 / $15.00 · 200k ctx · reasoning
    openai/o3-mini                  $1.10 / $4.40  · 200k ctx · reasoning

# 2. Fast alias switch
kolk-code> /model sonnet
model → anthropic/claude-3-7-sonnet (200k ctx · $3.00/$15.00 per 1M)
```

#### 0.2 North star rules compliance

| North star rule | How Item 8 complies | Enforced by |
|---|---|---|
| **1. Zero-config is the product** | If `model` is unset, Kolkrabbi queries the catalog for free models at startup under a 5s deadline. If offline or failing, it falls back to `openrouter/free`. No setup questions, no wizard. | `TestFreeModelStartupDiscovery`, `TestFallbackToOpenRouterFreeOnOutage` |
| **2. Every default computed, not asked** | Free models are ranked algorithmically by: (1) free status (`pricing == 0` or `:free`), (2) tool capability, (3) coding suitability, (4) intelligence score / context length. | `TestRankFreeModelsAlgorithm` |
| **3. One install command, static binary** | Catalog parsing uses standard `encoding/json` and the existing HTTP provider client. No external ranking tools or scrapers. | `scripts/check-purity.sh`, `make budgets` |
| **4. One key command** | OpenRouter keys unlock the complete catalog. Non-OpenRouter base URLs (`--base-url`) disable OpenRouter-specific free discovery and use the user's base model. | `TestCatalogBaseURLCompatibility` |
| **5. Complexity ships off, discoverable later** | Pinned models remain pinned. Auto-routing across models only applies to the unpinned free tier pool on 429 errors. Paid auto-routing never spends money silently. | `TestExplicitModelNeverAutoSwitched` |
| **6. Simple to type beats simple to explain** | `/model <alias>` enables single-word typing (`/model sonnet`, `/model free`). Bare `/model` acts as the catalog search. | `TestModelAliasGrammar` |

---

### 1. Catalog discovery & free model ranking

#### 1.1 Live catalog caching
- Kolkrabbi caches `GET /api/v1/models` in `$XDG_CACHE_HOME/kolk/models.json` with a 1-hour TTL.
- In-memory cache is refreshed on background start or when the user runs `kolk models --refresh`.
- If the cache is stale and the network is unreachable, Kolkrabbi uses the cached catalog without failing.
- A compiled-in seed catalog (`internal/provider/catalog_seed.json`) provides emergency offline fallback for zero-network first-runs.

#### 1.2 The free model ranking algorithm
When resolving a free default model, candidate models from the catalog are evaluated through four strict filters:

1. **Zero-Cost Invariant**:
   - `id` ends with `:free`, OR `id == "openrouter/free"`, OR `(pricing.prompt == 0 && pricing.completion == 0)`.
2. **Tool Support Invariant**:
   - `supported_parameters` must contain `"tools"`. A model that cannot invoke tools cannot run code mode.
3. **Context Length Threshold**:
   - `context_length >= 32768` (minimum 32k tokens required for coding sessions).
4. **Intelligence & Coding Heuristic Ranking**:
   - Models are sorted by intelligence tier and recency:
     1. Models with verified coding benchmarks (e.g. `cohere/north-mini-code:free`, `qwen/qwen-2.5-coder-32b-instruct:free`).
     2. Models with native reasoning/thinking parameters (`supported_parameters` contains `"reasoning"`).
     3. General frontier free endpoints (`meta-llama/llama-3.3-70b-instruct:free`, `google/gemini-2.0-flash-exp:free`).
     4. Fallback: `openrouter/free`.

---

### 2. The `/model` surface & aliases

#### 2.1 Direct command and slash command parity
- CLI: `kolk model <id|alias>` or `kolk -m <id|alias>`
- REPL: `/model <id|alias>` or bare `/model`

#### 2.2 Standard aliases
To avoid typing full vendor paths like `anthropic/claude-3-7-sonnet`, Kolkrabbi ships a curated alias map:

| Alias | Target Model ID | Notes |
|---|---|---|
| `sonnet`, `claude` | `anthropic/claude-3-7-sonnet` | Current Anthropic frontier coding model |
| `haiku` | `anthropic/claude-3-5-haiku` | Fast Anthropic model |
| `opus` | `anthropic/claude-3-opus` | Deep Anthropic model |
| `gpt`, `gpt-4o` | `openai/gpt-4o` | Standard OpenAI multi-modal model |
| `gpt-4o-mini`, `mini` | `openai/gpt-4o-mini` | Fast cheap OpenAI model |
| `o3`, `o3-mini` | `openai/o3-mini` | OpenAI reasoning model |
| `flash`, `gemini` | `google/gemini-2.5-flash` | Fast Google model |
| `pro` | `google/gemini-2.5-pro` | Frontier Google model |
| `deepseek`, `r1` | `deepseek/deepseek-r1` | Frontier open-weights reasoning model |
| `coder` | `qwen/qwen-2.5-coder-32b-instruct` | Specialized coding model |
| `free` | `openrouter/free` | OpenRouter dynamic free router |
| `auto` | `openrouter/auto` | OpenRouter auto router |

#### 2.3 Bare `/model` output
Executing bare `/model` prints the current selection followed by categorized catalog highlights:
```text
active model: anthropic/claude-3-7-sonnet ($3.00/$15.00 per 1M · 200k ctx · tools · reasoning)

free models:
  * cohere/north-mini-code:free        free · 200k ctx · tools · reasoning
    meta-llama/llama-3.3-70b-instruct:free free · 131k ctx · tools
    google/gemini-2.0-flash-exp:free  free · 1048k ctx · tools

frontier models:
    anthropic/claude-3-7-sonnet        $3.00/$15.00 · 200k ctx · tools · reasoning
    openai/o3-mini                     $1.10/$4.40  · 200k ctx · tools · reasoning
    deepseek/deepseek-r1               $0.55/$2.19  · 128k ctx · tools · reasoning

switch: /model <name|alias>
```

---

### 3. The Fast Lane (`slot.fast`)

#### 3.1 Purpose & constraints
Auxiliary engine operations must not block user interaction or incur high frontier model fees. The Fast Lane handles:
- Session titling (summarizing the first prompt into a 4-word slug).
- Auto-compaction summaries (summarizing pruned conversation history).
- Commit message suggestions (`/commit`).
- Diff summarization.

#### 3.2 Fast lane selection rules
1. **Default**:
   - If the main session uses a free model: Fast Lane uses the same free model or `:nitro` throughput variant.
   - If the main session uses a paid model: Fast Lane uses the cheapest high-throughput model (e.g. `google/gemini-2.5-flash` or `openai/gpt-4o-mini`), capped at $\le \$0.15 / \text{1M tokens}$.
2. **Speed invariant**:
   - `throughput_last_30m >= 50` tokens/sec from OpenRouter endpoint telemetry.
3. **Execution context**:
   - Isolated context (zero messages from parent turn history except the target text).
   - Tool execution is disabled for Fast Lane calls.

---

### 4. Rate-limit rotation & recovery

#### 4.1 Free tier rate limits (HTTP 429)
When operating on unpinned free models:
1. On an HTTP 429 response, the engine inspects the pre-stream error.
2. The current model is added to the turn's `tried` set.
3. The engine hops immediately to the next available model in the free ranking list.
4. Output log: `◆ free model rate-limited (429); rotating to meta-llama/llama-3.3-70b-instruct:free`.
5. The original turn context and messages are replayed without user intervention.

#### 4.2 The Pinned Model Invariant
> **An explicit user choice is inviolable.**

If the user launched with `-m anthropic/claude-3-7-sonnet` or ran `/model sonnet`:
- Kolkrabbi **never** auto-rotates to another model on 429, 500, or context exhaustion.
- The engine executes the bounded retry backoff (1s / 2s / 4s) per `U0.1e`.
- If retries fail, it stops and reports the provider error cleanly to the user with actionable next steps.

---

### 5. Model capabilities & quirks cache

Model quirks live in the cached `/models` catalog rather than compiled Go code:
- **`supports_tools`**: derived from `supported_parameters` containing `"tools"`.
- **`supports_reasoning`**: derived from `supported_parameters` containing `"reasoning"` or non-empty `reasoning` block.
- **`context_length`**: exact integer from `context_length`.
- **`max_completion_tokens`**: derived from `top_provider.max_completion_tokens`.

If a model lacks tool support and the user enters `code` mode:
`warning: model %s does not support tool calling; switching to chat mode or select a tool-capable model via /model`

---

## Rationale

1. **Preventing surprise billing**: Silent auto-routing in paid models is dangerous and breaches user trust. By restricting auto-rotation strictly to verified free models, Kolkrabbi delivers resilience without financial surprises.
2. **First impressions without API friction**: A user evaluating Kolkrabbi can install the binary, set an OpenRouter key, and immediately test coding workflows on top-tier free models without configuring a paid account.
3. **Fast Lane cost isolation**: Running title generation or memory compaction through an ultra-tier reasoning model (like `claude-3-7-sonnet` with thinking) wastes dollars on trivial JSON parsing. The Fast Lane keeps background tasks virtually free and instantaneous.

---

## Alternatives rejected

- **Hardcoding top models in Go constants**: Rejected because models deprecate and pricing changes monthly. Caching the OpenRouter `/models` catalog keeps Kolkrabbi up to date without binary upgrades.
- **Silent fallback to paid models**: If free models are exhausted, Kolkrabbi prompts the user before spending money; it never silently routes to paid endpoints.
- **Client-side web scraping of leaderboards**: Rejected; OpenRouter's official endpoints and catalog APIs provide authoritative latency, throughput, and pricing programmatically.

---

## Risks & open questions

- **Risk: Upstream free model capacity swings**: Free endpoints on OpenRouter experience variable traffic.
  *Mitigation*: The four-candidate free rotation pool + `openrouter/free` ensures there is always a fallback.
- **Risk: Stale cache on newly released models**:
  *Mitigation*: Running `/model` or `kolk models` with `--refresh` invalidates the 1-hour disk cache on demand.

---

## Sources

- `docs/research/openrouter.md`: OpenRouter `/models` schema, endpoints API, pricing structures.
- `docs/research/orcli.md`: Free-tier rotation pool, `tried` sets, loop detection analysis.
- `docs/plan/02-architecture.md`: Performance and startup latency bounds.
- `docs/plan/03-provider-layer.md`: Capability extraction, `ReasoningSupport`.
- `docs/plan/07-effort-dial.md`: Model tier mapping.
- `docs/plan/18-config.md`: Config key registry and 5-link resolution.

---

## Checkpoint breakdown

Implementation of Item 8 is divided into 4 atomic checkpoints:

- **M8.1 Catalog Cache & Discovery Seam**: Implement disk-cached catalog loader with 1-hour TTL and compiled seed fallback.
- **M8.2 Free Model Ranker & Auto-Rotation**: Implement intelligence ranking for free models and per-turn rotation on HTTP 429.
- **M8.3 Model Aliases & Catalog Browser**: Wire alias dictionary (`sonnet`, `haiku`, `flash`, etc.) and bare `/model` interactive display.
- **M8.4 Fast Lane Auxiliary Execution Slot**: Create isolated `slot.fast` execution path for titling and summarization tasks.
