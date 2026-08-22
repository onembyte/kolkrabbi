<!-- Verified constraint report produced 2026-08-22 by the item-2 architecture workflow.
     Source of truth for docs/plan/02-architecture.md. Re-verify before relying on version-specific claims. -->

# Research: kolk on iPad/iOS and Android — what the repo must support

Date: 2026-08-22. Method: `gh api` against `golang/mobile` + `golang/go` (source and issues read over HTTP, nothing cloned), WebFetch of Apple/Android/Tailscale docs, local cross-compiles of this prototype, and local inspection of the Xcode 27.0 iPhoneOS SDK. Feeds PLAN.md items **2** (architecture), **9** (`--output stream-json`), **17** (dash), **19** (desktop & iPad path). Companion to `docs/research/platform-strategy.md`, which sketched this; this document replaces its iPad paragraph with sourced detail and adds Android.

---

## 0. Verdict in one paragraph

**gomobile `bind` is not a viable way to ship the kolk engine to iOS or Android, and it never becomes one.** Not because of maintenance risk (x/mobile is alive, if thinly staffed), but because the ObjC/Java bridge accepts a set of Go types so narrow that *zero* functions in the current `internal/` packages are bindable, and because `bind` runs from a generated module named `gobind`, which Go's `internal/` rule forbids from importing `kolkrabbi/internal/...` (verified empirically below). The primary path is therefore the one PLAN item 2 already implies: **a versioned HTTP+SSE daemon protocol, with native SwiftUI/Kotlin clients generated from an OpenAPI 3.1 contract.** gomobile survives only as an *optional* offline fallback behind a hand-written facade of `string`/`[]byte`/interface methods — and even that is blocked today by `modernc.org/sqlite` not listing ios/android as supported targets. Structure the repo for the daemon; treat `bind/` as a stub you may never build.

---

## 1. gomobile `bind`: artifacts, targets, and the type wall

### 1.1 What it produces

From `cmd/gomobile/doc.go` (fetched from `golang/mobile@master`, 2026-08-22):

- `gomobile bind [-target android|ios|iossimulator|macos|maccatalyst] [package]`
- **Android** → *"an AAR (Android ARchive) file that archives the precompiled Java API stub classes, the compiled shared libraries, and all asset files"*. Default builds `arm, arm64, 386, amd64`. Requires `javac` 1.8+ and Android SDK API 16+; `ANDROID_HOME`/`ANDROID_NDK_HOME`.
- **Apple** → *"Unlike gomobile build, gomobile bind creates an XCFramework."* Targets `ios, iossimulator, macos, maccatalyst`; *"gomobile must be run on an OS X machine with Xcode installed."* Default `-iosversion` is 13.0.
- The generated language surface is **Objective-C** (consumed from Swift via the ObjC bridge) or **Java** (consumed from Kotlin). There is no Swift or Kotlin generator.
- Go structs cross as **opaque proxy objects**, not value types: a Go `*Counter` becomes a Java `Counter` / ObjC `GoMypkgCounter*` holding a refnum; exported fields become `getX()`/`setX()` **native** methods, i.e. one bridge crossing per field read.

### 1.2 The supported-type set — verbatim doc, then the actual source

`cmd/gobind/doc.go` § "Type restrictions", verbatim:

> At present, only a subset of Go types are supported. All exported symbols in the package must have types that are supported. Supported types include:
> - Signed integer and floating point types.
> - String and boolean types.
> - Byte slice types. Note that byte slices are passed by reference, and support mutation.
> - Any function type all of whose parameters and results have supported types. Functions must return either no results, one result, or two results where the type of the second is the built-in 'error' type.
> - Any interface type, all of whose exported methods have supported function types.
> - Any struct type, all of whose exported methods have supported function types and all of whose exported fields have supported types.
>
> […] Exceptions and panics are not yet supported. If either pass a language boundary, the program will exit.

The doc is *incomplete and optimistic*. The ground truth is `bind/gen.go:isSupported` (read at `golang/mobile@master`, 2026-08-22):

```go
func (g *Generator) isSupported(t types.Type) bool {
	if isErrorType(t) || isWrapperType(t) { return true }
	switch t := types.Unalias(t).(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool, types.UntypedBool, types.Int, types.Int8, types.Uint8,
			types.Int16, types.Int32, types.UntypedRune, types.Int64, types.UntypedInt,
			types.Float32, types.Float64, types.UntypedFloat,
			types.String, types.UntypedString:
			return true
		}
		return false
	case *types.Slice:
		return isBytesSlice(t)          // ONLY []byte
	case *types.Pointer:
		switch t := types.Unalias(t.Elem()).(type) {
		case *types.Named: return g.validPkg(t.Obj().Pkg())
		}
	case *types.Named:
		switch t.Underlying().(type) {
		case *types.Interface, *types.Pointer: return g.validPkg(t.Obj().Pkg())
		}
	}
	return false
}
```

Decoded, the **complete** bindable vocabulary:

| Go construct | Bindable? | Note |
|---|---|---|
| `bool`, `string` | ✅ | |
| `int`, `int8`, `int16`, `int32`/`rune`, `int64` | ✅ | `int` maps to `nint`; Java `long` |
| `uint`, `uint16`, `uint32`, `uint64`, `uintptr` | ❌ | `TODO(crawshaw)` in `gen.go:386`, unfixed since 2015 |
| `byte`/`uint8` | ✅ | only as an element of `[]byte` or a scalar |
| `float32`, `float64` | ✅ | |
| `complex64/128` | ❌ | |
| `[]byte` | ✅ | **by reference, mutable** |
| `[]string`, `[]int`, `[]*T`, `[]T`, any non-byte slice | ❌ | open since 2015: golang/go#13445 *"x/mobile/bind: support slices of supported structs"* |
| arrays `[N]T` | ❌ | |
| `map[K]V` (any) | ❌ | not in the switch at all |
| `chan T` | ❌ | |
| struct **by value** (`Message`) | ❌ | `*types.Named` with `*types.Struct` underlying falls through → false |
| `*T` where `T` is a named struct **in a bound package** | ✅ | opaque proxy handle |
| `*T` where `T` is in an *unbound* package | ❌ | `validPkg` checks the packages passed to `gobind` |
| named interface type in a bound package | ✅ | this is the callback mechanism |
| **named non-struct types** (`type Effort string`, `type Mode int`, `time.Duration`) | ❌ | underlying is `Basic`, matches neither `Interface` nor `Pointer`. They land in `g.otherNames`, which **no generator ever reads** — they vanish silently. |
| `time.Time`, `context.Context`, `io.Reader`, `error` from other stdlib pkgs | ❌ | `error` is the *sole* exception (`isErrorType`); everything else fails `validPkg` |
| **generics** | ❌ | hard error. golang/go#64486 (open, `NeedsInvestigation`+`mobile`+`generics`, filed 2023-12-01): `cannot use generic type demo.GenericTestData[T any] without instantiation`. Generic *functions* are silently skipped (type params fall to the `default` branch). |
| variadic `...T` | ❌ in practice | the param types as `[]T`; only `...byte` could pass `isSupported` |
| multiple returns | ⚠️ | exactly `()`, `(T)`, or `(T, error)`. Three returns is rejected (`gen.go:336`, `gengo.go:62`). |
| panics / Go errors as exceptions | ⚠️ | `(T, error)` → Java `throws Exception` / ObjC `NSError**`. A **panic** crossing the boundary **exits the process**. |

### 1.3 The silent-skip hazard — the single most dangerous property

Unsupported members are **not errors**. They are dropped with a comment:

```go
// bind/gengo.go:185
if !g.isSigSupported(o.Type()) {
    g.Printf("// skipped function %s with unsupported parameter or result types\n", o.Name())
    return
}
// bind/genjava.go:310  (struct fields)
if t := f.Type(); !g.isSupported(t) {
    g.Printf("// skipped field %s.%s with unsupported type: %s\n\n", n, f.Name(), t)
    continue
}
```

So a refactor that changes `Foo(ids []string)` to a map, or adds a `time.Time` field, **silently deletes API from the framework** and the failure surfaces as a Swift/Kotlin compile error in a downstream repo. Any `bind/` package must therefore be covered by a golden test that asserts the generated API surface, not just that generation succeeded.

### 1.4 The `internal/` blocker — verified empirically here

`cmd/gomobile/bind.go:287` builds the generated Go package into a **separate module literally named `gobind`** (`f.AddModuleStmt("gobind")`) with `replace` directives back to local dirs. Go's `internal/` visibility rule is import-path based, so `gobind/...` cannot import `kolkrabbi/internal/...`. Reproduced locally (2026-08-22, Go 1.26.4):

```
module gobind + replace kolkrabbi => /Users/francomichetti/kolkrabbi
import _ "kolkrabbi/internal/api"
→ main.go:3:8: use of internal package kolkrabbi/internal/api not allowed
```

**Everything gomobile is ever to bind must live outside `internal/`.** This is a repo-layout decision, not a build flag.

### 1.5 What that means for the *current* API surface

Not one exported signature in the prototype survives. Concretely:

| Current signature | Why it fails |
|---|---|
| `api.Client.StreamChat(ctx context.Context, model string, messages []Message, tools []Tool, onToken func(string)) (Message, Meta, error)` | `context.Context` (unbound pkg), `[]Message` + `[]Tool` (non-byte slices), `Message`/`Meta` returned **by value**, **three** results |
| `api.Client.ListModels(ctx) ([]ModelInfo, error)` | ctx + slice-of-struct |
| `agent.New(o Options) *Agent` | struct param by value |
| `tools.Execute(ctx, name, argsJSON string, confirm Confirm, pre PreWrite) (string, error)` | ctx; `Confirm`/`PreWrite` are *named func types* (underlying `*types.Signature`) → not `Interface`/`Pointer` → unsupported |
| `session.List(dir string) ([]*Session, error)` | slice of pointers |
| `checkpoint.Store.RewindLastTurn() ([]string, error)` | `[]string` |
| `stats.Aggregate(recs []Record) []ModelRow` | slices both ways |
| `api.Message` / `stats.Record` struct fields (`ToolCalls []ToolCall`, timestamps) | fields silently skipped |

**Conclusion: a bindable package must be a hand-written thin facade — there is no "just export the core" option.** The only shape that provably fits the vocabulary is:

```go
// kolkrabbi/bind/kolkmobile   (NOT under internal/)
package kolkmobile

// Callbacks: a named interface with string/[]byte/error-only methods.
type EventSink interface {
	OnEvent(jsonLine string)     // one protocol event, JSON-encoded
	OnError(message string)
	OnDone()
}

// The one handle. Everything else hangs off it.
type Engine struct{ /* unexported */ }

func NewEngine(configJSON string) (*Engine, error)
func (e *Engine) Send(requestJSON string, sink EventSink) (string, error) // returns a turn id
func (e *Engine) Cancel(turnID string) error                             // ctx replacement
func (e *Engine) Query(requestJSON string) (string, error)               // sync request/response
func (e *Engine) Close() error
```

Note what this facade *is*: it is the **daemon protocol, in-process**. Same JSON events, same versioning, same cancellation-by-id (because `context.Context` cannot cross). That is the key structural insight — **designing the daemon protocol first makes the gomobile facade nearly free later, and designing the facade first would have forced you into the same shape anyway.** One protocol, three transports: stdout (`--output stream-json`), HTTP+SSE (daemon), and JNI/ObjC (bind).

### 1.6 Second gomobile blocker: SQLite

`modernc.org/sqlite` v1.57.0 (2026-08-19) is CGo-free but its README's supported-platform table lists **darwin, freebsd, linux, netbsd, openbsd, windows** — **neither `ios` nor `android`**. Go's build-tag rules mean the files would still be *selected* (verified in `$GOROOT/src/go/build/build.go`: `if ctxt.GOOS == "android" && name == "linux"` → line 1959; `if ctxt.GOOS == "ios" && name == "darwin"` → line 1965), so it may well compile — but it is **untested and unsupported on both**, and iOS's file-locking/`mmap` sandbox behaviour is exactly where a translated-C SQLite port would break. *Unverified whether it actually works; do not assume it.* This alone kills "bind the whole engine including the dashboard".

---

## 2. x/mobile maintenance status and risk

Measured 2026-08-22 via `gh api`:

| Signal | Value |
|---|---|
| `golang/mobile` (mirror) | 6,208 ★, not archived, last push **2026-08-21** |
| Tags / GitHub releases | **0 / 0** — module is untagged forever |
| pkg.go.dev version | pseudo-version `v0.0.0-20260821190718-4776eadac327` |
| Official status text | *"The Go Mobile project is **experimental**. Use this at your own risk. While we are working hard to improve it, **neither Google nor the Go team can provide end-user support**."* |
| Commits since 2025-08-01 | **41**, of which **16 are `Gopher Robot`** (automated dep bumps) → ~25 human commits/year |
| Top human committers (12 mo) | racequite (10), Hajime Hoshi (6, the Ebiten author — community, not Go team), Ian Chechin (3) |
| Open `golang/go` issues with `x/mobile` in the title | **214** |
| Notable open issues | #68754 *"x/mobile: compatibility with Android 15"* (open since 2024-08-07), #80398 *"x/mobile: old iOS version"*, #64486 (generics), #13445 (slices of structs, open since 2015) |
| Recent real work | `cmd/gomobile: invoke the NDK Clang directly`, `support source overlays`, `reject signed iOS version components` — genuine but janitorial |

**Risk assessment: "alive but unowned."** It is *not* abandoned — commits land weekly and Go 1.26 support is current (`go.mod` says `go 1.26.0`). But: no releases, no support commitment, one platform-compat issue open for two years, and the type system frozen since 2015. Feature requests do not get built.

One resolved gotcha worth recording, since it will recur: **Android 15's 16 KB page-size requirement** broke gomobile output (golang/go#74839, `libgojni.so is not aligned with 16KB`, 2025-08-01). Resolution, from the reporter: *"Updating NDK to **r28** has fixed this issue for arm64-v8a/x86_64, and this is enough."* So NDK ≥ r28 is a hard floor for any AAR. (*Unverified:* the exact Google Play deadline for 16 KB support.)

**Rule for the plan: gomobile is never on the critical path, never in CI's required matrix, and never the reason an engine API is shaped a certain way.** Confirm PLAN item 19's existing stance; this research strengthens it from "optional" to "opt-in escape hatch only".

---

## 3. The primary path: native client ⇄ kolk daemon

### 3.1 Transport decision — SSE, not WebSocket, and the reason is codegen

This is the one place where a protocol choice has a large downstream cost for a solo developer:

- **OpenAPI 3.1 can describe SSE** (`content: text/event-stream`) and **cannot describe WebSocket at all.**
- **Apple ships an official generator**: `apple/swift-openapi-generator` v1.13.0 (2026-07-06, 1,958★, active) + `apple/swift-openapi-runtime` v1.12.0. The runtime has first-class event streams — `Sources/OpenAPIRuntime/EventStreams/` contains `ServerSentEvents.swift`, `ServerSentEventsDecoding.swift`, `JSONLinesDecoding.swift`, `JSONSequenceDecoding.swift`, plus `asDecodedServerSentEvents`. The generator repo even ships `Examples/event-streams-client-example/` and `Examples/streaming-chatgpt-proxy/`. Platform floor: **iOS 13 / macOS 10.15 / visionOS 1**.
- **Kotlin**: `OpenAPITools/openapi-generator` v7.24.0 (2026-07-20) for the client; SSE via `square/okhttp`'s `okhttp-sse` module or Ktor's `ktor-shared/ktor-sse`. Ktor also has `ktor-client-darwin` / `ktor-client-ios`, so a **Kotlin Multiplatform protocol client shared by SwiftUI and Compose** is a real option worth considering for a solo dev.
- Native primitives if you hand-write instead: `URLSession.bytes(for:delegate:)` → `URLSession.AsyncBytes` with `.lines`, **iOS/iPadOS 15+, macOS 12+**; `URLSessionWebSocketTask`, **iOS/iPadOS 13+, macOS 10.15+**.

Choosing WebSocket means hand-writing and hand-maintaining the client on every platform forever. Choosing HTTP+SSE (POST to start a turn, `GET .../events` to stream, POST to answer a permission prompt or cancel) means `swift-openapi-generator` writes the Swift for you from the same YAML that documents the CLI's `--output stream-json`. **Recommend HTTP+SSE; leave WebSocket as a later optimisation for bidirectional/low-latency needs, never as the only transport.**

### 3.2 What mobile forces into the protocol *now*

These are not nice-to-haves; retrofitting them is a protocol break.

1. **Resumable event log with monotonic ids.** iOS gives the app *five seconds* after backgrounding (`applicationDidEnterBackground(_:)`: *"That method has five seconds to perform any tasks and return. Shortly after that method returns, the system puts your app into the suspended state."*), extendable by `beginBackgroundTask(withName:expirationHandler:)`, and *"If you don't end your tasks in a timely manner, the system terminates your app."* App Review 2.5.4 forbids abusing background modes: *"Multitasking apps may only use background services for their intended purposes: VoIP, audio playback, location, task completion, local notifications, etc."* So a saga running for 40 minutes **will** outlive the client's connection. The daemon must keep a per-session event log and support **replay from a cursor**. Use SSE's own mechanism: emit `id:` on every event and honour the `Last-Event-ID` request header on reconnect (WHATWG SSE: *"The `Last-Event-ID` HTTP request header reports an `EventSource` object's last event ID string to the server when the user agent is to reestablish the connection"*; `retry:` sets the reconnection time). Getting this free from the spec is another argument for SSE.
2. **Heartbeats.** Emit `: ping` comment lines every ~15 s. `URLSession`'s `timeoutIntervalForRequest` is an *inactivity* timeout (default 60 s) and mobile NAT will drop idle flows.
3. **Session multiplexing + reattach.** `GET /v1/sessions`, `GET /v1/sessions/{id}/events?from=<seq>`. The iPad user opens the app and reattaches to the saga already running on the Mac.
4. **Permission prompts as protocol events, not TTY prompts.** `internal/agent.confirm()` currently reads stdin. It must become `event: permission_request {id, tool, detail, diff}` → `POST /v1/turns/{id}/permission {request_id, decision}`, with a server-side timeout policy for when no client is attached. This is the single largest refactor the mobile path implies, and it is needed for the desktop app anyway.
5. **Cancellation by id.** No `context.Context` over any of the three transports (HTTP, stdout, or a future JNI bridge). `POST /v1/turns/{id}/cancel`.
6. **Auth that survives leaving localhost.** A bearer token in config, not "it's on 127.0.0.1 so it's fine". PLAN item 2 says "token-authenticated on localhost"; over Tailscale it is no longer localhost.
7. **Protocol version in the URL and in a handshake event.** `/v1/...` plus `event: hello {protocol: "1", server: "kolk 0.4.0", capabilities: [...]}`. A 2-year-old iPad build must fail loudly and legibly.
8. **Out-of-band notification is a separate, real cost.** "Saga chapter 7 finished" reaching a suspended iPad needs APNs, which needs an Apple push key and a server that can reach Apple — a self-hosted daemon on a home Mac can do this (it has outbound internet), but it is a genuine piece of infrastructure. Do not promise push in v1; the honest v1 answer is "open the app and it reattaches".

### 3.3 Where the contract lives — proposed layout

```
api/
  openapi.yaml                # OpenAPI 3.1, hand-maintained, THE source of truth
  CHANGELOG.md                # protocol changes, one line per version
  testdata/events/*.json      # golden event fixtures — the cross-language conformance suite
protocol/                     # PUBLIC Go package: wire structs + `const Version = "1"` + encode/decode
                              # stdlib-only; no engine imports; the ONLY public package besides cmd
internal/                     # engine stays internal: providers, modes, tools, sessions, stats
serve/  (or internal/serve/)  # net/http handlers implementing api/openapi.yaml over protocol/
dash/                         # go:embed SPA, served by the same daemon
bind/kolkmobile/              # OPTIONAL gomobile facade — public path, string/[]byte/interface only
tools/go.mod                  # SEPARATE module for codegen deps (oapi-codegen, spectral, …)
clients/
  swift/                      # SwiftPM package; swift-openapi-generator build plugin
  kotlin/                     # openapi-generator output (or a Kotlin Multiplatform client)
```

Rationale for each non-obvious choice:

- **`protocol/` is public, everything else stays `internal/`.** Costs one small compatibility obligation; buys (a) a gomobile facade that can import it, (b) a Go client for tests/desktop-sidecar, (c) a clean statement of "this is the API, that is the implementation". PLAN item 1 floats `pkg/core` for exactly this — narrow it: publish the *protocol*, not the *core*.
- **`api/openapi.yaml` is contract-first, not generated from Go.** The whole point is generating Swift and Kotlin; a Go-first source of truth means writing a Go→OpenAPI generator yourself. Generation flows outward only.
- **`tools/go.mod` is a separate module** so the engine's `go.mod` stays at zero dependencies (PLAN item 2's dependency rule). Go 1.24+ `go mod edit -tool` (confirmed present in the local Go 1.26.4) would work too, but it puts the tool in the *main* module's graph — a separate module is cleaner given the stdlib-only commitment.
- **`api/testdata/events/*.json` is the cheapest possible cross-language test.** Go tests decode/re-encode them; the Swift and Kotlin packages later read the same directory. One fixture set, three languages, no shared test framework.
- **CI gate**: `openapi.yaml` must lint, must round-trip every fixture through `protocol/`, and a version bump must be accompanied by a `CHANGELOG.md` line. Cheap, and it is the thing that actually prevents the protocol drifting.

---

## 4. iPadOS: what is impossible, precisely

### 4.1 The App Review citation

**Guideline 2.5.2**, verbatim (fetched 2026-08-22):

> Apps should be self-contained in their bundles, and may not read or write data outside the designated container area, nor may they download, install, or execute code which introduces or changes features or functionality of the app, including other apps. Educational apps designed to teach, develop, or allow students to test executable code may, in limited circumstances, download code provided that such code is not used for other purposes. Such apps must make the source code provided by the app completely viewable and editable by the user.

Related: **2.5.1** *"Apps may only use public APIs and must run on the currently shipping OS."* **2.5.4** *"Multitasking apps may only use background services for their intended purposes: VoIP, audio playback, location, task completion, local notifications, etc."*

### 4.2 Corroborating SDK evidence (checked locally, Xcode-beta 27.0, iPhoneOS27.0.sdk)

- `Foundation.framework/Headers/` on **iOS has no `NSTask.h`**; the macOS SDK has both `NSTask.h` and `NSUserScriptTask.h`. Apple's docs confirm `Process` (*"An object that represents a subprocess of the current process"*) is **macOS 10.0+ / Mac Catalyst 13.0+ only — not available on iOS or iPadOS**.
- `usr/include/unistd.h` *does* declare `fork()`, `vfork()`, `execve()`, marked only `__WATCHOS_PROHIBITED __TVOS_PROHIBITED` — **not iOS-prohibited at the header level.** This is a trap: your Go code compiles and links for `ios/arm64`; the sandbox denies at runtime and App Review denies at submission. Do not conclude from a successful build that anything works.

Measured here, 2026-08-22 (Go 1.26.4, this prototype, zero deps):

| Target | Result |
|---|---|
| `android/arm64`, `CGO_ENABLED=0` | **builds clean** (all packages, incl. `os/exec` usage) |
| `windows/amd64`, `CGO_ENABLED=0` | builds clean |
| `linux/amd64`, `CGO_ENABLED=0` | builds clean |
| `ios/arm64`, `CGO_ENABLED=0` | fails: *"ios/arm64 requires external (cgo) linking, but cgo is not enabled"* |
| `ios/arm64`, `CGO_ENABLED=1` + iPhoneOS SDK clang, `-miphoneos-version-min=13.0` | **builds clean** |

So "no cgo" (PLAN item 2's dependency rule) holds for CLI/desktop/Android but **cannot** hold for iOS — iOS always requires external linking. That is a scoped exception, not a rule violation, since the iOS build is an Xcode build by definition.

### 4.3 Feature matrix on iPad

| kolk feature | On-device | Why |
|---|---|---|
| `bash` tool | ❌ **impossible** | 2.5.2 "execute code"; no `Process`; no shell reachable from the sandbox |
| Running builds / tests / linters / formatters | ❌ **impossible** | same; a toolchain is downloaded-and-executed code |
| Spawning `claude`/`codex` (PLAN item 4's external-agent backend) | ❌ **impossible** | subprocess spawning |
| MCP **stdio** servers | ❌ **impossible** | subprocess spawning. MCP **HTTP** servers ✅ |
| Git operations on a local checkout | ⚠️ only via a pure-Swift/Go git implementation over files in the app container; no `git` binary |
| **chat mode** (no tools) | ✅ | pure HTTPS to OpenRouter — this is just an API client |
| Reading/writing files **inside the app container** or via `UIDocumentPicker`/Files | ✅ | *"may not read or write data outside the designated container area"* — Files-provider access is the sanctioned route |
| Reviewing diffs, approving/denying tool calls | ✅ | rendering + a POST; the *execution* happens on the daemon |
| **Driving a remote daemon** (full code mode, agent mode, saga) | ✅ | code executes on your Mac/Linux box; the iPad is a terminal. Precedent: Blink, Termius, Prompt are all on the App Store |
| Viewing the dashboard | ✅ | either a WKWebView over `kolk dash`, or native charts over `/v1/stats` |
| Local SQLite for offline session cache | ✅ but **not** via `modernc.org/sqlite` (unsupported target) — use the system SQLite via Swift, or GRDB |
| Background saga execution *on the iPad itself* | ❌ | 5-second background window + 2.5.4 |
| Push notification when a remote saga finishes | ⚠️ | possible, but requires APNs plumbing (§3.2.8) |

Two App Review items to keep on the radar for an AI-chat app, beyond 2.5.2: **1.2 (User-Generated Content)** requires *"a method for filtering objectionable material… a mechanism to report offensive content… the ability to block abusive users… published contact information"*, and **4.7** explicitly enumerates **chatbots** among "software that is not embedded in the binary" — with 4.7.1 requiring the same filtering/reporting affordances, and **4.7.2** *"Your app may not extend or expose native platform APIs or technologies to the software without prior permission from Apple."* An LLM chat surface will be read under these. *Unverified:* whether Apple's current age-rating questionnaire has a dedicated AI-chatbot question; the guidelines page itself does not mention "AI chatbot" or "generative AI".

---

## 5. Android: is a local Go daemon viable?

Compilation is not the problem (`android/arm64` builds clean, above). Three things are.

**a) The W^X rule.** Android 10 behaviour changes, verbatim:

> Execution of files from the writable app home directory is a W^X violation. Apps should load only the binary code that's embedded within an app's APK file. Untrusted apps that target Android 10 cannot invoke `execve()` directly on files within the app's home directory.
> In addition, apps that target Android 10 cannot in-memory modify executable code from files which have been opened with `dlopen()`… because the library cannot have been mapped `PROT_EXEC` through a writable file descriptor. This includes any shared object (`.so`) files with text relocations.

So you cannot download or extract a binary to app storage and run it — which is exactly why Termux left Google Play. Two legal shapes remain:

1. **gomobile `bind` AAR** — the Go runtime is a JNI `.so` inside the APK, loaded in-process. No `exec` anywhere. Fully policy-clean. This is the *only* clean way to run Go inside an Android app.
2. **Ship the Go binary as `lib/<abi>/libkolk.so` in `jniLibs` and `exec` it from `applicationInfo.nativeLibraryDir`.** Not the writable home dir, so not a W^X violation, and Play's Device and Network Abuse policy only bans *downloaded* executables (*"an app may not download executable code (such as dex, JAR, .so files) from a source other than Google Play"* — with an exception only for VM/interpreter code). Requires `useLegacyPackaging = true` (AGP 4.2+ replaced `android:extractNativeLibs`) so the file is extracted to disk. *Unverified:* I found no explicit Android doc stating `nativeLibraryDir` contents are executable — this is a widely-used but community-established fact. Treat as fragile.

**b) Foreground-service lifetime is hostile to a long-running agent.** Android 14+ requires a declared `foregroundServiceType` plus its permission, and the types you'd plausibly claim all have problems:

- `dataSync` — no runtime prerequisite, but on Android 15+: *"The system permits `dataSync` and `mediaProcessing` foreground services to run for a total of 6 hours in a 24-hour period, after which the system calls the running service's `Service.onTimeout(int, int)` method."* Fail to `stopSelf()` and you get `android.app.RemoteServiceException: "A foreground service of type [x] did not stop within its timeout"`. Exhaust the quota and a restart throws `ForegroundServiceStartNotAllowedException` until the user foregrounds the app. Also: *"Apps that target Android 15 or higher are not allowed to launch a data sync foreground service from a `BOOT_COMPLETED` broadcast receiver."*
- `shortService` — ~3 minutes. Useless.
- `specialUse` — no timeout, but: *"developers should declare use cases in the manifest… These values and corresponding use cases are **reviewed when you submit your app in the Google Play Console**."* A human at Google decides whether "runs an AI coding agent" qualifies.
- `connectedDevice` — qualifies with just `CHANGE_NETWORK_STATE` in the manifest, but claiming it for a self-hosted daemon is a policy stretch.

**c) Even with the daemon running, "code mode" needs a toolchain** — compilers, test runners, `git`. Shipping those inside an APK is the Termux problem, and Termux is not on Play for this reason.

**Verdict: same thin-client path as iPad.** Android's rules are looser than Apple's, and the difference buys you exactly one thing that iOS cannot have — an *offline local engine* for chat mode and the dashboard, via a gomobile AAR (option a.1), subject to §1.6's SQLite caveat. That is a nice-to-have, not a product. Build the same SwiftUI/Compose thin client against the same OpenAPI contract, and if you ever want offline chat, that is when you write `bind/kolkmobile` — and it will be the same facade the daemon protocol already describes.

---

## 6. The zero-app interim: what it needs from the structure

This is shippable the day `kolk serve` + `kolk dash` exist, needs no App Store account, and — importantly — **validates the entire protocol design before any Swift is written.**

The setup: kolk runs on the Mac/Linux box. Tailscale on both machines (*"Tailscale works with iOS 15.0 or later… It supports both iPhone and iPad."*). Then:

- **Dashboard + any future web UI on the iPad**: `tailscale serve --https=443 localhost:<dashport>`. Tailscale terminates HTTPS with a **real Let's Encrypt certificate** for `*.ts.net` (*"Tailscale will automatically request a certificate for this machine on this domain, using Let's Encrypt"*; requires MagicDNS + "Enable HTTPS" in the admin console; *"HTTPS traffic uses an automatically provisioned TLS certificate"* and *"by default, the device's Tailscale daemon terminates the HTTPS connection"*). This matters more than it looks: **App Transport Security requires HTTPS** (*"ATS requires that all HTTP connections made with the URL loading system—typically using the URLSession class—use HTTPS"*), so a future native client talking to `https://mac.tailnet.ts.net` needs **no `NSAllowsArbitraryLoads` exception at all**. The interim setup and the eventual app want the identical server configuration.
- **TUI on the iPad**: Blink Shell (`blinksh/blink`, 6.9k★, *"the first professional, desktop-grade terminal for iOS that leverages the support of Mosh and SSH"*) + `mosh-server` on the box. Mosh survives sleep/network changes, which is the whole reason to prefer it over plain SSH from a tablet.
- Use `tailscale serve`, **not** `funnel` — serve is tailnet-only; funnel is *"publicly, open to the entire internet"*.

What the repo must therefore do, all of it cheap and all of it useful regardless:

1. **`kolk serve --addr` must bind beyond `127.0.0.1`** (default localhost, but configurable) — otherwise `tailscale serve` can't proxy to it, and neither can anything else.
2. **`kolk dash` must serve over the daemon's HTTP mux**, not a separate ad-hoc server, so one `tailscale serve` line covers both the SPA and `/v1/...`.
3. **Bearer-token auth from day one**, and no assumption that "same machine" means "trusted".
4. **The dashboard SPA must be usable on a touch screen at 1024pt** — it is the *first* mobile client, months before any native one. Building it responsive costs nothing now and saves a rewrite.
5. **`kolk --output stream-json`** (PLAN item 9) should emit *literally the protocol events* from `protocol/`, so the CLI, the daemon SSE stream, and the fixtures in `api/testdata/events/` are one thing with three exits. This is the highest-leverage single decision in this document.
6. **TUI-over-mosh sanity**: no reliance on terminal features Blink lacks; `NO_COLOR` and a plain-ASCII fallback (PLAN item 11 already lists these).

---

## 7. Decisions this research recommends closing

For PLAN item 19 (and the pieces of item 2 it depends on):

1. **Ship shape**: daemon + native thin clients. gomobile `bind` is an opt-in offline fallback, never a dependency, never in required CI.
2. **Transport**: HTTP + SSE described by OpenAPI 3.1, because it is the only streaming transport that Apple's own generator can produce Swift for. WebSocket later, additive.
3. **Contract location**: `api/openapi.yaml` (source of truth) + public `protocol/` Go package + `api/testdata/events/` golden fixtures + `tools/go.mod` for codegen deps.
4. **Public surface**: exactly `cmd/kolk`, `protocol/`, and (if ever) `bind/kolkmobile`. Everything else stays `internal/` — and note that `internal/` is *precisely* what gomobile cannot see, so this boundary is doing double duty.
5. **Protocol must have, from v1**: monotonic event ids + `Last-Event-ID` resume, heartbeats, session list/reattach, permission-request events, cancel-by-id, bearer auth, `/v1` + `hello` handshake.
6. **`confirm()` must stop reading stdin.** It becomes a protocol round-trip with a server-side default policy. This is the one refactor mobile forces on the engine, and desktop needs it too.
7. **Interim ships first**: `kolk serve --addr` + responsive `kolk dash` + Tailscale Serve + Blink/mosh. Use it yourself for a month before writing Swift; it will find the protocol's real holes for free.
8. **Toolchain constraint to state in the README**: on iPad, code mode requires a reachable daemon. There is no on-device fallback and there never will be. Say so up front rather than have users discover it.

---

## Sources

- `golang.org/x/mobile` — `cmd/gobind/doc.go` (Type restrictions, Binding Go, reference cycles), `cmd/gomobile/doc.go` (bind/build targets, AAR/XCFramework, `-iosversion` 13.0, NDK/SDK requirements), `bind/gen.go` (`isSupported`, `isSigSupported`, `cgoType`, `Init`), `bind/types.go` (`isErrorType`, `isBytesSlice`, `isCallable`), `bind/genjava.go` + `bind/genobjc.go` + `bind/gengo.go` (silent-skip behaviour), `cmd/gomobile/bind.go` (generated module named `gobind`). All via `gh api repos/golang/mobile/...`, 2026-08-22.
- https://pkg.go.dev/golang.org/x/mobile — experimental notice, pseudo-version `v0.0.0-20260821190718-4776eadac327`.
- `gh api repos/golang/mobile` — 0 tags, 0 releases, 41 commits since 2025-08-01 (16 by Gopher Robot), pushed 2026-08-21.
- golang/go issues: **#64486** (generics, open), **#13445** (slices of structs, open since 2015), **#68754** (Android 15 compat, open since 2024-08-07), **#74839** (16 KB alignment → NDK r28 fixes it, closed 2025-08-01), **#80398**, **#80426**. `gh api`, 2026-08-22.
- https://developer.apple.com/app-store/review/guidelines/ — 2.5.1, **2.5.2**, 2.5.3, 2.5.4, 1.2, 4.7/4.7.1/4.7.2. Fetched 2026-08-22.
- developer.apple.com documentation JSON: `Foundation/Process` (macOS/Mac Catalyst only), `URLSessionWebSocketTask` (iOS 13+), `URLSession.bytes(for:delegate:)` (iOS 15+), `UIKit/extending-your-app-s-background-execution-time` (5 s + `beginBackgroundTask`), `NSAppTransportSecurity` (HTTPS required; `NSAllowsArbitraryLoads` / `NSExceptionDomains` / `NSAllowsLocalNetworking`).
- Local inspection, Xcode-beta 27.0 `iPhoneOS27.0.sdk` vs macOS SDK: absence of `NSTask.h` on iOS; `fork`/`vfork`/`execve` in `unistd.h` marked only `__WATCHOS_PROHIBITED __TVOS_PROHIBITED`. 2026-08-22.
- https://developer.android.com/about/versions/10/behavior-changes-10 (W^X / `execve` in app home dir); `.../services/fgs`, `.../fgs/service-types`, `.../fgs/timeout` (types, permissions, 6-hour `dataSync`/`mediaProcessing` cap, `RemoteServiceException`, `specialUse` Play Console review, `shortService` ~3 min); `.../manifest/application-element` (`extractNativeLibs` → `useLegacyPackaging`).
- https://support.google.com/googleplay/android-developer/answer/9888379 — Device and Network Abuse: no downloading dex/JAR/`.so` from outside Play; VM/interpreter exception.
- https://tailscale.com/kb/1020/install-ios (iOS 15+, iPhone and iPad), /kb/1153/enabling-https (Let's Encrypt for `*.ts.net`), /kb/1242/tailscale-serve (`--https=443`, tailnet-only vs Funnel).
- `apple/swift-openapi-generator` v1.13.0 / `apple/swift-openapi-runtime` v1.12.0 (`Sources/OpenAPIRuntime/EventStreams/`, `Examples/event-streams-client-example`, `Examples/streaming-chatgpt-proxy`; platforms iOS 13/macOS 10.15); `OpenAPITools/openapi-generator` v7.24.0; `square/okhttp` `okhttp-sse`; `ktorio/ktor` `ktor-shared/ktor-sse`, `ktor-client-darwin`/`ktor-client-ios`. `gh api`, 2026-08-22.
- https://html.spec.whatwg.org/multipage/server-sent-events.html — `id` field / last event ID buffer, `Last-Event-ID` header, `retry` field.
- https://pkg.go.dev/modernc.org/sqlite v1.57.0 — CGo-free; supported-platform table excludes ios and android.
- Local measurements, 2026-08-22, Go 1.26.4 darwin/arm64 on this prototype: cross-compile matrix (§4.2); `use of internal package kolkrabbi/internal/api not allowed` from a module named `gobind` (§1.4); `$GOROOT/src/go/build/build.go:1959,1965` (android⊃linux, ios⊃darwin build tags); `go mod edit -tool` present.

**Flagged unverified**: `modernc.org/sqlite` actually working on `ios`/`android` despite build tags selecting files; executability of `applicationInfo.nativeLibraryDir` (community-established, no Android doc found); Google Play's exact 16 KB page-size deadline; gomobile XCFramework/AAR size overhead (not measured — expect ≥ the 6.1 MB static binary per architecture slice); whether Apple's age-rating questionnaire has a dedicated AI-chatbot question.