<!-- Verified constraint report produced 2026-08-22 by the item-2 architecture workflow.
     Source of truth for docs/plan/02-architecture.md. Re-verify before relying on version-specific claims. -->

# Facts: what a desktop shell imposes on kolkrabbi's repo structure

Read first: `/Users/francomichetti/kolkrabbi/PLAN.md` (items 1, 2, 19, 20), `/Users/francomichetti/kolkrabbi/docs/research/platform-strategy.md`, `ecosystem.md`, `dashboard.md`, plus `main.go` (root `package main`, imports `kolkrabbi/internal/{agent,api,checkpoint,config,session,stats}`) and `go.mod` (`module kolkrabbi`, `go 1.22.2`, zero deps).

---

## 1. Wails v3 — layout, status, cadence

**Status (verified via `gh api repos/wailsapp/wails/releases`, 2026-08-22)**
- `v3.0.0-beta.0` 2026-08-02 → `v3.0.0-beta.12` 2026-08-21. Betas are near-daily (12 in 20 days); before that, `v3.0.0-alpha2.*` daily since 2023. `v2.14.0` (stable) shipped 2026-08-10 — v2 is still maintained.
- Beta blog (`docs/src/content/docs/blog/2026-08-02-wails-v3-beta.md`): *"This is a beta release, not the final 3.0 release. The desktop API is stable and teams are already using v3 in production, but you should test thoroughly before deploying."* / *"Wails v2 remains the current stable release."* First v3 alpha tag was 18 Jan 2023 — 3.5 years in alpha. They now have *"explicit beta, release-candidate, and GA milestones"* and a WEP process. **No GA date is published** (`status.mdx` says only "Our goal is to reach a stable v3.0 release").
- Beta compatibility promise (`status.mdx`): Windows amd64/arm64 (WebView2 runtime); macOS Intel + Apple Silicon; Linux amd64/arm64 with **GTK4 + WebKitGTK 6.0** default (GTK3 + WebKit2GTK 4.1 via `-tags gtk3` through v3.0.x, **removed in v3.1**). *"All targets require Go 1.25 or later for development."*
- Mobile: iOS and Android templates + build assets exist in-tree, but *"Experimental mobile support for iOS and Android, available for exploration but outside the desktop beta compatibility promise."*

**Generated layout** (from `docs/.../getting-started/your-first-app.mdx` FileTree, cross-checked against `v3/internal/templates/_common` and `v3/internal/commands/build_assets`):

```
<project>/
  main.go              package main; //go:embed all:frontend/dist
  greetservice.go      a "service" = a plain Go struct
  go.mod  go.sum  Taskfile.yml  .gitignore  README.md
  frontend/            index.html, main.js, package.json, public/, bindings/ (generated)
  build/
    config.yml         <- v3's config file
    Taskfile.yml       common tasks
    appicon.png  appicon.icon/
    darwin/   Info.plist, Info.dev.plist, icons.icns, Assets.car, dmg-*.png/icns, Taskfile.yml
    windows/  icon.ico, info.json, wails.exe.manifest, nsis/project.nsi, msix/, Taskfile.yml
    linux/    nfpm/nfpm.yaml + scripts/, appimage/build.sh, Taskfile.yml
    ios/      Taskfile.yml, main.m, main_ios.go, Xcode project generation
    android/  Gradle project, AndroidManifest.xml, WailsBridge.java, ...
    docker/   Dockerfile.cross, Dockerfile.server
  bin/                 build output (BIN_DIR)
```

- **There is no `wails.json` in v3.** v2 had it at the repo root — verified against two live Wails v2.11.0 repos (`ArvinLovegood/go-stock`, `Syngnat/GoNavi`), both of which have `/wails.json`. v3 replaced it with `build/config.yml`.
- **go-task is a hard build dependency**; the root `Taskfile.yml` dispatches to `build/<goos>/Taskfile.yml` via a `GOOS` var. CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@latest`, then `wails3 setup`.

**Can it consume an existing Go package in the same module?**
- Services are ordinary Go structs registered with `application.NewService(&X{})`. The generator is `wails3 generate bindings [patterns...]`; `v3/internal/commands/bindings.go`: *"No input pattern, load package from current directory"* → `patterns = []string{"."}`, but **arbitrary Go package patterns are accepted**. Output dir defaults to `frontend/bindings` (flag `-d`). So the engine can live in any package and be bound. ✅
- But the generated build tasks run **`go build {{.BUILD_FLAGS}} -o "{{.OUTPUT}}"` with no package argument** (`build/darwin/Taskfile.yml`, `linux/`, `windows/`) — i.e. the main package is assumed to be in the task's dir = project root. The Taskfile is yours to edit (that's the v3 selling point: *"a visible, Taskfile-based build system you can inspect, extend, and debug"*), but **a non-root Wails main is undocumented and I found no repo doing it** — ⚠️ inferred, unverified.
- `wails3 init` flags (`v3/internal/flags/init.go`): `-d` project dir, `-mod` module path, `-p` package name. It writes its own `go.mod` from `go.mod.tmpl` → **by default `wails3 init` creates a new module**, not a package in yours.
- **Dependency + cgo cost.** `go.mod.tmpl` requires `github.com/wailsapp/wails/v3` plus ~35 indirect deps (go-git/v5, purego, go-winio, ProtonMail/go-crypto, adrg/xdg, samber/lo, x/crypto, x/net…). `build/darwin/Taskfile.yml` sets `CGO_ENABLED: 1` (+ `MACOSX_DEPLOYMENT_TARGET: 12.0`); `build/linux/Taskfile.yml` sets `CGO_ENABLED: 1` and falls back to a Docker cross-image when no C compiler exists; `build/windows/Taskfile.yml` defaults `CGO_ENABLED: 0`. **Wails v3 desktop = cgo on macOS and Linux**, needs Xcode CLT / gtk4 + webkitgtk-6.0. Binary size per `concepts/build-system.mdx`: *"~15MB"* on all three.
- Bonus, relevant to the roadmap: `task build:server` builds `-tags server,production` = *"no GUI, HTTP server only"*, with `build/docker/Dockerfile.server`. Wails v3 itself expects the same Go code to run headless.

---

## 2. Tauri v2 + external Go sidecar

- **Declaration**: `bundle.externalBin` in `src-tauri/tauri.conf.json`. Config reference, verbatim: *"A list of—either absolute or relative—paths to binaries to embed with your application. Note that Tauri will look for system-specific binaries following the pattern `binary-name{-target-triple}{.system-extension}`. E.g. for the external binary 'my-binary', Tauri looks for: `my-binary-x86_64-pc-windows-msvc.exe` for Windows, `my-binary-x86_64-apple-darwin` for macOS, `my-binary-x86_64-unknown-linux-gnu` for Linux so don't forget to provide binaries for all targeted platforms."*
- **Location**: relative paths resolve **from `src-tauri/`**; the convention `"binaries/my-sidecar"` → `<root>/src-tauri/binaries/my-sidecar-<TRIPLE>`. Triple via `rustc --print host-tuple`.
- **Permissions**: `src-tauri/capabilities/default.json` must grant `shell:allow-execute` (or `shell:allow-spawn` for `.spawn()`) with `{"name": "binaries/app", "sidecar": true}`; requires `tauri-plugin-shell`.
- **Bundling — verified in source, not just docs**:
  - `crates/tauri-bundler/src/bundle/settings.rs` → `copy_binaries()` strips the suffix on copy: `.replace(&format!("-{}", self.target), "")`. So `kolk-aarch64-apple-darwin` on disk becomes `kolk` in the bundle.
  - `crates/tauri-bundler/src/bundle/macos/app.rs`: sidecars are copied into `<App>.app/Contents/MacOS/` and each is pushed onto `sign_paths` with `is_an_executable: true`, with the comment *"Sign frameworks and sidecar binaries first, per apple, signing must be done inside out"*. **Tauri signs and notarizes your Go binary for you on macOS.** (Windows sidecar signing: not addressed in the docs I read — ⚠️ unverified.)
- **Project layout** (`v2.tauri.app/start/project-structure/`): `src-tauri/` sits at the repo top level next to the frontend, containing `Cargo.toml`, `Cargo.lock`, `build.rs`, `tauri.conf.json`, `src/{main.rs,lib.rs}`, `icons/`, `capabilities/`.
- **Config paths can reach outside `src-tauri`** — Jan uses `"frontendDist": "../web-app/dist"`; Portmaster uses `"desktopTemplate": "../../../packaging/linux/portmaster.desktop"`. So `src-tauri` need not own the repo root or the frontend.
- **Per-OS config split is native**: Jan ships `tauri.conf.json` + `tauri.macos.conf.json` / `tauri.windows.conf.json` / `tauri.linux.conf.json` / `tauri.ios.conf.json` / `tauri.android.conf.json`.
- **CI consequence**: 4–6 prebuilt Go binaries must exist at `src-tauri/binaries/kolk-<triple>` *before* `tauri build`, and Tauri builds per-OS on native runners — the official GH Actions matrix is `macos-latest` (×2 targets), `ubuntu-22.04`, `ubuntu-22.04-arm`, `windows-latest`.

---

## 3. Packaging & signing per OS; what GoReleaser does and doesn't cover

**Platform gates**
- **macOS**: Developer ID Application certificate + Apple Developer Program (Tauri docs: *"either paid (99$ per year) or on the free plan (only for testing and development purposes)"*; *"Only the Apple Developer Account Holder can create Developer ID Application certificates"*), hardened runtime, secure timestamp, `notarytool`. Notarization is required for Developer ID distribution; unnotarized apps are blocked/warned by Gatekeeper.
- **Windows**: OV or EV code-signing cert. CA/Browser Forum *Baseline Requirements for Code Signing* (v3.11.0, dated 2026-06-16), effective **2023-06-01**: *"CAs SHALL ensure that the Subscriber's Private Key is generated, stored, and used in a suitable Hardware Crypto Module that meets or exceeds the requirements specified in section 6.2.7.4.1."* → hardware token/HSM or a cloud signing service. Tauri supports `signtool` by thumbprint, `relic` (Azure Key Vault), `artifact-signing-cli` (Azure Artifact Signing), or any tool via `bundle.windows.signCommand`. EV gets immediate SmartScreen reputation; OV builds it over time.
- **Linux**: no signing gate. Wails v3 ships nfpm (deb/rpm) + a linuxdeploy-based `appimage/build.sh`; Tauri bundles AppImage/deb/rpm natively plus Flatpak/Snap/AUR guides.

**GoReleaser — the hard line** (from the Pro feature list and per-page banners)
- **OSS covers**: Go builds, archives, checksums, `signs` (default: detached GnuPG sig on checksums; cosign supported), `binary_signs`, `nfpms` → *"`.deb`, `.rpm`, `.apk`, `.ipk`, Archlinux, and Windows `.msix`"*, Homebrew formula tap, Scoop, winget, Snapcraft, Docker, GitHub/GitLab releases, **and cross-platform macOS notarization of plain binaries** (`notarize.macos`, needs `sign.certificate`/`sign.password` p12 + App Store Connect `issuer_id`/`key_id`/`key`; runs off-macOS).
- **OSS explicitly excludes bundles**: *"Do not use this method if you create App Bundles. App Bundles in which only the binary is signed/notarized are deemed damaged by macOS."*
- **Pro-only** ($165/yr personal, $15/mo): *"Create macOS App Bundles (.app)"* (v2.4), *"Create macOS disk images (.dmg)"*, *"Create macOS installers (.pkg)"*, *"Create Windows installers (.msi) with Wix"*, *"Create Windows installers (.exe) with NSIS"* (v2.14), *"Native sign and notarize macOS App Bundles, Disk Images, and Installers"* (`notarize.macos_native`, v2.8, must run on macOS), **"Import pre-built binaries with the prebuilt builder"**, **"Use GoReleaser within your monorepo"**, *"Publish versioned Homebrew Casks"*.
- **AppImage: not supported at all** (not in nfpm's format list, not in the Pro list).
- ⇒ **GoReleaser OSS cannot release a Wails or Tauri desktop app.** It can't build `.app`/`.dmg`/`.msi`/NSIS, can't notarize a bundle, and can't even *import* artifacts another tool produced (`prebuilt` = Pro). Its OSS notarize path works only for the bare `kolk` binary — which is exactly what you want it for.
- Config file: `.goreleaser.yaml` at repo root; accepted variants in precedence order: `.config/goreleaser.yml`, `.config/goreleaser.yaml`, `.goreleaser.yml`, `.goreleaser.yaml`, `goreleaser.yml`, `goreleaser.yaml`.

**Where packaging config conventionally lives (observed, not theorized)**

| Project | Location |
|---|---|
| GoReleaser convention | `/.goreleaser.yaml` or `/.config/goreleaser.yaml` |
| Wails v3 | `/build/` (`config.yml` + `darwin/ windows/ linux/ ios/ android/ docker/`) |
| Tauri v2 | `/src-tauri/` (`tauri.conf.json` + `tauri.<os>.conf.json`, `capabilities/`, `icons/`, `templates/{nsis,wix}`) |
| Tailscale | `/release/{deb,rpm,dist}` + `/packages/deb` |
| Portmaster | `/packaging/{linux,windows}` at root, referenced from `src-tauri` via `../../../` |
| Ollama | `app/ollama.iss` (Inno Setup) + `scripts/build_windows.ps1`, `scripts/build_darwin.sh` |
| Goose | `ui/desktop/forge.config.ts`, `forge.deb.desktop`, `forge.rpm.desktop`, `entitlements.plist` |

---

## 4. Precedents (repo layouts, verified via `gh api`)

**(a) Ollama** — 179k★, Go. *Go core + own webview, same module.* Engine at module root (`api/ server/ agent/ llm/ …`, root `main.go` = CLI). Desktop app is a sibling package **in the same Go module** at `app/`: `app/cmd/app` (main), `app/webview/{webview.go, webview.cc, glue.c, WebView2.h}` — a hand-rolled cgo webview, **not Wails** — `app/ui/app` (React + Vite), `app/updater/`, `app/wintray/`, `app/darwin/`, `app/ollama.iss` (Inno Setup). Dev loop: `go generate ./... && go run ./cmd/app`; `npm run dev` + `-dev` flag loads the Vite server. Its `scripts/deps_local.sh` / `deps_release.sh` show the desktop app consuming a *built copy* of the ollama binary (local or from a GitHub release) — sidecar thinking inside a monorepo.

**(b) Safing Portmaster** — 13.6k★, Go. *Go core + Tauri v2 shell, nested three deep.* Go module at root (`service/ base/ spn/ cmds/ runtime/`); shell at `desktop/tauri/src-tauri/` (`Cargo.toml`, `tauri.conf.json5`, `capabilities/`, `templates/{nsis,wix}`, `Cross.toml`), UI at `desktop/angular/`, packaging at `/packaging/{linux,windows}`. The Go core binary is staged to `desktop/tauri/src-tauri/binary/portmaster-core` and installed via `bundle.linux.deb.files` / `rpm.files` maps (`"/usr/lib/portmaster/portmaster-core": "binary/portmaster-core"`) — **not** `externalBin`, because it's a systemd daemon, not a spawned child. UI↔core is `tauri-plugin-websocket`. Targets `["deb","rpm","nsis"]` (msi commented out), with custom WiX fragments and NSIS `installerHooks`. **This is structurally the closest thing to kolk's daemon+shell plan that exists.**

**(c) Jan** — 44.1k★. *Tauri v2 at root with real sidecars.* `/src-tauri/` (root), `/web-app/`, `/core/`, `/extensions/`, `/flatpak/`, `/mlx-server/`. Uses **both** mechanisms: `"externalBin": ["resources/bin/bun", "resources/bin/uv"]` (triple-suffixed, spawnable) *and* `"resources": [..., "resources/bin/jan-cli"]` (plain resource). Linux additionally maps `"usr/bin/bun"` through `bundle.linux.deb.files`. Per-OS targets: macOS `["app","dmg"]`, Windows `["nsis","msi"]`, Linux `["deb","appimage"]`.

**(d) Goose** — 53.2k★, Rust core. *Native shell + core binary as sidecar.* `ui/desktop/` is Electron Forge (`forge.config.ts` with `extraResource: ['src/bin', …]`, `osxSign`, `osxNotarize`, makers zip/deb/rpm/flatpak); the core binary is staged per-platform under `ui/goose-binary/goose-binary-{darwin-arm64,darwin-x64,linux-arm64,linux-x64,win32-x64}/`. Shows the per-platform staging-directory pattern is language-independent.

**(e) Tailscale** — the best analogue for the *whole* roadmap. Core is a plain Go module `tailscale.com` with **no UI in it** (`cmd/ ipn/ client/ net/ …`), packaging at `release/{deb,rpm,dist}` + `packages/deb`. The Android app is a **separate repo and separate Go module**, `github.com/tailscale/tailscale-android`, whose `go.mod` reads `require tailscale.com v1.103.0-pre.0.…` + `golang.org/x/mobile`, with `libtailscale/` as the gomobile bind package and `android/` as the Gradle project. **The core is a normal importable dependency; every shell is its own module.**

**(f) Wails inside an existing Go repo (v2, verified working)** — `Syngnat/GoNavi` (1.9k★): one module (`GoNavi-Wails`, Go 1.25, `wails/v2 v2.11.0`) holding root `main.go` = desktop app, `internal/`, `frontend/`, `build/{darwin,windows}`, **plus** `cmd/gonavi` (CLI), `cmd/gonavi-mcp-server`, `cmd/*-driver-agent`, plus Dockerfiles for cli/web-server/mcp-server. `ArvinLovegood/go-stock` (7.3k★, Wails v2.11.0): root `main.go` + `app*.go` = desktop, engine in `backend/`, `frontend/`, `build/`, `wails.json`. Both prove one Go module can hold a Wails desktop main *and* other `cmd/` binaries — but **both put the Wails main at the module root and the CLI in `cmd/`**, the reverse of what kolk wants.

**Gap**: I found **no** AI-agent CLI shipping a Go-core desktop app via Wails v3 or Tauri+Go-sidecar. Ollama (Go + own cgo webview) and Goose (Rust core + Electron sidecar) are the closest.

---

## 5. The key question: does deferring Wails-vs-Tauri force anything now?

**What it does NOT force.** Window/menu/tray APIs, frontend framework, Rust-vs-Go shell language, bundle formats, icons, entitlements, updater — all of it lives inside `build/` (Wails) or `src-tauri/` (Tauri) and is added later as one new top-level directory. Portmaster bolted `desktop/tauri/` on at depth 3; Ollama bolted `app/` on; Jan put `src-tauri/` at root. **None of them restructured their core to do it.** The deferral is genuinely safe *provided* the five items below are settled now.

**What it DOES force — five decisions, cheap today, breaking later.**

**1. The module path must be a resolvable URL.** Today it's `module kolkrabbi`. A Tauri shell in a separate repo, a `desktop/go.mod`, or a gomobile bind package can only `require` the engine if the path resolves — Tailscale-android's `require tailscale.com v1.103.0-…` is the proof. Change to `github.com/<owner>/kolkrabbi` in PLAN item 1. One line now; a breaking change for every user later.

**2. The engine must not live under `internal/`.** Go's rule: `<module>/internal/…` is importable only from inside that module subtree. A Wails app *in the same module* can import `kolkrabbi/internal/agent`; a separate `desktop/` module, a gomobile bind package, or any third party **cannot**. Everything a shell (or an iPad/Android bind layer) might import has to move to a public path — `core/…` per `platform-strategy.md`. Keep `internal/` for genuinely private plumbing (`mockrouter`, CLI-only helpers). This is the single highest-cost item to defer, because it touches every import in the tree.

**3. The desktop main must be a different binary from `kolk` — and the directory layout has to show that today.** Wails v3's own tasks set `CGO_ENABLED=1` on darwin and linux, require Xcode CLT / gtk4 + webkitgtk-6.0, pull ~35 transitive deps, need Go 1.25+, and produce ~15 MB binaries. Fold that into `kolk` and the 10 ms startup, 6.1 MB binary, no-cgo rule and trivial cross-compilation all die at once. So: `cmd/kolk` stays `CGO_ENABLED=0` and stdlib-first; the shell gets its own directory with its own main — `desktop/` under either choice (Wails: `desktop/main.go` + `desktop/frontend/` + `desktop/build/`; Tauri: `desktop/src-tauri/` + `desktop/web/`). Same directory name either way — that's the point.
   **Corollary that has to happen now: move the root `main.go` into `cmd/kolk/`.** Wails v3's generated build tasks build the package in the project directory (`go build … -o out`, no package arg), and both real Wails-in-a-Go-repo precedents keep the Wails main at the module root. If kolk's CLI is still sitting at the root when a Wails app arrives, they collide over the same directory. `cmd/kolk` + `desktop/` sidesteps that under both options.

**4. The daemon must be importable as a package *and* runnable as a binary — both, not either.** This is the one place the two shells genuinely diverge, and the divergence lands on your code:
   - **Wails links in-process.** A Wails service is a plain Go struct, and `wails3 generate bindings [patterns]` accepts arbitrary package patterns — so the engine can sit in `core/` and be bound directly, no socket at all.
   - **Tauri spawns a binary** and talks over a pipe/socket (Portmaster: `tauri-plugin-websocket` → Go core).
   If the daemon exists only as a `kolk serve` subcommand you can't link it cleanly; if it exists only as a library you can't sidecar it. So structure it as: `core/…` (engine library) · `core/protocol` or `api/` (wire types + event schema, **no transport**) · `serve/` or `core/server` (HTTP+SSE/WS handler as an importable package) · `cmd/kolk` (CLI that mounts it behind `kolk serve`). Wails imports `serve` and skips the socket; Tauri spawns `kolk serve`; the iPad/Android thin client — the actual reason the protocol exists, given App Review 2.5.2 — uses the same wire format.
   **One protocol constraint is decided now, not later**: framing must work identically over a *spawned child process's stdio* and over a *network socket*, because a Tauri sidecar is stdio-attached while mobile is necessarily remote. Same JSON events on both. Claude Code's `--output-format stream-json`, Codex's app-server and pi's `--mode rpc` all take this shape (see `ecosystem.md`).

**5. Release pipelines split, and the repo should say so from the start.** GoReleaser OSS ships `kolk` (including cross-platform notarization of the bare binary) but **cannot** ship a desktop app — `.app`, `.dmg`, `.msi`, NSIS, native bundle notarization, the `prebuilt` importer and monorepo mode are all Pro. Plan for `/.goreleaser.yaml` (CLI, tag-triggered) plus a separate `.github/workflows/desktop.yml` running on native runners per OS calling `wails3 task <os>:package` or `tauri build`. Keep shell-specific packaging assets inside the shell directory (Wails owns `desktop/build/`, Tauri owns `desktop/src-tauri/`), but put anything **both** shells would need — app icon source, entitlements plist, `.desktop` file, systemd unit — in a root `packaging/`, Portmaster-style, so the shell choice never owns them.

**Two smaller consequences worth writing down.** (a) On the Tauri path, macOS signing of the Go binary is free — Tauri copies sidecars into `Contents/MacOS/` and signs inside-out — but CI must emit triple-named copies (`kolk-aarch64-apple-darwin`, `kolk-x86_64-pc-windows-msvc.exe`, …). Pick the CLI binary name now and never let it collide with the desktop binary name. (b) Wails v3 already ships iOS/Android build assets in-tree (Xcode project generation, Gradle + `WailsBridge.java`) and a headless `-tags server` build with a Dockerfile. That is not a reason to pick Wails — mobile code mode is a thin client regardless — but it does mean "Wails is desktop-only" is a false assumption to plan around.

---

## Flagged as unverified / inconsistent

- **A Wails v3 main package in a non-root directory of an existing module**: inferred from (i) the Taskfile being user-owned and (ii) `generate bindings` accepting package patterns. Not documented; not observed in any repo I checked (both Wails precedents are v2 with the main at the module root). Needs a 30-minute spike before committing to Wails.
- **Wails v3 GA date**: none published anywhere in the repo docs.
- **Wails v3 Go floor is inconsistent in its own docs**: `status.mdx` says "Go 1.25 or later"; `installation.mdx` says "Go (At least 1.24)"; `go.mod.tmpl` emits `go 1.24`.
- **Windows sidecar signing in Tauri**: not addressed in the docs I read (macOS behaviour is confirmed from bundler source).
- **GoReleaser `binary_signs` OSS-vs-Pro**: the page doesn't state it explicitly; only its `if` filter is marked Pro, implying the rest is OSS. Not confirmed.
- CA/B private-key-in-hardware requirement quoted from the CA/Browser Forum code-signing requirements page as fetched (doc version 3.11.0, 2026-06-16); effective date 2023-06-01 per its §1.2.2 date table.

**Sources**: [wailsapp/wails releases + `v3/internal/{templates,commands}` + `docs/src/content/docs/**` via `gh api`](https://github.com/wailsapp/wails) · [Tauri sidecar](https://v2.tauri.app/develop/sidecar/) · [Tauri config reference](https://v2.tauri.app/reference/config/) · [Tauri project structure](https://v2.tauri.app/start/project-structure/) · [Tauri distribute](https://v2.tauri.app/distribute/) · [Tauri macOS signing](https://v2.tauri.app/distribute/sign/macos/) · [Tauri Windows signing](https://v2.tauri.app/distribute/sign/windows/) · [Tauri GH Actions](https://v2.tauri.app/distribute/pipelines/github/) · [tauri-apps/tauri `crates/tauri-bundler/src/bundle/{macos/app.rs,settings.rs}`](https://github.com/tauri-apps/tauri) · [GoReleaser Pro feature list](https://goreleaser.com/pro/) · [notarize](https://goreleaser.com/customization/notarize/) · [app_bundles](https://goreleaser.com/customization/app_bundles/) · [dmg](https://goreleaser.com/customization/dmg/) · [msi](https://goreleaser.com/customization/msi/) · [nsis](https://goreleaser.com/customization/nsis/) · [nfpm](https://goreleaser.com/customization/nfpm/) · [sign](https://goreleaser.com/customization/sign/) · [customization index](https://goreleaser.com/customization/) · [Apple notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) · [CA/Browser Forum code-signing requirements](https://cabforum.org/working-groups/code-signing/requirements/) · repo layouts via `gh api`: [ollama/ollama](https://github.com/ollama/ollama), [safing/portmaster](https://github.com/safing/portmaster), [janhq/jan](https://github.com/janhq/jan), [aaif-goose/goose](https://github.com/aaif-goose/goose), [tailscale/tailscale](https://github.com/tailscale/tailscale), [tailscale/tailscale-android](https://github.com/tailscale/tailscale-android), [Syngnat/GoNavi](https://github.com/Syngnat/GoNavi), [ArvinLovegood/go-stock](https://github.com/ArvinLovegood/go-stock)