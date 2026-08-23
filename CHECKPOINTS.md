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
- [x] **R0.1 v0.1 two-mode surface** — ship only chat and code, with code as the default; keep the
  experimental orchestrator unreachable until the later agentic phase.
- [x] **R0.2 agentic surface restoration** — at the owner's direction, expose the preserved
  sequential agent orchestrator as the third mode while keeping code as the default.
- [!] **T0.4 release and installer** — local implementation is complete; the public cutover is
  postponed at the owner's request while the remaining project is built.
- [!] **T0.5 clean-machine rehearsal** — waits for the postponed public cutover, then proves install,
  first run, key addition, and first model response
  from a machine with no Go toolchain or prior Kolkrabbi files.
- [x] **U0.1 explicit auto-approve command** — add a discoverable, session-only
  `/auto-approve [on|off]` control while preserving `-y` and `/yolo` compatibility.
- [x] **U0.1b mode-prefixed prompt** — identify Kolkrabbi and the active mode as `kolk-<mode>`
  at every interactive prompt.
- [ ] **U0.1c in-session model catalog** — make bare `/model` show the current selection and the
  available provider models while preserving `/model <id>` as the direct switch.
- [ ] **U0.2 verified self-update** — add `kolk update` and `/update`, resolving the latest GitHub
  Release with platform checks, checksum verification, and atomic binary replacement.
- [ ] **U0.3 loading octopus** — show a TTY-only animated octopus while Kolkrabbi is waiting,
  without corrupting streamed replies, redirected output, or cancellation.

### U0.1 explicit auto-approve command — active detail

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

### U0.2 verified self-update — planned detail

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

- [ ] stable versions compare numerically; dev/malformed versions and unsupported targets fail
  before network or filesystem mutation, and a same/newer build downloads no archive.
- [ ] latest discovery accepts only the exact repository's `releases/tag/v<stable-semver>` result.
- [ ] manifest and archive downloads are status/size bounded; the manifest must contain one unique
  lowercase SHA-256 for the exact target archive, and digest mismatch fails closed.
- [ ] archive validation accepts exactly regular `kolk`, `README.md`, and `LICENSE` members, rejects
  links, extra/missing/duplicate paths and empty binaries, and never writes before all checks pass.
- [ ] successful replacement is atomic, mode `0755`, and reports old/new versions and path; every
  download, validation, cancellation, or write failure preserves the previous executable.
- [ ] `kolk update` is in top-level help, rejects arguments, runs without a key, and reports updated
  or already-current state.
- [ ] `/update` is in session help, rejects arguments without making a request, uses the same updater,
  continues the session after errors, and tells the user to restart after a replacement.
- [ ] focused updater/CLI tests, race-sensitive tests where applicable, architecture and full
  repository gates pass; the build log records red/green/refactor.

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
- [ ] **T0.4d2 public cutover** — explicit owner approval for the artifact host, `v0.1.0`, live
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

## Migration queue — one checkpoint group at a time

These are intentionally coarse until they become active; their detailed red/green checklist is
written only when the preceding group closes.

The original order put owner-trial checkpoints T0.1–T0.5 before A6. On 2026-08-23 the owner
explicitly postponed publishing and directed the remaining project work to continue first. T0.4d2
and T0.5 therefore stay blocked without being treated as failed, and the additive A6 migration may
proceed without changing repository visibility, tags, releases, or deployments.

- [~] **A6 protocol contract** — add `spec/`, public `protocol/`, and golden conformance tests.
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

### A6 protocol contract — active detail

Delivery slices:

- [x] **A6.1 envelope foundation** — protocol version 0, the single language-neutral envelope
  schema, a golden frame, and its stdlib-only public Go binding.
- [~] **A6.2 event vocabulary** — event-name constants, typed event payloads, and one golden frame
  per shipped event without connecting the engine yet.
- [ ] **A6.3 commands, entities, and errors** — client commands, shared entities, stable error
  mapping, and their conformance fixtures.
- [ ] **A6.4 transport contract closure** — NDJSON/SSE framing rules, stream fixtures, OpenAPI
  shape, spec-change CI guard, and the complete A6 gate.

A6.2 is intentionally delivered as independently reviewable vocabulary slices:

- [x] **A6.2a streamed deltas** — `message.delta` and `reasoning.delta`, whose required `text`
  payload is already explicit in the architecture and provider contracts.
- [x] **A6.2b lifecycle and completed content** — handshake, session, turn, and
  `message.completed` events after their payload fields are fixed.
- [x] **A6.2c tools and decisions** — tool and permission events.
- [ ] **A6.2d orchestration and operations** — subagent, chapter, checkpoint, accounting, score,
  error, and log events, followed by a complete closed-vocabulary check.

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
