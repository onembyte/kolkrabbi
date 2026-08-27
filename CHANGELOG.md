# Changelog

Notable changes to **kolkrabbi** (`kolk`). Generated from the commit history at each release tag;
`release:`, `chore:`, `docs:`, `test:` and `ci:` commits are omitted as noise.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases and prebuilt binaries: <https://github.com/onembyte/kolkrabbi/releases>

## [1.2.1] — 2026-08-27

### Added

- The octopus at icon size, and a frame with nothing in the way
- A session overview that can be polled
- A running session holds a lock, so liveness can be observed
- Show every provider, and dim the ones with no adapter

### Fixed

- The mock stopped offering a flag that was removed

## [1.2.0] — 2026-08-26

### Added

- A device token means less than the operator's
- Say how a served port can be reached, and by whom
- Pair a device with a short-lived code
- One revocable token per paired device
- /plan is read-only mode built from permission rules
- /diff shows what the session changed
- /undo takes back a turn's files and its conversation together
- Show context and session cost in the status line
- Complete @file mentions against the project
- Show the actual change before approving an edit
- Run independent orchestrated tasks concurrently
- Show what an orchestrated run costs, and let it be capped
- Route each task to a named model slot
- A run reports its failures instead of vanishing
- Give orchestrated tasks a kind and real dependencies
- Make "always" mean a rule the user can read
- Permission rules with scopes
- Describe binaries and page through large files
- Never ask a question a subagent cannot answer
- Scrub every tool result, and catch secrets with no vendor
- Confine tools to the project and replace yolo with three tiers
- Give a session a name worth reading
- Serve the local dashboard
- Keep the conversation a compaction replaced
- Give standing notes a place that is not a project file
- Resume the work done in this directory
- Search, rename, fork and export sessions
- Recover a turn refused for being too long
- Compact a filling session at the turn boundary
- Shrink a conversation without breaking it
- Measure how full a model's context window is
- Install a managed runtime only when its bytes are pinned
- Ask before pulling a local model
- Plan a local model before anything is downloaded
- Configure the managed local runtime through kolk config
- Report what this machine could run locally
- Measure free space and NVIDIA VRAM for the fit planner
- Read the hardware snapshot from platform metadata
- Plan whether a local model actually fits
- Keep a session's effort inside what its plan offers
- Confirm a subscription connector when it first answers
- Record cache tokens wherever a provider reports them
- Move the provider with the model on a live switch
- Let an enabled connector choose the session's provider
- Add managed sidecar process starter
- Define managed local runtime spec

### Fixed

- Refuse to serve a wildcard address without a token
- Elide the middle of a long path, not its filename
- Assert the resolved path on every platform, not just Linux
- Make the orchestrator slot reach the orchestrator
- Send engine warnings through the writer that owns the screen
- Stop tool output truncation from splitting runes
- Stop the test suite writing into real Kolkrabbi state
- Keep new commands from taking the keyboard or the turn
- One effort level is one row, and list recent sessions
- Let one bad line cost one line, not a history
- Stop claiming a login Kolkrabbi never saw
- Charge a Claude session turn for its own usage only
- Anchor the saga artifact to the project it belongs to
- Recover the provider stream after an interrupted turn
- Scope the persistent provider process to the session
- Keep provider logins out of the owned terminal

## [1.1.14] — 2026-08-26

### Fixed

- Match loading octopus pixel icon

## [1.1.13] — 2026-08-26

### Fixed

- Shrink loading octopus icon

## [1.1.12] — 2026-08-26

### Fixed

- Satisfy error string lint
- Keep fallback free catalog rankable

## [1.1.11] — 2026-08-26

### Added

- Enforce free model ranking contract
- Add backend lifecycle ownership
- Reuse Claude session in backend
- Add persistent Claude session

### Fixed

- Close backend with CLI session

## [1.1.10] — 2026-08-26

### Added

- Add persistent line process
- Retain Claude tool event metadata
- Implement Claude chat backend
- Add engine chat backend seam
- Adapt Claude results to provider shape
- Stream Claude provider events
- Translate Claude stream frames
- Define Claude CLI invocation
- Add live plan login picker
- Add provider plan login handoff
- Add provider login handoff
- Show plan model auth status
- Add plan model catalog
- Show connector status in plans
- Persist provider connector metadata
- Add provider plan search

## [1.1.9] — 2026-08-26

### Added

- Filter models while typing

## [1.1.8] — 2026-08-26

### Fixed

- Include embedded dashboard entrypoint

## [1.1.7] — 2026-08-26

### Added

- Filter model catalog from slash command
- Advance saga command and guardrails

## [1.1.5] — 2026-08-24

### Added

- Add persistent purple composer
- Render markdown and diffs

## [1.1.4] — 2026-08-24

### Added

- Add durable credential scanner

### Fixed

- Bound detached output capture

## [1.1.3] — 2026-08-24

### Fixed

- Default to verified free coding models

## [1.1.2] — 2026-08-24

### Added

- Ship persistent interactive runtime
- Separate persistent screen regions

## [1.1.1] — 2026-08-24

### Added

- Add bounded event journal

### Fixed

- Prevent octopus frame flooding

## [1.1.0] — 2026-08-23

### Added

- Detect installed versions
- Narrate self-update progress
- Animate provider activity
- Expose provider activity lifecycle
- Add verified update commands
- Replace executable atomically
- Verify release artifacts
- Define release discovery
- List models in session
- Prefix prompts with kolk
- Add explicit auto approve command
- Define permission decisions
- Add copyable install command

### Fixed

- Retry temporary rate limits
- Recover empty completions
- Clarify first-run sequence

### Changed

- Close owner-stable transport contract

## [0.1.0] — 2026-08-23

### Added

- Define permission requests
- Define tool outcomes
- Define tool output
- Define tool starts
- Define tool requests
- Define completed messages
- Add capabilities catalog
- Define turn lifecycle
- Define session lifecycle
- Restore public agent mode
- Define hello handshake
- Add streamed delta contracts
- Add versioned envelope contract
- Verify published artifacts
- Add verified macos and linux installer
- Limit v0.1 to chat and code
- Complete first-run credential flow
- Add secure key onboarding and landing site
- A key type that cannot be printed, and one that stopped leaking
- Durable replace for every file that must not tear
- One owner for process execution, with group teardown
- XDG directories, and a one-time move off the prototype layout
- Guard rails — layer table, budgets, buildinfo, kolk version

### Fixed

- Show the Apache-2.0 license
- Wrap both errors on a failed move; add lint to make check

### Changed

- Rule on L5-to-L2, approve the migration ratchet, mark step 4 done
- Mark migration step 3 done (build session, dfafa41)
- Split main.go into internal/cli, table-driven
- Harden item 5 -- auth, keys and secrets
- Add the zero-config north star, binding on every item
- Harden item 4 -- subscription backends (Claude Max via the vendor CLI)
- Harden item 3 -- the provider layer
- Adopt github.com/onembyte/kolkrabbi module path and target package names

