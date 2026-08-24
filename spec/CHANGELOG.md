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
- Define `permission.requested` identity, tool detail, and optional diff preview.
- Define `permission.resolved` correlation, decisions, and optional reason.
- Define parent/child correlation, presentation identity, mode, and outcome for the subagent
  lifecycle.
- Define per-attempt `usage.reported` accounting, optional measurement presence, cost provenance,
  and comparability classes.
- Define typed `score.recorded` values, target kinds, and human, judge, or implicit provenance.
- Define durable pre-write snapshot identity and context for `checkpoint.created`.
- Define structured `log` diagnostics with closed levels/codes and field-transition context.
- Define the shared error entity and the exhaustive code-to-HTTP, shell-exit, and default-retry
  mapping.
- Define `error` as an event wrapper around the shared error entity with no duplicate failure
  fields or policy.
- Publish the ordered 23-event catalog and require constants, schemas, IDs, and goldens to remain a
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
