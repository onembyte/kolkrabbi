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

**Superseded presentation decision (2026-08-31):** the original surface stopped at a running-agent
count. Live use with concurrent subscription-backed agents showed that the count cannot answer
which task is blocked, which model/effort actually ran it, or what step is currently moving. H10
therefore adds one bounded live row per planned task and a durable typed task-step ledger. This is
still not an estimated progress bar: there are no percentages, elapsed-time theatre, or ETAs. The
new rows report observed state transitions only.

The projection contract is deliberately smaller than the journal. Every typed task transition is
ordered and persisted for later inspection, while the live TUI replaces each task's row with its
latest sanitized step. Independent read-only work continues concurrently; dependencies and
shared-tree writers retain the scheduler's existing serialization rules. Completion milestones may
arrive chronologically, but buffered full reports are emitted in plan-index order so concurrency
never turns the transcript into interleaved prose.

The journal remains authoritative when a live surface falls behind: bounded subscribers disconnect
instead of blocking task publication, and spilled frames replay on reopen with their original
ordering and task/child correlation. A reconnecting surface therefore reconstructs observed work
from the ledger rather than from an in-memory progress guess.

The live row is intentionally one line and one grammar: `agent [i/n] · model · effort · state:
summary — step`. Its fixed state colour is decoration, not information: queued is muted,
waiting/blocked yellow, working purple, done green, and failed red; the state word stays explicit so
`NO_COLOR` produces the same meaning. A monotonic per-task sequence prevents an older concurrent
callback from replacing a newer observed step.

Resize is a projection concern, not a new lifecycle: the runtime redraws the same ordered task
snapshot at the current width. Rows clip to that width and sanitize every task field again at the
display boundary, so reflow cannot expose a stale order, hide the state word, or turn untrusted
planner/provider text into terminal control input.

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

**Order: a configured, verified subscription connector before a metered model.**

*Corrected 2026-08-28 by A33.6:* this said "for any slot it can fill", and that is not buildable as a
ranking. A subscription is not a model id in the gateway catalogue — it is a **backend**, chosen for
the session, and `streamChat` takes a model string against the one `a.Backend` every subagent shares.
Per-slot subscription routing would need per-task backends, which is a different and much larger
change than the sentence implied. What ships is the session-level preference, which is what the ask
was actually about: a signed-in plan is used instead of billing metered credit beside it. A subscription is already paid for; spending API credit while it sits idle is the plainest
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

*Corrected 2026-08-28 by A33.3:* calling this a bug was wrong. The function's own comment says
"if the session model is paid, FastLane uses a high-throughput, low-cost model" — it was **deliberate**,
and the likely reason is reliability, since free tiers rate-limit and this path calls the backend
directly rather than through the turn's rotation. It is a decision being changed, not a defect being
found, and the change carries one fallback so the saving does not buy a session title that sometimes
fails. The rule becomes: **a free,
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
- **A33.6 subscriptions first** — ✓ a verified subscription connector is preferred for the session.
  Per-slot subscription routing is refused: subagents share one backend, so it would need per-task
  backends.
- **A33.7 the limit decision** — `routing.on_subscription_limit` with `ask` (default), `switch`,
  `stop`, wired to the retry path and reported in the transcript. **Built.** Two corrections the
  code forced:

  **The limit is a session fact, not a task fact.** Subagents do not get their own `Agent` — they
  run as methods on the one that spawned them, sharing `Ask`. Eight subagents hitting one exhausted
  plan would have raised eight questions racing for one terminal, which is the exact failure
  `askUser` already refuses for the `ask_user` tool. So the answer is settled once and remembered
  for the session, and a second limit on the model we moved to stops the run instead of switching
  in a circle.

  **Detection is partial, and deliberately so.** Only two shapes are structured: HTTP 402, and a
  gateway that labels `limit_source`. A subscription answered by a vendor CLI returns prose, so
  that case is matched on wording and will miss phrasings nobody has seen yet. The asymmetry is
  the point — a miss costs nothing, because the error surfaces exactly as it does today, while a
  false positive would stop a healthy run. The wording list stays narrow until a real transcript
  widens it. Nothing here guesses at strings a vendor has not actually sent.

  `ask` with nobody to ask is a **stop**, never a yes: a `--full-auto` run, a cron tick or a pipe
  must neither hang on a prompt no one will see nor decide alone to start spending. The stop names
  the setting that changes it, because an error that ends a run without saying what to do about it
  is a dead end.
- **A33.8 a snapshot per writing subagent** — a task that makes a mess is rewindable on its own,
  using item 32's store. **Built.** What "on its own" had to mean, and what it cost:

  **Restoring the tree to the snapshot is not rewinding one task.** It would also discard every task
  that ran after it, which is the whole-turn `/undo` under a new name. So a snapshot records two
  things: the commit, which says what to put back, and the paths that task changed, which say how
  much. Only those paths move; a file the task created is taken back by removing it, since there is
  no earlier version to put there.

  **Measured before shipping.** A whole-tree snapshot of this repository costs **27 ms mean, 58 ms
  worst** over twelve runs. Against a subagent that takes seconds to minutes that is under a
  percent, so the cadence is one snapshot per writing task — not one per run, and not one shared
  between tasks that would have to be untangled afterwards.

  **The window is the writer's own.** The paths are read when the task finishes rather than later,
  because `nextRunnable` will not launch a writer while one is running (checked, not assumed:
  `writing && writesFiles(kind)` skips, and `writing` clears only when the run is received). That
  makes the moment between `BeginTask` and `EndTask` the only one where "what changed" means this
  task and nothing else.

  **Only writing kinds.** Research and explain change no files, so a snapshot for one would record a
  tree identical to the last.

  **A rewind tells the model.** `/undo task <n>` does not trim the conversation the way `/undo`
  does — a turn that ran five subagents is one turn, and trimming it to undo the second would take
  the other four with it. But leaving history describing edits that are gone is exactly the
  divergence `/undo` exists to prevent, so the transcript says what went back.

## Open questions

- **Should the orchestrator be allowed to change a slot mid-run** when a model keeps failing? It is
  the obvious next thing and it is also how a run becomes unpredictable to the person watching it.
- **How many free models is too many in one run?** Free tiers rate-limit, and eight subagents on one
  free model is one subagent that works and seven that wait.
