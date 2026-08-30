# Stigi — agentic orchestration over vendor subscriptions

*Stigi* is Old Norse for a path or a ladder. That is what this is: the model you selected is the top
rung, and an orchestrated run may climb **down** it, never up.

This file is the working checkpoint list. [`PLAN.md`](PLAN.md) owns product decisions and
[`CHECKPOINTS.md`](CHECKPOINTS.md) owns the wider build order; this owns one feature, in small steps,
one at a time. Tick a task only when it is done **and** its gates are green.

Status: `[ ]` queued · `[~]` in progress · `[x]` done · `[!]` blocked

---

## What we are building

A kolk session runs on model **X**. In agent mode X plans a big task and, per subtask, says how much
capability it needs. Code binds that to a model **Y**, and each Y runs as an independent background
vendor process on one part of the whole.

**X never names a model.** It states a level — `trivial | routine | hard` — and code binds the level
to an index into a ladder *whose element 0 is the model you selected*. A model above your selection
is therefore **unrepresentable**, not clamped after the fact. That is the whole design in one
sentence, and it is why the planner prompt must never contain a model name.

## The two rules that bind everything

1. **Your selection is a ceiling.** Routing goes down the ladder freely — that is the point, running
   a commit or an mkdir on the cheapest rung is how a subscription lasts the day — and never up.
   Enforced in code as a filter, never as a request in a prompt: a prompt is something a model may
   reasonably decide against, and a spending limit has to be a guarantee.

2. **Never increase spend without the user choosing it.** Crossing to another vendor is allowed
   **only when that vendor is signed in through kolk**. Claude → GPT is free at the margin if you
   hold both subscriptions; the same hop with no connector lands on a metered API and bills you,
   which is rule 1 violated sideways instead of upward.

## Verified ground truth

Checked in this tree. Re-verify anything you build on; do not re-litigate.

| Fact | Where |
|---|---|
| `planModelCatalog` has **no** `claude-haiku` and no `claude-fable` row — only `claude-sonnet` (Pro) and `claude-opus` (Max) | `internal/provider/plan_models.go` |
| `planBackendFor` returns `(nil, PlanModel{}, nil)` on `ErrNotAPlanModel` — nil backend, **nil error** | `internal/cli/run.go` |
| `claudeModelAliases` **does** carry all four rungs: sonnet, opus, haiku, fable | `internal/provider/agentcli/claude.go` |
| `vendorLadders` rungs are bare **match prefixes**, not spawnable model ids | `internal/engine/ceiling.go` |
| `claudeCodeTools` omits `Task`, so a vendor child cannot schedule vendor subagents | `internal/provider/agentcli/claude.go` |
| `claudeModeFlags("agent")` errors outright; `/mode agent` is refused on a plan backend | `agentcli/claude.go`, `internal/cli/slash.go` |
| `finished := make(chan taskRun)` is unbuffered while `runTasks` returns from inside the loop | `internal/engine/orchestrator.go` |
| `moveToMetered` writes `a.Backend` / `a.Model` / `a.Sess` unlocked, from a subagent goroutine | `internal/engine/subscription_limit.go` |
| `slot.*`, `max_run_cost_usd`, `max_concurrent_tasks` are printed by `kolk config` and rejected by `kolk config set` | `config/settings.go` vs `cli/cmd_config.go` |
| L4 (engine) may not import L5 (adapters) — cross-layer capability arrives as a port on `Options` | `internal/arch/layers.go` |

**Consequence, load-bearing throughout:** a plan subagent is one supervised vendor call, not a kolk
tool loop. `runSubagent`'s round loop runs once; `MaxRoundsFor`, `doomLoop` and
`executeSubagentTool` are inert on this path. `--safe-mode --permission-mode acceptEdits` is the jail.

---

## C1 — The two safety valves become settable

**Observable:** `kolk config set max_run_cost_usd 2.50`, `kolk config set max_concurrent_tasks 2` and
`kolk config set slot.fast claude-haiku` all succeed, and `get` / `unset` round-trip them.

**Why first:** everything after this leans on the cost ceiling and the concurrency limit as the only
brakes on a fan-out run, and today neither can be set from the command line.

**Files:** `internal/cli/cmd_config.go`

- [ ] **C1.1** Read the three switches (`get`, `set`, `unset`) and note where the `base_url` case sits in each — the new cases go beside it, in the same order in all three.
- [ ] **C1.2** `set max_run_cost_usd`: `strconv.ParseFloat`, refuse negatives with a message naming the value, write `cfg.MaxRunCostUSD`.
- [ ] **C1.3** `set max_concurrent_tasks`: `strconv.Atoi`, refuse anything below 1 (zero would mean a run that never starts a task), write `cfg.MaxConcurrentTasks`.
- [ ] **C1.4** `set slot.<name>`: validate through `engine.ValidateSlots` so `slot.explorer` is refused **at the point of typing** with the existing four-name message, then write into `cfg.Slots`, creating the map when nil.
- [ ] **C1.5** `get` for all three, matching how the existing keys print.
- [ ] **C1.6** `unset` for all three; deleting the last slot must nil the map so it stays out of the JSON entirely.
- [ ] **C1.7** Tests, then gates.

**Tests** — `internal/cli/cmd_config_test.go`
- `TestARunCostCeilingCanBeSetAndReadBack`
- `TestAConcurrencyLimitBelowOneIsRefused`
- `TestSettingAnUnknownSlotNamesTheFourThatExist`
- `TestUnsettingTheLastSlotLeavesNoSlotsKey`

---

## C2 — The session model and backend stop being racy

**Observable:** `go test -race ./internal/engine/...` is clean when a metered switch fires while
subagents are running. No behaviour change otherwise.

**Why:** `Model` and `Backend` are written from three places and read from twelve, and the write
happens on a subagent goroutine. This is a real race today, before any of this feature exists.

**Files:** `internal/engine/agent.go`, `subscription_limit.go`, `retry.go`, `route.go`, `fastlane.go`, `compact.go`, `internal/cli/run.go`

- [ ] **C2.1** Add `modelMu sync.RWMutex` to `Agent`, documented as guarding *only* the two `Options` fields that change mid-session.
- [ ] **C2.2** Add `sessionModel()` and `sessionBackend()` readers under `RLock`.
- [ ] **C2.3** Add exported `SetSessionModel` / `SetSessionBackend` writers under `Lock` — exported because `cli/run.go`'s `switchModel` is outside the package and must stop writing the fields directly.
- [ ] **C2.4** Replace every read of `a.Model` (`route.go`, `agent.go` ×2, `fastlane.go` ×2, `compact.go`) and of `a.Backend` (`route_backend.go` ×2).
- [ ] **C2.5** Route `moveToMetered`, the free-rotation branch and `cli/run.go`'s three writes through the setters. Construction stays a direct field write — no goroutine exists yet.
- [ ] **C2.6** Grep for stragglers: `grep -rn '\ba\.Model\b\|\ba\.Backend\b' internal/engine/` should show only the setters, the readers and construction.
- [ ] **C2.7** Tests under `-race`, then gates.

**Tests** — `internal/engine/subscription_limit_test.go`
- `TestAMeteredSwitchDuringAWideRunIsRaceFree` (N goroutines in `underCeiling`, one in `moveToMetered`)
- `TestSwitchingModelsFromTheCliStillChangesWhatTheCeilingHoldsTo`

---

## C3 — Cancelling a wide run leaks nothing

**Observable:** cancelling an orchestrated run with tasks in flight leaves no goroutine blocked on
`finished <- run`.

**Why:** `runTasks` returns from inside its loop on `ctx.Err()` while senders are still pending on an
unbuffered channel. Today that leaks a goroutine. After C7 it leaks a `claude` child process per
in-flight task, because the deferred `Close` never runs.

**Files:** `internal/engine/orchestrator.go`

- [ ] **C3.1** Buffer the channel: `make(chan taskRun, len(tasks))`.
- [ ] **C3.2** Confirm `runOneTask` uses the task's own `ctx` and never `context.WithoutCancel` — the parent backend deliberately survives a cancelled turn; a subagent deliberately must not.
- [ ] **C3.3** Test, then gates.

**Tests** — `internal/engine/orchestrator_failure_test.go`
- `TestACancelledRunLeavesNoSubagentGoroutineBehind` (baseline `runtime.NumGoroutine()`, cancel mid-run, poll back to baseline)

---

## C4 — A subscription session can enter agent mode at all

**Observable:** `kolk --model claude-sonnet` then `/mode agent` runs an orchestrated turn end to end.
Every task still runs on Sonnet.

**Why:** without this the entire feature is unreachable on exactly the sessions the ceiling was
written for. The old justification — "the vendor schedules its own subagents" — was already broader
than the code, because `claudeCodeTools` never included `Task`.

**Files:** `internal/provider/agentcli/claude.go`, `internal/cli/slash.go`

- [ ] **C4.1** `claudeModeFlags`: `"agent"` gets the same flags as `"code"`.
- [ ] **C4.2** Rewrite the tool-set comment to the sound half only: `Task` is off because *kolk's* orchestrator schedules *kolk's* subagents, and a vendor subagent tree is one kolk's bus cannot represent. Delete the circular clause.
- [ ] **C4.3** Delete the `/mode agent` refusal branch in `slash.go`; keep the restart-on-mode-change path below it untouched.
- [ ] **C4.4** Do the same for `codexModeSandbox` **only if** its justification is equally circular — read it first and decide on the evidence.
- [ ] **C4.5** Pin the tool set as a contract, not a convenience: a test that fails the day a vendor default changes.
- [ ] **C4.6** Tests, then gates.

**Tests** — `internal/provider/agentcli/argv_test.go`, `internal/cli/slash_test.go`
- `TestAgentModeRunsTheVendorWithTheSameToolsAsCodeMode`
- `TestTheVendorNeverGetsItsOwnSubagentScheduler` (`Task` absent in **every** mode)
- `TestASubscriptionSessionCanSwitchIntoAgentMode`

---

## C5 — The planner states a level and the plan shows it

**Observable:** a plan line reads `3. Wire the config key  [edit · routine]`. Nothing routes
differently yet.

**Files:** `internal/engine/level.go` (new), `task.go`, `orchestrator.go`

- [ ] **C5.1** `level.go`: `Level` type with `LevelUnstated` (`""`), `trivial`, `routine`, `hard`, and `knownLevels`. Document why unstated binds to the ceiling: a task whose difficulty could not be read is not one to hand to the cheapest thing available.
- [ ] **C5.2** `Task.Level` and `planTask.Level` with its JSON tag.
- [ ] **C5.3** `parseTasks` filters through `knownLevels` exactly as it already filters `Kind` — `"medium"`, `"VERY HARD"` and a model name stuffed in the field all become `LevelUnstated`.
- [ ] **C5.4** `Task.annotation()` grows to `kind · level · model`, each omitted when empty, so a planner that never emits levels shows as blanks rather than as a quietly expensive run.
- [ ] **C5.5** Planner prompt gains the level field and **zero model names**; update the example object.
- [ ] **C5.6** Tests, then gates.

**Tests** — `internal/engine/level_test.go`, `task_test.go`
- `TestAPlannerThatStatesALevelHasItRecorded`
- `TestAnInventedLevelIsNotGuessed`
- `TestAPlainStringListStillWorks` (existing — must keep passing)
- `TestThePlannerPromptNamesNoModel` (no `vendorLadders` rung string appears in the rendered prompt)
- `TestAPlanLineShowsKindLevelAndModel`

---

## C6 — The roster: what this session may spend on

**Observable:** agent mode prints the lane above the plan —
`lane: trivial → claude-haiku · routine → claude-sonnet · hard → claude-sonnet`. Nothing binds to it
yet. The lane is visible *before* it can cost anything, which is why this is split from C8.

**Files:** `internal/engine/ceiling.go`, `roster.go` (new), `agent.go`, `internal/cli/subagent_backend.go` (new)

- [ ] **C6.1** Export `LadderRungIDs(vendor)` returning canonical `vendor/rung` ids, strongest first. These are already rankable: `modelRank` folds `claude/haiku` to `claude-haiku`.
- [ ] **C6.2** `Rung{ID, Model, Vendor, Depth}` where `Depth 0` **is** the ceiling.
- [ ] **C6.3** `Rungs[0].Model` is `a.sessionModel()` **verbatim** — never a ladder string. This is judge defect B: ladder rungs are match prefixes, and using one as a model id would hand `claude-opus` to OpenRouter as a literal name.
- [ ] **C6.4** Rungs below depth 0 exist only when the port answers for them, and the port answers only for connectors that are signed in — rule 2.
- [ ] **C6.5** Print the lane on entering agent mode; replace the interim "capped at …" line from FR4.1 with this, which says the same thing and more.
- [ ] **C6.6** Tests, then gates.

**Open question to settle inside this checkpoint:** cross-vendor. The roster is currently one
vendor's ladder. Rule 2 says another vendor's rungs may join it when that vendor is signed in.
Decide the shape here, with cost and this machine's own ratings as the comparison — `stats.RatingsByModel`
already exists and already knows that "never rated" and "rated badly" are different facts.

---

## C7 — Each subagent gets its own vendor process

**Observable:** two concurrent subagents no longer serialise on one `ClaudeSession.mu`, and a
subagent whose child dies no longer retires the parent's conversation. Every task still runs on the
ceiling model.

**Files:** `internal/engine/subagent_backend.go` (new), `retry.go`, `orchestrator.go`, `internal/cli/subagent_backend.go`, `run.go`

- [ ] **C7.1** Port on `Options`: open a backend for one task. Nil means today's behaviour — share `a.Backend`.
- [ ] **C7.2** Implement in `internal/cli`, constructing the adapter **directly from the connector manifest**, never through `ResolvePlanModel` — judge defect A: the catalogue has no haiku row, so that path returns a nil backend with a nil error.
- [ ] **C7.3** Own it in `runOneTask`, which already owns the per-task lifetime; close on **every** path out.
- [ ] **C7.4** Teardown needs a type assertion — `ChatBackend` declares only `StreamChat`; every existing teardown goes through `io.Closer`.
- [ ] **C7.5** The graft: use the supplied backend **only while `model` still equals what it was opened for**. `streamChat` mutates `model` in-loop on free rotation and on the metered switch; after either, fall back to `backendFor` — which also preserves owned-prefix stripping.
- [ ] **C7.6** Tests, then gates.

**Tests** — `internal/engine/subagent_backend_test.go`, `internal/cli/subagent_backend_test.go`
- `TestEachSubagentTalksToItsOwnProvider`
- `TestASubagentBackendIsClosedOnEveryPathOutOfATask`
- `TestOneSubagentDyingDoesNotRetireTheParentConversation`
- `TestASubagentIsAlwaysOpenedInCodeMode`
- `TestAMeteredSwitchMidTaskStopsUsingTheSubagentProcess`
- `TestAnOwnedPrefixStillReachesItsOwnServerFromASubagent`
- `TestOpeningACheaperRungDoesNotGoThroughThePlanCatalogue`
- `TestEverySpawnableRungIsAModelItsAdapterAccepts` — the load-bearing direction, and the test that fails the day someone edits `vendorLadders` without editing `claudeModelAliases`

---

## C8 — Levels bind to rungs · **usable end to end here**

**Observable:** on `kolk --model claude-sonnet` in agent mode, a task the planner called `trivial`
runs on Haiku in its own process; `routine` and `hard` run on Sonnet; the plan line and the run cost
show it.

**Files:** `internal/engine/route.go`, `orchestrator.go`

- [ ] **C8.1** Bind level → rung depth, all of it on the **scheduler goroutine** (`nextRunnable`), never from a subagent goroutine — the graft that avoids a fresh race on `tasks[i].Model`.
- [ ] **C8.2** `LevelUnstated` binds to depth 0.
- [ ] **C8.3** A configured slot still beats a level; the ceiling still beats the slot.
- [ ] **C8.4** Tests, then gates.

**Tests**
- `TestATrivialTaskRunsOnTheCheapestRungTheUserAllows`
- `TestAHardTaskNeverRunsAboveTheModelTheUserChose`
- `TestAnInventedLevelRunsOnTheModelTheUserChose`
- `TestAConfiguredSlotStillBeatsALevel`
- `TestAGatewaySessionRoutesExactlyAsItDidBefore`
- `TestARunReportsWhatEachRungCost`

---

## C9 — The run survives a rung that will not open, and stops when the plan does

**Observable:** a subagent whose cheaper rung fails to spawn retries once on the ceiling and says so;
under `on_subscription_limit stop`, the rest of the run resolves in place instead of failing N times
identically.

**Files:** `internal/engine/orchestrator.go`, `subscription_limit.go`

- [ ] **C9.1** One retry on the ceiling when a cheaper rung will not start.
- [ ] **C9.2** Announce it — a silent downgrade to a more expensive model is the surprise this feature exists to prevent.
- [ ] **C9.3** A second failure fails the task rather than climbing further.
- [ ] **C9.4** Eight subagents hitting the plan limit ask **once**, not eight times.
- [ ] **C9.5** Tests, then gates.

**Tests**
- `TestARungThatWillNotStartFallsBackToTheModelTheUserChose`
- `TestTheFallbackToTheCeilingIsAnnouncedNotSilent`
- `TestASecondFailureOnTheCeilingFailsTheTask`
- `TestEightSubagentsHittingTheLimitAskOnce`
- `TestStoppingAtTheLimitResolvesTheRestInsteadOfRunningThem`
- `TestATaskAlreadyRunningWhenTheLimitLandsDoesNotSpendOnAStaleRung`

---

## C10 — A reader can tell which rung did what

**Observable:** `subagent.started` and `subagent.finished` carry `level` and `model`, so a dashboard
can say "4 subagents, 3 on haiku".

**Files:** `protocol/events.go`, `spec/schemas/events/subagent.*.json`, `spec/CHANGELOG.md`, `internal/engine/subagent_events.go`

- [ ] **C10.1** Add the fields to the Go structs.
- [ ] **C10.2** Add them to the schemas as **optional** — an additive change, so an old event still validates.
- [ ] **C10.3** Record it in `spec/CHANGELOG.md`; the spec-change gate will ask.
- [ ] **C10.4** Tests, then gates.

**Tests**
- `TestASubagentEventSaysWhichRungRanIt`
- `TestAnEventWithoutALevelStillValidates`
- `TestTheSchemaAndTheGoStructAgreeOnTheNewFields`

---

## Gates for every checkpoint

Run before ticking. `make check` does **not** run the race detector, so a concurrency bug passes
every gate — that is how a concurrent map write reached `main` once already.

```
make check
go test ./... -race            # mandatory for C2, C3, C7, C8
```

## Deliberately out of scope

**`SlotFast` → `FastLaneModel` → `google/gemini-2.5-flash` on a plan session.** A one-line fix once a
roster exists, and tempting for exactly that reason — but it is a different bug on a different path.
Titling, commit drafts, compaction summaries and the saga planner all ride the fast lane, and folding
it into an orchestration feature would put a regression on the most-used auxiliary path in the
product. Separate change, recorded here so it is not lost. It is also why C7's subagent path
deliberately does not touch `fastLaneCall`.

**`ClaudeBackend.getSession` ignoring the per-call model.** Not fixed; made unreachable on the
subagent path, because each subagent's backend *is* its rung. Still live on the fast lane, above.

**`CodexBackend.b.thread` written from an event callback.** Not fixed; made unreachable — one backend
and one goroutine per subagent.
