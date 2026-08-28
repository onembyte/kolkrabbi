# 33. Agentic mode — many models, one goal

Status: hardened on 2026-08-28 · supersedes: — · PLAN.md item 33

## Decision (the short version)

Five asks, and reading the code first changed three of them. Two are smaller than they look because
the machinery already exists and is simply not wired; one is a bug rather than a feature; one is a
genuine design decision; and one is new work with a clear shape.

| Ask | What is actually there | What is missing |
| --- | --- | --- |
| Show how many agents are running | `subagent.started` / `subagent.finished` are **in the protocol** | nothing publishes them, and nothing renders them |
| One sandbox or many | subagents share the tree; writes are serialised by `writesFiles` | a decision, now that item 32 gives snapshots |
| Spin up models from other providers | four slots, six kinds mapped to them, `slot.*` config | an empty slot falls back to **one** model for everything |
| Prefer subscriptions, then ask | plans, connectors, `kolk plans` | routing has **no** subscription awareness at all |
| Free models for small tasks | `SlotFast` → `FastLaneModel()` | the condition is backwards — see §5 |

## Spec

### 1. The agent count belongs above the composer

`protocol.EventSubagentStarted` and `EventSubagentFinished` are declared, documented and conformance-
tested. **The orchestrator never publishes either.** They are a vocabulary with no speaker, which is
why nothing can show a count: the information does not leave the engine.

So this is two small pieces, not one feature. The orchestrator publishes a lifecycle event per
subagent, carrying the task index, its kind and the model chosen. The TUI keeps a count of started
minus finished and renders it in the row above the composer, beside the existing mode/effort/model
status — a place a person is already looking, rather than a new panel.

**What it must not become:** a progress bar. Item 29 refused resource telemetry on the test that
nobody could name a decision it would change; a live agent count passes that test only because it
answers *"is this thing still working, and how wide did it go?"* — the two questions people actually
ask a long orchestrated run. It shows a count, the kinds in flight, and nothing else.

### 2. One sandbox, and the reason is the goal

**Decision: keep one working tree. Do not isolate subagents into worktrees.**

The question is only interesting because the agents share a goal. Isolation buys safety from
concurrent writes and costs exactly the thing an orchestrated run is for: a subagent that edits
`internal/foo` cannot see that another just changed the interface it depends on, so it either
duplicates work or writes against a tree that no longer exists — the same failure item 28 names for
uncommitted changes, moved inside a single turn.

Writes are already serialised (`writesFiles` treats every kind but research and explain as writing,
and `nextRunnable` refuses to launch a writer while one is running), so the concurrent-corruption
case the isolation would buy safety from **cannot arise today**. Isolation would be paying a real
cost for a hazard that is already closed by a cheaper mechanism.

What *is* worth adding is the thing that became possible when item 32 landed: **a snapshot per
writing subagent**, so a task that makes a mess can be rewound alone instead of by hand or by undoing
the whole turn. That is a small use of a store that already exists.

**Revisit if** a run ever wants two writers at once. Then isolation stops being a cost with no
benefit, and the trade changes.

### 3. One slot, one model — and today they all collapse to one

`modelForKind` resolves a task's kind to a slot, and a slot to a model. When a slot is unset it falls
back to `a.modelFor(a.Effort)`, so on a default install **every subagent runs the same model as the
main one**. The slots are the right design; nothing fills them.

The fix is a selection policy, not a new mechanism. When a slot is empty, choose from the live
catalogue by what the slot is *for*:

- **orchestrator** — strongest available; planning is where a weak model costs the most.
- **worker** — tool-capable and known-good at editing; this is the one the user watches.
- **explore** — cheap and high-context; it reads and summarises.
- **fast** — free, always, when a free tool-capable model exists (§5).

Cross-provider is already possible: the gateway routes any model id, so a run whose orchestrator is
one vendor's model and whose workers are three others' is a matter of *choosing*, not of plumbing.
The choice is printed with the plan before anything runs, which the orchestrator already does — so
the user sees "worker → some/model" and can override it with `slot.worker` forever after.

**Ranking uses what is already recorded**: the catalogue's context length, pricing and tool support,
and this machine's own ratings from `stats.jsonl`. A model the user rated badly should stop being
chosen for them, which is the one ranking signal no vendor benchmark has.

### 4. Subscriptions first, then a decision that is the user's

Routing has no idea a subscription exists. `kolk plans` lists them and the claude connector can run
them, but `modelForKind` only knows gateway model ids.

**Order: a configured, verified subscription connector before a metered model, for any slot it can
fill.** A subscription is already paid for; spending API credit while it sits idle is the plainest
waste this project can produce.

When the subscription refuses — a limit, a 429 that outlives its retry — the next step is **the
user's decision, made in advance**: a config key with three values, and no default that spends money
without being asked.

- `ask` (default) — stop and ask, once per session.
- `switch` — fall through to metered, and say so in the transcript.
- `stop` — end the turn and report which limit was hit.

This is item 21's error-matrix shape applied to routing: name the failure, give the next action, never
guess with someone's money.

### 5. Free for small work — and today it is exactly backwards

`FastLaneModel` reads:

```go
if len(a.FreeModels) > 0 && free(a.FreeModels[0]) && (a.Model == "" || free(a.Model)) {
    return a.FreeModels[0]
}
return defaultPaidFastLaneModel
```

The last clause means: **if the session's main model is a paid one, the fast lane refuses to use a
free model and bills a paid default instead.** Someone who chose a strong paid model for their real
work is charged for every session title, every commit-message draft, and every mechanical subtask —
precisely the work that has no need of a strong model.

That is a bug against the item's own intent, not a missing feature. The rule becomes: **a free,
tool-capable model is used for fast-lane and boilerplate work whenever one exists**, whatever the
main model is. The main model is used only when no free model is available, and `slot.fast` overrides
everything for anyone who disagrees.

## Build leaves

- **A33.1 publish subagent lifecycle events** — the orchestrator emits `subagent.started` and
  `subagent.finished` with task index, kind and model. The protocol already defines them.
- **A33.2 the live agent count** — the TUI renders how many subagents are running, above the
  composer, beside the existing status.
- **A33.3 free fast lane** — remove the paid-main-model clause; a free tool-capable model wins
  fast-lane and boilerplate work whenever one exists.
- **A33.4 per-slot model selection** — an empty slot resolves from the catalogue by what the slot is
  for, instead of collapsing to the effort model; printed with the plan.
- **A33.5 ratings inform the choice** — this machine's own 1–5 ratings weight the selection, so a
  model somebody rated badly stops being chosen for them.
- **A33.6 subscriptions first** — a verified subscription connector outranks a metered model for any
  slot it can fill.
- **A33.7 the limit decision** — `routing.on_subscription_limit` with `ask` (default), `switch`,
  `stop`, wired to the retry path and reported in the transcript.
- **A33.8 a snapshot per writing subagent** — a task that makes a mess is rewindable on its own,
  using item 32's store.

## Open questions

- **Should the orchestrator be allowed to change a slot mid-run** when a model keeps failing? It is
  the obvious next thing and it is also how a run becomes unpredictable to the person watching it.
- **How many free models is too many in one run?** Free tiers rate-limit, and eight subagents on one
  free model is one subagent that works and seven that wait.
