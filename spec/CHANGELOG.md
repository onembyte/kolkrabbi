# Kolkrabbi protocol changelog

The protocol remains version `0` while its contract is still being discovered. Breaking changes
are allowed during version 0, but every change is recorded here.

## 0 — unreleased

- Define the six-field event envelope and its first golden conformance frame.
- Define the `message.delta` and `reasoning.delta` event names and their non-empty `text` payloads.
- Define the `hello` handshake payload for protocol version, server identity, and capabilities.
- Define started, updated, and ended session lifecycle payloads.
- Define started, finished, and cancelled turn lifecycle payloads.
- Define the full-text `message.completed` snapshot payload.
- Define `tool.requested` identity, raw JSON arguments, and execution ownership.
- Define `tool.started` correlation and execution ownership.
- Define `tool.output` correlation, possibly empty content, and execution ownership.
- Define `tool.finished` correlation, boolean tool outcome, and execution ownership.
- Add optional paired `task_id`/`child_turn` coordinates to every tool event so concurrent
  subagent-owned tool work remains attributable while old main-tool frames stay unchanged.
- Define `permission.requested` identity, tool detail, and optional diff preview.
- Define `permission.resolved` correlation, decisions, and optional reason.
- Define parent/child correlation, presentation identity, mode, and outcome for the subagent
  lifecycle.
- Define ordered `work.updated` observations for main and subagent roles, with closed state/phase
  vocabularies, monotonic per-work sequence, bounded step text, and child coordinates only where
  they exist.
- Define per-attempt `usage.reported` accounting, optional measurement presence, cost provenance,
  and comparability classes.
- Define typed `score.recorded` values, target kinds, and human, judge, or implicit provenance.
- Define durable pre-write snapshot identity and context for `checkpoint.created`.
- Define structured `log` diagnostics with closed levels/codes and field-transition context.
- Define the shared error entity and the exhaustive code-to-HTTP, shell-exit, and default-retry
  mapping.
- Define `error` as an event wrapper around the shared error entity with no duplicate failure
  fields or policy.
- Publish the ordered 24-event catalog and require constants, schemas, IDs, and goldens to remain a
  closed, exact set.
- Promote the `usage.reported` accounting row to a shared usage entity while preserving the event
  payload as an alias and schema reference.
- Promote the `score.recorded` evaluation to a shared score entity while preserving typed-value and
  provenance validation through one alias and schema reference.
- Define the `permission.resolve` client command as a pending-request ID plus one existing closed
  permission decision, leaving resolution reasons server-owned.
- Define the `turn.cancel` client command as one canonical turn ID, leaving cancellation reason and
  runtime state server-owned.
- Publish the ordered two-command catalog and require shipped command constants, schemas, IDs,
  goldens, and validators to remain one exact set.
- Define exact single-event NDJSON and SSE byte framing, including byte-identical data payloads and
  the SSE heartbeat comment.
- Define bounded callback stream decoding for exact NDJSON and Kolkrabbi SSE syntax, including
  heartbeat filtering and SSE metadata integrity checks.
- Add canonical cross-format whole-turn streams for code, permission-denied, and agent-fanout
  scenarios while leaving saga and replay fixtures dependency-gated.
- Publish the first owner-stable OpenAPI 3.1 cut with hello, turn cancellation, permission
  resolution, shared safe errors, and bearer authentication outside the hello shape check; defer
  every route whose command, entity, or replay semantics are not frozen.
- Define the `turn.start` client command as one non-empty prompt bounded to 32 KiB, and its
  `POST /v1/turns` operation. It is what a paired device needs in order to ask a session for
  something rather than only watch it; the prompt is bounded because it enters the conversation
  and is carried in every later request.
- Add optional `level` and `model` to `subagent.started`, and optional `model` to
  `subagent.finished`. A wide run on a subscription spends different amounts on different tasks, and
  a reader could see how many subagents ran but not what they cost; `level` is what the planner
  judged the task to need and `model` is the rung that answer resolved to. Both are additive and
  omitted when unstated, so an event from a build that predates them still validates — and `model`
  on the finished event is the rung that actually ran, which is not always the one it started on,
  because a cheaper rung that will not spawn falls back to the model the user selected.
