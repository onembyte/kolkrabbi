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

## Ownership right now (2026-08-31)

- **No production V34 leaf is active.** Codex completed the documentation-only planning record in
  `PLAN.md`, `CHECKPOINTS.md`, and `docs/plan/34-vision-completion.md`; the next builder must claim
  exactly one queued V34 leaf before changing production code.
- **Worktree baseline:** `main` is at `4406cf19` (`release: v1.2.31`) and was clean before the
  documentation update. Re-check `git status` and recent mtimes before claiming a build leaf.
- The 2026-08-26 ownership note is historical. Its P11.6/S10/L13 work has since landed; do not use
  its list of open leaves as the current execution queue. The current forward queue is V34 in
  `PLAN.md` and `CHECKPOINTS.md`.

## Project pointers

- Plan index: `PLAN.md`. Checkpoint details: `CHECKPOINTS.md`. Migration queue order:
  A6 → A7 → A8 … one group at a time.
- Gates: `make check` (fmt/vet/test/arch/purity/platforms/lint/budgets/site/surface/
  installer/spec/release). Zero external deps; stdlib-only core is a hard rule.
