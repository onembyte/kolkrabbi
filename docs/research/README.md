# docs/research — inputs to the plan

Research gathered 2026-08-21/22 while building the plan-hardening checklist (`PLAN.md`). These are
snapshots: vendor policies, API limits and library status change — re-verify before relying on a
specific number or rule. Each file lists its sources.

| File | Feeds PLAN.md items | Summary |
|---|---|---|
| `orcli.md` | 8, 9, 22 | Review of Theanlegendary/orcli (read over HTTP, nothing cloned/run) + similar OpenRouter CLIs |
| `ecosystem.md` | 6, 9–16, 18, 22 | OpenCode, Crush, Goose, Hermes Agent, Codex CLI, Aider, Gemini CLI, Cline, Kilo, Amp… features worth borrowing; loop/autonomy patterns |
| `subscription-auth.md` | 4 | What Anthropic / OpenAI / Google permit for subscription-backed third-party CLIs (Claude Max via Claude Code etc.) |
| `openrouter.md` | 3, 5, 7, 8, 17 | Free tier limits, routing primitives, `reasoning` param, cost/usage endpoints, OAuth PKCE, rankings |
| `dashboard.md` | 12, 17 | Braintrust concept map → local equivalent; SQLite schema proposal; OTel GenAI conventions |
| `platform-strategy.md` | 2, 11, 19 | Go verdict; core/daemon/frontends architecture; desktop (Wails/Tauri) and iPad (remote execution) paths |
| `constraints/go-layout.md` | 2 | `internal/` import rule, go.work vs single module, `//go:embed` limits, build tags — claims built and run on go1.26.4 |
| `constraints/desktop-packaging.md` | 2, 19 | What Wails v3 / Tauri v2 sidecar impose on repo layout; signing, notarization, goreleaser limits |
| `constraints/mobile-binding.md` | 2, 19 | gomobile `bind` export vocabulary, iPadOS 2.5.2 consequences, thin-client-over-daemon path, Tailscale interim |
