# 14. Orchestration & per-task model routing

Status: hardened on 2026-08-26 · supersedes: — · PLAN.md item 14

## Decision (the short version)

Five facts about `internal/engine/orchestrator.go` as it stands today, each read out of the code
rather than assumed:

1. **One failed subagent throws the whole run away.** `runOrchestrated` returns
   `fmt.Errorf("subagent %d: %w", …)` the moment any subagent errors. Every result produced before
   it is discarded, the user sees an error instead of an answer, and the tokens are already spent.
   A subagent that merely runs out of rounds is worse: it returns `""` and an error, so its work is
   dropped even though it exists.
2. **Everything runs on one model.** `model := a.modelFor(a.Effort)` is passed to the planner, to
   every subagent, and to synthesis. The stated goal of this item — many agents, each on the model
   that suits its task — is not started. There is a routing table already (`Tiers`, effort → model)
   and a fast lane (`FastLaneModel`), and neither is used here.
3. **Subagents run strictly one at a time**, and each is handed the results of all previous ones.
   That is a real dependency chain, not an accident, but it means a plan of six independent tasks
   takes six times as long as it needs to.
4. **There is no budget cap.** Six subagents at `max`, each up to `MaxRoundsFor` tool rounds, is an
   unbounded amount of money for a request the user typed in one line.
5. **A task is a bare string.** There is nowhere to record what kind of work it is, which model it
   should get, or which earlier task it actually depends on — so none of those can be decided.

Points 1 and 4 are the ones that matter first. Orchestration is the feature where the user is
*least* able to supervise what is happening, and it is currently the feature that fails hardest and
costs the most. Routing is the point of the item, but a router bolted onto a run that discards its
own work on the first error would just spend money faster.

So the order is: make a run survivable and bounded, give tasks enough structure to route, then
route, then parallelise. Parallelism comes last deliberately — it multiplies every failure mode
above, and it is the only one of the four that nobody is blocked on.

## Spec

### 1. A task is a record, not a string

```go
// Task is one unit of orchestrated work.
type Task struct {
    Title   string   // what to do, as the planner wrote it
    Kind    Kind     // edit | test | research | explain | design | boilerplate
    Needs   []int    // indices of tasks whose results this one requires
    Model   string   // resolved by the router; empty until then
}
```

`Needs` is what makes everything else possible: it is the difference between "task 4 comes after
task 3" and "task 4 needs what task 3 produced". Today the code assumes the former and implements
the latter by accident, by concatenating every earlier result into every later briefing. With
`Needs` a subagent gets only the results it asked for — smaller context, less cross-contamination,
and a dependency graph that can be run in parallel later without changing anything else.

The planner returns `Kind` and `Needs` itself, in the same strict-JSON reply it already produces. A
planner that omits them yields `Kind: KindUnknown` and `Needs: [all earlier tasks]`, which is
exactly today's behaviour — so a weaker planner model degrades to what already works instead of
failing.

### 2. Routing: kinds map to slots, slots map to models

Two levels, because collapsing them is what makes routing tables unmaintainable.

| Kind | Slot | Why |
|---|---|---|
| `research`, `explain` | `explore` | Reading and summarising. Cheap, high-context, no editing. |
| `edit`, `test` | `worker` | The session model. This is the work the user is paying attention to. |
| `design` | `orchestrator` | Planning-shaped: the strongest model available. |
| `boilerplate` | `fast` | Mechanical. The existing fast lane. |

Slots resolve through config, then through the effort tiers already in `Tiers`, then to the session
model. A user who has configured nothing gets the session model for everything, which is today's
behaviour and is never surprising. A user who sets one slot changes one thing.

```json
{
  "slots": {
    "orchestrator": "anthropic/claude-opus-4",
    "worker":       "",
    "explore":      "google/gemini-2.5-flash",
    "fast":         "google/gemini-2.5-flash"
  }
}
```

**Classification is the planner's job, not a separate call.** A dedicated classifier pass was the
obvious design and is the wrong one: it adds a round trip and a second thing that can be wrong about
a task the planner just wrote. The planner already knows what it meant.

**Routing is always overridable and always visible.** The plan is printed before the run with the
model beside each task, because a run that quietly moves work to a different model is a run whose
cost and quality the user cannot account for.

### 3. Failure handling: a run reports, it does not vanish

This is the largest behavioural change and the one worth the most.

| Failure | Today | Decided |
|---|---|---|
| Subagent errors | whole run aborts, all results lost | task is marked failed with its reason; the run continues |
| Subagent exhausts rounds | returns `""` and an error → aborts | its partial work is kept and marked incomplete |
| A task's dependency failed | n/a | the task is skipped, marked blocked, and says which task blocked it |
| Every task fails | whole run aborts | synthesis still runs and says nothing succeeded |
| Context cancelled | aborts | aborts — the user asked it to stop, and that is not a failure to report |

Synthesis receives the failures alongside the successes and is told to report them. An orchestrated
answer that silently omits the third of six tasks that did not work is worse than no orchestration:
the user has no way to know the answer is partial. **Uncertainty is reported in the answer, not in a
log line the user has already scrolled past.**

A run where a majority of tasks failed prints a plain summary line before the answer, because at
that point the interesting fact is not the synthesis.

### 4. Budget caps

A cap on **money**, not on rounds — rounds are already capped and they are not what the user cares
about.

- Each run gets a ceiling: the config's `max_run_cost_usd`, default unset. (Written flat rather than nested: a one-field object is clutter in a file people are meant to open and read.)
- When a run would exceed it, the remaining tasks are skipped and marked over-budget, and synthesis
  reports it. It is a stop, not a refusal: the work already done is still delivered.
- With no ceiling set, the run prints its accumulated cost as it goes. Making the number visible is
  most of the value; a default ceiling someone did not choose would be a surprise the first time it
  truncated real work.

The existing `Recorder` already sees every call's cost, so this is accounting, not new plumbing.

### 5. Concurrency (later, and deliberately)

When it lands: **three at a time**, matching Hermes's `max_concurrent_children=3` — a number chosen
because it is small enough that the output of three agents can still be read, and because rate
limits, not CPU, are the binding constraint.

Three things must be true before it turns on, and none of them is true today:

1. **Failures are survivable** (§3). Parallelism multiplies partial failure; a run that aborts on the
   first error would abort on three times as many errors.
2. **Output is per-task, not interleaved.** Three subagents streaming into one terminal is
   unreadable. The decided form is a per-task log with a live one-line status per task, not
   streaming panes: panes need a layout engine and a terminal that cooperates, and the value is in
   knowing what is running, not in watching tokens arrive.
3. **Permissions do not deadlock.** Already solved in E13.4: a subagent never prompts, and anything
   its tier would ask about is refused with the tiers that would allow it. This is the Hermes
   auto-deny-in-children precedent and it is the reason parallelism is now merely hard rather than
   impossible.

**Spawn depth stays 1.** A subagent cannot orchestrate. Recursive delegation is where these systems
become impossible to reason about or bound, and nothing anyone has asked for needs it.

### 6. File isolation: not by default

A git worktree per subagent is the correct answer for parallel *edits* and the wrong default for
everything else. It costs a working copy per agent, it fails outside a repository, and it needs a
merge step that is itself a source of conflicts and wrong answers.

Decided: **sequential subagents share the working tree** (today's behaviour, and correct — later
tasks are supposed to see earlier edits). When concurrency lands, parallel tasks that write are
either serialised or given worktrees, chosen per run and off by default. Checkpoints already give
`/undo` for the shared-tree case.

### 7. Hermes ideas: explicitly in and out

| Idea | Decision |
|---|---|
| `max_concurrent_children=3` | in, when concurrency lands (§5) |
| `max_spawn_depth=1` | in, now — subagents cannot delegate |
| Summary-only return | in, already how it works |
| Children get parent toolset minus delegation | in, by construction |
| Worktree isolation | in, opt-in, later (§6) |
| `max_iterations=50` | out — `MaxRoundsFor` already bounds rounds by mode and effort, and 50 is far beyond what a bounded task needs |
| Skills / tool registries | out of item 14 — that is item 16 |
| Long-term memory | out of item 14 — item 12 |
| Messaging gateways, scheduled tasks | out of scope for kolk entirely |
| Mixture-of-Agents as `ultra` | deferred: it is a quality claim, and making it without a way to measure it here would be marketing. Revisit once the dashboard (item 17) can compare two presets on the same prompt. |

## Build leaves

- **F14.1 tasks carry structure** — planner returns kind and dependencies; briefings carry only the
  results a task declared it needs; a planner that omits them degrades to today's behaviour.
- **F14.2 a run survives its failures** — failed, incomplete and blocked tasks are reported rather
  than aborting the run; synthesis states what did not work.
- **F14.3 routing** — kinds resolve to slots, slots to models, printed with the plan and overridable.
- **F14.4 cost is visible and capped** — accumulated cost during the run, optional ceiling that
  stops rather than refuses.
- **F14.5 concurrency** — three at a time over the dependency graph, per-task status lines.
- **F14.6 the orchestrator slot reaches the orchestrator** — the planner and synthesis take it when
  set, closing the gap where the slot only affected `design` tasks.

## Open questions

- ~~**Should the planner itself be routed to the `orchestrator` slot by default?**~~ **Resolved in
  F14.6**: the planner and the synthesis are the orchestrator's own calls and take the slot when it
  is set — a slot named `orchestrator` that reached only tasks the planner happened to label
  "design" would mean something other than what it says. There is still no *default*: unset, they
  run on the session model. Paying for a stronger planner is cheap and probably right, but that is a
  judgement for the person paying, not one to make on their behalf the first time they open the
  config.
- **Does a failed task get retried on a stronger model?** Escalation-on-failure is attractive and is
  also how a cheap run silently becomes an expensive one. Deferred until F14.4 makes the cost of a
  run visible enough to judge that trade honestly.
