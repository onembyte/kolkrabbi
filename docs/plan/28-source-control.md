# 28. Source control — branches, commits, pull requests

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 28

## Decision (the short version)

Kolkrabbi touches git in exactly one place today: the saga commits a chapter when its quality gates
pass, rolls it back when they do not, and asks `git status --porcelain` whether there is anything to
commit at all. Everything else a user does with git, the model does with `bash`, under the same
confirmation as any other command.

**That division is right and this item mostly defends it.** The question "what should Kolkrabbi do
itself?" has a sharp answer: *only the things that must be atomic with something Kolkrabbi already
owns.* A saga chapter is one — its commit is part of the verify-or-discard cycle, and a model that
forgot to commit would break the rewind story. Nothing else qualifies.

The rest of this item is therefore shorter than its bullet list suggests, and most of it is refusals.

## Spec

### 1. What Kolkrabbi does itself

Two things, and one of them is already built:

- **Saga chapter commits** (built, S10.6/X5): stage, commit with `saga(chapter N): title`, read back
  the short hash. The title is shell-quoted, because a chapter title is model-written text arriving
  on a command line.
- **`/commit`, drafting only.** Reads the staged diff, drafts a message through the fast lane, shows
  it, and stops. **It does not commit.** Item 15 already recorded why: a `/commit` that commits
  without a confirmation is a shell command wearing a costume, and `git commit` through `bash` is
  already one keystroke and one prompt away.

The floor stays where it is. `hardline` refuses a force push that is not `--force-with-lease`, and
that applies to the model, to a hook, and to anything else.

### 2. Branch-per-session — refused, with the reason

An agent run getting its own branch sounds tidy and is a trap:

- It moves the user's HEAD. Someone with a terminal open in that checkout is now on a branch they did
  not choose, and the next thing they type happens somewhere they did not expect.
- It has no good ending. Merging is a decision; deleting loses work; leaving it creates one branch per
  session, which after a week is a list nobody reads.
- **The problem it solves is already solved differently.** Isolating an agent's edits is what
  checkpoints and `/undo` are for, and they work outside a repository too — which branch-per-session
  cannot.

Where branch isolation genuinely matters is parallel writers, and F14.5 already decided that:
concurrent tasks that write are serialised, and worktrees are opt-in and later.

### 3. Pull requests — GitHub only, through `gh`, and nothing else

`gh` is on most machines that have a GitHub remote, it holds its own credentials, and shelling out to
it costs a subprocess rather than an API client, an auth story and a token in the keystore.

**Bitbucket and Azure DevOps are refused, in writing.** Each is another REST client, another auth
flow, another set of fixtures, and another thing to keep working — for a user Kolkrabbi does not have
yet. t3code supports all three; t3code also has 393 dependencies. Someone who wants them can run
their own CLI through `bash` today, which is exactly what Kolkrabbi would be doing on their behalf.

The one thing worth building is small: `/pr` drafts a title and body from the branch's commits and
hands over the `gh pr create` command with them filled in. Drafting is where the model helps; running
it is a confirmation like any other.

### 4. Repo awareness — the part that is actually valuable

Item 15 listed "git status/diff in context" and deferred it. It belongs here, and it is the highest
value thing in this item:

**A session that cannot see uncommitted changes gives advice about a tree that no longer exists.** The
cheap version is what the saga already does — `git status --porcelain` — surfaced to the model at the
start of a turn when the tree is dirty, naming the files and nothing more. Not the diff: a diff is
expensive in context and the model can read one when it needs it.

## Build leaves

- **I28.1 dirty-tree awareness** — a turn knows which files are uncommitted before it starts.
- **I28.2 `/commit`** — drafts a message from the staged diff and stops.
- **I28.3 `/pr`** — drafts a title and body, hands over `gh pr create`.

## Open questions

- ~~**Should `/commit` offer to stage?**~~ **Answered by I28.2: no.** `git add -p` is a conversation,
  and a `/commit` that quietly staged everything would surprise exactly the person who typed it —
  someone who was staging deliberately. With nothing staged it says so and names `git add -p` and
  `git add <path>`, which is help without taking the decision.
- ~~**Does dirty-tree awareness belong in the system prompt or the first user turn?**~~ **Answered by
  I28.1, and by a cost rather than a taste.** The comment on `SetExtraSystem` already says why:
  mutating the system prompt mid-session costs the provider's prompt cache, which is why loop wakeups
  are injected as user turns instead. Dirty state changes every turn, so it is the worst possible
  thing to put somewhere that must stay stable. It goes beside the turn, the same way.
