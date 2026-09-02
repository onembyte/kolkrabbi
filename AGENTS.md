# AGENTS.md — kolkrabbi

## Team protocol (multi-agent)

This repo is built by **more than one coding agent at the same time**. Codex is the primary
builder; ox-alpha (Kolkrabbi agent) assists as an independent builder/verifier. Rules:

1. **One checkpoint leaf at a time, one owner.** The current owner is named in
   `CHECKPOINTS.md` under "Active group". Before editing a file, check `git status` and
   file mtimes — if another agent touched it in the last ~10 minutes, coordinate instead of
   overwriting.
2. **Never rewrite another agent's uncommitted work.** If something looks wrong, leave a note
   in this file or run the gates and report results rather than reverting.
3. **Verify independently.** Whoever did NOT write the code runs `make check` (or focused
   tests) before a checkpoint is marked `[x]`.
4. Record evidence (commands + results) under the checkpoint's acceptance section when
   closing it.

## Ownership right now (2026-09-02)

- **V34.1a credential-to-endpoint binding is closed 2026-09-02.** V34.1a.0–.2 were built by Codex;
  V34.1a.3 (adversarial matrix) and V34.1a.4 (walk-back, mutations, independent closeout) were
  completed by the Claude Code session at the owner's request, with the independent reviewer's
  U+0130 finding fixed and re-reviewed CLEAN. **V34.1b child environment minimization is part-done**:
  F2 of `FABLE_OPTIMIZATION.md` (Claude Code session, 2026-09-02) proved both delegated child paths
  scrub a sentinel set and decided the per-task network policy; the `kolk plans login` PTY path
  remains and is unclaimed. V34.0 is closed and V34.1f delegated execution
  capability is already complete; V34.1c–e remain queued. See also `FABLE_OPTIMIZATION.md` for the
  F1–F7 queue that follows F0/V34.1a; **F0–F4 are done** (F4 = the owner's discover-don't-burn
  decision: every vendor's models are mapped on start and on every login, never burned into source),
  and **F5 (per-turn efficiency) is next and unclaimed**.
  C5 progress-log observability remains queued under partial V34.3f. OS-level sandboxing is accepted
  v1 scope but remains unimplemented under V34.1e.**
  Do not label accepted-but-unimplemented sandboxing as shipped, and do not jump to C5 before the
  earlier V34.1/V34.2 boundaries are dispositioned.
- Re-check `git status` and recent mtimes before each further subcheckpoint. The 2026-09-01 baseline,
  Leaf A evidence, and the Leaf B acceptance contract are recorded in `docs/build-log.md` and
  `CHECKPOINTS.md`.
- The 2026-08-26 ownership note is historical. Its P11.6/S10/L13 work has since landed; do not use
  its list of open leaves as the current execution queue. The current forward queue is V34 in
  `PLAN.md` and `CHECKPOINTS.md`.

## Project pointers

- Plan index: `PLAN.md`. Checkpoint details: `CHECKPOINTS.md`. Migration queue order:
  A6 → A7 → A8 … one group at a time.
- Gates: `make check` (fmt/vet/test/arch/purity/platforms/lint/budgets/site/surface/
  installer/spec/release). Zero external deps; stdlib-only core is a hard rule.
