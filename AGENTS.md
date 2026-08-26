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

## Ownership right now (2026-08-26 05:05)

- **No agent is holding a leaf.** No Copilot, Gemini, or Codex process was running at 05:05 and no
  repository file had been touched for 25 minutes. Claim a leaf here before you start.
- **Worktree state:** `main` is level with `origin/main` at `031b0847`. Four files carry an
  uncommitted, tested change — `internal/cli/cli.go`, `cmd_plans.go`, `cmd_plans_test.go`,
  `tui_repl.go` — which is checkpoint **P11.6** (suspend raw mode around a provider login and
  restore it afterwards). It is unowned; whoever picks it up should commit it as its own leaf.
- **`internal/cli/SAGA.md` is untracked and stale** — a real `kolk saga fix all tests` run from
  inside `internal/cli` at 04:03 left it there. The saga tests use `t.Chdir(t.TempDir())` and do not
  produce it. Leave it alone unless the owner says otherwise; it is not test output.
- **Ledger correction (claude, 2026-08-26):** S10.4 was closable and is now closed. The plans,
  connector, Claude-backend, and managed-local work of 2026-08-26 had shipped with no ledger entry
  and is now recorded as groups **P11**, **B12**, and **L13** in `CHECKPOINTS.md`. The next open
  leaves are P11.6, L13.4, L13.5, and the S-series leaf that finally drives the engine saga loop
  from `kolk saga`.

## Project pointers

- Plan index: `PLAN.md`. Checkpoint details: `CHECKPOINTS.md`. Migration queue order:
  A6 → A7 → A8 … one group at a time.
- Gates: `make check` (fmt/vet/test/arch/purity/platforms/lint/budgets/site/surface/
  installer/spec/release). Zero external deps; stdlib-only core is a hard rule.
