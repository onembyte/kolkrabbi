# 19. Desktop & iPad path

Status: hardened on 2026-08-27 · supersedes: — · PLAN.md item 19

## Decision (the short version)

This is the last unhardened item and the only one where almost nothing is built, so it is a real
decision rather than a record. The decision is:

**No desktop app, and no iPad app. Not yet, and not for the reasons this item assumed.**

The item frames desktop as a stack choice — Wails v3 versus Tauri v2 versus Electron — and defers it
until the daemon protocol exists. The protocol now exists, which makes the question answerable, and
the answer is that the stack was never the hard part. What a desktop shell would add — a dashboard, a
session browser, several sessions at once, notifications — is four things, and **three of them
already shipped in the terminal and the browser**: `kolk dash` renders the dashboard on the server,
`kolk sessions` browses them, and item 27 gives many sessions one view. The fourth, OS notifications,
is a real gap and a small one, and it does not justify a second application with its own release
train, its own crash reports and its own auto-update path.

So this item's output is not a stack choice. It is:

1. **The condition that would make a desktop shell worth building** — stated, so the answer can
   change on evidence rather than on enthusiasm.
2. **The stack, decided now anyway**, because deciding it costs nothing and leaving it open invites
   the wrong default later: **Tauri v2 with `kolk serve --stdio` as a sidecar**, not Wails.
3. **The iPad answer, which item 26 already made** — a native app is refused; the iPad is a client.
4. **The protocol constraints these impose**, three of which are met and one of which is not.

And it fixes three claims in the plan that turned out to be fiction, found by looking rather than by
reading.

## Spec

### 1. Should a desktop app exist?

Not until someone can name a thing they cannot do today. The four candidate reasons:

| What desktop would add | State |
| --- | --- |
| A dashboard | shipped: `kolk dash`, server-rendered, no script, no assets, loopback only |
| A session browser | shipped: `kolk sessions` with search, rename, fork, export |
| Several sessions at once | shipped: item 27's overview, plus advisory locks so liveness is observed rather than reported |
| OS notifications | **not shipped**, and the only real gap |

A notification when a long turn finishes or a permission prompt is waiting is genuinely useful, and
it is the entire remaining case for a desktop application. That case is not strong enough for a
second binary, a webview runtime, a code-signing identity, notarization (item 20 refuses it precisely
until this day comes), and a separate update path. It *is* strong enough to consider a 200-line
notifier that watches the event stream and calls `notify-send` or `terminal-notifier` — which is a
tool, not an application, and which is where this should start if anyone wants it.

**The condition for revisiting:** more than one person is running several sessions at once and says
the terminal is where the work gets lost. Not "a GUI would be nice".

### 2. The stack, if it is ever built

**Tauri v2 with `kolk serve --stdio` as a sidecar.** Decided now because leaving it open is how a
project ends up with the default rather than the choice.

The reason is the cgo rule, which is a load-bearing architectural property and not a preference:
every binary this repository ships is `CGO_ENABLED=0`. Wails v3 sets `CGO_ENABLED=1` on darwin and
linux and needs Xcode CLT or gtk4 + webkitgtk — a toolchain in the Go build. Tauri's shell is Rust
and talks to a **child process**, so the Go side stays exactly what it is today: a cgo-free binary
speaking NDJSON over stdio. That exit already exists and was built for this — `kolk serve --stdio`
is one of three exits carrying byte-identical frames, alongside `kolk -p --output stream-json` and
HTTP+SSE.

Wails v3 is also still in beta, but that is the smaller objection. The larger one is that the sidecar
boundary is a protocol we already ship, test and version, while an in-process webview binding is a
new surface with no contract.

Electron is refused: it is the sidecar model with a much larger runtime and no compensating benefit.

### 3. iPad

**A native Swift app and a gomobile-bound core are both refused**, which is item 26's decision
(native mobile apps cost two release trains between a fix and its users) applied to a second device.
The technical objection is sharper here: iPadOS cannot spawn a shell or a toolchain, so code mode
cannot exist locally on an iPad at all. A native app could only ever be chat, which is the weakest
thing kolk does and the thing every other client already does.

**The answer is the one the item already calls pragmatic, and it is now built rather than proposed:**
run kolk on a Mac or Linux box, reach it over Tailscale, and use the iPad as a client — a terminal
app for the session itself, the dashboard in Safari, and item 26's paired-device path to watch a
session and answer its permission prompts. What a paired device still cannot do is *send a turn*;
that is I26.7, and it is queued.

This is not a lesser version of an iPad app. It is the version where the code runs on a machine that
can compile it.

### 4. Protocol constraints imposed on the core

The item asks which requirements these targets impose *now*. Four, and the score is three to one:

| Requirement | State |
| --- | --- |
| Streaming events | **met**: one in-process bus with three exits carrying byte-identical frames, and `?after=` for resumption after a dropped connection |
| Auth on localhost | **met**: loopback-only by default, a wildcard bind refused before the socket opens, device tokens stored as hashes, a six-digit pairing code armed briefly |
| Versioning | **met**: `protocol.Version` mirrors `spec/VERSION`, decoders accept unknown fields, and a spec guard fails the build on an unrecorded change |
| Session multiplexing | **not met** |

**Session multiplexing is the one thing a desktop shell would need that does not exist.** A `Bus` is
created per session (`bus.New(session, …)`) and `kolk serve` serves exactly one; there is no way to
subscribe to several sessions over one connection. Item 27 met "many sessions, one view" a different
way — header-only reads of session files plus an advisory lock for liveness — which is adequate for
a viewer and would be wrong for an application holding one socket open.

**It should stay unbuilt.** Multiplexing is a real change to the bus, the serve surface and the
protocol envelope's routing, and the only consumer that would justify it is the application this item
just declined to build. Building it now would be building a feature for a hypothetical client, which
is the exact shape of work this plan keeps refusing. It is written down here so that whoever revisits
the desktop question knows it is the first thing they must do.

### 5. Three claims in the plan that were not true

Found by looking at the tree rather than reading the document, which is the only way this class of
error is ever found:

1. **"Three nested modules are pre-carved and empty: `desktop/`, `bind/`, `tools/`"** (item 2).
   None of the three directories exists. The fence they describe is real — it follows from the
   module-path rule, and a nested module at `github.com/onembyte/kolkrabbi/<sub>` may legally import
   `internal/…` whenever someone creates one — but "pre-carved" describes work nobody did.
2. **"`modernc.org/sqlite` is the one heavy dependency"** (item 2). The dashboard shipped rendering
   entirely on the server with no database. The dependency was never added, and `internal/dash`
   contains one file.
3. **"`tools/` keeps codegen deps out of the root graph"** (item 21, mine, one day old). Inherited
   from the same sentence without checking. The root graph is clean, but not because of a directory
   that does not exist.

The layer table had gone stale in the same direction: `internal/dash` was allowed to import
`modernc.org/sqlite` and `modernc.org/libc` on the strength of claim 2. That allowance is removed,
and a rot test now fails on any allowance nothing imports — the same shape as the dead-export
allowlist's rot test, and for the same reason: an allowance nobody uses is a decision nobody
re-reads. A budget that pre-approves what nobody asked for is not a budget.

## Build leaves

- **L19.1 the third-party allowance list cannot rot** — every allowance in `layers.go` must be
  imported by the package it names; the speculative SQLite entry is gone. *Built.*
- **L19.2 the stale platform claims corrected** in items 2 and 21. *Built.*
- **L19.3 session multiplexing** — unbuilt on purpose, and the prerequisite for reopening the
  desktop question.
- **L19.4 a notifier** — a small tool that watches the event stream and raises an OS notification,
  if the notification gap is ever felt. Not an application.

## Open questions

- Whether the notification gap is real enough for L19.4. Nobody has complained yet, and nobody has
  used kolk for a week of long turns yet either.
