# Research: anomalyco/opencode read at source level — what to take

Date: 2026-08-27. Method: `git clone --depth 50 https://github.com/anomalyco/opencode.git`, branch
`dev` at `5f5ea53`, read locally. Nothing was built, installed or executed; every claim below is a
file in that tree, cited as `packages/<pkg>/src/<file>:<line>`. Licence: MIT (`LICENSE`,
`package.json`), so both the designs and the code are legally borrowable; attribution is the polite
move. Feeds PLAN.md items 12, 13, 14, 15, 16, 17, 19, 20, 24, 26, 27, 28, 29, 30, 31, 32.

This supersedes nothing in `ecosystem.md`, which described OpenCode from its published docs. That
entry is still correct; this one is the source-level pass that turns "they have a doom-loop rule"
into "here is the threshold, the file, and the shape of the permission it raises".

## A) What the project is now

- **31 packages, ~3,270 TS/TSX files, zero Go.** The Go TUI documented in older write-ups is gone;
  `packages/tui/src` is TypeScript on SolidJS + OpenTUI. There is no `go.mod` anywhere in the tree.
- **Weight:** `packages/opencode/package.json` declares 99 direct dependencies (21 dev). `bun.lock`
  resolves 3,236 packages. The root `package.json` carries 15 `patchedDependencies`, and the whole
  thing runs on `effect@4.0.0-beta.83` under Bun 1.3.14. Kolkrabbi's gate is two modules
  (`golang.org/x/sys`, `golang.org/x/term`). This is the number to quote when someone asks why we
  are not just contributing to OpenCode.
- **Half the monorepo is a business, not an agent:** `console`, `stats`, `enterprise`, `identity`,
  `function`, `control-plane`, `containers`, `slack`, and a root `sst.config.ts` deploying AWS
  infrastructure. Sessions are shared by syncing them to `opncd.ai`
  (`packages/opencode/src/share/share-next.ts`). Read the repository for decisions; do not read it
  for shape.
- **No sandbox exists.** `grep -rl 'sandbox-exec\|bwrap\|seccomp\|landlock' packages` returns
  nothing. Their entire containment story is the permission ruleset plus, for cloud workspaces,
  running the agent inside a Docker image (`packages/containers/*/Dockerfile`). `--auto` approves
  everything not explicitly denied, and there is no rule a user cannot override. Item 13's hardline
  blocklist is a stronger safety claim than the market leader makes; keep it and say so.
- **Desktop is Electron** (`packages/desktop/electron-builder.config.ts`, `electron.vite.config.ts`),
  shipping `.dmg` / `.exe` / `.deb` / `.rpm` / `.AppImage`. A datapoint for item 19: the best-funded
  competitor did not pick Wails or Tauri.

## B) Item-by-item verdict

| PLAN.md item | What OpenCode has | Verdict for us |
|---|---|---|
| 12 sessions/compaction | tuned compaction with published constants (§D) | take the numbers |
| 13 tools/permissions | full ruleset engine; **no sandbox at all** | take the engine, keep our floor |
| 14 orchestration | real `worktree/index.ts`, per-agent permissions, `task` tool | closes our worktree gap |
| 15 code mode | shadow-git snapshots (§C.3) | closes the "bash isn't checkpointed" gap |
| 16 extensibility | MCP + skills + md commands + typed plugin hooks, all shipped | reference implementation |
| 17 dashboard | `opencode stats` — tokens and cost only, **no quality signal** | our ratings stay unique |
| 19 desktop/iPad | Electron; no mobile app | evidence, not a plan |
| 20 distribution | `install` script, `upgrade`, `uninstall`, brew/scoop/choco/nix/mise/AUR | good template |
| 24 subscriptions | Codex, Copilot, GitLab, Poe, Cloudflare, Azure, DO, xAI, Cerebras, Snowflake — every one a **plugin** behind one `auth` hook | the seam is the lesson |
| 25 managed local models | config-only (Ollama, LM Studio, llama.cpp, Atomic Chat) | nobody manages the runtime; we would be first |
| 26/27 remote & many sessions | `serve` / `web` / `attach`, basic auth, mDNS, web console | shape confirmed (§E) |
| 28 source control | `git` service + `opencode pr` + `opencode github`; GitHub only | our refusal list matches theirs |
| 29 workspace services | **nothing** — no port discovery anywhere | still uncontested |

## C) Take these four

### C.1 Command-prefix arity → honest bash permission patterns (feeds item 31)

`packages/opencode/src/permission/arity.ts` is a 139-entry table mapping a command prefix to the
number of tokens that constitute "the command", flags excluded: `cat`→1, `git`→2, `npm run`→3,
`docker compose`→3, `aws`→3, `git stash`→3. Longest matching prefix wins; unlisted commands fall
back to the first token. `prefix(tokens)` returns the slice.

This is the piece that makes an "always allow" answer safe. Without it, approving `git status` either
whitelists the literal string (useless the moment an argument changes) or the whole `bash` tool
(dangerous). With it, the prompt can offer `git status *` and mean it. The file is candid that the
table was LLM-generated — the generating prompt is left in the source as a comment, which is also the
recipe for regenerating or extending it.

Our item 13 doc specifies allow/ask/deny with last-match-wins but never says how a bash command
becomes a pattern. This is the missing half.

### C.2 The doom-loop guard (feeds item 30)

`packages/opencode/src/session/processor.ts:29` — `DOOM_LOOP_THRESHOLD = 3`. At `:355` the processor
looks at the last three parts of the assistant message; if all three are tool calls of the same tool,
already settled, with `JSON.stringify(input)` identical, it raises a permission request
`{permission: "doom_loop", patterns: [toolName], always: [toolName]}` instead of running the call.

It costs about thirty lines and it catches the failure mode that burns the most money and the most
trust. `doom_loop` defaults to `ask` even in their otherwise permissive default set
(`packages/opencode/src/agent/agent.ts:121`), and `--auto` does not silence it because auto only
upgrades `ask` to `allow` for rules that are not explicitly set — worth re-checking against our own
`full-auto` semantics before copying the default.

### C.3 Shadow-git snapshots with borrowed objects (feeds item 32)

`packages/opencode/src/snapshot/index.ts`:

- `:71` the snapshot store is a **separate git dir** at `<data>/snapshot/<projectID>/<hash(worktree)>`.
- `:75` every command runs as `git --git-dir <store> --work-tree <project> …`. The user's own
  `.git`, index, stash stack and reflog are never touched.
- `:224` the store writes `objects/info/alternates` pointing at the real repository's object
  directory, so a snapshot of a huge checkout reuses existing blobs instead of rehashing the tree.
  The comment names Chromium as the case that forced it.
- `:173` it resolves and reuses the project's `info/exclude` so ignored files stay ignored.
- `:328-333` the store is configured `core.autocrlf=false`, `core.longpaths=true`,
  `core.symlinks=true`, `core.fsmonitor=false`, `feature.manyFiles=true`.

This is strictly better than the "git stash per turn" our README floats as the fix for untracked
`bash` changes: it captures everything the working tree did regardless of which tool did it, it works
on a dirty tree, it cannot corrupt the user's git state, and it degrades to nothing outside a repo.
Our `internal/checkpoint` already snapshots files before `write_file`/`edit_file`; this replaces the
storage layer, not the `/undo` and `/rewind` semantics.

### C.4 The plugin hook interface (feeds items 16 and 24)

`packages/plugin/src/index.ts:222` defines `interface Hooks` — the entire third-party surface in one
typed object. Every hook is `(input, output) => Promise<void>` and mutates `output`; there is no
dynamic loading, no ABI, no registry protocol. The points:

`event`, `config`, `tool` (define new tools), `auth`, `provider`, `chat.message`, `chat.params`,
`chat.headers`, `permission.ask`, `command.execute.before`, `tool.execute.before`,
`tool.execute.after`, `tool.definition`, `shell.env`, `experimental.chat.messages.transform`,
`experimental.chat.system.transform`, `experimental.provider.small_model`,
`experimental.session.compacting`, `experimental.compaction.autocontinue`,
`experimental.text.complete`.

Item 16 was hardened on the same day this read was made, independently and compatibly: it ships
markdown commands first, hooks second with three post-events, and **no `pre-tool` hook**, on the
grounds that a hook which can veto a tool call is a second permission system. OpenCode is the
counter-example that supports the refusal — `permission.ask` is exactly that veto, and it has to be
reconciled against the ruleset in §C.1 every time a rule changes.

Two things still follow. First, `auth` being a hook is why they support ten consumer/subscription
logins without ten special cases in the core — item 24's matrix wants exactly this seam, and that is
recorded against item 24 rather than here. Second, the input/output split is what lets a hook be
advisory (read `input`, adjust `output`) rather than control flow: the shape survives translation to
a shell contract, which is the only form our plugin boundary can take.

## D) Take these numbers

**Compaction** (`packages/opencode/src/session/compaction.ts:26-165`): prune tool *outputs* before
dropping messages; `TOOL_OUTPUT_MAX_CHARS = 2_000`; `PRUNE_MINIMUM = 20_000` tokens before pruning
starts and `PRUNE_PROTECT = 40_000` tokens of recent context never pruned; `skill` output is exempt
(`PRUNE_PROTECTED_TOOLS`); the preserve-recent budget is clamped to 2k–15k tokens; splits happen on a
turn boundary (`turns()` / `splitTurn()`). Item 12's doc picks its own thresholds — these are a second
opinion from a codebase with real users.

**Permission defaults** (`packages/opencode/src/agent/agent.ts:100-135`): `"*": "allow"`,
`doom_loop: "ask"`, `question: "deny"`, `plan_enter`/`plan_exit`: `"deny"`,
`external_directory: {"*": "ask", <skill dirs>: "allow", <tmp>: "allow"}`, and
`read: {"*": "allow", "*.env": "ask", "*.env.*": "ask", "*.env.example": "allow"}`. Note the
inversion: `evaluate()` at `permission/index.ts:28` defaults to **ask** when no rule matches, and the
permissiveness comes entirely from the seeded `"*": "allow"`. Fail-closed engine, fail-open defaults.
We should keep the engine and not the seed.

**Skill discovery** (`packages/opencode/src/skill/index.ts:21-24, 187-207`): reads
`.claude/skills/**/SKILL.md` and `.agents/skills/**/SKILL.md` at both global and project scope, plus
its own `{skill,skills}/**/SKILL.md`. That answers item 16's open "compatibility with Claude Code's
format — import?" question: **read it natively, never import it.** `skill/discovery.ts` adds remote
registries — an `index.json` listing `{name, files, version}`, downloaded to a staging directory and
swapped in with an atomic rename plus rollback.

**Markdown commands** (`packages/opencode/src/command/index.ts`): `$1…$N` and `$ARGUMENTS`
substitution surfaced as `hints`, a per-command `agent` and `model` override, and `subtask: true` to
run the command inside a subagent. Their built-in `review` command uses exactly that.

## E) The remote shape, and the hole it fixes in ours

`opencode serve` and `opencode web` default to `127.0.0.1:4096`, take basic auth from
`OPENCODE_SERVER_PASSWORD` (username `OPENCODE_SERVER_USERNAME`, default `opencode`), require an
explicit repeatable `--cors <origin>` allowlist, and offer opt-in `--mdns` discovery.
`opencode attach <url>` then points the TUI at any running server, local or remote.

Two consequences for items 26 and 27. The bind default plus mandatory-password-for-non-loopback is the
same floor item 26 specifies, arrived at independently. And because their TUI is *only* a client,
"steer from another device" and "many sessions in one view" were never separate features — they fell
out of the architecture. Our advisory-lock discovery decision (I27.1) is cheaper and still sound, but
the lesson is that the client/server seam is what makes both items small.

Also worth stealing when items 18 and 24 next move: `policies` (`docs/policies.mdx`) is a separate
allow/deny list for *resources* rather than tools — today only `provider.use` — and global config
beats project config, so a cloned repository cannot re-enable a provider the user denied globally.

## F) Where we are not behind

- **Ratings.** `opencode stats` reports tokens, cost, tools and models. Nothing in 3,270 files
  captures whether an answer was any good. "Which model earns its cost on your tasks" is still nobody
  else's feature.
- **The effort dial.** Their agents have a model and `maxSteps`. There is no cross-provider ladder
  moving model tier, tool-round limit, shell timeout and task width together.
- **The floor.** See §A. They have no non-bypassable rule and no OS sandbox.
- **No relay.** `/share` uploads the conversation to their servers. Item 26 refuses a relay on
  purpose; that is a product difference, not a missing feature.
- **Port discovery** (item 29). Nobody has built it. Their answer is a cloud container with a preview
  URL, which is a different product.

## G) Pitfalls — what not to copy

- Do not copy the architecture. `Effect` 4.0 beta, `LayerNode`/`Context.Service` ceremony, 15 patched
  dependencies and a Bun runtime are the opposite of a two-module gate. Copy decisions and constants.
- Do not copy the permissive default seed (§D) into a project whose selling point is that `full-auto`
  still refuses things.
- Do not read `control-plane/`, `containers/`, `console/`, `stats/` or `enterprise/` as agent design.
  They are the hosted product.
- The tree moves fast and this is one commit of a `dev` branch. Re-verify a line number before citing
  it in a hardened doc.

## H) Sources

Local clone of `github.com/anomalyco/opencode` at `5f5ea53` (branch `dev`), 2026-08-27:
`packages/opencode/src/{permission/{index,arity}.ts, session/{processor,compaction}.ts,
snapshot/index.ts, skill/{index,discovery}.ts, command/index.ts, plugin/{index,loader}.ts,
worktree/index.ts, git/index.ts, share/{session,share-next}.ts, agent/agent.ts, mcp/index.ts,
cli/cmd/*}`, `packages/plugin/src/index.ts`, `packages/desktop/electron-*.ts`,
`packages/containers/*/Dockerfile`, `install`, `package.json`, `bun.lock`, `LICENSE`, and
`packages/web/src/content/docs/{permissions,policies,share,server,cli,agents,skills,providers,
network}.mdx`.
