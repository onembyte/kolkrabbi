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
| Linux | no OS sandbox | **Landlock**, decided in §7.2; proved under V34.1e | — |
| macOS | no OS sandbox | **Seatbelt** via `sandbox-exec`, decided in §7.2; proved under V34.1e | — |
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

#### 7.2 The OS sandbox — V34.1e, hardened 2026-09-05

Accepted v1 scope, decided here, **unshipped until V34.1e.0–V34.1e.6 close.** Nothing public says
"available" before the closing leaf flips it; the README, the capabilities catalogue, the comparison
pages and `llms.txt` all currently say there is no execution sandbox, and they stay that way until
the escape tests are green on native macOS and Linux runners.

**Decision.** One policy, two enforcers, fail closed.

*One policy.* A `shell.Sandbox` value carried on `shell.Cmd`: writes are allowed only under the jail
root and the process temp dir; reads are allowed everywhere except a credential denylist (`~/.ssh`,
`~/.aws`, `~/.gnupg`, kolk's own credential store — §3's hardline paths, now enforced by the kernel
instead of by matching strings); network is `allow` or `deny`. The root is `tools.Options.Root` —
the same value the path jail uses, never a second setting that can drift from it. Network for a
delegated child comes from §7.1's existing decision; for the user's own bash tool it defaults to
`allow`, because `go test`, `npm` and `pip` fetch, and the status line says which it is.

*macOS — Seatbelt.* `/usr/bin/sandbox-exec -f <profile> bash -c <cmd>`, with a generated SBPL
profile: `(deny default)`, allow `process-exec`/`process-fork`, `(allow file-read*)` with
`(deny file-read* (subpath …))` for each denylist path, `(allow file-write* (subpath <root>)
(subpath <temp>))`, network allowed or `(deny network*)`, and the small set of `mach-lookup` and
`sysctl-read` allowances a shell needs to start. **Amended in V34.1e.1:** the profile travels inline
(`sandbox-exec -p`) rather than through a 0600 file — it holds only paths, and a file would need a
lifetime, a cleanup and a race with the process that reads it. And **writes also include the
toolchain caches** (user cache dir, `GOPATH`, `GOMODCACHE`): escape test 8 found that `go test`
writes its build cache outside the root, so "root and temp only" broke the one command people
turn a sandbox on for. The child's `TMPDIR` is set to the policy's temp for the same reason. Verified live on Darwin arm64 on 2026-09-05: a deny-default profile
allowed a write inside the granted subpath and refused one outside with `Operation not permitted`.
Apple marks `sandbox-exec` deprecated and has shipped it on every release since 10.5; Chromium,
Bazel and Codex CLI use it today. Its absence is checked once at startup and is a fail-closed
condition.

*Linux — Landlock.* Through `golang.org/x/sys/unix`, which is already the module's dependency; no
cgo, no external binary. The ABI is probed at runtime: ABI ≥ 1 gives filesystem confinement, ABI ≥ 4
(kernel 6.7+) gives TCP `connect`/`bind` rules. Landlock is allow-only — there is no deny rule — so
**every** grant, reads and writes alike, is a tree walk (`grantTree`, **amended in V34.1e.2b**): a
directory is granted whole only when no denylist path lies beneath it, and is otherwise enumerated
one level with the denied children skipped. Reads start at `/`; writes start at the root, the temp
and each toolchain cache. The write half was not in the first draft, and CI found the hole: a root
widened to the whole home carried `~/.ssh` with it, because a single write rule on the root cannot be
"excepted". Escape tests 4 and 9 pin both halves. Go has no pre-exec hook, so the child is `kolk` re-executed in front of the
command, told what to do by two environment variables (`KOLK_LANDLOCK_CHILD=1` and the policy as
JSON in `KOLK_LANDLOCK_POLICY` — paths only) rather than by an argv verb: **amended in V34.1e.2a**,
because a `landlock-exec` verb would have been a fifth outside-session command on a surface this
plan and its tests insist has four. The child applies the ruleset, strips both variables so a
`kolk` run inside the sandbox cannot mistake itself for the child, and `execve`s `bash -c <cmd>`;
the wrapper is the process-group leader, so §S10.1d2's group kill is unchanged. Until the ruleset
exists the child **refuses** (exit 125) rather than exec unconfined. A kernel without Landlock
(`ENOSYS`, `EOPNOTSUPP`, LSM not enabled) is a fail-closed condition. `network = deny` on ABI < 4
is **refused**, not approximated: the sandbox cannot enforce it, so it does not pretend to.

*Windows and everything else.* Outside the matrix, as §7 already says. The mechanism reports
`unsupported`, and the bash tool refuses unless `sandbox = off`.

*Config and the command.* **Opt-in, owner's decision 2026-09-05.** `sandbox = on | off`, default
`off`; `/sandbox on|off` switches it for the session and bare `/sandbox` prints the state;
`/config set sandbox on` persists it. There is deliberately **no `auto`**: auto is a silent
downgrade, and the one rule that survives the switch to opt-in is that the state is always explicit
and always visible — the status line shows `sandbox: off` / `seatbelt` / `landlock v4`, and
`/doctor` shows the mechanism, the ABI or profile path, and whether network is enforced.

Sandbox on does **not** mean offline. It confines writes to the root and temp; network stays
`allow` for the user's own bash tool unless §7.1's policy says otherwise, so builds still fetch.

"Fail closed" now means: `/sandbox on` on a platform that cannot establish isolation is **refused
at the command**, with the reason and nothing toggled; and if a mechanism that was present vanishes
mid-session, the next command refuses rather than running unconfined. Default-off is a knowing
trade: `full-auto` runs unconfined unless the user turns the sandbox on, which weakens §7's
"`--yolo` inside a sandbox" pairing. Mitigation, not a fix: choosing `/full-auto` prints one line
suggesting `/sandbox on`, once per session — a nudge, never a silent switch.

*Refusal text.* When the sandbox is on and cannot be established, the bash tool does not run and
says exactly what is missing and what to do:

    the sandbox could not be established: /usr/bin/sandbox-exec is not present.
    Commands will not run unconfined while the sandbox is on. To run them anyway: /sandbox off

*Bounded diagnostics.* No attempt is made to read Seatbelt's system log or Landlock audit. When a
sandboxed command exits non-zero and its output contains `Operation not permitted` or `Permission
denied`, one line is appended to the result:

    [sandbox: writes are confined to <root> and <temp>; network allowed. If this command
     legitimately needs more: /sandbox off]

One line, never a claim about cause. The model reads it and adapts, or the user changes the knob.

*Escape tests, red first.* Each must fail before the mechanism lands and pass after, natively on
macOS and Linux — CI already has both runners:

1. write outside the root — refused
2. write through a symlink created inside the root that points outside — refused
3. `../` traversal past the root — refused
4. write to `~/.ssh/` — refused even when it is inside a widened root
5. read kolk's credential store — refused
6. TCP connect to `127.0.0.1` with `network = deny` — refused (Seatbelt; Landlock ABI ≥ 4)
7. write under the temp dir — allowed
8. `go test ./...` on a fixture inside the root — allowed and passes

Windows skips each with the matrix as the stated reason, loudly, never silently.

*Claims flip in lockstep.* README "Known limitations", `site/capabilities.html` rows 491/495, the
sandbox cells on `site/compare/*.html`, and `site/llms.txt` line 46 all change **in the closing
leaf only**, in one commit, together with the `test-site.sh` pins that guard them.

**Alternatives rejected**

- **bubblewrap** — the most common Linux answer, and an install-time dependency. Every binary this
  repository ships is a single static file with nothing to install beside it; a sandbox that needs
  `apt install` first is a sandbox most users will not have.
- **seccomp-bpf alone** — filters syscalls, not paths. It cannot express "write here but not there".
- **chroot** — needs root, and is not a security boundary.
- **Containers** — post-v1 for `saga`, per the matrix. Not a per-command mechanism.
- **`sandbox = auto`** — a downgrade nobody sees. Rejected by name.
- **Enforcing network on Linux ABI < 4** via iptables or a network namespace — needs privileges the
  process does not have. Refusing is the honest answer.

**Risks**

- `sandbox-exec` is deprecated and could be removed. The mechanism sits behind the `Sandbox`
  interface; a replacement is a new file, not a redesign, and the absence check already fails
  closed.
- Landlock's allow-only reads make the denylist an enumeration, and enumerations drift. The test
  asserts each denylist path is unreadable, so drift fails the build rather than the user.
- Legitimate work sometimes needs to write outside the root. The refusal names the path and the
  knob — the same mitigation §1 already uses for the jail — and `tools.root` widens it explicitly.
- Overhead: the wrapper adds one exec in front of every command — `sandbox-exec` compiling a
  profile on macOS, kolk re-executing itself and installing a ruleset on Linux. Measured in
  V34.1e.5 (2026-09-05) as p50 of bare against sandboxed `true`: **5.5–6.7 ms on macOS, 2.1 ms on
  Linux** (ubuntu-latest). The pre-measurement expectation of 10–30 ms was pessimistic. The test
  holds the difference to the cold-start lines — 20 ms soft as a logged warning, 30 ms hard as a
  failure — and `check-budgets.sh` carries the number in the budgets log beside cold start, so a
  regression is a red gate, not a note.
- `exec_unix.go` is territory the open S10.1d2 touched. The change here is one wrapper at one call
  site, the cancel ladder is not touched, and the leaf rebases before it lands.

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
