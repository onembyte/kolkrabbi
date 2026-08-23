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
