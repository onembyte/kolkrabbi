# 38. Product polish — terminal clarity, efficient agents and continuity

Status: hardened on 2026-09-14 · PLAN.md item 38 · implementation V43.1–V43.4 · wording
reconciled with verified behaviour on 2026-09-25 (V43.5; evidence in CHECKPOINTS.md)

## Decision

The owner's six priorities refine the zero-config north star. Build one verified leaf at a time.
Keep existing permission, sandbox and billing boundaries. No external Go dependencies are needed.

1. **Terminal clarity.** The selected model and effort belong together and take precedence over
   long session names. The model picker reserves room for the active effort; extra descriptive
   text yields first. Worker rows preserve state, model and effort at ordinary narrow widths.
   Colour is decoration: the same information remains legible without it.
   Work logs use compact bullets and indented results: commands show what ran and its outcome,
   exploration groups reads/searches (dedicated tools and read-only shell commands), and native
   edits show file names, added/removed counts and signed excerpts; delegated provider edits
   show the reported paths, with counts and excerpts only when the provider reports them. Stable agent labels identify parallel work. Explanations are public progress summaries
   and tool purposes; success and edit counts come from observed results. Bound long output and
   preserve clear failure/empty-output states. The activity footer remains brief while the work
   history stays readable above it.
   Colour and symbols work together: purple activity, green completion/additions, red failure/
   removals, amber waiting/warnings and muted supporting text. Bullets start groups; branches and
   arrows connect results to actions. Keep explicit words and signs for colourless terminals.
   The owner's screenshot specifies shaded red/green backgrounds across changed lines, line
   numbers and coloured added/removed counts, not foreground colour alone. Submitted prompts get
   full-width shading within their transcript column and clear spacing on both sides; multiline
   prompts and blank lines remain one block in every theme.
2. **Octopus.** Use the owner's supplied purple pixel artwork as an emoji-sized icon beside
   the thinking indicator: two columns by one row. Inline images on detected Kitty/Ghostty and
   iTerm/WezTerm terminals preserve its colours; the text fallback is 🐙. Multiplexers, unknown
   terminals and colourless output use the fallback. This supersedes the wheel-only decision
   and the initial three-row block sketch, rejected by the owner as too large.
3. **Agent routing.** The selected model remains the capability ceiling. Difficult and unstated
   work get that model; on signed-in adapters with discovered ranks, routine work uses the
   nearest lower model and mechanical work the lowest. Where the menu holds only the selected
   model (gateway, compatible endpoints, local runtimes, or no ranked lower model signed in),
   every task runs on the selected model, and a configured slot outside the menu is announced
   and replaced by it.
   Hard work asks for the highest effort, routine medium, mechanical low, and unstated work
   keeps the session's effort, each resolved to a level the chosen model offers. Task count is
   distinct from concurrency: queued work can exceed the number of simultaneously active agents.
   An unavailable or unranked candidate must never become a guessed upgrade or billing switch.
   Use fresh discovered capability ranks and exact model IDs on signed-in adapters. Mechanical
   work uses the lowest ranked model, routine work the nearest lower model, and hard/unstated
   work the selected ceiling. Explicit slots are also bounded by that discovered menu: a slot
   outside it is replaced by the rung for the task's level, never by a guess above the ceiling. Resolve
   advertised effort before launch and retain exact vendor spellings, including max vs xhigh.
   The owner's request for as many agents as necessary means there is no fixed total-task cap.
   This can run a large planner-produced queue, including on metered models. The existing
   optional `max_run_cost_usd` setting bounds spending; concurrency alone does not bound total
   spend. Do not silently discard tasks or introduce a new approval step for large plans.
4. **Continuity.** A usage limit suspends the current work. Durable state includes pending input,
   the accepted plan, completed results, child messages and remaining tasks. On resume completed
   tasks are not re-executed. Context pressure compacts working context while retaining the stored
   transcript; Kolk compacts the context it owns, and a vendor CLI's private conversation is
   compacted by that vendor. Waiting alone cannot solve a context overflow. Failed persistence is visible.
   A reset timestamp makes a resume eligible; it never consumes pending input. Manual policy and
   an unavailable surface keep the pause. Automatic delivery must acknowledge acceptance and
   return an unaccepted request to the session. Shutdown cancels and joins the delivery.
   Save the original request and working-directory binding, resolved task routes, dependencies,
   outcomes, child conversation handles, completed tool results and run accounting together.
   Stop launching work when a child hits a limit. Let operations already in progress settle, then
   pause before the next provider or tool call; an interrupted external side effect cannot be
   assumed safe to repeat. Keep a paused writer's isolated tree until it can finish and land.
   Working-context compaction and stored history have separate lifetimes: shrinking a request
   must not erase the conversation that explains the goal, edits or earlier decisions.
5. **Localia.** Kolk handles native runtime setup when local execution is requested, on macOS 14+
   and Linux amd64/arm64; Windows managed setup and persistent runtimes are not implemented, and
   existing runtimes are still adopted there. Existing
   runtimes are reused. Downloaded binaries are verified before use; weights remain cached.
   `local.ephemeral` defaults on, with a project override through `/config`. On releases, at
   session exit, only what that session started; a runtime an earlier `off` left running is reused
   and left running. Off keeps the managed runtime available. A user's
   separately started server is never stopped. Setup must not require Docker, sudo, shell-piped
   installers, or editing a config file.

## Validation

Offline regressions cover layout, model selection, pause after child side effects, restart from
saved state, and local process ownership. Run focused race tests for concurrent changes and the
repository gates. An independent agent verifies each leaf before it is marked complete. Record
live-machine limitations separately from simulated results; do not describe all six priorities as
finished while any acceptance item remains open.
