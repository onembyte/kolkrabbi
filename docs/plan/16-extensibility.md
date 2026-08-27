# 16. Extensibility — MCP, skills/commands, hooks

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 16

## Decision (the short version)

Three mechanisms are on the table and they are not equally ready. Read against the tree, the order is
forced by what each one costs the parts of Kolkrabbi that already exist:

1. **Markdown commands ship first.** They add no dependency, no process, no new permission surface —
   a `/name` that expands to a prompt is a prompt. Everything about them is in the file format.
2. **Hooks ship second**, and only because item 15 parked formatter-after-edit here on the grounds
   that "the permission story is the design". It is: a hook is a shell command that runs without a
   human at the prompt, which is the one thing E13 spent a whole phase making impossible by accident.
3. **MCP is deferred**, not refused — but it needs work in `internal/engine` that does not exist yet,
   and pretending otherwise would ship a permission model that cannot describe the tools it is
   permitting.

**No dynamic Go loading, ever.** A plugin that runs in the agent's address space shares its
filesystem access, its credentials and its floor. Everything below is a file the user wrote, a
process the OS isolates, or nothing.

## What the tree already provides

Worth stating because two of the three depend on it:

- `Permission.Judge` ends in `default: return VerdictAsk, "unrecognised tool"`. **A tool nobody has
  heard of already asks rather than runs**, which is the correct default for a third-party tool and
  is already there.
- The hardline floor is checked before anything else, so no rule, tier, hook or MCP server can reach
  a credential file or `sudo`.
- `internal/shell` is the only package permitted `os/exec`, so any subprocess an extension needs goes
  through one audited door.

And one thing it does not provide: **`ruleFamilies` knows `bash`, `read` and `write` and nothing
else.** A permission rule cannot currently name an MCP tool, which is the concrete blocker below.

## Spec

### 1. Markdown commands — ships first

A file becomes a slash command. `.kolk/commands/*.md` for the project, `~/.config/kolk/commands/`
for the user, project winning a name clash because it is nearer the work.

```markdown
---
description: review a diff for the things CI cannot see
---
Read the staged diff and comment on naming, error handling and anything a
reviewer would ask about. Ignore formatting; the formatter has it.

$ARGUMENTS
```

| | Rule |
|---|---|
| Name | The filename. `review.md` is `/review`. |
| Body | The prompt. Sent as a user turn, not a system prompt — a command is a thing the user said. |
| `$ARGUMENTS` | Replaced by whatever followed the command. Absent means the arguments are appended. |
| Front matter | Optional, and only `description`, which `/help` shows. |
| Permissions | None of its own. A command produces a prompt; what the model then does is judged exactly as if the user had typed it. |

**Claude Code's `.claude/commands` are read too, when `.kolk/commands` has no file of that name.**
Not converted, not imported — read. Someone who already wrote them should not have to move them to
try Kolkrabbi, and the format is close enough that divergence would be our problem to explain.

**Refused: executable commands.** A markdown command that could run a shell line would be a hook
wearing a friendlier name and would skip every decision in §2.

### 2. Hooks — ships second, and the permission story is the point

A hook is a shell command Kolkrabbi runs at a named moment. Item 15 sent formatter-after-edit here
rather than building it there, because a formatter that runs silently after every edit is a shell
command executing without anyone at the prompt.

```json
{"hooks": {"post-edit": ["gofmt -w $KOLK_FILE"], "session-end": ["notify-send 'kolk done'"]}}
```

The events, and no others to begin with: `post-edit`, `post-write`, `session-end`. Each is a moment
where something already happened; **there is no `pre-tool` hook in v1**, because a hook that can veto
a tool call is a second permission system, and E13 exists so there is exactly one.

| | Rule |
|---|---|
| Confirmation | Shown once per distinct command per session, then remembered — the same shape as a permission rule, because it is one. |
| The floor | Applies. A hook is judged by `hardline` like any other command: no `sudo`, no credential paths, no piping a download into a shell. |
| Failure | Reported, never fatal. A formatter that is not installed must not fail the edit that already happened. |
| Timeout | Bounded by effort, like `bash`. A hook that hangs must not hang the session. |
| Environment | `$KOLK_FILE` and `$KOLK_SESSION` only. Not the user's whole environment, and never a credential. |

**Refused: hooks from project files, silently.** A `.kolk/hooks.json` in a cloned repository is a
shell command a stranger wrote. Project hooks are read, shown, and confirmed once before the first
one runs — cloning a repository must not be enough to execute anything.

### 3. MCP — deferred with a named blocker

Not refused. MCP is how the ecosystem shares tools and Kolkrabbi will want it. But it cannot ship
until two things are true, and both are real work rather than schedule:

**The permission model must be able to name an MCP tool.** `ruleFamilies` covers `bash`, `read` and
`write`; a tool called `github__create_issue` belongs to none of them. `allow mcp(github__*)` needs to
mean something before a server's tools can be governed, and until it does the only honest posture is
the current one — ask every time, which makes a twelve-tool server unusable.

**Tool schemas have to stop being free.** The five built-in schemas are **2,816 bytes** of every
request — measured on 2026-08-27 by G16.5, which is also when this sentence stopped saying "about
5 KB". The estimate was wrong by nearly a factor of two, in the direction that would have justified
more mechanism than the problem needs; the search-and-load bridge is still the right shape, but it is
less urgent than a guessed number made it look. A single MCP server can add a dozen more, and the research notes this exact failure in
Hermes and Goose: schemas devour the window before the work starts. The answer the ecosystem has
converged on is a search-and-load bridge rather than a full manifest, and that is a design, not a
line of config.

Sequenced after those: stdio and HTTP transports through `internal/shell` and `net/http`, both
stdlib, so the module budget is not the obstacle. `kolk mcp add/list/rm`, namespaced as
`<server>__<tool>` so a name clash is impossible and a rule can match a prefix.

## Build leaves

- **G16.1 markdown commands** — a file is a command; project over user; `$ARGUMENTS`; `.claude/commands` read as a fallback.
- **G16.2 hook events and the confirmation** — three post-events, confirmed once per command, floor applies, failures reported not fatal.
- **G16.3 project hooks are shown before they run** — cloning a repository executes nothing.
- **G16.4 `mcp(...)` permission rules** — ✓ `mcp` is a rule family whose membership is the `__` namespace, so `allow mcp(github__*)` means one server and nothing else.
- **G16.5 tool-schema budget** — ✓ measured (2,816 bytes for five tools), bounded by a failing budget, and reported by `kolk doctor`.

## Open questions

- **Does a markdown command get to declare a mode or effort?** `---mode: chat---` is one line and
  obvious; it also makes a command a thing that reconfigures the session, which is a larger promise
  than "expands to a prompt". Left out of v1 deliberately.
- **Should `session-end` hooks run when the session was interrupted?** A notification saying work
  finished when the user pressed Ctrl+C is worse than none.
