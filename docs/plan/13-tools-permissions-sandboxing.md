# 13. Tools, permissions & sandboxing

Status: hardened on 2026-08-26 · v1 scope amended 2026-09-01 · supersedes: — · PLAN.md item 13

## Decision (the short version)

Three facts about the tree as it stands today, each verified in the code rather than assumed:

1. **There is no path confinement.** `internal/tools` calls `os.WriteFile(path, …)` with whatever path
   the model produced. Nothing stops a write to `~/.ssh/authorized_keys`, `~/.bashrc`, or any other
   file the process can reach. The only thing between the model and the filesystem is the confirm
   prompt.
2. **`--yolo` removes that prompt unconditionally.** `Agent.confirm` returns `true` the moment `Yolo`
   is set. There is no action it will not take, and no blocklist beneath it.
3. **Tool output is not scrubbed.** `internal/redact` and `internal/secret` both expose a `Scrub`,
   and neither is applied to tool results. A model that reads `.env` puts the contents into the
   conversation, the session file on disk, and every subsequent request to the provider.

Individually these are survivable in an interactive session where a human reads each prompt.
Together they are the reason phase F cannot come first: the moment several agents run unattended,
"a human reads each prompt" stops being true, and each of the three becomes the whole safety story.

The design is therefore ordered by what has to exist before autonomy, not by what is most
interesting: a path jail, a hardline blocklist that survives `--yolo`, scrubbed tool output, and
auto-deny inside subagents. Those in-process controls ship first. The owner accepted OS-level
sandboxing as v1 scope on 2026-09-01, but it remains a later implementation leaf because each
supported platform needs native negative proof; an accepted safety requirement is not an available
control until that evidence exists.

## Spec

### 1. Path jail (v1, on by default)

Every filesystem tool resolves its path and requires the result to be inside the **project root** —
the working directory Kolkrabbi started in, or its enclosing repository when there is one.

| | Rule |
|---|---|
| Resolution | `filepath.Abs`, then `filepath.EvalSymlinks` on the deepest existing ancestor. Symlinks are resolved *before* the check, or a link inside the root is a hole through it. |
| Inside | The resolved path equals the root or is under it after `filepath.Rel` returns no leading `..`. |
| Outside | Refused with the path and the root, before any read or write. |
| Opt-out | `kolk config set tools.root <path>` widens it deliberately; there is no flag that removes it. |
| Reads | Jailed too. Exfiltration is a read followed by a provider request, and the request is automatic. |

### 2. Permission rules

Grammar, matched **last rule wins**, following the precedent in `docs/research/ecosystem.md`:

```
allow bash(git *)
ask   write(*)
deny  write(~/.ssh/*)
deny  bash(rm -rf /*)
```

| Scope | Meaning | Storage |
|---|---|---|
| once | this call only | memory |
| session | until the process exits | memory (`SessionDecider`, already built by A8.3) |
| project | this project root | `<config>/permissions.json`, keyed by root |
| always | every project | `<config>/permissions.json`, global list |

### 3. The hardline blocklist

A small set of actions that are **denied even under `--yolo`**, because no plausible task requires
them and every one of them is unrecoverable or a credential theft:

- writes to `~/.ssh/**`, `~/.aws/**`, `~/.config/kolk/credentials.json`, `~/.gnupg/**`
- `rm -rf` of `/`, `~`, or the project root itself
- `git push --force` to a protected branch, and `git reset --hard` with unstaged changes present
- writing to Kolkrabbi's own credential store through any tool

`--yolo` means "stop asking me", not "there is nothing you would refuse". A tool that cannot refuse
anything cannot be trusted with autonomy, which is precisely what phase F asks of it.

### 4. Subagents: auto-deny, never prompt

A subagent has no terminal. Prompting from one is either a deadlock or a prompt attributed to the
wrong context, so a subagent's decider **denies** anything the rules do not already allow, and the
denial is reported to the parent as a tool result. Widening what subagents may do is done by writing
rules, which are reviewable, not by answering a prompt nobody can see.

### 5. Tool output

| Concern | Decision |
|---|---|
| Truncation | Done (R3): line-boundary, rune-safe, 12 000 bytes, announced. |
| Secrets | `redact.Scrub` applied to every tool result before it enters the conversation. The bus already scrubs (A7.2); the conversation must too, because that is what reaches the provider. |
| Binary | Detected by NUL byte in the first 8 KiB; replaced with a description and size rather than sent. |
| Large files | `read_file` accepts a line range; the unranged read of a large file returns the head plus the file's length and a hint to range it. |

### 6. New tools in v1

`grep` and `glob`, in Go, jailed and `.gitignore`-aware. They are the highest-value additions
because the agent currently cannot search: it substitutes `bash("grep …")`, which is slower, less
portable, and needs a bash confirmation for a read-only act.

Deferred with reasons: `web_fetch` and `web_search` need a network policy that does not exist yet and
belong with item 16's MCP work; `multi_edit` is worth having but is a strict addition once the jail
exists; background `bash` interacts with U0.4f's detached-process handling and needs its own leaf.

### 7. Sandboxing matrix

| Platform | Shipped now | Accepted v1 work | Post-v1 |
|---|---|---|---|
| all | path jail + blocklist + auto-deny in subagents, in process | one shared sandbox policy, explicit fallback/refusal, and bounded diagnostics | — |
| Linux | no OS sandbox | select and prove a narrow OS isolation mechanism under V34.1e; `bubblewrap` and Landlock remain candidates | — |
| macOS | no OS sandbox | select and prove a native profile under V34.1e; Seatbelt/sandbox profiles remain candidates | — |
| any | — | — | container execution for `saga` |

#### 7.1 Delegated network policy (decided 2026-09-02, F2 of `FABLE_OPTIMIZATION.md`)

A delegated child either reaches the network or it does not, and the briefing, the status line, and
the vendor flag must say the same thing. The decision is made once, in the engine, before any of
them is written, from the policy, the task's kind, and whether the vendor's child has a switch.

| `subagent_network` | `research` task | any other kind | Claude child (no switch) |
|---|---|---|---|
| `auto` (default) | enabled | disabled | **enabled, and said so** — the vendor's Bash reaches the network whatever web tools are listed, and a "disabled" that the child could contradict is worse than an honest "enabled" |
| `on` | enabled | enabled | enabled |
| `off` (strict) | disabled | disabled | **refused** — the envelope asks for what the vendor cannot do, the open fails visibly, and the task falls back or fails |

Codex states the switch both ways on every delegated envelope
(`-c sandbox_workspace_write.network_access=true|false`); omitting it would leave
`~/.codex/config.toml` in charge and let a user-side `network_access = true` contradict kolk's status
line. Only the bare session invocation — the user's own process, not a delegate — leaves the vendor's
config to decide.

**Child environment.** Both child paths (the persistent one Claude uses, the one-shot one Codex
uses) inherit the parent environment minus a denylist of credential-shaped names (`*_API_KEY`,
`*_TOKEN`, `*_SECRET`, `*_ACCESS_KEY`, `*_PAT`, `*_PASSPHRASE`, `*_PASSWORD`, and `API_KEY`/`SECRET`/
`PASSWORD` anywhere in the name; `SSH_AUTH_SOCK`). Not an allowlist: a coding child runs the
repository's own build tools, which read `GOFLAGS`, `NVM_DIR`, `CARGO_HOME` and whatever else the
user's shell exported, and an allowlist would have to know them all. The vendor's own API key is on
the denylist **on purpose**: a claude or codex child that received `ANTHROPIC_API_KEY` or
`OPENAI_API_KEY` would bill the API instead of the plan the user signed in with. Subscription children
authenticate through the vendor's own login, never through the parent environment. A user who wants
the metered API is not using the subscription handoff and should use the gateway path.

`--yolo` **inside** a sandbox is the intended default for `saga`, and only there: the sandbox is what
makes "stop asking me" safe, and until one exists, `saga` inherits the same blocklist as everything
else.

Scope acceptance does not choose a mechanism by prose. V34.1e must name the supported OS/version
matrix, fail closed when an accepted platform cannot establish isolation, explain the exact refused
capability, and exercise escape attempts on native Linux and macOS runners. Container execution and
Windows sandboxing remain outside this accepted matrix.

## Rationale

- **Order follows dependency, not interest.** Phase F runs several agents unattended. Every guard
  here is load-bearing only in that world, and absent from this one.
- **A jail beats a rule list for the common case.** Rules are for what varies; confinement is for
  what never should. Most users never need a rule at all.
- **Reads are jailed too**, because the interesting attack is not a write: it is reading a key and
  letting the next automatic request carry it to a provider.
- **In-process guards ship first** because they work identically on every platform and can be tested
  here. Native-runner evidence is required before an OS profile is called available; acceptance into
  v1 changes the completion boundary, not the evidentiary standard.

## Alternatives rejected

- **Rules without a jail** — one missing rule is a hole, and the default posture should not depend on
  a user having anticipated an attack.
- **A blocklist that `--yolo` overrides** — that is what exists now, and it means `-y` has no floor.
- **Prompting from subagents** — a prompt nobody can see is a deadlock or a lie about who approved.
- **OS sandbox first** — per-platform, unverifiable in this CI, and it would have delayed the guards
  that work everywhere.
- **Scrubbing only on the event bus** — the bus is observability; the conversation is what reaches
  the provider, and it was the unscrubbed one.

## Risks & open questions

- **A jail breaks legitimate work** — editing a dotfile, or a monorepo tool outside the root →
  mitigation: `tools.root` widens it explicitly, and the refusal names both paths so the fix is
  obvious.
- **Scrubbing can corrupt a legitimate result** — a file that legitimately contains a key-shaped
  string → mitigation: `redact` is already tested for exactly this at the bus boundary; reuse rather
  than re-invent, and count scrubs so a surprised user can see it happened.
- **Blocklist drift** — a list of dangerous commands is never complete → mitigation: treat it as a
  floor, not a perimeter; the jail and confirmations remain the primary control.
- **Open:** whether the jail should default to the repository root or the working directory when the
  two differ. Repository root is more useful and strictly wider; that widening deserves the owner's
  agreement rather than a silent choice.

## Sources

- `internal/tools/tools.go` — unconfined `os.WriteFile`, verified 2026-08-26.
- `internal/engine/agent.go:498` — `confirm` returns true under `Yolo` with no floor, verified 2026-08-26.
- `internal/redact`, `internal/secret` — existing `Scrub`, not applied to tool results, verified 2026-08-26.
- `internal/engine/decider.go` — `SessionDecider` already implements session-scoped retention (A8.3).
- `docs/research/ecosystem.md` — allow/ask/deny globs with last-match-wins, hardline blocklist
  surviving yolo, subagent auto-deny.
