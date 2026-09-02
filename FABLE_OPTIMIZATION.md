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
| R10 | P3 | `internal/shell/process_options.go:21` | abs→EvalSymlinks→Stat→IsDir hand-copied ×3 (`shell`, `agentcli`, `cli.verifiedProjectRoot`) | F6 |
| R11 | P3 | `internal/cli/repl.go:59` | Both REPLs re-detect the inline marker and duplicate the interrupt/error block; `runInteractivePrompt` already routes | F6 |
| R12 | P3 | `internal/engine/saga_executor.go:138` | `RunWake` is a copy of `Run`'s loop body and already diverges (strike counting, `finishStop`); compares `"blocked"` literal | F6 |
| R13 | P3 | `internal/provider/agentcli/codex.go:146` | Directory validation repeated per turn (constructor, `Build*Invocation`, `RunLinesWithOptions`) — 2×(1+dirs) syscalls per Codex turn; Claude builds an unused invocation per turn | F5 |
| R14 | P3 | `internal/cli/flags.go:17` | `posture` option and `Options.Posture` pass-through are dead; `ExecutionOptions.Provider` exists only to make the struct non-empty | F6 |
| O1 | owner | vendor model tables (`codexRungs`, `planModelCatalog`, `claudeModelAliases`, `vendorLadders`) | Model names are burned into source; the vendor renames and kolk breaks. Owner directive 2026-09-02: discover and map on every start and every login, for every vendor, then show | F4 |
| R15 | P3 | `internal/engine/subagent_backend.go:36` | Two factory signatures; legacy `SubagentBackend` silently skips confinement/network declaration; Claude-only network rule bolted onto the shared normalizer | F6 |

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
  messages name the next step; dossier in `CHECKPOINTS.md` §F1. Also folded in from F6.3:
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

## F3 — Fable is a model the harness can actually select and route below  ·  **done 2026-09-02**

**Observable:** `kolk pmodels anthropic` lists all four Claude rungs — `claude-haiku` (Claude Pro,
`low,medium,high`), `claude-sonnet`, `claude-opus`, `claude-fable` (Claude Max, `low,medium,high,max`);
a Max login selects any of them, a Pro login selects haiku and sonnet and is told fable needs
`kolk plans login anthropic "Claude Max"`; `--model claude-fable -e max` reaches the vendor as
`--model fable --effort max` and `xhigh` is accepted as an alias; a Fable session with the claude
connector signed in has the lane `claude-fable → claude-opus → claude-sonnet → claude-haiku` and
routes a `trivial` task to `claude-haiku` while `routine`/`hard` stay on Fable; with nothing signed
in, `/mode agent` on Fable says what a sign-in would unlock instead of saying nothing.

**Why:** the model this program is named after had no catalog row, so a Max user could not select it
through the plan selector. Feeds V34.4a/b.

**Correction to the plan as written.** F3.3 promised to *build* plan-native downward routing on the
strength of `docs/build-log.md` FR4.3 ("not built"). That note was stale by two days: STIGI C6–C8
built the roster (`roster.go`), level binding (`level_routing.go`), and `rungAvailable`, and on that
path `bindLevel` always binds — `modelForKind`'s gateway slots are unreachable for a plan session.
F3.3 therefore became verification with a Fable-specific test, not construction. The ground-truth
table above that says otherwise is dated and left as written.

**Evidence (live, 2026-09-02, claude 2.1.258, this machine's login):** an invented model returned
`[claude-code:unrecognized_model]` with `api_error_status: 404` and `total_cost_usd: 0` — the zero-cost
fire-and-check path `docs/plan/04` documents; `--model haiku --effort low` and `--model fable --effort
max` each completed a one-turn `-p` call (`stop_reason: end_turn`). `claude --help` lists `--effort
(low, medium, high, xhigh, max)` and names `fable` as an alias verbatim.

- [x] **F3.1** `claude-haiku` (Claude Pro) and `claude-fable` (Claude Max) rows added to
  `planModelCatalog`, with the evidence in the source comment; Max reaches haiku through
  `planSupportsModel`. `TestPlanCatalogListsFableAndHaikuWithVerifiedEfforts`,
  `TestFableNeedsMaxAndHaikuIsOnEveryClaudePlan`. The stale-premise guard
  `TestOpeningACheaperRungDoesNotGoThroughThePlanCatalogue` was rewritten: it now proves every ladder
  rung opens through the connector manifest, never nil-and-nil, regardless of the catalogue.
- [x] **F3.2** `max` → `--effort max`, `xhigh` accepted; `TestMaxEffortReachesClaudeAsMaxOnFable`.
  `EffortForPlan` already folds `xhigh` to `max` without reporting a downgrade.
- [x] **F3.3** Verified, not built: `TestAFableSessionRoutesTrivialWorkToHaikuOnThePlan` — the roster
  from Fable, trivial → haiku, routine/hard → fable, everything on fable with nothing signed in, nothing
  above the top rung, `ModelsBelowCeiling` lists the three below.
- [x] **F3.4** `reportAgentLane` at the top rung with nothing signed in prints
  `agent lane: claude-fable only — nothing cheaper is signed in; \`kolk plans login\` lets trivial work
  run on claude-haiku`; an unranked model still prints nothing. `engine.ModelsBelowCeiling` added
  beside `ModelsAboveCeiling`. `TestTopRungLaneSaysWhatASignInWouldUnlock`.
- [x] **F3.5** `docs/plan/24` Anthropic row names the four rungs and the tier each needs; dossier in
  `CHECKPOINTS.md` §F3. Three mutations (fable row removed, `ModelsBelowCeiling` empty, lane hint
  disabled) each fail their focused test and restore byte-identically.

**Exit met 2026-09-02.** Not done here, by decision: `StandardModelAliases` still maps bare `haiku`,
`opus`, `sonnet` to Claude 3-era gateway ids (`anthropic/claude-3-5-haiku` …) and `claude-max` to
`claude-opus`, not `claude-fable`. Changing what a shorthand means moves users' models silently;
that is V34.4c's catalog disposition, with an owner decision.

## F4 — Discover, don't burn: every vendor's models are mapped before they are shown  ·  `[~]` F4.1–F4.5 done 2026-09-02

**Owner decision, 2026-09-02** (verbatim): *"when kolk see models availables, ID them, do not burn
model names before knowing what's available. because if not, tomorrow claude or codex will update
his model names and kolk will stop working correctly. so EVERY time, on start, on claude login, on
codex login, on ANY login, do a MAPPING on the models, and only then show them in the model command
with the info"* — and: *"this should be like these for EVERY vendor, not only codex or claude."*

**Observable:** every connector kolk can sign into has a discovery method, and a connector without
one cannot be registered; `kolk plans login <any>` and every startup run that method (bounded, cached,
stale-while-revalidate like the gateway catalog) and write one vendor catalog file; `kolk models`,
`/model`, and `pmodels` show only mapped rows, each with its info (efforts, default effort, context,
tier, vendor version, when it was fetched) and a STATUS — `listed` (the vendor said so), `verified`
(answered a turn), `unverified` (a seed nothing has confirmed), `gone` (the vendor no longer lists
it); a model name that exists only in kolk's source is never presented as available.

**Why:** F3 proved the failure mode the owner describes. Before this leaf, `codexRungs` and
`planModelCatalog` name `gpt-5.6-pro`; `codex debug models` on 2026-09-02 lists eight models and
`gpt-5.6-pro` is not one of them, while `gpt-5.5` and `gpt-5.2` (listed) are unknown to kolk and
Sol/Terra accept an `ultra` effort `codexEfforts` refuses. That table was right on 2026-08-30 and
wrong three days later. Feeds V34.4a/b/c/d.

**Ground truth, probed 2026-09-02:**

| Vendor | Listing surface | Cost | Shape |
|---|---|---|---|
| Codex 0.149.1 | `codex debug models` (`--bundled` skips refresh); also cached at `~/.codex/models_cache.json` | zero | `{models:[{slug, display_name, visibility: list\|hide, priority, context_window, default_reasoning_level, supported_reasoning_levels:[{effort, description}]}]}`; `priority` ascends with distance from the flagship |
| Claude Code 2.1.258 | **none of its own** — no subcommand lists models; `--help` names alias examples (`fable`, `opus`, `sonnet`) and the effort set; stream-json `init.model` carries the resolved id of whatever alias was used. **The gateway catalog carries the exact ids** (`anthropic/claude-fable-5`, `claude-opus-5`, `claude-sonnet-5`, `claude-haiku-4.5`, … with context lengths) | a valid name can only be confirmed by a turn (`--max-turns 0` still spends one, $0.0088); an invalid name fails locally at zero cost with `[claude-code:unrecognized_model]`; an unreachable-API probe retries with backoff for minutes and is unusable | **preview from the gateway** (owner correction 2026-09-02): exact ids grouped by family → CLI alias, efforts from `--help`, shown as `unverified` before the first prompt; the first prompt's `init.model` → `verified`; `unrecognized_model` → `gone` |
| OpenRouter gateway | `GET /models` (already cached at `models.json`) | zero | already mapped; gains STATUS |
| Ollama host / cloud | `GET /api/tags` (already `host-models.json`) | zero | already mapped; gains STATUS |
| Gemini, future | must supply a lister to be registered | — | — |

**Files:** `internal/provider/discovery.go` (new), `internal/provider/connectors.go`,
`internal/provider/plan_models.go`, `internal/provider/agentcli/{claude,codex}.go`,
`internal/engine/ceiling.go`, `internal/cli/{cmd_plans,cmd_models,cmd_plan_models,run,slash}.go`,
`internal/paths/paths.go`.

- [x] **F4.1 The port.** `internal/provider/discovery.go`: `ModelLister`, `VendorCatalog`
  (`Find`, `Visible` — hidden and gone out, ranked first), `DiscoveredModel`, the four statuses,
  `NotListable{Reason}` (an answer, never blank), and `GatewayPreviewLister{Prefix}` (exact gateway
  ids, no `:batch`/`:fast` variants, `unverified`). The registry is `cli.modelListerFor(connector,
  gateway)`; `TestEveryConnectorCanListItsModels` iterates `provider.Plans("")` and fails on any
  connector without a lister — today: codex (catalog), claude/gemini/xai/perplexity/mistral/deepseek/
  qwen/cohere (gateway preview by prefix), ollama (ollama.com `/api/tags`), copilot (`NotListable`,
  with the reason). Mutations: hidden kept visible, variants kept, a connector returning nil.
- [x] **F4.2 Codex lister.** `agentcli.CodexLister` runs `codex --version` then `codex debug
  models` through the scrubbed child path; `ParseCodexModelCatalog` maps slug/display/visibility/
  priority/context/levels, tolerates unknown fields, and refuses non-JSON, an empty list, and rows
  without slugs. Fixture `testdata/codex_debug_models_2026-09-02.json` (the `--bundled` catalog):
  eight models, Sol rank 1 with `ultra`, `gpt-5.4` hidden, **`gpt-5.6-pro` absent**. Live run
  (`KOLK_LIVE_VENDOR=1`, env-gated test): codex 0.149.1 answered in 50 ms with the same eight, and the
  *refreshed* catalog lists `gpt-5.4`/`gpt-5.4-mini` where the bundled one hides them — the vendor's
  own answer moved between the binary and the service, which is the reason to ask live. Mutations:
  hidden ignored, rank dropped.
- [x] **F4.3 Gateway preview for every vendor without a catalog — Claude first.** Owner correction
  2026-09-02: *"we could get the exact name from openrouter, and show the available models and
  efforts from there to be the first thing that kolk do when 1st prompt to claude. and the same
  behaviour for every vendor that do not expose the models like codex does."* Built:
  `agentcli.ClaudePreviewLister` groups the gateway's `anthropic/claude-*` ids by the CLI's own
  family aliases (`fable`, `opus`, `sonnet`, `haiku` — both the modern `claude-opus-5` and the legacy
  `claude-3.5-sonnet` spellings), one row per family strongest first, exact ids newest first,
  the largest context, the CLI's five efforts, `unverified`; `:batch`/`-fast` variants never match;
  a family the CLI does not name is never invented. `provider.VendorCatalogs` store
  (`vendor-models.json`, atomic, creates its directory): `Replace` carries a turn's `verified` forward
  and keeps a dropped row as `gone`; `Verify` promotes and records the vendor's resolved id first;
  `Gone` retires only a listed row. `verifyingBackend` now observes every turn:
  `recordVendorModelOutcome` → `Verify(vendor, asked, meta.Model)` on success (the stream-json
  `init.model`), `Gone` on `IsModelRefusal` (the vendor's own phrasing only), nothing on any other
  error. `StatusVerified` left the dead-export allowlist. The registry's `claude` entry is the family
  lister; every API-key connector keeps the prefix preview. Tests:
  `TestClaudePreviewGroupsTheGatewayByTheCLIsFamilies`, `TestClaudePreviewNeedsAGatewayAndAKnownFamily`,
  `TestIsModelRefusalMatchesOnlyTheVendorsPhrasing`, `TestVendorCatalogStoreRoundTripsAndStartsEmpty`,
  `TestATurnPromotesAndARefusalRetires`, `TestReplaceCarriesVerificationForwardAndRetiresTheDropped`,
  `TestTheFirstPromptVerifiesTheModelInTheVendorCatalog`. Mutations: family pattern loosened to
  accept variants, refusal match loosened, `Replace` forgetting `verified`, the turn teaching nothing.
- [x] **F4.4 Cache and hooks.** `paths.VendorCatalogFile()` (`vendor-models.json`, F4.3's store).
  `cli.discoverVendorModels(ctx, gateway, only)` asks every enabled connector (or one), 15 s bound per
  vendor, forgets a vendor whose version changed before its fresh rows land, keeps the last catalog
  when a vendor will not answer (reported, never blanked), saves once. Hooks: **every start** —
  `newAgent` runs it behind the prompt with the gateway catalog startup already loaded
  (`refreshVendorCatalogsInBackground`); **every login** — `runConnectorLoginWith` runs it for that
  connector after the connector is recorded and prints one line a person can act on (`codex 0.149.1:
  5 models listed by codex debug models: gpt-5.6-sol, … — \`kolk models\` shows them`, or the reason
  it could not); **`kolk models --refresh`** asks every vendor in front of the user. The gateway
  preview at login reads the cached catalog (`provider.CachedCatalog`) without a client. A test seam
  (`app.modelLister`) replaces the registry so tests never run an installed CLI. Tests:
  `TestStartupDiscoversEveryEnabledConnectorInTheBackground` (a disabled connector is not asked, each
  enabled one once), `TestLoginDiscoversThatConnectorAndSaysWhatItFound` (hidden rows not named; a
  failure keeps the last catalog), `TestAVendorVersionChangeForgetsWhatATurnHadVerified` (and the
  same version carries it forward), `TestPlanLoginRunsTheVendorMappingBeforeReturning` (through the
  real login path). Mutations: disabled vendors asked, version change ignored, failure blanking the
  catalog, login hook removed.
- [x] **F4.5 Derivation.** Decision: the seed ladder stays the *ranking* (kolk ranks only what it
  knows how to rank); **availability** — which rungs the roster may descend to, which names resolve,
  which efforts are accepted — comes from the vendor catalog when the vendor has been asked, and
  from the seed only when it has not. `provider.DerivePlanModels(store)`: a seed row the vendor lists
  takes the vendor's efforts, context, and status; a seed row the vendor no longer lists is `gone`; a
  model the vendor lists and the seed never heard of is added on every tier the seed uses for that
  connector; a vendor never asked keeps its seed rows as `unverified`. `PlanModelsFrom`,
  `ResolvePlanModelFrom` (a `gone` name is refused with `ErrModelGone` naming the vendor and version;
  an unknown name is still not a plan model). `PlanModel` gains `Status` and `Context`. In cli:
  `vendorKnowsModel(store, vendor, model)` answers `rungAvailable` and the subagent factory's vendor
  detection; `discoveredEfforts` feeds `agentcli.ExecutionOptions.Efforts`, which replaces the seed
  effort set for validation (`ultra` accepted when listed, never from the seed; efforts alone never
  make an envelope); every surface (`pmodels`, TUI groups, subscription-first default, plan resolution,
  the agent-lane "out of reach" line) reads `a.planModels`/`a.resolvePlanModel`. The seed-only entry
  points `PlanModels`, `ResolvePlanModel`, `CodexEffortValid`, `NewCodexBackendFromHandle` were
  deleted — the dead-export ratchet caught them the moment production stopped calling them, which is
  the ratchet doing its job. Tests: `TestDerivedPlanCatalogIsWhatTheVendorsSaid` (gpt-5.6-pro gone,
  gpt-5.5 on both tiers with the vendor's efforts, hidden rows out, Claude preview status, a
  never-asked vendor unverified), `TestResolvePlanModelFromTheVendorCatalog`,
  `TestCodexEffortsFollowTheDiscoveredSet`, `TestRungAvailabilityFollowsTheVendorCatalog`. Mutations:
  seed never gone, gone still resolves, availability ignoring the catalog, efforts ignoring discovery.
  Known limit, recorded: kolk's dial has four levels, so a vendor's `ultra` shows in the catalog and
  is accepted by name but is not reachable through `/effort` (V34.4b).
- [ ] **F4.6 Surfaces.** `kolk models`, `/model`, `pmodels`, and the bare-model chooser render
  from the vendor catalog: `MODEL  STATUS  EFFORTS  DEFAULT  CONTEXT  TIER  SOURCE  FETCHED`. A
  `gone` model the user has configured is named at startup with what replaced it, not silently
  swapped. The plan matrix in `docs/plan/24` says which vendors list and which only verify.
- [ ] **F4.7 Proof.** Tests for: stale cache served then refreshed; missing CLI; malformed output;
  version change; a seed row never shown without `unverified`; `gpt-5.6-pro` reported `gone` against
  the 2026-09-02 fixture; ceiling/roster behaviour on a name discovery added that the seed ladder
  never heard of. One mutation per rule. `-race` on the refresh path. Dossier in `CHECKPOINTS.md`.

**Exit:** on this machine, `kolk models` after `kolk plans login openai "ChatGPT Plus"` lists exactly
what `codex debug models` lists, with `gpt-5.6-pro` absent or `gone`, and a Claude session shows its
aliases `unverified` until the first turn and `verified` after it — with no model name added to the
source in the process.

## F5 — Stop repeating work on every Fable turn  ·  `[ ]`

**Observable:** per Codex turn, directory validation runs once (constructor), not 2×(1+dirs);
the persistent Claude session path builds no unused invocation per turn; `make budgets` still passes
(startup ≈ 2 ms, ~5 MB binary); a 20-turn saga wake performs no more `EvalSymlinks`/`Stat` syscalls
than the first turn did.

**Why:** the harness's own overhead is small compared with a model call, but it is *per turn* and per
subagent, and a Fable saga is hundreds of turns. Measure first, then remove only what the measurement
shows; do not optimize past the budget gate.

**Files:** `internal/provider/agentcli/codex.go`, `claude.go`, `execution_options.go`, `session.go`,
`internal/shell/lines_process.go`, `process_options.go`.

- [ ] **F5.1** Measure: a focused benchmark (`BenchmarkCodexTurnArgv`, `BenchmarkClaudeTurnArgv`)
  and an `strace -c`-style count on Linux for one persistent Claude turn and one Codex one-shot,
  recorded in the dossier as the "before".
- [ ] **F5.2 (R13)** Normalize `ExecutionOptions` **once** in `NewCodexBackendFromHandleWithOptions` /
  the Claude constructor and pass the canonical result through; `Build*InvocationWithOptions` and
  `RunLinesWithOptions` accept already-normalized options (a marker type or a `normalized bool` that
  the constructor sets — pick the one `make arch` and the dead-export gate accept).
- [ ] **F5.3 (R13)** The persistent Claude session path stops building a full invocation per turn;
  the resume argv is composed from the retained session handle and the effort/model flags only.
- [ ] **F5.4** SAGA wake I/O: `SAGA.md` is parsed once per wake and the parsed state threaded to
  planner, budget, and executor; chapter prompts are built from the parsed state, not by re-reading.
- [ ] **F5.5** Re-measure; the "after" numbers go beside the "before". `make budgets` and `-race`.

**Tests** — `internal/provider/agentcli/execution_options_test.go`, `codex_test.go`
- `TestOptionsAreNormalizedOnceAtConstruction`
- `TestPersistentClaudeTurnBuildsNoInvocation`

---

## F6 — One implementation per rule  ·  `[ ]`

**Observable:** no behaviour change; `go test ./... -count=1` and `-race` green; the dead-export and
arch gates pass; each rule below has exactly one implementation and one test.

**Why:** the review found the same logic hand-copied three times, two REPLs duplicating the same
error block, a saga loop body that already diverged from its copy, and dead plumbing that implies a
CLI feature which does not exist. Duplicates are where the next Fable-path defect will come from.

**Files:** `internal/shell/process_options.go`, `internal/provider/agentcli/execution_options.go`,
`internal/cli/run.go`, `repl.go`, `tui_repl.go`, `flags.go`, `internal/engine/saga_executor.go`,
`subagent_backend.go`.

- [ ] **F6.1 (R10)** Export one `shell.VerifiedDir(path) (string, error)`; `agentcli.normalizeExecutionDirectory`
  and `cli.verifiedProjectRoot` call it. One error wording. Grep for `EvalSymlinks` should show one
  implementation outside tests.
- [ ] **F6.2 (R11)** `runInteractivePrompt` is the single boundary for every non-`/` line in both
  REPLs; marker detection lives only inside it; `repl.go:88`'s direct `ag.RunTurn` and the three
  copies of the interrupt/error block collapse to one.
- [ ] **F6.3 (R12)** Extract `step(ctx, repoDir, state) (StopReason, error)` from `Run`/`RunWake`;
  `RunWake` calls it once, `Run` loops it. Terminal-status guard moves into `nextChapter` so no third
  caller can reopen a completed/blocked saga. Replace the `"blocked"` literal with `SagaStatusBlocked`.
  This is where F1.1's deleted guard lands.
- [ ] **F6.4 (R14)** Delete the `posture` option and `Options.Posture` pass-through (posture is set by
  `ag.SetPosture` at wake time only), or wire a real `--posture`; drop `ExecutionOptions.Provider` or
  use it. The dead-export gate decides which.
- [ ] **F6.5 (R15)** One factory: `SubagentBackend` takes capabilities (shim at the boundary for the
  3-arg callers, removed once none remain); provider network capability becomes **data** on each
  backend's constructor/`ExecutionOptions`, so the Claude-only `validateClaudeExecutionOptions` free
  function disappears and Codex gets the same invariant by construction.
- [ ] **F6.6** `make check` and a diff review that confirms "no behaviour change" line by line.

---

## F7 — Proof and walk-back  ·  `[ ]`

**Observable:** a fresh clone at the closing commit passes `make check`; every claim in README,
`site/capabilities.html`, `docs/plan/10`, `13`, `24`, `08` matches the binary; `CHECKPOINTS.md` holds
one dossier per phase with commands and results; an independent reviewer who did not implement F1–F3
reruns the failure matrices.

- [ ] **F7.1** Fresh-clone `make check` on Linux and macOS (the V34.5a matrix), `-race` on
  `internal/cli internal/engine internal/provider internal/shell internal/secret`.
- [ ] **F7.2** Manual Fable transcript in an isolated repo: install → `claude` login → `kolk --model
  claude-fable` → `/mode agent` (ceiling line) → a three-chapter saga across three wakes → reset with a
  new goal. Recorded verbatim in `docs/build-log.md`.
- [ ] **F7.3** Independent review of F1, F2, F3 diffs against their invariants; reviewer and commands
  named in `CHECKPOINTS.md`.
- [ ] **F7.4** Tick the corresponding V34 leaves (`V34.1a`, `V34.1b`, `V34.3a/b/f`, `V34.4a/b`) or
  record exactly why each stays open. Never tick a leaf from this file alone.

---

## Order and stop rules

`F0 → F1 → F2 → F3 → F4 → F5 → F6 → F7`. F0 is blocked on the V34.1a owner and must close before any
other phase's checkpoint is ticked (a red tree proves nothing). F1 and F2 may be **implemented** while
F0 is in flight — they touch different files — but coordinate on `run.go`, which both F0 and F2 edit,
and check mtimes per `AGENTS.md` before every edit. F5 and F6 are behaviour-preserving and may be
reordered if a measurement in F5.1 shows nothing worth removing. Any new P0/P1 found on the way is
triaged into the earliest phase that owns its boundary and blocks that phase's exit.

## Non-goals

- No new provider, gateway credential scheme, MCP, Windows runtime, or OS sandbox (V34.1e owns it).
- No prompt-level "prefer cheaper models" instruction — routing stays a code filter.
- No re-introduction of a `kolk saga run/resume/status/stop` surface; reset is a rule of the inline
  workflow (F1.3), not a subcommand, unless the owner decides otherwise in `docs/plan/10`.
- No performance work beyond what F5.1 measures.

## Sources

- Multi-angle code review of the uncommitted tree, 2026-09-02 (fourteen findings, verified against
  on-disk state; `go build`/`go vet` clean, `go test` red in `internal/cli`, `internal/engine`).
- `go test ./internal/cli/ ./internal/engine/` on 2026-09-02: failures share
  `secret: credential origin is not allowed`.
- `AGENTS.md` ownership note, `CHECKPOINTS.md` §V34.1a dossier, `docs/plan/34-vision-completion.md`,
  `docs/build-log.md` FR4.1–FR4.3 (ceiling, plan-native routing not built), `STIGI.md` ground truth.
- `docs/plan/04-subscription-backends.md` §model flag: fire-and-check via
  `[claude-code:unrecognized_model]`.
