# Research: language & platform strategy (Go now, desktop later, iPad after)

Date: 2026-08-22. Method: local measurements on the prototype, `gh api` repo facts, WebFetch of
Apple/pkg.go.dev docs, plus the ecosystem findings (ecosystem.md) and storage findings
(dashboard.md). Feeds PLAN.md items 2, 11, 19.

## Verdict: Go — yes, with three caveats

**For:**
- Measured on the prototype (this machine, M-series): **6.1 MB static binary, ~10 ms per
  invocation** (fork+exec+run averaged over 20 warm runs), zero dependencies, trivial
  cross-compilation. That is the "lightweight and fast" requirement already met.
- The Go TUI stack is production-proven for exactly this product: **Crush (27.6k★) ships on
  Bubble Tea v2.0.9 stable + Lip Gloss v2 + Glamour v2 + Bubbles v2** (`charm.land/*` import
  paths), with reusable patterns documented in its `internal/ui/AGENTS.md` (see ecosystem.md).
  Charm also maintains **fantasy** (provider abstraction incl. OpenRouter/openai-compat) and
  **catwalk** (auto-updated model DB) — usable directly or as reference.
- The "thin LLM SDK ecosystem" objection doesn't bite: kolk speaks OpenAI-compatible HTTP + SSE,
  which the prototype already implements in ~300 lines of stdlib. OpenRouter reports token usage
  and exact cost authoritatively (openrouter.md §4), so no local tokenizer is needed for
  accounting — only rough context estimation (a heuristic or tiktoken-go approximation is fine).
- SQLite without cgo exists and is adequate: `modernc.org/sqlite` (dashboard.md §4) — keeps
  cross-compilation clean.
- Concurrency/streaming (parallel subagents, SSE fan-in, cancellation via context) is Go's home
  turf.

**Caveats:**
1. Real code-intelligence features (tree-sitter, LSP integration) are heavier in Go than in
   TS/Rust — schedule them late, behind the daemon API, or shell out to language servers.
2. Bubble Tea v2 lives under the `charm.land` vanity imports — pin versions.
3. Windows needs deliberate work later (shell abstraction, ANSI); don't block v0.x on it.

**Counterpoints considered:** Rust (Codex CLI, Goose) matches Go on binary/startup but costs
more development speed for a solo builder and has no TUI stack advantage over Charm.
TypeScript/Bun (OpenCode, Gemini CLI, Claude Code) has the richest LLM ecosystem (AI SDK) and
contributor pool, but a heavier runtime footprint and larger single-file binaries; nothing in
kolk's scope needs that ecosystem. A pragmatic hybrid remains open: Go core + a small web
(TS/HTML) dashboard UI embedded via `go:embed` — already the plan for `kolk dash`.

## Architecture: core library + daemon protocol + thin frontends

Recommended layout (evolves the prototype, doesn't rewrite it):

```
core/        engine: providers, modes, effort, tools, sessions, checkpoints, stats  (stdlib-first)
cmd/kolk/    CLI/TUI frontend (Bubble Tea later; hand-rolled REPL now)
serve/       `kolk serve`: localhost daemon exposing the SAME engine over HTTP + SSE/WebSocket
             (JSON events; versioned; token-authenticated on localhost)
dash/        embedded dashboard SPA (go:embed)
```

Precedents for the client/server split: OpenCode's client/server design, Gemini CLI's core
package, Codex's `app-server`, and Anthropic's own guidance that other languages should drive
the agent loop "as a subprocess with `-p` and `--output-format json`" (subscription-auth.md §1)
— every serious agent CLI ends up with a programmatic event-stream surface. Design the event
protocol once (turn lifecycle, token deltas, tool-permission requests, cost events) and let the
TUI be client #1; desktop and iPad become clients #2 and #3 without touching the engine.
Pitfalls to spec early: protocol versioning, auth token for localhost, backpressure/slow
consumers on streams, reconnect/resume semantics.

## Desktop path (later)

- **Wails v3 is in Beta** — v3.0.0-beta.12 released 2026-08-21 with near-daily betas
  (github.com/wailsapp/wails releases); v2 is stable. Go-native webview; the Go core links in
  directly.
- Alternative: **Tauri v2 + kolk daemon as a sidecar binary** (any external binary can be a
  sidecar) — better ecosystem/polish, Rust shell, still zero changes to the Go core because it
  talks the daemon protocol.
- Decision can wait until the daemon protocol exists; both paths consume it. (Claude Desktop is
  Electron; no need to follow.)

## iPad path (after)

- **On-device toolchains are effectively out.** App Review Guideline 2.5.2 (fetched 2026-08-22):
  "Apps should be self-contained in their bundles … nor may they download, install, or execute
  code which introduces or changes features or functionality of the app" (narrow educational
  exception only). So code mode on iPad ⇒ **remote execution**: a Swift (or web) client speaking
  the kolk daemon protocol to a Mac/Linux box over Tailscale/SSH/WebSocket.
- **gomobile `bind`** produces an XCFramework (targets: ios, iossimulator, macos, maccatalyst;
  ObjC bridge; x/mobile is untagged/experimental per pkg.go.dev) — feasible for embedding a
  chat-only core locally, but unnecessary once the daemon protocol exists; treat as optional.
- **Pragmatic v0 for iPad (no app at all):** run kolk on the Mac; use Blink/Termius + mosh for
  the TUI and open the `kolk dash` URL over Tailscale. Works the day the daemon/dash exist.

## Risks
- Wails v3 beta churn → don't build on it until it RCs; Tauri sidecar is the hedge.
- Bubble Tea v2 pinning (`charm.land`), Glamour perf on giant streams (use Crush's
  stable-prefix re-render trick).
- gomobile experimental status → never on the critical path.
- Protocol designed too late → the TUI grows engine entanglements; spec the event stream in
  item 2's hardening loop, before the TUI rewrite.

## Sources
- Local measurements (2026-08-22): `go build` 6.1 MB; 20-run timing ~10 ms/invocation.
- github.com/wailsapp/wails releases + README (`v3 | Beta`), via `gh api`, 2026-08-22.
- https://developer.apple.com/app-store/review/guidelines/ (2.5.2, 4.7, 2.5.3), 2026-08-22.
- https://pkg.go.dev/golang.org/x/mobile/cmd/gomobile (bind → XCFramework; targets; untagged).
- docs/research/ecosystem.md (Crush/charm stack, OpenCode split, Codex app-server);
  docs/research/dashboard.md (modernc SQLite); docs/research/subscription-auth.md (subprocess
  guidance).
