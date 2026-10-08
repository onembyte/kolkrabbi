# October continuation and release checklist

Resumed by Codex on 2026-10-07 at the owner's request. The original working tree
contained V43 implementation begun in earlier sessions, based on `5869a00`
after public `v1.3.4`. Verified `v1.3.5` shipped commit `05986d1`; verified
`v1.3.6` ships `631cd60` after the CI follow-ups below; the owner-directed UI
patch `v1.3.7` ships `fe397f9`. File timestamps showed the
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
the verified handoff recommendation is now v1.3.6.

- [x] Add a deterministic RED buffering regression, route the fallback notice
  through the existing child output buffer, and verify live route events remain.
- [x] Independent review, focused race repetitions, full gates and snapshot.
- [x] Green branch CI, publish v1.3.6, authenticate artifacts, update/test tap,
  public installer and in-session updater. Then resume credential-free setup.
- [x] Follow-up fixture leaf (Codex): CI 37666528082 found the ordinary-error
  recovery test assumed a sibling was running without establishing readiness.
  Add a channel barrier before the first failure; retain quiescent save and
  third-task admission assertions. Production scheduler stays unchanged.

Fixture acceptance: independent scheduler-yield old RED matches CI's exact
failure (no data-race diagnostic); fixed adversarial race ×100 GREEN and
ordinary/pause recovery race ×30 GREEN. Parent focused recovery race ×100
GREEN and fresh serial full gate GREEN (5,162 tests, lint 0, binary unchanged,
cold p50 5.9 ms, sandbox p50 7.5 ms). Original provider-handle semantics,
three-second context bound and all recovery assertions are preserved.
Evidence: `/private/tmp/kolk-recovery-fixture-review.MUVZzO`.

Acceptance: parent regression RED (both transcript/live), focused race ×30
GREEN, full root race GREEN (83.6%), serial `make check` GREEN (5,162 tests,
lint 0, all contracts), snapshot 21 GREEN. Non-author no-network backend probe
race ×30 GREEN and old-writer mutation RED; checks concurrent scheduler writes,
private warning/result, final flush, live callback and monotonic durable
fallback event. Independent site 474/release 24/workflow 41/verifier 30/surface
24 GREEN; scoped fix CLEAN. Evidence: `/private/tmp/kolk-output-review.CPV9Em`.
The reviewer's listener-based run was blocked by approval-review limits; its
accepted pure probe needed neither escalation nor network. Fresh CI 37667517349
subsequently exercised the full listener-based suite successfully before tagging.

### Verified patch handoff — 2026-10-07

- Final branch CI 37667517349 at `631cd60`: all six jobs passed, including
  real Ubuntu race/coverage and Linux/macOS tests. Final local root race passed
  (83.6%); serial snapshot at this commit passed 21 checks.
- Annotated `v1.3.6` resolves to `631cd60a94fa54016768aaff52ae6c7f6f55a767`
  (tag object `08f1f71c1f6860a9846741e524b861544e052757`). Release workflow
  37668047438 verify/publish passed. Tagged Linux gate: 5,159 tests,
  10,760,376 bytes, cold/sandbox p50 both 1.6 ms; snapshot 21 passed.
- Public verifier passed signature, four archive checksums/layouts and host
  identity. Non-author offline verification independently authenticated the
  exact tag-workflow/OIDC identity using cached trusted roots, checked all
  four archives/architectures/under-11-MiB sizes and generated tap bytes: CLEAN.
  Evidence: `/private/tmp/kolk-v136-offline-review.sAQ1aT`.
- Tap `3d36f53` pushed after review. Real `brew update`, scoped 1.3.5 → 1.3.6
  upgrade and `brew test onembyte/tap/kolk` passed. The kolkrabbi alias resolves
  the same current formula. No unrelated formula/cask was upgraded or trusted.
- Freshly downloaded public installer matches `site/install.sh`; isolated
  1.3.5 → 1.3.6 then up-to-date passed. Real PTY `/update` likewise upgraded an
  isolated copy, restarted and reported current 1.3.6; no model prompt was sent.
- Actual Brew binary completed mock-backed code write, agent plan/two delegated
  tools/synthesis; final file exactly two lines. Private state/workspace only,
  no remote provider or API credit. Host reports 1.3.6, tagged commit and Go
  1.25.0 darwin/arm64. These are not real provider/GPU/physical-terminal trials.
- The older curl binary remains first in PATH; launch the Brew executable
  explicitly. Fresh credential-free setup below remains open in this patch.

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

## v1.3.7 UI patch publication — 2026-10-07

Owner explicitly requested publication after testing the source UI successfully.
Codex owns packaging; the non-author reviewer checks release/tap artifacts.
Scope: the three verified effort/model-choice/public-refresh leaves below.
Credential-free onboarding remains open and is not included.

- [x] Confirm v1.3.7 is unclaimed; preserve earlier published tags.
- [x] Refresh site/snapshot version, final local full/race gates and commit the
  reviewed patch. Rehearse the committed candidate's four archives.
- [x] Push and require green branch CI before tagging v1.3.7.
- [x] Require green release CI and authenticate every published artifact.
- [x] Independently verify public artifacts and generated tap formula; publish
  the tap update and test actual Homebrew/installer/in-session update paths.
- [x] Record the exact commits/runs and hand off the update command.

Preflight: final `make check` GREEN (5,181 tests, lint 0, all five platform
compiles and contracts); full root race/coverage GREEN (83.7%). The test-count
floor is ratcheted to 4,662 (90% of the current suite), not lowered; size and
performance budgets are unchanged. Fresh non-author integrated actual-TUI and
model-discovery tests plus release/site/installer/surface contracts CLEAN:
`/private/tmp/kolk-v137-preflight-review.YSrYXa/REVIEW.md`.

Published handoff: commit `fe397f9848dc4ea5cb40e0aef6e1a079777b1324`, annotated
tag `5cfbae146262cf1f04811ff5dbd2ed44b3f0e819`. Branch CI 37692468790 all six
jobs GREEN before tag; release 37692924340 verify/publish GREEN. Tagged Linux
full gate: 5,178 tests, 10,776,760-byte binary, cold/sandbox p50 1.4/1.5 ms;
snapshot 21 passed. Parent `scripts/verify-release.sh v1.3.7` and independent
exact Sigstore workflow/OIDC authentication, four archives/build stamps and
generator-matched tap CLEAN: `/private/tmp/kolk-v137-public-review.nnwHCA`.

Tap `f9af21d` pushed. Actual `brew update`, scoped 1.3.6 → 1.3.7 upgrade and
formula test passed; the kolkrabbi alias reports 1.3.7. Actual Brew PTY effort
selection and refresh passed against metadata only; mock-backed code and agent
turns left exactly two lines. Public installer matches the source script:
private fresh install, upgrade and current check passed. Another isolated old
copy upgraded through actual PTY `/update`, then reported current after restart.
No API credit, credential inspection or real user state mutation. All temporary
processes closed. Homebrew cleaned old package/cache normally; older curl copy
still precedes Brew in PATH. Use `"$(brew --prefix)/bin/kolk" --mode code`.
No real vendor inference, fresh-key onboarding or universal model entitlement
is claimed. Publication is closed; credential-free startup below is next.

## Next correctness checkpoint, before optional optimization

Owner-directed UI follow-up (2026-10-07), Codex:

- [x] Bare `/effort` opens a purple, keyboard-selectable effort overlay in the
  TUI, with current level selected; Enter uses existing `/effort <level>`, Esc
  cancels without mutation. Preserve explicit arguments and plain REPL output.
  Test-first, actual-dispatch selection/Esc regression, independent behavioral
  old-wiring RED/current race ×30 GREEN and scoped CLEAN; full `make check`
  GREEN (5,170 tests, lint 0, all platforms/contracts). Acceptance is in
  CHECKPOINTS.md. This small leaf precedes onboarding at the owner's request.
- [x] Published and handed off through Homebrew in v1.3.7; evidence above.

Owner-directed model-list follow-up (2026-10-07), Codex:

- [x] Offer distinct discovered Claude versions alongside the latest family
  aliases, and every visible discovered Codex model before its first turn.
  Exact Claude IDs must reach vendor argv unchanged (gateway dotted versions
  become native hyphenated IDs). Preserve each pinned context, preview status,
  active-account routing and conservative cross-tier derivation. No inference
  probes, guessed entitlement or model-version seed additions. Exclude hidden,
  gone and absent seed entries; signed-out rows need a real login instruction.
  Parent test-first regressions/focused race, independent behavioral mutations
  and race ×30 CLEAN, final full `make check` GREEN (5,175 tests). Metadata-only
  installed Codex lister and public gateway rehearsal passed without inference.
  Acceptance is recorded in CHECKPOINTS.md.
- [x] Published both UI follow-ups in v1.3.7. Run `/models --refresh` before
  opening `/model` so an older
  cached family-only catalog does not hide the new rows.

Owner screenshot follow-up (2026-10-07), Codex:

- [x] Connect the previously unreachable `/models` catalog handler to session
  dispatch, help and completion, including `--refresh`. The preceding model-list
  tests exercised discovery/presentation but missed the public command. Actual
  slash and exact outside-help/refusal RED/GREEN now cover that gap; selected
  model/effort/conversation stay unchanged. Independent actual slash/TUI race
  ×30 and behavioral mutations CLEAN; final full gate GREEN (5,181 tests),
  CLI/TUI race GREEN. Acceptance is in CHECKPOINTS.md.
- [x] Published with both UI leaves in v1.3.7 and verified through Homebrew;
  no fifth outside verb was introduced.

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
  then a new version and packaged smoke; never rewrite any published tag.
