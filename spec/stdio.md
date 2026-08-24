# Kolkrabbi event framing

Kolkrabbi carries the same validated compact event envelope through three exits. CLI
`--output stream-json` and a daemon child's stdout use NDJSON. HTTP streaming uses Server-Sent
Events (SSE). This document freezes event output only; client-to-server command framing remains
outside this slice.

## NDJSON

One event is the exact compact UTF-8 JSON returned by the protocol encoder followed by one byte
`LF` (`0x0a`):

```text
<envelope-json>\n
```

There is no leading whitespace, blank line, trailing space, pretty printing, or `CR`. JSON escapes
embedded carriage returns and line feeds inside strings, so every envelope occupies one physical
line.

## Server-Sent Events

The same event is one SSE block:

```text
id: <decimal-seq>\n
event: <event-type>\n
data: <envelope-json>\n
\n
```

`<decimal-seq>` is the unsigned base-10 `seq` with no padding. `<event-type>` is the envelope's
exact lowercase wire type. `<envelope-json>` is byte-for-byte identical to the NDJSON line after
removing only its final `LF`. It is always one `data:` field; Kolkrabbi does not fold one envelope
across multiple SSE data lines.

An idle-connection heartbeat is the SSE comment block:

```text
: ping\n
\n
```

It is not an event, has no sequence, is never persisted, and never appears in NDJSON. The server
owns heartbeat scheduling, reconnection delay, replay cursors, and `Last-Event-ID` handling.

## Stream decoding

A protocol frame is bounded to 1 MiB (1,048,576 bytes) of envelope JSON. The NDJSON limit excludes
its final `LF`; the SSE limit excludes the `data: ` prefix and following `LF`. This protocol bound
is independent of the larger vendor-frame limits used by external-agent adapters.

Readers stream one validated envelope at a time to their consumer and do not collect a whole
stream in memory. An empty stream is valid. Every non-empty NDJSON frame and every SSE field/block
must be LF-terminated; a final partial line is corruption, even when its JSON is otherwise valid.
Blank NDJSON lines, leading or trailing transport whitespace, and any `CR` byte are invalid.

The SSE reader accepts only the field order and spelling above. It verifies that `id` is the
canonical unsigned-decimal rendering of the decoded envelope's `seq` and that `event` equals the
decoded envelope's `type` before delivering the event. Reordered or repeated fields, multiline
`data`, extension fields, unknown comments, and empty blocks are invalid. Only the exact
`: ping\n\n` block is ignored as a heartbeat.

## Conformance streams

`spec/testdata/streams/` contains three canonical whole-turn basenames: `code-turn`,
`permission-denied`, and `agent-fanout`. Each has an `.ndjson` source and an `.sse` twin. The twin is
the exact concatenation of the SSE encoder's blocks over the NDJSON envelopes, with no heartbeat or
transport-only event inserted. Both decode to the same envelopes in the same order.

The `saga-chapter` stream remains absent until chapter state and terminal outcomes are part of the
event contract. `resume-after-drop` remains absent until retained-log cursor and replay semantics
are fixed. Their absence is intentional; implementations must not infer those protocols from the
current fixtures.
