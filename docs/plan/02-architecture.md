# 2. Language & architecture — "One Wire, Many Arms"

Status: hardened on 2026-08-22 · supersedes: — · PLAN.md item 2

## Decision (the short version)

**Go stays.** One Go module at `github.com/onembyte/kolkrabbi`, one shipped binary tree under
`cmd/`, and a center of gravity that is not the CLI but **`spec/`** — a language-neutral event and
RPC contract with a Go binding in `protocol/`. Everything the engine produces flows through one
in-process event log (`internal/bus`) that has **three exits carrying byte-identical frames**:
NDJSON on stdout (`kolk -p --output stream-json`), NDJSON over a spawned child's stdio
(`kolk serve --stdio`, for a Tauri sidecar), and HTTP+SSE (`kolk serve`, for the desktop shell,
the iPad, Android, and the dashboard SPA). The CLI is therefore *a client*, not the program — which
is the only structural property that makes the four roadmap targets (Windows CLI, desktop shell,
iPad, Android) additions of a directory rather than refactors of the tree.

Everything else lives in `internal/`, which is a **same-repo fence, not a same-module fence**: a
nested module whose path is `github.com/onembyte/kolkrabbi/<sub>` may legally import
`github.com/onembyte/kolkrabbi/internal/...`, so `desktop/` and `bind/` get full access to the
engine while no foreign repository ever can. Three nested modules are pre-carved and empty:
`desktop/` (Wails **or** Tauri — same directory either way), `bind/` (gomobile facade), `tools/`
(codegen deps). They exist so the CLI's `go.mod` never acquires cgo, `golang.org/x/mobile`, or a
webview toolchain.

Layering is enforced by `internal/arch/arch_test.go` — a data-driven layer table plus a per-package
third-party allow-list — with **no `//arch:allow` escape hatch**. Migration keeps
`go build ./... && go test ./...` green at every step; all **22 tests stay passing**, and the one
step that could break them (the engine stops printing) is made safe by keeping `Options.Out` as a
retained convenience wired to a byte-identical renderer.

---

## Spec

### 1. Language verdict, dependency policy, and the cgo rule

**Go stays.** Measured on the current prototype (PLAN.md §0, verified 2026-08-21/22 on
go1.26.4 darwin/arm64):

| Metric | Today | Why it decides the question |
|---|---|---|
| LOC | 3,399 | small enough that a rewrite is cheap, large enough that the shape is proven |
| External deps | **0** | Rust/TS equivalents need a runtime or a crate tree on day one |
| Binary | **6.1 MB** static | `curl \| sh` and `go install` both work with no runtime |
| Cold start | **~10 ms** (fork+exec+run, 20 runs) | the budget in this item is 30 ms; Node/Bun start at 25–60 ms |
| Tests | **22**, fully offline via the in-process scripted OpenRouter mock | streaming SSE + tool-call reassembly already covered |
| `go vet` | clean | — |

Go wins on the four things this product actually needs: a single static cross-compiled binary,
goroutines+channels for parallel subagents over streaming SSE, `//go:embed` for the dashboard SPA,
and a stdlib HTTP/JSON/TLS stack good enough that the engine needs no dependencies at all. Rust
would buy nothing the budgets demand and cost the solo-developer velocity this project runs on.
TypeScript/Bun forfeits the single-binary and startup properties outright.

**Dependency policy — enforced by `internal/arch/layers.go` as data, tested in CI, not promised:**

| Layer | Packages | Allowed non-stdlib |
|---|---|---|
| L0 platform | `paths shell atomicfile lock term secret xid buildinfo` | `golang.org/x/sys` — **only** in `term` and `secret` `*_windows.go`/`*_unix.go` |
| L1 contract | `protocol` (public) | **none, forever** |
| L2 hinge | `bus` | **none** |
| L3 domain | `provider` (+`openrouter`, `openaicompat`, `agentcli`), `tools`, `perm` | **none** |
| L4 engine | `engine orchestrator saga` | **none** |
| L5 adapters | `config session checkpoint stats` | **none** |
| L6 surfaces | `cli` (+`render`), `tui`, `serve`, `dash` | `charm.land/*` in `tui` only · `modernc.org/sqlite` (+ pinned `modernc.org/libc`) in `dash` only |
| nested modules | `desktop/` `bind/` `tools/` | anything — they are outside the CLI's `go.mod` by construction |

So the honest claim is not "zero dependencies" but **"zero dependencies below the surface layer,
mechanically verified"** — a claim that survives growth. `scripts/check-purity.sh` additionally
asserts that no `*_windows.go` / `*_darwin.go` file and no `os/exec` import exists anywhere in
L1–L5, so "the engine touches no OS" is a build failure rather than a code-review convention.
That property is the load-bearing precondition for every constrained target on the roadmap.

**The cgo rule: `CGO_ENABLED=0` for every binary this repo ships.** `kolk`, `kolkd` and
`kolk-mock` are cgo-free on darwin, linux, windows, freebsd and android. Two scoped, written-down
exceptions, both outside the root module:

- **Wails v3 desktop** sets `CGO_ENABLED=1` on darwin and linux (its own generated Taskfiles do),
  and needs Xcode CLT / gtk4 + webkitgtk-6.0. Confined to `desktop/go.mod`.
- **`GOOS=ios` always requires cgo** — verified: `ios/arm64 requires external (cgo) linking, but
  cgo is not enabled`. `gomobile` sets `CGO_ENABLED=1` itself. Confined to `bind/go.mod`, which
  is never in the required CI matrix. An iOS build is an Xcode build by definition.

`modernc.org/sqlite` is the one heavy dependency, chosen precisely because it is **pure Go** and
therefore does not break the cgo rule; it is confined to `internal/dash` so a future `cmd/kolk-dash`
split is a 20-line `main.go`. Note its supported-platform table lists darwin/freebsd/linux/netbsd/
openbsd/windows and **neither ios nor android** — which is why the dashboard can never travel into
a gomobile build, and why `bind/` is chat-only forever.

---

### 2. The directory tree

```
kolkrabbi/                                repo root · module github.com/onembyte/kolkrabbi
│
├── go.mod                                go 1.25 · NEVER contains a `replace` (breaks go install @latest)
├── go.sum
├── LICENSE                               Apache-2.0 (patent grant; PLAN item 1)
├── README.md                             vision · quickstart · "on iPad, code mode needs a reachable daemon"
├── CHANGELOG.md                          product changelog (the protocol has its own, in spec/)
├── CONTRIBUTING.md                       incl. "run scripts/dev-workspace.sh or gopls loses cross-module nav"
├── KOLKRABBI.md                          project memory kolk reads about itself (dogfooding)
├── PLAN.md
├── Makefile                              every target enumerates ALL go.mod dirs — bare ./... is a lie here
├── .goreleaser.yaml                      CLI ONLY: builds ./cmd/kolk → kolk, ./cmd/kolkd → kolkd. CGO_ENABLED=0.
├── .golangci.yml
├── .gitignore                            /go.work* /web/dash/node_modules /kolk /kolkd *.test
│
├── spec/                                 ★ THE CONTRACT. Language-neutral. The only thing that can break a client.
│   ├── VERSION                           one line: the protocol version. "0" today, "1" at the freeze (~v0.5).
│   ├── CHANGELOG.md                      one line per change. CI fails any spec/** diff that does not edit this.
│   ├── kolk.openapi.yaml                 OpenAPI 3.1: the /v1 REST surface + text/event-stream responses
│   ├── stdio.md                          NDJSON framing. MUST be byte-identical to the SSE `data:` payloads.
│   ├── errors.md                         one table: error code ↔ HTTP status ↔ exit code ↔ retryable
│   ├── schemas/
│   │   ├── envelope.json                 {seq, ts, session, turn, type, data} — the one and only wrapper
│   │   ├── events/<event.name>.json      filename == event name == the `type` field value (one greppable string)
│   │   ├── commands/<noun.verb>.json     client→server bodies
│   │   └── entities/                     session.json model.json usage.json permission.json score.json chapter.json
│   └── testdata/                         ★ the cross-language conformance suite; Go now, Swift/Kotlin/TS later
│       ├── events/*.json                 one golden frame per event type
│       ├── streams/*.ndjson              whole turns: code-turn · permission-denied · agent-fanout ·
│       │                                 saga-chapter · resume-after-drop
│       └── foreign/*.ndjson              REAL captured `claude -p --output-format stream-json` and
│                                         `codex … --json` output — the agentcli adapters' input fixtures
│
├── protocol/                             ★ THE ONLY PUBLIC GO PACKAGE besides cmd/. Hand-written, stdlib-only.
│   ├── doc.go                            "spec/ is the source of truth; this is one language's view of it"
│   ├── version.go                        const Version (mirrors spec/VERSION) · var Capabilities []string
│   ├── event.go  events.go               Envelope + one struct per event type
│   ├── command.go  entity.go             client→server commands · shared entities
│   ├── codec.go                          Encode / Decode / DecodeStream (NDJSON frames and SSE data: frames)
│   ├── errors.go                         typed errors + Retryable() (mirrors spec/errors.md)
│   └── conform_test.go                   ★ round-trips EVERY file in ../spec/testdata. The drift alarm.
│
├── cmd/                                  binary name == LAST ELEMENT OF THE DIRECTORY, not the module path
│   ├── kolk/main.go                      ~40 lines: os.Args → internal/cli.Main(ctx, args) → os.Exit(code)
│   ├── kolkd/main.go                     ~25 lines: internal/serve.Mux() only. No TUI, no term deps.
│   │                                     The Tauri sidecar / systemd / container binary.
│   └── kolk-mock/main.go                 dev-only scripted OpenRouter mock (was cmd/mockserver)
│
├── internal/                             everything else. Private by Go's rule — but reachable from
│                                         desktop/, bind/ and tools/ (path-prefix children; see §5).
│   ├── arch/
│   │   ├── layers.go                     the layer table + per-package third-party allow-list, AS DATA
│   │   └── arch_test.go                  fails CI on: upward import · unapproved dep · os/exec outside shell ·
│   │                                     os.UserHomeDir/UserConfigDir outside paths · a *_unix.go with no
│   │                                     //go:build line · bind/ reaching an OS package. NO escape hatch.
│   │
│   │   ══ L0 platform — the ONLY layer that knows what an OS is ══
│   ├── paths/     paths.go · dirs_unix.go · dirs_darwin.go · dirs_windows.go · migrate.go
│   ├── shell/     shell.go (Shell iface) · exec_unix.go · exec_windows.go · kill_unix.go ·
│   │              kill_windows.go · lookpath.go (wraps exec.ErrDot legibly)
│   ├── atomicfile/ write_unix.go · write_windows.go
│   ├── lock/      lock_unix.go (flock) · lock_windows.go (LockFileEx)
│   ├── term/      term.go · vt_unix.go · vt_windows.go (ENABLE_VIRTUAL_TERMINAL_PROCESSING)
│   ├── secret/    secret.go (Store iface + Redact) · keyring_darwin.go (/usr/bin/security, no cgo) ·
│   │              keyring_windows.go (Credential Manager) · keyring_unix.go (Secret Service) · file.go (0600)
│   ├── xid/       monotonic sortable ids for sessions / turns / events
│   ├── buildinfo/ Version · Commit · Date via -ldflags -X, with a debug.ReadBuildInfo() fallback
│   │
│   │   ══ L2 hinge (L1 is protocol/, above — public) ══
│   ├── bus/                              ★ per-session append-only event log + fan-out + replay
│   │   ├── bus.go                        Publish(Event) assigns a monotonic seq; Subscribe(fromSeq) → chan
│   │   ├── log.go                        bounded ring + spill file. Serves Last-Event-ID and ?from= identically.
│   │   └── bus_test.go                   slow consumer · dropped subscriber · resume-from-cursor · ordering
│   │
│   │   ══ L3 domain ══
│   ├── provider/
│   │   ├── provider.go                   Chat iface + the canonical Message / Tool / ToolCall / FunctionCall / Meta
│   │   ├── registry.go                   name → constructor + capability matrix (tools? reasoning? cost?)
│   │   ├── catalog/                      ★ model profiles + pricing: cached HTTP doc + //go:embed seed +
│   │   │                                 user override, 12 h mtime TTL, never on the startup path (item 3 §5)
│   │   ├── openrouter/                   client.go stream.go models.go pricing.go errors.go oauth.go
│   │   ├── openaicompat/                 one engine + a data-only Dialect table: Ollama · LM Studio · vLLM ·
│   │   │                                 llama.cpp · LiteLLM · Vercel AI Gateway · generic (item 3 §7)
│   │   └── agentcli/                     ★ external agent CLI backends — spawns the user's OWN logged-in binary
│   │       ├── agentcli.go               spawn (via L0 shell) · stdio pump · capability probe · redaction
│   │       ├── claude.go  codex.go       vendor NDJSON → protocol.Event, as PURE FUNCTIONS
│   │       ├── detect.go                 pure: candidate paths in, choice out — OS lookup is L0 shell's job,
│   │       │                             so this file needs no _unix/_windows twin and stays testable
│   │       └── translate_test.go         replays spec/testdata/foreign/** — offline forever, no vendor binary
│   │                                     (real captured fixtures already committed, 2026-08-22)
│   ├── tools/     registry.go bash.go fs.go edit.go list.go search.go web.go  (+ tools_test.go, unchanged)
│   ├── perm/      allow/ask/deny globs · last-match-wins · hardline blocklist that survives --yolo · doom-loop
│   │
│   │   ══ L4 engine ══
│   ├── engine/
│   │   ├── engine.go  mode.go  effort.go  prompt.go  repair.go
│   │   ├── port.go                       ★ THE INJECTION INTERFACES: Provider · ToolSet · Recorder ·
│   │   │                                 SessionStore · Checkpointer · Decider · Clock. Engine reads
│   │   │                                 no file and no env var.
│   │   ├── decider.go                    replaces the bufio stdin read: TTY prompt | protocol round-trip | auto
│   │   ├── runner.go                     type Runner — breaks the cycle so orchestrator/saga → engine, never back
│   │   ├── events.go                     the only engine file that constructs a protocol.Event
│   │   └── engine_test.go                the 5 offline e2e tests
│   ├── orchestrator/ orchestrator.go plan.go subagent.go route.go synth.go worktree.go
│   ├── saga/         saga.go chapter.go gate.go budget.go progress.go (SAGA.md)
│   │
│   │   ══ L5 adapters (implement engine ports) ══
│   ├── config/    config.go schema.go merge.go migrate.go doctor.go resolve.go
│   ├── session/   session.go (own persisted Message type) · repair.go · testdata/v0-session.json
│   ├── checkpoint/ checkpoint.go · git.go (later: shadow-git for bash-made changes)
│   ├── stats/     stats.go — stats.jsonl raw log, a bus subscriber, implements engine.Recorder
│   │
│   │   ══ L6 surfaces ══
│   ├── cli/
│   │   ├── cli.go                        Main() + THE COMMAND TABLE (one source: help + completions + parity)
│   │   ├── repl.go  slash.go             slash.go dispatches into the SAME handlers as cmd_*.go →
│   │   │                                 the item-9 parity rule holds by construction, not by discipline
│   │   ├── cmd_config.go cmd_models.go cmd_sessions.go cmd_stats.go cmd_serve.go cmd_dash.go
│   │   ├── cmd_saga.go cmd_login.go cmd_doctor.go cmd_version.go cmd_update.go
│   │   ├── streamjson.go                 ★ EXIT #1: bus → NDJSON on stdout
│   │   ├── prompt.go                     TTY implementation of engine.Decider
│   │   ├── exit.go                       0 ok · 1 error · 2 usage · 3 budget · 130 interrupt
│   │   └── render/  plain.go  ansi.go  diff.go  markdown.go  (+ testdata/golden/*.txt)
│   ├── tui/                              RESERVED. Bubble Tea v2 — the ONLY package allowed charm.land deps.
│   ├── serve/                            ★ EXITS #2 and #3
│   │   ├── serve.go                      Mux(Options) http.Handler — importable by cmd/kolk, cmd/kolkd, Wails
│   │   ├── rest.go  sse.go               id: on every event · retry: · Last-Event-ID · `: ping` every 15 s
│   │   ├── stdio.go                      `kolk serve --stdio` — identical frames over a pipe (Tauri sidecar)
│   │   ├── auth.go  permission.go        bearer token from day one · permission.requested/resolved + timeout
│   │   ├── listen_unix.go  listen_windows.go   tcp + unix socket / tcp + named pipe
│   │   └── conform_test.go               drives the real server, diffs against spec/testdata/streams/*.ndjson
│   ├── dash/                             ★ EMBEDDED DASHBOARD ASSETS LIVE HERE, and nowhere else
│   │   ├── embed.go                      //go:embed all:dist   ← must be in THIS directory (see below)
│   │   ├── dist/index.html               COMMITTED PLACEHOLDER with a sentinel string, so a clean clone
│   │   │                                 builds with no Node; release.yml greps for the sentinel and fails
│   │   ├── store.go migrate.go migrations/000N_*.sql   modernc.org/sqlite, WAL, busy_timeout=5000
│   │   ├── ingest.go                     bus subscriber + stats.jsonl importer
│   │   ├── query.go  export.go           the 5 v1 views · CSV + OTLP export
│   │   └── handler.go                    /dash/* and /v1/stats/* mounted on the SAME mux as serve
│   │
│   │   ══ test kit ══
│   ├── enginetest/ router.go (was mockrouter) · steps.go · fakes.go (in-memory ports incl. Clock) · golden.go
│   └── mockagent/                        fake `claude`/`codex` binaries replaying spec/testdata/foreign/**
│
├── web/dash/                             SPA SOURCE. Node lives here, far from the Go tree.
│   └── src/ index.html package.json vite.config.ts
│       # `vite build --outDir ../../internal/dash/dist` — the ONLY legal shape (see §2 note)
│
├── desktop/                              ★ NESTED MODULE, added later. SAME DIRECTORY FOR WAILS OR TAURI.
│   ├── README.md                         the deferral record + both attach recipes, written out now
│   │  [Wails]  go.mod (module …/kolkrabbi/desktop + replace ../) · main.go · frontend/ ·
│   │           build/{config.yml,darwin,windows,linux}/Taskfile.yml
│   │  [Tauri]  src-tauri/{tauri.conf.json,tauri.<os>.conf.json,capabilities/,binaries/} · web/
│
├── bind/                                 ★ NESTED MODULE. Optional, opt-in, never in required CI.
│   ├── go.mod                            module …/kolkrabbi/bind + replace ../   ← the replace is LOad-BEARING
│   └── kolkmobile/                       gomobile facade on a PUBLIC path (gobind cannot see internal/)
│       ├── engine.go  golden_test.go     asserts the GENERATED API surface (gobind drops silently)
│       └── import_test.go                asserts the transitive import graph never reaches shell/tools/agentcli/dash
│
├── clients/                              protocol clients, generated from spec/kolk.openapi.yaml
│   ├── swift/  kotlin/  ts/  README.md   "every client reads spec/testdata/** as its own test corpus"
│
├── packaging/                            shell-agnostic assets — neither Wails nor Tauri owns these
│   ├── icons/{kolk.svg,kolk.png,kolk.icns,kolk.ico}  entitlements.plist
│   ├── linux/{kolk.desktop,kolkd.service}  completions/{bash,zsh,fish}  homebrew/kolk.rb.tmpl
│   └── install.sh                        the `curl | sh` path
│
├── tools/                                ★ NESTED MODULE: codegen + lint deps, keeps the root go.mod honest
│   ├── go.mod  tools.go                  //go:build tools
│   └── cmd/specgen/                      spec/schemas/*.json → protocol/*.go skeletons + docs/protocol/*.md
│
├── scripts/
│   ├── test.sh                           `go test ./...` for EVERY go.mod in the repo. Use this, never bare ./...
│   ├── check-arch.sh  check-purity.sh  check-buildtags.sh  check-spec.sh
│   ├── check-budgets.sh                  binary size + cold start + test-count floor
│   ├── build-ui.sh                       web/dash → internal/dash/dist; fails loudly if the output is empty
│   └── dev-workspace.sh                  writes a GITIGNORED go.work so gopls sees all modules as one build
│
├── docs/
│   ├── plan/                             NN-slug.md, one per PLAN.md item (template: docs/plan/README.md)
│   │   ├── README.md  01-identity-release.md  02-architecture.md  …  23-roadmap.md
│   ├── research/                         dated snapshots (exists: platform-strategy, ecosystem, dashboard, …)
│   │   └── constraints/                  the three verified 2026-08-22 constraint reports behind this doc
│   ├── adr/                              NNNN-slug.md — REVERSALS and structural decisions only
│   ├── protocol/                         human-readable protocol guide, generated by tools/cmd/specgen
│   ├── reference/                        commands.md (generated) · config.md · modes.md · effort.md · saga.md
│   └── contributing/                     workspace.md ("why bare ./... lies to you") · release.md
│
└── .github/
    ├── workflows/ci.yml                  {ubuntu, macos, windows} × scripts/test.sh + arch + purity +
    │                                     buildtags + budgets + go vet + golangci-lint
    ├── workflows/spec.yml                paths: spec/** → lint + conformance + CHANGELOG gate
    ├── workflows/release.yml             tag v* → goreleaser (kolk + kolkd; OSS tier only)
    ├── workflows/ui.yml                  web/dash build + "is internal/dash/dist still the placeholder?"
    ├── workflows/desktop.yml.disabled    RESERVED: native runners per OS → wails3 package | tauri build
    ├── workflows/mobile.yml.disabled     RESERVED: workflow_dispatch only — gomobile bind smoke + golden_test
    └── dependabot.yml                    one entry per go.mod + one per package.json
```

**Why the embedded dashboard assets are at `internal/dash/dist/` and can be nowhere else.** The
`embed` package interprets patterns **relative to the directory of the source file containing the
directive**, and: patterns may not contain `..`, may not begin with `/`, may not cross a `go.mod`,
and **symlinks are refused** (`cannot embed irregular file`). So `//go:embed ../../web/dash/dist`
is a compile error, and symlinking a built SPA into place is a compile error. The JS build must
*output into the embedding package's directory*. Two hard consequences:

- Use `//go:embed all:dist`, never `//go:embed dist`. Without `all:`, files and directories whose
  names begin with `.` or `_` are excluded — you would silently ship a dashboard missing every
  `_next/` and `.vite/` chunk, **with no build error**.
- `internal/dash/dist/index.html` is **committed** as a sentinel placeholder. A fresh clone, and
  every `go install …@latest` (which builds from the module-proxy zip on a machine that has no
  Node), must compile without the JS toolchain. `release.yml` greps for the sentinel and fails the
  release if the real SPA was not built over it.

---

### 3. Module strategy

**Module path: `github.com/onembyte/kolkrabbi`.** Today it is bare `kolkrabbi` — no dot in the
first path element, so it is unresolvable and `go install` can **never** work. Fixing it costs
**24 import lines across 9 files** (counted, 2026-08-22) — one sed. It is a 30-second change now
and a breaking change for every user later.

**`go 1.25`.** Not conservatism: Go 1.24 gives `os.Root`/`os.OpenRoot`, which is exactly the
path-jail-to-project-root that PLAN item 13 wants, and 1.25 is already the floor Wails v3 declares
for development — so if `desktop/` is ever folded back in there is no version fight.

**One root module + three nested modules that ship nothing:**

| dir | module path | why nested | tagged? |
|---|---|---|---|
| `.` | `github.com/onembyte/kolkrabbi` | the product | `vX.Y.Z` |
| `desktop/` | `…/kolkrabbi/desktop` | Wails v3 needs `CGO_ENABLED=1`, gtk4/webkitgtk, ~35 transitive deps, ~15 MB output. None of it may touch the CLI's graph. (Tauri variant: no `go.mod` at all.) | never |
| `bind/` | `…/kolkrabbi/bind` | **mandatory**: `gomobile bind` hard-fails unless `golang.org/x/mobile` is resolvable through the *invoking* module (`mobileModuleAvailable()` → "missing golang.org/x/mobile dependency"). Putting the facade in the root module would poison `cmd/kolk`'s `go.mod` with x/mobile and its x/tools graph. | never |
| `tools/` | `…/kolkrabbi/tools` | codegen/lint deps quarantined from the root graph | never |

**`replace ../` in `desktop/go.mod` and `bind/go.mod` is load-bearing — do not "tidy" it away.**
`gomobile` synthesizes its build module from `go list -m -json all` and emits a local `replace`
only when a module reports an empty `Version` or a directory `Replace`; otherwise it emits a
`require` that must resolve from the proxy. With the `replace`, `gomobile bind` works **offline,
on uncommitted work**. Without it, you can only bind published tags. A `go.work` does not
substitute: gobind is synthesized in `TMPDIR`, outside any workspace. A comment saying exactly
this goes at the top of both `go.mod` files.

**No `replace` in the root `go.mod`, ever.** `go install <mod>/cmd/x@VERSION` hard-fails on any
module whose `go.mod` carries one ("*It must not contain directives that would cause it to be
interpreted differently than if it were the main module*"). This is why gopls strips its `replace`
at every release tag. CI gate: `! grep -q '^replace' go.mod`.

**No committed `go.work`.** The module reference calls committing one "generally inadvisable" — it
overrides a contributor's own workspace and can make CI test the wrong versions.
`scripts/dev-workspace.sh` writes a gitignored one so gopls sees all four modules as one build
(without it gopls guesses a build per open file and cross-module navigation is incomplete).
`go install pkg@version` ignores `go.work` entirely (`RootMode = NoRoot`), so this can never reach
a user.

**How `go install` works** (verified end-to-end):

```
go install github.com/onembyte/kolkrabbi/cmd/kolk@latest    →  $GOBIN/kolk
go install github.com/onembyte/kolkrabbi/cmd/kolkd@latest   →  $GOBIN/kolkd
```

The binary name is the **last element of the command directory**. A root `package main` — the
crush/terraform/hugo shape — takes its name from the *module path* and would produce a binary
called `kolkrabbi`. **`cmd/kolk/main.go` is therefore mandatory, not stylistic.** `@latest`
resolves a pseudo-version off the default branch before any tag exists, so the install line works
from the first push.

**Versioning and tags.** Plain semver `vX.Y.Z` on the root module — plain, because with one shipped
module there is nothing to prefix. Subdirectory-module tag prefixes (`desktop/v0.1.0`,
`bind/v0.1.0`) are reserved and unused; a nested module released from this repo would *require*
them, since that is how the go command resolves a nested module's versions. **The protocol version
is not a git tag**: it lives in `spec/VERSION` (§7).

**GoReleaser stays on the OSS tier.** Monorepo mode, the `prebuilt` importer, `.app`/`.dmg`/`.msi`/
NSIS bundling and native bundle notarization are all Pro ($165/yr). One `.goreleaser.yaml` emits
`kolk` and `kolkd` from the one root module — including cross-platform notarization of the *bare*
binaries, which OSS does support. Desktop bundles go through `desktop.yml` on native runners
instead, and GoReleaser is never asked to do them.

---

### 4. Every prototype file's new home

| Today | New home | Note |
|---|---|---|
| `go.mod` (`module kolkrabbi`, go 1.22.2) | `go.mod` (`github.com/onembyte/kolkrabbi`, go 1.25) | step 1 |
| `main.go` (606 L) | split, see rows below; `cmd/kolk/main.go` keeps ~40 lines | |
| `main.go:32-40` `configDir` / `sessionsDir` | `internal/paths/paths.go` + `dirs_{unix,darwin,windows}.go` | the hardcoded `~/.config/kolk` dies here |
| `main.go:42` `main()` | `cmd/kolk/main.go` → `internal/cli.Main(ctx, args)` | |
| `main.go:201` `resolveBaseURL` | `internal/config/resolve.go` | |
| `main.go:214` `runREPL` | `internal/cli/repl.go` | |
| `main.go:254` `handleSlash`, `:371` `yoloTag` | `internal/cli/slash.go` | dispatches into the same handlers as `cmd_*.go` |
| `main.go:378` `runConfigCmd`, `:453` `saveCfg` | `internal/cli/cmd_config.go` | |
| `main.go:459` `runStatsCmd` | `internal/cli/cmd_stats.go` | calls `stats.Aggregate`/`Render`, which stay put |
| `main.go:482` `runSessionsCmd` | `internal/cli/cmd_sessions.go` | |
| `main.go:520` `runModelsCmd`, `:542` `formatPricing` | `internal/cli/cmd_models.go` | |
| `main.go:554` `maskKey` | `internal/secret/redact.go` | becomes `secret.Redact`, applied to sessions/stats/logs too |
| `main.go:474` `printJSON`, `:561` `orDefault`, `:603` `fatal` | `internal/cli/cli.go`, `internal/cli/exit.go` | |
| `main.go:568` `printUsage` | `internal/cli/cli.go` — the command table | generates help + completions + `docs/reference/commands.md` |
| `cmd/mockserver/main.go` | `cmd/kolk-mock/main.go` | `kolk-` prefix keeps `$PATH` clean |
| `internal/api/client.go` wire structs (`chatRequest`, `streamChunk`, `wireUsage`, `streamOptions`, `usageInclude`) | `internal/provider/openrouter/` (stay unexported) | |
| `internal/api/client.go` exported types (`Message`, `ToolCall`, `FunctionCall`, `Tool`, `FunctionDef`, `Meta`, `ModelInfo`) | `internal/provider/provider.go` | the canonical in-memory conversation types |
| `internal/api/client.go` `Client`, `NewClient`, `StreamChat`, `ListModels` | `internal/provider/openrouter/{client,stream,models}.go` | |
| `internal/api/client_test.go` (2 tests) | `internal/provider/openrouter/stream_test.go` | **assertions unchanged** — the SSE-fragmentation coverage is the most valuable thing in the suite |
| `internal/agent/agent.go` core | `internal/engine/{engine,mode,effort,prompt}.go` | |
| `agent.go:27-36` ANSI consts, `:305` `footer` | `internal/cli/render/plain.go` | byte-identical output; this is what keeps the e2e tests green |
| `agent.go:186` `repairDanglingToolCalls` | `internal/engine/repair.go` | |
| `agent.go:223` `record` | emits `usage.reported`; `internal/stats` subscribes to the bus | |
| `agent.go:272` `confirm` (reads stdin) | `internal/engine/decider.go` (interface) + `internal/cli/prompt.go` (TTY impl) | the single largest behavioural change |
| `agent.go:290` `preWrite`, `:380` `Rewind` | `internal/engine/port.go` (`Checkpointer`) + `internal/checkpoint` | |
| `internal/agent/orchestrator.go` | `internal/orchestrator/{orchestrator,plan,subagent,route}.go` | behind `engine.Runner`, so orchestrator→engine is one-directional |
| `internal/agent/agent_test.go` (5 e2e tests) | `internal/engine/engine_test.go` | **assertions unchanged** (see migration step 6) |
| `internal/tools/tools.go` `Definitions`, `Execute`, `schema`, `truncate` | `internal/tools/registry.go` | `Execute`'s signature is **unchanged** |
| `internal/tools/tools.go` the 5 tool bodies | `internal/tools/{bash,fs,edit,list}.go` | |
| `internal/tools/tools.go:119` `exec.CommandContext(cctx,"bash","-c",…)` | `internal/shell/exec_unix.go` | **the single Windows blocker in the prototype** |
| `internal/tools/tools.go` `Confirm`, `PreWrite` types | stay in `internal/tools` | so all 5 tool tests are untouched |
| `internal/tools/tools_test.go` (5 tests) | unchanged, in place | |
| `internal/session/session.go` | `internal/session/session.go` | |
| `session.go:26` `Messages []api.Message` | `Messages []session.Message` + conversion at the store boundary | **the leak this doc fixes** — see §5 |
| `session.go:51,64` tmp + `os.Rename` | `internal/atomicfile` | atomic on POSIX, fragile on Windows against an open target |
| `internal/session/session_test.go` (3 tests) | unchanged + **new** `internal/session/testdata/v0-session.json` | on-disk-format regression fixture, committed *before* the type move |
| `internal/checkpoint/*` (4 tests) | `internal/checkpoint/*` — verbatim | |
| `internal/stats/stats.go` (`Record`, `Append`, `Load`, `Aggregate`, `Render`, `ModelRow`) | `internal/stats/stats.go` | gains a bus subscriber; implements `engine.Recorder`; **nothing renamed** |
| `internal/stats/stats_test.go` (3 tests) | unchanged, in place | |
| `internal/config/config.go` | `internal/config/{config,schema,merge,migrate}.go` | |
| `config.go:18` `dir()` | delegates to `internal/paths` | |
| `internal/mockrouter/mockrouter.go` | `internal/enginetest/router.go`, package `enginetest` | grows `fakes.go` (in-memory ports incl. `Clock`) and `golden.go` |

**Renames, and only these.** `internal/api` → `internal/provider` (because `api` becomes hopelessly
ambiguous the moment `kolk serve` exposes an actual HTTP API), `internal/agent` → `internal/engine`
(an "agent" is a product concept; the package is a turn loop), `internal/mockrouter` →
`internal/enginetest`, `cmd/mockserver` → `cmd/kolk-mock`. **`tools`, `session`, `checkpoint`,
`stats`, `config` keep their names** — churn without payoff is not the goal, and PLAN.md's
ground-truth table, README and `docs/research/*` get a find-and-replace pass in the **same commit**
so the ground truth never lags the tree.

---

### 5. Layering rules and the `internal/` boundary

**Allowed import direction — strictly downward. `internal/arch/arch_test.go` enforces it.**

| Layer | Packages | May import |
|---|---|---|
| **L0 platform** | `paths shell atomicfile lock term secret xid buildinfo` | stdlib only (+`x/sys` in `term`/`secret` OS files) |
| **L1 contract** | `protocol` *(public)* | **stdlib only. Never `internal/…`.** |
| **L2 hinge** | `bus` | L0, L1 |
| **L3 domain** | `provider` (+`openrouter`, `openaicompat`, `agentcli`), `tools`, `perm` | L0, L1, L2 |
| **L4 engine** | `engine orchestrator saga` | L0, L1, L2, L3 — **and no adapter, ever** |
| **L5 adapters** | `config session checkpoint stats` | L0, L1, L3, **L4** (only to implement its port interfaces) |
| **L6 surfaces** | `cli` (+`render`), `tui`, `serve`, `dash` | everything. Surfaces do the wiring. |

Three rules do the real work:

1. **The engine imports no adapter.** `internal/engine/port.go` declares `Provider · ToolSet ·
   Recorder · SessionStore · Checkpointer · Decider · Clock`; `internal/cli` and `internal/serve`
   construct the concrete `session`/`checkpoint`/`stats`/`config` implementations and inject them.
   `internal/enginetest/fakes.go` provides in-memory versions of all seven — including `Clock`,
   which is what makes budgets, timeouts and TTFT deterministic under test. The prototype's
   `Options{Sess, Ckpt, StatsDir, Out, In}` becomes `Options{Ports…, Bus}`.
2. **The provider wire type is not the on-disk format.** Today `session.Session.Messages` is
   `[]api.Message` — the OpenRouter message shape *is* the transcript format on disk, in a product
   whose premise is many providers plus external CLI backends whose shapes differ. `internal/session`
   gets its own `session.Message` with frozen JSON tags and converts at the store boundary;
   `internal/provider` owns the canonical in-memory type; the OpenAI-specific wire structs stay
   unexported inside `internal/provider/openrouter`. `testdata/v0-session.json` — captured from the
   prototype *before* the move — proves old session files still load. (That fixture exists to prove
   the migration reads old files, **not** to freeze the old shape.)
3. **Only `internal/engine/events.go` constructs a `protocol.Event` inside the engine.** The one
   documented exception is `internal/provider/agentcli`, whose entire job is translating a foreign
   frame into a kolk frame; the layer table lists it explicitly so the rule and the tree agree.

`arch_test.go` also fails CI on: `os/exec` imported outside `internal/shell`;
`os.UserHomeDir`/`os.UserConfigDir` called outside `internal/paths`; a third-party import not on the
per-package allow-list; any `*_unix.go` / `*_other.go` / `*_posix.go` lacking a `//go:build` line;
and any `bind/kolkmobile` transitive import reaching `internal/shell`, `internal/tools`,
`internal/provider/agentcli` or `internal/dash`. **There is no `//arch:allow` escape hatch** — a
violation is fixed by editing `layers.go`, which is a reviewed data file, or by fixing the import.
An escape hatch is a comment that gets typed at 1 a.m.; a data file is a decision.

**The `internal/` boundary: everything except `protocol/` is private, and that is deliberate.**
Go's rule is checked against the **importer's import path prefix**, not the module (verified today:
a nested module at `example.com/onembyte/kolkrabbi/bind` imported the root's `internal/engine` and
built clean; a module named `gobind` importing the same path got
`use of internal package … not allowed`). So:

- **`desktop/` and `bind/` and `tools/` can import every `internal/` package** — they are
  path-prefix children of the root module, and on GitHub a module path under
  `github.com/onembyte/kolkrabbi/…` can only resolve to a subdirectory of that repo. No foreign
  code can claim it.
- **A desktop shell in a different repo or org could not** — it would get only `protocol/`. If the
  desktop app is ever handed to a collaborator with their own repo, the answer is the daemon path.
- **`gomobile` cannot see `internal/` at all**, because it synthesizes a module *literally named*
  `gobind` (`f.AddModuleStmt("gobind")`). Hence `bind/kolkmobile` is a **public path** that may
  freely import internals but may never *be* one. Verified both directions today.
- **Exporting the engine is refused.** It would commit the largest and most volatile surface in the
  repo to Go's compatibility expectations in exchange for zero consumers. `protocol/` is the whole
  export list, it is cheap to version, and at v0.x it can still be broken freely.

---

### 6. Platform expansion

Every target is **adding** a directory or a `_windows.go` twin. None moves an existing file.

#### 6a. Windows CLI (soon)

Zero structural change. `windows-latest` joins the CI matrix at **migration step 3, before any
restructuring** — you never want to be reshaping a tree whose CI status is unknown. Expect a red
baseline (`internal/tools/tools_test.go` shells out to bash; tag it `//go:build !windows` and record
the honest number) and close it file by file:

| Divergence | File | Prototype origin |
|---|---|---|
| shell execution | `internal/shell/exec_windows.go` (`powershell -NoProfile -Command`, fallback `cmd.exe /c`; quoting rules diverge entirely) | `internal/tools/tools.go:119`, hardcoded `bash -c` |
| process teardown | `internal/shell/kill_windows.go` (job objects) | — |
| `.`-relative binaries | `internal/shell/lookpath.go` — `errors.Is(err, exec.ErrDot)` with a legible message | — |
| config / data / cache dirs | `internal/paths/dirs_windows.go` | `main.go:32-40`, hardcoded `~/.config/kolk` |
| atomic replace | `internal/atomicfile/write_windows.go` | `internal/session/session.go:51,64` |
| file locking | `internal/lock/lock_windows.go` (`LockFileEx`) | — |
| ANSI / VT enable | `internal/term/vt_windows.go` (`SetConsoleMode` + `ENABLE_VIRTUAL_TERMINAL_PROCESSING`) — no stdlib API exists, so this is where `golang.org/x/sys` enters, isolated so daemon/desktop/mobile never link it | — |
| keychain | `internal/secret/keyring_windows.go` (Credential Manager) | — |
| daemon listener | `internal/serve/listen_windows.go` (named pipe instead of unix socket) | — |
| agent-CLI binary discovery | `internal/shell/lookpath.go` (L0) — `agentcli/detect.go` stays pure and OS-free | — |

Then: `windows/{amd64,arm64}` in `.goreleaser.yaml` (one line — `CGO_ENABLED=0` already), and the
`hello` event's capabilities drop `shell:posix` and gain `shell:pwsh`, so a client knows without
probing.

#### 6b. Desktop — **both Wails v3 and Tauri v2 stay open**

`desktop/` is one directory under either choice; only its children differ. **Reused unchanged:
everything.** Neither shell needs a single line of Go moved, because `internal/serve.Mux()` is both
importable *and* spawnable from day one — which is precisely where the two shells diverge.

- **Wails v3 path.** `desktop/go.mod` (module `…/kolkrabbi/desktop`, `replace ../`), `desktop/main.go`,
  `desktop/frontend/`, `desktop/build/{config.yml,darwin,windows,linux}/`. A Wails "service" is a
  plain Go struct, and `wails3 generate bindings` accepts arbitrary package patterns — so it wraps
  `serve.Mux()` **in-process, with no socket at all**. Critically, `desktop/` being its **own module
  root** retires the one genuinely unverified risk in the research (a Wails v3 main package in a
  *non-root* directory of an existing module is undocumented and observed in no repository): here the
  main sits at its module root, which is the documented shape both real Wails-in-a-Go-repo
  precedents use, and the generated Taskfile's `go build … -o out` (no package argument) works
  unmodified. Cost — `CGO_ENABLED=1` on darwin/linux, gtk4 + webkitgtk-6.0, Go 1.25+, ~35 transitive
  deps, ~15 MB — is entirely inside `desktop/go.mod` and never touches `kolk`.
- **Tauri v2 path.** `desktop/src-tauri/` (Rust; the `desktop` Go module is simply never created)
  plus `desktop/web/`. `bundle.externalBin: ["binaries/kolkd"]`; CI stages triple-named copies at
  `desktop/src-tauri/binaries/kolkd-aarch64-apple-darwin`, `kolkd-x86_64-pc-windows-msvc.exe`, …
  The bundler strips the triple on copy and, on macOS, places each sidecar in
  `<App>.app/Contents/MacOS/` and signs it **inside-out** — Tauri notarizes the Go binary for free.
  The shell spawns `kolkd --stdio` and reads `internal/serve/stdio.go`'s NDJSON.

**What defers the choice:** nothing in the Go tree. `spec/stdio.md` already mandates that a spawned
child's frames are byte-identical to the SSE `data:` payloads — a Tauri sidecar is stdio-attached
while mobile is necessarily remote, so that constraint had to be settled *now*, and it is the only
one either shell imposes. Shared assets (icon source, entitlements, `.desktop`, systemd unit) live
in `packaging/`, so the shell choice never owns them.

**What triggers it:** (i) a 30-minute Wails v3 spike on beta ≥ .12 that builds a window, links
`serve.Mux()` in-process and packages on all three OSes; **or** (ii) Wails v3 reaching GA (no date
is published anywhere in its repo; it has been pre-GA for 3.5 years); **or** (iii) the first time a
desktop-only feature needs native menus/tray/notifications badly enough to matter. Until one fires,
`desktop/README.md` holds both recipes and `docs/adr/0003-desktop-shell-choice.md` does not exist.

Release pipelines split, knowingly: `.goreleaser.yaml` (OSS) ships `kolk` and `kolkd`;
`.github/workflows/desktop.yml` runs on native runners calling `wails3 task <os>:package` or
`tauri build`. GoReleaser OSS cannot build `.app`/`.dmg`/`.msi`/NSIS, cannot notarize a bundle, and
cannot even import prebuilt artifacts — so it is never asked to.

#### 6c. iPad / iOS — a thin client, permanently

**App Review 2.5.2 is the constraint, quoted:** *"Apps should be self-contained in their bundles,
and may not read or write data outside the designated container area, nor may they download,
install, or execute code which introduces or changes features or functionality of the app,
including other apps."* Corroborated at the SDK level: the iOS `Foundation.framework` headers have
**no `NSTask.h`**, and `Process` is macOS / Mac Catalyst only. **So `bash`, builds, tests, linters,
stdio MCP servers, and spawning `claude`/`codex` are impossible on-device and always will be.**
The README says so up front rather than letting users discover it.

The trap to know: `fork`, `vfork` and `execve` *are* declared in the iOS `unistd.h`, marked only
`__WATCHOS_PROHIBITED __TVOS_PROHIBITED`. Go code that spawns shells **builds clean for
`ios/arm64`** and is denied at runtime and at review. A successful build proves nothing — which is
exactly why `bind/kolkmobile/import_test.go` asserts the import graph instead.

**Directories that appear:** `clients/swift/` (SwiftPM package generated from
`spec/kolk.openapi.yaml` by `apple/swift-openapi-generator`). **Go code reused unchanged: all of
it.** Nothing in the root module changes.

Transport is **HTTP+SSE, not WebSocket**, and the reason is codegen, not taste: OpenAPI 3.1 can
describe `text/event-stream` and **cannot describe WebSocket at all**, and Apple ships the
generator (`OpenAPIRuntime`'s `asDecodedServerSentEvents`, iOS 13+ / macOS 10.15+). WebSocket would
mean hand-writing and hand-maintaining the Swift *and* Kotlin clients forever. It stays available
later as an *additional* transport carrying the same frames — never as a replacement.

#### 6d. Android — the same thin client

`clients/kotlin/` from the same `spec/kolk.openapi.yaml` (openapi-generator 7.x + `okhttp-sse`, or a
Ktor Multiplatform client shared with Swift — worth considering for a solo developer, since
`ktor-client-darwin` covers iOS too). `android/arm64` cross-compiles clean from this prototype today
with `CGO_ENABLED=0`, but a *local* daemon is still not the product: Android 10's W^X rule bans
`execve` on files in the app home directory; `dataSync` foreground services are capped at 6 hours
per 24 with an `onTimeout` kill, `shortService` is ~3 minutes, and `specialUse` is reviewed by a
human in the Play Console; and shipping a toolchain in an APK is the Termux problem.

The one thing Android can have that iOS cannot is **offline chat mode**, via `bind/` compiled to an
AAR (a JNI `.so` inside the APK — no `exec` anywhere, fully policy-clean; NDK ≥ r28 for Android 15's
16 KB page alignment). It is an option, not a plan. Its facade is forced to be **the daemon protocol
in-process**, because gobind's type vocabulary is bool / string / numeric / `[]byte` /
pointer-to-bound-struct / named-interface **and nothing else** — no maps, no non-byte slices, no
structs by value, no `context.Context`, no named non-struct types, at most two results with `error`
second. Not one exported signature in the current prototype survives that filter. So:

```go
package kolkmobile // bind/kolkmobile — a PUBLIC path; gobind cannot see internal/

type EventSink interface {          // named interface, string-only methods
	OnEvent(jsonLine string)        // one protocol frame, JSON-encoded
	OnError(message string)
	OnDone()
}

type Engine struct{ /* unexported */ }

func NewEngine(configJSON string) (*Engine, error)
func (e *Engine) Send(requestJSON string, sink EventSink) (turnID string, err error)
func (e *Engine) Cancel(turnID string) error   // there is no context.Context across a JNI/ObjC bridge
func (e *Engine) Close() error
```

Two hazards, both handled: gobind **silently drops** unsupported members with a comment rather than
erroring — so a refactor can delete API from the framework and only fail in a downstream
Swift/Kotlin compile; `golden_test.go` therefore asserts the *generated API surface*, not that
generation succeeded. And `modernc.org/sqlite` supports neither ios nor android, so the dashboard
can never travel — `import_test.go` asserts `bind/` never reaches `internal/dash`.

#### 6e. The interim: daemon + dash over Tailscale, shipping years before any Swift

This is usable the day `kolk serve` merges, needs no Apple or Google account, and — the real point —
**validates the whole protocol design for free**.

kolk runs on the Mac/Linux box; Tailscale on both ends (iOS 15+, iPhone and iPad). Then
`tailscale serve --https=443 localhost:7777` terminates HTTPS with a **real Let's Encrypt
certificate** on `*.ts.net`. That matters more than it looks: App Transport Security requires HTTPS,
so a future native client needs **no `NSAllowsArbitraryLoads` exception at all** — the interim setup
and the eventual app want the identical server configuration. Use `serve`, never `funnel` (funnel is
open to the entire internet). TUI on the iPad is Blink Shell + `mosh-server`, which survives sleep
and network changes.

**Directories that appear: none.** What the tree must therefore already do, all of it cheap and all
of it useful regardless:

1. `kolk serve --addr` binds beyond `127.0.0.1` (default loopback, but configurable) — otherwise
   `tailscale serve` cannot proxy to it.
2. `kolk dash` serves on the **same mux** as `serve`, so one `tailscale serve` line covers the SPA
   and `/v1`.
3. Bearer-token auth from day one, including on loopback.
4. `web/dash` is responsive at 1024 pt — **it is the first mobile client**, months before any native
   one, and building it responsive now saves a rewrite.
5. `kolk -p --output stream-json` emits *literally the protocol frames*, so the CLI, the SSE stream
   and `spec/testdata/` are one thing with three exits. This is the highest-leverage single decision
   in this document.
6. No reliance on terminal features Blink lacks; `NO_COLOR` and a plain-ASCII fallback.

Use it yourself for a month before writing any Swift. It will find the protocol's real holes for
free.

---

### 7. Daemon and protocol sketch

Enough to unblock PLAN item 19 and the mobile clients. The full spec is `spec/`; this is the shape.

**Transport — three exits, one frame.**

| Exit | Command | Framing |
|---|---|---|
| stdout | `kolk -p "…" --output stream-json` | NDJSON, one Envelope per line |
| child stdio | `kolkd --stdio` (Tauri sidecar) | NDJSON, identical bytes |
| HTTP + SSE | `kolk serve` / `kolkd` (desktop, iPad, Android, dash) | `id:` = seq · `event:` = type · `data:` = the same JSON |

`spec/stdio.md` mandates byte-identity between the NDJSON line and the SSE `data:` payload. This is
non-negotiable and it is what keeps the Wails-vs-Tauri choice free.

**Envelope** (snake_case, matching OpenAPI and both generators' idioms, not Go's):

```json
{"seq":412,"ts":"2026-08-22T10:14:03.221Z","session":"s_01J…","turn":"t_01J…",
 "type":"message.delta","data":{"text":"…"}}
```

**Event taxonomy** — `<subject>.<past-participle>`, lowercase, dot-separated, a **closed** vocabulary
declared as constants in `protocol/events.go`:

```
hello                                     {protocol, server, capabilities[]}
session.started   session.updated   session.ended
turn.started      turn.finished     turn.cancelled
message.delta     message.completed reasoning.delta
tool.requested    tool.started      tool.output      tool.finished
permission.requested  permission.resolved
subagent.started  subagent.finished        (agent mode)
chapter.started   chapter.finished         (saga)
checkpoint.created
usage.reported    {model, input_tokens, cache_read_tokens, output_tokens, cost_usd, ttft_ms}
score.recorded    error    log
```

`usage.reported` field names mirror **OpenTelemetry GenAI** attributes (`input_tokens`,
`cache_read_tokens`, `operation_name`, `provider_name`, `conversation_id`) so `kolk export --otlp`
is later a column→attribute map and nothing else. Heartbeats are SSE **comment lines** (`: ping`,
every 15 s), never events, and `spec/stdio.md` says so, so no client tries to parse one.

**Commands** (client→server) are `<noun>.<verb>` imperative: `turn.create`, `turn.cancel`,
`permission.resolve`, `session.fork`, `session.list`.

**REST surface** (the shape; `spec/kolk.openapi.yaml` is authoritative):

```
GET  /v1/hello                              handshake, unauthenticated shape check only
POST /v1/turns                              → {turn_id}          (turn.create)
GET  /v1/sessions/{id}/events?from=<seq>    text/event-stream    (honours Last-Event-ID)
POST /v1/turns/{id}/cancel                                       (turn.cancel)
POST /v1/permissions/{id}                                        (permission.resolve)
GET  /v1/sessions   GET /v1/sessions/{id}
GET  /v1/models     GET /v1/stats/*         /dash/*  (same mux)
```

**The five things mobile forces into v1, none of them retrofittable:**

1. **Resumable log with monotonic ids.** iOS gives a backgrounded app *five seconds*
   (`applicationDidEnterBackground(_:)`), extendable by `beginBackgroundTask`; a 40-minute saga
   **will** outlive the client. Emit `id:` on every event, honour `Last-Event-ID` on reconnect, set
   `retry:`. Getting this from the SSE spec for free is another argument for SSE. **The log lives in
   `internal/bus`, below the transport** — so `--output stream-json`, the Tauri stdio sidecar and a
   Wails in-process link get replay through the *same* code path as SSE. A cursor older than the
   retained window returns `error{type:"cursor_expired"}` and the client re-fetches session state.
2. **Heartbeats.** `: ping` every 15 s; `URLSession.timeoutIntervalForRequest` is an *inactivity*
   timeout (default 60 s) and mobile NAT drops idle flows.
3. **Session multiplexing and reattach.** Open the app, list sessions, reattach to the saga already
   running on the Mac.
4. **Permission prompts as protocol events.** `internal/agent.confirm()` reads stdin today; it
   becomes `permission.requested` → `POST /v1/permissions/{id}`, with a server-side timeout policy
   for when no client is attached. This is the one refactor mobile forces on the engine, and desktop
   needs it anyway — which is why it lands early (migration step 7).
5. **Cancellation by id.** No `context.Context` crosses HTTP, a pipe, or a JNI bridge.

**Auth.** Bearer token, **always, including on loopback** — "it's on 127.0.0.1 so it's fine" stops
being true the moment Tailscale is involved. Token is generated on first `kolk serve` into
`$config/token` (0600), compared in constant time, and binding to anything other than `127.0.0.1`
**refuses to start** without one. The token is a `secret.Redact` target everywhere.

**Versioning rule.** `spec/VERSION` holds a **single integer as a string** — not semver, not a git
tag. It appears in three places: the URL (`/v1/…`), `protocol.Version`, and the `hello` frame's
`protocol` field. Within a version, changes are **additive only**, and clients **MUST ignore unknown
event types and unknown fields** — stated in `protocol/doc.go` and exercised by a fixture containing
a bogus type. A bump mounts `/vN` alongside the old one for one minor cycle. It is held at **`"0"`
until roughly v0.5**, so breaking changes are free while the product is still being discovered; it
freezes at `"1"` once `kolk serve` has been used daily for a month. `.github/workflows/spec.yml`
fires on `paths: spec/**` and fails any diff that does not also edit `spec/CHANGELOG.md`.

---

### 8. OS-divergence conventions

**Build tags — the verified trap first.** `unix` is **not a GOOS**. A file named `shell_unix.go`
with no build line compiles **on Windows too**. Reverified on go1.26.4, 2026-08-22:

```
GOOS=darwin   GoFiles: [main.go shell_unix.go]
GOOS=linux    GoFiles: [main.go shell_unix.go]
GOOS=windows  GoFiles: [main.go shell_unix.go shell_windows.go]   ← the bug
```

`_other`, `_posix`, `_stub`, `_generic` are equally decorative. This produces a **silently wrong
build**, not a compile error, which is why it gets its own named CI gate.

**The rule: every OS-divergent file carries an explicit `//go:build` line, no exceptions, and the
filename suffix is chosen to match it.**

| Filename | Required first line |
|---|---|
| `x_windows.go` | `//go:build windows` |
| `x_unix.go` | `//go:build !windows` |
| `x_darwin.go` | `//go:build darwin` |
| `x_unix.go` *(when a `_darwin.go` sibling exists)* | `//go:build !windows && !darwin` |

Prefer `!windows` over the `unix` build tag: the real `unix` tag includes **darwin, ios and
android**, so `//go:build unix` collides with any `_darwin.go` sibling. Every divergent pair sits
behind an interface declared in an untagged `<topic>.go`. **There is no `if runtime.GOOS ==`
anywhere in the tree.** `scripts/check-buildtags.sh` fails CI on any `*_unix.go`, `*_other.go` or
`*_posix.go` lacking a build line.

**Path strategy — decided, not deferred.** The rule is **XDG on all Unix including macOS**, Known
Folders on Windows:

| | Config | Data / state (sessions, checkpoints, `stats.jsonl`, `dash.db`) | Cache (model catalog) |
|---|---|---|---|
| **linux** | `$XDG_CONFIG_HOME/kolk` else `~/.config/kolk` | `$XDG_DATA_HOME/kolk` else `~/.local/share/kolk` | `$XDG_CACHE_HOME/kolk` else `~/.cache/kolk` |
| **darwin** | same as linux — **deliberately not `~/Library/Application Support`** | same as linux | same as linux |
| **windows** | `%AppData%\kolk` | `%LocalAppData%\kolk` | `%LocalAppData%\kolk\cache` |

macOS uses XDG, against `os.UserConfigDir()`, for three reasons: the prototype already writes
`~/.config/kolk`; developers symlink dotfiles and `~/Library/Application Support` is hostile to
`cd`; and it keeps the Unix code path single. `KOLK_CONFIG_DIR` / `KOLK_DATA_DIR` / `KOLK_CACHE_DIR`
override everything. `internal/paths/migrate.go` moves `~/.config/kolk/sessions` →
`~/.local/share/kolk/sessions` once, on first run, with a one-line notice. **`internal/paths` is the
only package permitted to call `os.UserHomeDir`/`os.UserConfigDir`**, enforced by `arch_test.go`.

**The shell abstraction lives in `internal/shell` and nowhere else.** `type Shell interface {
Run(ctx, Cmd) (Result, error); Spawn(ctx, Cmd) (*Proc, error) }`, implemented by `exec_unix.go`
(`bash -c`, byte-compatible with the prototype so `TestBash` never changes) and `exec_windows.go`.
`arch_test.go` bans `os/exec` everywhere else — including `internal/provider/agentcli`, which spawns
the user's `claude`/`codex` binary **through** `shell.Spawn`.

---

### 9. Naming conventions — the compact reference

| Thing | Rule | Examples |
|---|---|---|
| **Modules** | one lowercase word naming a *role*; never `core`, `common`, `shared`, `lib`, `pkg` | `desktop` `bind` `tools` |
| **Packages** | one lowercase word, no underscores, no plural unless it *is* a catalog; directory name == package name (except `cmd/*`) | `bus` `engine` `shell` `serve` `paths` `tools` |
| **Banned package names** | — | `util` `utils` `common` `helpers` `base` `types` `models` `misc`; **no `pkg/` directory ever** |
| **Files** | `lower_snake.go`, one concept per file, named for the thing | `bash.go` `sse.go` `orchestrator.go` |
| **Banned filenames** | `arch_test.go` greps for them | `util.go` `helpers.go` `common.go` `misc.go` `types.go` |
| **Command files** | exactly one per top-level verb | `internal/cli/cmd_stats.go` |
| **Tool files** | exactly one per tool | `internal/tools/bash.go` |
| **GOOS files** | `x_windows.go` / `x_unix.go` / `x_darwin.go`, **always** with an explicit `//go:build` line (§8) | `exec_windows.go` |
| **Tests** | `<same>_test.go` beside the source; fixtures in `testdata/`; golden files in `testdata/golden/*.txt` | `sse_test.go` |
| **Extra binaries** | always `cmd/kolk-<thing>/` so `$PATH` stays clean | `cmd/kolk-mock` |
| **Plan docs** | `docs/plan/NN-slug.md`, `NN` zero-padded == the PLAN.md item number, slug ≤ 4 kebab words | `02-architecture.md` `10-saga-loop.md` `19-desktop-ipad.md` |
| **Research docs** | `docs/research/<topic>.md` with a date on line 3 — they are dated snapshots, never edited in place | `platform-strategy.md` |
| **ADRs** | `docs/adr/NNNN-slug.md`, four digits, sequential, **never renumbered, never deleted**; `Status: proposed \| accepted \| superseded by NNNN`. Structural reversals only; product decisions stay in `docs/plan` | `0003-sse-not-websocket.md` |
| **Config keys** | dotted, lowercase, `snake_case` within a segment; the CLI flag is the last segment | `model` · `effort` · `effort.high.model` · `mode.code.effort` · `slot.title.model` · `slot.judge.model` · `provider.openrouter.base_url` · `serve.addr` · `serve.token_file` · `dash.addr` · `dash.retention_days` · `permission.rules` · `saga.max_chapters` · `saga.budget_usd` · `tool.bash.timeout_s` |
| **Env overrides** | `KOLK_` + key uppercased, `.` → `_`, for a **curated** list only (auto-deriving is ambiguous once keys contain underscores). `OPENROUTER_API_KEY` / `OPENROUTER_BASE_URL` keep working forever | `KOLK_SERVE_ADDR` `KOLK_EFFORT_HIGH_MODEL` |
| **Secrets** | never a config key; **0600 manifest by default, OS keychain opt-in** (item 5 §3); `OPENROUTER_API_KEY` forever, `KOLK_API_KEY` as the provider-agnostic addition; `redact.Mask`/`redact.Scrub` run over sessions, stats, the bus and logs. `KOLK_OPENROUTER_KEY` was never implemented and is not introduced. | amended by item 5 |
| **Commands** | one word, lowercase, ≤ 6 letters, no synonyms; grammar `kolk <verb> [subverb] [args]` | ship: `key logout model effort mode config models sessions stats dash saga serve login doctor update help version` — **`key` and `logout` added by item 5**: the North star names `kolk key` as one of the three commands the product *is*, so its absence here was a bug |
| **Parity rule** | every top-level verb has a slash twin with identical argument grammar, because `slash.go` dispatches into the same handlers as `cmd_*.go` | `kolk model x` ≡ `/model x` |
| **Reserved verbs** | deliberately unimplemented | `mcp skills hooks worktree export compact undo diff cost profile theme` |
| **Binaries** | `kolk` (CLI) · `kolkd` (daemon) · `kolk-mock` (dev). `kolkd` must never collide with `kolk` inside `Contents/MacOS/` | — |
| **Protocol events** | `<subject>.<past-participle>`, lowercase, dot-separated, closed vocabulary; **event name == schema filename == the `type` value** — one greppable string in three places | `turn.started` → `spec/schemas/events/turn.started.json` |
| **Protocol fields** | `snake_case` (OpenAPI + both generators' idiom, not Go's) | `input_tokens` |
| **Git branches** | `plan/NN-slug` for PLAN items · `spec/<slug>` for contract changes (makes the CHANGELOG gate unmissable) · `fix/<slug>` · `chore/<slug>`. `main` protected, always green, no long-lived branches | `plan/02-architecture` |
| **Git tags** | root module plain semver `vX.Y.Z`; nested-module prefixes reserved and unused; the protocol version is **never** a tag | `v0.1.0` · (reserved: `desktop/v0.1.0`) |
| **Commit prefixes** | conventional commits with the **package** as scope; `spec:` reserved for contract changes | `feat(engine): …` `fix(shell): …` `spec: add usage.reported.cache_read_tokens` `chore(release): v0.1.0` |

---

### 10. Concurrency model — parallel subagents and a streaming UI

**Context discipline.** One `context.Context` per turn, derived from a per-session context, derived
from the process context. Because **no `context.Context` crosses any of the three transports**,
cancellation is always **by id**: `internal/engine` keeps a `map[turnID]context.CancelFunc` behind a
mutex; `POST /v1/turns/{id}/cancel`, `/cancel` over stdio, and Ctrl+C in the TUI all resolve to the
same call. Ctrl+C cancels the turn; a second Ctrl+C within 2 s exits. Every blocking call in the
tree takes a `ctx` — HTTP requests, tool execution, shell spawn, SQLite queries — with **no
`context.Background()` below `cmd/`**, checked by `arch_test.go`.

**Goroutine ownership — one owner per goroutine, and every one is joined.**

| Goroutine | Owner | Lifetime |
|---|---|---|
| the turn loop | `engine.RunTurn` — runs on the *caller's* goroutine | returns before `RunTurn` does |
| SSE read pump (provider stream) | `provider/openrouter` | joined before `StreamChat` returns |
| parallel tool calls within one assistant message | `engine`, bounded worker pool, default **4** | all joined before the round advances |
| subagents (agent mode) | `orchestrator`, bounded pool, default **3** concurrent (Hermes `delegation.max_concurrent_children`), spawn depth 1 | all joined before synthesis; a failed subagent returns partial results plus an `error` event |
| saga chapters | `saga` — strictly sequential, never concurrent | one chapter at a time, by definition |
| bus fan-out | **none** — `Publish` appends to the log and does a non-blocking send per subscriber on the *publisher's* goroutine | — |
| SSE connection writer | `internal/serve`, one per HTTP connection | dies with the request context |

No `x/sync`: a `sync.WaitGroup` plus a buffered error channel is the pattern, because L4 is
stdlib-only. `engine_test.go` asserts a `runtime.NumGoroutine()` delta of zero across a full turn —
goroutine leaks in a long-running daemon are a class of bug worth a permanent test.

**Backpressure to the UI — the bus is the only place it is handled.** Each subscriber gets a
bounded channel (default cap 256). `Publish` never blocks:

- **Lifecycle, permission, usage, checkpoint, error and score events are never dropped.** If a
  subscriber's buffer is full for one of these, the bus blocks that subscriber's *delivery* and
  keeps the event in the log; the subscriber catches up by cursor.
- **`*.delta` events may be coalesced and, at the limit, dropped.** Consecutive `message.delta`
  frames merge; if the buffer still fills, the oldest deltas are dropped and a
  `log{level:"warn", dropped:N}` frame is emitted so the client knows its render is lossy. A tablet
  on a bad link must never be able to stall the engine.
- **Durable subscribers (`stats`, `dash/ingest`) read from the log by cursor, not from the live
  channel**, so a slow SQLite write can never apply backpressure to a token stream.

**The log's bounds are explicit**, because a 40-minute saga emitting token deltas produces a lot of
frames: a per-session ring of 10,000 events / 8 MB in memory, spilling to
`$data/sessions/<id>.events.ndjson`. `Subscribe(fromSeq)` reads the spill file, then the ring, then
attaches live — one code path, which is exactly why `Last-Event-ID` and `?from=` and
`--output stream-json --resume` behave identically.

**Permission prompts under parallelism.** All `Decider` calls funnel through a single serialized
queue, so two subagents can never fight over the TTY. In `serve` mode they are independent protocol
round-trips keyed by request id. Inside subagents the default is **auto-deny with a reason**
(Hermes's rule) rather than prompting, which is what prevents approval deadlock in unattended saga
runs; `--yolo` inside a sandbox flips it to auto-allow, still subject to the hardline blocklist.

---

### 11. Performance budgets, enforced in CI

`scripts/check-budgets.sh` runs on every push and **fails**, never warns.

| Budget | Limit | Baseline today (2026-08-21/22) |
|---|---|---|
| `kolk` binary, `-trimpath -ldflags "-s -w"` | **≤ 20 MB** hard; **≤ 12 MB** soft (warn) | 6.1 MB |
| `kolkd` binary | ≤ 15 MB | — |
| Cold start (`kolk version`, p50 of 20 runs) | **≤ 30 ms** hard; ≤ 20 ms soft | ~10 ms |
| First-token overhead above raw provider TTFT | ≤ 50 ms | — |
| Idle RSS after one REPL turn | ≤ 50 MB | — |
| Offline test suite wall time | ≤ 30 s | — |
| **Test-count floor** | root module **≥ 22**, and ≥ 1 per nested module | 22 |
| `go vet` / `golangci-lint` | clean | clean |
| Third-party modules in the root graph | ≤ 2 (`modernc.org/sqlite` + `libc`), + `charm.land/*` and `x/sys` once L6 lands | 0 |

The test-count floor is not decoration. A nested module's tests are **invisible to bare
`go test ./...`**, which prints `ok` and exits 0 — a nested module containing an unconditional
`t.Fatal()` is simply never run. From the day `tools/go.mod` exists, `scripts/test.sh` (which
enumerates every `go.mod`) is the only command you or a coding agent should type; the Makefile,
CI, `CONTRIBUTING.md` and `KOLKRABBI.md` all say so, and `docs/contributing/workspace.md` has a
section titled *"why bare ./... lies to you"*.

Two budget hits are expected and must be **measured at the step that causes them, not discovered at
v1.0**: `modernc.org/sqlite` (step 12) and the Bubble Tea stack (later). The pre-carved escape hatch
is `cmd/kolk-dash` — a 20-line `main.go`, possible only because `internal/dash` never imports
`internal/cli` or `internal/tui`, which `arch_test.go` enforces from day one.

---

### 12. Migration checklist

**Invariant: `go build ./... && go test ./...` is green after every step, and the 22 tests stay
passing.** There is **no red build window** anywhere in this plan. Exactly two steps touch test
files; neither changes an assertion.

| # | Step | Breaks | Green after |
|---|---|---|---|
| 0 | ✅ **DONE 2026-08-22** — `git init`, prototype committed verbatim, tagged `proto-0`. | nothing | 22 |
| 1 | ✅ **DONE 2026-08-22** — the identity commit (below): module path + all renames in one mechanical pass. Repo pushed to `onembyte/kolkrabbi`. | nothing user-visible (no published version, no installs); `go build .` at the root stops working — intentional, the binary is now `kolk` | 22 |
| 2 | ◐ **PARTLY DONE 2026-08-22** — `.github/workflows/ci.yml` runs `{ubuntu, macos}` + a budgets job (20 MB binary / 30 ms cold start / test-count floor of 22); first run green at 6.25 MB and 2 ms. **Windows is deliberately deferred to step 13** rather than added red now — revisit if the `_windows.go` work slips. Budget checks live in the workflow; extract to `scripts/check-budgets.sh` when the other `scripts/check-*.sh` land at step 4. | Windows baseline is red by design | 22 unix / 17 windows |
| 3 | ✅ **DONE 2026-08-22** (commit `dfafa41`, build session) — split `cmd/kolk/main.go` (606 L) into a table-driven `internal/cli/*` per the §4 table, leaving ~40 lines. The command table's argument grammar is filled in from `docs/plan/09-commands.md`. | nothing | 22 |
| 4 | Guard rails: `internal/arch/{layers.go,arch_test.go}`, `internal/buildinfo`, `scripts/{check-purity,check-buildtags,test}.sh`, `Makefile`, `LICENSE`, `.goreleaser.yaml`. CI asserts `! grep -q '^replace' go.mod`. | nothing | 22 + 1 |
| 5 | **L0 platform extraction** — `paths` (from `main.go:32-40` + `config.dir()`), `shell` (from `tools.go:119`), `atomicfile` (from `session.go:51,64`), `lock`, `term`, `secret`. Real unix impls, honest Windows stubs. | nothing observable; `TestBash` still passes because `shell.Run` on unix is the same `bash -c` | 22 + new |
| 6 | Add `spec/` + `protocol/` + `protocol/conform_test.go`. **Pure addition, nothing moves** — so the contract can be iterated on cheaply for a week before anything depends on it. | nothing | 22 + ~8 |
| 7 | **`internal/bus` + the engine emits events.** ★ The one risky step, made safe: **retain `Options.Out`** as a convenience that attaches `cli/render.Plain` as a bus subscriber, and write `render.Plain` **byte-identical** to today's output (the ANSI consts and `footer()` move over verbatim). Result: **zero test edits** — `Out: &out` appears at exactly 2 sites and neither changes. `kolk -p --output stream-json` falls out for free. Only once green: add event-sequence assertions alongside the string ones, then delete the string ones. | nothing, if `render.Plain` is byte-identical. If it is not, you are debugging five e2e tests and a new event bus simultaneously — do not skip the byte-identity check. | 22 |
| 8 | **`confirm()` → `engine.Decider`.** Delete the `bufio` stdin read at `agent.go:272`. `internal/cli/prompt.go` implements the TTY prompt; tests use auto-allow. `tools.Execute`'s signature is unchanged, so all 5 tool tests are untouched, and the e2e tests already run with `Yolo: true`. **This one refactor unblocks desktop, iPad and Android simultaneously** — which is why it comes before any of them exist. | nothing testable | 22 |
| 9 | `engine.Port` interfaces + `internal/enginetest/fakes.go` (incl. `Clock`); `engine` stops importing `session`/`checkpoint`/`stats` concretely; `internal/cli` does the wiring; lift `orchestrator.go` into `internal/orchestrator` behind `engine.Runner`. | nothing | 22 |
| 10 | ⚠ **Amended by item 3 (`docs/plan/03-provider-layer.md` §11): this step MUST land BEFORE the reasoning round-trip work**, not after. Reasoning bytes are persisted on the assistant message, so the format cut has to exist first or a half-signed thinking block gets written in the old shape and bricks the session on disk. **The on-disk format cut.** Commit `internal/session/testdata/v0-session.json` (a real session captured from the prototype) **and** a load test **in the same commit** as introducing `session.Message` with frozen JSON tags + conversion at the store boundary. `session.Session.Messages` stops being `[]provider.Message`. | this is the one place a silent data regression is possible — the fixture is the whole defence | 22 + 1 |
| 11 | **`internal/serve` + `kolk serve` + `cmd/kolkd`.** `sse.go` (`id:`/`retry:`/`Last-Event-ID`/`: ping`), `auth.go` (bearer, required off-loopback), `stdio.go`, `permission.go`. `serve/conform_test.go` replays `spec/testdata/streams/*.ndjson` through **both** stdout NDJSON and SSE and requires byte-identical frames. **★ The interim iPad story ships here** — `--addr` must bind beyond `127.0.0.1` or `tailscale serve` cannot proxy. | nothing; purely additive | 22 + new |
| 12 | `web/dash` + `internal/dash`. First third-party dependency (`modernc.org/sqlite`). `//go:embed all:dist`; commit the sentinel `dist/index.html`; `ingest.go` imports the existing `stats.jsonl` so no recorded data is lost. **Record the size and startup numbers in this PR.** | budgets move — measure, do not discover | 22 + new |
| 13 | **Windows.** Fill in every `_windows.go` twin. `windows-latest` goes from advisory to required; `windows` joins the goreleaser goos list. | expect breakage here, in CI, on purpose | 22 on all three |
| 14 | `internal/tui` (Bubble Tea) as renderer #2 behind the seam step 7 established; `internal/provider/agentcli` + `internal/mockagent` + `spec/testdata/foreign/`; `internal/saga`. All additive leaves. | none | — |
| 15 | `tools/` nested module → `clients/ts` generated for `web/dash` — **the first proof that the contract generates a client**, and the cheapest one (no Apple or Google account needed). | bare `go test ./...` starts silently skipping `tools/` — `scripts/test.sh` and the test-count floor are now load-bearing | — |
| 16 | `desktop/`, `bind/`, `clients/swift`, `clients/kotlin` — each a new directory, **zero changes to the root module.** That is the whole claim of this architecture, and steps 6–11 are what buy it. | none | — |

**Step 1 in full — the identity commit.** The module-path change forces touching all **24 import
lines across 9 files** exactly once; ride that commit for every rename so import churn is never paid
twice. (BSD `sed -i ''`; drop the `''` on Linux.)

```sh
git init && git add -A && git commit -m "chore: import prototype verbatim" && git tag proto-0

go mod edit -module github.com/onembyte/kolkrabbi && go mod edit -go=1.25

mkdir -p cmd/kolk && git mv main.go cmd/kolk/main.go
git mv cmd/mockserver              cmd/kolk-mock
git mv internal/api                internal/provider
git mv internal/agent              internal/engine
git mv internal/mockrouter         internal/enginetest
git mv internal/enginetest/mockrouter.go internal/enginetest/router.go

# package clauses
sed -i '' 's/^package api$/package provider/'          internal/provider/*.go
sed -i '' 's/^package agent$/package engine/'          internal/engine/*.go
sed -i '' 's/^package mockrouter$/package enginetest/' internal/enginetest/*.go

# import paths (24 lines, 9 files)
grep -rl '"kolkrabbi/internal' --include='*.go' . | xargs sed -i '' \
 -e 's|"kolkrabbi/internal/api"|"github.com/onembyte/kolkrabbi/internal/provider"|' \
 -e 's|"kolkrabbi/internal/agent"|"github.com/onembyte/kolkrabbi/internal/engine"|' \
 -e 's|"kolkrabbi/internal/mockrouter"|"github.com/onembyte/kolkrabbi/internal/enginetest"|' \
 -e 's|"kolkrabbi/internal/|"github.com/onembyte/kolkrabbi/internal/|'

# qualifiers — the capital-letter guard is what makes this safe (verified: every match is a
# real qualifier; the only lowercase `agent`/`api` occurrences are prose in help text and comments).
#
# ⚠ USE perl, NOT sed/grep. BSD grep and BSD sed do not support \b, and they fail SILENTLY:
#   `grep -rlE '\b(api|agent)\.[A-Z]'` matches nothing on macOS, xargs then gets no input, the
#   substitution never runs, and the first sign of trouble is `undefined: api` from go vet.
#   (Hit for real on 2026-08-22 during the actual identity commit.) perl's \b is portable, and
#   \b before `agent` correctly does NOT match `subagent`.
find . -name '*.go' -print0 | xargs -0 perl -pi -e \
 's/\bapi\.([A-Z])/provider.$1/g; s/\bagent\.([A-Z])/engine.$1/g; s/\bmockrouter\.([A-Z])/enginetest.$1/g;'

# verify the pass actually did something — 0 means clean, and never trust silence
grep -roE '(^|[^A-Za-z0-9_])(api|agent|mockrouter)\.[A-Z]' --include='*.go' . | wc -l   # expect 0

gofmt -w . && go vet ./... && go build ./cmd/... && go test ./...   # expect: ok, 22 tests
```

Then, **in the same commit**, a documentation pass over `PLAN.md` §0's ground-truth table,
`README.md` and `docs/research/*` so no document refers to `internal/api` or `internal/agent` again.

`go install github.com/onembyte/kolkrabbi/cmd/kolk@latest` starts working the moment this is pushed.

---

## Rationale

**Why Go.** The measured numbers decide it: 0 dependencies, 6.1 MB, ~10 ms, 22 offline tests at
3.4k LOC. The product's hard requirements are a single static cross-compiled binary, goroutines over
streaming SSE for parallel subagents, `//go:embed` for the dashboard, and a stdlib good enough that
the engine needs nothing external. Rust buys nothing the budgets ask for and costs solo-developer
velocity; TypeScript/Bun forfeits the binary and startup properties outright.

**Why one module.** The single-module verdict is what the ecosystem actually does: crush,
terraform, hugo, k6, ollama, fzf, lazygit and golangci-lint are all single-module. Kubernetes,
opentelemetry-go and golang/tools went multi-module only because they **publish independently
versioned libraries** — kolk publishes a binary and one contract. Splitting the *product* would buy
GoReleaser Pro ($165/yr, monorepo mode is Pro-only), a five-tag release script you debug at 2 a.m.,
`go install @latest` broken for the length of the migration, a `go.work` the Go team says not to
commit, a per-feature cross-cutting tax across five modules, and — worst — a bare `go test ./...`
that runs a third of the tree and exits 0 during the exact week the 22 tests are being relocated.
The three nested modules that *do* exist ship nothing, so they never pay the release tax; two of
them (`bind/`, `desktop/`) are **required** to be nested, for reasons the compiler enforces.

**Why the contract is a top-level `spec/` and not a Go package member.** The deliverables generated
from it are a SwiftPM package, a Kotlin/Ktor client and a TypeScript client for the dashboard. A
Swift consumer must not pull a Go module to read the contract, and the fixture corpus in
`spec/testdata/` is the cross-language conformance suite — one directory, four languages, no shared
test framework. Generation flows outward only; a Go-first source of truth would mean writing a
Go→OpenAPI generator.

**Why the event log is in `internal/bus` and not in the HTTP layer.** Replay-from-cursor is an
engine concern, not an HTTP fact. Putting it in the transport — which is what the two rival layouts
did — denies resume to the Tauri stdio sidecar and to a Wails in-process link, i.e. to two of the
four roadmap targets, and forces `--output stream-json` to grow a second, divergent implementation.
One log serving `Last-Event-ID`, `?from=`, stdio replay, stats ingest and dash ingest through a
single code path is the only shape that keeps the three exits honest.

**Why HTTP+SSE and not WebSocket.** OpenAPI 3.1 can describe `text/event-stream` and **cannot
describe WebSocket at all**, and Apple ships `swift-openapi-generator` with first-class SSE
decoding. For a solo developer, WebSocket means hand-writing and hand-maintaining a client on every
platform forever, and losing `Last-Event-ID` resume, which the spec gives for free and which iOS
backgrounding makes mandatory.

**Why the layering is a test rather than module fences.** Compiler-enforced boundaries are strictly
stronger — that argument is real and it is why the multi-module proposal scored well on longevity.
But module fences only stop *cross-module* violations; nothing stops one module from becoming a
26-package mud ball inside. ~150 lines of `go/parser` catches both, costs no tooling tax, and is
reversible in ten seconds. The concession to the objection is that there is **no `//arch:allow`
escape hatch**: an escape hatch is a comment typed at 1 a.m.; editing `layers.go` is a decision.

**Why the provider type stops being the on-disk format.** `session.Session.Messages []api.Message`
means the OpenRouter message shape *is* the transcript format, in a product whose premise is many
providers plus external CLI backends with different shapes. This is already load-bearing at 3.4k
LOC and gets more expensive every week. Cutting it costs one new type and a conversion; the
`v0-session.json` fixture is how the migration proves it reads old files, not how it freezes the old
shape.

**Why the permission refactor lands at step 8, before serve, desktop or mobile exist.** It is the
single change that unblocks all three at once, and it is cheapest at 3.4k LOC. Every week it waits,
the surface that reads stdin grows.

---

## Alternatives rejected

- **Rust / TypeScript+Bun instead of Go** — Rust buys nothing the measured budgets ask for at
  significant velocity cost for one person; Bun forfeits the single static binary, the 10 ms start
  and trivial cross-compilation. Neither has a `//go:embed` equivalent that is as boring.
- **Six modules (`protocol` / `engine` / `host` / `dash` / `mobile` + root CLI)** — genuinely
  stronger boundaries, and it is the only shape that makes "the engine cannot exec" a dependency
  edge. Rejected because the costs land on the daily loop: a verified `go test ./...` that runs 2 of
  6 packages and exits 0 in week one, a deliberate multi-day red build window, `go install @latest`
  broken mid-migration, a five-tag release script, and a nine-file cross-module edit for one field
  flowing provider→dashboard. Its own author's go/no-go test — "if in six months no second consumer
  exists, the boundaries bought nothing" — is the right test, and the honest answer today is that
  the daemon is not shipped yet. `arch_test.go` + `check-purity.sh` recover most of the guarantee.
- **`main.go` at the repo root (crush / terraform / hugo shape)** — a root `package main` takes its
  binary name from the *module path*, which would produce `kolkrabbi`, not `kolk`. Verified.
  `cmd/kolk/main.go` is mandatory, not stylistic.
- **A `pkg/` directory** — zero meaning to the go command; go.dev/doc/modules/layout never mentions
  it; golang-standards/project-layout says in its own README that it is *not* an official standard
  and is "overkill" for a project like this.
- **Exporting the engine (`engine/`, `core/`) as a public package** — commits the largest and most
  volatile surface in the repo to Go's compatibility expectations in exchange for zero consumers.
  `protocol/` is the entire export list; anyone wanting to build on the engine runs the daemon.
- **`mobile/` inside the root module** — verified fatal: `gomobile bind` calls
  `mobileModuleAvailable()` and hard-fails unless `golang.org/x/mobile` is resolvable through the
  invoking module, which would put x/mobile and its x/tools graph into the go.mod that serves
  `go install …/cmd/kolk@latest`.
- **`gomobile bind` as the primary mobile path** — the ObjC/Java bridge accepts bool / string /
  numeric / `[]byte` / pointer-to-bound-struct / named-interface and nothing else; **zero** exported
  signatures in the current prototype are bindable, unsupported members are *silently dropped*,
  x/mobile has 0 tags and 0 releases with ~25 human commits a year, and `modernc.org/sqlite`
  supports neither ios nor android. It survives as an opt-in offline-chat escape hatch, never on the
  critical path and never in required CI.
- **WebSocket as the daemon transport** — not describable in OpenAPI 3.1, so no generated Swift or
  Kotlin client, forever. Remains addable later as an *additional* transport carrying the same
  frames.
- **Committing `go.work`** — the module reference calls it "generally inadvisable"; it overrides a
  contributor's own workspace and can make CI test the wrong versions. `scripts/dev-workspace.sh`
  generates a gitignored one for gopls.
- **`~/Library/Application Support` on macOS** — rejected in favour of XDG everywhere on Unix: the
  prototype already writes `~/.config/kolk`, developers symlink dotfiles, and it keeps one Unix code
  path.
- **Deciding Wails vs Tauri now** — the only thing either shell imposes on the Go tree is
  stdio/SSE frame identity, which `spec/stdio.md` settles today. Wails v3 is in beta with no
  published GA date after 3.5 years pre-GA; deciding now buys nothing and forecloses the cheaper
  option.

---

## Risks & open questions

- **Wails v3 vs Tauri v2 is deliberately open.** Deferred by `serve.Mux()` being both importable and
  spawnable and by `spec/stdio.md`'s byte-identity mandate. Triggered by (i) a successful 30-minute
  Wails v3 spike, (ii) Wails v3 GA, or (iii) the first desktop-only native feature that matters.
  Until then `desktop/README.md` holds both recipes and `docs/adr/0003-desktop-shell-choice.md` does
  not exist. Residual risk: the Wails path needs the spike anyway — but because `desktop/` is its
  own **module root**, the "Wails main in a non-root directory" unknown does not apply here.
- **The protocol tax is real and paid daily.** Every user-visible engine change needs a schema file,
  a fixture, a `protocol/` struct and a CHANGELOG line. Mitigated by holding `spec/VERSION` at `"0"`
  until ~v0.5 (breaking changes free; the CHANGELOG is a log, not a contract) and by gating only
  events that have already shipped. **Go/no-go checkpoint:** if six months from now `kolk serve` has
  shipped and there is still no second consumer — not even the generated `clients/ts` for the
  dashboard — then `spec/` bought nothing and should be collapsed into `protocol/`. `clients/ts` is
  the named proof obligation because it is the cheapest client and needs no Apple or Google account.
- **`internal/bus` is a new failure surface with no analogue in the prototype** — slow consumers,
  dropped subscribers, log bounds, replay correctness, ordering between renderer and stats writer.
  Its bugs will present as iPad-only or desktop-only symptoms. Mitigation: `bus_test.go` covers all
  five before anything depends on it, and the drop policy in §10 is explicit rather than emergent.
- **Hand-written `protocol/` catches shape drift, not intent drift.** Renaming a field in both the
  schema and the struct in one commit round-trips cleanly while breaking the Swift client.
  `tools/cmd/specgen` closes this and will not exist for a while; until then, protocol changes go on
  a `spec/<slug>` branch specifically so the gate is unmissable.
- **Debug paths get longer.** "Why is my output wrong" now crosses `engine → bus → render` instead
  of one `fmt.Fprintf`. Accepted: it is the price of the CLI being a real client, and it is the only
  thing that keeps the protocol honest.
- **Budget hits are coming.** `modernc.org/sqlite` (step 12) and Bubble Tea (step 14). Measured at
  the causing step, gated by `scripts/check-budgets.sh`, with `cmd/kolk-dash` pre-carved as the
  escape hatch.
- **Nested modules make bare `go test ./...` lie** from step 15 onward. `scripts/test.sh` plus the
  test-count floor plus `docs/contributing/workspace.md` are the answer; muscle memory and coding
  agents will still forget, so `KOLKRABBI.md` says it too.
- **`bind/kolkmobile` may be pure sunk cost.** It is a stub with two tests, never in required CI. The
  honest position: an option, not a plan. The mobile story is the daemon.
- **Open, owned by other items:** the exact SQLite schema (item 17 — `docs/research/dashboard.md`
  §4's 7 tables are the starting point), effort level names (item 7 recommends `low/medium/high/max`,
  used in this doc's config examples), the TUI framework spike (item 11), the Codex/`agentcli`
  legal gate (item 4), and the GitHub owner + license (item 1 — this doc assumes
  `github.com/onembyte/kolkrabbi` and Apache-2.0; if item 1 decides otherwise, change it *before*
  the first push, since the module path is a breaking change afterwards).

---

## Sources

Verified locally on **go1.26.4 darwin/arm64, 2026-08-22**, in this repo and in scratch modules:

- `internal/` is a **path-prefix** fence, not a module fence — a nested module at
  `…/kolkrabbi/bind` imports the root's `internal/engine` and **builds**; a module named `gobind`
  importing the same path gets `use of internal package … not allowed`. Both reproduced today.
  Rule text: `go help gopath` "Internal Directories"; implementation:
  `$(go env GOROOT)/src/cmd/go/internal/load/pkg.go` `disallowInternal()`.
- **`_unix` carries no build constraint** — reproduced: `GOOS=windows go list` includes
  `shell_unix.go`. Same for `_other`, `_posix`, `_stub`, `_generic`. `go help buildconstraint`;
  the real `unix` tag list is `$(go env GOROOT)/src/internal/syslist/syslist.go` (includes darwin,
  ios, android).
- **Binary naming** — `go build ./cmd/kolk` produces `kolk` (last element of the *directory*); a root
  `package main` is named from the module path. Reproduced today.
- **Prototype facts** — 3,399 LOC, 22 tests green, 24 `"kolkrabbi/internal…"` import lines across 9
  files, `Out: &out` at exactly 2 sites in `internal/agent/agent_test.go`,
  `session.Session.Messages []api.Message` at `internal/session/session.go:26`, hardcoded
  `exec.CommandContext(cctx,"bash","-c",…)` at `internal/tools/tools.go:119`, hardcoded
  `~/.config/kolk` at `main.go:32-40`. All counted 2026-08-22.
- **6.1 MB / ~10 ms / 22 tests / 0 deps** — PLAN.md §0, measured 2026-08-21 over 20 runs on an
  M-series Mac.

Constraint reports (2026-08-22, `docs/research/constraints/`), which carry the primary citations for:

- `go install` refusing modules with `replace` (`go help install`; gopls's release-tag dance);
  `go.work` not committed (go.dev/ref/mod#workspaces); `go install pkg@version` ignoring `go.work`
  (`modload` `RootMode = NoRoot`); the single-module survey (crush, terraform, hugo, k6, cli/cli,
  ollama vs kubernetes, opentelemetry-go, golang/tools); `pkg/` having no meaning to the toolchain
  and golang-standards/project-layout's own disclaimer.
- `//go:embed` rules — no `..`, no leading `/`, no symlinks (`cannot embed irregular file`), no
  crossing a `go.mod`, and `all:` required for `_`/`.` prefixed entries (`embed` package doc);
  crush's `internal/cmd/stats/` precedent.
- Wails v3 status (`v3.0.0-beta.12`, 2026-08-21; no published GA date; GTK4 + WebKitGTK 6.0;
  `CGO_ENABLED=1` on darwin/linux in its generated Taskfiles; ~15 MB output) and Tauri v2
  `bundle.externalBin` semantics + inside-out macOS sidecar signing
  (`crates/tauri-bundler/src/bundle/macos/app.rs`, `settings.rs::copy_binaries`).
- GoReleaser OSS vs Pro (monorepo mode, `prebuilt` importer, `.app`/`.dmg`/`.msi`/NSIS, native
  bundle notarization are all Pro; OSS `notarize.macos` covers bare binaries) — goreleaser.com/pro/.
- gomobile — `cmd/gobind/doc.go` type restrictions; `bind/gen.go` `isSupported` (the real
  vocabulary); silent-skip in `bind/gengo.go:185` and `bind/genjava.go:310`;
  `cmd/gomobile/bind.go` `f.AddModuleStmt("gobind")` and `mobileModuleAvailable()`; x/mobile
  status (0 tags, 0 releases, ~25 human commits/yr, "experimental… neither Google nor the Go team
  can provide end-user support"); NDK ≥ r28 for Android 15's 16 KB alignment (golang/go#74839).
- Apple — App Review **2.5.2** (quoted in §6c), 2.5.1, 2.5.4, 1.2, 4.7/4.7.1/4.7.2;
  `Foundation/Process` macOS-only; no `NSTask.h` in iPhoneOS27.0.sdk while `fork`/`execve` *are* in
  its `unistd.h`; `applicationDidEnterBackground` 5-second window; App Transport Security.
- Android — Android 10 W^X (`execve` in the app home dir), foreground-service types and the 6-hour
  `dataSync` cap, `specialUse` Play Console review, Device and Network Abuse policy.
- SSE + codegen — WHATWG server-sent-events (`id`, `Last-Event-ID`, `retry`);
  `apple/swift-openapi-generator` v1.13.0 + `swift-openapi-runtime` v1.12.0
  (`asDecodedServerSentEvents`, iOS 13+/macOS 10.15+); `OpenAPITools/openapi-generator` v7.24.0;
  `okhttp-sse`; Ktor `ktor-sse` + `ktor-client-darwin`.
- Tailscale — iOS 15+ (iPhone and iPad), `tailscale serve --https=443`, Let's Encrypt certs for
  `*.ts.net`, serve (tailnet-only) vs funnel (public).
- `modernc.org/sqlite` v1.57.0 — CGo-free; supported-platform table excludes ios and android.

Project inputs: `PLAN.md` (items 1, 2, 9, 10, 11, 12, 13, 14, 17, 19, 20, 21, 23),
`docs/research/platform-strategy.md`, `docs/research/ecosystem.md`, `docs/research/dashboard.md`,
`docs/research/openrouter.md`, `docs/research/subscription-auth.md`, `docs/research/orcli.md`
(all 2026-08-21).
