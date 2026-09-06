# 7. The effort dial — fully configurable, including inside code mode

Status: hardened on 2026-08-26 · supersedes: — · PLAN.md item 7

## Decision (the short version)

**Effort is a first-class, five-level dial — `low`, `medium`, `high`, `max`, `ultra` — that controls how much computational and economic budget Kolkrabbi invests in a turn.** It governs five concrete dimensions: (1) model tier mapping (`effort.<level>.model`), (2) provider reasoning effort (`reasoning.effort` / thinking tokens — built 2026-09-05 for the keyed vendor origins only, as `reasoning_effort` projected per vendor from `internal/provider/disposition.go`; the gateway and compatible endpoints still receive no reasoning field, plan 03 §reasoning says why), (3) max tool rounds per turn, (4) orchestration subagent width (`width`), and (5) verification depth. It operates identically across all modes (`code` and `chat`), is live-switchable inside any session via `/effort <level|number>` with immediate model re-resolution and status line update, and is configurable per-user, per-project, and per-mode.

The vocabulary aligns strictly with user muscle memory (matching Claude Code's `low/medium/high/max`) while accepting single-digit numeric shortcuts (`1`, `2`, `3`, `4`, `5`) and preserving seamless backward compatibility with the prototype's legacy names (`quick` → `low`, `standard` → `medium`, `deep` → `high`; `ultra` stopped being a spelling of `max` on 2026-09-05 and is the fifth rung, V34.4b). **Zero-config is preserved**: every unset tier inherits the session's base `model`, and fresh installations without tier configuration run on computed defaults without creating a config file.

---

## Spec

### 0. ★ North star compliance

#### 0.1 The napkin test
```console
# 1. Fresh install without config
$ kolk
# Prompt appears immediately with medium effort computed as default.
kolk-code> /effort
effort: medium (default) → openrouter/auto (inherited)
knobs:  reasoning: medium · rounds: 12 · subagents: 2 · timeout: 120s

# 2. Fast single-digit live switch inside code mode
kolk-code> /effort 3
effort: high → openrouter/auto (inherited) [24 rounds · reasoning: high]

# 3. Dedicated model tiers are opt-in, never required
$ kolk config set effort.high.model anthropic/claude-3-7-sonnet
effort.high.model → anthropic/claude-3-7-sonnet
```

#### 0.2 North star rules compliance

| North star rule | How Item 7 complies | Enforced by |
|---|---|---|
| **1. Zero-config is the product** | All 4 tier models (`effort.{low,medium,high,max}.model`) default to **unset**. Unset tiers inherit the session base `model`. No configuration file is written on startup or first run. | `TestEffortZeroConfigInheritance`, `TestFreshInstallWritesNoConfigFile` |
| **2. Every default computed, not asked** | `medium` is the computed default effort. Reasoning token allocations, round budgets, and widths are computed pure functions of `(effort, mode, modelCapabilities)` without interactive questionnaires. | `TestComputedEffortBudgets` |
| **3. One install command, static binary** | Zero dependencies added. Standard library and existing internal primitives only. | `scripts/check-purity.sh`, `make budgets` |
| **4. One key command** | The effort dial is provider-agnostic. Keys are untouched. For providers with native reasoning support, effort projects onto the provider's schema; for fixed-model keys, effort scales tool rounds and subagent widths. | `TestEffortProviderAgnostic` |
| **5. Complexity ships off, discoverable later** | Per-mode overrides (`mode.code.effort.high.model`), custom reasoning token overrides, and auto-escalation ship turned off. A user simply typing `/effort 1` or `/effort 4` gets intuitive behavior immediately. | `TestLiveNamespaceMatchesGolden` |
| **6. Simple to type beats simple to explain** | `/effort 1` through `/effort 4` provides instantaneous single-keystroke tuning. Words `low`, `medium`, `high`, `max` match industry conventions. | `TestEffortCommandGrammar` |

---

### 1. Vocabulary, aliases & grammar

#### 1.1 Canonical levels
The effort dial has exactly **five** canonical levels (the fifth added 2026-09-05 by owner decision, V34.4b):

```
Level 1: low      (quick, bounded, cheap)
Level 2: medium   (balanced, standard default)
Level 3: high     (thorough, extended reasoning, test verification)
Level 4: max      (exhaustive exploration, multi-pass critic, full budget)
Level 5: ultra    (above max on every dimension; the rung a vendor's own `ultra` is reached through)
```

#### 1.2 Alias resolution table
Any user input or configuration string is resolved through a deterministic mapping:

| Input Token | Canonical Level | Numeric Value | Meaning |
|---|---|:---:|---|
| `low`, `l`, `1` | `low` | 1 | Lowest latency, minimal reasoning tokens, strict tool budgets. |
| `medium`, `med`, `m`, `2`, `standard` | `medium` | 2 | Default standard effort. Balanced reasoning and tool rounds. |
| `high`, `h`, `3`, `deep` | `high` | 3 | High reasoning depth, generous tool rounds, automated build verification. |
| `max`, `x`, `4`, `xhigh` | `max` | 4 | Maximum reasoning depth, critic verification pass, deepest subagent width. |
| `ultra`, `u`, `5` | `ultra` | 5 | Above max: 80/30 rounds, 8 subagents, 900 s; reaches a vendor `ultra` where listed, clamps to the plan's top rung where not. |

Values outside this table reject with:
`invalid effort %q: expected low (1), medium (2), high (3), max (4), or ultra (5)`

---

### 2. The effort matrix (level × mode × knob)

The dial simultaneously governs five knobs across modes:

| Level | Reasoning (`reasoning.effort`) | Thinking Budget Cap | Code Mode Tool Rounds (`max_rounds`) | Chat Mode Tool Rounds (`max_rounds`) | Code Subagent Width (`width`) | Verification Pass | Bash Cmd Timeout |
|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **`low` (1)** | `low` / `minimal` / `none` | $\le 20\%$ or 1,024 tok | 4 rounds | 2 rounds | 1 (no delegation) | None | 30s |
| **`medium` (2)** | `medium` | $\le 50\%$ or 4,096 tok | 12 rounds | 6 rounds | 2 subagents | Standard self-check | 120s |
| **`high` (3)** | `high` | $\le 80\%$ or 16,384 tok | 24 rounds | 12 rounds | 4 subagents | Test / build check after edits | 300s |
| **`max` (4)** | `xhigh` / `max` | $\le 95\%$ or 32,768 tok | 50 rounds | 20 rounds | 6 subagents | Dual-pass critic verification | 600s |
| **`ultra` (5)** | `ultra` where the vendor lists it, else the plan's top rung | as max | 80 rounds | 30 rounds | 8 subagents | Dual-pass critic verification | 900s |

#### 2.1 Mode-specific details

1. **`code` mode (default)**:
   - Tool rounds govern the maximum iterations in `Agent.runLoop`.
   - On `high` effort: after an edit tool modifies files, the agent is prompted to run tests/build commands to verify correctness if the repository has identifiable test runners (`go test`, `npm test`, `cargo test`, `pytest`, `make test`).
   - On `max` effort: before turn completion, a critique pass evaluates diff completeness and regression safety.

2. **`chat` mode (read-only)**:
   - Tool rounds govern read/search inspection (`read_file`, `list_dir`, `grep`, `glob`).
   - Network and write tools are disabled by mode policy (`06-modes.md`).
   - Higher effort permits the read loop to traverse larger dependency graphs or deeper directory trees before synthesizing answers.

---

### 3. Model resolution & tier inheritance

#### 3.1 Model resolution order
On every turn, the effective model for the active turn is resolved via `modelFor(effort, mode)`:

$$\text{Active Model} = \operatorname{coalesce}(\text{ModeEffortTier}, \text{GlobalEffortTier}, \text{SessionBaseModel})$$

1. **Mode-specific effort tier** (if configured): e.g. `mode.code.effort.high.model`
2. **Global effort tier** (if configured): `effort.high.model`
3. **Session base model**: `a.Model` (set via `--model`, `/model`, `model` config, or computed free default)

#### 3.2 Tier inheritance invariant
```go
func (a *Agent) ModelForEffort(effort string) string {
    eff := NormalizeEffort(effort)
    // 1. Check active mode override if present
    if m := a.ModeTiers[a.Mode+"."+eff]; m != "" {
        return m
    }
    // 2. Check global effort tier
    if m := a.Tiers[eff]; m != "" {
        return m
    }
    // 3. Fallback to session base model
    return a.Model
}
```
If all tiers are unset (the default fresh-install state), every effort level resolves to `a.Model`. Zero config always produces a functional agent.

---

### 4. Provider reasoning & token budgeting

When communicating with OpenRouter or OpenAI-compatible backends supporting reasoning:

1. **Unified param emission**:
   ```json
   "reasoning": {
     "effort": "high"
   }
   ```
2. **Projection via `ReasoningSupport.Project` (from `03-provider-layer.md`)**:
   - If the model's catalog metadata reports `supported_efforts: ["high", "medium", "low"]`, requested `max` projects onto `high` with a logged non-fatal notice (`WarnEffortClamped`).
   - If the model requires explicit max reasoning tokens instead of named efforts (Anthropic style), the budget percentage is multiplied by `max_completion_tokens` (clamped between 1,024 and 32,768).
   - If the model's metadata reports `mandatory: true`, effort `none` is never sent.
   - If the provider does not support reasoning parameters (e.g. standard Llama 3 without thinking), the parameter is omitted without failing the call (`WarnEffortDropped`).

---

### 5. Config schema & precedence

Following `18-config.md`'s 5-link resolution chain:
$$\text{Flag} > \text{Environment} > \text{Project Config} > \text{User Config} > \text{Computed Default}$$

#### 5.1 Registered keys

| Key | Type | Default | Environment Var | Flag | Summary |
|---|---|---|---|---|---|
| `effort` | enum(`low\|medium\|high\|max`) | `medium` | `KOLK_EFFORT` | `-e`, `--effort` | Default session effort level |
| `effort.low.model` | string | `""` (inherits) | `KOLK_EFFORT_LOW_MODEL` | — | Model used when effort is low |
| `effort.medium.model` | string | `""` (inherits) | `KOLK_EFFORT_MEDIUM_MODEL` | — | Model used when effort is medium |
| `effort.high.model` | string | `""` (inherits) | `KOLK_EFFORT_HIGH_MODEL` | — | Model used when effort is high |
| `effort.max.model` | string | `""` (inherits) | `KOLK_EFFORT_MAX_MODEL` | — | Model used when effort is max |
| `effort.auto_escalate` | bool | `false` | `KOLK_EFFORT_AUTO_ESCALATE` | — | Automatically bump effort on consecutive errors |

#### 5.2 Legacy config migration
Existing `tiers` blocks in `~/.config/kolk/config.json` migrate transparently:
- `tiers.quick` $\to$ `effort.low.model`
- `tiers.standard` $\to$ `effort.medium.model`
- `tiers.deep` $\to$ `effort.high.model`
- `tiers.ultra` $\to$ `effort.max.model`

---

### 6. REPL & CLI surface UX transcripts

#### 6.1 Interactive slash command
```console
kolk-code> /effort
effort: medium (default)
model:  anthropic/claude-3-5-sonnet (inherited)
knobs:  reasoning: medium · max tool rounds: 12 · subagents: 2 · timeout: 120s
usage:  /effort <low|medium|high|max|ultra> or /effort <1|2|3|4|5>

kolk-code> /effort high
effort: high → anthropic/claude-3-7-sonnet [24 rounds · reasoning: high · verify: on]

kolk-code> /effort 1
effort: low → openrouter/auto [4 rounds · reasoning: low · verify: off]
```

#### 6.2 Status bar integration
The persistent TUI footer reflects the active effort and resolved model in real time:
```text
──────────────────────────────────────────────────────────────────────────────
session: s_20260826-003618-9a2c | model: anthropic/claude-3-7-sonnet
effort: high (3) | mode: code | approval: ask | ready
──────────────────────────────────────────────────────────────────────────────
```

#### 6.3 Command line execution
```console
# Launch with explicit effort
$ kolk -e high "diagnose failing race condition in internal/bus"

# Set persistent default effort
$ kolk config set effort high
effort → high

# Map a frontier model to high effort
$ kolk config set effort.high.model anthropic/claude-3-7-sonnet
effort.high.model → anthropic/claude-3-7-sonnet
```

---

### 7. Escalation & de-escalation policy

1. **Automatic escalation (`effort.auto_escalate: true` / opt-in)**:
   - When a turn encounters 2 consecutive tool errors or test failures on the same step, effort escalates by +1 level (e.g. `medium` $\to$ `high`).
   - The engine logs: `◆ escalating effort: medium → high (step retrying with expanded reasoning and tool budget)`.
   - The escalation is scoped strictly to that turn or until the step succeeds; it does not permanently overwrite the session default.

2. **Internal de-escalation for auxiliary tasks**:
   - Internal helper sub-turns (such as session auto-titling, diff summary generation, and commit message drafting) force `low` effort internally.
   - This prevents background fast-lane tasks from burning high reasoning budgets or frontier model costs.

---

### 8. External agentcli (subscription) mapping

When Kolkrabbi spawns the user's local `claude` (Claude Agent) CLI (`04-subscription-backends.md`):
- `claude` natively accepts `--effort <level>`.
- Kolkrabbi passes `--effort low`, `--effort medium`, `--effort high`, or `--effort max` straight through to the child process.
- The user's muscle memory works identically whether running OpenRouter models or local subscription agents.

---

## Rationale

1. **Alignment with Claude Code muscle memory**: Claude Code developers intuitively use `low`, `medium`, `high`, `max`. Kolkrabbi matches this standard while maintaining backwards compatibility for existing `quick/standard/deep/ultra` users.
2. **Multi-dimensional control**: Users do not want to configure 5 separate knobs (tool rounds, timeout, reasoning percentage, subagent width, verification) before asking a hard question. A single coherent dial sets all five in balance.
3. **Decoupled model tiers from base model**: Allowing `effort.<level>.model` to inherit the session base model guarantees zero-config out of the box, while giving power users the flexibility to route `low` to a fast cheap model (e.g. `meta-llama/llama-3.3-70b-instruct`) and `high`/`max` to frontier reasoning models (e.g. `anthropic/claude-3-7-sonnet` or `openai/o3-mini`).

---

## Alternatives rejected

- **Continuous 0.0–1.0 slider**: Rejected because discrete levels match provider reasoning parameters and concrete tool budget thresholds; continuous floats create ambiguous expectations.
- **Requiring manual tier setup on first run**: Rejected as a direct violation of North Star Rule 1.
- **Separate effort dials per tool**: Rejected as unnecessary cognitive overhead. The mode already dictates tool availability; the effort dial dictates thoroughness.

---

## Risks & open questions

- **Risk: Provider rate limits on high reasoning models**: When `high` or `max` is active, models consume more tokens and can trigger pre-stream 429 errors.
  *Mitigation*: Bounded exponential backoff retry handler (already implemented in `U0.1e`) gracefully catches and handles 429s without aborting the turn.
- **Risk: Overspending on `max` effort**: Long tool loops (up to 50 rounds) on frontier models can become expensive.
  *Mitigation*: The TUI footer displays cumulative session cost in real time, and `/cost` / per-turn stats keep costs transparent.

---

## Sources

- `docs/research/openrouter.md`: OpenRouter reasoning specification (`reasoning.effort`, `supported_efforts`, `mandatory`).
- `docs/research/ecosystem.md`: Codex, Claude Code, Hermes, and Amp effort dial paradigms.
- `docs/plan/02-architecture.md`: Performance and layer budgets.
- `docs/plan/03-provider-layer.md`: `ReasoningSupport.Project`, `WarnEffortClamped`.
- `docs/plan/04-subscription-backends.md`: Claude Agent `--effort` CLI integration.
- `docs/plan/06-modes.md`: Mode boundaries (`chat` vs `code`).
- `docs/plan/18-config.md`: Config key registry and 5-link resolution.

---

## Checkpoint breakdown

Implementation of Item 7 is split into 4 atomic, independently testable checkpoints:

- **E7.1 Vocabulary & Canonical Normalization**: Add `low/medium/high/max` with numeric `1..4` and legacy `quick/standard/deep/ultra` aliases to `internal/engine` and `internal/cli`.
- **E7.2 Effort Knob Matrix**: Wire tool round limits (`max_rounds`), orchestration width, and reasoning token parameters into `Agent.runLoop` and `Agent.plan`.
- **E7.3 Config & Tier Resolution**: Wire `effort.<level>.model` resolution into `internal/config`, `Agent.modelFor`, and `internal/cli/cmd_config.go`.
- **E7.4 Interactive REPL & Slash Surface**: Wire `/effort <level|num>` with immediate model re-resolution, transcript echo, and TUI footer updates.
