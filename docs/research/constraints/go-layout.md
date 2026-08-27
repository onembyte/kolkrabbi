<!-- Verified constraint report produced 2026-08-22 by the item-2 architecture workflow.
     Source of truth for docs/plan/02-architecture.md. Re-verify before relying on version-specific claims. -->

All verification below ran on **go1.26.4 darwin/arm64, 2026-08-22**. "Verified" = I built it and read the output; scratch modules are in `<scratch>`.

---

# 1. `internal/` — the exact rule (get this one right)

**The stated rule** (`go help gopath`, "Internal Directories"; original proposal golang.org/s/go14internal):

> Code in or below a directory named "internal" is importable only by code in the directory tree rooted at the parent of "internal".

**The rule as actually implemented in module mode** — this is the part that matters and it is *not* what most people assume. `$(go env GOROOT)/src/cmd/go/internal/load/pkg.go`, `disallowInternal()`:

```go
} else {
    // p is in a module, so make it available based on the importer's import path instead
    // of the file path (https://golang.org/issue/23970).
    ...
    parentOfInternal := p.ImportPath[:i]
    if str.HasPathPrefix(importerPath, parentOfInternal) {
        return nil
    }
}
```

**The check is on the importer's IMPORT PATH prefix. Not the module. Not the repo. Not the filesystem.** Verified matrix (all with a real `go build`):

| importer | imports `example.com/kolkrabbi/internal/engine` | result |
|---|---|---|
| same module, `cmd/kolk` | ✓ | builds |
| **separate module** `example.com/kolkrabbi/desktop` (own go.mod, joined by `replace` or go.work) | ✓ | **builds** |
| separate module `example.com/kolkrabbi-desktop` | ✗ | `use of internal package … not allowed` |
| separate module `example.com/otherorg/app` | ✗ | `use of internal package … not allowed` |
| `internal/tools` → `internal/agent/internal/secret` | ✗ | `not allowed` (each `internal` is its own fence, at any depth) |

**Real-world confirmation:** open-telemetry/opentelemetry-go. Module `go.opentelemetry.io/otel/sdk` lives in `sdk/go.mod` — a *separate module* — and imports `go.opentelemetry.io/otel/internal/…` in 25 files (`sdk/metric/view.go`, `sdk/trace/span.go`, `sdk/log/batch.go`, …). Legal purely because the path prefix matches.

**Why this is still a useful fence:** on GitHub, module path `github.com/OWNER/kolkrabbi/<sub>` can only resolve to the `<sub>/` subdirectory of repo `github.com/OWNER/kolkrabbi` (go.dev/ref/mod: "the module subdirectory is the part of the module path that names the directory… This also serves as a prefix for semantic version tags"). No foreign repo can legitimately claim a path under yours. So:

> **`internal/` is a same-repo fence, not a same-module fence.**

**Consequences for the three future consumers:**

- **Desktop shell (Wails/Tauri).** Wails v3 apps are separate modules importing `github.com/wailsapp/wails/v3/pkg/application` (verified: `v3/go.mod`, module `github.com/wailsapp/wails/v3`). If kolk's desktop app is module `github.com/OWNER/kolkrabbi/desktop` in the same repo → **it can import kolk's internals directly**. If it's `github.com/OWNER/kolkrabbi-desktop` or another org → **only exported packages**. Tauri imposes nothing (the Go side is just a sidecar binary).
- **gomobile-bind — hard blocker, verified.** `gomobile bind` synthesizes a module *literally named `gobind`* in a temp dir: `cmd/gomobile/bind.go` calls `f.AddModuleStmt("gobind")`; `bind_iosapp.go` writes it to `filepath.Join(outDir, "src", "gobind")` with its own `writeGoMod`. I simulated it exactly (a module named `gobind` with a `replace` back to the repo):
  - `gobind` → `example.com/kolkrabbi/internal/engine` → **`use of internal package … not allowed`**
  - `gobind` → `example.com/kolkrabbi/mobile` (exported facade that itself imports `internal/engine`) → **OK**

  ⇒ **The gomobile-bound package MUST be exported.** It may freely call internal packages. It must also satisfy gobind's type restrictions (signed ints, floats, string, bool, `[]byte` by reference, funcs returning 0/1/2 results with `error` second, interfaces and structs whose exported members are all supported types — pkg.go.dev/golang.org/x/mobile/cmd/gobind). Panics crossing the boundary exit the program.
- **Exemption worth knowing:** `importerPath == ""` → "anything listed on the command line is fine." So `go test ./internal/...` and `gomobile bind ./internal/x` *load* fine; it's the generated importer that fails.

---

# 2. Single module vs multi-module + `go.work`

Nine consequences, each verified or quoted:

**a) `go install <mod>/cmd/x@VERSION` hard-fails if that module's go.mod has any `replace`.** Verified end-to-end over a `file://` GOPROXY:
```
go: example.com/kk/cli/cmd/kolk@v0.1.0 (in example.com/kk/cli@v0.1.0):
	The go.mod file for the module providing named packages contains one or
	more replace directives. It must not contain directives that would cause…
```
The same module without `replace` installed cleanly as `kolk`. Documented in `go help install`. **Real world:** `gopls/go.mod` at master carries `replace golang.org/x/tools => ..`, but the released tags `gopls/v0.19.0` and `gopls/v0.20.0` have **no** replace — swapped for a pseudo-version require at release time. Multi-module ⇒ a two-step release dance forever.

**b) `go.work` is the sanctioned alternative to `replace`, but shouldn't be committed.** go.dev/ref/mod#workspaces: it is "generally inadvisable to commit `go.work` files into version control" — it can override a developer's own go.work, and "may cause a CI system to select and thus test the wrong versions of a module's dependencies." (Exception granted for modules "developed exclusively with each other".) k8s commits a *generated* one.

**c) `go install pkg@version` ignores `go.work` entirely.** `work/build.go:863` sets `loaderstate.RootMode = modload.NoRoot`; `modload/init.go` `FindGoWork()` returns `""` when `RootMode == NoRoot`. The published-version path is never the workspace path.

**d) `./...` breaks — the most under-appreciated cost.** Verified:
- go.work root that is *not* itself a module: `go build ./...` / `go list ./...` → `pattern ./...: directory prefix . does not contain modules listed in go.work or their selected dependencies`.
- go.work root that *is* a module with `use (. ./core ./cli)`: `go list ./...` returns **only the root module's packages**. Nested modules silently excluded.
- Working forms: `go list ./core/... ./cli/...`, or `go list all`.

⇒ every Makefile, CI step, agent prompt and muscle-memory `go test ./...` needs rewriting, and the failure mode is *silently skipped tests*.

**e) Dependency drift is per-module.** go.dev/ref/mod: `go mod init/why/edit/tidy/vendor` and `go get` "always operate on a single main module." `go work sync` exists (`go help work sync`) but only *upgrades* each member to the workspace's MVS build list — it never downgrades, and it's a manual step.

**f) Version tagging.** Subdirectory module ⇒ prefixed tags. Verified in the wild: `gopls/v0.20.0` (golang/tools), `sdk/metric/v1.45.0` + `exporters/…` (otel-go, 28 go.mod files), `webview2/v1.0.28` alongside `v3.0.0-beta.12` (wailsapp/wails). So `v0.3.0` releases the root module only; a `core/` submodule needs `core/v0.3.0` — a second, independent tag namespace.

**g) GoReleaser monorepo support is Pro-only (paid).** goreleaser.com/customization/monorepo/: "exclusively available with GoReleaser Pro"; keys `monorepo.tag_prefix` and `monorepo.dir`; one release per invocation; and "Tag prefixes that do not match the module name are not supported by go mods." Crush uses goreleaser (`$schema=…/schema-pro.json`) against **one** module.

**h) CI caching.** actions/setup-go caches modules + build outputs, keyed off `go.mod`/`go.sum` at the repo root by default; multi-module requires `cache-dependency-path` with a glob over every `go.sum` (glob support documented; the exact monorepo YAML example was not in the fetched page).

**i) gopls.** Without a `go.work`, gopls v0.15+ "will guess the builds you are working on based on the set of open files" and executes each operation "in *the default build for that file*" — cross-module References/Implementations are incomplete. A `go.work` gives one logical build plus better memory/perf. Combined with (b), every contributor must hand-create a go.work or lose navigation.

**What large real projects actually do** (surveyed by `gh api` today):

| repo | modules | layout |
|---|---|---|
| charmbracelet/crush | **1** (`github.com/charmbracelet/crush`) | `main.go` at root; ~40 packages **all** under `internal/`; no `pkg/`, no `cmd/` (it's `internal/cmd`) |
| hashicorp/terraform | **1** | `main.go` + `signal_{unix,windows}.go` at root; **everything else in `internal/`** |
| gohugoio/hugo | **1** | `main.go` at root; flat top-level packages + `internal/` |
| grafana/k6 | **1** (`go.k6.io/k6/v2`) | `main.go` at root; small public top-level surface + fat `internal/` |
| cli/cli | **1** (`github.com/cli/cli/v2`) | `cmd/gh` + `cmd/gen-docs`, `pkg/` (legacy, holds the command tree), `internal/` (26 pkgs, where new code goes) |
| ollama/ollama, junegunn/fzf, jesseduffield/lazygit, golangci/golangci-lint | **1** | mixed conventions, all single-module |
| kubernetes/kubernetes | **many** + committed generated `go.work` (`use (.)` + ~35 `staging/src/k8s.io/*`) | because it *publishes* client-go, api, apimachinery… independently |
| open-telemetry/opentelemetry-go | **28** go.mod, no root go.work, per-module `replace ../` + prefixed tags | same reason |
| golang/tools | 2 (`gopls/` submodule) | gopls has its own release cadence |

**Pattern: repos go multi-module only when they must publish several independently-versioned *libraries*. Application/CLI repos stay single-module.**

**Decision-relevant conclusion:** one module for kolkrabbi. Every reason to split (desktop, mobile, dashboard) is served by §1's path-prefix rule or by an exported facade, and splitting later is a `git mv` + one `go.mod` + a tag-prefix convention. Splitting *now* buys goreleaser Pro, a two-step release, broken `./...`, an uncommittable go.work, and per-module tidy — for a solo dev, all cost, no benefit.

---

# 3. `cmd/` when module name (`kolkrabbi`) ≠ binary name (`kolk`)

- **Binary name = last element of the package *directory*** for `cmd/` subdirs. Verified: module `example.com/kolkrabbi`, `go install ./cmd/kolk` → `$GOBIN/kolk`; `go build ./cmd/kolk` → `./kolk`. (`go help gopath`: "Each command is named for its source directory, but only using the final element.")
- **For a root `main` package, the name comes from the MODULE path**, with the major-version suffix stripped. Verified: module `example.com/kolkrabbi` → binary `kolkrabbi`; module `example.com/kolkrabbi/v2` → binary `kolkrabbi`.

  ⇒ **The crush/terraform/hugo "main.go at repo root" style produces `kolkrabbi`, not `kolk`. `cmd/kolk/main.go` is mandatory here.** (It's also what go.dev/doc/modules/layout recommends for mixed repos.)
- **`go install github.com/OWNER/kolkrabbi/cmd/kolk@latest`** — verified working end-to-end (installed as `kolk`). Constraints from `go help install`: args must be package *paths* (no relative/absolute file paths), all args at the same version, all in one module, main packages only, no main module assumed, **no `replace`/`exclude` in that module's go.mod**, vendor/ ignored.
- **`@latest` semantics** (`modload/query.go`, `Query` doc): "the latest available, allowed tagged version, with non-prereleases preferred over prereleases. If there are no tagged versions in the repo, latest returns the most recent commit." So the install command works *before* you tag v0.1.0, resolving a pseudo-version off the default branch.
- **Blocker today:** module path is bare `kolkrabbi` — no dot in the first element, not resolvable, so `go install` can never work. Must become `github.com/<owner>/kolkrabbi` (PLAN item 1). Cost: **24 import lines across 9 files** (`grep -rn '"kolkrabbi/internal'`) — one sed pass.
- GoReleaser then needs `builds: [{ main: ./cmd/kolk, binary: kolk }]`.

---

# 4. `pkg/` vs `internal/` vs flat top-level — what's actually official in 2026

**Official (Go team):** https://go.dev/doc/modules/layout, "Organizing a Go module".
- On internal: *"Initially, it's recommended placing such packages into a directory named `internal`; this prevents other modules from depending on packages we don't necessarily want to expose and support for external uses. Since other projects cannot import code from our `internal` directory, we're free to refactor its API and generally move things around without breaking external users."* Plus: *"It's recommended to keep packages in `internal` as much as possible."*
- On cmd: *"A common convention is placing all commands in a repository into a `cmd` directory; while this isn't strictly necessary in a repository that consists only of commands, it's very useful in a mixed repository that has both commands and importable packages."*
- **It never mentions `pkg/`.**

**golang-standards/project-layout — community, contested, self-disclaimed. Confirmed true.** 56,483 stars, owner type Organization (not golang). Its own README says, in bold code formatting: **"This is `NOT an official standard defined by the core Go dev team`"**, links readers to go.dev/doc/modules/layout, and adds: *"If you are trying to learn Go or if you are building a PoC or a simple project for yourself this project layout is an overkill. Start with something really simple instead (a single `main.go` file and `go.mod` is more than enough)."*

**Tooling truth:** `pkg/` has **zero** meaning to the go command. The only specially-treated directory names are `internal`, `vendor`, `testdata`, and anything beginning with `.` or `_` (`go help packages`: *"Directory and file names that begin with '.' or '_' are ignored by the go tool, as are directories named 'testdata'"*). `cmd/` is special **only inside GOROOT** (*"Import paths beginning with 'cmd/' only match source code in the Go repository"*).

**2026 practice:** no `pkg/` in crush, terraform, hugo, k6, ollama, fzf. `pkg/` present in cli/cli (legacy — their newer code goes to `internal/`), lazygit, golangci-lint.

⇒ **`cmd/kolk` + `internal/*` + a deliberately tiny exported top-level surface. No `pkg/`.**

---

# 5. Engine importable as a library, binary guts private

The proven pattern is a **thin exported facade over a fat `internal/` implementation**:

- **grafana/k6 v2** is the closest analogue: exported top-level `lib`, `metrics`, `js`, `api`, `cloudapi`, `output`, `errext`, `ext`, `secretsource`, `subcommand`; implementation in `internal/{api,cmd,js,lib,metrics,output,execution,loader,dashboard,…}`. (Their `lib/doc.go` candidly calls it "a kitchen sink of… anything that doesn't belong in a specific part of the codebase" — a curated surface, not an accident.)
- **otel-go** is the multi-module version of the same idea: exported `trace`/`metric`/`log`/`sdk/*`, shared guts in root `internal/`, reachable by every submodule via the §1 path-prefix rule.
- **terraform** is the opposite pole: literally everything in `internal/` — "we ship a binary, not a library."
- **crush** is terraform-shaped: even its daemon split (`internal/proto`, `internal/client`, `internal/server`) is internal, because they publish no SDK.

Concrete shape for kolkrabbi, single module `github.com/OWNER/kolkrabbi`:

```
cmd/kolk/            main → binary "kolk"          (mandatory, §3)
cmd/kolkd/           later, if the daemon ships separately
internal/            everything the prototype has today, moved verbatim:
                       api agent tools session checkpoint stats config mockrouter
                     plus: internal/tui  internal/server  internal/shell  internal/lock
                       internal/dash/  (SPA assets live INSIDE this dir, §7)
  ── exported surface, added only when a second consumer exists ──
engine/              Engine, Options, Event — thin wrappers over internal/agent
proto/               daemon event/JSON wire types
mobile/              gomobile-bind facade — MUST be exported (§1), bind-safe types only
```

Two rules that make it hold:
1. **Exported packages must not leak `internal/` types in their signatures.** It compiles fine inside the module, but an external importer can't name the type, declare a variable of it, or implement the interface — the API is dead on arrival. Express the facade in stdlib types + your own exported types.
2. **`proto/` is the one package worth exporting early.** Desktop shell, mobile client, and any third-party tool all need the event schema, and versioning it is cheap. Everything else can stay internal until a real consumer appears.

Versioning pressure: exporting commits you to Go's compatibility expectations at v1. At v0.x you can break freely — go.dev/ref/mod: *"Major version suffixes are not allowed at major versions v0 or v1."*

---

# 6. GOOS-specific code

**The suffix rule** (`go help buildconstraint`): *"Naming a file `dns_windows.go` will cause it to be included only when building the package for Windows; similarly, `math_386.s` will be included only when building the package for 32-bit x86."* Forms: `name_GOOS.go`, `name_GOARCH.go`, `name_GOOS_GOARCH.go` (+ `_test` variants). The suffix must be a **known GOOS/GOARCH** from `internal/syslist`.

### ⚠️ The trap, verified: `_unix.go` carries NO constraint

`unix` is not a GOOS. A file `shell_unix.go` with no build line compiles **everywhere**:

```
GOOS=darwin  GoFiles: [main.go shell_darwin.go shell_unix.go]  Ignored=[shell_windows.go]
GOOS=linux   GoFiles: [main.go shell_unix.go]                  Ignored=[shell_darwin.go shell_windows.go]
GOOS=windows GoFiles: [main.go shell_unix.go shell_windows.go] Ignored=[shell_darwin.go]
```

Same for `_other`, `_posix`, `_stub`, `_generic` — all decorative.

The **`unix` build tag** (not filename suffix) *does* exist since Go 1.19: `go/build/build.go` → `if name == "unix" && syslist.UnixOS[ctxt.GOOS]`. `UnixOS` = **aix, android, darwin, dragonfly, freebsd, hurd, illumos, ios, linux, netbsd, openbsd, solaris** (`internal/syslist/syslist.go`). Verified selection for `//go:build unix`: darwin ✓, linux ✓, android ✓, windows ✗, js/wasm ✗, plan9 ✗. Note it **includes darwin/ios/android**, so `//go:build unix` collides with a `_darwin.go` file — write `//go:build unix && !darwin`.

### The reference implementation to copy: crush

Every OS-divergent file is `//go:build !windows` / `//go:build windows`; the filenames are pure convention (verified by reading each file's first line):

| crush file pair | divergence |
|---|---|
| `internal/shell/exec_unix.go` / `exec_windows.go` | **shell execution** |
| `internal/cmd/root_other.go` / `root_windows.go` | process/signal setup |
| `internal/client/dial_other.go` / `dial_windows.go` | unix socket vs named pipe |
| `internal/server/net_other.go` / `net_windows.go` | listener |
| `internal/config/config_unix.go` / `config_windows.go` | **config dirs** |
| `internal/config/atomicwrite_unix.go` / `atomicwrite_windows.go` | rename-over-existing |
| `internal/lock/lock_unix.go` / `lock_windows.go` | **file locking** |
| `internal/fsext/drive_other.go` / `drive_windows.go`, `owner_windows.go` | drive letters, ACLs |
| `internal/agent/tools/mcp/process_unix.go` / `process_other.go` | process groups/kill |
| `internal/ui/notification/icon_darwin.go` / `icon_other.go` | notifications |

(`process_other.go` actually carries `//go:build windows` — conclusive proof the name means nothing.) Terraform does the same at repo root: `signal_unix.go` / `signal_windows.go`.

### Where each divergence belongs for kolk

- **Shell exec.** `internal/tools/tools.go:119` hardcodes `exec.CommandContext(cctx, "bash", "-c", a.Command)` — the single Windows blocker in the prototype. Move behind a `Shell` interface in `internal/shell` with `exec_unix.go` / `exec_windows.go` (`cmd.exe /c` or `powershell -Command`; quoting rules differ entirely). Also: on Windows `exec.LookPath` no longer resolves `.`-relative binaries — test with `errors.Is(err, exec.ErrDot)` (*"cannot run executable found relative to current directory"*) rather than letting the bash tool fail cryptically.
- **Config/data dirs.** `os.UserConfigDir()` → `$XDG_CONFIG_HOME` or `$HOME/.config` (Unix), `$HOME/Library/Application Support` (Darwin), `%AppData%` (Windows). `os.UserCacheDir()` → `$XDG_CACHE_HOME`/`$HOME/.cache`, `$HOME/Library/Caches`, `%LocalAppData%`. Prototype hardcodes `~/.config/kolk` at `main.go:32-40` (plus a user-facing string at `main.go:395`). Route through one function in `internal/config`; decide XDG-everywhere vs `os.UserConfigDir` once.
- **ANSI/terminal.** No stdlib API enables VT processing on Windows; you need `golang.org/x/sys/windows` (`SetConsoleMode` + `ENABLE_VIRTUAL_TERMINAL_PROCESSING`) or a library. crush depends on `charmbracelet/x/ansi`, `charmbracelet/x/term`, `colorprofile`, `mattn/go-isatty`, `golang.org/x/sys`. Isolate in `internal/term` so daemon/desktop/mobile clients never touch it.
- **File locking.** No exported stdlib file-lock API (the runtime's `internal/filelock` isn't importable, and isn't even at that path in 1.26.4). SQLite/WAL covers the dashboard DB; for config/session files write `internal/lock` with `flock` vs `LockFileEx`, like crush.
- **Atomic writes.** `internal/session/session.go:50,64` uses tmp+`os.Rename` — atomic on POSIX, fragile on Windows when the target is open. That's exactly why crush split `atomicwrite_{unix,windows}.go`.
- **Mobile/cgo interaction.** Verified: `GOOS=ios GOARCH=arm64 go list` → *"ios/arm64 requires external (cgo) linking, but cgo is not enabled"*; and `cmd/gomobile/env.go` sets `CGO_ENABLED=1` in two places. **The "no cgo" rule survives every CLI/desktop target and is unavoidably violated by any gomobile build** — an independent argument for the thin-client-over-daemon mobile path in `platform-strategy.md`.
- **Cross-compile template.** crush's `.goreleaser.yml`: `CGO_ENABLED=0`, `-trimpath`, `-s -w -X …/internal/version.Version={{.Version}}`, goos = linux darwin windows freebsd openbsd netbsd android × amd64 arm64 386 arm.

---

# 7. `//go:embed` — rules and what they force

From the `embed` package doc (Go 1.26.4), each verified by build:

| rule | verification |
|---|---|
| *"The patterns are interpreted relative to the package directory containing the source file… Patterns may not contain '.' or '..' or empty path elements, nor may they begin or end with a slash."* | `//go:embed ../../dash/dist` → `pattern ../../dash/dist: invalid pattern syntax`; `//go:embed /dash/dist` → same. **You cannot embed from a parent or sibling directory. Full stop.** |
| *"Patterns must not match files outside the package's module … or any directories containing go.mod (these are separate modules)."* | a `dist2/` containing a `go.mod` → `cannot embed directory dist2: in different module` |
| symlinks refused | `ln -s ../../dash/dist linked` → `cannot embed irregular file linked`. **No symlinking a built SPA into place.** |
| *"each pattern … must match at least one file or non-empty directory"* | empty dir → `cannot embed directory empty: contains no embeddable files`; missing dir → `no matching files found` |
| *"files with names beginning with '.' or '_' are excluded"* unless the `all:` prefix is used | **This bites SPA builds.** With `//go:embed dist`: only `dist/index.html`, `dist/app.css`. With `//go:embed all:dist`: also `dist/_next/static/a.js` and `dist/.vite/manifest.json`. |
| package-scope vars only; `string`/`[]byte`/`embed.FS`; string/[]byte need one pattern matching one file | per doc |

**Precedent:** crush's HTML stats report — `internal/cmd/stats.go` has `//go:embed stats/index.html`, `stats/index.css`, `stats/index.js` + three SVGs, with assets at `internal/cmd/stats/`. Assets live **inside the embedding package's directory**. Exactly `kolk dash`'s shape.

**Implications for `kolk dash`:**
1. SPA *source* can live anywhere (`web/`), but the JS build must **output into the embedding package's directory** — e.g. `internal/dash/dist/`. No `../`, no symlink.
2. Use `//go:embed all:dist`, or you'll ship a dashboard missing its `_next/`/`.vite/` chunks with no build error.
3. Never let a `go.mod` appear under the embedded tree.
4. A fresh clone must build without the JS toolchain: commit a placeholder `dist/index.html`, **or** put the embedding file behind a build tag with a stub fallback — otherwise `go build ./...` fails on a clean checkout.
5. Serve with `http.FS(fs.Sub(assets, "dist"))`.

---

# Flags — not verified / lower confidence

- **actions/setup-go monorepo YAML.** `cache-dependency-path` and its glob support are documented; the exact multi-`go.sum` example lives on an "Advanced usage" page that wasn't in the fetched content.
- **"No stdlib API for Windows VT/ANSI."** Asserted from my own knowledge, corroborated only indirectly by crush's dependency set (`x/sys`, `charmbracelet/x/ansi`, `x/term`, `colorprofile`, `go-isatty`). I did not fetch a doc stating the absence.
- **gomobile was not actually run** (not installed; needs Xcode/NDK). The "gobind module can't import `internal/`" conclusion comes from reading `cmd/gomobile/bind.go` (`f.AddModuleStmt("gobind")`) + `bind_iosapp.go` (`src/gobind` temp dir) plus an exact simulation with a module literally named `gobind`. High confidence, not an end-to-end run.
- **GoReleaser monorepo = Pro** is as of today's fetch of goreleaser.com/customization/monorepo/; tiering can change.
- **otel-go's `replace go.opentelemetry.io/otel => ../` in `sdk/go.mod`** is at HEAD; I didn't check whether it's stripped at release tags. (Irrelevant to the `go install` rule — `sdk` has no main packages.)
- **Wails v3 / Tauri layout demands** beyond "a module importing `wails/v3/pkg/application`" / "any binary as a sidecar" were not investigated.
- Minor: bare module path `kolkrabbi` still builds locally; only publishing/`go install` requires a resolvable path.