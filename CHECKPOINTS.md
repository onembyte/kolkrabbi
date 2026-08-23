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

A checkpoint is not complete merely because production code compiles. All seven gates must close.
Unrelated dirty files are user-owned and remain untouched.

Status: `[ ]` queued · `[~]` active · `[x]` verified · `[!]` blocked

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

- [ ] the exact install URL returns a reviewed script over HTTPS.
- [ ] the script selects the correct signed/checksummed release for macOS or Linux and installs
  `kolk` on `PATH` without requiring Go or another runtime.
- [ ] `kolk` from a clean shell launches the installed binary.
- [x] the domain root serves the reviewed purple retro-octopus landing page.
- [x] first launch without a key shows the short guidance above, never a stack trace or config-file
  instruction.
- [x] `kolk key <API_KEY>` infers the supported provider, stores the key with safe permissions, and
  never echoes the full value.
- [x] the next `kolk` starts a working model session with computed defaults.
- [ ] a clean-machine smoke test proves the entire flow end to end.

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
- [ ] **T0.4 release and installer** — tagged macOS/Linux artifacts, checksums, install script, and
  the exact public URL.
- [ ] **T0.5 clean-machine rehearsal** — install, first run, key addition, and first model response
  from a machine with no Go toolchain or prior Kolkrabbi files.

### T0.3 first-run path — active detail

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

## Migration queue — one checkpoint group at a time

These are intentionally coarse until they become active; their detailed red/green checklist is
written only when the preceding group closes.

The owner-trial checkpoints T0.1–T0.5 run after L0.8 and before A6. They deliberately ship a safe,
installable version of the working prototype before the protocol migration resumes.

- [ ] **A6 protocol contract** — add `spec/`, public `protocol/`, and golden conformance tests.
- [ ] **A7 event bus** — emit events while preserving today's plain output byte-for-byte.
- [ ] **A8 decision port** — move interactive approval out of the engine.
- [ ] **A9 engine ports** — inject stores/recorders/clock and isolate orchestration.
- [ ] **A10 session format cut** — freeze a v0 fixture before changing persisted messages.
- [ ] **A11 serve surfaces** — identical NDJSON, stdio, and SSE event frames.
- [ ] **A12 local dashboard store** — SQLite ingest and measured size/startup budget changes.
- [ ] **A13 Windows** — replace every honest stub and make Windows CI required.
- [ ] **A14 additive product leaves** — TUI, external agent adapters, and saga, separately.
- [ ] **A15 generated client proof** — nested tools module and TypeScript protocol client.
- [ ] **A16 platform clients** — desktop and mobile directories without root-module rewrites.

## Product decision queue

Each item is hardened independently in its own `docs/plan/NN-*.md` before its implementation
checkpoint is expanded. Status here mirrors [`PLAN.md`](PLAN.md); PLAN remains authoritative.

- [~] 1 identity, repository, and release skeleton
- [x] 2 language and architecture
- [x] 3 provider layer
- [x] 4 subscription backends
- [x] 5 authentication, keys, and secrets
- [ ] 6 modes (draft exists; review and PLAN update still required)
- [ ] 7 effort dial
- [ ] 8 model selection and routing
- [ ] 9 command surface
- [ ] 10 saga
- [ ] 11 REPL/TUI
- [ ] 12 sessions, context, and memory
- [ ] 13 tools, permissions, and sandboxing
- [ ] 14 orchestration and per-task routing
- [ ] 15 code-mode specifics
- [ ] 16 extensibility
- [ ] 17 local dashboard
- [ ] 18 config system (draft exists; review and PLAN update still required)
- [ ] 19 desktop and iPad path
- [ ] 20 distribution, updates, and CI
- [ ] 21 quality, testing, and security
- [ ] 22 onboarding and docs
- [ ] 23 roadmap, phasing, and non-goals
