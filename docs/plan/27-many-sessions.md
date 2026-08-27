# 27. Many sessions, one view

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 27

## Decision (the short version)

**Discovery, not a supervisor** — decided by building it. A running session holds an advisory lock on
`<sessions>/<id>.lock`, so liveness is *observed* rather than reported: a session whose process was
killed stops looking live without anything having to notice it died. There is no daemon to run, keep
alive, or reconcile with reality.

That choice costs one thing and it is worth naming: **nothing can start a session from the phone.**
A view built on discovery can only show what already exists. Starting work remotely needs a
supervisor, and a supervisor needs to be running before there is anything to supervise — which is a
different product with a different failure mode.

Two things are already built: `session.Overview` returns a card per session with its liveness, and it
does so in 26 ms across 549 real sessions while allocating less than `session.List` — because a card
decodes the header and never the transcript. The rest of this item is what a card should carry, and
where each field can honestly come from.

## Spec

### 1. What a card shows, and where it comes from

| Field | Source | Built |
|---|---|---|
| id, title, model, cwd | the session file's header, decoded without its messages | ✓ I27.2 |
| updated | same | ✓ I27.2 |
| live / idle / unknown | the advisory lock, probed without taking or creating it | ✓ I27.1 |
| **blocked** | the last `permission.requested` in the event journal with no matching `permission.resolved` | — |
| **cost** | `stats.jsonl` rows carrying this session's id | — |
| **context** | the most recent `usage.reported` event | — |

**Blocked is the field that makes this a control plane rather than a list.** A session waiting on a
permission prompt has stopped, is spending nothing, and needs a person — and it looks exactly like a
session that is thinking hard. A view that cannot tell those apart is a view that lets work sit
unnoticed for an hour.

**`unknown` stays a state.** Windows and the fallback build return `ErrUnsupported` for advisory
locks, so liveness genuinely cannot be observed there. A dashboard that reports "idle" for every
session on Windows is worse than one that admits the gap.

### 2. Reading the journal, cheaply

Each session spills its events to `<sessions>/<id>.events.ndjson`. There are 559 on this machine.
**Only live sessions are read, and only their tail** — a few KB from the end, enough to find the last
permission and usage events. An idle session cannot be blocked, so there is nothing to look for.

That constraint comes from measurement rather than taste: I27.2 showed that a full decode of every
session costs 26 ms, and parsing every journal in full would be orders of magnitude worse for a view
somebody polls. A listing that is expensive is a listing that gets called less often than it should.

### 3. Do provider CLIs appear as sessions?

**A Claude-backed Kolkrabbi session already does**, because it is a Kolkrabbi session — it has a
session file, a lock and a journal, and the backend is an implementation detail of how its turns are
answered. Nothing extra is needed and nothing extra should be invented.

**A `claude` process the user started themselves does not appear.** Kolkrabbi did not start it, cannot
see its conversation, cannot tell whether it is waiting, and cannot stop it. A card for it would be a
row with five empty fields and a name — the appearance of a control plane without the substance, and
the appearance is worse than the gap.

### 4. Two sessions, one repository

The plan asked for this and it is the sharpest question here. Two sessions editing one checkout will
interleave edits, and each one's `/undo` restores files the other may have changed since.

**Decided: the view shows it, and does not prevent it.** A card names its `cwd`, and two live cards
sharing one directory are marked as such. Kolkrabbi does not take a second lock on the working tree,
because a session that refuses to start in a directory somebody else is using would break the
ordinary case — two terminals, one repository, one person, different files — that people do all day
and that works.

What is refused is silence: sharing a checkout is a thing the user should be told about once, not a
thing discovered when an undo restores someone else's work.

## Build leaves

- **I27.3 blocked cards** — the journal tail says which live sessions are waiting on a prompt.
- **I27.4 cost and context per card** — from `stats.jsonl` and the last usage event.
- **I27.5 shared-checkout warning** — two live cards in one directory say so.
- **I27.6 the view** — `kolk sessions` and the dash page render cards; steering is item 26's tiers.

## Open questions

- **Does a card offer to answer the prompt it is blocked on?** That is the one action that turns a
  view into a control plane, and it is exactly the action item 26 gates behind the `steer` tier. The
  mechanism exists; whether the *listing* is the right place to expose it is a UI question nobody has
  hit yet.
- **Should an idle session with unfinished work look different from a finished one?** "Idle" today
  covers both the session someone closed deliberately and the one whose process was killed mid-turn.
