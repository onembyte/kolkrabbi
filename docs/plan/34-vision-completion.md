# 34. Vision completion, independent review, and release truth

Status: active on 2026-08-31 · supersedes: the forward execution order in the historical phase table · PLAN.md item 34

## Decision (the short version)

Kolkrabbi reaches the product vision through one bounded completion program, not through an
unranked accumulation of features. The program first removes known security and integrity failures,
then makes saga/orchestrated work recoverable, then makes provider and subscription claims match
what users can select, and finally proves the release outside the development machine. A checkpoint
cannot close on a passing happy-path test alone: it needs the failure case that motivated it and an
independent review of the finished diff.

## What “100% of the vision” means

The release may call itself complete only when all of the following are true:

1. The North Star flow works without a config-file edit: install, add a supported credential or
   subscription, start a session, select a valid model, and complete a turn.
2. Every accepted v1 capability has a named owner, acceptance test or manual proof, current help and
   documentation, and an explicit supported-platform statement.
3. There is no known P0 or P1 safety, persistence, lifecycle, accounting, or recovery defect within
   accepted scope. A newly found P0/P1 reopens the owning checkpoint group.
4. Security claims are enforced at the boundary, tested with a negative case, and never depend only
   on user behaviour, naming conventions, or a best-effort filter.
5. A clean environment can install the released artifact and perform the first successful turn;
   release artifacts, checksums, signatures, changelog, site, and command help agree.

This is deliberately bounded. New providers, models, desktop/mobile clients, Windows runtime
support, and an OS sandbox are not automatically v1 requirements just because an earlier roadmap
mentions them. Before final closure the owner must mark each as either **accepted v1 scope** with a
checkpoint or **post-v1 deferred** with the reason and revisit trigger. A product cannot be 100%
complete against an undefined, moving target.

## Checkpoint contract

Only one implementation leaf is active at a time. Every leaf receives a dossier in
`CHECKPOINTS.md` (or its focused test/doc record) containing:

| Step | Required evidence |
| --- | --- |
| Intent | Scope, non-goals, risk rating, invariant, and affected boundaries. |
| Red | A focused failing test, reproduction, or executable exploit showing the old behaviour. |
| Green | Smallest implementation change and focused test command that passes. |
| Adversarial review | Negative cases: cancellation, malformed input, race, symlink, crash, or privilege boundary as applicable. |
| Independent review | A different reviewer checks the diff against the invariant and reruns the focused gate. The reviewer and commands are recorded. |
| Repository gate | `make check` on a stable worktree; if the environment cannot run a portion, record the exact command, restriction, and a compensating gate. |
| Walk-back | Update `PLAN.md`, `CHECKPOINTS.md`, docs/help/release notes, and the build log so user-visible claims match code. |

For concurrency/lifecycle leaves, the adversarial review includes `-race`, bounded completion, and a
test proving that cancellation cannot leave a mutating worker behind. For security leaves, it includes
a concrete attempted bypass. For persistence and saga leaves, it includes failure injection or a
restart/crash simulation. For a release leaf, it includes a fresh-machine transcript rather than a
developer-machine assertion.

No reviewer approves their own implementation. A reviewer may request a correction; the correction
reopens the focused test and is reviewed again. Phase exits also receive a short independent audit of
all open items, so historical tables cannot drift ahead of the actual product.

## Completion hierarchy

### V34.0 — establish the release baseline

**Goal:** create a reproducible source-of-truth snapshot before changing behaviour.

- [ ] **V34.0a baseline evidence** — record commit, worktree state, Go/tool versions, supported
  platforms, enabled providers, and the exact passing/blocked gates.
- [ ] **V34.0b ledger reconciliation** — map every `[~]`, queued, superseded, and deferred entry in
  `PLAN.md`/`CHECKPOINTS.md` to this hierarchy or an owner decision; do not delete history.
- [ ] **V34.0c scope freeze** — owner accepts the v1 capability and platform matrix, including the
  explicit disposition of extra providers, desktop/mobile, Windows runtime, and OS sandboxing.

**Exit review:** an independent reader can answer “what ships, what is deferred, and what proves it”
without interpreting stale phase prose.

### V34.1 — close credential, process, and filesystem boundaries

**Goal:** no untrusted endpoint, child process, file path, or output sink can casually expose a
credential or write outside its intended boundary.

- [ ] **V34.1a credential-to-endpoint binding** — prevent a general `--base-url` or persisted
  endpoint from receiving an OpenRouter credential; choose and document the trusted-endpoint or
  endpoint-specific-credential model.
- [ ] **V34.1b child environment minimization** — ensure provider login/handover/PTY paths receive
  only the environment explicitly required for that provider; prove a sentinel secret is absent.
- [ ] **V34.1c confidential, symlink-safe checkpoints** — prevent backups from copying secrets
  without policy, reject link/race escapes on restore, and preserve restrictive source modes.
- [ ] **V34.1d bounded and scrubbed outputs** — bound child capture before allocation; redact durable
  session and terminal sinks; reject URL userinfo; replace key/token argv UX with protected input.
- [ ] **V34.1e full-auto safety floor** — decide whether full-auto is a trusted-user mode or has an
  enforceable containment boundary. If containment is promised, implement it; if not, remove any
  contrary safety claim and require explicit owner acceptance.

**Exit review:** each prior exploit has a regression test and the reviewer attempts an equivalent
bypass instead of only reading the fix.

### V34.2 — make turns, storage, and transports finish coherently

**Goal:** a cancelled or failed turn is terminal, accounted for, and cannot continue mutating state.

- [ ] **V34.2a cancellation-aware provider shutdown** — unread process output cannot deadlock close;
  cancellation always joins reader and child workers within a testable bound.
- [ ] **V34.2b consistent session snapshots** — `Save` snapshots messages under the same
  synchronization as mutation, and `/undo task` persists the reconciliation message.
- [ ] **V34.2c causal task rewind** — per-task rewind cannot erase a later task’s work and consumes
  or invalidates the corresponding snapshot after a successful restoration.
- [ ] **V34.2d terminal event and replay contract** — ordinary errors publish a terminal error event;
  SSE and stdio deliver retained replay before live events without loss or duplication.
- [ ] **V34.2e joined orchestration cancellation** — cancellation waits for every subagent before
  `RunTurn` returns or clears accounting; no post-return file, event, checkpoint, or cost mutation.
- [ ] **V34.2f atomic run-cost limits** — reserve or serialize cost budget before concurrent work so
  `MaxRunCostUSD` cannot be crossed by in-flight calls.

**Exit review:** race, timeout, cancellation, restart, and duplicate-delivery tests pass; the
reviewer checks that all public terminal states have exactly one terminal outcome.

### V34.3 — make saga runs transactional and controllable

**Goal:** a saga can stop, resume, fail, and roll back without silently committing or retaining work
from a failed chapter.

- [ ] **V34.3a exclusive ownership and active stop** — lock acquisition errors are fatal and
  `kolk saga stop` reaches the active executor through a cancellable protocol.
- [ ] **V34.3b durable chapter state** — persist planned/executing state before work and coordinate
  artifact persistence with commits so restart has one unambiguous resume anchor.
- [ ] **V34.3c clean rollback** — preserve pre-existing user changes while discarding failed
  chapter changes, including staged and untracked files created by that chapter.
- [ ] **V34.3d complete saga accounting** — include planner, worker, and repair usage in the same
  enforceable saga budget.
- [ ] **V34.3e crash and dirty-tree proof** — fault-inject stop, failed verification, persistence
  failure, and restart; prove neither retry nor later commit includes abandoned work.

**Exit review:** start/stop/resume/rollback is demonstrated in an isolated repository with an
independent reviewer repeating the failure matrix.

### V34.4 — make provider and model selection truthful

**Goal:** users can select only a model their configured provider and subscription can actually run,
and the catalog reflects current, supported vendor capabilities.

- [ ] **V34.4a subscription eligibility and tier matching** — unsupported connectors cannot become
  defaults; selection includes the signed-in plan tier rather than only the connector name.
- [ ] **V34.4b Codex catalog policy** — verify vendor-supported Codex subscription models and model
  identifiers; add Luna/Terra only if their access, tier, effort mapping, and fallback semantics are
  documented and tested. Otherwise explain their absence in the selector rather than inventing rows.
- [ ] **V34.4c provider-matrix disposition** — choose the next supported provider(s), with current
  terms/capabilities/billing/redaction fixtures, or explicitly defer each remaining matrix row.
- [ ] **V34.4d managed-local truth** — reconcile host Ollama hardware-fit, runtime, and `/localia`
  claims with executable tests and the accepted local-support matrix.

**Exit review:** every selector row has a capability/tier test; an unsupported or stale row is
rejected before becoming the default.

### V34.5 — prove the product can ship

**Goal:** demonstrate the actual release experience, not only a development checkout.

- [ ] **V34.5a supported-platform matrix** — run the defined Linux/macOS matrix; make Windows
  runtime support an accepted tested target or an explicit post-v1 boundary.
- [ ] **V34.5b T0.5 clean-machine rehearsal** — on a machine without Go or Kolkrabbi state, install
  the release artifact, configure a credential/subscription, start `kolk`, and complete a response.
- [ ] **V34.5c reproducible release evidence** — run GoReleaser and signing/verification tooling in
  a release-capable environment, preserve artifact and checksum evidence, and exercise update.
- [ ] **V34.5d surface and documentation audit** — reconcile README, help, website, installer,
  protocol, capabilities page, changelog, and all deferred-scope language with the released binary.
- [ ] **V34.5e final independent audit** — fresh security, lifecycle, and release review against the
  final candidate; triage every finding and block on accepted P0/P1 issues.

**Exit review:** a release candidate has evidence from a stable commit, a fresh installation, and a
reviewer who did not implement the candidate’s high-risk leaves.

### V34.6 — owner acceptance and closure

**Goal:** make the final claim honest and durable.

- [ ] **V34.6a owner trial** — owner performs the North Star workflow and the selected advanced
  flows; feedback becomes a leaf or a documented non-goal.
- [ ] **V34.6b closure audit** — verify every V34 leaf is closed or owner-deferred, no unacknowledged
  P0/P1 remains, and all evidence links resolve.
- [ ] **V34.6c release decision** — publish the bounded v1 completion statement or label the build
  beta with its remaining blockers. Never use a completion label for an undecided scope.

## Execution order and stop rules

The order is mandatory: V34.0 → V34.1 → V34.2 → V34.3 → V34.4 → V34.5 → V34.6. A phase may be
split into independent leaves, but a higher phase cannot claim completion while a lower-phase P0/P1
remains. Any newly discovered P0/P1 is triaged into the earliest relevant phase and blocks the phase
exit. Owner decisions are required only where scope or safety posture changes; implementation choices
inside an accepted checkpoint remain engineering work.

## Sources

- `PLAN.md` and `CHECKPOINTS.md`, reviewed 2026-08-31.
- `docs/plan/10-saga-loop.md`, `docs/plan/21-quality-testing-security.md`, and
  `docs/plan/33-agentic-mode.md`.
- Architecture, security, provider-routing, and release-gate review findings recorded 2026-08-31.
