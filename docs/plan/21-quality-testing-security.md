# 21. Quality, testing & security

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 21

## Decision (the short version)

The plan's `**Today:**` line for this item reads "22 offline tests". That was true when it was
written. There are now **2,078**, across 170 test files, and the interesting part of this item is no
longer "write more tests" — it is deciding which *kinds* of test earn their keep, and writing down
the security properties that are currently true only because nobody has broken them yet.

Four things are settled here:

1. **The pyramid has a shape, and two of its layers are refused.** Golden output tests for the TUI
   and property tests for the edit tool are both declined, with reasons.
2. **The error UX matrix is built**, not merely specified — a table in a document that nothing
   executes is a wish. It lives in `internal/provider/advice.go` and prints at all three places a
   turn can fail.
3. **Observability for us is one command and one flag**, and both are queued rather than pretended:
   `kolk doctor` and `--debug`.
4. **The security checklist is written against what the code does**, with each line naming the test
   or ratchet that holds it — and the two lines that have no such test say so.

## Spec

### 1. The test pyramid

| Layer | State | Notes |
| --- | --- | --- |
| Unit | 2,078 tests, 170 files | the base, and it is broad rather than deep by design: every package tests its own decisions. |
| End-to-end over HTTP | built | 38 sites stand up an `httptest` server and drive the real client through it — streaming, fragmentation, tool calls, errors. |
| Fuzz | 3 targets | `redact.Scrub` (UTF-8 preservation and idempotence), `redact.ScrubJSON`, `xid` round-tripping. |
| Architecture ratchets | 8 scripts in `make check` | layering, purity, build tags, platforms, budgets, the mode surface, the spec guard, the release and workflow contracts. These are tests that fail when a *property* regresses, which no example-based test can do. |
| Live | weekly, opt-in | item 20's smoke workflow. The only test allowed to cost money. |

**Fuzzing the SSE and tool-argument parsers: accepted, queued as L21.3.** These are the two places
where bytes from a third party become control flow, which is exactly what fuzzing is for. The SSE
reader already survives fragmentation tests written by hand; those tests encode the fragmentations
someone thought of.

**Golden output tests for the TUI: refused.** A golden file for a terminal frame asserts every pixel
of a layout that is still moving, so it fails on every deliberate change and teaches the reviewer to
regenerate it without reading — which is worse than no test, because it looks like coverage. The TUI
is instead tested on the properties that actually matter and do not move: the activity row never
enters the transcript, sanitisation preserves runes, the overlay does not flatten the diff (a bug
found precisely because three tests were asserting on a flattened row and were vacuously green).
Revisit if the layout ever stops changing.

**Property tests for the edit tool: refused as stated, replaced with something narrower.** "Property
test the edit tool" has no useful invariant behind it — the honest property is "applying an edit does
what the edit says", which is the implementation restated. The properties worth asserting are
specific and already are: an edit that does not match changes nothing, a rune is never split, a
path outside the project root is refused before the file is opened, and a write is atomic. Generative
testing adds nothing to any of those.

### 2. The error UX matrix

Built in this item as `provider.Advise(err) (Advice, bool)`. Every entry gives a summary and a next
action, and the tests assert both — including that the summary fits a terminal line and does not end
in a full stop, because these are lines read by someone who is already annoyed.

| Failure | What we say | What we tell them to do |
| --- | --- | --- |
| 401 | the key was rejected | `kolk key <API_KEY>`, or check `OPENROUTER_API_KEY` |
| 402 | out of credit for a paid model | add credit, or a `:free` id from `kolk models` |
| 403 | the provider refused outright | usually region or content policy, not a kolk setting |
| 404 | no model under that id | `kolk models` — ids change |
| 408 / 504 | timed out before answering | retry; another model is likely faster to first token |
| 429 | rate-limited | kolk rotates free models itself; `Retry-After` is quoted when given |
| 5xx | the provider's problem, not yours | retry, or route around it |
| 400 + overflow phrase | the conversation outgrew the window | kolk compacts and retries once; then `/compact` or a wider model |
| 400 + tool phrase | this model cannot call tools | a tool-capable model, or chat mode |
| network drop mid-stream | the connection died part-way | what arrived is kept; ask again |
| anything else | *nothing* | `Advise` returns false rather than volunteering a vague line |

Two design points are load-bearing. **Advice never displaces a command's own guidance:** a
`GuidedError` prints its own hints and no status-code advice, because the command knows more than a
table does. And **advice prints at all three sites a turn can fail** — the one-shot command, the
plain REPL and the TUI — via a single `writeAdvice`, with a test that fails if a fourth site appears
without it or one of the three loses it. The interactive paths are where a person actually meets a
401.

Building it moved `IsContextOverflow` from the engine down to `internal/provider`, where the phrase
list now has two callers instead of one copy each.

### 3. Observability for us

**`kolk doctor` — accepted, queued as L21.1.** Keys (present, shape, which store), network (can it
reach OpenRouter), model access (does the configured model answer), terminal capabilities (colour,
width, TTY), and the writable directories. The rule it must follow: it prints what it found, never
what it found *with* — no key material, masked or otherwise, beyond the last four characters that
`kolk key` already shows.

**`--debug` log file with redaction — accepted, queued as L21.2.** Off unless asked for, one file
per session under the state directory, every line through `redact.Scrub` before it is written, and
the path printed at the end so a bug report can attach it. The redaction is not optional and not a
flag: a debug log is the single most likely place for a key to end up in a public issue.

Both are queued rather than built because this item's hardening bar is the plan and the checklist,
and both are ordinary work once the shape is settled. Neither is referenced by anything shipping —
`Advise` was written to name `--base-url`, not `kolk doctor`, so nothing promises a command that
does not exist.

### 4. Security checklist

Each line names what holds it. Two lines name nothing, and say so.

| Property | Held by |
| --- | --- |
| Secrets never reach the terminal, a log, or a session file | one scrubbing chokepoint over every tool result (E13.3), `secret.Scrub` on every provider error before it is constructed, and `redact` with a fuzz target for UTF-8 preservation and idempotence |
| Secrets never reach the model | tool output is scrubbed before it enters the transcript, not when it is displayed |
| Keys at rest | a 0600 manifest written atomically under a lock, never the config JSON — a key left there by the prototype is migrated out, manifest first so a crash never destroys the only copy — and `redact.Mask` keeps a shape prefix and the last four characters, nothing more |
| Path traversal | every file tool resolves against the project root **with symlinks resolved first**, and reaching outside asks; in `/full-auto` it proceeds and is logged with the reason (E13.1) |
| Command injection via tool args | commands are never assembled by string interpolation — `shellQuote` for the saga's git calls, and `bash` invocations are a confirmed surface, not a hidden one |
| Permission floor | `hardline` refusals cannot be lifted by any tier, rule, or subagent; the subagent path auto-denies rather than prompting (E13.4) |
| Remote surface | loopback-only by default, a wildcard bind refused before the socket opens, every route token-gated except two that say nothing, and widening that set fails a test (I26.1–I26.6) |
| Supply chain — dependencies | two third-party modules, enforced by a budget that fails the build above two, and a layer table where every allowance must be imported by the package it names *(the `tools/` claim this line first made was inherited from item 2 and untrue — see item 19)* |
| Supply chain — CI actions | the release and smoke workflows pin every action by digest; **the ci.yml jobs still float on `@v5`/`@v6`** |
| Release integrity | keyless Cosign over `checksums.txt`, verified from outside by `scripts/verify-release.sh` after every publish; the fast install paths verify SHA-256 only (item 20 is explicit about what that does and does not prove) |
| Prompt injection from files and web | **nothing** |

**The two gaps, stated plainly.**

*Floating action pins in `ci.yml`.* The two workflows that matter most — the one that publishes
releases and the one that holds a live API key — pin by digest. The ordinary CI jobs do not, so a
compromised `actions/checkout@v5` would run in a job that can read the repository and nothing else.
That is a real but bounded exposure, and pinning it is L21.4.

*Prompt injection.* A file kolk reads, or a page fetched into context, can contain text addressed to
the model. Nothing in the codebase detects or defuses that, and this document is not going to claim
otherwise. What limits the damage is not detection but the permission model: an injected instruction
still has to get a tool call past the floor, and every write outside the project root, every command,
and every network fetch asks. `/full-auto` is where this stops being true, which is why `/full-auto`
logs what it is reaching for rather than doing it silently. A real answer — provenance tracking, so the model
is told which parts of its context came from somewhere untrusted — is not on the plan today. It
would be a new item, and it should be written as one before anyone claims kolk resists injection.

## Build leaves

- **L21.0 the error matrix** — `provider.Advise`, wired at all three failure sites, with the
  overflow detector moved down a layer to be shared. *Built.*
- **L21.1 `kolk doctor`** — keys, network, model access, terminal capabilities, directories.
- **L21.2 `--debug`** — a per-session log file, scrubbed on the way in, path printed at the end.
- **L21.3 fuzz the SSE reader and tool-argument decoding** — the two places third-party bytes become
  control flow.
- **L21.4 pin ci.yml's actions by digest** — the release and smoke workflows already do.

## Open questions

- Provenance tracking for untrusted context, if prompt injection is ever to be more than a
  documented gap.
