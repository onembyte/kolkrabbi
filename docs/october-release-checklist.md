# October continuation and release checklist

Resumed by Codex on 2026-10-07 at the owner's request. The working tree contains
the V43 implementation begun in earlier sessions; its newest committed baseline
is `5869a00`, following the public `v1.3.4` release. File timestamps show the
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
8. [~] Commit reviewed changes, push, require green branch CI, and publish the
   next available version after v1.3.4. Verify signed release artifacts.
9. [ ] Update the Homebrew tap from published checksums and exercise a clean
   tap install plus upgrade. Verify the curl installer/updater independently.

## Homebrew handoff

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
