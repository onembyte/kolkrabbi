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

- [x] the exact install URL returns a reviewed script over HTTPS.
- [x] the script selects the correct signed/checksummed release for macOS or Linux and installs
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
- [x] **T0.4 release and installer** — the public release, signed assets, no-store website installer,
  and independent live verifier are green.
- [ ] **T0.5 clean-machine rehearsal** — the public cutover is complete; this still must prove install,
  first run, key addition, and first model response
  from a machine with no Go toolchain or prior Kolkrabbi files.
- [x] **R1.1 v1.1.0 installer-upgrade release** — publish the owner-requested three-part SemVer
  release, verify all signed assets and latest-version discovery, then exercise the public installer
  over an existing v0.1.0 installation.
- [x] **U0.1 explicit auto-approve command** — add a discoverable, session-only
  `/auto-approve [on|off]` control while preserving `-y` and `/yolo` compatibility.
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
- [~] **U0.4 persistent terminal UI** — add a Codex-style persistent multiline input area, live
  activity/tool status, visible model/mode/effort/session state, and robust terminal interaction.
- [x] **U0.4e spinner-only free-default patch** — remove loader decoration, dynamically prefer a
  free coding model, retire the stale documented free preset, and publish the verified `v1.1.3`.

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
- [ ] keyboard behavior, multiline/paste handling, narrow/Unicode layouts, Markdown/diff rendering,
  and approval focus have deterministic golden or model tests.
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

## Migration queue — one checkpoint group at a time

These are intentionally coarse until they become active; their detailed red/green checklist is
written only when the preceding group closes.

The original order put owner-trial checkpoints T0.1–T0.5 before A6. On 2026-08-23 the owner
explicitly postponed publishing and directed the remaining project work to continue first. T0.4d2
and T0.5 therefore stay blocked without being treated as failed, and the additive A6 migration may
proceed without changing repository visibility, tags, releases, or deployments.

- [~] **A6 protocol contract** — add `spec/`, public `protocol/`, and golden conformance tests.
- [~] **A7 event bus** — emit events while preserving today's plain output byte-for-byte.
- [ ] **A8 decision port** — move interactive approval out of the engine.
- [ ] **A9 engine ports** — inject stores/recorders/clock and isolate orchestration.
- [ ] **A10 session format cut** — freeze a v0 fixture before changing persisted messages.
- [ ] **A11 serve surfaces** — identical NDJSON, stdio, and SSE event frames.
- [ ] **A12 local dashboard store** — SQLite ingest and measured size/startup budget changes.
- [ ] **A13 Windows** — replace every honest stub and make Windows CI required.
- [ ] **A14 additive product leaves** — TUI, external agent adapters, and saga, separately.
- [ ] **A15 generated client proof** — nested tools module and TypeScript protocol client.
- [ ] **A16 platform clients** — desktop and mobile directories without root-module rewrites.

### A7 event bus — active detail

Delivery slices (only one active at a time):

- [x] **A7.1 bounded in-memory journal** — assign ordered envelopes, retain a bounded replay
  window, and fan out to bounded live subscribers without a goroutine.
- [ ] **A7.2 publish scrub chokepoint** — scrub every event string field without corrupting its
  typed payload, then prove shipped credential shapes cannot cross the journal boundary.
- [ ] **A7.3 durable event log** — spill exact NDJSON frames and replay one cursor across disk and
  memory before attaching live.
- [ ] **A7.4 byte-stable plain renderer** — move current engine formatting behind an event
  subscriber while retaining `Options.Out` and exact output bytes.
- [ ] **A7.5 engine event projection** — emit canonical lifecycle, content, tool, permission,
  accounting, and diagnostic events alongside the still-green plain renderer.
- [ ] **A7.6 stream-json surface** — expose the same retained envelopes through the one-shot CLI
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
- [ ] **A7.2b JSON string preservation** — scrub decoded JSON strings, including escaped forms,
  while retaining all untouched outer bytes and returning valid JSON.
- [ ] **A7.2c bus splice boundary** — scrub before retention/fan-out, validate the result, forbid
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
- [ ] JSON scrubbing catches plain and escaped canaries at every nesting position, retains numeric,
  boolean, null, whitespace, key order, and untouched string bytes exactly, and fails closed on
  malformed/non-object input.
- [ ] publishing any shipped canary yields only scrubbed replay/live/return copies; failed scrub or
  post-scrub protocol validation consumes no sequence and notifies no subscriber.
- [ ] focused/fuzz/race tests, import bans, architecture/purity/platform gates, and the full
  repository suite pass with red/green/refactor evidence recorded.

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
