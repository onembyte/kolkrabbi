# Product polish checklist

Updated 2026-09-24. Owner: Codex. Execution order and acceptance evidence live in
`CHECKPOINTS.md`; this checklist expands the owner's priorities into reviewable work.
Only the active leaf changes production code. A checked implementation is not a release claim.

## 1. Terminal identity and model controls — V43.1, verified

- [x] Reserve visible space for the selected model and effort.
- [x] Preserve worker state, model and effort when labels are long.
- [x] Handle narrow terminals, invalid picker indexes and control characters.
- [x] Preserve urgent pause/reset notices during footer compression.
- [x] Embed the supplied purple octopus at two columns by one row.
- [x] Detect supported inline-image terminals and provide an emoji fallback.
- [x] Cache image uploads and remove owned placements on Park/Close.
- [x] Pass focused tests, full gates and independent review.
- [ ] Final physical terminal rehearsal: supported image protocols, resize, exit and resume.

## 2. Readable work and prompt history — V43.1b, verified

- [x] Group commands, exploration and edits with bullets and indented results.
- [x] Attribute child work to stable agent identities.
- [x] Display actual edit counts, line numbers and bounded diff excerpts.
- [x] Use full-row green/red backgrounds and readable colourless fallbacks.
- [x] Retain warnings, failures, empty output and unfinished tool states.
- [x] Separate submitted multiline prompts with shading and spacing, including scrollback.
- [x] Sanitize untrusted terminal controls and avoid invented success or edit counts.
- [x] Pass focused tests, full gates and independent review.
- [ ] Final integrated rehearsal: simultaneous agents, long outputs and prompt visibility.

## 3. Task-specific models and effort — V43.2, verified

- [x] Treat the selected model as the capability ceiling.
- [x] Use current discovery and the exact signed-in provider identity.
- [x] Route hard work to the ceiling, routine work lower and mechanical work lowest.
- [x] Keep unknown/unavailable models out of guessed upgrades or provider switches.
- [x] Resolve offered effort once for requests, status and accounting.
- [x] Preserve exact provider effort spellings and safe opening-failure fallback.
- [x] Keep total task count separate from concurrency and optional spending limits.
- [x] Exercise large plans without dropping tasks.
- [x] Pass focused race tests, full gates and independent review.

## 4. Allowance pauses and durable continuation — V43.3a/b, verified

- [x] Retain pending input after reset time until a resume accepts it.
- [x] Rearm repeated pauses; prevent callback self-joins and join shutdown.
- [x] Deliver automatic resumes only to an available, started surface.
- [x] Return an accepted-but-canceled resume to its saved pause.
- [x] Persist original goal, plan, routes, dependencies and completed outcomes.
- [x] Retain child messages, remaining tool calls, guards, rounds and actual spend.
- [x] Drain in-flight operations and prevent new work after a child limit.
- [x] Keep and verify paused worktrees and child provider conversation handles.
- [x] Resume without replanning or repeating completed side effects.
- [x] Respect a lower spending limit after resume.
- [x] Archive old execution journals outside routine saves; include them in JSON exports.
- [x] Provide `/resume discard` without deleting work or history.
- [x] Distinguish settled allowance pauses from uncertain process interruptions.
- [x] Pass restart/concurrency regressions, full gates and independent review.

## 5. Context pressure and complete history — V43.3c, verified

### Retention before compaction

- [x] Save the full pre-compaction transcript durably before replacing working messages.
- [x] Preserve working messages when archival or compacted-session persistence fails.
- [x] Give concurrent children independent, immutable archive records.
- [x] Keep historical payloads out of repeated hot session saves.
- [x] Associate child archives with the correct task; clone archive references safely.
- [x] Include main and child compaction records in full JSON export.
- [x] Carry retained history through session fork and remove it on explicit session deletion.
- [x] Test restart, concurrent archive writes, failed writes and export completeness.

### Main and child working context

- [x] Reproduce and fix a single long turn that cannot compact.
- [x] Preserve system instructions, the original goal and recent user instructions.
- [x] Preserve assistant decisions and useful command/path/result evidence.
- [x] Compact only complete tool transactions; never orphan pending calls or answers.
- [x] Keep the newest transaction intact when older traffic can free sufficient room.
- [x] Label shortened outputs and retain their complete originals in archives.
- [x] Give the summarizer original facts, not already discarded tool evidence.
- [x] Retry overflow only after a measured reduction, even with stale window metadata.
- [x] Bound retries and avoid retrying an unchanged, oversized original prompt.
- [x] Compact at safe tool-round boundaries during long tasks.
- [x] Use each child's own model/backend context window.
- [x] Preserve task identity, rounds, guards, budget and pause state through compaction.
- [x] Keep provider-owned conversation handling honest about private context.

### Planning, dependencies and synthesis

- [x] Give relative follow-ups such as “implement that” the preceding conversation.
- [x] Bound dependency briefings while retaining original results in the journal.
- [x] Handle many/large results in synthesis without dropping task outcomes.
- [x] Recover synthesis overflow without running completed children again.
- [x] Keep shortened evidence explicit; never claim unseen work succeeded.

### Closure

- [x] Add smallest failing regressions and record the red results.
- [x] Run focused race tests and repository gates.
- [x] Obtain independent verification and resolve findings.
- [x] Update earlier turn-boundary, preserve-all-recent and archive-failure promises.
- [x] Record reproducible evidence and remaining physical/provider trial limits.

## 6. Managed native Localia — V43.4, verified with physical trial limits

### Project lifetime — V43.4a, verified

- [x] Inspect existing native runtime discovery/startup and project configuration.
- [x] Add project-scoped `local.ephemeral` on/off, default on.
- [x] On: stop only Kolk-owned runtime resources when the session closes.
- [x] Off: detach safely, record the endpoint and reuse it from later sessions.
- [x] Never stop an adopted user-managed server.
- [x] Handle concurrent sessions/projects and stale runtime records safely.
- [x] Keep pull, chat, model listing and signin on the same session endpoint.
- [x] Verify real parent-exit detachment, failed publication and late-discovery cleanup.
- [x] Pass lifetime/config focused race tests, repository gates and independent review.

### Native installation — V43.4b, verified with physical trial limits

- [x] Resolve current official runtime artifacts without a hardcoded release version.
- [x] Validate the exact release asset, HTTPS redirect origins, size and SHA-256 digest.
- [x] Bound compressed bytes, expanded data, members, path depth and individual files.
- [x] Extract gzip and Zstandard bundles using standard-library-only Go code.
- [x] Preserve and document the vendored Go decoder's source hashes and BSD license.
- [x] Reject traversal, sparse/special entries, unsafe links and hidden trailing archives.
- [x] Sync files and nested directories before publishing a completed installation.
- [x] Install into private user storage without Docker, sudo or shell-piped installers.
- [x] Serialize concurrent installation; cancel waiting/download/extraction safely.
- [x] Refuse symlink/hardlink/FIFO lock aliases before changing unrelated files.
- [x] Keep per-platform completion records and recover completed publication orphans.
- [x] Reuse installed runtimes offline and retain cached weights.
- [x] Show grouped setup/download/startup progress and actionable failures.
- [x] Wire explicit setup, approved pulls and local model selection to the session owner.
- [x] Open and resume local sessions without remote credentials or catalog requests.
- [x] Keep status and model browsing read-only, including before runtime installation.
- [x] List exact cached tags/custom namespaces; avoid assuming cloud stubs run locally.
- [x] Preserve local model rows during remote catalog errors.
- [x] Retain explicit remote endpoints after switching away from a local-only session.
- [x] Reject incomplete local model names before installation or selection changes.
- [x] Pass focused race tests and independent archive/installer/CLI adversarial review.
- [x] Validate one real official macOS archive in disposable storage without executing it.
  Ollama v0.34.4: verified download, extraction, offline reuse and cleanup passed under race.
- [x] Pass final repository gates after all fixes and documentation settle.
- [x] Record independent acceptance and supported/unperformed platform trials.

### Integrated Localia — V43.4c, verified

- [x] Review the complete setup, pull, selection, signin and session-exit flow.
  V43.4c.1: fit recheck, setup disclosure, Cloud pulls, idle-runtime sign-in; three verifier passes.
- [x] Check cancellation and concurrent startup across installation and runtime ownership.
  V43.4c.1: per-caller cancellation, one server per session, a footer that never waits on setup.
- [x] Disposition additional Linux GPU bundles against actual detection and official packaging.
  V43.4c.2: ROCm and JetPack 5/6 as install.sh does, MLX excluded; fixture-verified, four review passes.
- [x] Reconcile help, first-run guidance and platform support with implemented behavior.
  V43.4c.3: `/localia`, `/doctor`, picker, `/model` listing, pull question and docs/localia.md agree
  across states (running, kept, stalled, unserved GPU, remembered failure, Windows).
- [x] Verify the integrated flow independently and record remaining machine trial needs.
  V43.4c.3: three integrated verifier passes plus a delta re-check, CLEAN; machine trials listed
  in CHECKPOINTS.md under V43.4c.3, not claimed.

## 7. Final integrated review — V43.5, active

- [x] Review the six priorities against actual behavior and plan wording.
- [x] Exercise narrow/colourless output, prompt separation and work logs together.
- [x] Exercise mixed models/efforts, pause, resume and compaction in one fixture run.
- [x] Verify clean session exit, image cleanup and Localia ownership together.
- [x] Review error messages, slash help, configuration and export discoverability.
- [x] Run the final repository gates once after all material changes settle.
- [x] Record remaining external/physical trial needs without calling them verified
  (recorded below under "Trials still needed"; reviewed by a non-author 2026-09-26).
- [x] At the release that ships V43, update the landing page's first-run sample
  (`site/index.html` and its pins in `scripts/test-site.sh`) to the released text.

### Trials still needed — recorded 2026-09-26, none verified

Everything above was verified with fixtures, overlays, recorded vendor shapes and cross-builds on one
macOS/arm64 development machine. The only real artefacts used were:

- one downloaded and checksummed macOS Ollama archive, never executed;
- real `bash` runs that confirmed the Q1 bypass before its fix;
- a real `tree` 2.3.2 run that confirmed R1's accepted forms;
- headless xterm.js 5.5.0 cross-checks of the fixture VT model.

None of the following has been run for real, and none may be described as verified until it has.
This list includes §1's and §2's final rehearsals.

**Terminal (priorities 1–3)**

- [ ] Kitty, Ghostty, iTerm2 and WezTerm: the inline octopus renders at two cells without flicker. It
  is removed on `/exit`, on Ctrl+D, and around an in-session sign-in (Park/Resume). Resizing the
  window mid-turn leaves no stale octopus or frame.
- [ ] Zellij and tmux (with and without passthrough): the 🐙 text fallback is shown, never a stray
  image sequence.
- [ ] Cell widths in Apple Terminal and the VS Code terminal: CJK, emoji and box drawing keep every
  row inside the width.
- [ ] Shading against the owner's screenshot: prompt blocks, diff backgrounds and muted work rows in
  all three themes, at 16, 256 and truecolor.
- [ ] Narrowing: in iTerm2, narrow the window mid-session, then `/exit`. An inline renderer cannot
  erase what a reflowing terminal has already pushed into scrollback, so a stale partial frame may
  remain there (known, pre-existing); record how it looks.
- [ ] Pending wrap: in xterm and the VS Code terminal, a diff or prompt row exactly as wide as the
  terminal, followed by an erase. Its last cell may be dropped.
- [ ] Simultaneous agents with long outputs: a real agent-mode run with three workers.
  - The worker rows keep model, effort and state at 44+ columns.
  - Each agent's work log is readable; its compaction notices appear (Z1).
  - The prompt stays visible.
  - After `/exit` the whole conversation is in scrollback (X1), with record colours intact (Y1, Y3).
- [ ] Automatic resume on a real terminal after a real limit resets: the "is back; continuing" line,
  and the waiting turn delivered once.

**Routing (priority 4)**

- [ ] Discovery against signed-in Claude and Codex accounts: the real menus, the rungs the planner
  spawns, and the effort spellings each vendor accepts. The worker rows show the spelling that was
  sent.
- [ ] A mixed-model run across real vendors.
  - Tasks never route to another vendor's model.
  - A slot naming another vendor's model outside the menu is announced and replaced by the task
    level's rung (E3).
  - Nothing runs above the selected model.
- [ ] A real planner's large plan that hits its output limit: the reply is detected from its shape
  alone as "may have been cut off" (E2). Check which line the user sees, and that the request then
  runs directly.
- [ ] Billed runs of the routed and compacted paths (V43.2, V43.3), none made so far.
  - Spend against `max_run_cost_usd`, and the per-run cost lines.
  - A real compaction at the real model's window, with its archive present in `--json` export.

**Continuity (priority 5)**

- [ ] A real mid-plan usage limit, and resume through the vendors' own conversation handles.
  - The plan pauses before its next call, and queued tasks do not start.
  - On resume, completed tasks are not re-run.
  - Each child resumes its own vendor conversation: it knows its earlier turns.
- [ ] A vendor handle that has expired: resume refuses and retains uncertain work, rather than
  replaying the accepted request in a fresh conversation. Explicit discard remains available.
- [ ] Real 429 shapes:
  - OpenAI's "Request too large … tokens per min" (EO1: classified today as a capacity pause,
    though waiting cannot help);
  - Ollama Cloud's usage limit: a pause that says "retry at" (an estimate); auto-resume re-checks
    without switching to a paid fallback;
  - Claude's plan limit: the pause takes the vendor's reset (§8), says "reset at", with the day
    when it is not today, and auto-resume waits once for that time, including across the
    machine sleeping, instead of re-checking every 15 minutes. An HTTP 429 carrying
    `Retry-After` pauses with "reset at" that time.
- [ ] A real disk that fails at a pause, recovers, and fails again: every failed pause save is
  reported (E1).
- [ ] Killing kolk mid-tool, then starting it again.
  - The saved request is not resumed automatically ("saved work was interrupted outside a completed
    pause…").
  - `/resume discard` keeps history, files and worktrees.
  - No completed action is repeated.
- [ ] A Localia runtime that dies mid-plan: the transport pause, then resume against the restarted
  real runtime.
- [ ] A real Claude or Codex child's work log: grouping, paths, and counts and diffs only where the
  vendor reports them (V43.1b); failed or declined items render as failed (C3).
- [ ] Vendor event shapes seen only in fixtures, or not at all. Today's rendering is wrong for each:
  - a Claude `tool_use_result.interrupted:true` with `is_error:false` reads as completed today; it
    should read unfinished or failed;
  - a Codex `item.completed` with an unknown status such as `cancelled` reads as completed today;
  - a Copilot `success:true` with no `result` object reads as failed today, the reverse over-claim.

**Localia (priority 6)**

- [ ] The NVIDIA GTX 1660 SUPER (TrueNAS): fit plan, pull and chat. The VRAM shown and the
  recommended quantization match the card, and the GPU is actually used.
- [ ] Linux amd64 (the TrueNAS box) and Raspberry Pi 5 arm64: first run, setup, pull and chat.
- [ ] A physical AMD card: the ROCm companion downloads (with its size disclosed before the yes) and
  the GPU is used.
- [ ] A machine with both an AMD and an NVIDIA card: ROCm is skipped while the NVIDIA driver is
  loaded.
- [ ] `nvidia-smi` installed while the NVIDIA driver is not loaded: the one remaining divergence
  from `install.sh`. Record what setup does.
- [ ] A Jetson on JetPack 6 (L4T R36) and JetPack 5 (L4T R35): the matching companion downloads and
  the GPU is used. An older L4T such as R32 (JetPack 4) gets the unsupported-JetPack note and no
  companion.
- [ ] Windows: managed setup is refused with the platform advice, and an existing Ollama is adopted.
- [ ] macOS below 14, and other edge platforms: the unsupported-platform advice.
- [ ] A real sign-in and a real Cloud `/api/show`. Signed out, `/localia pull --yes <tag>-cloud`
  gives the sign-in advice (the 401 mapping is untested against a real server).
- [ ] Persistent detach and reuse.
  - With `local.ephemeral off`, exit and reopen: the same runtime (same address) is reused, and
    `/localia` says "stays running for this project after session close".
  - Then set it back to `on` and reopen: `/localia` says "kept running from an earlier
    `local.ephemeral off`; this session reuses it and leaves it running", and the runtime is
    still running after `/exit`.
- [ ] A blocked network or firewall.
  - A firewall that drops or refuses connections, or blocks DNS: setup fails with "…; check the
    network and retry, or install Ollama yourself and Kolk will use it", not the disk-space
    advice.
  - A proxy that answers with an HTTP error: "official runtime download returned HTTP N; retry
    setup later", with the generic install-it-yourself advice.
- [ ] Interrupted real downloads.
  - A cancelled or dropped download removes its `.install-*` staging directory, and the next
    attempt downloads again.
  - A crash during publication of `current.json` recovers the completed tree without downloading
    it again. The release lookup still has to be online.
  - A killed process cannot run that cleanup. Its `.install-*` directory goes on the next runtime
    start (§8), whether or not a setup follows, and never while another setup holds
    `install.lock`; a setup sweeps under the lock before its room check. Kill a real download
    mid-way, and confirm that the next start frees the space.
- [ ] A downloaded vendor runtime actually running on a GPU. Only the macOS archive has been
  downloaded and verified; no downloaded executable has run.
- [ ] `/update` or a mid-session sign-in restart with a runtime this session started: the runtime
  really stops before the process is replaced (W1).

**Known, low, not in §8** (found during §8 item 1, pre-existing since before V43): `applyRules`
rebuilds `ag.Rules` from scratch, so /plan, /plan off and /permissions edits drop a rule kept at a
prompt ("kept for this session", `keepRule`). It errs safe: the next such action asks again.

**For the owner, found during §8 item 3a (not built):** `local.gpu_mode cpu` has never forced
Ollama off a GPU it can already use: CUDA in the standard NVIDIA bundle, or a ROCm tree already
installed. It is the user's acceptance of CPU. Kolk plans for CPU, fetches no GPU bundle and (with
§8 item 3b) stops warning. Enforcing it needs per-backend device hiding (`CUDA_VISIBLE_DEVICES`,
`HIP_VISIBLE_DEVICES`/`ROCR_VISIBLE_DEVICES`, and nothing equivalent for Metal), which is
unverifiable here without the hardware.

**Owner decisions, not trials** (decided 2026-09-28; the work is §8)

- F1: stream-json cannot report a failed save at a pause without a new protocol log code.
- F2: whether an ephemeral session should ever stop a kept runtime (as built, it never does).
- ROCm: download automatically on AMD, as `install.sh` does and as built, or make it a separate
  opt-in.
- For Codex's V43.2 owner: the unreachable `modelForKind` path and the vacuous gateway test.
- Low, same class as EO2: a Claude plan limit names the vendor's reset time (`LimitResets`), but
  it never reaches the pause, which falls back to kolk's 15-minute estimate.

## 8. Owner decisions — decided 2026-09-28, active

The owner's answers to §7's open decisions. Every item follows AGENTS.md: a red test first, the fix,
overlay mutants, focused race tests and `make check`. It is [x] only after a non-author verifier
returns CLEAN. Evidence is in CHECKPOINTS.md.

- [x] Vendor reset times reach the pause (was the EO2-class note). Closed 2026-09-28 after eight
  review rounds (CHECKPOINTS.md, "V43.6 owner decisions"); the rounds also made /new a safe session
  swap and plan mode explicit state. A Claude plan limit's
  `LimitResets`, and Ollama Cloud's reset when its message names one, become the pause's real
  `ResetAt`. The pause says "reset at", auto-resume waits for that time instead of re-checking
  every 15 minutes, and the session is fully recoverable after the limit.
- [x] Stale Localia staging is swept. Under `install.lock`, before the room check, setup removes
  `.install-*` directories a killed process left behind, so their space counts again. Closed
  2026-09-28 after three review rounds: every runtime start also sweeps, without waiting on a
  setup in progress, and in stream-json the local runtime's words go to stderr.
- ROCm stays automatic, and CPU is a choice the user makes. Two leaves:
  - [x] (a) `local.gpu_mode cpu` skips every GPU companion (ROCm, JetPack): nothing is
    downloaded, nothing is reported missing, and no GPU note is shown. It is read by every
    installer the CLI builds, discovery included. Closed 2026-09-28 after round 3 and
    independent CLEAN verification: CPU/auto changes preserve the running tree's facts
    and failure history even when another setup replaces the current installation.
  - [x] (b) When the automatic choice leaves local models on CPU, or otherwise worse than the
    machine allows, a warning stays at the bottom of the TUI until the user explicitly selects
    CPU in settings. That choice (`local.gpu_mode cpu`) is persisted in config for good.
    Closed 2026-09-29 after independent CLEAN verification. Known catalog models use the fit
    estimate; unknown-size custom models warn that CPU placement is possible when a usable GPU
    exists, because actual Ollama placement is not measured.
- [x] Kept runtimes stop only when asked (F2: option C). Closed 2026-09-29
  after independent CLEAN verification; physical Ollama stop remains a §7 trial.
  - No session stops a kept runtime implicitly, as built.
  - `/localia stop` stops one. It first confirms the process is Kolk's managed Ollama at the
    recorded address, and never signals a process ID it cannot vouch for.
  - Setting `local.ephemeral` back to `on` while a verified kept runtime runs says how to stop it.
- [x] `modelForKind` is kept, and `TestAGatewaySessionRoutesExactlyAsItDidBefore` becomes
  meaningful: it fails if the gateway path stops running or routes differently (Codex's V43.2
  code; the decision is recorded in `docs/v43-5-codex-review.md`). Closed 2026-09-29 after
  a real planner, two-child and synthesis request regression and independent CLEAN verification.
- [x] A complete resume snapshot (F1, widened by the owner). Nothing done before a pause, limit
  or error is lost or redone.
  - [x] Explore the options and record the design before building. The chosen sidecar,
    boundaries, recovery rule and ordered leaves are in `docs/v43-resume-snapshot.md`.
    Closed 2026-09-29 after independent CLEAN review and green gates.
  - [x] Store: atomic compressed recovery file, revision/load precedence, archive integrity,
    old-session compatibility, and list/export/delete. Closed 2026-09-29 after independent
    CLEAN verification, including corrupt candidates, archive closure, symlink safety and forks.
  - [x] Capture: quiescent pause/limit/error writes, explicit task state, partial output and
    vendor handles; failed writes cannot claim durable recovery. Closed 2026-09-30 with
    independent CLEAN verification of the original fixes and Claude's later F1 failed-write
    hold/F2 cancel probes. Follow-up: 6/6 valid mutants killed, focused race passed,
    `make check` green (4,917 tests); evidence in CHECKPOINTS.md.
  - [x] Resume: settled results, pending native calls, vendor conversations, worktrees and
    background children continue at proven boundaries without completed work rerunning.
    - [x] Native recovery: explicit durable-boundary provenance, manual error/limit resume,
      pending native calls and completed replies, and durable retirement before more work.
      Closed 2026-09-30 after independent CLEAN verification, 16/16 author mutants killed,
      focused race and `make check` green (4,936 tests); evidence in CHECKPOINTS.md.
    - [x] Vendor recovery: adapter acceptance/continuation proof, handle/model ownership,
      hard-exit refusal and no replay of an accepted unfinished prompt.
      Closed October 7 after independent CLEAN and the final shared gate (5,158
      tests). See CHECKPOINTS.md's October evidence.
    - [x] Restart validation: full task/dependency/worktree/archive/binding matrix, settled
      results and background children, old snapshots, synthesis and refusal preservation.
    - [x] In-turn fallback safety. Found by the second Vendor recovery verifier; it predates
      V43. `routing.on_subscription_limit switch` (or ask answered yes) and free rotation send
      a vendor turn that already acted to another model within the same turn, so that model
      redoes what the vendor did. Apply the contract v3 rule: a turn may move only if it was
      never delivered, or closed with no vendor tool reported. Also pin that a child's limit
      never switches (the pause gate at retry.go:174 has no test).
  - [x] Surfaces: save failure on plain/TUI/stream-json and pure NDJSON from startup.
    Implemented by Codex under Claude's parallel task split, 2026-09-30; scoped
    independent CLEAN, 13 author/7 verifier mutants killed. Closed October 7
    with the current shared full gate. September peer import/format blockers
    were resolved before the October baseline; evidence is in CHECKPOINTS.md.
  - [x] Walk-back: corruption, race and restart matrix, docs and gates.
    October closeout retains physical trials and long-term optimizations as
    unverified/open, corrects stale full-replay/tap/effort guidance and reruns
    distribution/doc gates independently.
  - A compressed snapshot is written at every pause, limit or error. It holds the conversation,
    the plan and each task's state, completed results, each child's vendor handle and partial
    output, and background tasks.
  - A resume continues exactly where the run stopped, with no completed work repeated.
  - A failed save is visible on every surface, stream-json included.
  - stream-json's stdout stays pure NDJSON. Found during item 2 and dating from aac7994:
    `--mode agent` prints the "agent lane: …" report (`reportAgentLane`) there first.
- [~] Release, once every item above is [x] and every gate is green.
  - Commit, push, cut the next version, and publish the release.
  - Update the Homebrew formula and the curl installer, and verify each end to end.
  - Update the landing page (§7's release-time item) and the site.
  - Leave the tree clean.
