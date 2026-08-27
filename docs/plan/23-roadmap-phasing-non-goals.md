# 23. Roadmap, phasing & explicit non-goals

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 23

## Decision (the short version)

The phases this item proposes — "**v0.1** polish prototype → **v0.2** TUI + sessions + saga →
**v0.3** dashboard, MCP, parallel agents → **v0.4** subscriptions, sandboxing → **v1.0** daemon
frozen, desktop → **later** iPad" — were written before any of it existed. The shipped version is
**v1.2.1**. Every one of those milestones except the desktop app and the frozen daemon API has been
passed, and two of them (MCP, sandboxing) were passed by *deciding not to do them*, which a version
number cannot express.

So this item does three things instead of restating a plan that reality overtook:

1. **Replaces version-numbered phases with the phase letters that are actually used.** A–J, one
   `/loop` each, already in PLAN.md and already how the work runs. Version numbers were a proxy for
   ordering; the phases are the ordering.
2. **Defines done per phase against numbers that already fail the build**, rather than inventing new
   ones. `make check` is the definition of done, and it is executable.
3. **Collects the non-goals from the refusals already written**, item by item, instead of inventing a
   fresh list. A non-goal nobody has had to refuse yet is a guess; a non-goal that cost an argument
   is a decision.

And it adds one ratchet, because this item is *about* the plan and the plan's bookkeeping had no
guard: **a ticked item must have the document it claims** (L23.1).

## Spec

### 1. Phasing

The unit of work is a phase, not a version. Each is one `/loop`; each names the checkpoint leaves it
closes. Two loop shapes: **harden** (resolve an item's Decide bullets, write the doc, no production
code) and **build** (turn a hardened doc into TDD leaves, one leaf per iteration). Never run a build
loop for an item whose doc is not hardened, and never run two phases at once.

| Phase | Items | State |
| --- | --- | --- |
| A — finish the subscription path | 4, 24 | built; 24 stays `[~]` by design, since the matrix tracks vendors who change it |
| B — managed local models | 25 | `[~]`: `localia` reports and plans; installing is blocked on L13.5b4 pinning a release |
| C — sessions, context, memory | 12 | complete |
| D — the local dashboard | 17 | complete |
| E — tools, permissions, sandboxing | 13 | complete; the OS sandbox matrix is deferred, not forgotten |
| F — orchestration & per-task routing | 14 | complete |
| G — the surface | 11, 15, 16 | docs complete; the G16 leaves are queued |
| H — ship it for real | T0.5, 19–23 | 20, 21, 22, 23 hardened here; 19 remains |
| I — reach | 26–29 | docs complete; I26.7 and the I27–I29 leaves queued |
| J — borrowed hardening | 30, 31, 32 | queued; item 32 fixes a real hole rather than adding surface |

**Ordering rule, unchanged and worth restating:** finish what is half-built before starting what is
unbuilt, put correctness before the surface that displays it, and put permissions before autonomy.
That last clause is why phase E came before F: an orchestrator that can spawn subagents before the
permission floor exists is a machine for doing the wrong thing quickly.

### 2. Definition of done

A phase is done when every item in it is `[x]`, every leaf it named is closed in `CHECKPOINTS.md`,
and `make check` is green. That is not a slogan: `make check` is fifteen gates, and the numeric ones
**fail rather than warn**, because a budget that warns is a budget that gets ignored for six months.

| Measured | Budget | Where |
| --- | --- | --- |
| Binary size | 20 MB hard, 12 MB soft | `scripts/check-budgets.sh` |
| Cold start, p50 | 30 ms hard, 20 ms soft | same |
| Test count | a floor that only rises | same |
| Third-party modules | fails above two | same |
| Architecture | seven layers, imports one-way | `internal/arch` |
| Dead exports | fails, with an allowlist that must give reasons | `internal/arch` |
| Documented commands | must exist | item 22's rule |
| Plan bookkeeping | a tick must have its document | L23.1, added here |

**Cost per task on the dashboard is measured but is not a budget.** `kolk stats` records what each
session cost, which is what the item asked for; making it a *gate* would fail builds for a model
price change we do not control. It informs; it does not block.

### 3. Non-goals

Almost everything here was refused in the item that had to decide it, and each line says where. This
is a record, not a manifesto — none of it is refused permanently, and several entries name the
condition that would change the answer. Two lines are marked differently and honestly: one non-goal
is decided *here* because no item had been forced to decide it, and one is deferred rather than
refused. Collecting a list is not licence to promote a deferral into a principle.

| Non-goal | Refused in | Why, in one line |
| --- | --- | --- |
| Telemetry of any kind | 20, 29 | no background version check, no analytics: it fires on a schedule the user did not choose and leaks when they work |
| A hosted service, cloud sync | **here** | decided in this item, not collected: nothing had forced the question. Sessions are local files, so privacy is a property rather than a promise, and a sync server would convert it into one |
| Windows support *(deferred, not refused)* | 2, and the CI matrix | item 2 names a Windows CLI as a target; it is cross-built and advisory in CI, and unsupported in practice until migration step 13 |
| Plugins compiled in Go | 16 | Go plugins pin the toolchain and the build; extensibility is MCP, skills and hooks |
| Native mobile apps | 26 | two release trains between a fix and its users |
| QR pairing | 26 | Reed–Solomon we would have to own, for a six-digit code that works |
| Branch-per-session | 28 | it moves the user's HEAD and has no good ending |
| Bitbucket, Azure DevOps | 28 | each is another REST client, auth flow and fixture set for a user we do not have |
| Supervising workspace services | 29 | restart, logs and health all need to outlive kolk, which means a daemon |
| Resource telemetry (CPU, memory) | 29 | nobody could name a decision it would change; a number nobody acts on teaches its reader to skip the panel |
| LSP, tree-sitter | 15 | the two-module budget, and both are large |
| Whitespace-tolerant matching, multi-hunk edits | 15 | both trade reviewability for round trips |
| Homebrew, scoop, winget, AUR | 20 | a package that lags the release is worse than none — revisit when install friction is the complaint |
| macOS notarization for the CLI | 20 | `curl` does not quarantine; it becomes mandatory when item 19 ships something with an icon |
| OAuth "login with OpenRouter" | 22 | pasting a key is one command; OAuth is a listener, a token store and a second class of auth failure |
| A `docs/` prose reference tree | 22 | `kolk help` is generated from the table and cannot drift; a second source of truth would |
| Demo GIFs | 22 | a binary that ages the moment the UI moves and looks authoritative while wrong |
| Golden output tests for the TUI | 21 | they fail on every deliberate change and train reviewers to regenerate without reading |
| A third dependency | 2 | the budget fails the build, which is the point |

**GitHub milestones: refused.** The item asks for the roadmap to be "reflected in README and GitHub
milestones". The README half is done — it links here. The milestones half is the same mistake item 22
refused for docs: a second copy of the phase list, in a system that is not versioned with the code,
which drifts the first week someone reorders a phase in a commit. `PLAN.md` and `CHECKPOINTS.md` are
the roadmap, they review in a diff, and they now have a ratchet keeping them honest with each other.

### 4. What is actually next

1. Item 19 — the last unhardened item.
2. The queued leaves: L21.1 `kolk doctor`, L21.2 `--debug`, L21.3 fuzzing the SSE and tool-argument
   parsers, L21.4 pinning `ci.yml`'s actions.
3. Phase J: 30 (doom-loop guard), 31 (command-prefix patterns), 32 (shadow-git snapshots) — 32 first,
   since `internal/checkpoint` misses everything `bash` changed, so `/undo` cannot restore it.
4. Phase I's build leaves, and G16.

## Build leaves

- **L23.1 the plan's bookkeeping is checked** — a ticked item has a document that says it is
  hardened; a part-done item's document does not; every document has an item; every link resolves.
  Wired into `make check` and CI. *Built.*
- **L23.2 the README carries the roadmap and the non-goals** — a reader deciding whether to use kolk
  needs the refusals more than the phases. *Built.*

## Open questions

- Whether item 24 and item 25 ever become `[x]`, or stay `[~]` on purpose because the world they
  describe keeps moving.
