# 15. Code mode specifics

Status: hardened on 2026-08-26 · supersedes: — · PLAN.md item 15

## Decision (the short version)

Read against the tree rather than against the plan's summary of it, item 15 is further along than it
looks and its gaps are not the ones the bullet list emphasises.

**Already built, and built well enough to leave alone:** unique-match edits that refuse an ambiguous
`old_str` with the match count, checkpoints per turn, `/changes`, `/rewind`, project memory,
`/effort` mid-task — which already re-resolves the model, prints it, *and* says when a provider CLI
cannot be re-dialled because it was started with its effort and keeps it for the life of its process.
That last detail is the kind of thing this item exists to get right. G11.1 has since added the diff
preview and the create-versus-overwrite guard, both of which this item asked for; they landed under
item 11 because the confirmation prompt is where a person can act on them.

**The three gaps that matter**, in the order they are worth building:

1. **`/rewind` restores files and leaves the conversation.** Its own note says so. The model's history
   still contains the edits it made, so the next turn is reasoning about a tree that no longer
   matches what it believes. That is not an incomplete feature, it is a divergence the user cannot
   see and the model cannot detect.
2. **There is no way to see what changed.** `/changes` lists paths and verbs. Deciding whether to
   keep a session's work means reading the actual diff, and today that means leaving Kolkrabbi.
3. **No plan mode.** The item asks for read-only exploration → plan → approve → execute. Everything
   needed for it now exists — E13's permission rules can express "deny every write" in one line — so
   this is a mode, not a mechanism.

**What is deliberately out**, and why, because a list of everything anyone might want is not a plan:

- **Formatter hooks after edit.** Running a formatter is a shell command; a shell command that runs
  without appearing at a confirmation prompt is a hole in E13, and one that does appear is a prompt
  after every edit. The right home for this is item 16's hook system, where the permission story is
  the whole design rather than an afterthought.
- **Test/build command detection per language.** Guessing that a Go project is tested with
  `go test ./...` is right often enough to be trusted and wrong often enough to waste a run. The
  model can read the Makefile. Deferred until there is a reason beyond symmetry with other tools.
- **"Run tests after edits" policy per effort.** Depends on the above and inherits its problem.
- **LSP and tree-sitter symbols.** Both are modules, and the budget is spent. Not deferred —
  refused, unless the budget changes.
- **Whitespace-tolerant edit matching.** The current tool refuses an inexact match and says how many
  candidates it found, which teaches the model to include more context. Tolerant matching would make
  the failure silent instead: an edit that lands somewhere subtly different is worse than an edit
  that does not land.
- **Multi-hunk edits.** Two calls to `edit_file` are two diffs a person reads and approves
  separately. Batching them saves a round trip and costs the reviewability that G11.1 just bought.

## Spec

### 1. `/undo` — one command, both halves

`/rewind` restores files. `/undo` restores files **and** removes the turn from the conversation, so
the model's belief about the tree matches the tree.

- It undoes one turn, the most recent, and says what it did to both halves.
- If the files cannot be restored, the conversation is left alone: a half-undo that rewinds history
  while leaving the edits in place produces exactly the divergence this exists to prevent, in the
  opposite direction.
- `/rewind` stays, unchanged, for someone who genuinely wants only the files — but its note now
  points at `/undo` as the one that keeps the two in step.

### 2. `/diff` — what this session changed

The checkpoint store already keeps the pre-edit contents of every file it touched; `internal/diff`
already renders a unified diff. `/diff` is those two facts joined.

- No argument: every file changed this session, each with its diff.
- With a path: that file only.
- Truncated per file, in the middle, the way a confirmation is — and it says the count when it does.
- Files that were created show as new, not as a diff against nothing.

### 3. Plan mode — a permission rule, not a new engine

`/plan` switches the session to a read-only posture: `deny write(*)`, `deny bash(*)` as session-scoped
rules, plus a system-prompt line telling the model to explore and propose rather than act.

Building it out of E13's rules rather than a new mode flag means there is one place where "may I do
this" is answered, and `/permissions` shows a plan-mode session exactly why it is refusing things.
Leaving plan mode drops the rules it added and nothing else.

Approving a plan is not a new mechanism either: the user reads it and leaves plan mode. An "approve"
verb that silently re-enables writing would be a second permission system.

### 4. `/commit` — later, and only as a draft

Drafting a commit message through the fast lane is cheap and useful. Actually committing is a shell
command and belongs at a confirmation like any other. Sequenced after `/diff`, since it needs the
same reading of what changed, and not specified further here until that exists.

## Build leaves

- **G15.1 `/undo`** — one turn, both halves, and neither half moves without the other.
- **G15.2 `/diff`** — the session's changes as diffs, per file, truncated in the middle.
- **G15.3 plan mode** — `/plan` as session-scoped deny rules plus a prompt line, visible in
  `/permissions`, dropped on exit.

## Open questions

- **Should `/undo` be able to walk back more than one turn?** The checkpoint store keeps every turn,
  so the mechanism allows it. Repeated single undos are clearer to reason about and harder to get
  wrong, and nobody has asked for the other thing yet.
