# 36. Parallel writers — one tree each, landed in order

Status: drafted 2026-09-06 · supersedes: — · PLAN.md item 36

## Decision (the short version)

Agent mode queues. The owner watched a four-task plan run one agent, then two, with the rest
`waiting`, on a width of three. The cause is not the width and not the vendor: it is the
scheduler's shared-tree rule in `internal/engine/orchestrator.go`, which starts a second
file-writing task only after the first has returned, because "one working tree, two agents
editing it at once is how a run produces a state neither of them intended". Every kind but
`research` and `explain` writes, so a plan of edits is a queue by construction, and `agents 2`
appears only when a read-only task overlaps a writer.

The rule is right for one tree. The fix is more trees. **A writing task runs in its own git
worktree, seeded from `HEAD` plus the user's uncommitted changes, and its result is landed into
the user's tree when it finishes, under the same per-task snapshot `/undo` already keeps.**
Writers then wait only for the width. Wherever git cannot do this — not a repository, no `git`,
a worktree that will not add, a patch that will not apply — the run falls back to today's rule
for that task and says so on its status row. A run never fails because isolation failed.

The task carries no list of paths and the vendor's own tool loop cannot be confined to one, so
"writers with disjoint paths may overlap" was considered and refused: it would be parallelism by
hope. A worktree is parallelism by construction.

## Spec

### 1. Where a task runs

| Tree | Writer task | Read-only task |
| --- | --- | --- |
| git repository, `git` present, worktree adds | its own worktree | the user's tree |
| anything else | the user's tree, serialised as today | the user's tree |

The worktree lives under the data directory, `<data>/worktrees/<session>/<task>`, never inside
the project, detached at `HEAD`. It is seeded with the user's uncommitted state — staged,
unstaged and untracked, as one binary patch — so a subagent sees the tree the user sees. Ignored
files are not there: a `node_modules` or a build directory has to be rebuilt by the task that
needs it, and the briefing says so. The subagent's tools and its vendor child both take the
worktree as their workspace; that is one field, `SubagentCapabilities.Workspace`, already read by
both.

### 2. Landing

When a writer finishes, its worktree's difference from its seed — again staged, unstaged and
untracked, one binary patch — is applied to the user's tree, one task at a time under a lock, in
finish order. The per-task snapshot (`Ckpt.BeginTask`/`EndTask`) moves from around the task's
run to around its landing: the user's tree changes only at landing, and that keeps the promise
that a snapshot means "this task alone". A patch that does not apply cleanly is not forced: the
task is reported `did not land`, its patch is kept at a named path under the data directory, and
the reason names the earlier task it collided with when git can tell. The worktree is removed
after landing, and on every failure path, including a cancelled run.

### 3. The setting

`orchestration.isolation`: `worktree` (default) or `shared`. `shared` is today's behaviour, for a
user who wants every subagent in the one tree. The plan print names the choice once per run
("each writer in its own tree" or "one tree, writers one at a time"), and a status row of a task
that fell back says `shared tree: <why>`.

### 4. Not in scope

Worktrees for the main session, a `kolk worktree` verb (reserved in plan 02 and still
unimplemented), merging by any means other than `git apply`, and any path-based confinement of
a vendor child.

## Leaves

- V36.2a — the finding above, recorded; this document; item 36 in PLAN.md.
- V36.2b — `internal/shell` owns the git plumbing: add a detached worktree, take a binary patch of
  a tree's difference from `HEAD` including untracked files, apply a patch, remove a worktree,
  each under a deadline, tested against a real repository in a temp directory.
- V36.2c — the engine: a writer task gets a worktree when it can, the scheduler no longer holds
  writers for one another when it did, landing under the snapshot in finish order, fallback and
  status rows, `-race` on the scheduler.
- V36.2d — the setting, the plan print, the settings table, docs, and the site's claim, which may
  say "several writers" only once V36.2c is on main.
