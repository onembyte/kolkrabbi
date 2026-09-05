# 32. Shadow-git snapshots — checkpoint what `bash` did too

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 32

## Decision (the short version)

`/undo` currently restores what kolk's own file tools changed and nothing else. A formatter run
through `bash`, a codegen step, an `rm` — all invisible. The README admits it, which is honest and
not a fix: the user's mental model is "kolk changed my files, kolk can put them back", and the
carve-out is exactly where a destructive turn lands.

**Adopt the shadow store, keep the copy store, and choose between them on whether `git` can do the
job here.** Not "probably both" as the item guesses — definitely both, because each covers a case the
other cannot: the copy store works in a directory that is not a repository and on a machine with no
`git`; the shadow store is the only one that sees what `bash` did.

The mechanism is a git object store outside the work tree, driven with an explicit `GIT_DIR` and
`GIT_WORK_TREE`, so the user's `.git`, index, HEAD, stash stack and reflog are never touched. That
last property was verified rather than assumed (§3).

## Spec

### 1. Which store, when

| Situation | Store | Why |
| --- | --- | --- |
| A git repository, `git` present and able to run the probe | **shadow** | the only one that sees `bash` |
| A git repository, `git` missing or the probe fails | copy | fail closed, never fail the turn |
| Not a repository | copy | there is no object store to be cheap against |
| The shadow store errors, ever | copy, for the rest of the session | a snapshot layer that breaks turns is worse than one that misses some |

The repository test is the one item 13's path jail already does to find the project root, so the two
cannot disagree about what the project is.

**No version number is checked.** Everything used here is old — a separate `GIT_DIR` with an explicit
work tree, `objects/info/alternates`, `add -A` — and a version comparison is a guess about which
release added what, made once and never revisited. The probe is the operation itself: create the
store, configure it, take one snapshot. If that succeeds, the strategy is available; if anything in
it fails, on any git, for any reason, the session uses the copy store and says so once. A feature
test cannot be wrong about the machine it is running on.

**`git` does not become a runtime requirement.** It becomes an *upgrade*: present and recent, `/undo`
covers `bash`; absent, `/undo` covers what it covers today, and says so. Anything else would make a
tool that works offline with no dependencies depend on a binary it does not ship.

### 2. Cadence and semantics

**Per turn, not per tool call.** `/rewind`'s unit is a turn, `/undo`'s unit is a turn, and the
existing port already has `BeginTurn()`. A per-tool-call snapshot would multiply the cost by the
number of tools in a turn to record intermediate states nothing can address.

The snapshot happens at **turn start**, capturing the tree as it was before anything in this turn ran
— which is what "take back the last turn" means, and it captures `bash` changes from the previous
turn as a side effect of being a whole-tree snapshot rather than a set of pre-write copies.

`Record(tool, path)` stays on the port and stays meaningful for the copy store. For the shadow store
it is a no-op: a whole-tree snapshot already contains every path, and re-snapshotting per write would
be the per-tool-call cadence rejected above.

### 3. What it costs, measured

Measured on this repository — 544 tracked files, a 222 MB `.git`, git 2.55.0, with
`objects/info/alternates` pointing at the real object store:

| Operation | Time | Notes |
| --- | --- | --- |
| First snapshot (`add -A` + `commit`) | **63 ms** | 48 ms of it is the initial `add -A` |
| Every snapshot after that | **15 ms** | one file touched |
| Shadow store size after two snapshots | **148 KB** | for a 222 MB repository |

The 148 KB is the alternates trick doing its job: blobs already in the project's object store are
referenced, not rehashed. Without it, the first snapshot would copy the tree.

Two properties were verified rather than assumed, because both are the reason to do this at all:

- **The shadow store sees a change made outside kolk.** A `sed -i` against `README.md` — the shape of
  a formatter or a codegen step — showed up as `M README.md` in the shadow store's status.
- **The user's own git state was untouched.** `git status --short` in the real repository reported
  zero lines throughout: no index entry, no stash, no reflog motion.

15 ms per turn against a turn that takes seconds is not a cost worth discussing. It is worth
*bounding*, which §4 does.

### 4. Retention, disk and garbage

- **A session keeps its snapshots for the session's life**, in a store under the session's data
  directory, keyed by a hash of the work-tree path exactly as the prior art does. Two sessions in one
  project share the store; two projects never collide.
- **Deleting a session deletes its store.** `kolk sessions rm` and `clear` already delete the
  conversation and the compaction archive; the store joins them. Nothing outlives the thing it was a
  snapshot of.
- **`kolk sessions` reports the store's size** next to the session, because a per-turn snapshot layer
  is the first thing to suspect when a data directory grows and the last thing anyone would guess.
- **No background garbage collection.** `git gc` in a store the user cannot see, on a schedule they
  did not choose, is exactly the kind of surprise this project refuses elsewhere. The store is
  bounded by session deletion, and if that turns out not to be enough, the fix is a cap on snapshots
  per session — a number, visible, in config — not a daemon.
- **Backups may hold secrets, and this is the policy for them** (V34.1c.3, 2026-09-05). A session
  edits `.env` files and key files like any other, and `/undo` needs their exact bytes, so the store
  keeps them: the copy store's `.bak` files are 0600 inside a 0700 directory, the shadow store's
  objects likewise, and both live under the session and are deleted with it (above). What the store
  never does is *show* them: every line `/diff` renders — the backup's side and the working file's
  side, new files included — passes through `internal/redact.Scrub`, the same scrubber the transcript
  passes through, so the value is replaced and the assignment's name survives. Restores are byte-exact;
  only display is scrubbed. A backup is never copied anywhere else, never published over the protocol
  (item 26 keeps content, checksums, modes and store paths private), and never registered with the
  scrubber as a literal, since a backup is not a credential kolk holds — it is the user's file.

### 5. What it must refuse

- **It never writes to the user's repository.** Not to `.git`, not the index, not a stash, not a
  branch, not the reflog. This is the whole reason for a separate `GIT_DIR`, and it is testable:
  after a snapshot, the user's `git status` must be byte-identical to before.
- **It never commits on the user's behalf.** Item 28 refuses branch-per-session and refuses a
  `/commit` that commits; a snapshot store that quietly made commits in the user's history would
  contradict both. The shadow commits live in an object store the user's git never looks at.
- **It is not exposed as a branch to check out, and `/diff --since` does not read it.** The store
  stays an implementation detail behind `/undo`, `/rewind`, `/changes` and `/diff`. Exposing it makes
  it an interface, and an interface has to keep working after the storage strategy changes for a
  directory that is not a repository — where there is no branch to offer.
- **It refuses to snapshot outside the project root**, on the same resolution rule as item 13's path
  jail, symlinks resolved first.
- **It respects the project's ignore rules.** `git add -A` reads the work tree's `.gitignore`, so
  build output and `node_modules` stay out for free. A snapshot that captured them would be the one
  thing that made this expensive.
- **It never displays a backup unscrubbed**, and **it never writes a restore through a link** — a
  restore resolves the path again, refuses when it resolves elsewhere than it did when recorded or
  outside the root, and writes through a root-anchored `openat`/`renameat` walk that a link cannot
  redirect (V34.1c.2). A restored file gets back the mode it had, under either store (V34.1c.1).

### 6. Migration

There is nothing to migrate and that is deliberate. The copy store's manifest format does not change,
existing sessions keep their `NNNNNN.bak` files and keep rewinding from them, and the shadow store
starts empty for sessions created after it lands. `RewindLastTurn` restores from whichever store
recorded the turn, which is decided per turn and written in the manifest — so a session that started
without `git` and gained it halfway through rewinds each turn the way that turn was captured.

## Build leaves

- **L32.1 the shadow store** — create, configure (`core.autocrlf=false`, `core.longpaths=true`,
  `core.symlinks=true`, `core.fsmonitor=false`, `feature.manyFiles=true`), point
  `objects/info/alternates` at the project's object store, snapshot at `BeginTurn`.
- **L32.2 strategy selection and fail-closed** — repository test, `git` presence and version test,
  and the one-way switch to the copy store on any shadow error.
- **L32.3 rewind from either store** — the manifest records which strategy captured each turn.
- **L32.4 the user's git is untouched, as a test** — `git status` byte-identical across a snapshot,
  and no new reflog entry. This is the test that must never be deleted.
- **L32.5 size reporting** in `kolk sessions`, and deletion with the session.

## Open questions

- A cap on snapshots per session, if session-lifetime retention turns out to be too generous in a
  repository with large binary assets that change every turn.
