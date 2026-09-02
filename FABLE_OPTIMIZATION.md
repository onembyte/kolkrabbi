# Fable optimization — making the harness earn Claude Fable

*Fable* is the top rung of the Claude ladder (`internal/engine/ceiling.go`): the vendor's own picker
calls it "most capable for your hardest and longest-running tasks". Kolkrabbi is the harness that
drives that model — through the `claude` CLI on a Max plan, or through a gateway — for chat, code,
agent fan-out, and SAGA wakes. This file is the plan for making that harness worth the model: first
correct, then truthful about what it runs, then cheap in the places that repeat on every turn.

This is a working checkpoint list in the style of [`STIGI.md`](STIGI.md). [`PLAN.md`](PLAN.md) owns
product decisions, [`CHECKPOINTS.md`](CHECKPOINTS.md) owns the V34 build order, and this file owns
one program in small steps, one at a time. Tick a task only when it is done **and** its gates are
green. It is deliberately not `docs/plan/35-*.md`: `make plan-check` requires every numbered doc to
have a PLAN.md item, and this program is a queue of engineering leaves, not a new product decision.

Status: `[ ]` queued · `[~]` in progress · `[x]` done · `[!]` blocked

Written 2026-09-02 from a review of the uncommitted tree on `main` after `v1.2.32` (`5074e620`).

---

## Why this program exists

Two things are true at once on 2026-09-02:

1. **The tree is red.** `go build ./...` and `go vet ./...` are clean, but `go test ./...` fails in
   `internal/cli` and `internal/engine` — roughly forty tests, all with the same error,
   `secret: credential origin is not allowed`. This is the *expected* mid-state of V34.1a
   (V34.1a.1 landed the origin-bound transport; V34.1a.2 endpoint-first construction is next), but a
   red tree cannot prove anything else, so it comes first.
2. **A multi-angle code review of the same diff found fourteen further defects**, four of which make
   the inline SAGA unusable after its first wake and three of which make the delegated-execution
   policy diverge from what `docs/plan/13` in the same diff promises. All are recorded in the
   findings ledger below with the phase that owns each.

On top of the fixes, the program adds what a Fable user on a Max plan is missing today: a catalog
row for the model they selected, subagent routing down the *plan's own* ladder instead of gateway
ids, and per-turn work that stops being repeated.

## The rules that bind everything

1. **The selection is a ceiling.** Selecting Fable makes {Fable, Opus, Sonnet, Haiku} reachable;
   selecting Sonnet makes {Sonnet, Haiku} the whole set. Enforced in code as a filter (`underCeiling`),
   never as a prompt. Nothing in this program may add a branch that skips the ceiling.
2. **Never increase spend without the user choosing it.** Downward routing on the same subscription is
   free at the margin; a hop to a metered gateway is not, and needs a visible decision.
3. **Security claims are enforced at the boundary with a negative test.** A "network disabled"
   status line that the child can contradict is a defect, not a display issue.
4. **One leaf, one owner, one checkpoint dossier** (Intent → Red → Green → Adversarial → Independent
   review → `make check` → Walk-back), per `docs/plan/34-vision-completion.md`. V34.1a is owned by
   Codex right now (`AGENTS.md`); F0 below coordinates with that owner rather than duplicating it.
5. **Stdlib only; the engine touches no OS** (`make purity`, `make arch`). Nothing here adds a
   dependency or crosses L4 → L5.

## Verified ground truth

Checked in this tree on 2026-09-02. Re-verify anything you build on; do not re-litigate.

| Fact | Where |
|---|---|
| `provider.NewClient(key)` binds the credential to `https://openrouter.ai` inside the constructor; `run.go` and `cmd_models.go:29` then overwrite `client.BaseURL` | `internal/provider/client.go`, `internal/cli/run.go:216`, `internal/cli/cmd_models.go:29` |
| `Client.SetKey` returns `ErrCredentialBinding` on an unbound client; `AuthTransport` token is mutex-guarded | `internal/secret/transport.go`, `internal/provider/client.go` |
| `planModelCatalog` has **no** `claude-fable` and no `claude-haiku` row — only `claude-sonnet` (Pro) and `claude-opus` (Max) | `internal/provider/plan_models.go` |
| `claudeModelAliases` carries all four rungs including `"claude-fable": "fable"` | `internal/provider/agentcli/claude.go:152` |
| `claudeEfforts` accepts `low, medium, high, xhigh, max`; kolk folds `xhigh` → `EffortMax` | `agentcli/claude.go:132`, `internal/engine/agent.go:80` |
| `vendorLadders` puts `claude-fable` at rung 0; `underCeiling("claude-fable")` clamps to Opus on an Opus session | `internal/engine/ceiling.go:35`, `model_race_test.go:123` |
| On a plan session the orchestrator slots still resolve **gateway** ids; routing a subagent to the plan's own cheap model "is the other half and is not built" | `docs/build-log.md` FR4.3 |
| `claudeCodeTools` deliberately omits `Task`, so a vendor child cannot schedule vendor subagents | `agentcli/claude.go:105` |
| Every subagent is launched with `NetworkAccess: true`, unconditionally | `internal/cli/run.go:376` |
| The Codex one-shot path now runs through `RunLinesWithOptions` with `cmd.Env = inheritedEnv(nil)` | `internal/shell/lines.go:100` |
| Gates: `make check` = fmt/vet/test/arch/purity/buildtags/platforms/lint/budgets/site/surface/installer/spec/release/plan/workflow-pins | `Makefile:97` |

---

## Findings ledger (code review, 2026-09-02)

Fourteen verified findings from a multi-angle review (line-by-line diff, removed-behaviour audit,
cross-file trace, Go pitfalls, wrapper/proxy correctness, simplification, efficiency, altitude) plus
the failing-gate observation. Every row is owned by exactly one phase below.

| ID | Sev | Where | Finding (short) | Phase |
|---|---|---|---|---|
| R1 | P1 | `internal/cli/run.go:216`, `cmd_models.go:29` | Credential bound to the default origin inside `NewClient`, then `BaseURL` overwritten → every custom `--base-url` / Ollama / LiteLLM / vLLM request refused; ~40 tests red | F0 |
| R2 | P1 | `internal/cli/cmd_saga_run.go:36` | Pre-`RunWake` guard returns "nothing left to work" once planned chapters are done, so the planner is never asked for chapter 2 — inline SAGA cannot advance | F1 |
| R3 | P1 | `internal/cli/cmd_saga.go:116` | `saveSagaGoal` overwrites the persisted Goal with every wake's prompt (`next chapter /saga` becomes the goal) | F1 |
| R4 | P1 | `internal/cli/cmd_saga.go:117` | A completed/blocked `SAGA.md` is reused verbatim by a new `/saga <goal>`; no reset path exists since `saga stop/rewind/status` were deleted | F1 |
| R5 | P1 | `internal/cli/run.go:376` | `NetworkAccess: true` hard-coded for every subagent → Codex `sandbox_workspace_write.network_access=true` for all task kinds; contradicts `docs/plan/13` | F2 |
| R6 | P1 | `internal/shell/lines.go:100` | One-shot path now scrubs `*_API_KEY/*_TOKEN/*_SECRET`; a Codex user authenticated via `OPENAI_API_KEY` regresses to "not logged in" | F2 |
| R7 | P1 | `internal/provider/agentcli/codex.go:174` | Network-disabled is expressed by *omitting* the flag, so Codex falls open to `~/.codex/config.toml` while status says `network=disabled` | F2 |
| R8 | P2 | `internal/cli/cmd_saga_run.go:55` | Wake budget omits `DoomThreshold` → `Budget.Check` blocks at the default 3 strikes regardless of `MaxStrikes` in `SAGA.md` | F1 |
| R9 | P2 | `internal/engine/chapter_verify.go:50` | `sagaCancellation` inside Verify discards the real commit/rollback error on Ctrl+C; `sagaCancellationResult` exists one level up for exactly this | F1 |
| R10 | P3 | `internal/shell/process_options.go:21` | abs→EvalSymlinks→Stat→IsDir hand-copied ×3 (`shell`, `agentcli`, `cli.verifiedProjectRoot`) | F5 |
| R11 | P3 | `internal/cli/repl.go:59` | Both REPLs re-detect the inline marker and duplicate the interrupt/error block; `runInteractivePrompt` already routes | F5 |
| R12 | P3 | `internal/engine/saga_executor.go:138` | `RunWake` is a copy of `Run`'s loop body and already diverges (strike counting, `finishStop`); compares `"blocked"` literal | F5 |
| R13 | P3 | `internal/provider/agentcli/codex.go:146` | Directory validation repeated per turn (constructor, `Build*Invocation`, `RunLinesWithOptions`) — 2×(1+dirs) syscalls per Codex turn; Claude builds an unused invocation per turn | F4 |
| R14 | P3 | `internal/cli/flags.go:17` | `posture` option and `Options.Posture` pass-through are dead; `ExecutionOptions.Provider` exists only to make the struct non-empty | F5 |
| R15 | P3 | `internal/engine/subagent_backend.go:36` | Two factory signatures; legacy `SubagentBackend` silently skips confinement/network declaration; Claude-only network rule bolted onto the shared normalizer | F5 |

---

## F0 — A green tree, on the endpoint-first contract  ·  **done 2026-09-02**

**Observable:** `go test ./... -count=1` and `go test -race ./internal/cli/... ./internal/engine/...`
pass; `kolk --base-url http://localhost:4000/v1` completes a keyless turn against a compatible
endpoint; `kolk` with a stored key completes an authenticated turn against `openrouter.ai` only.

**Why first:** nothing below can be proven on a red tree, and R1 is the same defect V34.1a.2 exists to
fix. This phase is **owned by Codex** under V34.1a (`AGENTS.md`, 2026-09-02). Do not implement it in
parallel; supply the acceptance list, review the diff, and rerun the gates.

**Files:** `internal/cli/run.go`, `internal/cli/cmd_models.go`, `internal/provider/client.go`, the
~40 fixtures that pattern `NewClient(key)` + `BaseURL = httptest`.

- [x] **F0.1** Resolve the endpoint (flag → env → saved config → default) **before** the key
  requirement. An authenticated client is constructed only when the resolved origin is canonical
  OpenRouter; every other origin gets a keyless compatible client, and the debug/help line says so.
  *(Codex, commit `a5b2b792`, `internal/cli/provider_client.go`.)*
- [x] **F0.2** Replace the `NewClient(key); client.BaseURL = …` pattern in `run.go:216` and
  `cmd_models.go:29` with the endpoint-first constructor. `provider.NewClient` was removed outright.
  *(Codex, `a5b2b792`.)*
- [x] **F0.3** Rewrite the red fixtures truthfully: tests that exercise authentication bind a test-only
  origin; tests that do not are explicitly keyless. `go test ./... -count=1` green on `a5b2b792`.
  *(Codex, `a5b2b792`.)*
- [x] **F0.4** V34.1a.3 adversarial matrix — 18 replacement shapes × {catalog, turn, verifier}, 7
  canonical spellings, cancellation, host/compatible routes, startup keyed/keyless matrix over a
  corrupt manifest; one substring-keyed request-shape divergence fixed. Dossier in `CHECKPOINTS.md`
  §V34.1a.3. *(2026-09-02.)*
- [x] **F0.5** Walk-back: `--base-url` help, `base_url` setting description, `kolk config
  set-base-url` output, README, `SECURITY.md`, capabilities page, and `docs/plan/34` say a custom
  endpoint is keyless. Eight guard mutations each caught by a focused test with byte-identical
  restore. Independent reviewer broke the binding once (U+0130 `İ` case-folds to ASCII `i`; net/http
  dials `openrouter.xn--ai-sub`), the guard now refuses non-ASCII hosts, re-review CLEAN after a
  7,054-candidate reverse scan. `make check` green at 3,190 tests. Dossier: `CHECKPOINTS.md`
  §V34.1a.4.

**Exit met 2026-09-02.** Carried forward to V34.1d: a compatible endpoint's own URL userinfo is sent as
Basic auth by net/http and echoed in unscrubbed transport errors (`client.go` `StreamChat`/`listModels`
return paths).

**Exit:** `go test ./... -count=1` green, `-race` clean on cli/engine, and an independent reviewer
has tried to move the credential to a replacement host and failed.

---

## F1 — The inline SAGA advances, remembers its goal, and can be reset  ·  **done 2026-09-02**

**Observable:** in an isolated repository, `build X /saga` plans and finishes chapter 1;
`continue /saga` plans and finishes chapter 2 with the goal still `build X`; after a saga completes,
`add Y /saga` starts a fresh saga instead of reporting the old one complete; Ctrl+C during a commit
reports the git error, not a bare `(interrupted)`.

**Why:** R2–R4 together mean a Fable-driven saga does exactly one chapter and then lies about the rest.
This is the "hardest and longest-running tasks" path — the one the top rung is for. Feeds V34.3a/b/f.

**Files:** `internal/cli/cmd_saga.go`, `internal/cli/cmd_saga_run.go`, `internal/cli/saga_marker.go`,
`internal/cli/saga_prompt.go`, `internal/engine/saga_executor.go`, `internal/engine/chapter_verify.go`,
`internal/engine/saga_budget.go`, `internal/engine/saga_lifecycle.go`.

- [x] **F1.1 (R2)** The pre-`RunWake` "nothing left to work" guard and `hasPendingChapter` are
  deleted; terminal status is the executor's to judge from the artifact's own `Status` line.
  `TestASecondWakeAsksThePlannerWhenEveryChapterIsDone` drives a REPL over a scripted provider:
  chapter 1 done, `continue /saga`, exactly one model request (the planner), verdict reported.
- [x] **F1.2 (R3)** `saveSagaGoal` became `openSaga(text) (sagaOpening, error)`: an in-flight saga
  keeps its goal and the text becomes a **note** (`AgentPlanner.Note`, `AgentWorker.Note`) shown in
  both prompts; restating the goal is not a note. `TestAWakeNoteDoesNotReplaceTheGoal`.
- [x] **F1.3 (R4)** Reset rule chosen and written into `docs/plan/10` §4: a finished artifact is
  archived as `SAGA.<started YYYYMMDD-HHMMSS>.md` (counter suffix on collision) and the text starts a
  new saga; the completion and doom-loop messages say so. No subcommand.
  `TestANewGoalAfterAFinishedSagaArchivesAndStartsFresh` (completed and blocked),
  `TestArchivingTwiceInTheSameSecondKeepsBoth`.
- [x] **F1.4 (R8)** `sagaWakeBudget(state)` carries `MaxChapters`, `CostLimit`, and
  `DoomThreshold: state.MaxStrikes`. `TestWakeBudgetCarriesMaxStrikesFromSagaFile`: 3/5 continues,
  5/5 dooms.
- [x] **F1.5 (R9)** `ChapterVerifier.Verify` and `VerifyChapter` use `sagaCancellationResult` on
  every real error path; `TestACancelledCommitKeepsTheGitError` proves the hook failure survives
  `context.Canceled` and the chapter is left `executing` with no strike.
- [x] **F1.6** One mutation per fix (guard reinserted, goal overwritten, terminal reused, threshold
  dropped, plain cancellation) — each focused test fails, each file restored byte-identically.
  Existing adversarial coverage retained: executing persisted before work
  (`TestAPlannedChapterPersistsExecutingBeforeWorkerStarts`), cancellation during work and
  verification without a strike, terminal artifacts not reopened, persistence failure surfaced.
  `-race` clean on `cli` and `engine`. Hand-edited/garbage `SAGA.md` still fails at parse before any
  turn. Not added here: a crash-injection harness (V34.3e owns it).
- [x] **F1.7** `docs/plan/10` §3.1 and §4 carry the wake table and the reset rule; the stop
  messages name the next step; dossier in `CHECKPOINTS.md` §F1. Also folded in from F5.3:
  `RunWake` compares `SagaStatusBlocked`, not a literal.

**Exit met 2026-09-02.** Carried to V34.3e: crash injection between persist and work.

---

## F2 — Delegated execution says what it does, and does what it says  ·  **done 2026-09-02**

**Observable:** the status line and the briefing are rendered from the same per-task envelope the
child is opened with; `network=disabled` on Codex means `-c sandbox_workspace_write.network_access=false`
on the child's argv regardless of `~/.codex/config.toml`; an `edit`/`test`/`explain`/unlabelled task
runs without network by default while a `research` task has it; a Claude child is declared
`network=enabled` because the vendor has no switch, and under the strict policy is refused rather than
quietly given it; `kolk config set subagent_network auto|on|off` round-trips; a sentinel secret in the
parent — including the vendor's own API key, `AWS_SECRET_ACCESS_KEY`, `GITHUB_PAT`, and
`OPENAI_API_KEY_BACKUP` — is provably absent from both child paths while `GOFLAGS` survives.

**Why:** R5–R7 were policy-vs-code divergences in the boundary Fable subagents run inside. Feeds
V34.1b (child environment) and V34.1f (envelope).

**Files:** `internal/engine/subagent_backend.go`, `orchestrator.go`, `agent.go`, `internal/cli/run.go`,
`cmd_config.go`, `internal/config/config.go`, `settings.go`, `internal/provider/agentcli/codex.go`,
`internal/shell/shell.go`.

- [x] **F2.1 (R5)** The hard-coded `NetworkAccess: true` is gone. `Agent.subagentNetwork(kind, model)`
  decides per task from `Options.SubagentNetwork` (`auto` default: `research` only, plus vendors with
  no switch; `on`; `off` strict) and the ceiling ladder's vendor. `openSubagentBackend` and
  `subagentCapabilities` take the task kind. `kolk config set subagent_network` validates at the point
  of typing. `TestSubagentNetworkFollowsPolicyKindAndVendorSwitch` (12 rows),
  `TestBackgroundTaskKindsRunWithoutNetwork`, `TestSubagentNetworkPolicyRoundTripsAndRejectsUnknown`.
- [x] **F2.2 (R7)** Codex states network both ways in every delegated envelope
  (`network_access=%t`); only the bare, envelope-less session invocation leaves the vendor's config
  in charge. `TestCodexNetworkDisabledIsExplicitNotOmitted`.
- [x] **F2.3 (R6)** Decided against the allowlist: a delegated coding child runs the repository's build
  tools, which read whatever the user's shell exported, and an allowlist would have to know them all.
  The denylist stays and gains `_ACCESS_KEY`, `_PAT`, `_PASSPHRASE`, and anywhere-in-name `API_KEY`,
  `SECRET`, `PASSWORD`. The vendor's own API key stays scrubbed **on purpose**: a claude or codex
  child that received it would bill the API instead of the plan — the spend rule violated sideways.
  The `OPENAI_API_KEY`-authenticated Codex user the reviewer described is a metered-API user, and that
  is not the subscription handoff this backend is; `docs/plan/13` §7.1 says so.
  `TestChildrenNeverInheritASentinelSecretOnEitherPath` (10 sentinels × 2 paths, `GOFLAGS` kept).
- [x] **F2.4** `subagentCapabilitySummary` and the briefing are both rendered from
  `subagentCapabilities(kind, model)` — the same call the factory receives, recomputed when the
  fallback changes vendor. One source; drift is structurally impossible.
- [x] **F2.5** Adversarial: `-c` on argv overrides `~/.codex/config.toml` by vendor precedence, so a
  config-side re-enable cannot win; `OPENAI_API_KEY_BACKUP`, `MY_SECRET_2`, `DB_PASSWORD_PROD` now
  scrubbed; symlinked `AdditionalDirs` were already canonicalised by `normalizeExecutionOptions`.
  Three mutations (network always on, Codex omits `false`, denylist loses `_ACCESS_KEY`) each fail
  their focused test and restore byte-identically. `-race` clean on engine/shell/agentcli/cli.
- [x] **F2.6** Walk-back: `docs/plan/13` §7.1 (policy table, the API-key decision), `docs/plan/34`
  V34.1b marked part-done with what remains (the interactive login/PTY path), `AGENTS.md`,
  `CHECKPOINTS.md` §F2, build log. `make check` green.

**Exit met 2026-09-02.** Carried to V34.1b's remainder: the `kolk plans login` PTY/handover path is
not covered by these two child paths and still needs its own sentinel proof.

## F3 — Fable is a model the harness can actually select and route below  ·  `[ ]`

**Observable:** `kolk pmodels` lists `claude-fable` (Claude Max, efforts `low medium high xhigh max`)
and `claude-haiku`; `kolk --model claude-fable /mode agent` prints a truthful ceiling line and routes
`trivial`/`routine` subtasks to `claude-haiku`/`claude-sonnet` **on the same plan** rather than to a
gateway id; `/effort 4` reaches the vendor as `--effort max` and `xhigh` is accepted as an alias.

**Why:** the model this program is named after has no catalog row (`plan_models.go`), so a Max user
cannot select it through the plan selector, and on a plan session subagents still resolve gateway
models (FR4.3). That is the single largest cost/latency lever for a Fable user: the expensive rung
plans, the cheap rungs on the *same subscription* execute. Feeds V34.4a/b.

**Files:** `internal/provider/plan_models.go`, `internal/provider/agentcli/claude.go`,
`internal/engine/ceiling.go`, `internal/engine/route.go`, `internal/engine/task_effort.go`,
`internal/cli/cmd_models.go`, `internal/cli/repl.go` (ceiling line).

- [ ] **F3.1** Add `claude-fable` (Claude Max) and `claude-haiku` (Pro and Max) rows to
  `planModelCatalog`. Efforts per row come from a **fire-and-check** against the live CLI
  (`[claude-code:unrecognized_model]` / `KindModelNotFound` at zero quota cost) recorded in the
  dossier — no row is invented from memory. `kolk pmodels` and `kolk pmodels anthropic` print them.
- [ ] **F3.2** Effort projection: kolk `max` → claude `--effort max`; `xhigh` stays an accepted input
  alias (`agent.go:80`). Verify live that Fable accepts each of the five values and record which
  ones the vendor warns on. Red: `/effort xhigh` on a Claude plan session must not error.
- [ ] **F3.3** Plan-native downward routing: on a plan session the slot resolver ranks the **plan's
  own ladder** below the ceiling (`claude-fable → opus → sonnet → haiku`) before any gateway id, and
  never crosses to a metered gateway without the visible decision from rule 2. Red: a Fable session
  with `slot.fast` unset must route a `trivial` task to `claude-haiku`, not `google/gemini-2.5-flash`.
- [ ] **F3.4** Ceiling messaging for the top rung: entering agent mode on Fable prints what is
  reachable (`agent runs may route down to claude-haiku`) — a guarantee, not a prediction, in the
  FR4.3 sense. On Opus it keeps `claude-fable stays out of reach`.
- [ ] **F3.5** `docs/plan/24-subscription-provider-matrix.md` and `08-model-routing.md` gain the
  Fable row, the plan-native ladder, and the "why not clamp unranked" refusal restated.

**Tests** — `internal/provider/catalog_test.go`, `internal/engine/model_race_test.go`, `route_test.go`,
`internal/cli/repl_test.go`
- `TestPlanCatalogListsFableAndHaikuWithVerifiedEfforts`
- `TestMaxEffortReachesClaudeAsMax`
- `TestFableSessionRoutesTrivialWorkToHaikuOnThePlan`
- `TestTopRungCeilingLineNamesWhatIsReachable`

---

## F4 — Stop repeating work on every Fable turn  ·  `[ ]`

**Observable:** per Codex turn, directory validation runs once (constructor), not 2×(1+dirs);
the persistent Claude session path builds no unused invocation per turn; `make budgets` still passes
(startup ≈ 2 ms, ~5 MB binary); a 20-turn saga wake performs no more `EvalSymlinks`/`Stat` syscalls
than the first turn did.

**Why:** the harness's own overhead is small compared with a model call, but it is *per turn* and per
subagent, and a Fable saga is hundreds of turns. Measure first, then remove only what the measurement
shows; do not optimize past the budget gate.

**Files:** `internal/provider/agentcli/codex.go`, `claude.go`, `execution_options.go`, `session.go`,
`internal/shell/lines_process.go`, `process_options.go`.

- [ ] **F4.1** Measure: a focused benchmark (`BenchmarkCodexTurnArgv`, `BenchmarkClaudeTurnArgv`)
  and an `strace -c`-style count on Linux for one persistent Claude turn and one Codex one-shot,
  recorded in the dossier as the "before".
- [ ] **F4.2 (R13)** Normalize `ExecutionOptions` **once** in `NewCodexBackendFromHandleWithOptions` /
  the Claude constructor and pass the canonical result through; `Build*InvocationWithOptions` and
  `RunLinesWithOptions` accept already-normalized options (a marker type or a `normalized bool` that
  the constructor sets — pick the one `make arch` and the dead-export gate accept).
- [ ] **F4.3 (R13)** The persistent Claude session path stops building a full invocation per turn;
  the resume argv is composed from the retained session handle and the effort/model flags only.
- [ ] **F4.4** SAGA wake I/O: `SAGA.md` is parsed once per wake and the parsed state threaded to
  planner, budget, and executor; chapter prompts are built from the parsed state, not by re-reading.
- [ ] **F4.5** Re-measure; the "after" numbers go beside the "before". `make budgets` and `-race`.

**Tests** — `internal/provider/agentcli/execution_options_test.go`, `codex_test.go`
- `TestOptionsAreNormalizedOnceAtConstruction`
- `TestPersistentClaudeTurnBuildsNoInvocation`

---

## F5 — One implementation per rule  ·  `[ ]`

**Observable:** no behaviour change; `go test ./... -count=1` and `-race` green; the dead-export and
arch gates pass; each rule below has exactly one implementation and one test.

**Why:** the review found the same logic hand-copied three times, two REPLs duplicating the same
error block, a saga loop body that already diverged from its copy, and dead plumbing that implies a
CLI feature which does not exist. Duplicates are where the next Fable-path defect will come from.

**Files:** `internal/shell/process_options.go`, `internal/provider/agentcli/execution_options.go`,
`internal/cli/run.go`, `repl.go`, `tui_repl.go`, `flags.go`, `internal/engine/saga_executor.go`,
`subagent_backend.go`.

- [ ] **F5.1 (R10)** Export one `shell.VerifiedDir(path) (string, error)`; `agentcli.normalizeExecutionDirectory`
  and `cli.verifiedProjectRoot` call it. One error wording. Grep for `EvalSymlinks` should show one
  implementation outside tests.
- [ ] **F5.2 (R11)** `runInteractivePrompt` is the single boundary for every non-`/` line in both
  REPLs; marker detection lives only inside it; `repl.go:88`'s direct `ag.RunTurn` and the three
  copies of the interrupt/error block collapse to one.
- [ ] **F5.3 (R12)** Extract `step(ctx, repoDir, state) (StopReason, error)` from `Run`/`RunWake`;
  `RunWake` calls it once, `Run` loops it. Terminal-status guard moves into `nextChapter` so no third
  caller can reopen a completed/blocked saga. Replace the `"blocked"` literal with `SagaStatusBlocked`.
  This is where F1.1's deleted guard lands.
- [ ] **F5.4 (R14)** Delete the `posture` option and `Options.Posture` pass-through (posture is set by
  `ag.SetPosture` at wake time only), or wire a real `--posture`; drop `ExecutionOptions.Provider` or
  use it. The dead-export gate decides which.
- [ ] **F5.5 (R15)** One factory: `SubagentBackend` takes capabilities (shim at the boundary for the
  3-arg callers, removed once none remain); provider network capability becomes **data** on each
  backend's constructor/`ExecutionOptions`, so the Claude-only `validateClaudeExecutionOptions` free
  function disappears and Codex gets the same invariant by construction.
- [ ] **F5.6** `make check` and a diff review that confirms "no behaviour change" line by line.

---

## F6 — Proof and walk-back  ·  `[ ]`

**Observable:** a fresh clone at the closing commit passes `make check`; every claim in README,
`site/capabilities.html`, `docs/plan/10`, `13`, `24`, `08` matches the binary; `CHECKPOINTS.md` holds
one dossier per phase with commands and results; an independent reviewer who did not implement F1–F3
reruns the failure matrices.

- [ ] **F6.1** Fresh-clone `make check` on Linux and macOS (the V34.5a matrix), `-race` on
  `internal/cli internal/engine internal/provider internal/shell internal/secret`.
- [ ] **F6.2** Manual Fable transcript in an isolated repo: install → `claude` login → `kolk --model
  claude-fable` → `/mode agent` (ceiling line) → a three-chapter saga across three wakes → reset with a
  new goal. Recorded verbatim in `docs/build-log.md`.
- [ ] **F6.3** Independent review of F1, F2, F3 diffs against their invariants; reviewer and commands
  named in `CHECKPOINTS.md`.
- [ ] **F6.4** Tick the corresponding V34 leaves (`V34.1a`, `V34.1b`, `V34.3a/b/f`, `V34.4a/b`) or
  record exactly why each stays open. Never tick a leaf from this file alone.

---

## Order and stop rules

`F0 → F1 → F2 → F3 → F4 → F5 → F6`. F0 is blocked on the V34.1a owner and must close before any
other phase's checkpoint is ticked (a red tree proves nothing). F1 and F2 may be **implemented** while
F0 is in flight — they touch different files — but coordinate on `run.go`, which both F0 and F2 edit,
and check mtimes per `AGENTS.md` before every edit. F4 and F5 are behaviour-preserving and may be
reordered if a measurement in F4.1 shows nothing worth removing. Any new P0/P1 found on the way is
triaged into the earliest phase that owns its boundary and blocks that phase's exit.

## Non-goals

- No new provider, gateway credential scheme, MCP, Windows runtime, or OS sandbox (V34.1e owns it).
- No prompt-level "prefer cheaper models" instruction — routing stays a code filter.
- No re-introduction of a `kolk saga run/resume/status/stop` surface; reset is a rule of the inline
  workflow (F1.3), not a subcommand, unless the owner decides otherwise in `docs/plan/10`.
- No performance work beyond what F4.1 measures.

## Sources

- Multi-angle code review of the uncommitted tree, 2026-09-02 (fourteen findings, verified against
  on-disk state; `go build`/`go vet` clean, `go test` red in `internal/cli`, `internal/engine`).
- `go test ./internal/cli/ ./internal/engine/` on 2026-09-02: failures share
  `secret: credential origin is not allowed`.
- `AGENTS.md` ownership note, `CHECKPOINTS.md` §V34.1a dossier, `docs/plan/34-vision-completion.md`,
  `docs/build-log.md` FR4.1–FR4.3 (ceiling, plan-native routing not built), `STIGI.md` ground truth.
- `docs/plan/04-subscription-backends.md` §model flag: fire-and-check via
  `[claude-code:unrecognized_model]`.
