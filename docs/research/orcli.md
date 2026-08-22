# Research: orcli (Theanlegendary/orcli) and similar OpenRouter CLIs

Date: 2026-08-21. Method: read-only over the GitHub API (`gh api` contents/trees/commits) — the
repository was **not cloned, downloaded, or executed** on this machine. Feeds PLAN.md items 8, 9, 22.

## 1. What it is (confirmed)
- Single-file **Python 3 stdlib-only** script (`orcli`, 1,271 lines / 55.7 KB) + `install.sh`,
  `install.bat`, `orcli.bat`, `orcli.command`. No deps (urllib, readline, subprocess).
- Maturity: **0 stars, 0 forks, 3 commits all on 2026-07-02**, 0 releases/tags, no tests, no CI,
  repo description literally "df". Self-labelled "v7.0 — Messenger Edition". Inference: hobby /
  vibe-coded weekend tool.

## 2. Architecture & features (confirmed from source)
- **One mode**: chat REPL (`input()` loop, `\` line-continuation) plus one-shot `orcli "q"` /
  `cat x | orcli`.
- **Model selection**: `GET /models`, keep only `pricing.prompt==0 && completion==0`, rank by
  substring match against a hard-coded `CODING_PRIORITY` list (DeepSeek R1, Qwen3, Gemini
  flash-thinking, Llama 3.3…), rest alphabetical. `/model` = numbered picker showing ctx length,
  ⭐ for priority, ✗ for exhausted.
- **Auto-rotation**: on HTTP 429/503/402 *or* body keywords ("rate limit", "quota", "credits",
  "context length"…) → add model to `exhausted`, hop to next, re-send same message; per-turn
  `tried` set; exits when all exhausted.
- **Loop detector**: rolling window of last 6 rendered lines; a line repeated ≥3× aborts the
  stream, drops the model, retries next one.
- **OpenRouter specifics**: Bearer key; `HTTP-Referer`/`X-Title` attribution headers;
  `GET /auth/key` for validation + `limit/usage` → 20-dot credit bar at startup and `/credits`;
  reads `usage.total_tokens` from the final SSE chunk; parses `<think>…</think>` in content (not
  the `reasoning` delta field) and renders dimmed.
- **Key handling**: `--auth`/`/auth` prompts, warns if not `sk-or-`, validates, then **writes
  `export OPENROUTER_API_KEY` into ~/.zshrc AND plaintext `~/.orcli-config.json`** (no chmod).
  No OAuth.
- **"Tools"**: post-response regex finds ```bash/sh/zsh/python``` blocks; one `Run? [Y/n]`
  (Enter = run all); `/mode auto` runs silently; cwd persistence via appending
  `pwd > /tmp/orcli-pwd` then `os.chdir`. System prompt tells the model its fenced commands will
  be executed.
- **Sessions**: JSON `{model, messages, saved}` in `~/.orcli-history/<ts>_<slug>.json`; autosave
  after every exchange; `/resume` picker; "Welcome back · last chat: …" greeting; `/retry` pops
  last user+assistant and resends on next model; `/file PATH` (≤100 KB, ext→fence lang);
  keyword-triggered "smart context" appends cwd + dir listing to the *user message*.
- **TUI**: raw ANSI 256-color; 5 theme dicts; regex markdown + regex syntax highlighter; threaded
  "AI is typing" spinner; Messenger-ish "✓✓ seen", timestamps, per-reply token count.
- Config: `~/.orcli-config.json` (theme, mode, api_key).
- Inferred bugs: each autosave creates a **new** timestamped file per exchange (history
  explosion); piped-stdin mode calls `input()` for Run? → EOFError.

## 3. For kolk — incorporate (ranked) / avoid
**Incorporate**
1. **Free-tier fallback chain** as the bottom of the effort dial: filter `/models` by zero pricing
   (or `:free` / `openrouter/free` router), rank by a preference list, auto-hop on 429/503 with
   per-turn `tried`, show ✗ exhausted in the picker.
2. **`/auth/key` check at startup + `/credits`** (limit/usage) and `X-Title`/`HTTP-Referer`
   headers — cheap, and a remote-spend cross-check for the local efficiency dashboard.
3. **Degeneration/loop detector** → abort stream, mark model, retry/escalate tier. Generalize to
   n-gram repetition.
4. **`/retry` = resend on next model / next effort notch**; **`/resume` + "welcome back"
   greeting**; per-reply tokens+timestamp and inline `model · msgs · tok` status (add $ from
   `/models` pricing).
5. Pipe/one-shot ergonomics (`kolk "q"`, `cat f | kolk`) with zero prompts when stdin is not a
   TTY; `/file` with size cap + ext→lang.
6. Single-binary + `curl | sh` installer ethos (Go gives this for free; skip .bat/.command).

**Avoid**
- Executing fenced blocks from chat with one default-yes prompt / `/mode auto`: prompt-injection
  and blast-radius hazard; kolk uses structured tool calls with per-tool permission and allowlists.
- Writing keys into shell rc + 0644 JSON; use env/keychain/0600 XDG config (consider OpenRouter
  OAuth-PKCE — see openrouter.md).
- `<think>`-tag scraping; use OpenRouter's `reasoning` deltas / `reasoning.effort` param (maps to
  the effort dial).
- Conflating "context length" with rate-limit → classify: 429→rotate/backoff, 402→stop,
  context→compact.
- Regex markdown/highlighting/themes → glamour/chroma/lipgloss; `/tmp/orcli-pwd` cwd hack; context
  injected into stored user messages; new file per autosave.

## 4. Similar projects (GitHub API-confirmed stars, 2026-08-21)
- charmbracelet/crush — Go, 27.6k★, Bubble Tea agentic coding TUI, multi-provider incl.
  OpenRouter. https://github.com/charmbracelet/crush
- charmbracelet/mods — Go, 4.5k★, pipe-first "AI on the command line". https://github.com/charmbracelet/mods
- sigoden/aichat — Rust, 10.4k★, REPL/shell-assistant/agents, `.model` switching UX. https://github.com/sigoden/aichat
- simonw/llm — Python, 12.4k★, SQLite logs of every call w/ tokens (dashboard data-model
  reference). https://github.com/simonw/llm
- anomalyco/opencode (ex-sst) — TS, ~200k★, provider-agnostic coding agent TUI. https://github.com/anomalyco/opencode
- morganlinton/Albatross — Rust, 222★, "transparent multi-model routing" local+cloud. https://github.com/morganlinton/Albatross
- lwlee2608/tokentop — Go, 17★, terminal OpenRouter usage dashboard. https://github.com/lwlee2608/tokentop
- jwill9999/openrouter-cli — TS, 5★, `/model` inline search, free-model tip. https://github.com/jwill9999/openrouter-cli

## 5. Sources
- https://github.com/Theanlegendary/orcli (README, `orcli`, `install.sh`, `install.bat`,
  `orcli.bat`, `orcli.command` via `gh api repos/…/contents`, `git/trees`, `commits`, `releases`)
- `gh api search/repositories` ("openrouter cli", "openrouter terminal agent", "openrouter tui")
- https://openrouter.ai/openrouter/free, https://openrouter.ai/collections/free-models,
  https://github.com/alexolinux/openrouter-cli
