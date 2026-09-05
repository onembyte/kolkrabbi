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

- [x] **V34.0a baseline evidence** — recorded 2026-09-01 in `docs/build-log.md`: commit `5074e620`
  (`v1.2.32`), clean pre-record worktree, Go/tool versions, supported platforms/providers, and
  exact passing/blocked gates.
- [x] **V34.0b ledger reconciliation** — completed 2026-09-01 through C4.2a–c: every V34 status and
  still-open historical entry is mapped to a V34 leaf, an owner decision, an explicit deferral, or a
  superseded historical decision. Stale V34.3f/A12.5 claims were corrected; no history was deleted.
- [x] **V34.0c scope freeze** — completed 2026-09-02: owner accepted the v1 capability and platform
  matrix, explicitly including OS-level sandboxing and confirming the clean-machine/provider proof;
  current-facing docs were reconciled and an independent reviewer returned clean. Accepted scope is
  not the same as shipped implementation, so every named downstream proof remains open.

Accepted scope matrix:

| Disposition | Capability/platform boundary | Remaining proof owner |
|---|---|---|
| shipped candidate | macOS/Linux amd64+arm64 CLI/TUI; chat/code/agent; OpenRouter-compatible endpoints; host Ollama; Claude Pro/Max and ChatGPT Plus/Pro CLI handoff; sessions, dashboard/service, current permissions, and inline SAGA | owning V34.1–V34.5 leaves must close known safety, integrity, provider, local, SAGA, and release gaps |
| shipped 2026-09-05, opt-in | OS-level sandboxing on supported Linux and macOS targets (`/sandbox on`; Seatbelt, Landlock) | V34.1e closed with native fail-closed evidence on both runners; README/site say opt-in, off by default |
| owner-proven release input | clean-machine install and provider response | owner confirmed completion on 2026-09-01; V34.5b owns the exact reproducible transcript link |
| post-v1 deferred | Windows runtime support; desktop/iPad/Android clients; additional subscription providers; generated clients; containerized SAGA execution | revisit when an owner requests the surface and supplies a checkpoint/evidence environment |

**Exit review:** an independent reader can answer “what ships, what is deferred, and what proves it”
without interpreting stale phase prose.

### V34.1 — close credential, process, and filesystem boundaries

**Goal:** no untrusted endpoint, child process, file path, or output sink can casually expose a
credential or write outside its intended boundary.

- [x] **V34.1a credential-to-endpoint binding** — completed 2026-09-02: the trusted-endpoint model
  was chosen; the OpenRouter credential is bound to the canonical `https://openrouter.ai` origin
  inside the transport, endpoints are resolved before any credential is required, and every other
  `--base-url`/`OPENROUTER_BASE_URL`/saved endpoint receives a keyless client. An adversarial matrix
  (lookalike hosts, userinfo authority, HTTP downgrade, explicit ports, trailing dot/slash, case,
  query/fragment, cancellation, host/compatible routes) covers catalog, turn, and key-verification
  requests; one targeted mutation per guard is caught by a focused test; an independent reviewer
  attempted an equivalent exfiltration. Evidence in `CHECKPOINTS.md` §V34.1a.3–.4.
- [x] **V34.1b child environment minimization** — completed 2026-09-05. F2 (2026-09-02) proved the
  one-shot and persistent delegated children scrub a denylist of credential-shaped names, the vendor's
  own API key included by decision, with a sentinel proof on each and an ordinary build variable kept.
  The remaining path, the interactive `/plans login` handover, was found on inspection to inherit the
  whole parent environment — as was the own-window login runner one process further away. Both now
  build their environment with the same denylist, each with its own sentinel test that was red first
  (`CHECKPOINTS.md` §V34.1b). Three child paths, three proofs.
- [x] **V34.1c confidential, symlink-safe checkpoints** — completed 2026-09-05 as three sub-leaves
  (`CHECKPOINTS.md` §V34.1c): a restored file gets back its recorded mode under either store; a
  restore refuses a path that resolves elsewhere than when recorded or outside the root and writes
  through a root-anchored `openat`/`renameat` walk (`atomicfile.WriteBeneath`); backups of secrets
  have a stated policy in `docs/plan/32` §4–§5 — kept byte-exact for `/undo`, never displayed
  unscrubbed (`/diff` scrubs every rendered line). Each sub-leaf was red first.
- [x] **V34.1d bounded and scrubbed outputs** — completed 2026-09-05 as four sub-leaves
  (`CHECKPOINTS.md` §V34.1d): a child's output is bounded to 1 MiB before it is kept and the tool's
  note counts what was dropped; every error the provider client returns leaves through the scrubber;
  a base URL carrying userinfo is refused at the client site, the saved setting and `/doctor`; a key or
  token is never taken from a command line — `serve --token` is refused for the environment and
  pairing, `/key` reads hidden (no-echo on a terminal, a masked overlay in the TUI, one line from a
  pipe) and refuses a key typed after it, and the TUI's history keeps only the bare `/key`. Each
  sub-leaf was red first.
- [x] **V34.1e full-auto safety floor and OS sandbox** — completed 2026-09-05 as V34.1e.0–V34.1e.6 in
  `CHECKPOINTS.md`: one policy, two enforcers (Seatbelt on macOS, Landlock on Linux), opt-in via
  `/sandbox on` and off by default at the owner's direction; fail closed, no `auto`; a network deny the
  kernel cannot enforce is refused in the parent; nine native escape tests on both runners; the
  in-process floor untouched; wrapper overhead 2 ms (linux) / 6 ms (macOS); the cancel ladder proven
  through the wrapper; public claims flipped in one commit with inverse pins. Design in
  `docs/plan/13-tools-permissions-sandboxing.md` §7.2.
- [x] **V34.1f delegated execution capability envelope** — completed 2026-09-01: every provider child
  receives an explicit canonical repository root/working directory and declared network capability;
  Codex workspace-write network access and Claude web tools are enabled only through that envelope,
  while ambient credentials, unrelated writable directories, and danger-full-access remain excluded.
  Invalid capability handoffs fail visibly rather than falling back to a blind child. Focused,
  adversarial, race, cross-platform, and full repository gates are recorded in `CHECKPOINTS.md` and
  `docs/build-log.md`.

**Exit review:** each prior exploit has a regression test and the reviewer attempts an equivalent
bypass instead of only reading the fix.

### V34.2 — make turns, storage, and transports finish coherently

**Goal:** a cancelled or failed turn is terminal, accounted for, and cannot continue mutating state.

- [x] **V34.2a cancellation-aware provider shutdown** — completed 2026-09-05: a grandchild holding the
  pipes outside the process group no longer hangs `Close`; the persistent child has `WaitDelay`, kolk's
  end of the stdout pipe is closed when the reader has not returned after the kill, and the bound is
  `closeGrace + 2·outputDrainTimeout` (`CHECKPOINTS.md` §V34.2a).
- [x] **V34.2b consistent session snapshots** — completed 2026-09-05: `Save` marshals under the
  messages lock (a race under autosave was observed), and `/undo task` saves right after appending its
  reconciliation message (§V34.2b).
- [x] **V34.2c causal task rewind** — completed 2026-09-05: a path a still-standing later task also
  changed is kept and named; a restored snapshot is marked consumed in the manifest, listed as spent,
  and refused a second time (§V34.2c).
- [x] **V34.2d terminal event and replay contract** — completed 2026-09-05: an errored turn finishes
  with `turn.finished` reason `error` (open reason vocabulary, changelog bullet); SSE and stdio write the
  retained replay before live events, and a `Last-Event-ID` resume duplicates nothing (§V34.2d).
- [x] **V34.2e joined orchestration cancellation** — completed 2026-09-05: `runTasks` drains every
  running task before returning on cancellation, and each task releases its backend once, before it
  reports; the flaky subagent-backend test is deterministic (§V34.2e).
- [x] **V34.2f atomic run-cost limits** — completed 2026-09-05: admission reserves the worst known
  call for every task in flight and calibrates on the first call, so a wave cannot cross the ceiling;
  the run may still exceed it by one call, as a sequential run always could (§V34.2f).

**Exit review:** race, timeout, cancellation, restart, and duplicate-delivery tests pass; the
reviewer checks that all public terminal states have exactly one terminal outcome.

### V34.3 — make saga runs transactional and controllable

**Goal:** a saga can stop, resume, fail, and roll back without silently committing or retaining work
from a failed chapter.

- [x] **V34.3a advisory ownership and active stop** — completed 2026-09-03. Reworded by owner
  decision: the session hold is *advisory*, not a gate. `run.go` takes the lock so `kolk sessions`
  and the dashboard can say which session is live and warn when two share a directory, and a
  platform without file locks still runs sessions — it just cannot report which are running. A lock
  acquisition error is therefore not fatal, by design; the protection against two writers on one
  saga is the commit-per-chapter artifact (V34.3b), not the lock. The stop half is unchanged: Esc
  cancels the active SAGA wake through the same cancellable turn protocol as ordinary work
  (`TestTUIInlineSagaEscapeCancelsTheWakeAndRestoresPosture`). The earlier wording, "lock acquisition
  errors are fatal", contradicted the code's deliberate comment and was the reason this leaf stayed
  open through F7.4; the owner chose the code. Evidence in `CHECKPOINTS.md` §F7.
- [x] **V34.3b durable chapter state** — completed 2026-09-02 (F1 + F7 of `FABLE_OPTIMIZATION.md`):
  executing state is persisted before the worker starts and terminal state on completion, with a
  persistence failure surfaced rather than swallowed (four focused tests, red under an independent
  reviewer's mutations); the live F7.2 run shows `SAGA.md` committed inside every chapter commit, so
  the artifact and the commit are one resume anchor. The fault-injected crash/restart proof remains
  V34.3e. Evidence in `CHECKPOINTS.md` §F1 and §F7.
- [ ] **V34.3c clean rollback** — preserve pre-existing user changes while discarding failed
  chapter changes, including staged and untracked files created by that chapter.
- [ ] **V34.3d complete saga accounting** — include planner, worker, and repair usage in the same
  enforceable saga budget.
- [ ] **V34.3e crash and dirty-tree proof** — fault-inject stop, failed verification, persistence
  failure, and restart; prove neither retry nor later commit includes abandoned work.
- [~] **V34.3f SAGA inline workflow and hidden progression directive** — the inline marker, internal
  posture, one bounded wake, durable chapter/terminal state, and cancellation lifecycle are built
  through C4.1. The visible running TUI progress log remains C5 and is deliberately not claimed here
  until its own acceptance contract closes; no separate run/resume/status/stop product surface exists.
  F7.4 (2026-09-02): stays part-done for C5. Live evidence added by F7.2: the inline marker opened,
  advanced and finished a six-chapter saga across seven wakes on Fable and reset on the next goal,
  in an isolated repository (`docs/transcripts/f72-fable-saga-2026-09-02.txt`); stop and rollback
  were not demonstrated live and remain this section's exit review.

**Exit review:** start/stop/resume/rollback is demonstrated in an isolated repository with an
independent reviewer repeating the failure matrix.

### V34.4 — make provider and model selection truthful

**Goal:** users can select only a model their configured provider and subscription can actually run,
and the catalog reflects current, supported vendor capabilities.

- [~] **V34.4a subscription eligibility and tier matching** — part-done 2026-09-02 (F3/F4 of
  `FABLE_OPTIMIZATION.md`): Claude tier eligibility is tested (a Max login reaches every rung, a Pro
  login is told which plan fable needs) and model selection now reads the vendor catalog rather than
  kolk's seed. Remaining: tier gating for a *discovered* model — a vendor catalog carries no tier, so
  a newly discovered model is listed on every tier its connector already uses. F7.4 (2026-09-02):
  stays part-done; F7.2 showed a Max login reaching `claude-fable` live and the catalog marking it
  `verified` with exact id `claude-fable-5-1` on the first answered turn, which is eligibility
  observed, not the missing gate built.
- [~] **V34.4b Codex catalog policy** — part-done 2026-09-02 (F4 of `FABLE_OPTIMIZATION.md`): the
  Codex catalog is no longer written by kolk. `codex debug models` is the source, verified live
  against 0.149.1; identifiers, efforts, context and order come from the vendor; `gpt-5.6-pro` — a
  kolk seed the vendor does not list — is reported `gone` and refused by name. Remaining: kolk's dial
  has four levels, so a vendor `ultra` appears in the catalog and is accepted by name but cannot be
  reached through `/effort`. F7.4 (2026-09-02): stays part-done; unchanged by F5–F7, and F7.2's
  `/pmodels` output shows `ultra` still listed for the Codex rungs that carry it.
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
- OpenAI Codex CLI 0.149.1 help and workspace-write configuration reference, checked 2026-09-01:
  `https://github.com/openai/codex/blob/main/codex-rs/prompts/templates/permissions/sandbox_mode/workspace_write.md`.
