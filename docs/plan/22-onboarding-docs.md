# 22. Onboarding & docs

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 22

## Decision (the short version)

A documentation item is the easiest one in this plan to write fiction about, so this one starts from
what the binary actually does. Built fresh, with an empty HOME and no key, `kolk "hello"` prints:

```
kolk needs an API key before it can use models.
Add one:  kolk key <API_KEY>
Then run: kolk
```

Three lines, one next action, exit 0. That is the first-run path, and it is already right. Most of
what this item asks for around it is either built or should not be built:

1. **"Login with OpenRouter" — refused.** Pasting a key is one command; an OAuth flow is a browser
   round-trip, a redirect listener, a token store, a refresh path and a second way for auth to break.
2. **A mode picker on first run — refused.** kolk starts in code mode. A question at the start is a
   worse default than a default.
3. **`/help` shown once — already there**, and the welcome now also names the three dials, which the
   status line can show but cannot explain.
4. **A `docs/` reference tree — refused as specified.** `kolk help` is generated from the command
   table and is therefore always correct; a parallel prose tree is a second source of truth that
   drifts. What is built instead is a rule that makes drift *fail the build*.

## Spec

### 1. First run

| Step | State |
| --- | --- |
| No key | built: a `GuidedError` naming the exact command, then how to start |
| Free models preloaded and mapped to efforts | built: live discovery of zero-cost tool-capable models per session, with `openrouter/free` as the outage fallback |
| Pick a default mode | **refused** — code is the default and the status line says so |
| Show `/help` once | built: the first line of every session |
| How to switch mode / effort / model | built here (L22.1) |

**Why no OAuth login.** OpenRouter issues keys from a page, and `kolk key sk-or-v1-…` is one command
against a key the user already has in their clipboard. An OAuth flow buys convenience for people
without a key yet, and costs a local redirect listener, a token store with its own expiry and refresh
semantics, a second class of auth failure to diagnose, and a browser dependency on a machine that may
be a remote shell. The trade is bad at this size. It becomes reasonable if kolk ever has an installed
base that outnumbers the people who can paste a key — not before.

**Why no first-run mode picker.** Every question asked before the first turn is a question asked
before the user knows what the answers mean. kolk starts in code mode, the status line shows the
mode, and `/mode` changes it in the middle of a conversation without losing it. A picker would trade
a good default for a decision made at the worst moment.

**L22.1 — the orientation line.** The status line already reports mode, effort, model and permission
tier. What it cannot report is that all of them are changeable mid-conversation, which is the single
thing that makes the three dials worth having and the thing a first-time user has no way to discover.
A new session now ends its welcome with:

```
Switch anytime with /mode, /effort or /model. Each lists its options.
```

It names the three commands and stops there. The first draft spelled out every value
(`/mode chat|code|agent · /effort low|medium|high|max · …`), which wrapped across two lines in a
72-column terminal and — more usefully — tripped an existing guard: the phrase "mid-session: "
matched the test that forbids duplicated `session: ` metadata in the startup transcript. Each of the
three commands prints its own options when called bare, so spelling them out here bought a wrapped
line and nothing else.

A **resumed** session does not get it. That asymmetry is the whole design: an orientation repeated
every time is noise, and noise is what people learn to skip. Both halves are tested, and the welcome
is capped at three lines by a test, because the failure mode of a welcome is growth.

### 2. Docs

**What exists.** The README is the user's document: vision, install (three real paths, corrected in
item 20), the three modes, usage, the effort dial, what is refused. `docs/plan/` is the design
record, one file per plan item, and `docs/README.md` maps it. `CHANGELOG.md` is generated from commit
history at each tag. `kolk help` lists every command from the table; `kolk help <command>` prints its
grammar, also from the table; `/help` lists the slash commands and the key bindings.

**The eight-file `docs/` tree the item asked for is refused.** Commands, config, modes, effort, saga,
dashboard, providers and the subscriptions policy would all be prose restating either the command
table (which generates `kolk help` and cannot drift) or `docs/plan/` (which is the settled design and
is written for exactly this purpose). Two sources of truth for one behaviour is how a project ends up
with docs that confidently describe last quarter's flags. The README plus generated help plus the
plan docs cover the same ground with one copy of each fact.

**What replaces it: a rule that makes documentation fiction fail the build.** The failure mode of
user-facing docs is not incompleteness — `kolk help` is complete by construction. It is fiction, and
this repository has produced two examples inside a week. The README's own first line told people to
run `go build -o kolk .` against a root that holds no main package, so the very first command a new
user typed returned an error. And an error message drafted one item ago recommended `kolk doctor`,
which is queued and unbuilt; it was caught by reading, which is not a control.

So, as tests (L22.2):

- Every `` `kolk <command>` `` in the README — in backticks or inside a fenced block, never in prose,
  since "kolk asks" and "kolk contacts" are sentences — must name a command in the table.
- Every slash command named in the welcome must exist in the slash table, because that line is the
  first thing a new user is told and the last thing anyone re-reads.

The rule is deliberately asymmetric: it never demands that a command *be* documented. `kolk help` is
the complete reference, the README is a tour, and a tour that omits `kolk completion` is fine. What
is forbidden is naming something that is not real. Both checks were mutation-tested — inserting
`kolk doctor` into the README and renaming `/mode` to `/moode` each fail.

**Demo GIFs (vhs): refused for now.** A recorded terminal is a binary blob that ages the moment the
UI moves, cannot be diffed, cannot be tested, and shows a version of kolk that no longer exists while
looking authoritative. The site shows the logo and the commands as text, which is not as
persuasive and does not go stale. Revisit when the TUI stops changing — the same condition attached to golden TUI tests in item 21, for the same reason.

### 3. Built-in help

`kolk help` and `kolk help <command>` are generated from the command table, so a command that is
added, renamed or removed cannot leave stale help behind. That property is the reason the docs tree
is refused: it is the only kind of reference that cannot drift.

**`/help` contextual per mode: refused.** The slash list is short, the same commands work in all
three modes, and hiding some of them per mode would make `/help` a different document each time it is
read — which is exactly the confusion the flat list avoids. The mode-specific thing a user needs is
what the *mode* does, and that is the status line plus the README's three-mode section.

## Build leaves

- **L22.1 the orientation line** — a new session is told the three dials are changeable; a resumed one
  is not. *Built.*
- **L22.2 documentation cannot describe what does not exist** — README invocations checked against the
  command table, welcome slash commands against the slash table. *Built.*

## Open questions

- OAuth login, if the installed base ever outgrows the people who can paste a key.
- Demo recordings, when the TUI stops moving.
