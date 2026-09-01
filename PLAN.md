# Kolkrabbi — plan-hardening checklist

Kolkrabbi (`kolk`) is a fast, lightweight terminal agent: **chat / code / agent** modes, an
**effort dial**, **one key → many models** (OpenRouter first), and a **100% local** dashboard
that tells you which model earns its cost on *your* tasks. CLI first; desktop later; iPad after.

This file is the **index of open plan items**. Each item is sized to be hardened in one focused
`/loop` session: resolve the decisions, write the detail into `docs/plan/NN-slug.md`
(template: `docs/plan/README.md`), tick the box here and add a one-line decision summary.
Research inputs gathered on 2026-08-21 live in `docs/research/`.

---

## ★ North star — the product constraint that outranks every item

> **Stated by the owner, 2026-08-22:** *"I need this project to be perfect, easy to use. No configs
> needed. Just a command to install — like Anthropic's Claude Code CLI — and a command to configure
> the OpenRouter API key, or any API key the user wants to use."*

Kolkrabbi's whole surface must fit on a napkin:

```
curl -fsSL <install-url> | sh        # 1. one command to install. no runtime, no deps, no prompts.
kolk key sk-or-v1-…                  # 2. one command for a key (any provider's key).
kolk                                 # 3. it works. no config file was ever opened.
```

Binding rules, which every remaining item inherits:

1. **Zero-config is the product, not a feature.** A brand-new user must never be required to read,
   create or edit a config file to get a working agent. Config exists only to *override* defaults
   that are already good. If a decision produces a required setting, the decision is wrong.
2. **Every default must be computed, not asked.** Free models, effort tiers, the fast lane, the
   catalog — all derived at runtime (item 8), never a setup questionnaire.
3. **One install command**, single static binary, no runtime and no toolchain on the user's machine
   (item 20). Package managers are additional paths, never the required one.
4. **One key command**, provider-agnostic — `kolk key <key>` accepts any supported provider's key
   and infers the provider from the key's shape where it can; `kolk login` is the optional
   nicer path, never the required one (item 5).
5. **Complexity is opt-in and discoverable later.** Profiles, tiers, routing, permissions, MCP —
   every one of them ships *off*, with a working default, and is found when wanted (items 7, 9, 16, 18).
6. **Simple to type beats simple to explain.** Short verbs, no flags required for the common path
   (item 9).

When an item's design conflicts with this section, this section wins, and the item's doc must say
how it complied. Items most directly bound: **5** (keys), **8** (free-model defaults), **9**
(commands), **18** (config), **20** (install), **22** (onboarding).

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

## Phase plan — one `/loop` per phase (written 2026-08-26)

The items below are ordered by decisions, not by number. Each phase is one `/loop`, and each phase
names the checkpoint leaves it must close in [`CHECKPOINTS.md`](CHECKPOINTS.md). Two loop shapes are
used, matching the two things this repository does:

- **harden** — resolve an item's "Decide" bullets and write `docs/plan/NN-slug.md`. No production
  code. This is the loop template already documented above.
- **build** — turn a hardened doc into TDD checkpoint leaves, one leaf per iteration, red → green →
  refactor → focused verify → `make check` → record in `docs/build-log.md`.

Never run a build loop for an item whose doc is not hardened, and never run two phases at once: the
checkpoint contract allows one active leaf, and the 2026-08-26 session showed what happens when
twenty-five commits land outside it.

**Ordering rule used here:** finish what is half-built before starting what is unbuilt, put
correctness before the surface that displays it, and put permissions before autonomy.

### Phase A — finish the subscription path · items 4, 24

Everything from `kolk plans` to a Claude-answered turn now works end to end, but four boxes are
still open and each one is a way for a user to be told something untrue.

- `P11.7` a clean provider-CLI exit is treated as proof of login; nothing verifies the user actually
  authenticated before the connector is marked enabled.
- `B12.12` the active effort is never validated against the plan's advertised effort levels, so
  `max` on a plan that stops at `high` silently means something else.
- `B12.13` `newAgent` demands an OpenRouter key before anything, so a user whose only provider is a
  Claude subscription cannot start a session. This one is a product decision, not a bug: it
  contradicts the north star for subscription users and needs the owner's call before code.
- `B12.14` `Collect` drops `cache_read_input_tokens` and `cache_creation_input_tokens`, so cached
  turns are mis-costed even though the wire shape already carries them.

**Exit:** all four leaves closed, item 24's Anthropic row `[x]`, a second provider still not started.

```
/loop build phase A of PLAN.md: close P11.7, B12.12 and B12.14 as separate TDD checkpoint leaves,
one leaf per iteration, red first. Ask the owner about B12.13 before writing any code for it.
Record each leaf in CHECKPOINTS.md and docs/build-log.md, and stop when all four are resolved.
```

### Phase B — managed local models · item 25

`internal/local` can start and stop a sidecar and nothing can drive it. A runtime with no planner
and no command surface is weight without value, and the longer it sits the more it looks finished.

- `L13.4` hardware probe and fit planner: the documented `{accelerators, system_ram_bytes,
  disk_free_bytes}` shape, failing closed to unknown, reserving headroom, refusing what does not fit
  instead of degrading into swap.
- `L13.5` `/localia` and its CLI twin, with parity tests that need neither a GPU nor Ollama.

**Exit:** `docs/plan/25-managed-local-models.md`'s five TDD checkpoints closed, plus one manual GPU
smoke test recorded as separate evidence.

```
/loop build phase B of PLAN.md: implement L13.4 then L13.5 against docs/plan/25-managed-local-models.md,
one TDD leaf per iteration, red first, no GPU or Ollama required by any test. Record each leaf.
```

### Phase C — sessions, context and memory · item 12

This is the next real cliff. Both new backends accumulate context and nothing compacts it: a long
Claude session will hit the provider's context limit with no recovery path, and the fast lane
already exists to summarise but is not used for it. Storage also has to be decided before the
dashboard picks a database, so this phase blocks phase D.

**Exit:** `docs/plan/12-sessions-context-memory.md` hardened — storage decision (JSON vs the
dashboard's SQLite), compaction algorithm and threshold, memory precedence, session command spec.

```
/loop harden item 12 of PLAN.md: read the item and docs/research/dashboard.md and ecosystem.md,
resolve every "Decide" bullet, write docs/plan/12-sessions-context-memory.md from the template in
docs/plan/README.md, then tick the item with a one-line decision.
```

### Phase D — the local dashboard · item 17, migration group A12

Only now. Per-turn accounting was wrong until 2026-08-26 (B12.11): Claude turns recorded `$0`, and
under the persistent process they would have recorded running totals. A chart built on those numbers
would have been confidently wrong, which is worse than no chart. The A12 leaves are already written
in the migration queue.

**Exit:** A12.1–A12.5 closed, `kolk dash` serving the five v1 views, budgets re-measured with the
SQLite dependency.

```
/loop harden item 17 of PLAN.md first, then build migration group A12 leaf by leaf, one per
iteration, red first, re-measuring the binary-size and dependency budgets in the closing leaf.
```

### Phase E — tools, permissions and sandboxing · item 13

Before autonomy, not after. Phase F deploys several agents at once; the permission model has to
exist before anything runs unattended, and `--yolo` needs a hardline blocklist that survives it.

**Exit:** `docs/plan/13-tools-permissions-sandboxing.md` hardened, then its build leaves.

```
/loop harden item 13 of PLAN.md: resolve every "Decide" bullet, cite the permission precedents in
docs/research/ecosystem.md, write docs/plan/13-tools-permissions-sandboxing.md, tick the item.
```

### Phase F — orchestration and per-task routing · item 14

The owner's stated goal for `ultra`: many agents, each on the model that suits its task. Depends on
item 8 (done), phase C (context isolation and compaction) and phase E (permissions).

**Exit:** `docs/plan/14-orchestration-routing.md` hardened — orchestration state machine, routing
table format, concurrency and confirm design, cost caps — then built in leaves.

```
/loop harden item 14 of PLAN.md: resolve every "Decide" bullet including parallel subagents,
per-task routing, roles, worktree isolation and failure handling; write
docs/plan/14-orchestration-routing.md; tick the item.
```

### Phase G — the surface · items 11, 15, 16

TUI and input, code-mode specifics, and extensibility (MCP, skills, hooks). Deliberately after the
engine phases: each of these is a surface over something the phases above decide.

```
/loop harden items 11, 15 and 16 of PLAN.md in that order, one item per iteration, writing each
doc before starting the next.
```

### Phase H — ship it for real · T0.5, items 19–23

The owner trial gate still has two open boxes, both of them the same missing evidence: nobody has
proved the flow on a machine with no Go toolchain and no prior Kolkrabbi state. Everything else in
this phase is distribution, quality, onboarding and the roadmap that records what was refused.

**Exit:** T0.5 closed with a recorded clean-machine rehearsal, both trial-gate boxes green, and the
owner told the app is ready to try.

```
/loop close T0.5 of CHECKPOINTS.md: rehearse install, first run, key addition and first model
response on a machine with no Go toolchain and no prior Kolkrabbi files, record the evidence, then
harden items 20, 21, 22, 23 and 19 one per iteration.
```

---

### Phase I — reach · items 26–29

Kolkrabbi is pinned to the terminal that started it. This phase unpins it, in the shape a two-module
static binary can actually hold: a hardened bind-and-auth floor first, because `kolk serve` currently
accepts `--addr :8080` — every interface — with no token, and everything else in this phase would be
built on that. Then pairing, then a web client that can steer rather than only watch.

Item 26 comes first and its first leaf is the security fix, not a feature. Items 27–29 are ordered by
what they cost the core: many-sessions is mostly discovery, source control is mostly refusals, and
workspace services is one cheap useful piece (port discovery) attached to two expensive ones.

**Exit:** each item hardened into `docs/plan/`, then built in leaves.

```
/loop harden item 26 of PLAN.md, fixing the wildcard-bind hole as its first leaf, then items 27, 28
and 29 in that order, one item per iteration, writing each doc before starting the next.
```

### Phase J — borrowed hardening · items 30, 31, 32

Added 2026-08-27. Three small items lifted from a source-level read of OpenCode
([`docs/research/opencode.md`](docs/research/opencode.md)), each closing a gap an already-hardened doc
left open. They are ordered cheapest-first and none of them blocks another phase. Items 30 and 31 are
amendments to item 13's model and should be hardened together; item 32 is independent and can be
built as soon as it is hardened.

**Exit:** `docs/plan/30-doom-loop.md`, `docs/plan/31-command-prefix-patterns.md` and
`docs/plan/32-shadow-git-snapshots.md` written, all three items ticked, no production code before
the docs exist.

```
/loop harden items 30 and 31 of PLAN.md together in one iteration each, then item 32: read the item,
its prior-art section in docs/research/opencode.md, and the hardened doc it amends (13 for 30 and 31,
15 and 12 for 32); resolve every "Decide" bullet; write docs/plan/NN-slug.md from the template in
docs/plan/README.md; tick the item with a one-line decision. Stop when all three are hardened.
```

### Phase K — vision completion and release truth · item 34

Added 2026-08-31 after a fresh architecture, security, lifecycle, subscription-routing, and release
review. The historical phase table records how features landed; it is **not** a statement that their
failure paths are ready to ship. This phase is the single forward queue for turning the product
vision into a supportable release: contain security boundaries first, then make concurrent and saga
work recoverable, then make catalog/provider claims true, then prove the release on clean machines.

“100%” means every accepted v1 promise has an implementation, test evidence, independent review,
and release evidence; it does not mean silently treating every possible future provider, desktop
client, or OS sandbox as committed scope. Such work must be explicitly accepted or recorded as
post-v1 before the release claim is made.

**Exit:** every V34 checkpoint in [`docs/plan/34-vision-completion.md`](docs/plan/34-vision-completion.md)
is closed or explicitly owner-deferred, no acknowledged P0/P1 defect remains in accepted scope, and
a clean-machine release rehearsal plus an independent final review has passed.

```
/loop build phase K of PLAN.md: claim exactly one V34 leaf from CHECKPOINTS.md; write its red,
green, failure-path, and independent-review evidence before marking it complete. Do not start the
next leaf until the current leaf's focused gate, review, and ledger walk-back are recorded.
```

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
offline e2e testing via `internal/enginetest`.

---

## A. Foundations

### [~] 1. Identity, repository & release skeleton
**Done 2026-08-22:** repo live at **https://github.com/onembyte/kolkrabbi** (private, `main`), history = `proto-0` tag → docs → identity commit → CI. Module path `github.com/onembyte/kolkrabbi`, binary `kolk`, **Apache-2.0**, go 1.25. CI green on ubuntu + macOS with budget enforcement (6.25 MB binary, 2 ms cold start, 22-test floor).
**Still open:** public vs private (private for now — one click to flip, not reversible the other way); goreleaser + release-on-tag; `go install …/cmd/kolk@latest` cannot work for anyone else while the repo is private; README still describes the prototype rather than the vision; `docs/plan/01-identity-release.md` not written.
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

### [x] 3. Provider layer — **hardened → [`docs/plan/03-provider-layer.md`](docs/plan/03-provider-layer.md)**
**Decision:** one `Chat{Stream, Capabilities, Close}` interface returning a closable pull-`Stream` of a flat `Event` union, implemented by three adapters — `openrouter` (HTTP+SSE), `openaicompat` (the shared engine, with Ollama/LM Studio/vLLM/llama.cpp/LiteLLM/Vercel AI Gateway as data-only `Dialect` presets, **no second gateway adapter**), and `agentcli` (spawns the user's own logged-in binary) — with retry/rotation/budget/recording in L4 driven by a pure `provider.Decide()` table. Reasoning bytes enter a message only on the terminal frame; content and tool-arg deltas concatenate as raw JSON-escaped bytes (split runes are silently corrupted otherwise); every count is a pointer with a `CostSource` + `Measurement` so unknown ≠ zero ≠ free; a committed stream is never replayed, only rotated or surfaced; one usage row per attempt on every terminal path. **Native provider keys are out of v0.x with the hole pre-cut**; **per-model quirks live in the cached `/models` catalog + a generated `//go:embed` seed, never in the binary.** 12-step migration (M0–M11) slots into architecture §12 steps 5–9 keeping all 22 tests green — and forces §12 step 10 (`session.Message`) before the reasoning round-trip.
**Scope:** the abstraction every mode talks to; OpenRouter as primary; local/self-hosted; optional direct providers.
**Today:** `internal/provider` streaming client (chat/completions, tools, usage+cost), `ListModels`, `--base-url` override.
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

### [x] 4. Subscription & external agent backends — **hardened → [`docs/plan/04-subscription-backends.md`](docs/plan/04-subscription-backends.md)**
**Decision:** spawn the user's own unmodified, self-logged-in `claude`, `codex`, or `antigravity` (`agy`) CLI binaries as first-class `provider.Chat` backends (`internal/provider/agentcli`, registry keys `claude` ["Claude Agent"], `codex` ["Codex Agent"], `antigravity` / `agy` ["Antigravity Agent"]) — kolk never sees, stores or proxies a credential, and that is enforced by code shape + a CI source denylist, not a promise. `ExecutesOwnTools:true` / `HistoryOwned:true` / `IdempotentConnect:false` / `ModelSelection:ModelAliasOnly` — item 3's interface needed **no** change. In this mode kolk is a frontend and recorder: the vendor runs its own tools, so kolk's permissions/path jail/checkpoints do **not** gate them and the UI says so per tool line. Tokens are exact; **cost is not a charge** — `total_cost_usd` is a deterministic function of tokens at list prices (verified against both fixtures), so it is labelled `API-equiv.`, never pooled with metered rows, and `rate_limit_event.utilization` becomes the real cost series. `kolk login <claude|codex|antigravity>` invokes native CLI auth via a clean terminal handover (no pipes). Codex and Antigravity ship on identical bones; **Gemini never spawns** (Google names account suspension) — API key only.
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
- ~~If (iii) is the path: … a "claude-code" provider …~~ **Settled.** Registry key `claude`, label **"Claude Agent"**, package `agentcli`. The string `claude-code` must never ship as a product/feature name — it is on Anthropic's own "Not permitted" branding list. Flags verified present in 2.1.240: `--output-format stream-json`, `--verbose`, `--safe-mode`, `--setting-sources`, `--model`, **`--effort`** (so the effort dial maps straight through), `--allowedTools`, `--permission-mode`, `--resume`, `--append-system-prompt`. `--bare` is banned (it ignores subscription login). See `docs/plan/04-subscription-backends.md` §4.
- Product stance for the prohibited shapes (token reuse, Gemini OAuth): document clearly, offer the API-key path, keep adapters pluggable in case policy changes; decide whether to build the Codex adapter before or after verifying OpenAI's terms.
- Terms/risk note in README (account-suspension risk language) and a per-backend capability matrix (tools, streaming, cost reporting, context size).
**Hardened when:** a written allowed/gray/prohibited table with citations, a chosen shape, a flag-mapping spec, and a fallback plan.
**Inputs:** `docs/research/subscription-auth.md`

### [x] 5. Auth, keys & secrets — **hardened → [`docs/plan/05-auth-keys-secrets.md`](docs/plan/05-auth-keys-secrets.md)**
**Decision:** `kolk key <key>` is the product — one provider-agnostic command that infers the provider from the key's shape (deny rows beat infer rows, so a `sk-ant-oat…` subscription token is refused, never stored), verifies it, and writes a **0600 manifest** at `$data/kolk/credentials.json` on every OS, in a container, over SSH, in CI, with **no prompt, dialog, browser or network**. `kolk login` (OAuth PKCE, ephemeral loopback, 128-bit nonce **in the callback path** because OpenRouter has no `state`, abandonable to paste-a-code after 20 s) and the **OS keychain are opt-in** — `/usr/bin/security` has no non-interactive flag, so keychain-by-default can raise a macOS password dialog on the turn path, and its ACL trusts `security`, not kolk, so it buys encryption at rest and **nothing** against code running as you. One published, numbered chain (flag ⌀ → `KOLK_API_KEY` → provider env → the manifest's **one** named backend → none), first hit wins, `kolk key --why` prints every link including the losers; a locked backend is a named error with a re-paste recovery that never reads it, **never** "kolk needs a key". Three L0 packages — `redact` (pure, importable by `agentcli`), `secret` (an opaque **vault handle**: measured, a plaintext-in-the-type design leaks through **8** `fmt`/`slog` paths from an unexported field, the handle through none) and `keystore` (the only reader/writer, importable only by `cli`/`serve`) — plus `secret.AuthTransport`, because `%+v` on an `*http.Request` prints the key even with a perfect handle. Scrubbing is **split by sink**: exact known literals (every stored credential, not just the active one) plus high-confidence prefixes for what the model sees; the keyword rule only for durable copies — so the agent can still edit `.env.example` and kolk's own tests. Fixes six live defects (**`api_key` leaves `config.json`**; `cmd.Env` nil ⇒ the model's shell sees the key; `os.WriteFile` cannot repair 0644; checkpoints copy `.env` pre-images; `/rewind` restores 0600 files at **0644**; the credential read leaves the startup path). Measured: chain **23 µs** vs a **5.64 ms** cold start; scrub scanner **219 MB/s** vs regexp **9.3 MB/s**. Multi-provider ships **on**; profiles ship **off** with the manifest already keyed `provider/profile` and **no hidden `KOLK_PROFILE`**; a project file may name a profile but **never a credential source** (`helper:evil` from a `git clone` is code execution). Item 4's boundary is five `arch_test` import rules, two factory signatures (`provider.Config.APIKey` deleted), the shape deny-list, and two `doctor` renderers sharing no formatter and no value column.
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

### [x] 6. The modes — chat / code — **hardened → [`docs/plan/06-modes.md`](docs/plan/06-modes.md)**
**Decision:** a mode is a record, not a code path, and there are two visible ones: `chat` reads (`read_file` `list_dir` + item 13's grep/glob; **no network, no bash** — the printed guarantee is "changes nothing on your machine and sends nothing off it"), `code` writes and is the default everywhere. **`agent` does not survive as a mode** — orchestration becomes a `delegate` tool inside code, width owned by item 7's effort dial; `/agent` keeps working through v0.3 as a self-translating deprecation and stats aliases the historical value. Modes are pure data resolved once per turn by two pure functions; hidden rows (`task`, `title`, reserved `plan`) are what stop items 8/10/14/15 from each hardcoding another prompt. Reach is enforced by omitting the schema, never by prompt instruction. Per-mode model maps and effort overrides ship as hooks with unset values (no config on day one); sticky per-mode models are rejected outright.
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

### [x] 7. The effort dial — fully configurable, including inside code mode — **hardened → [`docs/plan/07-effort-dial.md`](docs/plan/07-effort-dial.md)**
**Decision:** four levels `low/medium/high/max` (numeric aliases `1..4`, legacy `quick..ultra` aliases preserved), governing model tier, provider reasoning effort, tool rounds (4/12/24/50 in code, 2/6/12/20 in chat), subagent width (1/2/4/6), and verification depth. Live `/effort` re-resolves model and updates persistent footer immediately in any mode. Zero-config tier inheritance from session model.
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

### [x] 8. Model selection, routing, the fast lane, and free-model defaults — **hardened → [`docs/plan/08-model-routing.md`](docs/plan/08-model-routing.md)**
**Decision:** zero-config startup automatically selects highest-ranked free coding model from live OpenRouter catalog with fallback to `openrouter/free`; curated vendor aliases (`sonnet`, `haiku`, `flash`, `deepseek`, `o3-mini`, `free`); dedicated zero-cost Fast Lane (`slot.fast`) for background bookkeeping; 429 auto-rotation across free pool; pinned user models are never auto-switched.
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

### [x] 9. Command surface — few, obvious, typeable — **hardened → [`docs/plan/09-command-surface.md`](docs/plan/09-command-surface.md)**
**Decision:** strict parity between CLI verbs and slash commands (`kolk <verb> [args]` ≡ `/<verb> [args]`); rigid guardrails: single-word, lowercase, ≤ 6 letters (`key`, `model`, `effort`, `mode`, `config`, `login`, `update`, `stats`, `dash`, `saga`, `doctor`, `help`, `exit`); deterministic UNIX exit codes (0, 1, 2, 3, 130); `--output stream-json` machine NDJSON; 10-item reserve list.
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

### [x] 10. The careful-progression loop (working name: `saga`) — **hardened → [`docs/plan/10-saga-loop.md`](docs/plan/10-saga-loop.md)**
**Decision:** `saga` engine advances longitudinal goals chapter by chapter; each chapter follows a 5-step loop (plan, bounded change, shell quality gates, commit on green, log); progress preserved in `SAGA.md`; stop conditions: criteria met, max chapters (15), max budget ($5.00), timeout (1h), 3-strike doom-loop detector; chapter-level `/rewind`.
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

### [x] 11. REPL / TUI & input
**Hardened 2026-08-26** ([`docs/plan/11-repl-tui-input.md`](docs/plan/11-repl-tui-input.md)): the framework question was already answered by the module budget — Bubble Tea plus Lip Gloss plus Glamour is not two modules, so `internal/tui` is hand-rolled and built. Reading the code against the bullets found three real gaps: no diff preview before a confirm (someone approving an edit is approving a description of it), no `@file` mentions, and a status line carrying neither context nor cost though both already exist in the engine. Syntax highlighting and three extra keybindings are explicitly out.
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

### [x] 12. Sessions, context & memory
**Hardened 2026-08-26** ([`docs/plan/12-sessions-context-memory.md`](docs/plan/12-sessions-context-memory.md)): sessions stay JSON because the dependency budget hard-fails above two modules; compaction measures the window from provider-reported tokens, fires at 75%, drops old tool output first, stays reversible and visible, and an overflow error compacts and retries once.
**Scope:** persistence, resume, compaction, memory files.
**Today:** JSON sessions, atomic save, `-r`/`-s`, list/rm/clear, title from first input, dangling tool-call repair.
**Decide:**
- Storage: JSON files vs SQLite (shared with the dashboard, item 17); migration path; one DB for everything?
- Features: `sessions` list/search/rename/fork/export (md/json), per-project session scoping (cwd-aware resume), auto-title via the fast lane.
- Context management: token accounting per model, auto-compaction at threshold with a summary strategy (keep plan, decisions, file list; drop tool noise), `/compact` manual, tool-output truncation rules, conversation rewind (`/undo` last turn incl. messages).
- Memory layers: project (`KOLKRABBI.md`/`AGENTS.md`), user-level (`~/.config/kolk/memory.md`?), and "save this" command; size caps; what the agent may write back.
**Hardened when:** storage decision, compaction algorithm, memory precedence, session command spec.
**Inputs:** `docs/research/dashboard.md`, `docs/research/ecosystem.md`

### [x] 13. Tools, permissions & sandboxing
**Hardened 2026-08-26** ([`docs/plan/13-tools-permissions-sandboxing.md`](docs/plan/13-tools-permissions-sandboxing.md)): today there is no path jail, `--yolo` has no floor, and tool output is unscrubbed; v1 is a path jail, a blocklist that survives `--yolo`, scrubbed results and auto-deny in subagents, with OS sandboxes deferred because this CI cannot verify them.
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

### [x] 14. Agent mode — orchestration & per-task model routing ("Hermes-style, but multi-model")
**Hardened 2026-08-26** ([`docs/plan/14-orchestration-routing.md`](docs/plan/14-orchestration-routing.md)): a run must survive its own failures before it is worth routing or parallelising — today one failed subagent discards every result before it. Tasks become records carrying kind and dependencies; kinds route to named slots (orchestrator/worker/explore/fast) resolved through config → effort tiers → session model; cost is shown and optionally capped; concurrency is three at a time, spawn depth 1, and lands last because it multiplies every failure mode above it.
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

### [x] 15. Code mode specifics (Claude Code-style loop, done right)
**Hardened 2026-08-26** ([`docs/plan/15-code-mode.md`](docs/plan/15-code-mode.md)): further along than the bullet list suggests — unique-match edits, checkpoints, `/changes`, `/rewind`, `/effort` mid-task and (from G11.1) the diff preview and create-vs-overwrite guard are built. Three gaps worth building: `/rewind` restores files but leaves the conversation, so the model reasons about a tree that no longer matches its history; there is no way to see what actually changed; and no plan mode. Formatter hooks belong to item 16 where the permission story is the design, test-command detection is deferred, and LSP/tree-sitter are refused on the module budget. Whitespace-tolerant matching and multi-hunk edits are refused on principle: both trade reviewability for round trips.
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

### [x] 16. Extensibility — MCP, skills/commands, hooks
**Hardened 2026-08-27** ([`docs/plan/16-extensibility.md`](docs/plan/16-extensibility.md)): markdown commands ship first because they add no dependency, no process and no permission surface — a `/name` that expands to a prompt is a prompt, and Claude Code's `.claude/commands` are read as a fallback rather than imported. Hooks ship second with the permission story as the design: three post-events only, confirmed once per command like a rule, floor applies, failures reported not fatal, and **no `pre-tool` hook** because a hook that can veto a tool call is a second permission system. MCP is deferred with two named blockers — `ruleFamilies` cannot name an MCP tool, so governing one is impossible today, and tool schemas already cost ~5 KB per request before a server adds a dozen more. No dynamic Go loading, ever: a plugin in the agent's address space shares its credentials and its floor.
**Later evidence (2026-08-27, [`docs/research/opencode.md`](docs/research/opencode.md) §C.4):** a source-level read of OpenCode arrived after this item was hardened and confirms rather than reopens it — their `.claude` compatibility is exactly "read it natively, never import", and the one hook we refused, a `pre-tool` veto, is the one they shipped (`permission.ask`) and then had to reconcile with their permission engine. Two things it leaves for later: their scan also reads `.claude/skills/**/SKILL.md` and `.agents/skills/**/SKILL.md`, which this item's spec does not cover because it ships commands and not skills; and `auth` is one of their hook points, which belongs to item 24 and is recorded there.
**Scope:** how users add capabilities without forking.
**Today:** none.
**Decide:**
- MCP client (stdio + HTTP): `kolk mcp add/list/rm`, tool namespacing, permission rules per server, timeouts. Priority vs. v1?
- Skills/commands: Markdown-defined slash commands (`.kolk/commands/*.md`, `~/.config/kolk/commands/`), argument substitution, compatibility with Claude Code's `.claude/commands` format (import?).
- Hooks: pre/post tool-call shell hooks (lint after edit, notify on stop), config format, safety.
- Plugin boundary: keep the core small; plugins = MCP + markdown + hooks, no dynamic Go loading.
**Hardened when:** which of the three ship in v0.x, the file formats, and the permission story for third-party tools.
**Inputs:** `docs/research/ecosystem.md`, `docs/research/opencode.md` §C.4

## C. Data

### [x] 17. The local dashboard
**Hardened 2026-08-26** ([`docs/plan/17-local-dashboard.md`](docs/plan/17-local-dashboard.md)): `stats.jsonl` stays the store — a heavy user's year aggregates in 578 ms, so SQLite would cost the third module and the budget gate to buy imperceptible speed; `kolk dash` serves loopback-only, server-rendered SVG with no JavaScript library. — model efficiency, session by session (Braintrust, but local and yours)
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

### [x] 18. Config system — **hardened → [`docs/plan/18-config.md`](docs/plan/18-config.md)**
**Decision:** a closed, typed key registry in `internal/config` resolved through five links (flag > env > project > user > computed default) where every default is computed and no key is required. The file is `paths.Config()/config.json`: flat depth-one JSONC (comments + trailing commas), read via a blanking pass + stdlib, written by a byte-splice that never reserializes so `kolk config set` cannot eat a comment; no dependency added. Eight string keys in v0.1; credentials are never settings (arch rule S5 extended). Project config ships the ratchet boundary first (`Kind`/`ProjectOK`/`Tighter`, can only tighten), loader deferred to v0.2 behind item 13's `permission.rules`. Prototype migration: sessions/stats move (`paths.Migrate`), key evacuates (`keystore.MigrateLegacyConfig`), nested `tiers` flatten through an alias table with the quick→low vocabulary rename — idempotent, crash-safe, no write-back under old names.
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

### [x] 19. Desktop & iPad path (decided early, built later)
**Hardened 2026-08-27** ([`docs/plan/19-desktop-ipad.md`](docs/plan/19-desktop-ipad.md)): the last unhardened item, and the only one where almost nothing is built, so it is a decision rather than a record. **No desktop app and no iPad app**, and not for the reason the item assumed — the stack was never the hard part. Of the four things a desktop shell would add, three already shipped: `kolk dash` renders the dashboard on the server, `kolk sessions` browses them, and item 27 gives many sessions one view. The fourth, OS notifications, is the whole remaining case, and it justifies a 200-line notifier watching the event stream, not a second application with its own release train, signing identity, notarization and update path. The condition for revisiting is written down: more than one person running several sessions at once who says the terminal is where the work gets lost — not "a GUI would be nice". The stack is decided anyway, because leaving it open invites the wrong default: **Tauri v2 with `kolk serve --stdio` as a sidecar**, since Wails v3 sets `CGO_ENABLED=1` and needs gtk4/webkitgtk, which puts a toolchain inside the Go build, while the sidecar boundary is a protocol already shipped, tested and versioned. iPad: a native app and a gomobile core are refused — iPadOS cannot spawn a shell, so code mode cannot exist locally and a native app could only ever be chat. The built answer is kolk on a real machine over Tailscale with the iPad as a client. Of the four protocol constraints these targets impose, three are met (streaming with resumption, loopback auth with paired devices, versioning) and **session multiplexing is not** — one bus per session, one session per `serve` — and it stays unbuilt on purpose, because its only justifying consumer is the application this item declined. Also found by looking rather than reading: three claims in the plan were fiction — `desktop/`, `bind/` and `tools/` were never carved, `modernc.org/sqlite` was never added, and item 21 repeated the `tools/` claim a day old. All three corrected, the stale SQLite allowance removed, and a rot test added so an allowance nothing imports fails the build.
**Scope:** don't block the future; pick the shape now, build after CLI v1.
**Today:** nothing (CLI only).
**Decide:**
- Desktop: Wails v3 (Go-native webview; **in Beta**, beta.12 2026-08-21) vs Tauri v2 + Go sidecar vs Electron + sidecar; what desktop adds (dashboard, session browser, multiple parallel sessions, notifications). Shared protocol = the daemon API from item 2; defer the shell choice until that protocol exists.
- iPad: iPadOS apps can't spawn shells/toolchains → code mode on iPad means **remote execution** (kolk daemon on a Mac/Linux box over Tailscale/SSH) or chat-only local; Swift client vs gomobile-bound core; App Store constraints. Pragmatic v1: run kolk on your Mac, use it from iPad via a terminal app (Blink/Termius + mosh) + the web dashboard over Tailscale.
- Protocol requirements these impose on the core *now*: streaming events, session multiplexing, auth on localhost, versioning.
**Hardened when:** a one-page platform strategy with the chosen stacks, what ships when, and the protocol constraints list.
**Inputs:** `docs/research/platform-strategy.md`

### [x] 20. Distribution, updates & CI
**Hardened 2026-08-27** ([`docs/plan/20-distribution-updates-ci.md`](docs/plan/20-distribution-updates-ci.md)): most of this item was already built while other items were hardened — installer, four signed archives, `kolk update`, the two-OS matrix, lint, the failing budgets, the tag-only release workflow and the public verifier — so the doc records what exists and answers the three questions that were still open. Homebrew, scoop, winget and AUR are refused for now: a package that lags the release teaches people that upgrading does not get them the current kolk, and GoReleaser's `brews:` block keeps the mechanical part a ten-line change whenever install friction becomes the complaint. macOS notarization is refused for the CLI because `curl` does not quarantine what it downloads, so Gatekeeper never sees the installed binary; it becomes mandatory the day item 19 ships something with an icon. A version check that runs on its own is refused outright — it is a phone-home by another name and it leaks when someone is working — so kolk contacts the release server only when a person types `kolk update`. The doc is also honest that both fast paths verify SHA-256 against a manifest from the same origin, which is not evidence against a compromised origin; `scripts/verify-release.sh` is the signature-level path, because taking on Sigstore would break the two-module gate. Built here: the weekly live smoke test against the free model the fallback catalogue seeds — everything else in the suite is offline by construction, which is a good property and also a blind spot — plus a README install section that documented only `go build`, and documented it wrongly.
**Scope:** how people get and update kolk.
**Decide:**
- Install paths: `curl | sh` script, Homebrew tap, `go install`, GitHub Releases (goreleaser), later scoop/winget/AUR; checksums/signing; macOS notarization (needed once there's a desktop app; CLI maybe).
- `kolk update` self-update vs package managers only; version check (opt-in, local-only otherwise).
- CI: test matrix (macOS/Linux), lint (golangci-lint), release on tag, size/startup budget checks, weekly live smoke test against free models (opt-in secret).
**Hardened when:** release pipeline spec + install docs + budget checks in CI.
**Inputs:** —

### [x] 21. Quality, testing & security
**Hardened 2026-08-27** ([`docs/plan/21-quality-testing-security.md`](docs/plan/21-quality-testing-security.md)): the `Today` line said 22 offline tests; there are 2,078 across 170 files, so the question stopped being "more tests" and became which kinds earn their keep. Golden output tests for the TUI are refused — a golden frame asserts every pixel of a layout that is still moving, fails on every deliberate change, and teaches reviewers to regenerate without reading, which looks like coverage and is worse than none. Property tests for the edit tool are refused as stated, because the honest property is the implementation restated; the specific invariants worth having (a non-matching edit changes nothing, a rune is never split, a path outside the root is refused before the file opens, a write is atomic) are already asserted. Fuzzing the SSE reader and tool-argument decoding is accepted — those are the two places third-party bytes become control flow. The error matrix is built rather than tabulated, because a table nothing executes is a wish: `provider.Advise` gives every failure a summary and a next action, never displaces a command's own guidance, and prints at all three sites a turn can fail with a test that fails if a fourth appears without it. `kolk doctor` and `--debug` are accepted and queued, and nothing shipping references either, so no command is promised that does not exist. The security checklist names what holds each property and names the two that nothing holds: ci.yml's actions still float while the release and smoke workflows pin by digest, and prompt injection is undefended — what limits it is the permission floor, not detection, and the doc says so instead of claiming resistance.
**Scope:** how we stay correct as it grows.
**Today:** 22 offline tests incl. e2e via `mockrouter`; SSE fragmentation covered.
**Decide:**
- Test pyramid: unit, `mockrouter` e2e per mode/effort/saga, golden output tests for the TUI, fuzzing the SSE/tool-arg parsers, property tests for edit tool; optional live tests.
- Error UX matrix: 401/402/404/408/429/5xx, network drop mid-stream, model lacking tool support, context overflow — each with a clear message and a next action.
- Observability for us: `--debug` log file with redaction, `kolk doctor` (keys, network, model access, terminal caps).
- Security review checklist: secrets, command injection via tool args, path traversal, prompt injection from files/web, supply chain (deps policy, pinned actions).
**Hardened when:** test plan + error matrix + security checklist committed.
**Inputs:** —

### [x] 22. Onboarding & docs
**Hardened 2026-08-27** ([`docs/plan/22-onboarding-docs.md`](docs/plan/22-onboarding-docs.md)): a doc item is the easiest one to write fiction about, so this one starts from what the binary does — built fresh with an empty HOME and no key, `kolk "hello"` prints three lines, one next action and exits 0, which is already right. "Login with OpenRouter" is refused: pasting a key is one command, while OAuth buys a redirect listener, a token store with refresh semantics, a second class of auth failure and a browser dependency on machines that are often remote shells. A first-run mode picker is refused because every question asked before the first turn is asked before the user knows what the answers mean, and code is a better default than a prompt. The eight-file `docs/` tree is refused as specified: `kolk help` is generated from the command table and cannot drift, so a prose reference would be a second source of truth for the same facts. What replaces it is a rule that makes documentation fiction fail the build — every `kolk <command>` in the README must name a real command, and every slash command in the welcome must exist — asymmetric on purpose, since incompleteness is fine and invention is not. This repo produced two examples in a week: the README's own first line told people to run `go build -o kolk .` against a root with no main package, and an error message drafted one item earlier recommended the unbuilt `kolk doctor`. Demo GIFs are refused for now (a binary that ages the moment the UI moves, cannot be diffed and looks authoritative while being wrong), and `/help` per mode is refused because a help list that differs each time it is read is the confusion a flat list avoids. Built: the orientation line a new session gets and a resumed one does not.
**Scope:** the first 60 seconds and the reference.
**Decide:**
- First run: no key → offer "login with OpenRouter" or paste; free models preloaded and mapped to efforts; pick a default mode; show `/help` once; a 3-line "how to switch mode/effort/model".
- Docs: README (vision + quickstart), `docs/` (commands, config, modes, effort, saga, dashboard, providers, subscriptions policy), demo GIFs (vhs), CHANGELOG.
- Built-in help: `kolk help <topic>` generated from the command table; `/help` contextual per mode.
**Hardened when:** onboarding flow script + docs outline + README rewrite.
**Inputs:** `docs/research/orcli.md`, `docs/research/ecosystem.md`

### [x] 23. Roadmap, phasing & explicit non-goals
**Hardened 2026-08-27** ([`docs/plan/23-roadmap-phasing-non-goals.md`](docs/plan/23-roadmap-phasing-non-goals.md)): the v0.1→v1.0 phases this item proposed were written before any of it existed; the shipped version is v1.2.1 and every milestone but the desktop app and a frozen daemon API has been passed — two of them (MCP, sandboxing) by deciding not to do them, which a version number cannot express. So version-numbered phases are replaced by the phase letters actually in use, done-per-phase is defined against numbers that already fail the build rather than new ones, and the non-goals are collected from refusals that were already argued item by item, each with its source and the condition that would change it. Two lines are marked honestly as exceptions: a hosted service and cloud sync are decided here because nothing had forced the question, and Windows is deferred rather than refused — collecting a list is not licence to promote a deferral into a principle. GitHub milestones are refused for the reason item 22 refused a docs tree: a second copy of the phase list, in a system not versioned with the code, drifts the first week someone reorders a phase. Cost per task is measured but never a gate, since a model price change we do not control should not fail a build. Built: a ratchet on the plan's own bookkeeping — a ticked item must have a document that says it is hardened, a part-done item's document must not, every document must have an item, and every link must resolve — plus the README's roadmap and non-goals section.
**Scope:** order of work and what we refuse to do (for now).
**Decide:**
- Phases (proposal): **v0.1** polish prototype → module path/CI, `model`/`effort` verbs, free-model defaults, effort per mode, stats kept; **v0.2** TUI + multiline, sessions/compaction, `saga`; **v0.3** dashboard (SQLite + `kolk dash`), MCP, parallel agent mode + routing; **v0.4** subscription backends if permitted, sandboxing; **v1.0** daemon API frozen, desktop; **later** iPad.
- Definition of done per phase; what's measured (startup, binary size, test count, cost per task on the dashboard).
- Non-goals for now: Windows polish, plugins in Go, cloud sync, telemetry of any kind, a hosted service.
**Hardened when:** phased roadmap with DoD and the non-goals list, reflected in README and GitHub milestones.
**Inputs:** all of the above

### [~] 24. Subscription provider matrix and login backends
**Scope:** make subscription and plan coverage explicit without conflating consumer accounts with API access.
**Evidence (2026-08-27, [`docs/research/opencode.md`](docs/research/opencode.md) §C.4):** OpenCode carries ten consumer/subscription logins — Codex, Copilot, GitLab, Poe, Cloudflare, Azure, DigitalOcean, xAI, Cerebras, Snowflake — with no special case in its core, because every one is a plugin behind a single `auth` hook. Whether this matrix grows a connector per provider or one seam that connectors plug into is the decision that sets its cost, and it should be made before the second provider, not the fifth.
The inventory and acceptance gates live in [`docs/plan/24-subscription-provider-matrix.md`](docs/plan/24-subscription-provider-matrix.md).

- [~] Anthropic Claude Free/Pro/Max/Team/Enterprise via the user's own CLI handover — shipped
  2026-08-26 as checkpoints P11 (plans, connectors, `kolk plans login`) and B12 (persistent
  `ClaudeBackend` over `claude -p --output-format stream-json`). Two of the three items this row
  listed as open have since shipped and the row did not know: connector→backend selection is B12.9
  (`planBackendFor` picks the provider and refuses an unusable plan model with its reason), and proof
  of authentication is P11.7a/P11.7b (a clean exit records `unverified` and says what that does not
  prove; the first answered turn confirms it). **Both remaining items closed 2026-08-28 and this row
  did not know:** the failure-path tests are B12.15a/b/c (an expired mid-session login, the vendor
  CLI removed underneath a live session, a connector that stopped qualifying after selection), and
  B12.13 was decided — dropping the OpenRouter key outright was rejected, and the order is free
  first, subscription when there is one, free again when there is not.
- [x] OpenAI ChatGPT Free/Plus/Pro/Business/Enterprise via a permitted Codex/first-party CLI path —
  shipped 2026-08-28 as S10 (`internal/provider/agentcli/codex.go` over `codex exec --json`, wired
  at `internal/cli/run.go:482`), gates closed 2026-08-29. Model and effort are switchable inside the
  subscription. The catalogue's ids were corrected against codex-cli 0.149.1 on a real ChatGPT
  login, where `gpt-4.1` is refused outright: the rows had been advertising a model the vendor will
  not serve.
- [ ] Google Gemini Free/AI Pro/AI Ultra/Workspace via documented API access.
- [ ] xAI Grok consumer/business plans.
- [ ] Perplexity Pro/Max/Enterprise.
- [ ] Mistral Le Chat Free/Pro/Team/Enterprise.
- [ ] DeepSeek, Qwen/Alibaba, GitHub Copilot, and Cohere plans.
- [ ] For every provider: terms review, capability matrix, billing-mode labeling,
  offline fixtures, and credential-redaction tests.

### [~] 25. Local models through a host Ollama
**Scope:** the Ollama the user already has — found on the loopback default and adopted read-only,
or started for the session on a port kolk chooses; its models in the picker; Ollama Cloud behind
its own sign-in. Kolk never installs one.
The contract lives in [`docs/plan/25-managed-local-models.md`](docs/plan/25-managed-local-models.md).

- [x] ~~Kolk-owned versioned sidecar, private endpoint, and a model store inside Kolk's data
  directory~~ — superseded by option E (2026-08-29) and deleted: the user's own Ollama is found,
  adopted read-only or started for the session, and its store is the store.
- [x] Runtime lifecycle: start at most once and lazily, on a port kolk chooses, stop only what kolk
  started, close with the session.
- [ ] Hardware probe with the fixed `{accelerators, system_ram_bytes, disk_free_bytes}` shape that
  fails closed to "unknown" and never lets a missing probe authorize a pull.
- [ ] Fit planner: show size, required VRAM/RAM, reserved headroom, and fallback before any pull;
  refuse what does not fit instead of degrading into swap.
- [ ] `/localia` and its CLI twin, with parity tests that need neither a GPU nor Ollama.
**Hardened when:** the contract doc's five TDD checkpoints are closed and a manual GPU smoke test is
recorded as separate evidence.
**Inputs:** items 3, 8, 13, 18

---

## E. Reach — the machine is not the only place you are

These four came from reading [t3code](https://github.com/pingdotgg/t3code) (2026-08-26), which is a
different kind of product: an agent *control surface* that runs no inference and drives other
people's CLIs — Claude Code, Codex, Cursor, Grok, OpenCode — from phone, web and desktop. It is
~715k lines across 393 third-party packages. Kolkrabbi cannot and should not chase that shape: its
whole advantage is being the agent, in one 7.9 MB static binary, under a two-module dependency gate.

But the gap t3code exposes is real and it is not about features. **Kolkrabbi is pinned to the
terminal that started it.** Everything below follows from taking that seriously, in versions that fit
this project's constraints rather than copies of theirs.

### [x] 26. Remote access — steer a session from another device
**Hardened 2026-08-26** ([`docs/plan/26-remote-access.md`](docs/plan/26-remote-access.md)): Kolkrabbi never runs a relay — reaching a machine is Tailscale's or SSH's problem, and a relay is where an open-source agent quietly becomes a SaaS. Tokens are per device and stored only as hashes; pairing is a six-digit code armed briefly, single-use and attempt-capped, on a route that 404s when not armed. Reading and steering are separate tiers with read as the default, and the item 13 floor is unreachable from the network. QR is refused (Reed-Solomon we would have to own) and native mobile apps are refused (two release trains between a fix and its users).
**Scope:** reach a running Kolkrabbi from a phone, a tablet, or another machine, safely.
**Today:** `kolk serve` exists with an SSE event stream, a permission-resolve endpoint and bearer
auth — and a hole: `isLoopback` treats an empty host as loopback, so `--addr :8080` binds every
interface and is accepted with no token. `kolk dash` is loopback-only and read-only.
**Decide:**
- The bind/auth floor: what may be exposed without a token, what may never be, and how a wildcard
  bind is refused rather than guessed at. This is a prerequisite, not a feature.
- Pairing: how a phone learns the token without the user typing 64 characters — QR in the terminal,
  short-lived pairing codes, per-device tokens that can be revoked separately.
- Transport: Tailscale and SSH tunnels are the honest answer for a project that will not ship a
  relay service. Decide whether Kolkrabbi *detects and advises* (`kolk serve --tailscale` printing
  the right URL) or *integrates* (a module, and the budget cannot pay for one).
- The client: a native mobile app is out — it is a second product with its own release train. A
  server-rendered, installable web client over the existing `dash` is in scope. Decide how far it
  goes: read a session, or steer one.
- What "steer" means: answer a permission prompt, send a turn, interrupt, switch model. Each is a
  write endpoint and each needs the permission story of item 13 to hold over a network.
**Hardened when:** the bind/auth floor is specified, pairing chosen, transport decided, and the
write-endpoint list fixed with what each may do.
**Inputs:** item 13 (permissions), item 19 (platform strategy), `internal/serve`, `internal/dash`

### [x] 27. Many sessions, one view
**Hardened 2026-08-27** ([`docs/plan/27-many-sessions.md`](docs/plan/27-many-sessions.md)): discovery over a supervisor, decided by building it — an advisory lock means liveness is observed, so a killed session stops looking live without anything noticing it died, at the cost that nothing can start a session remotely. The card's decisive field is **blocked**: a session waiting on a permission prompt has stopped, is spending nothing, needs a person, and looks exactly like one thinking hard. Read from the journal tail of live sessions only, because I27.2 measured a full decode at 26 ms and a view somebody polls cannot afford more. A Claude-backed session already appears because it is a Kolkrabbi session; a `claude` the user started themselves does not, since a card with five empty fields is the appearance of control without the substance. Two sessions in one checkout are shown, not prevented.
**Scope:** several Kolkrabbi sessions — and the provider CLIs it already drives — visible and
steerable side by side.
**Today:** one session per terminal. `kolk sessions` lists them; nothing shows two running at once.
**Decide:**
- ~~Whether a supervising process owns the sessions or the dashboard discovers them~~ **Decided
  (I27.1)**: discovery. A running session holds an advisory lock on `<sessions>/<id>.lock`, so
  liveness is observed rather than reported — a session whose process was killed stops looking live
  without anything having to notice it died. No daemon, nothing to crash, and no supervisor to keep
  in sync with reality. "Start a session from the phone" stays out of reach and stays out of scope.
- What a session card shows: last turn, context, cost, whether it is waiting on a permission prompt.
  A view that cannot show *blocked* is not a control plane.
- Whether the `agentcli` backends (Claude, and later others) appear as sessions here, which is
  t3code's whole trick and is nearly free given the backend seam already exists.
**Hardened when:** discovery-versus-ownership decided, the session card specified, and the
concurrency story for two sessions editing one repository written down.

### [x] 28. Source control — branches, commits, pull requests
**Hardened 2026-08-27** ([`docs/plan/28-source-control.md`](docs/plan/28-source-control.md)): Kolkrabbi does itself only what must be atomic with something it already owns — the saga's chapter commit — and everything else goes through `bash` under the same confirmation as any command. Branch-per-session is refused: it moves the user's HEAD, has no good ending, and solves a problem checkpoints already solve outside a repository too. Pull requests are GitHub-only through `gh`; Bitbucket and Azure DevOps are refused in writing, each being another REST client, auth flow and set of fixtures for a user Kolkrabbi does not have. `/commit` and `/pr` draft and hand over, never run — a `/commit` that commits without a confirmation is a shell command wearing a costume. The valuable piece is dirty-tree awareness: a session that cannot see uncommitted changes advises on a tree that no longer exists.
**Scope:** the git work around a coding session, not inside it.
**Today:** checkpoints, `/changes`, `/diff`, `/undo`. Nothing touches git.
**Decide:**
- What Kolkrabbi does itself versus what it asks the model to do with `bash`. A `/commit` that
  drafts a message is useful; one that commits without a confirmation is a shell command wearing a
  costume.
- Branch-per-session: whether an agent run gets its own branch by default, and what happens to the
  branch when the session ends.
- Pull requests: `gh` is on most machines and is one exec away; Bitbucket and Azure DevOps are two
  more APIs and two more auth stories. Decide whether anything beyond GitHub is in scope at all, and
  say no in writing if not.
**Hardened when:** the split between Kolkrabbi's own git actions and the model's is fixed, and the
PR provider list is decided — including the refusals.

### [x] 29. Workspace services — what the code you just wrote is doing
**Hardened 2026-08-27** ([`docs/plan/29-workspace-services.md`](docs/plan/29-workspace-services.md)): port discovery ships and is the whole feature — a task starts a dev server and the port it chose is the one fact the user needs and cannot easily get, for the price of a `/proc/net/tcp` read. Only loopback ports get a clickable URL, by the same reasoning as I26.5. Supervision is refused: restart needs to know how it started, logs need somewhere kept, health needs defining per service, and all three need to outlive Kolkrabbi, which means the daemon item 27 just refused. Resource telemetry is refused on the test it failed — nobody could name a decision CPU and memory would change, and a number nobody acts on teaches its reader to ignore panels.
**Scope:** the running program, not the source.
**Today:** nothing. A dev server started by the agent is a process nobody can see.
**Decide:**
- Port discovery: noticing that a task started something listening, and offering it. This is the
  highest-value piece and the cheapest.
- Whether Kolkrabbi supervises long-running processes (restart, logs, stop) or only reports them.
  Supervision is a process manager, and that is a product.
- Resource telemetry: what a session costs in CPU and memory. Interesting; not obviously actionable.
  Decide whether it earns its place or is cut.
**Hardened when:** port discovery specified, the supervise-or-report line drawn, and telemetry either
justified or refused in writing.

## F. Borrowed hardening — three behaviors the field already proved

Added 2026-08-27 after a source-level read of `anomalyco/opencode` @ `5f5ea53` (MIT), recorded in
[`docs/research/opencode.md`](docs/research/opencode.md). Each of these closes a gap that an existing
hardened doc left open, so each gets its own item rather than a silent amendment to a `[x]` file.
All three are small: they are here because they are cheap, not because they are grand.

### [x] 30. The doom-loop guard
**Hardened 2026-08-27** ([`docs/plan/30-doom-loop-guard.md`](docs/plan/30-doom-loop-guard.md)): the item says "nothing at the turn level", which is nearly true — `RunTurn` already ends a turn after two consecutive empty completions, catching a model that has stopped answering but not one that answers plenty while changing nothing. `MaxRoundsFor` is a ceiling, not a detector: at max effort in code mode a model repeating one useless call is stopped on round 51, having been paid for fifty. The saga's `StopDoomLoop` is the right shape at the wrong altitude, counting failed chapters, so a chapter that spins inside one turn never registers. The rule adopted: three consecutive calls with identical canonical arguments **and** identical results are a loop, not a fourth attempt. Both halves are required, and the results half is load-bearing — success is the wrong discriminator, since a test failing differently each run is progress while a read succeeding identically three times is waste. It also kills the obvious false positive: a model that tests, edits and tests again has not made three consecutive identical calls. Only canonical re-serialization is normalized — never trimmed paths or "similar" arguments, because an edit differing by one space is a different edit and over-normalizing is how safety devices get switched off. Fixed at three and deliberately not scaled with effort: effort buys more work, not more permission to repeat the same work. On trigger the call is not executed — asked in the interactive tiers, a logged abort in `/full-auto` (a guard that yields in the mode with the largest budget is decoration), and an auto-denial in a subagent with a second trigger ending the child's turn. An injected "you are looping" notice is rejected as the primary response: it costs a round trip, is advice a looping model is bad at taking, and leaves the call executed — it belongs as the text of the denial. The counter resets per turn, "always allow this tool" is refused because it means "always allow me to waste your budget", and the guard is not a permission rule and must not be expressible as one: `allow bash(*)` answers "is this dangerous?" while this answers "is this futile?"
**Scope:** stop a turn that is repeating itself before it spends the budget.
**Today:** nothing at the *turn* level, which is what this item is about. Note the saga already has a
chapter-level doom guard — `StopDoomLoop` in `internal/engine/saga_budget.go`, threshold 3 — so the
vocabulary and the stop-reason plumbing exist; what is missing is detection inside one turn's tool
rounds. A model that calls the same tool with the same arguments forever is bounded only
by the effort dial's tool-round limit, which is a ceiling, not a detector — and every round before
that ceiling is paid for.
**Prior art:** OpenCode raises a permission request instead of running the call when the last three
settled parts are the same tool with byte-identical input (`DOOM_LOOP_THRESHOLD = 3`,
`session/processor.ts:29,355`), with `doom_loop` defaulting to `ask` (`agent/agent.ts:121`).
**Decide:**
- The trigger: N identical `(tool, args)` calls in a row, and whether N is fixed at 3 or scales with
  effort. Identity by exact argument match, or normalized (trimmed paths, ordered keys)?
- Whether a repeat that *succeeded* counts. Re-reading one file three times is waste; re-running a
  failing test three times is the job.
- What happens on trigger: a confirm prompt, an injected system notice telling the model it is
  looping, or a hard turn abort. Only the first is safe in `full-auto`, and it must not deadlock a
  subagent (item 13 auto-denies in children — decide what auto-deny means here).
- Whether the counter resets per turn or per session, and whether "always allow this tool" is offered.
**Hardened when:** the trigger rule, the response, the `full-auto` and subagent behavior, and the
reset scope are written down with the interaction against item 13's permission tiers.
**Inputs:** `docs/research/opencode.md` §C.2, `docs/plan/13-tools-permissions-sandboxing.md`

### [x] 31. Command-prefix patterns — making "always allow" mean something
**Hardened 2026-08-27** ([`docs/plan/31-command-prefix-patterns.md`](docs/plan/31-command-prefix-patterns.md)): three of the four questions shipped as E13.7, so this item records them and settles the one that was open — whether to replace an eighteen-entry heuristic with a 139-entry arity table. The item said measure first, so 57 commands from this repository's Makefile, CI workflows and scripts were run through `generaliseCommand` and the output read. It was right on 55. **The two failures were not about arity**: `goreleaser check` validates a config file while `goreleaser release` publishes to the internet, and `cosign verify-blob` reads while `cosign sign` signs — so a one-word rule derived from approving the safe subcommand grants the dangerous one. The mistake is not that the prefix was too short; it is that the derived rule permits a strictly more powerful command than the one approved. A table fixes that for the programs it lists and reproduces it exactly for the next one, which is the tail where a table is weakest and its upkeep dearest. So: no table. `commandDrivers` grows by evidence, one program at a time, and this measurement grew it by two, each regression-tested by name. Where a table would live is moot. The exact command as a third choice is refused — that is already `y`, and a stored rule matching one exact invocation will never match again. A compound command may never become an `always` rule, because a rule is a prefix match and a compound command has no prefix that means anything without parsing shell grammar to guess which half was agreed to.
**Scope:** turning a shell command into the pattern a permission answer is stored against.
**Today:** more than this row assumed when it was written. **E13.7 shipped a structural derivation**
in `internal/engine/suggest.go`: `generaliseCommand` takes the first token, or the first two when the
first is one of eighteen known drivers (`git`, `npm`, `cargo`, `docker`…), so `git status --porcelain`
is already offered as `allow bash(git status *)`. A compound command — anything containing `|`, `&&`,
`;`, `>` or a subshell — is never generalised and is offered verbatim, and a destructive first word
(`rm`, `mv`, `chmod`, `dd`…) is never generalised either. `generalisePath` does the same job for
path-taking tools by widening to the directory. The prompt shows the derived rule in full before it
is kept, which is what makes generalising honest.

So the open question is narrower than it looks: **do we replace an eighteen-entry heuristic with a
139-entry table?** — not "how does a command become a rule".
**Prior art:** OpenCode ships a 139-entry prefix→arity table (`permission/arity.ts`) where flags never
count as tokens and the longest matching prefix wins — `git`→2, `npm run`→3, `docker compose`→3,
`cat`→1 — so an approval can offer `git status *` honestly. The table was LLM-generated and the
generating prompt is left in the file.
**Decide:**
- ~~Whether we carry a table at all, or derive the prefix structurally~~ **Provisionally answered by
  E13.7: structural, eighteen drivers.** What remains is whether a table beats it — measure how often
  the heuristic produces a prefix a user would not have chosen, before importing 139 rows of data
  somebody else generated with an LLM and has to keep correct.
- Where it lives: `//go:embed`ed JSON, or Go source. Size budget consequences either way.
- ~~What the confirm prompt offers~~ **Answered by E13.7**: the derived prefix, shown in full, as the
  `a` option beside `y`/`N`. The bare tool is never offered — `allow bash(*)` from one approval is not
  something anyone means. Still open: whether the *exact* command should also be offered as a third
  choice for someone who wants exactly it and nothing more.
- Pipelines, `&&` chains, subshells, and redirection — which token is "the command", and whether a
  compound command may ever be stored as an `always` rule at all. This is where the hardline blocklist
  and this feature meet, and the blocklist wins.
- ~~Whether the same mechanism produces patterns for path-taking tools~~ **Answered by E13.7**: it
  does, via `generalisePath`, which widens to the containing directory and leaves a top-level file
  alone because `write(*)` from one approval is the whole project.
**Hardened when:** the derivation rule, the compound-command refusal, the storage format, and the
prompt copy are fixed, and the interaction with item 13's blocklist is stated.
**Inputs:** `docs/research/opencode.md` §C.1, `docs/plan/13-tools-permissions-sandboxing.md`

### [x] 33. Agentic mode — many models, one goal
**Hardened 2026-08-28** ([`docs/plan/33-agentic-mode.md`](docs/plan/33-agentic-mode.md)): five asks, and reading the code first changed three of them. The agent count is two small pieces rather than a feature — `subagent.started` and `subagent.finished` are already in the protocol, conformance-tested, and **published by nothing**, so the information never leaves the engine. One sandbox is kept and worktree isolation refused: writes are already serialised, so the corruption it would guard against cannot arise, while isolation costs the shared view that is the point of agents working toward one goal — what is added instead is a snapshot per writing subagent, which item 32 made cheap. Cross-provider spin-up needs no plumbing, because the gateway already routes any model id; what is missing is *choosing*, since an empty slot collapses to the effort model and every subagent runs the same one. Subscriptions are simply invisible to routing, and the limit behaviour becomes the user's decision made in advance rather than a guess with their money. And free-for-small-work is a **bug, not a feature**: `FastLaneModel` refuses a free model whenever the main model is paid, so exactly the person who chose a strong model for real work is billed for every session title and commit draft.
**Scope:** what agent mode does when it opens more than one model.
**Today:** four slots and six kinds exist and are configurable; nothing fills the slots, nothing knows about subscriptions, and the fast lane bills paid sessions for mechanical work.
**Hardened when:** done — the doc records the decisions and A33.1–A33.8 are the build.
**Inputs:** `internal/engine/route.go`, `internal/engine/orchestrator.go`, `internal/engine/fastlane.go`, `protocol/events.go`

### [~] 34. Vision completion, independent review, and release truth
**Started 2026-08-31** ([`docs/plan/34-vision-completion.md`](docs/plan/34-vision-completion.md)):
the project now has one forward completion program rather than a mixture of historical phase rows,
open-ended provider ideas, and audit findings. It prioritizes credential and filesystem safety,
concurrency and saga integrity, subscription/model truth, and reproducible release evidence.
**Scope:** define the accepted v1 finish line and the only order in which remaining work may claim
to close it.
**Hardened when:** no — this remains active until the owner accepts the bounded v1 scope, every V34
leaf is closed or deliberately deferred, and the final independent review and clean-machine trial
have evidence.
**Inputs:** `CHECKPOINTS.md`, `docs/plan/10-saga-loop.md`, `docs/plan/21-quality-testing-security.md`,
the 2026-08-31 architecture/security/routing/release review.

### [x] 32. Shadow-git snapshots — checkpoint what `bash` did too
**Hardened 2026-08-27** ([`docs/plan/32-shadow-git-snapshots.md`](docs/plan/32-shadow-git-snapshots.md)): `/undo` restores what kolk's own file tools changed and nothing else, and the README admitting it is honest rather than a fix — the user's model is "kolk changed my files, kolk can put them back", and the carve-out is exactly where a destructive turn lands. Both stores are adopted, not "probably both": the copy store is the only one that works outside a repository or without `git`, and the shadow store is the only one that sees what `bash` did. Measured on this repository rather than guessed — 544 files, a 222 MB `.git` — the first snapshot costs 63 ms, every later one 15 ms, and the store is 148 KB because `objects/info/alternates` reuses the project's blobs instead of rehashing them. Two properties were verified rather than assumed: a `sed -i` made outside kolk shows up in the shadow store, and the user's own `git status` stayed byte-identical throughout, with no index entry, stash or reflog motion. No git version is checked — a version comparison is a guess made once and never revisited, so the probe is the operation itself and any failure falls back to copying for the rest of the session. Cadence is per turn, matching what `/undo` and `/rewind` already mean and what the port's `BeginTurn` already provides. Retention is the session's life, deletion goes with the session, size is reported in `kolk sessions`, and there is no background `git gc` — a daemon collecting garbage a user cannot see is the kind of surprise this project refuses elsewhere. The store is never exposed as a branch or through `/diff --since`: exposing it would make a storage strategy an interface, and there is no branch to offer in a directory that is not a repository. Nothing migrates: existing sessions keep rewinding from their `.bak` files, and the manifest records which strategy captured each turn.
**Scope:** the storage layer under `/undo`, `/rewind`, `/changes` and `/diff`. Not their semantics,
which item 15 already fixed.
**Today:** `internal/checkpoint` snapshots files before `write_file` and `edit_file`. A change made by
`bash` — a formatter, a codegen step, a `rm` — is invisible to `/undo`, and the README says so.
**Prior art:** OpenCode keeps a **separate git dir** at `<data>/snapshot/<project>/<hash(worktree)>`
and drives it with `git --git-dir <store> --work-tree <project>`, so the user's own `.git`, index,
stash stack and reflog are never touched (`snapshot/index.ts:71,75`). It writes
`objects/info/alternates` pointing at the real repository's object store so a snapshot of a huge
checkout reuses existing blobs instead of rehashing (`:224`), reuses the project's `info/exclude`
(`:173`), and configures the store `core.autocrlf=false`, `core.longpaths=true`, `core.symlinks=true`,
`core.fsmonitor=false`, `feature.manyFiles=true` (`:328-333`).
**Decide:**
- Whether we adopt the shadow store or keep copying files. Copying is dependency-free and works
  outside a repo; the shadow store is O(changed blobs) and covers `bash`. Probably both, chosen by
  whether the project is a git repository — which is already how item 13's path jail decides its root.
- Whether `git` is allowed to become a runtime requirement, and what happens when it is missing or
  is a version without `--path-format`. Failing closed to the copy strategy is the obvious answer;
  say it in writing.
- Snapshot cadence: per turn, or per tool call. Per turn is cheaper and matches `/rewind`'s unit.
- Retention and disk: how many snapshots a session keeps, when the store is garbage-collected, and
  what `kolk sessions` reports about its size.
- Whether the shadow store is ever exposed directly (`/diff --since`, a branch the user can check
  out) or stays an implementation detail.
**Hardened when:** the strategy split (repo vs not, git present vs not), the cadence, the retention
policy, and the failure modes are written down, with the migration path for existing checkpoints.
**Inputs:** `docs/research/opencode.md` §C.3, `docs/plan/15-code-mode.md`,
`docs/plan/12-sessions-context-memory.md`, `internal/checkpoint`

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
- `docs/research/opencode.md` (2026-08-27, later than the rest of this appendix) — source-level read
  of `anomalyco/opencode` @ `5f5ea53` (MIT), cloned and read, nothing built or run.
  Takeaways: (1) a **139-entry command-prefix arity table** is what makes an "always allow" bash
  answer honest — item 31; (2) a **doom-loop guard** at three identical settled tool calls, raised as
  a permission request rather than an abort — item 30; (3) **shadow-git snapshots** in a separate git
  dir with `objects/info/alternates` borrowed from the real repo, which checkpoints `bash` changes
  without touching the user's index or stash — item 32; (4) the **plugin hook interface** as one typed
  object of ~20 advisory `(input, output)` points, with `auth` among them, which is how they support
  ten subscription logins with no core special cases — items 16 and 24; (5) compaction constants worth
  a second opinion (prune tool outputs at 2k chars, `PRUNE_MINIMUM` 20k / `PRUNE_PROTECT` 40k,
  preserve-recent clamped 2k–15k, split on turn boundaries) — item 12. Confirmations rather than
  ideas: their `serve` binds `127.0.0.1` by default with basic auth and an explicit CORS allowlist
  (item 26's floor, reached independently), and their TUI is only a client, which is why "steer from
  another device" and "many sessions in one view" were never features there. Refusals recorded: they
  ship **no sandbox at all** (no `sandbox-exec`, `bwrap`, `seccomp` or `landlock` in 3,270 files) and
  no non-bypassable rule, so our hardline blocklist is a stronger claim than the market leader's;
  their `/share` uploads conversations to `opncd.ai`, which item 26 refuses; and nobody has built
  port discovery, so item 29 is uncontested. Weight for the record: 99 direct dependencies in the CLI
  package, 3,236 in the lockfile, 15 patched, on an `effect` 4.0 beta under Bun — against our
  two-module gate.
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
