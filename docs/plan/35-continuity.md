# 35. Continuity — when a limit hits, the work stays

Status: draft 2026-09-05 · awaiting owner answers (§9) · supersedes: the "03 / CONTINUITY" cards on
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
LimitScope  = model | key | account | endpoint       // what the cooldown is keyed on
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

### 2.2 PAUSE — stop honestly when nothing eligible remains

Built second, before any switching, because it is the safe floor every other leaf falls back to.

- Session gains `Paused *Pause {Kind, Scope, Connector, Model, ResetAt, Since, PendingTurn string}`,
  persisted with the session (under the messages lock, V34.2b). The pending user input is kept
  verbatim; it is **not** appended to the transcript as an answered turn.
- A paused session refuses to spend: `RunTurn` returns a `Pause` error naming the reset time and the
  resume path; the TUI status line reads `paused · <reason> · resets <time>`; `/doctor` says it.
- Resume path: `/resume` (or the next prompt — §9 Q6) re-checks the cooldown; if clear, the pending
  turn runs on the same model; if still cooling, kolk says when, and offers RECOMMEND's next move.
- Terminal event: the turn that paused ends with `turn.finished` reason `paused`, `raw_reason` the
  limit — every started turn still has exactly one terminal event (V34.2d).
- Pinned-model invariant: a pinned model that hits any limit pauses; nothing is substituted.
- **Red first:** today a run out of allowance under `stop` returns an error and the pending input is
  gone from the composer with nothing durable saying why.

### 2.3 RECOMMEND — explain the next move

- `Eligible(task) []Candidate`: configured connectors that are `Enabled` and `Verified` (or a stored
  key), not cooling for this scope, tier-eligible for the signed-in plan (V34.4a's gate), and
  capability-fit for the task (tools needed → `supports_tools`; context need → `context_length`).
- Ranking: the user's own ratings (`stats.RatingsByModel`, average then count) → the existing slot
  ranking (`slotrank.go`) → catalog order. Free models rank after paid and subscription unless the
  session is already on free.
- On a limit, kolk prints one block: which backend stopped and why (the `Limit`), when it resets, the
  top recommendation with its billing path and the tradeoffs (tools, context, rating), and the
  command that applies it. Nothing is applied. Published as `provider.limit{action: recommend}`.
- **Red first:** today the message is "out of allowance; `/config set … switch` to continue" — the
  same sentence for every model and every limit.

### 2.4 CHAIN — respect every configured option

- `routing.chain`: an ordered list of connector or model names the user may set; the default chain
  is derived: the current connector's sibling models within the same plan and tier ceiling, then
  other verified subscription connectors, then metered key models, then free — each filtered by
  `Eligible`, each skipped while cooling, each tried once per turn (the `tried` set generalised from
  the free rotation).
- A hop replays the turn's messages (as the free rotation does) and prints
  `◆ <connector>/<model> <kind>; continuing on <next> (<billing>)`, publishes `provider.limit{action:
  rotate|switch}`, and records the switch in the session (`Switches []Switch`) and in stats, so the
  dashboard can show it.
- Money boundary: a hop from a subscription or free model **to a metered model** is never automatic
  under `ask` (§2.5) and requires the explicit opt-in under `auto` (§2.6 and §9 Q3).
- **Red first:** two verified subscription connectors configured; the first hits its allowance; kolk
  offers only the single `MeteredModel` and never the second subscription.

### 2.5 SAFE DEFAULT — ask before free

- When the chain has exhausted subscription and paid candidates, kolk asks — through the existing
  `Ask.Choose` (TUI question overlay, plain-REPL prompt) — showing the free model, its tools and
  context, what retained context will be replayed, and the option to pause instead. The question
  is asked once per run, like `limitDecided` today.
- Config: the two existing knobs are folded into one `routing.continuation` with values
  `ask` (default) `| auto | stop`, plus `routing.allow_metered_switch true|false` for the money
  boundary; the old keys keep working as aliases for one release and are named in the changelog
  (§9 Q4 decides whether to keep them longer).
- **Red first:** `routing.on_free_exhausted` defaults to `free`: today the last paid or subscription
  option failing on an *unpinned* run can land on a free model with no question.

### 2.6 AUTO — automatic switching, opt-in

- `routing.continuation auto`: the chain runs without asking, every hop still printed and published;
  the pinned invariant still holds; a hop onto metered requires `routing.allow_metered_switch true`,
  otherwise auto degrades to a question at that one boundary. Never the default, never inferred.
- **Red first:** there is no such mode; `switch` today is a single move to one model.

### 2.7 Honesty and walk-back

Each leaf flips exactly its card on `site/capabilities.html` (badge and text), the matching line in
`site/llms.txt`, and the `test-site.sh` pins — including inverse pins so the old wording cannot
survive. Nothing flips before its leaf's red→green is on main with CI green on both runners. README
"Known limitations" gains and loses lines in the same commits.

## 3. Build order and leaves

Order is mandatory, each leaf red first under the checkpoint contract:

- **V35.1 DETECT** — `Limit`, `Classify`, fixtures, `Cooldowns` with session and connector
  persistence, `provider.limit` event with schema and changelog, `/doctor limits`, status line.
- **V35.2 PAUSE** — `Pause` on the session, refusal to spend, `/resume`, `turn.finished{paused}`,
  pinned invariant test.
- **V35.3 RECOMMEND** — `Eligible`, ranking over ratings, the explanation block, tier gate from
  V34.4a wired in.
- **V35.4 CHAIN** — default chain derivation, `routing.chain`, generalised `tried`, hop record in
  session and stats, dashboard row.
- **V35.5 SAFE DEFAULT** — `routing.continuation`, the free question, aliases for the old keys.
- **V35.6 AUTO** — the opt-in mode and the metered boundary.

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

## 9. Questions for the owner — the plan is not complete until these are answered

1. **Resume:** should a paused session resume by itself on the next prompt once the cooldown has
   passed, or only on an explicit `/resume`? (Plan proposes: the next prompt resumes if clear, and
   says when otherwise; `/resume` also exists.)
2. **Pinned models:** confirm that a pinned model (`-m`, `/model`) pauses on every limit and is never
   substituted, even under `auto`. Plan 08 §4.2 says so; this plan keeps it.
3. **Money boundary under `auto`:** may automatic switching ever move from a subscription or free
   model onto a metered key without a question? Plan proposes: only with
   `routing.allow_metered_switch true`, otherwise it asks at that one boundary.
4. **Config shape:** fold `routing.on_subscription_limit` and `routing.on_free_exhausted` into one
   `routing.continuation ask|auto|stop` with aliases for one release, or keep the two keys?
5. **Cross-session cooldowns:** a plan-scope limit (your Claude weekly window) is written to the
   connector so another kolk session sees it. Yes?
6. **Default cooldowns when the vendor gives no reset time:** capacity 60 s, allowance 15 min,
   account quota until you act, model refusal until catalog refresh, transport 30 s. Change any?
7. **Chain order default:** siblings on the same plan → other verified subscriptions → metered →
   free. Is that the order you want, and should the four new providers (Google, xAI, Perplexity,
   GitHub) join the chain as they land under plan 24?
8. **Ranking source:** your own ratings first, then kolk's slot ranking, then catalog order. Yes?
9. **Dashboard:** should switches and pauses appear as rows in the local dashboard (plan 17), or is
   the transcript and `/doctor limits` enough for v1?
10. **Sequencing against V34.4a/b/c:** the three engineering leaves you approved (tier gate, fifth
    effort level, four providers) — before this plan's leaves, after, or interleaved? The tier gate
    is an input to RECOMMEND, so the plan assumes it lands first.
