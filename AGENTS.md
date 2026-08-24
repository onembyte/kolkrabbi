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

## Ownership right now (2026-08-24 02:29)

- **Live workspace note:** the owner has a `kolk` session running beside Codex in this same
  worktree. Treat its edits as independently owned immediately; re-check status and mtimes before
  every edit/stage boundary.

- **Codex:** A7.2a pure durable scanner (`internal/redact/scrub.go`, `scrub_test.go`,
  `shapes.go`, `secret.go` wiring) — in progress, files actively being edited.
- **ox-alpha:** independent verification of A7.2a gates + next-up prep. Does not edit
  `internal/redact/*` while codex holds it.
- **A7.2a handoff (02:38):** candidate is ready for independent read-only verification. Run
  `go test -race ./internal/redact ./internal/secret ./internal/arch -count=1`, the bounded scrub
  fuzz target, benchmark `BenchmarkScrub12KiB`, and `make check`; record findings in
  `CHECKPOINTS.md` or `docs/build-log.md`, but do not edit the owned implementation files.
  **VERIFIED by ox-alpha 03:05** — all gates green, evidence in CHECKPOINTS.md (acceptance box +
  verification log). A7.2a is clear to close from the verifier side.
- **ox-alpha (2026-08-24 01:50):** claimed the last open U0.4d acceptance leaf — deterministic
  golden/model tests for Markdown/diff transcript rendering (`internal/tui/markdown.go`,
  `markdown_test.go`). `internal/redact/*`, `internal/secret`, `internal/arch` remain codex's;
  this leaf touches only `internal/tui`. **DONE 02:10** — leaf closed, gates green; ownership
  released. Codex's spinner/default-model leaves (01:54–02:04) untouched.

## Project pointers

- Plan index: `PLAN.md`. Checkpoint details: `CHECKPOINTS.md`. Migration queue order:
  A6 → A7 → A8 … one group at a time.
- Gates: `make check` (fmt/vet/test/arch/purity/platforms/lint/budgets/site/surface/
  installer/spec/release). Zero external deps; stdlib-only core is a hard rule.
