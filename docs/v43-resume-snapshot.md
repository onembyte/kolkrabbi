# V43.6 complete resume snapshot — design, 2026-09-29

Owner decision: at every pause, limit, or error, save one compressed recovery
point containing the conversation, plan, each task and result, children's vendor
handles and partial output, and background work. A resumed request must use the
saved boundary rather than redo settled work. A failed save must be visible in
the TUI, plain CLI, and stream-json. This design covers §8 item 6 in
`docs/v43-checklist.md`; each implementation leaf has its own red/green and
independent verification gate.

## What exists and what is missing

The session's atomic JSON save already stores the main working transcript,
`Pause`, and a versioned `continuity.Run`. That run stores the plan, resolved
routes and effort, completed task outcomes, native child messages/tool results,
provider handles, rounds, spend, worktrees and checkpoints. Orchestration drains
other active children at a limit before it returns; resumed tasks with settled
outcomes are skipped. Immutable compaction and old-execution archives live in
sibling directories. These are useful foundations, but they are not yet a
complete recovery point:

- The session file is uncompressed; no exceptional-boundary snapshot ties the
  session, its archives, and the run state together.
- Streamed text can reach a surface before an assistant message or child result
  is committed. The vendor handle may survive while those partial words do not.
- An ordinary error makes `finishExecution` mark the run `stopped`; only a
  completed limit pause can enter `restoreExecution`.
- Saving a session warns through `Agent.Out`; stream-json sets that writer to
  `io.Discard`, so its subscriber never sees the failure. `reportAgentLane`
  also writes text before stream-json subscribes and redirects output.
- A task's empty `Status` conflates queued, running and paused. A fresh process
  cannot infer whether an external side effect finished from that alone.

## Storage options considered

| Option | Advantage | Cost and decision |
|---|---|---|
| Compress the existing `<id>.json` in place | One file | Breaks the frozen readable session format, listing/export and old clients. Reject. |
| Append-only event journal | Fine-grained replay | Requires replay, compaction and multi-file commit protocol for facts already held in `Run`. Too much machinery for exceptional boundaries. Reject. |
| Atomic gzip sidecar, keep the JSON session | Standard library, small and independently verifiable; old session format remains readable | Requires an explicit revision and load precedence. Choose. |

Use `compress/gzip`, `encoding/json` and `internal/atomicfile`; no external
dependency or shell compressor. `<id>.resume.json.gz` is the recovery file. Its
versioned envelope contains the full session state (including current main
messages and current execution), the complete current run, pause/error reason,
all archived main/child conversation bytes needed to reconstruct that run,
relevant old execution archives, a monotonically increasing session revision,
and SHA-256 digests for integrity and identity. The envelope adds no values
from the credential store or auth tokens of its own; transcript or tool output
may itself contain sensitive text, so the file is mode 0600.
The gzip trailer checks accidental corruption; envelope digests identify the
content rather than trusting a filename. Read only a regular file, reject
symlinks, invalid IDs, unsupported versions, truncated data and trailing gzip
members, and bound expanded data. An unreadable newer recovery point is a
visible error, never a reason to silently load older work and repeat it.

`Session.SaveRecovery(reason)` takes the same `writeMu` and `messagesMu` as
`Save`, freezes one coherent state, and durably writes the compressed file by
atomic rename and directory fsync. It then writes the existing JSON mirror and
header for compatibility. Recovery success depends on the compressed file;
mirror/header failure is separately visible, while the recovery file stays
loadable. Normal saves and recovery saves carry increasing revisions. `Load`
chooses the highest valid revision from JSON and sidecar. If either candidate
exists but cannot be decoded, its revision is unknowable; loading fails visibly
even when the other is readable, rather than risking older work. A failed
mirror write that never reached an atomic rename leaves its prior valid file;
there is no exception based only on a warning or file mtime. Equal revisions must
describe identical state. Older JSON sessions have revision zero and remain
loadable. A newer ordinary JSON record is usable only after every archive it
references has been verified; it carries later state, while the prior sidecar
remains an immutable complete recovery point for its own revision. Missing or
damaged required archives fail closed, without replay from the older sidecar.
`List` enumerates the union of JSON files and recovery sidecars, de-duplicates
IDs, and derives a header from the latest valid state. It must expose a corrupt
sidecar as an error rather than silently omit that session; `Latest`, export,
fork, doctor/header repair and delete use the same preference and account for
the embedded archive data. Thus a first-turn snapshot is discoverable even if
the JSON mirror was never written.

## Boundary and resume protocol

Only a quiescent boundary is promised. An error that survives the internal
retry policy and is exposed as a planner/child/tool/synthesis failure counts
even if it becomes a task outcome and `RunTurn` later returns nil. On such an
error, provider limit, or pause, close admission, wait for every launched child
to return, merge each settled outcome and saved private transcript, and then
freeze and write recovery before admitting further work. Do
not publish a durable-pause promise or auto-deliver a resumed turn until that
write succeeds. Keep the in-memory state if it fails, tell the user on every
surface, and never imply that a restart is safe. A limit remains timed by its
vendor `ResetAt`. A non-limit recoverable error preserves the run and exposes
an explicit retry path instead of changing it to `stopped`; an intentional
cancel/discard remains terminal. Save errors themselves cannot be retried by
blindly replaying work.

The journal gains explicit queued/running/waiting/settled state and partial
output for main and child provider calls. Stream callbacks append bounded
chunks to the journal under its lock; the final provider message supersedes
the partial text, while an interrupted call retains it. Never truncate the
accumulated text. Amended at Capture review (2026-09-29): chunks accumulate in
a per-call builder, linear in the stream, and materialize into the run only at
a snapshot or store; there is no spill file. One call's partial is bounded by
the provider's own output limit, and the recovery envelope is built whole in
memory at write time anyway, so a spill would move bytes to disk only to read
them back into the same envelope, adding a second integrity-checked file
without lowering peak memory. Persist each child's
provider handle, its confirmation state, and last confirmed tool boundary
before releasing its backend. A minted handle is not proof the vendor accepted
the turn: Claude's backend exposes a handle before `system/init`, and Codex's
`exec resume` takes a new prompt. Add a per-adapter continuation capability
that reports whether the handle was acknowledged, whether the turn was
accepted/completed, and whether the provider can attach to an unfinished turn
without resending its prompt. Do not infer any of these from a non-empty
handle; a hard-exited or unattachable provider turn is uncertain.
For native tool batches, preserve pending tool-call IDs and completed results;
resumption executes only unanswered calls. For vendor-owned conversations,
resume the saved handle only through an adapter that proves the relevant
continuation operation. If the vendor accepted an unfinished prompt, never
send it again merely because `--resume` accepts a handle. If a provider cannot
establish whether an external action completed, mark the task uncertain and
require inspection/reconciliation before another action; never claim exact
replay safety from an unverified handle.

On load, validate the project root, mode, model/provider binding, plan indices,
task states, archive hashes, child handle ownership and worktree registration
before any provider call. Rehydrate settled outcomes directly; show their
results without rerunning them. Reconstruct queued work from the saved plan.
Previously running background children become waiting continuations with their
own transcript and handle, then re-enter the scheduler at the first safe
boundary. The scheduler resumes only unfinished tasks and then synthesizes
once, from all saved outcomes. A saved synthesis response is not repeated.
"Background tasks" here means user-work children launched by orchestration,
including queued and active children and their dependencies; each has a task
record. CLI catalog refresh, model warming and update checks are infrastructure
jobs, do not perform user-request work, and can be rediscovered or restarted
idempotently on the next process. No background worker is presumed to remain
alive across a process restart.

The guarantee applies to completed, observed actions at a persisted boundary.
A hard process kill while a third-party command or provider-owned tool is in
flight has no atomic handshake with that external process. Recovery must label
such an outcome uncertain and inspect it; automatic duplicate execution would
violate the owner's no-redo requirement. A failed recovery write is likewise
reported as a failed guarantee, not silently treated as a snapshot.

## Surface contract

Use one typed save-failure notification emitted by the engine at every failed
exceptional write. Plain output shows the failed path/reason and that restart
recovery is not guaranteed. The TUI keeps the warning visible through status
refreshes. Stream-json emits a valid protocol envelope with a stable code,
reason and scrubbed error text; stdout remains only NDJSON and stderr is for
human setup/progress. Move `reportAgentLane` after the stream-json branch has
installed its subscriber, or route that report into the event stream. Test
startup and failed save with a real decoder, not a string-prefix assertion.

## Ordered implementation leaves

1. **Store:** red load/save/corruption/revision/archive tests; implement the
   atomic gzip recovery envelope and safe `Load`/list/export/delete precedence.
2. **Capture:** red concurrent-child and partial-stream tests; add explicit
   task state, partial output, and one quiescent recovery write at pause, limit
   and error. Do not publish a successful durable pause before the write.
3. **Resume:** red restart tests with completed tasks, pending native tool
   calls, acknowledged and merely minted vendor handles, hard exits,
   worktrees, background children and synthesis; add an explicit per-adapter
   acceptance/continuation seam, continue only at proven boundaries, and fail
   closed on uncertain actions.
4. **Surfaces:** red plain/TUI/stream-json save-failure and NDJSON-startup
   tests; make every failure visible and remove the pre-stream text.
5. **Walk-back:** adversarial corruption and race probes, old-session migration,
   docs and help, all required gates. Any uncovered issue becomes one new leaf
   before §8 item 6 can close.

Every leaf gets an author red test, fix, overlay mutants, focused race tests,
`make check`, and a fresh non-author read-only verifier returning CLEAN before
its checkbox closes. The release follows only after all five are closed.


### Resume implementation refinement — 2026-09-30

Resume is split into three reviewable leaves in §8: native recovery, vendor recovery,
then the complete restart-validation matrix. The first adds an explicit recovery reason
on the journal, written only with an exceptional recovery save. Main `running` state
alone is not evidence of a hard exit: a native tool-error snapshot can be between calls.
Before another action is admitted, clear the saved marker and pause and durably save that
retirement. If retirement fails, admit nothing. A newer ordinary record with no recovery
marker or completed pause is an uncertain interruption and remains blocked. Existing
pause-only journals retain their compatibility path; non-pause vendor recovery waits for
the adapter-proof leaf. This does not change the later exact-resume acceptance contract.

### Vendor recovery — 2026-09-30 (contract v3, after Codex's review)

All three shipped adapters send kolk's whole transcript each turn and resume a vendor
conversation by its handle. None can attach to a turn the vendor left open, so the only
continuation that exists is a new message on a conversation whose last turn the vendor
itself closed. Asking a model to "inspect first" is not reconciliation. Each adapter
reports facts about its latest turn, and they are recorded in the journal beside the
handle when it is captured:

- `TurnNeverStarted`: positive proof that the prompt never reached a vendor process,
  because the process could not start or a write to its stdin failed. Missing output
  after delivery proves nothing: a Node CLI can act and lose frames on exit. Claude and
  Codex give this proof; Copilot does not. An adapter's own retry is allowed only for a
  turn that was never delivered.
- `TurnClosed`: the vendor ended the turn itself. That means Claude's result frame
  (including one drained after an interrupted read), Codex's `turn.completed` or
  `turn.failed`, or Copilot's result frame. A plan limit arrives this way.
- `ProviderHandleConfirmed`: a vendor frame named the conversation. It survives the
  process that named it, until the handle is retired (for example, after a kill).
- `ResumesConversation`: a confirmed conversation continues by its handle.

The plan decorator forwards all four and asserts none itself. It also forwards observed
streams, so vendor tool reports reach the journal. A failed turn notes its handle only
once the vendor has confirmed it.

Before any provider is opened, each unsettled vendor task and the main session are
decided from the journal alone, at pauses and at error/limit boundaries alike:

1. **Never delivered, no vendor tool reported:** start over on a new conversation from
   the task's own briefing.
2. **Last turn committed, nothing unfinished:** nothing can be redone.
3. **Vendor-closed turn:** the vendor closed the interrupted turn, no tool it started is
   unfinished, the handle is confirmed, and the adapter resumes. Continue with a new
   message on that same conversation. This is the automatic recovery after a plan limit.
4. **Anything else is refused, with the work retained:** an open turn, an unfinished
   vendor tool, an unreachable or unconfirmed conversation, an adapter that cannot
   resume, or a journal saved before these facts existed. `/resume discard` sets it
   aside.

The saved conversation must be the session's own at any boundary: the main backend must
still be vendor-owned and drive the very handle saved. A task that must continue its
conversation never falls back to another model. If its model cannot start, the run
stops at a recovery boundary with the task held (tree kept, nothing landed, not an
outcome) until `/resume` can open it.

Round-2 corrections (second verifier, 2026-09-30):

- **Delivery is a fact the process reports.** The production line process never fails a
  write, since its exit explains itself. `Queue` says whether the line reached a live child:
  it returns false only when the child had already exited. A line queued in the instant the
  child dies counts as delivered, and fails safe.
- **The drain reports what it consumes.** Tools drained after an interrupted read reach
  the observer, and so the journal.
- **Delivery is sticky, and an attempt that never arrives changes nothing.**
  `ProviderDelivered` records that some attempt of the task reached the vendor in this run;
  "never started" means no attempt ever did. When an attempt provably never arrived after
  one that did, the task keeps the state, open or committed turn, and partial output it had
  before that attempt. A continuation whose process could not start therefore stays
  continuable, never "never started" and never open.
- **Identity holds for children too.** The backend a continuation opens must drive the
  saved handle, not the session's shared provider and not another conversation. For the
  main session, the model must also still resolve to the vendor it was saved with.
- **Legacy pause-only journals are refused.** A pre-journal pause holds no run facts, and
  its conversation lives only in the session file. When the session's backend is a vendor
  holding a conversation, such a journal is refused rather than having its turn resent.

Round-3 corrections (third verifier, 2026-10-01):

- **An adapter retry never changes conversation.** A retry happens only for a turn that
  never reached the vendor, and it resumes the same handle. A dead resume (a stored handle
  the vendor no longer knows) costs one failed turn, and the next turn opens a fresh
  conversation. It never silently moves an unfinished turn onto a new one.
- **A dead resume is judged over the process's life.** The handle is retired only when a
  resumed process failed without ever printing a frame, not because one turn was quiet.
  A process killed while idle keeps its conversation (round 4 makes the kill rule exact).
- **"Could not start" is a typed fact.** The shell returns `NotStartedError` when the
  executable is missing or fails to start. Claude's one-shot path and Codex use it to
  report that a turn was never delivered, and nothing else counts as proof.
- **An attempt that never arrived keeps the conversation.** When an attempt provably never
  reached the vendor, the journal keeps the handle and confirmation it already had, for
  the main session and for children alike.

Round-4 corrections (fourth verifier, 2026-10-01):

- **A kill retires exactly the conversation it left unfinished.** A conversation is
  retired when a killed process had a delivered turn the vendor had not closed. That
  includes the adapter's retry process, and a cancelled turn that Close kills after its
  grace. A kill after the vendor's result frame retires nothing, because that turn is
  closed and the conversation whole (except the fail-safe trailing-frame case noted in
  round 6). The kill is judged after the process is closed, on
  every process that ran the turn.
- **A retirement reaches the session file.** The plan decorator records an empty handle
  when the adapter reports `ProviderHandleRetired`. A restart then never resumes a
  conversation whose turn was left unfinished, even after `/resume discard`.
- **A respawn resumes the vendor's own name** for the conversation, which is the one the
  journal and the session file record.
- **Only an interrupted request is a continuation.** The continuation message is sent
  only when the journal shows the request's own vendor call stopped
  (`ProviderInFlight`). An ordinary later request in the same vendor conversation is sent
  as itself.
- **`/resume` can always reopen retained work.** A resumed turn releases its claim on the
  run when it ends. Work it left at a new error boundary (a held continuation, a
  continuation that never arrived) is then offered by `/resume` again in the same
  process.
- **A new session leaves the old session's conversation.** `/new` keeps the backend but
  calls `ForgetConversation`, so the new session's requests never land in the
  conversation where the old session's saved work waits.
- Watchpoint, fails safe: if the vendor printed a frame after its result and then exited
  idle, a continuation would count as delivered and fail. The journal then refuses it
  rather than continuing it, and it never reaches another conversation.

Round-5 corrections (fifth verifier, 2026-10-01):

- **A new session records its own conversation.** The plan decorator's note follows the
  agent to whatever session it is on (`noteOnCurrentSession`), for the startup backend and
  for one built by `/model` alike. After `/new`, the new session's file learns the new
  conversation, and the old session's keeps its own, so a restart can continue either.
- **Agent-mode planner and synthesis calls are continued, never resent.** A main vendor
  call that stopped while planning or synthesizing (`Main.ProviderInFlight` in that
  phase) is continued with the continuation message on its conversation. One closure
  builds each request, so the overflow retry keeps the continuation too. A call saved
  before it was sent is made afresh, and the call after a continued one is an ordinary
  request.
- **A kill is judged only on a session that lost its place in the stream,** the retry's
  replacement included. A process killed after the vendor's result frame retires nothing,
  with one exception, which fails safe: if the vendor printed further frames after its
  result before the kill, the next continuation reads them and is judged killed (round 6,
  K3). That conversation is then retired and the continuation refused. It never moves
  elsewhere.
- **A resumed turn releases only the claim it was given.** Each claim is counted, so a
  delivery armed inside a turn (continuity's own monitor) keeps its claim. Round 6
  completes this (see below).
- **A turn refused by a closed or unusable session proves nothing.** Its per-turn facts
  are reset before the refusal, so it never reports the previous turn's delivery or
  closure.

Round-6 corrections (sixth verifier, 2026-10-01):

- **A continuation goes where the conversation is.** A stopped main call, whether direct,
  planner or synthesis, continues on the model that answered it (`savedMainModel`). That
  is the same model whose backend was checked to drive the saved handle. Tiers or slots
  changed since then never route a continuation to a backend that did not see the turn.
- **A delivered turn runs only while its claim is its own.** The resume monitor marks the
  context it hands the surface with the claim count it delivered under. A delivered turn
  that finally runs after another turn has continued the run, or after the run was claimed
  again, is skipped and says so, instead of sending the request a second time.
- **A refused request gives back only its own claim.** A refused resume of the saved
  request releases its claim so `/resume` can retry. A different request, refused because
  a run is saved, leaves the claim to the pending delivery that holds it. Together with
  the above, `/resume` cannot hand the same run out twice.
- **A native main never gets the vendor continuation.** With no vendor conversation,
  nothing is continued; the stopped call is made again as itself.
- Equivalent, kept on purpose: the delivered turn removes its ticket from the context, so
  a nested continuity hop never re-checks it. No path re-claims a run between a delivered
  turn and its hop, so that removal cannot be told apart by any test.
