# October continuation and release checklist

Resumed by Codex on 2026-10-07 at the owner's request. The original working tree
contained V43 implementation begun in earlier sessions, based on `5869a00`
after public `v1.3.4`. Verified `v1.3.5` now ships commit `05986d1`. File timestamps showed the
unfinished vendor-recovery follow-up last changed on 2026-10-01. Older V34 and
optimization headings are historical and must not override this queue.

## Ordered release work

Each leaf needs its own focused verification and a non-author review before
closure. The final repository gate is run after material implementation changes.

1. [x] Establish the current baseline and independently review the latest
   round-6 vendor recovery fixes. Preserve the existing working tree and verify
   saved model/handle ownership, delivery claims, refusal and continuation.
2. [x] Close Surfaces against the unmodified shared gate. Its prior scoped
   review was CLEAN; the peer build blockers recorded in September were fixed
   in October and need current evidence.
3. [x] Restart validation: settled results, task dependencies, project/model
   binding, archives, worktrees, background children, old snapshots and synthesis.
   Adopt missing observable regressions; preserve uncertain work on refusal.
4. [x] In-turn fallback safety: a vendor call that has delivered a request
   cannot retry or move to another model unless it is proved never delivered or
   vendor-closed with no reported tools. A coordinated child's limit pauses the
   run and cannot switch models. Check ask/switch and free/paid rotation policies.
5. [x] Finish the optimization audit against current code. Validate O7 bounded
   spill replay and O10 test/CI improvements; reconcile O4 with the accepted
   recovery protocol before changing any prompt. O13/O14/O18 remain separate
   refactor leaves and must demonstrate a benefit before expanding release scope.
   O7's tail-only scan is deferred for integrity design, not implemented.
   O10.3–5/O17 are built; O10.1–2 and the larger refactors remain explicitly open
   in `docs/optimization-resume-audit.md`. O4's replay-on-doubt rule is superseded.
6. [x] Walk back superseded claims and reconcile PLAN, CHECKPOINTS, V43 and
   optimization checklists. Record physical/provider trials as unverified.
7. [x] Verify the final shared tree: focused race, full make check, release
   snapshot and installer contracts. Refresh release-time landing-page examples.
8. [x] Commit reviewed changes, push, require green branch CI, and publish the
   next available version after v1.3.4. Verify signed release artifacts.
9. [x] Update the Homebrew tap from published checksums and exercise a clean
   tap install plus upgrade. Verify the curl installer/updater independently.

## Homebrew handoff

### Immediate patch follow-up — parallel transcript output

Owner: Codex. Documentation CI 37632527908 found a production data race in
`TestARoutedPlanPausesRestartsResumesAndCompacts`: a worker's fallback notice
wrote directly to the shared transcript while the scheduler reported run cost.
This takes precedence over credential-free startup. v1.3.5 remains immutable;
the handoff recommendation must move to v1.3.6 after verification.

- [x] Add a deterministic RED buffering regression, route the fallback notice
  through the existing child output buffer, and verify live route events remain.
- [x] Independent review, focused race repetitions, full gates and snapshot.
- [ ] Green branch CI, publish v1.3.6, authenticate artifacts, update/test tap,
  public installer and in-session updater. Then resume credential-free setup.

Acceptance: parent regression RED (both transcript/live), focused race ×30
GREEN, full root race GREEN (83.6%), serial `make check` GREEN (5,162 tests,
lint 0, all contracts), snapshot 21 GREEN. Non-author no-network backend probe
race ×30 GREEN and old-writer mutation RED; checks concurrent scheduler writes,
private warning/result, final flush, live callback and monotonic durable
fallback event. Independent site 474/release 24/workflow 41/verifier 30/surface
24 GREEN; scoped fix CLEAN. Evidence: `/private/tmp/kolk-output-review.CPV9Em`.
The reviewer's listener-based run was blocked by approval-review limits; its
accepted pure probe needed neither escalation nor network. Fresh CI must still
exercise the full listener-based suite before tagging.

The existing distribution is a formula in the `onembyte/tap` tap, covering
macOS/Linux and amd64/arm64. Confirm the tap and formula against the published
release. The intended install command is `brew install onembyte/tap/kolk`;
existing users run `brew update` followed by `brew upgrade kolk`.

The older conversation's v1.1.5 target is superseded by the current repository.
No release is declared before its verification and package-manager handoff.

## Evidence so far

- Untouched baseline: `make check`, 5,119 tests, all gates green.
- October safety fixes: test-first, independent scoped CLEAN; commands and
  red/green/mutation evidence are in CHECKPOINTS.md's October closeout section.
- Full root race+coverage passed; 83.6% statement coverage. Current focused
  no-replay/context/binding tests passed after the final context-rule correction.
- GoReleaser 2.17.1 snapshot: all four target archives and host identity verified,
  21 checks. Pinned tool binaries downloaded from official releases and matched
  their SHA-256 manifests.
- Independent final contracts: site 474, surface 21, installer 72, spec 29,
  release 24/workflow 41/verifier 30, plan 110, workflow pins 57, all passed.
- Final shared gate passed: 5,158 tests, lint 0 issues, five platform compiles,
  stripped binary 10,428,370 bytes (9.95 MiB), cold start p50 6.3 ms, native
  sandbox overhead p50 7.4 ms. Test floor ratcheted to 4,642 (90%); no size
  budget increase. All named contract gates passed.
- Pushed CI, published assets and Homebrew handoff are not complete until
  their results are recorded here. The current PATH selects the older curl
  binary at `~/.local/bin/kolk`; Homebrew identity must be checked explicitly.
- Candidate commit b042dbb pushed. CI 37624838397 passed every job except the
  old binary ratchet (Linux 10,760,376 bytes). Release held. Measured rebaseline,
  top-30 size map and the actual public-size check are recorded in build-log;
  fresh CI must be green before tagging. Baseline Homebrew 1.3.4 install/test
  passed; the old curl copy was preserved.
- Budget follow-up independent CLEAN: all four builds below the 11 MiB promise;
  scratch matrix rejects public-bound equality, true ratchet growth and the
  unchanged absolute ceiling. Legacy MB/drifted site claims are also rejected.
- Follow-up CI 37626548802 exposed Linux PID/thread and recovery-test scheduling
  assumptions. Release was held at that point. Fixtures now use an actual foreign process
  and an explicit sibling-readiness barrier; independent adversarial scheduling
  reproduced old RED/fixed GREEN. Cached-startup timing also uses blocked network
  readiness instead of a CPU-sensitive speed threshold. Evidence in build-log.

## Verified publication and handoff — 2026-10-07

- Final branch CI 37628950051 at `05986d1`: all six jobs green, including Ubuntu
  race/coverage, Linux/macOS tests, lint, budgets and guardrails.
- Annotated `v1.3.5` resolves to `05986d115392b25a44a709c9dc829338304f0e73`.
  Release workflow 37629415554: verify and publish green. Tagged Linux gate:
  5,156 tests; size 10,760,376 bytes; cold and sandbox overhead p50 both 2.3 ms.
  Surface now 24 checks; remaining contracts and four-archive snapshot pass.
- Public `scripts/verify-release.sh v1.3.5`: Sigstore signature, all four SHA-256
  archives and stamped host identity pass. A non-author reran it independently:
  CLEAN. No physical terminal, real model subscription or GPU trial is implied.
- Homebrew tap commit `387c7c5`: generated formula exactly matches authenticated
  release checksums for four targets; non-author CLEAN before pushing. Real
  fresh baseline 1.3.4 install/test, then `brew update` and scoped
  `brew upgrade onembyte/tap/kolk` to 1.3.5 passed. `brew test` passed;
  `brew install onembyte/tap/kolkrabbi` resolved the same current formula.
- Downloaded public installer: isolated 1.3.4 → 1.3.5 upgrade, then up-to-date
  check passed. Real PTY `/update`: separate isolated 1.3.4 → 1.3.5 upgrade,
  restart, and up-to-date check passed. Piped stdin is a model prompt, not slash
  dispatch; the initial pipe experiment made only a failed localhost request.
- Actual Homebrew binary: mock-backed code write, agent plan/two tasks/synthesis
  passed; final file exactly two lines. All state and edits isolated in temporary
  directories, no API credit spent. Downloaded binary reports version 1.3.5,
  tagged commit and Go 1.25.0 darwin/arm64.
- Older curl copy remains first in the owner's PATH and was not modified.
  Use `$(brew --prefix)/bin/kolk --mode code` to test this Homebrew build.

## Next correctness checkpoint, before optional optimization

Fresh credential-free startup still exits with `/key` guidance before opening
the session in which `/key` runs. Confirmed with the published Homebrew binary,
empty private KOLK directories and both key environment overrides removed.
This pre-existing onboarding gap is **open**, not part of the completed upgrade
rehearsal. Existing credentials or a compatible/local endpoint allow startup.
Design a credential-free control/setup surface without adding a fifth outside
verb or sending commands to a model; cover hidden key input, provider login,
cancellation, persistence and first model turn test-first. Then take the ordered
optimization queue in `docs/optimization-resume-audit.md`, one owned leaf at a time.

Independent fresh-profile PTY verification reproduced this gap and the refusal
of `kolk key`. Acceptance for this next, unimplemented leaf:

- Fresh interactive `kolk` reaches a credential-not-ready setup session; `/key`,
  help and exit work without a provider/tool call or invented authorization.
- Hidden key cancellation/success, same-session readiness and restart persistence
  pass; credential shape/storage/endpoint binding remain unchanged.
- Keyed-backend model/tool execution occurs only after a valid credential and
  explicit request; key verification/catalog discovery and keyless local or
  compatible endpoints remain possible. Single-shot/stream-json missing
  credentials stays bounded and reports
  an actually executable recovery path, not an unreachable slash command.
- Existing keys, environment precedence, corrupt stores, local/compatible
  endpoints and signed-in vendors without an OpenRouter key remain correct.
- Real fresh-profile PTY RED first, focused race, independent review/full gates,
  then a new version and packaged smoke; never rewrite the published v1.3.5 tag.
