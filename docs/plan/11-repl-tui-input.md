# 11. REPL / TUI & input

Status: hardened on 2026-08-26 · supersedes: — · PLAN.md item 11

## Decision (the short version)

The item's headline question — Bubble Tea, tview, or hand-rolled — **was already answered by the
code, and the answer is forced.** `scripts/check-budgets.sh` fails the build above two third-party
modules, and the two are spent on `golang.org/x/sys` and `golang.org/x/term`. `charm.land/bubbletea`
plus Lip Gloss plus Glamour is not two modules; it is not close. The choice was never between three
frameworks, it was between a hand-rolled TUI and abandoning the dependency gate, and the gate is
load-bearing for everything else in the plan.

So `internal/tui` is hand-rolled, roughly 3,600 lines, and it exists: its own terminal byte decoder,
composer with paste and multiline, a markdown-ish renderer, `/` command completion with history, a
status line, an approval overlay, and the loading sprite. The Crush playbook shaped it — single
top-level model, regions that cannot corrupt each other, a snapshot type tests assert on.

What this document is for, then, is not the framework. It is the honest accounting of which of the
item's bullets the built surface actually satisfies, which it does not, and what happens next. Three
gaps came out of reading the code rather than the plan:

1. **No diff preview before a confirm.** The bullet asks for "show diffs/commands in full"; the
   approval overlay shows the action and a detail string. Someone approving an edit is approving a
   description of it. This is the largest gap in the item and the one with a safety edge — E13's
   permission tiers assume the human at the prompt knows what they are agreeing to.
2. **No `@file` mentions.** Naming a file to the model means typing a path and hoping.
3. **The status line does not carry context or cost.** It has model, mode, effort, session, folder,
   approval and lifecycle. The two numbers that tell someone whether to compact or stop are absent,
   and both already exist in the engine (`ContextUsage` from phase C, `spend` from F14.4).

Everything else the item asks for is either built or deliberately out.

## What is built

| Bullet | State |
|---|---|
| Framework | Hand-rolled. Forced by the module budget; recorded here so it is a decision rather than an accident. |
| Multiline input | Built: `KeyNewline` from `Shift+Enter` (`\x1b[13;2u`) and `Esc`-prefixed returns. |
| Paste handling | Built: bracketed paste (`\x1b[200~`/`\x1b[201~`) as its own key kind, so a paste cannot be read as a submit. |
| History | Built for `/` commands, feeding completion. |
| `/` completion | Built, with a suggestion list and recency. |
| Streaming markdown | Built: fences, quotes, lists, headings, rendered into the composer's own visual tokens. |
| Cursor keys, Home/End, Delete | Built. |
| Ctrl+C interrupt, Ctrl+D exit | Built, and Ctrl+C clears the composer before it exits — the double-press rule. |
| Confirm prompt | Built, and E13.6/E13.7 gave it once / session-rule / deny with the rule shown in full. |
| `NO_COLOR` | Built in `internal/term`, and it beats `FORCE_COLOR`. |
| Terminal sanitisation | Built, and it runs *before* structural parsing so an escape sequence cannot masquerade as a fence. |

## What is not, and what to do about it

### 1. Diff preview before confirm — build it

The approval overlay gets `Action` and `Detail`. For `write_file` and `edit_file` the detail is a
preview, not a diff: it does not say what is being replaced. Approving "Edit file config.go" tells
you the file, not the change.

Decided: `edit_file` and `write_file` carry a unified diff into the confirmation, rendered in the
overlay with the same sanitisation everything else gets. Truncated in the middle, not the end, with
the count of hidden lines — the last hunk is as important as the first and a preview that always
drops the tail teaches people that the tail does not matter.

For an overwrite of an existing file the diff is against its current contents; for a new file it is
the content with a line saying the file does not exist yet. **A create and an overwrite must not look
the same**, which is the create-vs-overwrite guard from item 15 arriving here first because this is
where a person can act on it.

### 2. `@file` mentions — build it

`@` in the composer completes against the project, and the named file's path is what reaches the
model — not its contents. Inlining contents at mention time would spend the window on a file the
model may not need and would race the jail: a path is checked when the tool runs, and a mention is
not a tool call.

Completion is `.gitignore`-aware where the project is a repository, because a completion list whose
first twenty entries are `node_modules` is not completion.

### 3. Context and cost in the status line — build it

`ContextUsage` and the run's spend both exist. The status line is where someone looks before deciding
whether to `/compact`, and today it makes them run a command to find out. Add context percentage and
session cost; keep them the last two fields so a narrow terminal drops them first.

### 4. Syntax highlighting — out for now

The markdown renderer marks code blocks but does not colour their contents. Doing it properly needs a
lexer per language; doing it improperly is a regex that mis-highlights and is worse than plain. Chroma
is a module the budget cannot pay for. Deferred with no plan to revisit unless the budget changes.

### 5. `Ctrl+R` history search, `Ctrl+O` verbose, `Shift+Tab` mode cycling — out for v1

Three keybindings for things that already have commands (`/model`, `/permissions`, and a history
people mostly reach for with Up). Keybindings are cheap to add and expensive to change once anyone
has learnt them; adding them before there is a complaint is guessing at muscle memory.

Reasoning/thinking display and tool-call collapsibles are deferred the same way — both need a
transcript model that can re-render a region after it has scrolled, which the current append-only
renderer deliberately does not have.

## Keymap (as built, plus what this document adds)

| Key | Action |
|---|---|
| `Enter` | submit |
| `Shift+Enter`, `Esc Enter` | newline |
| `Ctrl+C` | clear composer; again on an empty composer exits |
| `Ctrl+D` | exit |
| `Tab` | accept completion |
| `←` `→` `Home` `End` | move |
| `↑` `↓` | history / completion selection |
| `Backspace` `Delete` | delete |
| `@` | *(new)* file completion |
| `y` / `n` / `a` | in the approval overlay: allow, deny, allow with the rule shown |

## Build leaves

- **G11.1 diff preview before confirm** — edits and writes show a real diff, truncated in the middle,
  and a create is visibly not an overwrite.
- **G11.2 `@file` mentions** — completion over the project, path not contents, `.gitignore`-aware.
- **G11.3 context and cost in the status line** — the two numbers that drive the decision to compact
  or stop, dropped first when the terminal is narrow.

## Open questions

- **Does the plain REPL get the diff preview too?** It is the fallback when there is no terminal, and
  a piped session cannot render an overlay — but it can print a diff. Leaning yes, and cheap, but the
  overlay is where the decision is actually made.
