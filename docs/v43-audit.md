# Product polish audit — 2026-09-14

Implementation and acceptance are tracked in CHECKPOINTS.md under V43. This is a record of
demonstrated gaps, not a claim that the forward queue has shipped.

## Terminal

- Long picker descriptions and session titles hide the active model/effort. V43.1 reserves
  their space first. A long worker summary had the same problem.
- The requested octopus must occupy one row and two cells. The owner's final supplied purple
  artwork is embedded as a tiny transparent PNG; unsupported terminals retain the emoji fallback.
- Independent review found that footer compression could discard the pause/reset notice and
  that the expanded effort dial accepted raw terminal controls. Both have failing regressions
  and fixes in V43.1.
- Work records already exist, but native tool output is mostly hidden from the interactive
  transcript and provider trails are compressed to one line. V43.1b adds grouped, attributed
  actions, results, explanations and coloured signed edits, per the owner's follow-up.

## Model and effort routing — V43.2

Closed 2026-09-15 with independent CLEAN review and full repository gates. The gaps below
have regressions and fixes. Discovery now supplies the authoritative menu and provider binding;
per-task effort is resolved before launch. Total queue length is intentionally uncapped, per
the owner's request; concurrency and the optional run-cost setting remain separate controls.

- The task effort reaches child setup and stats, but the API request context still inherits
  the parent's effort. Test the provider-observed context, not only the backend factory arguments.
- Routine work currently binds to the ceiling, like hard work. A discovered, available lower
  rung should handle routine work when possible.
- Plan task count is coupled to effort and silently truncated, independently of the parallel
  execution limit. Separate plan size from simultaneous execution so necessary tasks survive.
- Model discovery is live; capability ranking is still based on a partial family ladder.
  Unknown capability must not be guessed into an upgrade or billing change.

## Continuation — V43.3

Independent offline engine probes demonstrated four gaps:

1. A synthesis allowance pause reruns already completed children on resume.
2. A child allowance error is treated as task failure and scheduling continues.
3. An expired reset timestamp clears a manual pause and drops its pending input.
4. When synchronous automatic resumption reaches another limit, the old monitor prevents
   the new monitor from being armed.

Items 3–4 are closed by V43.3a (2026-09-15), together with callback reentrancy, shutdown,
TUI startup acceptance and stale pause notices. Independent lifecycle/surface review is CLEAN.
Items 1–2 and durable accepted-delivery state are closed by V43.3b (2026-09-16).

V43.3b now has a versioned execution journal alongside the pause: the accepted plan, settled
outcomes, private child messages/tool results, effort, rounds, spend, worktree and provider handle.
Offline restart tests pass for code mode, multi-agent work and single-task agent fallback; a
concurrent tool-batch test preserves the finished call and defers the remaining call. Worktree
reuse verifies its task directory and repository registration. Independent review is CLEAN and
`make check` passes (3,727 tests, zero lint issues). Older execution histories are archived outside
routine saves and remain exportable. `/resume discard` recovers an abandoned or uncertain request
without removing its files; cancellation before an accepted resume restores its saved pause.

Provider-owned private conversations resume through each child's saved handle. They cannot be
transferred to another provider while unfinished; native tool conversations retain their messages.
Context pressure needs working-context compaction while retaining the saved history; waiting for
an allowance reset cannot by itself fix a context overflow.

V43.3c closes this separation (2026-09-22). Main and native child tool loops archive
complete conversations before shrinking them. A single long turn can now compact completed tool
traffic; an unanswered batch stays intact. JSON export and fork include retained archives. The
planner receives preceding conversation; large dependency and synthesis results use explicit
previews with raw results preserved. Independent review found routed-child and smaller-orchestrator
window mismatches, now fixed. An additional regression fixed undo dropping post-compaction work.
Independent review is CLEAN; `make check` passes (3,745 tests, zero lint issues). Kolk compacts
transcripts it owns; vendor-private context remains vendor-owned.

## Localia — V43.4

Native Ollama reuse and lazy session-owned startup already exist. Native installation,
project-level ephemeral policy and managed persistent-runtime reuse were missing at the audit.
The owner's choice is explicit: **ephemeral on stops on session close; cached models remain**.
Persistence must also account for Linux parent-death process cleanup and record the managed
endpoint so later sessions can reuse it. Skipping Close alone does not implement persistence.

V43.4a closes project lifetime on 2026-09-24: project-scoped configuration, genuine detachment,
serialized persistent reuse, safe ownership, shared session pull/chat discovery and endpoint-bound
signin. Independent review is CLEAN; `make check` passes (3,764 tests, zero lint issues).
Windows persistent startup remains unavailable pending the platform lock implementation.
V43.4b closes native installation on 2026-09-24. The verified downloader/extractor and session
wiring are implemented. Independent review reproduced and resolved: unreadable normalized
symlinks, missing nested directory sync, architecture-switch cache collisions, lock aliases
modifying unrelated files, keyless picker/listing failures, lost explicit endpoints, falsely
local cached cloud rows, hidden local rows during remote outages, and setup on an incomplete
model ID. Focused race tests and independent overlays pass. Final `make check` passes (4,419
tests, zero lint issues). An independent real macOS v0.34.4 archive trial passed verified download,
extraction, offline reuse and cleanup under race. No vendor executable ran; Linux/GPU trials remain
unperformed. Integrated flow and additional Linux GPU bundles remain V43.4c.
