# 35. Continuity — when a limit hits, the work stays

Status: decided 2026-09-05 (owner answers folded in, §9) · supersedes: the "03 / CONTINUITY" cards on
`site/capabilities.html` as the only statement of this policy · PLAN.md item 35

## Decision (the short version)

Six of the seven continuity cards the capabilities page promises are `Designed` or `Planned`. What
ships is the narrow part: a free model that answers 429 is swapped for the next free model when the
user never pinned one; a subscription that says "out of allowance" is met with one question, one
metered fallback, or a stop, by `routing.on_subscription_limit`; and `routing.on_free_exhausted`
decides whether the last free model's 429 may become a paid model. Everything else — knowing *which*
limit stopped the work, explaining the next move, walking a chain of every configured option,
asking before free, switching automatically by opt-in, and pausing honestly — is prose.

**Build one continuity engine in the engine layer, over the knobs that already exist, in six leaves
that each flip one card.** Every limit becomes a classified, scoped, time-bounded fact before any
policy reads it. Every continuation is visible: said in the transcript, published on the bus, and
recorded in the session. The pinned-model invariant of plan 08 §4.2 is untouched: an explicit model
choice is never left automatically; a pinned model that hits a limit is *paused*, not replaced. No
policy ever moves money without a question the user has answered — once, in config, or at the
moment. The safe default of the page is the safe default of the code: ask before free, never
auto-switch by default, stop honestly when nothing is left.

## 1. What ships today, exactly

Read from `internal/engine/retry.go` and `subscription_limit.go` on 2026-09-05, not from memory:

| Behaviour | Where | Contract it already keeps |
|---|---|---|
| Pre-stream error classified; `Retry-After` honoured; bounded backoff `rateLimitRetryDelays`, capped by `maxRateLimitRetryDelay` | `streamChatOnObserved` | plan 08 §4.2 backoff |
| Unpinned **free** model on 429 → next untried free candidate, turn replayed, `◆ free model rate-limited (429); rotating to …` | same | plan 08 §4.1 |
| Last free model exhausted → `routing.on_free_exhausted` `stop \| free \| paid`; `paid` moves to the one `MeteredModel` | same, `free_exhausted.go` | plan 08 "never silently route to paid" — `paid` is an explicit setting |
| Subscription allowance (402, or 429 with `limit_source` naming plan/quota/credit, or a phrase) → `routing.on_subscription_limit` `ask \| switch \| stop`; decided once per run (`limitDecided`) | `subscription_limit.go` | one question per run, never repeated |
| Host refusal (Ollama) explained, never rotated | `hostRefusal` | plan 25 |
| Vendor `rate_limit_event` frames on the Claude handover: `allowed_warning` → warning; `rejected` → **planned** `Cooldowns.Mark`, never built | plan 04 T15–T17 | the cooldown registry is paper |

What is missing is not a knob. It is that the code has one notion of "limit" with two branches, no
memory of a limit across calls or sessions, no scope (model, key, plan, endpoint), no reset time kept
beyond the current retry loop, no ranking of what to try next, no record of a switch, and no state
in which a session can honestly stop and later resume.

## 2. Spec

### 2.0 Vocabulary, closed

```
LimitKind   = subscription_allowance   // the plan's window: Claude/ChatGPT usage limits, 402 credits_required on a plan row
            | account_quota            // metered account out of credit: 402, "out of credit", insufficient balance
            | endpoint_capacity        // 429 without an allowance source; 5xx overloaded; provider-side capacity
            | budget_stop              // kolk's own ceilings: MaxRunCostUSD, saga cost/chapter/time
            | model_refusal            // 400/404 for this model: policy, context length, unsupported feature, gone
            | transport                // could not reach the endpoint at all
LimitScope  = model | account | endpoint             // what the cooldown is keyed on; "key" was
                                                     // dropped in V35.1b: OpenRouter's bare 429 is the
                                                     // model's (rotation on the same key works), and a
                                                     // key-wide cooldown would cool every model at once
Limit       = {Kind, Scope, Model, Connector, ResetAt time.Time (zero = unknown), RetryAfter, Message (scrubbed), Source}
```

`Source` says how kolk knows: `retry-after`, `limit_source`, `vendor frame`, `phrase`, `kolk`. A
limit whose `ResetAt` is unknown gets a default cooldown per kind (§2.1); it is never treated as
permanent, and never as zero.

### 2.1 DETECT — one classifier, one cooldown registry

- `internal/provider/limit.go`: `Classify(err error, connector Connector) (Limit, bool)` over
  `*HTTPError` (status, `LimitSource`, `RetryAfter`, `Message`, `Origin`), the existing
  `subscriptionLimited` phrases (moved here, kept), the Claude handover's `rate_limit_event`
  (`status: rejected` → `subscription_allowance`, `resetsAt` → `ResetAt`; `allowed_warning` →
  a `Warning`, not a limit), kolk's own budget stops, and transport errors. Pure, table-driven, one
  fixture per row in `spec/testdata/limits/*.json`.
- `internal/engine/cooldowns.go`: a registry keyed by `Scope+Connector+Model`, `Mark(limit)`,
  `Cooling(key) (until time.Time, bool)`, persisted as `cooldowns.json` in the **session** directory
  (a plan-scope limit also written to the **connector** so a second session in the same hour does not
  hit the same wall — plan 04 §7 asked for exactly this). Default cooldowns when `ResetAt` is
  unknown: capacity 60 s, allowance 15 min, quota until the user acts, refusal for the model until the
  catalog refreshes, transport 30 s. Never a loop: a key that is cooling is skipped, not retried.
- Protocol: one new event `provider.limit` `{kind, scope, model, connector, reset_at?, action}` where
  `action ∈ retry | rotate | recommend | ask | switch | pause | stop`, published every time a limit
  is classified. Schema, golden fixture, changelog bullet, spec gate.
- Surfaces: `/doctor` gains a `limits` section listing cooling keys with reset times; the status line
  shows `cooling · <connector> · resets 14:05` while the session's own connector is cooling.
- **Red first:** a 429 with `limit_source: "plan"` and a 429 with none are the same error to kolk
  today; a `rate_limit_event{rejected}` writes nothing anywhere.

### 2.2 PAUSE and RESUME — the default: stop, then continue by itself, spending nothing to wait

*Shipped 2026-09-05 as V35.2a–c: the pause on the session, the token-free resume monitor with
`continuity.resume auto|manual` and `/resume`, and the three surfaces. The PAUSE card is flipped.*

Owner decision: **by default kolk stops when a limit hits and resumes automatically when the limit
resets; the wait costs no tokens.** Switching models is a separate opt-in (§2.4).

- Session gains `Paused *Pause {Kind, Scope, Connector, Model, ResetAt, Since, PendingTurn string}`,
  persisted with the session (under the messages lock, V34.2b). The pending user input is kept
  verbatim; it is **not** appended to the transcript as an answered turn.
- A paused session refuses to spend: `RunTurn` returns a `Pause` naming the reset time; the TUI
  status line reads `paused · <reason> · resumes <time>`; `/doctor` says it.
- **The resume monitor is code, not a model.** It runs inside kolk, the way Claude Code runs its own
  monitor scripts: a goroutine per paused session that sleeps until `ResetAt` (or the default cooldown
  when the vendor gave none, §2.1), then confirms the limit is gone **without spending tokens** — the
  OpenRouter key-status endpoint for keyed models, the vendor's quota-free auth/status command for a
  handover (plan 04 `AuthArgv`), a cheap `/models` probe for a compatible endpoint — and only then
  re-runs the pending turn on the same model. If the probe still says capped, it backs off to the next
  reset and says so. Cancelling the session cancels the monitor; nothing runs after exit.
- `continuity.resume = auto (default) | manual`: `manual` keeps the pause and waits for `/resume`.
  `/resume` always works.
- Terminal event: the turn that paused ends with `turn.finished` reason `paused`, `raw_reason` the
  limit; the resumed run is a new turn (V34.2d holds: one terminal event per started turn).
- Pinned-model invariant: a pinned model pauses and resumes on itself; nothing is substituted.
- **Red first:** today a run out of allowance under `stop` returns an error and the pending input is
  gone with nothing durable saying why, and nothing brings it back.

### 2.3 RECOMMEND — explain the next move

*Shipped 2026-09-06 as V35.3a–c: `continuity.Recommend`, the block after a pause or stop, the
CLI's candidate list, the card. Keyed vendor origins join the list with their live listers.*

- `Eligible(task) []Candidate`: configured connectors that are `Enabled` and `Verified` (or a stored
  key), not cooling for this scope, tier-eligible for the signed-in plan (V34.4a's gate), and
  capability-fit for the task (tools needed → `supports_tools`; context need → `context_length`).
- **Equivalence (owner: "similar models", "review that equivalency on ratings").** Two models are
  equivalent when they sit on the **same rung of the effort ladder** in their vendors' rosters — the
  routing tables already pair rungs to levels per vendor (`ceiling.go`, `level_routing.go`), so
  `claude-fable` at `max` is equivalent to the Codex `max` rung, not to a free 7B. Within a rung the
  order is the user's own ratings (`stats.RatingsByModel`, average then count), then kolk's slot
  ranking, then catalog order. One rung *above* is acceptable; more than one rung *below* is never
  equivalent, and a free model is never equivalent to a subscription or paid rung unless the user put
  it on the preferred list (§2.4).
- On a limit, kolk prints one block: which backend stopped and why (the `Limit`), when it resets and
  that it will resume then (default), the top equivalent recommendation with its billing path and
  tradeoffs, and the command that applies it. Nothing is applied unless §2.4 says so.
- **Red first:** today the message is "out of allowance; `/config set … switch` to continue" — the
  same sentence for every model and every limit.

### 2.4 CONTINUITY — switching, an opt-in with three shapes

Owner decision, verbatim in structure: `continuity.mode = off (default) | on`. With `on`, a second
setting opens:

```
continuity.mode      off | on                      # default off: pause, resume on the same model
continuity.select    auto | preferred | ask         # only read when mode is on; default auto
continuity.preferred [models…]                      # subs, paid or free; read by preferred, and it
                                                    # widens what auto may reach (free only if listed)
continuity.order     [subs, paid, free]             # default; user-configurable
continuity.resume    auto | manual                  # §2.2
```

- **auto:** the chain (§2.5) is walked without asking, **only across equivalent models** (§2.3), in
  `continuity.order`: every verified subscription first, then paid keys, then free — and free only if
  the model is on `continuity.preferred`. The effort is carried across: a turn at `max` on fable
  continues at the `max` rung of the next vendor. If no equivalent eligible model exists, kolk does
  **not** continue; it pauses and resumes (§2.2) and says why.
- **preferred:** the chain is exactly `continuity.preferred`, in that order, filtered by eligibility
  and cooling; equivalence is not enforced, because the user wrote the list.
- **ask:** the recommendation block (§2.3) becomes a question through `Ask.Choose` — the top
  equivalent candidate, the next one, "pause and resume later" — asked once per run.
- Every switch is **printed in the console** (`◆ <connector>/<model> <kind>; continuing on <next> at
  <effort> (<billing>)`) and published as a `provider.limit{action: switch}` event for the TUI and
  streams. **Nothing about switches is persisted** (owner: no dashboard rows, no session record).

### 2.5 CHAIN — respect every configured option

*Shipped 2026-09-06 as V35.4: `Agent.ContinueOn` and `/continue [n]`, `Options.Switch`, the
preferred chain behind `Select`/`Preferred` (fed by V35.5), cooldowns reloaded before each hop.*

- Default chain derivation: for the current effort rung, the equivalent models of every verified
  subscription connector in the order configured, then metered key models, then free models on the
  preferred list; each filtered by `Eligible`, each skipped while cooling, each tried once per turn
  (the `tried` set generalised from the free rotation). A hop replays the turn's messages as the free
  rotation does today.
- **Cross-session awareness (owner: "active sessions should be aware of the limits").** A plan- or
  account-scope limit is written to the connector-level `cooldowns.json`; every running session
  watches that file (fsnotify-free: a cheap mtime poll on the monitor's tick) and treats a cooling
  connector as ineligible at its next hop. No session waits on another; each learns.
- **Red first:** two verified subscription connectors configured; the first hits its allowance; kolk
  offers only the single `MeteredModel` and never the second subscription.

### 2.6 What replaces today's two knobs

`routing.on_subscription_limit` and `routing.on_free_exhausted` are superseded by `continuity.*`.
They keep working for one release as aliases (`switch` → `mode on, select auto`; `stop` → `mode
off`; `on_free_exhausted paid` → `order` with paid before free), are named in the changelog and in
`/config` output as deprecated, and are removed in the release after.

### 2.7 Honesty and walk-back

Each leaf flips exactly its card on `site/capabilities.html` (badge and text), the matching line in
`site/llms.txt`, and the `test-site.sh` pins — including inverse pins so the old wording cannot
survive. Nothing flips before its leaf's red→green is on main with CI green on both runners. README
"Known limitations" gains and loses lines in the same commits.

## 3. Build order and leaves

Order is mandatory, each leaf red first under the checkpoint contract. V34.4a's tier gate lands
before V35.3 because RECOMMEND reads it; the fifth effort level (V34.4b) and the four providers
(V34.4c) follow this plan.

- **V35.1 DETECT** — `Limit`, `Classify`, fixtures, `Cooldowns` with session and connector
  persistence, `provider.limit` event with schema and changelog, `/doctor limits`, status line.
- **V35.2 PAUSE and RESUME** — `Pause` on the session, refusal to spend, the code-only resume monitor
  with token-free probes, `continuity.resume`, `/resume`, `turn.finished{paused}`, pinned invariant.
- **V35.3 RECOMMEND** — `Eligible`, rung equivalence across vendors, ranking over ratings, the
  explanation block, tier gate wired in.
- **V35.4 CHAIN** — default chain derivation, generalised `tried`, effort carried across, console
  line and event per hop, cross-session cooldown awareness.
- **V35.5 CONTINUITY settings** — `continuity.mode/select/preferred/order/resume`, `preferred` and
  `ask` shapes, aliases for the two old keys, `/config` surface.
- **V35.6 AUTO** — the `auto` shape end to end: equivalence enforced, free only when preferred, no
  equivalent → pause, every hop printed.

Each leaf: scope with non-goals; red observed; green; `-race` on engine, session and cli; the spec
gate for any event change; `make check`; record in `CHECKPOINTS.md` and `docs/build-log.md`; the
card flip in the same commit as the record.

## 4. Tests that make it bulletproof

- Classifier table: one fixture per `LimitKind × Source`, including the three vendor frame shapes
  from plan 04 T15–T17 and kolk's own budget stops; a mutation test per row.
- Cooldowns: persistence round-trip; a second session in the same connector sees the plan cooldown;
  expiry; never-loop (a capped key is asked once per cooldown, proven with a counting backend).
- Pause: pending turn survives a restart; a paused session spends nothing (a counting client);
  resume after expiry runs the pending turn once; resume before expiry says when.
- Chain: two subscription connectors, one metered, two free, with every combination of cooling and
  pinned; each turn tries a candidate at most once; the transcript names every hop; the money
  boundary is never crossed under `ask` and is crossed under `auto` only with the flag.
- Ask: the question carries model, tools, context and retained-context size; "pause" is offered;
  asked once per run.
- Events: every hop and pause publishes exactly one `provider.limit`; every started turn keeps one
  terminal event.
- Surfaces: TUI status line and question overlay under `-race`; `/doctor limits`; dashboard row.
- Walk-back: the site guard's inverse pins for all six cards.

## 5. Rationale

The page promises a policy; the code has two switches. Users hit these walls at the worst moment —
mid-turn, with context they do not want to lose — and the difference between "stopped, here is why,
here is when, here is the next move" and "error" is the product. Classification first because every
later decision reads it; pause second because it is the floor nothing can fall below; recommendation
before chain because a chain that cannot explain itself is the silent switching the page refuses.

## 6. Alternatives rejected

- **Silent auto-switching as default.** Refused by plan 08 and by the page; billing path changes are
  the user's decision.
- **A single metered fallback forever.** It is what ships; it ignores every other configured
  option and cannot know it is looping.
- **Per-provider special cases in the retry loop.** The loop already has two; a third makes it
  unreadable. One classifier, one registry, one chain.
- **Cooldowns only in memory.** A wall is hit by the next session too; plan 04 §7 asked for a
  durable registry for that reason.

## 7. Risks and open questions

- Vendor `limit_source` and messages drift; the classifier degrades to `endpoint_capacity` with a
  default cooldown and never to a stop. Fixtures pin what was seen and when.
- A wrong default cooldown either loops (too short) or stalls (too long); the values in §2.1 are
  starting points and are logged with their source so a user can see them.
- Replaying a turn onto a different vendor changes prompt-cache economics; the hop line says so.

## 8. Sources

`internal/engine/retry.go`, `subscription_limit.go`, `free_exhausted.go`, `slotrank.go`;
`internal/provider/http_error.go`, `advice.go`, `connectors.go`; `internal/stats/ratings.go`;
plan 04 §7 and T15–T17; plan 08 §4; plan 24 checklist; `site/capabilities.html` "03 / CONTINUITY";
`scripts/test-site.sh` continuity pins.

## 9. Owner answers — 2026-09-05

1. **Resume:** by default kolk stops on a limit and resumes by itself when the limit resets, through
   a monitor that is code and spends no tokens; a config turns automatic resume off. (§2.2)
2. **Pinned models / switching:** switching is a separate opt-in. `continuity.mode off|on`; with `on`,
   `continuity.select auto|preferred|ask`. `auto` picks the best *equivalent* model from another
   subscription first, then paid; it never drops to a far-lower category (fable → a free 7B) unless
   the user listed that model in `continuity.preferred`. (§2.4)
3. **Money boundary:** `auto` may move onto paid, but only between similar models; equivalence is
   reviewed against ratings. Implemented as rung equivalence first, ratings second. (§2.3)
4. **Config shape:** as in 2; the two old keys become aliases for one release. (§2.6)
5. **Cross-session:** active sessions must be aware of limits. (§2.5)
6. **Default cooldowns:** the default is a monitor inside kolk that watches for the reset and probes
   without spending; the fixed durations of §2.1 apply only when the vendor gave no reset time and no
   probe exists. (§2.2)
7. **Chain order:** all subscriptions first, then paid, then free; user-configurable; auto by default.
8. **Ranking:** owner deferred to the plan; decided: effort-rung equivalence across vendors, then the
   user's own ratings, then kolk's slot ranking, then catalog order.
9. **Dashboard:** no.
10. **Switch visibility:** shown in the console; not persisted. Sequencing (not asked back): the tier
    gate (V34.4a) lands first because RECOMMEND reads it; the fifth effort level and the four providers
    follow this plan.
