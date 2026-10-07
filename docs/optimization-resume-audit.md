# Optimization continuation audit — 2026-10-07

The interrupted working tree is V43 product polish and recovery, not a fresh
optimization baseline. The September F0–F7 work is closed. The September 9 O0,
O1, O2, O3, O5, O6, O8, O9, O11, O12 and O15 work is already committed. Preserve
those results rather than rebuilding them. The original continuation baseline
was v1.3.4; the verified public release is now v1.3.5.

## Current testable release scope

Finish V43 recovery and terminal/Localia polish, then verify the release and
Homebrew tap. October review found and fixed:

- model fallback/rotation that could replay a vendor turn after tool work;
- lost resume-delivery claims across the asynchronous TUI boundary;
- invalid saved main-task state/round counts admitted on durable restart;
- a test helper's disk-size limit leaking into coverage-report generation.

Tests fail before the corresponding fix. Independent real-adapter/runtime
mutation reproduces a third request with the old TUI API; the corrected path
makes only the original request and one continuation. Durable matrix tests
preserve refused journals and completed background actions/dependency results.
CI race+coverage and executable smoke checks close O10.3–5/O17, not all of O10.

## Ordered unfinished optimization work

Before optional optimization, close the fresh credential-free startup gap
recorded in `docs/october-release-checklist.md`: the public binary exits before
the `/key` session surface becomes reachable. Existing-profile upgrade and
mock modes are verified; fresh onboarding is not. Vendor interrupted/unknown
tool-status rendering and real 429 classification observations remain separate
correctness/observability follow-ups in `docs/v43-checklist.md` §7, not verified
live-provider behavior. They must not be disguised as performance work.

Each item is its own owned, measured checkpoint; these are not shipped claims.

1. O7: measure spill-reopen cost at the existing cap; design a validated
   frame index/integrity scheme before a tail scan. Preserve prefix corruption,
   session identity, cursor-expiry and persisted replay guarantees.
2. O10.1: remeasure today's slow tests, replace synchronization sleeps with
   readiness channels/clocks individually. Keep failure-path timeouts.
3. O10.2: parallelize independently owned pure tests one package at a time;
   keep process-global environment/current-directory tests serial. Recheck
   race and wall time. Coverage-floor ratcheting is a separate future leaf.
4. O4: obtain the delta-watermark design decision with the no-redo recovery
   contract as a constraint. Benchmark happy-path prompt bytes first. Unknown
   vendor state must refuse, not fall back to replaying the whole request.
5. O13: refactor newAgent, event validation, slash dispatch and config dispatch,
   one function at a time, with before/after size and unchanged behavior gates.
6. O14: package splits only after O13 exposes reviewed seams; no big-bang move.
7. O18: measure TUI duplicate blocks before extracting shared helpers.
8. O16: benchmark the scrubber/double-encode hotspot; never bypass redaction
   for token deltas. O1 already rejected that unsafe shortcut.

Physical terminal/GPU/provider-account trials remain listed in
`docs/v43-checklist.md` §7. Fixtures, race checks and cross-compilation do not
verify those machines or real subscription behavior. The September 2 sandbox
note is historical: V34.1e closed September 5. Kolk's native Bash tool has an
opt-in Seatbelt/Landlock sandbox, off by default, with explicit unsupported
platform/network-policy refusal. That is not a claim that every vendor-owned
tool loop or Windows process is confined by Kolk; delegated backends retain
their documented capability envelopes and vendor-specific limits.

Release execution and evidence: `docs/october-release-checklist.md`.
