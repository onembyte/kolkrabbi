# 6. The modes — chat / code

Status: hardened on 2026-08-23 · supersedes: — · PLAN.md item 6

## Decision (the short version)

**A mode is a record, not a code path, and there are two of them: `chat` reads, `code` writes.**
`engine.Mode` is a plain Go struct — an allow-list of tool *names*, one prompt paragraph, a model
slot, an effort override, a permission floor, a width cap and a presentation triple — resolved once
per turn by two pure functions into a `Resolved` value that the single turn loop reads. Nothing
downstream of `Resolve` ever compares a mode name. `code` is the default in every invocation
(interactive, one-shot and piped alike); `chat` is one keystroke away and is **read-only, local**:
it can read, list and (once item 13 lands) grep and glob, and it can change nothing on your machine
and send nothing off it.

**`agent` does not survive as a mode.** It was never one: `systemPrompt()` has two branches for
three names, agent falls into `default:`, and the mode's whole identity lives in three hardcoded,
mode-blind prompts inside `runOrchestrated`. Orchestration is a *width*, not an intent — the user
cannot state it before stating the problem, and neither can a classifier. It becomes a `delegate`
tool inside `code`, whose per-turn budget comes from item 7's effort dial. `/agent` and
`--mode agent` keep working through v0.3 as a deprecation that prints its own translation, and
`kolk stats` aliases the historical value at read time so no recorded data fractures.

The record supports rows the user never sees (`Visible: false`) — the delegated worker, item 8's
title writer, item 10's saga chapter, item 14's critic — which is what stops those five items from
each inventing another hardcoded prompt. It also supports a third *visible* row: `plan` is reserved
for item 15 and ships the day item 13 gives it search and a read-only shell, as one row of data.

Everything a mode needs is compiled in. `internal/engine` is forbidden by `internal/arch` from
importing `os` or `internal/config`, so "computed, not asked" is a build failure rather than a
promise.

---

## Spec

### 0. ★ North star compliance

#### 0.1 The literal first run

```console
$ curl -fsSL https://kolk.sh/install | sh
kolk 0.1.0 → /usr/local/bin/kolk

$ kolk key sk-or-v1-…4f2a
openrouter  sk-or-v1-…4f2a   verified · $12.47 credits

$ kolk "why is TestStreamTools flaky?"
assistant  → read_file(internal/provider/stream_test.go)
           → bash(go test ./internal/provider -run TestStreamTools -count=20)
The test shares one *bytes.Buffer between the fixture goroutine and the assertion …
  [standard → openrouter/auto · 3.1k tok · $0.0041 · 2.4s]
```

The word "mode" appears nowhere. No flag was typed. `$XDG_CONFIG_HOME/kolk/` contains exactly one
file — the credential — and nothing in this document reads it. Interactively:

```console
$ kolk
kolk · ready · it asks before it changes anything · openrouter/auto · s_20260823-141233-9a2c

› add a --json flag to kolk stats
```

**The banner states capability, not taxonomy, and the prompt is bare in the default mode.** A new
user is never taught a noun they do not need. The moment they leave the default, the prompt says so
(`chat › `) — see §6.

#### 0.2 The one sentence

> **chat reads, code writes.**

That is the whole mode system. `kolk help mode` adds two lines and nothing else:

```
chat  reads, searches and answers · changes nothing on your machine, sends nothing off it
code  the default · reads, edits and runs · asks before it changes anything
      shift+tab toggles · /chat /code · --mode on the command line
```

#### 0.3 Rule by rule

| North star rule | How item 6 complies |
|---|---|
| **1. Zero-config is the product** | `Builtins()` is a Go slice literal. `internal/arch/layers.go` forbids `internal/engine` from importing `os` *or* `internal/config`, so a mode default **cannot** be read from disk even by accident. `TestZeroConfigModes` builds the registry with `KOLK_CONFIG_DIR` pointed at an empty temp dir and asserts both modes resolve runnable. |
| **2. Every default computed, not asked** | Model = `slot.<name>` → `slot.main` → effort tier → session model (verbatim `modelFor`'s existing shape, `agent.go:140-145`, whose own comment says *"everything works zero-config and tiers are a pure optimization"*). Width and rounds = pure switches on effort with a `default:`. Effort, posture and permission floor = inherit. No wizard, no `kolk init`, no first-run write. |
| **3. One install command** | Zero new dependencies. `slices`, `strings`, and packages already imported. |
| **4. One key command** | Modes never touch credentials. `Mode.Check(caps)` reuses item 5 §1.5's *computed* shape: the `/mode` list is derived from the resolved backend, never canned — and it never shrinks (§3.1). |
| **5. Complexity ships off, discoverable later** | Hidden modes: not listed, not cyclable, not valid `--mode` values. Per-mode model slots: absent, fall back to the session model. Per-mode effort: absent, inherits. User-defined modes (item 16): a directory that does not exist. `plan`: one row, not shipped until it can do its job. Auto-suggest: fires only after the model *asks*, never speculatively, never under `-p`. |
| **6. Simple to type beats simple to explain** | `kolk "…"` → zero flags. The second state is **one chord**, not a three-way cycle. Two words, four letters each, both in every English speaker's vocabulary. `--mode` is never required for anything. The default is `code` in **every** invocation — one sentence, against Claude Code's six-row table of preconditions for "which mode a session starts in". |

#### 0.4 Where the North star overruled the PLAN item

1. **PLAN item 6 asks for per-project persisted mode defaults (`.kolk/`).** kolk **reads** a project
   config if one is present and **never creates one**. A directory appearing in the user's repo
   unbidden is a config file wearing a different hat.
2. **PLAN item 6 asks for mode auto-suggest driven by the fast lane.** Refused as a classifier
   (§8), replaced by an affordance that fires on a *completed turn*. A guess the user cannot see is
   a config file you did not let them edit.
3. **PLAN item 6 asks for three modes.** Two, plus a record that makes a third cost one row. The
   argument is §9; the migration for the owner's mental model is §9.3.

---

### 1. The boundary table

Reach is enforced by **omitting the schema**. Never by a prompt instruction, never by a deny rule at
call time. A model cannot call what it cannot see; every soft-enforcement leak in the survey
(Claude Code #38255, 37👍, *"model made file edits despite plan mode being active"*; Cline #9518 and
#4848) came from trusting the prompt. The mode paragraph is belt-and-braces only.

| | **`chat`** ◇ | **`code`** ▸ *(default)* | *`task`* (hidden) | *`title`* (hidden) |
|---|---|---|---|---|
| **the job, in one line** | reads and answers; touches nothing | do the work | one delegated unit of work | name the session |
| **visible** | ✓ `/chat` | ✓ `/code` | ✗ — reached by `delegate` | ✗ |
| **tools today (v0.1)** | `read_file` `list_dir` `needs_write` | `read_file` `list_dir` `bash` `write_file` `edit_file` `delegate` | `read_file` `list_dir` `bash` `write_file` `edit_file` | — |
| **tools after item 13** | + `grep` `glob` | + `grep` `glob` `web_fetch` `web_search` `multi_edit` | + `grep` `glob` | — |
| **network** | **never in v0.x** — see §1.2 | ✓ (item 13) | ✓ (item 13) | — |
| **schema bytes on the wire** | 763 B ≈ 190 tok | **1,899 B ≈ 474 tok** (measured 2026-08-23) + `delegate` | as code, minus `delegate` | 0 |
| **can mutate?** | **no — provably: no mutating schema is ever sent** | yes | yes | no |
| **orchestration** | none | `delegate`, **offered never forced**; per-turn width from the effort dial | none (`delegate` absent ⇒ spawn depth 1, no counter) | none |
| **tool rounds / turn** | 4 (runaway guard) | from effort: 8 / 16 / 30 / 60 | 12 | 1 |
| **default effort** | **inherit the session dial** | **inherit the session dial** | inherit | pinned lowest |
| **model slot** | `main` | `main` | `worker` → `main` | `fast` → `main` |
| **permission floor** | **none — inherits item 13's dial** | **none — inherits item 13's dial** | none | none |
| **may stop and ask?** | ✓ | ✓ | **✗ — auto-deny with a reason** (arch §10) | ✗ |
| **project memory** | ✓ identical | ✓ identical | ✓ identical | ✗ |
| **prompt paragraph** | 2 sentences, compiled in | 3 sentences, compiled in | 3 sentences | 1 sentence |
| **reminder cadence** | every turn (free — §3.4) | every turn | on the sub-turn | — |
| **glyph · label · hue** | `◇` `chat` neutral | `▸` `code` action | `·` quiet | `·` quiet |
| **the guarantee, stated** | **"kolk reads. It changes nothing on your machine and sends nothing off it."** | "kolk asks before it changes anything." | — | — |

#### 1.1 Why `chat` reads, instead of having no tools

This is the single biggest change to the owner's original shape, and it is the one the evidence is
most one-sided about.

- **The market voted read-only.** Goose is the *only* surveyed product with a no-tools mode.
  Roo (`ask`), Kilo (`ask`), OpenCode (`explore`/`plan`), Gemini CLI (`plan`) and Claude Code
  (`plan`) all ship the read-only version. Claude Code's own *"a /chat mode for when you just want
  to talk"* request ([#46634](https://github.com/anthropics/claude-code/issues/46634)) got **2 👍
  and was closed as stale**; *"per-mode models"*
  ([#15721](https://github.com/anthropics/claude-code/issues/15721)) got **67 👍**.
- **A tool-free chat cannot answer the most common question in a terminal.** *"What does
  `internal/engine/agent.go` do?"* In today's chat mode that turn produces no answer at all. The
  probe corpus made this concrete: *every one* of a hand-built classifier's ten `code→chat`
  misroutes was a question **about the repository** (*"why is TestStreamTools flaky?"*, *"where is
  the yolo flag handled?"*). Question form is the strongest signal in the text and in this product
  it is the wrong one.
- **A second mode that is worse than the default at the thing users do most is abandoned after one
  try**, and three modes collapse to one in practice.
- **The token argument does not survive measurement.** The five schemas are **1,899 bytes ≈ 474
  tokens**, and they sit in the *cacheable prefix*: paid once at 1.25×, read at 0.1× thereafter — a
  fraction of a cent per session. **Chat mode does not exist to save tokens.** It exists to make one
  sentence true, and that sentence survives the redefinition intact.

#### 1.2 Why chat has no network, and why that is item 6's call and not item 13's

`web_fetch` and `web_search` are *read* tools by class, and the tempting move is to admit them. They
are refused in `chat` in v0.x, deliberately:

People choose the read-only mode precisely when they are screen-sharing, on a production box, or
inside a client's repository. "Read anything + reach the network" is an exfiltration path, and it is
reachable without the model intending anything: a `read_file` of a config that contains an injected
instruction, followed by a fetch. The guarantee kolk prints must be *one accurate sentence*, and
"changes nothing" is not it. **"It changes nothing on your machine and sends nothing off it"** is,
and it costs two tool names.

When item 13 ships its network toggle, `chat` gains `web_fetch`/`web_search` **behind that toggle**,
as a data edit to one row plus one line in `kolk help mode`. Until then the honest version ships.

#### 1.3 Why `bash` is not in `chat`, and why `plan` is not shipping yet

`bash` is the write vector: `>`, `rm`, `git checkout`, a migration, `npm install`. A read-only mode
containing general `bash` is a guarantee you cannot keep without a sandbox. (OpenCode's own docs
claim its `plan` agent sets bash to `ask`; the source at `packages/opencode/src/agent/agent.ts`
never touches `bash`, so their plan mode can run arbitrary shell. kolk ships the honest version.)

The consequence is that a read-only mode cannot run `git diff`, `go test` or `git log`, and until
item 13 lands `grep`/`glob` it cannot search either. That is exactly why **`plan` — a read-only mode
with a planning prompt and a write exception for `.kolk/plans/*.md` — is a reserved row and not a
shipped one.** Shipping the most attractively-named mode in the product as the least capable one is
how a third mode dies on first contact. `plan` flips `Visible: true` in the release that gives it
search plus item 13's sandboxed read-only shell profile. Item 15 owns its prompt and its plan-file
exception; item 6 owns the row.

`chat` is shipped now anyway, because answering questions from `read_file` + `list_dir` + knowledge
is genuinely useful on day one, which "plan without search" is not.

#### 1.4 Why `code` is a superset and is the default

Asked a pure-knowledge question in `code`, the model simply does not call a tool — it is a better
per-turn classifier than any heuristic, for free (§8). Asked to change something, it asks first
(item 13). So code-by-default is not "dangerous by default": the **trust** dial makes it safe, not
the reach dial. Every surveyed product defaults to its build/code state, and defaulting to a
read-only mode makes kolk answer *"I can't do that"* to the first thing a terminal-agent user types.

---

### 2. The Go structure

#### 2.1 What the prototype actually is, so the size of the change is honest

Three mode names, **two of everything**: two system prompts (`agent.go:158-167`; agent falls into
`default:`), two tool-set deciders (`agent.go:148-153` and `orchestrator.go:174`, which bypasses
`toolsFor` entirely so subagents ignore mode), two dispatch deciders (`agent.go:326-329` and the
nested fallback at `orchestrator.go:51-58`, which pops the user message, saves the truncated
session, and re-enters `runLoop`, which re-appends it). The mode string leaks to 26 sites. Twelve of
them collapse into the record; the rest are text, colour and `printf`s that arch §12 step 7 already
owns.

#### 2.2 File placement (arch §2, §5, §9)

| File | Layer | Contents | Status |
|---|---|---|---|
| `internal/engine/mode.go` | L4 | `Mode` `ToolFilter` `Style` `Hue` `Builtins` `Registry` `Note` | **new** (arch §2 reserves the filename) |
| `internal/engine/prompt.go` | L4 | `Env` `Render` `identity` `reminder` | **new** (arch §2 reserves it) |
| `internal/engine/effort.go` | L4 | `roundsFor` `widthFor` — item 7 owns the values, item 6 the shape | item 7 |
| `internal/engine/engine.go` | L4 | `RunTurn` `Sub` `turn` (**the** loop) `SetMode` `pending` | rewritten |
| `internal/engine/runner.go` | L4 | `Runner` `SubSpec` — the seam for orchestrator/saga | arch §12 step 9 |
| `internal/engine/port.go` | L4 | + `MemorySource`, + `Slots` (8th and 9th ports) | amended |
| `internal/orchestrator/delegate.go` | L4 | the `delegate` tool | **new** |
| `internal/tools/registry.go` | L3 | `Tool` `Set` `Env` `Register`; `Definitions(names)` | amended |
| `internal/tools/needs_write.go` | L3 | the escape-hatch tool, `Terminal: true`, `Effect: none` | **new** |
| `internal/memory/{memory.go,walk.go}` | **L5, new package** | the VCS-root→cwd walk, the budget, the digest | **new** |
| `internal/perm/perm.go` | L3 | `Ruleset` — item 13 owns the type; item 6 only carries it | item 13 |
| `internal/session/session.go` | L5 | + `Mode` `Effort` `Memory`; `Load` demotes a v0 system head | amended |
| `internal/cli/{flags,slash,repl}.go` | L6 | `--mode` validation, `/mode` + generated twins, the prompt | amended |
| `internal/arch/layers.go` | — | + `internal/engine` may not import `os` or `internal/config` | amended |

**Three amendments to `02-architecture.md`, recorded rather than assumed:**

1. §2's L5 adapter list gains `internal/memory`. It is not folded into `internal/config`: config is
   *settings*, memory is *context*, and §9 bans a package named for a bucket rather than a role.
2. §2's `internal/orchestrator` line loses `plan.go` and `synth.go` and gains `delegate.go`. There
   is no planner pre-pass and no synthesis call any more — the plan is the tool call's arguments and
   the synthesis is the model's own next message.
3. §9's config-key row: **`slot.title.model` → `slot.fast.model`.** A slot is named for the function
   it fills, not for the one caller that happens to use it first; item 8's fast lane and item 6's
   `title` mode are the same slot.

**One amendment to `03-provider-layer.md`:** `provider.Capabilities` gains
`SystemInTail Tri` — some model families accept an appended `{"role":"system"}` message
mid-conversation *without invalidating the cached prefix*, which is exactly the primitive a mode
switch wants; others do not, and the universal fallback is a synthetic user turn. Additive within
`spec/VERSION == "0"`. `provider.Message` gains `Synthetic bool \`json:"synthetic,omitempty"\`` so a
rendered marker is distinguishable from a real user turn on resume.

#### 2.3 `internal/engine/mode.go` — the whole mode system

```go
package engine

// Mode is the complete policy for one kind of turn: what it may reach, which
// model fills it, how hard it thinks, whether it may ask, and what it looks
// like. Every branch on a mode string in the prototype is a FIELD here, which
// is what lets the engine have exactly one turn loop.
//
// This file imports neither "os" nor internal/config, and internal/arch fails
// CI on either — a Mode is COMPUTED, never read from disk. Config keys and
// (item 16) markdown files override individual FIELDS of an already-complete
// value; they never supply one.
//
// If you find yourself writing `if m.Name == …` outside this file, the field
// you want is missing. arch_test asserts that the identifiers ModeChat and
// ModeCode appear in no non-test file outside mode.go and internal/cli.
type Mode struct {
	// ── identity
	Name    string // stable id, lowercase, <=6 letters: "chat" "code" "task"
	Summary string // <=64 chars, one line, no trailing period. /mode, help, hello.

	// Visible modes appear in /mode, in the shift+tab toggle, get a generated
	// one-word slash twin, and are the only legal --mode values. Hidden modes
	// are reachable only by delegation: the worker (this item), the title
	// writer (item 8), the saga chapter (item 10), the critic (item 14).
	Visible bool
	Default bool // exactly one visible mode sets this

	// ── reach: THE ONLY AXIS ITEM 6 OWNS, and the only enforcement mechanism.
	// Tool NAMES, not schemas: names keep Mode free of provider types, let L6
	// inject a tool the engine may not import (delegate lives in
	// internal/orchestrator), survive a tool that does not exist yet, and are
	// meaningful on a backend that takes names instead of schemas (item 4).
	Tools ToolFilter

	// ── behaviour
	Prompt string // ONE paragraph, rendered into the message TAIL every turn.
	                // May contain {width} and {rounds}. NEVER cwd, OS, file
	                // bytes, or slash-command syntax: agent.go:160 hardcodes
	                // "/mode code" inside the engine today, and the engine must
	                // not know that a CLI exists.
	Slot   string         // "" == "main". Which model slot fills this mode.
	Effort provider.Effort // "" == inherit the session dial.
	Rounds int            // 0 == derive from effort.
	Width  int            // 0 == derive from effort. Per-TURN delegation budget.

	// Perm is a FLOOR this mode adds, never a ceiling. A mode may not make a
	// user's item-13 ask/deny rule unreachable, and the hardline blocklist
	// survives everything. EVERY BUILT-IN LEAVES THIS ZERO: trust is item 13's
	// dial and item 6 does not write to it. The field exists for item 10's
	// unattended saga chapter and for user-defined modes.
	Perm perm.Ruleset

	Interactive bool // may a turn in this mode stop and ask the user?
	Memory      bool // does the project-memory digest reach this mode?

	// ── presentation for item 11. Semantic only: NO ANSI leaves L4.
	Style Style
}

// ToolFilter is a reach declaration. One trailing "*" is the only glob, which
// is enough for item 16's MCP namespaces ("mcp/github/*") and cheap enough to
// stay a pure function with no dependency.
//
// It is an ALLOW-LIST OF NAMES and not a totally-ordered "reach level" or a
// tool-class enum, because both of those fail the two customization requests
// that will actually arrive: "chat with web search but no file access" and
// "code without bash". Under a scalar, both fall through to a call-time deny
// rule — i.e. the schema is sent and then refused, which is the exact soft
// enforcement this design exists to avoid, and on an item-4 vendor backend
// (where --tools is kolk's ONLY lever) it is not expressible at all.
type ToolFilter struct{ Allow []string }

func (f ToolFilter) Match(name string) bool
func (f ToolFilter) Mutating(s tools.Set) bool // drives the read-only badge

type Style struct {
	Glyph string // exactly one rune, display width 1
	Hue   Hue    // SEMANTIC. Never an escape code, never a hex value.
}

type Hue uint8

const (
	HueNeutral Hue = iota // chat
	HueAction             // code — the mode that changes things
	HueThinking           // reserved: plan
	HueCaution            // reserved: a mode that is dangerous by design
	HueQuiet              // hidden modes
)

const (
	ModeChat = "chat"
	ModeCode = "code"
	ModeTask = "task"  // hidden
	ModeTitle = "title" // hidden
)
```

```go
// Builtins is the entire mode system. No I/O, no init(), no config: a Registry
// built from this alone is fully functional, which is what `kolk` does on a
// machine with an empty config directory.
//
// Names item 13 has not shipped yet (grep, glob, web_*) are listed anyway and
// resolve to nothing — a mode degrades when a tool is missing, it never fails.
func Builtins() []Mode {
	read := []string{"read_file", "list_dir", "grep", "glob"}
	return []Mode{{
		Name:    ModeChat,
		Summary: "reads, searches and answers — changes nothing, sends nothing",
		Visible: true,
		Tools:   ToolFilter{Allow: append(read, "needs_write")},
		Prompt:  promptChat,
		Rounds:  4, // a runaway guard: read→grep→read is a legitimate chain
		Interactive: true, Memory: true,
		Style: Style{Glyph: "◇", Hue: HueNeutral},
	}, {
		Name:    ModeCode,
		Summary: "reads, edits and runs — asks before it changes anything",
		Visible: true, Default: true,
		Tools:   ToolFilter{Allow: append(read, "write_file", "edit_file", "bash",
			"web_fetch", "web_search", "delegate")},
		Prompt:  promptCode,
		Interactive: true, Memory: true,
		Style: Style{Glyph: "▸", Hue: HueAction},
	}, {
		Name:    ModeTask,
		Summary: "one delegated unit of work",
		Tools:   ToolFilter{Allow: append(read, "write_file", "edit_file", "bash")},
		Prompt:  promptTask,
		Slot:    "worker",
		Rounds:  12, // today's maxSubagentRounds
		Interactive: false, // arch §10: auto-deny inside subagents, never prompt
		Memory:  true,
		Style:   Style{Glyph: "·", Hue: HueQuiet},
	}, {
		Name:    ModeTitle,
		Summary: "session title",
		Slot:    "fast",
		Effort:  provider.EffortLow,
		Rounds:  1,
		Style:   Style{Glyph: "·", Hue: HueQuiet},
	}}
}
```

**`delegate` is in `code`'s list unconditionally.** Tool-set membership is a function of the mode
and **only** the mode. It may never depend on the effort dial: items 7, 10 and 15 all require
`/effort` to change live and mid-task, and a schema-list change is both a prompt-cache write and a
silent capability grant the user did not ask for. Effort sets the *width budget* and the *round
budget*, which live in the tail and in a counter — never in the tool array.

**No mode ever forces a tool call.** There is no `ToolChoice` on `Mode`. A forced first-round
`delegate` is a bill the user did not agree to, it lands hardest on the fresh-install free model
(the worst model to be decomposing with), and it rests on the least uniformly supported parameter in
the provider surface. The model decides, per turn, with the conversation in front of it.

#### 2.4 The registry

```go
// Registry is the resolved mode table for one run. internal/cli builds it; the
// engine only reads it. Sources apply in order, later wins, and a later source
// overrides individual FIELDS of an earlier one rather than replacing a record:
//
//	1. Builtins()                        compiled in — complete on its own
//	2. config keys  mode.<name>.<field>  targeted overrides   (item 18)
//	3. $XDG_CONFIG_HOME/kolk/modes/*.md  user modes           (item 16, v0.4)
//	4. ./.kolk/modes/*.md                project modes        (item 16, v0.4)
//	5. --mode / /mode / shift+tab        this run only
type Registry struct{ /* byName map[string]Mode; order []string */ }

func NewRegistry(layers ...[]Mode) (*Registry, error)

func (r *Registry) Get(name string) (Mode, bool) // ALL modes, hidden included
func (r *Registry) Visible() []Mode              // /mode list, in table order
func (r *Registry) Default() Mode                // the single Default:true row
func (r *Registry) Names() []string              // --mode validation: VISIBLE ONLY
func (r *Registry) Toggle(cur string) Mode       // shift+tab (§3.2)

// Check reports why this mode cannot run on a backend, or nil. The one place
// that knows what a mode means when the vendor owns tools and history (§7).
func (m Mode) Check(c provider.Capabilities) error
```

`NewRegistry` returns an error, at load, for: a **visible** mode whose name collides with a reserved
top-level verb (arch §9's list — the check does **not** apply to hidden modes, which is what lets
item 10 have a `chapter` mode while `saga` is a reserved verb); a duplicate name; a duplicate label;
a `Default: true` on more than one row; and a mode with `Interactive: false` and a mutating tool set
but no `Perm` floor (an unattended mode that can write must declare its allow-list).

**`--mode` and the generated slash twins validate against `Visible()`, never `Names()`.** Otherwise
`kolk --mode title` drops the user into the title writer — the same class of bug as today's
unvalidated `--mode banana`, which this item exists to fix.

#### 2.5 Two pure functions, and the reason there are two

```go
// Env is the frozen, INJECTED world a session runs in. The engine discovers
// none of it: internal/cli captures it once at session start and hands it over.
// That is what makes Render pure, and it is what keeps the rendered system
// segment byte-stable for the life of a session.
type Env struct {
	OS  string // runtime.GOOS
	Cwd string // captured at session start, NEVER re-read
}

// ResolveModel is pure: which model fills this mode's slot.
//
//	slot.<name>.model  →  slot.main.model  →  effort tier  →  session model
//
// An explicit /model or -m PIN replaces the session model and clears every
// COMPUTED slot for the session; user-CONFIGURED slots still apply, because the
// user wrote them. Nothing computed, learned or bootstrapped ever overrides a
// pin — PLAN item 8's "never silently change a pinned model", inherited, not
// renegotiated.
func ResolveModel(m Mode, s Slots, e provider.Effort) string

// Resolve is pure: no clock, no file, no network, no writer, no goroutine, and
// no hidden read of session state. Its signature is its whole world, which is
// why the entire mode system is table-testable with struct literals — no HTTP
// server, no temp dir, no engine.
//
// Capabilities arrive as a VALUE and are fetched by the caller AFTER
// ResolveModel, because capabilities are per-model (item 3 §1.4): a mode that
// names a non-default slot must be projected against ITS model's capabilities,
// not the session model's.
func Resolve(m Mode, in Inputs) Resolved

type Inputs struct {
	Model   string
	Effort  provider.Effort  // the session dial (item 7)
	Perm    perm.Ruleset     // the session's live posture (item 13, /yolo)
	Caps    provider.Capabilities
	Tools   tools.Set
	Env     Env
}

type Resolved struct {
	Mode        string
	Model       string
	Effort      provider.Effort
	System      string           // identity(Env) — MODE-INVARIANT, MEMORY-FREE
	Reminder    string           // the mode paragraph, for the message tail
	Tools       []provider.Tool
	ToolNames   []string         // what a vendor backend gets instead (§7)
	Rounds      int
	Width       int              // the per-turn delegation budget
	Perm        perm.Ruleset
	Interactive bool
	Executor    Executor         // ExecLocal | ExecVendor
	Notes       []Note           // one user-facing line each, printed ONCE
}

type Executor uint8
const (
	ExecLocal  Executor = iota // kolk runs the tools
	ExecVendor                 // the backend runs its own (item 4)
)

type Note struct {
	Code string // "tools.dropped" | "delegate.off" | "overlay.tools" | …
	Text string
}
```

`Resolve`'s six steps, each a pure expression:

1. `Effort = orDefault(m.Effort, in.Effort)`; `Rounds = orDefault(m.Rounds, roundsFor(Effort))`;
   `Width = orDefault(m.Width, widthFor(Effort))`.
2. `System = identity(in.Env)` — *"You are Kolkrabbi, a terminal agent running on {OS} in {Cwd}."*
   and nothing else. No mode text, no memory bytes, no date.
3. `Reminder = expand(m.Prompt, {width, rounds})`.
4. `ToolNames = in.Tools.Filter(m.Tools)`; `Tools = in.Tools.Definitions(ToolNames)`. A named tool
   the set does not know is dropped into `Notes`, never a panic.
5. Capability projection: `Caps.Tools == No` → drop all tools + Note;
   `!Caps.AcceptsToolSchemas` → keep `ToolNames`, drop `Tools` + Note;
   `Caps.ExecutesOwnTools` → drop `delegate` + Note, `Executor = ExecVendor`.
6. `Perm = in.Perm.With(m.Perm)` — floor union, deny always wins, hardline survives.

#### 2.6 `RunTurn` stops branching

```go
// RunTurn runs one user turn under the session's active mode. There is exactly
// ONE branch below the resolve, and it is NOT on a mode: item 3 §1.5 requires
// the engine to read Capabilities.ExecutesOwnTools to decide whether it runs
// the tool loop or observes the backend's ("The engine reads exactly this one
// flag … and it MUST NOT branch on len(msg.ToolCalls) != 0 as agent.go:runLoop
// does today"). The mode never reaches this far: it was resolved into r before
// the first byte.
func (e *Engine) RunTurn(ctx context.Context, in string) error {
	if e.pending != nil { // a switch requested mid-turn lands here (§3.3)
		e.applyMode(*e.pending)
		e.pending = nil
	}
	t := e.begin(in) // turn id · Ckpt.BeginTurn · SetTitleFromInput — already
	                 // shared today at agent.go:320-324, outside the old branch
	defer e.finish(t)

	model := ResolveModel(e.mode, e.slots, e.effort)
	r := Resolve(e.mode, Inputs{
		Model: model, Effort: e.effort, Perm: e.perm,
		Caps: e.provider.Capabilities(ctx, model), // MUST NOT block (item 3 §1.1)
		Tools: e.tools, Env: e.env,
	})
	e.emit(TurnStarted{Turn: t, Mode: r.Mode, Model: r.Model, Width: r.Width})
	e.noteOnce(r.Notes)

	_, err := e.turn(ctx, t, r, e.sess.Messages, in)
	return err
}

// Sub implements Runner: one isolated turn under another mode. Its messages
// never touch the parent session; only the returned summary does.
//
// The recursion guard reads the CALLER's mode, not the session's: a hidden
// item-14 mode running as a sub-run must not pass the check because the session
// happens to be `code`.
func (e *Engine) Sub(ctx context.Context, caller Mode, spec SubSpec) (string, error)

// turn is THE loop. history == nil means an isolated sub-turn. It reads r and
// the session; it never reads e.mode, e.effort or e.perm — a turn runs to
// completion under the policy it was resolved with.
func (e *Engine) turn(ctx context.Context, t *Turn, r Resolved,
	history []provider.Message, in string) (string, error) {

	msgs := Render(r, e.mem, history, in) // prompt.go, §3.4 — pure
	budget := r.Width                     // ★ per-TURN, shared by every delegate call

	for round := 0; round < r.Rounds; round++ {
		msg, meta, err := e.stream(ctx, r, msgs)
		if err != nil {
			return "", err
		}
		e.record(r.Mode, meta, len(msg.ToolCalls))
		msgs, history = e.commit(t, r, history, msgs, msg)

		if r.Executor == ExecVendor {
			return msg.Content, nil // the vendor already ran everything
		}
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}
		for _, tc := range msg.ToolCalls {
			out, terminal := e.exec(ctx, t, r, tc, &budget)
			msgs = append(msgs, provider.Message{
				Role: "tool", ToolCallID: tc.ID, Content: out})
			if terminal {
				return "", nil // needs_write ended the turn (§8)
			}
		}
	}
	return "", fmt.Errorf("%s: exceeded %d tool rounds without finishing", r.Mode, r.Rounds)
}
```

Deleted by this: `runOrchestrated`, `plan`, `parseTaskList`, the synthesis call, `toolsFor`,
`systemPrompt`, `SetMode`'s prompt rewrite, `New`'s `Messages[0]` write, the direct
`tools.Definitions()` call at `orchestrator.go:174`, and the pop-then-re-append hack at
`orchestrator.go:51-58`.

#### 2.7 No L4 type ever appears in an L3 signature

`internal/engine` imports `internal/tools` today (`agent.go:24`). The reverse edge would be a real
Go import cycle *and* an `arch_test` failure (`mayImport[L3Domain] = {L0,L1,L2}`). So the tool port
is phrased entirely in L3 types, and `*tools.Registry` satisfies it with no adapter:

```go
// internal/tools (L3) — the execution context, widened from today's two
// callback parameters into one struct so item 13 can add fields without
// touching a signature.
type Env struct {
	Confirm  Confirm
	PreWrite PreWrite

	// Sub runs one isolated sub-turn under a named mode and returns its
	// summary. Nil when the caller may not delegate. Bound PER TURN by the
	// engine, so a Tool value holds no engine and one immutable Set is
	// shareable across every session in kolkd.
	Sub func(ctx context.Context, mode, task string) (string, error)

	// Width is the REMAINING delegation budget for this turn, shared by every
	// tool call in it. A per-call clamp is defeated by two delegate calls in
	// one assistant message; arch §10 already runs tool calls in a pool of 4,
	// so the two pools would nest. Prose in a reminder is not a clamp.
	Width *int
}

type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
	Terminal    bool // executing it ENDS the turn; its result is rendered, not fed back
	Run         func(ctx context.Context, args string, env Env) (string, error)
}

type Set []Tool

func (s Set) Filter(f func(string) bool) []string
func (s Set) Definitions(names []string) []provider.Tool
```

```go
// internal/engine/port.go — the tool port, in L3 types only.
type ToolSet interface {
	Filter(f func(name string) bool) []string
	Definitions(names []string) []provider.Tool
	Run(ctx context.Context, call provider.ToolCall, env tools.Env) (string, bool, error)
}
```

#### 2.8 `delegate` — orchestration as a tool

```go
// internal/orchestrator/delegate.go (L4)
//
// Tool returns the `delegate` tool: {tasks: []string}. The model decides when
// work is parallel, inside its own turn, with the real conversation in front of
// it. There is no planner pre-pass — the prototype paid a planner round-trip on
// EVERY agent-mode turn, gave the planner none of the conversation
// (orchestrator.go:116-119) and none of the project memory (verified: all four
// agent-mode calls report sysHasMemory=false), then silently became code mode
// whenever the planner returned one task.
//
// Prior art is unanimous: Amp, Claude Code, Codex and Gemini all expose fan-out
// as a tool, and Kilo DEPRECATED its Orchestrator mode with the exact reasoning
// that applies here — "Will be removed; agents with full tool access now
// support subagents natively."
//
// Run: clamps len(tasks) against *env.Width, decrementing it, and refuses past
// zero with a returned string (never an error — a budget is not a failure).
// Each task becomes env.Sub(ctx, "task", brief). Returns SUMMARIES ONLY, capped
// at 2 KiB per task (Hermes's summary-only return); the full sub-run transcripts
// go to the bus, never into the main history. Sequential in v0.x; item 14 makes
// them concurrent without changing this signature.
func Tool() tools.Tool
```

Four things this buys before item 14 exists:

1. **The cache stops being structurally hostile.** The prototype issued **N+2 distinct prefixes per
   turn**: a 47-character planner prompt (far below every model's 512–4096-token cacheable minimum,
   so never cacheable), one briefing per subagent embedding `subagent %d of %d` plus the request
   plus all prior results (**unique on every call, cross-turn reuse impossible**), and a synthesis
   prompt also far too small. Now the delegation is a tool call inside the main conversation, riding
   the session's warm prefix, and every sub-run shares a byte-identical preamble.
2. **Project memory reaches the workers**, because a sub-run *is a turn in a mode* and `task` sets
   `Memory: true`.
3. **Recursion is prevented by data.** `task`'s tool list does not contain `delegate`. Spawn depth
   is 1 with no counter — Hermes needs `max_spawn_depth: 1` for this; kolk gets it from the table.
4. **Delegated work becomes auditable.** The `delegate` call and its summaries are ordinary
   messages, so `/rewind`, resume, `/session` and `kolk export` can all explain where an answer came
   from. The prototype discarded the planner output and every subagent conversation into a local
   slice.

**The tradeoff, stated:** each fan-out now costs two messages in the main history (the call and its
summaries) where the prototype cost zero. Capped at 2 KiB per task, this is 2–4k tokens for a
six-task delegation. It is the correct trade — today's agent-mode transcript on disk literally
cannot explain itself — but the main context does grow faster than before.

#### 2.9 What `arch_test.go` gains

Four rules, all data-file-driven, no `//arch:allow` escape hatch:

1. **`internal/engine` may import neither `os` nor `internal/config`.** Package-scoped, not
   file-scoped: a file-scoped ban just moves the `os.Getwd()` call to the file next door. This is
   the mechanism that makes North-star rule 1 a build failure. It lands with a `knownViolations`
   entry naming the step that retires it (arch §5.0's shrink-only ratchet).
2. `ModeChat`/`ModeCode` appear in no non-test file outside `internal/engine/mode.go` and
   `internal/cli`.
3. No ANSI escape literal (`\033[`) anywhere under `internal/engine` or `internal/orchestrator`.
   Today `agent.go:27-33` owns five colour constants and `footer()`, `runLoop` and `runOrchestrated`
   print them — the direct blocker for arch §7.
4. Every `tools.Tool` literal sets `Name`, `Description` and `Parameters` (a zero-value tool is a
   silent no-op in the registry).

---

### 3. Every switching path

#### 3.1 The paths

| Path | Form | Behaviour |
|---|---|---|
| **flag** | `kolk --mode chat "…"` | Validated against `Registry.Visible()` **at parse time**. Unknown → **exit 2**. Today `flags.go` copies the raw string into `options.mode` and `New` only substitutes on empty, so `--mode banana` runs *code* with five tools and says nothing. `-e/--effort` has the identical hole and is fixed in the same pass. |
| **env** | `KOLK_MODE=chat` | arch §9's curated env list. Same door. |
| **slash, status** | `/mode` | prints the computed list; changes nothing |
| **slash, set** | `/mode chat` | switches |
| **slash, twin** | `/chat` `/code` | **generated from `Registry.Visible()`**, never hand-written — so a user-defined visible mode (item 16) gets its twin for free and the vocabulary cannot drift. Goose has a filed bug ([block/goose#4097](https://github.com/block/goose/issues/4097)) for exactly this drift: its `/mode` help text and error message are missing one of its own four values. |
| **slash, one turn** | `/chat <text>` | runs **one** turn in that mode, then restores the previous one. Claude Code's precedent (*"prefixing a single prompt with `/plan`"*). Costs one line in `slash.go`. |
| **keybinding** | **shift+tab** | toggles `code` ↔ **the last non-default mode used** (§3.2) |
| **top-level verb** | `kolk mode chat` | item 9's parity rule, by construction — `slash.go` dispatches into the same handler as `cmd_mode.go` |
| **deprecated** | `/agent`, `--mode agent` | translates to `code` + `deep` effort and prints why. Removed at v0.4 (§9.3). |
| **model-initiated** | — | **never.** The model may *ask* (§8); it may never switch. [cline#10497](https://github.com/cline/cline/issues/10497) is the live bug report: *"the model can autonomously choose to switch to act mode… I believe the switch should always be something the user does."* |

**The `/mode` list is computed but never filtered.**

```console
$ /mode
  ◇ chat   reads, searches and answers — changes nothing, sends nothing
▸ ▸ code   reads, edits and runs — asks before it changes anything

  shift+tab toggles · /chat /code · --mode on the command line
```

On a backend that cannot run a mode, the row stays and carries its reason:

```console
  ◇ chat   reads and answers — unavailable on ollama/llama3: no tool support
```

A vocabulary that shrinks with the backend breaks every doc, tutorial, screencast and habit, and a
missing row explains nothing about why it is missing. One predicate (`Mode.Check`), one place, never
a shorter list. `--mode <it>` still exits 2 with the same sentence, and the shift+tab toggle skips
a mode `Check` rejects rather than erroring per press.

```console
$ kolk --mode banana "fix the build"
kolk: unknown mode "banana" (chat|code)
$ echo $?
2
```

#### 3.2 shift+tab is reach, and only reach

**One key, one dial.** [claude-code#5466](https://github.com/anthropics/claude-code/issues/5466)
(45👍) — *"Plan Mode can't be used with Bypass Permissions… I use Bypass Permissions all the time
during Plan Mode so Claude Code can run commands and gather information"* — is the proof of what
happens when reach and trust share a control: "read-heavy but don't nag me" becomes unrepresentable.
In kolk it is `chat` + `/yolo`, and it means exactly what it says.

So shift+tab is mode. Trust keeps `/yolo` (item 13 owns the key if it grows a ladder; **Ctrl+P is
reserved here for it** and shift+tab never acquires a second meaning).

**The toggle targets the last non-default mode used, not "the next row".** With two modes this is a
plain toggle; with three it stays a toggle instead of becoming a cycle that is overshot and returned
to. That decouples the keybinding from the table length, so adding `plan` later is a data change and
not a re-litigation of the ergonomics claim.

#### 3.3 Mid-session semantics

| Thing | `code → chat` | `chat → code` |
|---|---|---|
| **conversation history** | **untouched. Never truncated, rewritten, replayed or re-labelled.** A mode switch is not a session boundary. | same |
| **assistant messages carrying `write_file` calls** | **left in place.** Providers validate `tool_calls`/`tool_result` *pairing*, not membership in the current `tools` array, and `repairDanglingToolCalls` (`agent.go:186-213`, kept verbatim) already guarantees pairing. Asserted by a new e2e (§11), not assumed; if a backend ever rejects it the fallback is to render past tool blocks **as text**, never to strip them — stripping rewrites a persisted transcript. | same |
| **system segment** | **byte-identical.** It holds identity + OS + cwd and nothing else. | same |
| **project memory** | **byte-identical.** Frozen at session start, rendered at `messages[0]`. | same |
| **the mode paragraph** | changes on the next turn — it is re-rendered into the tail every turn from the *current* mode, so exactly one instruction is live at a time and no contradictory standing instructions accumulate in history. | same |
| **tool block** | shrinks → one cache write (§3.4) | grows → one cache write; the pre-switch prefix stays warm for the return leg |
| **model / effort** | **unchanged.** A mode does not touch either unless it declares a slot, and neither built-in does. | same |
| **trust (`/yolo`)** | **does not reset.** Separate dial. | same |
| **checkpoints** | `Ckpt` untouched; `/changes` and `/rewind` still see the code-mode turns | same |
| **persistence** | `session.Mode` is written on switch and on save; resume restores it | same |
| **protocol** | `session.updated{mode:{…}}` | same |

**Resume is fixed.** Verified against HEAD: resuming a session whose `Messages[0]` was the chat
prompt yields `ag.Mode == "code"`, rewrites `Messages[0]`, and hands the model five tools it did not
have — with no message to the user. After the format cut, `session.Mode` round-trips and a v0 file
with no `mode` field resolves to the computed default.

**An in-flight turn: queued, and visibly queued.**

```
code ▸ → chat ◇ ›            ← the prompt changes the instant the key is pressed
```

- **Queued, not refused.** Refusing makes the product's only keybinding look broken at exactly the
  moment the user most wants out. The prompt changing is the feedback; the switch applies at the
  turn boundary.
- **Queued, not applied now.** Applying mid-turn changes the schema list *between rounds of one tool
  loop*, producing an assistant message whose `tool_calls` reference schemas that no longer exist —
  a guaranteed 400 on the next round, or a synthesised denial, which is the leak class this design
  exists to avoid, self-inflicted.
- **Ctrl+C applies it now.** Cancel is already the verb for "stop now" (`repl.go` per-turn signal
  context), so the escape exists and costs one clause of help text: `code ▸ → chat ◇ (next turn ·
  ctrl+c to switch now)`.
- **Carried as protocol state**, not as a dim TTY line: `session.updated{mode:{pending:"chat"}}`,
  so a headless client can render it.

#### 3.4 The prompt cache — the structural change, and the price

**The system prompt stops being mutable state in `Sess.Messages[0]`.** There are exactly two writers
today — `agent.go:102-107` (`New`, on **every** process start including `-r`) and `agent.go:120`
(`SetMode`) — and both overwrite index 0 unconditionally. `03-provider-layer.md` §6.4 already names
those two lines as the blocker for the whole caching design. They are deleted.

The request is rendered at build time, in four segments. Wire order is `tools` → `system` →
`messages` on every provider, and prefix caching invalidates everything after the first changed byte.

| # | Segment | Size | Changes when | Cost when it changes |
|---|---|---|---|---|
| **A** | tool schemas | 763 B chat / 1,899 B + `delegate` code | **the mode's reach changes** | full prefix rewrite |
| **B** | `system` — identity, OS, cwd | ~420 B ≈ 105 tok | never within a session | — |
| **C** | `messages[0]` (`user`, synthetic) — the memory digest | 0–16 KiB | `/memory reload`, or a new session | full prefix rewrite |
| **D** | the conversation | grows | — | — |
| **E** | the mode paragraph, appended to the tail | ~40–120 tok | every turn, by design | **free** — it lands after the last cache breakpoint, and turn N+1's prefix is a prefix of turn N's cached one |

**B and C are mode-invariant, and that is the whole design.** OpenRouter's default sticky-routing
key is `hash(first system message + first non-system message)` = **B + C**. Segment A is not in it.
So a mode switch **no longer moves the sticky key**, which means the *implicit*-caching families —
OpenAI ≥1024 tok, Gemini 2.5+, DeepSeek, Groq, where kolk sends no cache directive at all — keep
their cache across a switch for free. Today a `/mode` changes the key, lands the request on a
different provider endpoint, and guarantees a cold cache on backends kolk never asked to cache.
`Request.SessionID` (item 3 §1.2) fixes the other half and **does not change on a mode switch**.

**The arithmetic.** Sonnet-class published list rates ($3.00/MTok input, $0.30 cache read,
$3.75 cache write), a 40,000-token prefix at turn ~10, 30 calls in the session. *Stated as
arithmetic on published rates, not as a measurement.*

| | steady state | one `chat`↔`code` switch | 30-call session, input |
|---|---|---|---|
| **today** (`Messages[0]` rewritten *and* the sticky key moves) | $0.012/call warm | write $0.150 **plus** a new endpoint → likely cold at $0.120/call for the rest | ≈ **$2.30** |
| **this design** (A changes, B+C stable) | $0.012/call warm | write $0.150, forfeit one $0.012 read → **+$0.138 once**, then warm again | ≈ **$0.15** |
| **modes sharing a tool set** (item 16's user modes differing only by prompt) | — | segment A identical → **$0.00** | — |

Two more facts worth stating plainly:

- **On a fresh session a switch is free.** Minimum cacheable prefixes are 512–4096 tokens depending
  on the model; kolk's own prefix is ~600 tokens. The cache only begins to matter once the
  conversation itself grows past the minimum, and then the whole conversation sits behind the
  breakpoint.
- **This cost is unavoidable and the design accepts it.** The only way to make a switch free is to
  send schemas the mode must not use, i.e. soft enforcement. **Hard enforcement is worth $0.14.**

**Five landmines this removes**, all live today: `New()` rewriting `Messages[0]` on every process
start including `-r`; `SetMode` rewriting it; `systemPrompt()` re-reading `KOLKRABBI.md`/`AGENTS.md`
on every call; `os.Getwd()` per construction; and a resumed chat session silently getting the code
prompt *and* five tools. After this item, **`/mode` is the only invalidator.**

#### 3.5 Transcripts

```console
$ kolk
kolk · ready · it asks before it changes anything · openrouter/auto · s_20260823-141233-9a2c
memory  AGENTS.md (2.1 KB) · internal/engine/AGENTS.md (0.6 KB)   2.7 KB

› what does internal/engine/agent.go do?
assistant  → read_file(internal/engine/agent.go)
It wires the provider client, tools, session persistence, checkpoints and stats …
  [standard → x-ai/grok-4 · 3.1k tok · $0.0041 · 1.2s]

› ⇧⇥
chat ◇ · reads only · standard → x-ai/grok-4

chat ◇ › mutex or channel for the bus fan-out?
assistant  A bounded channel per subscriber, with a non-blocking send …
  [standard → x-ai/grok-4 · 0.9k tok · $0.0007 · 0.8s]
```

The retired verb, printed in full **once per session**, then a one-line echo:

```console
› /agent
agent mode was retired. it is code mode at deep effort:

  orchestration is a tool now — kolk splits a task into subagents when the task
  is wide enough, instead of you deciding before you have stated the problem.
  one dial:  /effort deep

mode: code · effort: deep → anthropic/claude-sonnet-5
```

Every switch echoes `mode · effort → model` on one line, because a mode may carry a slot and
therefore change the model. **A model change is always stated, never inferred.**

---

### 4. Per-mode computed defaults, and the override story

#### 4.1 The template is already in the tree

Two prototype functions are already exactly North-star-shaped and every default below copies one of
them rather than inventing a pattern:

- `modelFor` (`agent.go:140-145`) — configured tier if set, else the session model. Its own comment:
  *"Missing tiers fall back to Model, so everything works zero-config and tiers are a pure
  optimization."*
- `maxTasksFor` (`orchestrator.go:16-27`) — a pure `switch` with a `default:`.

#### 4.2 The table

| Knob | `chat` | `code` | Derived by | Override key (item 18) |
|---|---|---|---|---|
| tool names | 3 → 5 with item 13 | 6 → 10 with item 13 | `Mode.Tools`, a literal in `mode.go` | `mode.<name>.tools` |
| model | session model | session model | `slot.<name>.model` → `slot.main.model` → effort tier → session model | `mode.<name>.model`, `slot.*.model` |
| effort | **inherit** | **inherit** | `Mode.Effort` is `""` on both | `mode.<name>.effort` |
| rounds/turn | 4 | `roundsFor(effort)` → 8/16/30/60 | `effort.go`, pure switch + default | `mode.<name>.rounds` |
| delegation width | 0 (no `delegate`) | `widthFor(effort)` → 2/3/4/6 (today's `maxTasksFor`) | `effort.go` | `effort.<level>.width` (item 7 owns it) |
| permission floor | **none** | **none** | `Mode.Perm` is zero on every built-in | `mode.<name>.perm` |
| system prompt | `identity(Env)` — **identical** | identical | `Resolve`, pure | — |
| mode paragraph | compiled in | compiled in | `Mode.Prompt` | `mode.<name>.prompt` (**appended**, never replacing) |
| memory | ✓ | ✓ | engine-level, not mode policy (§5) | — |
| glyph/hue | `◇` neutral | `▸` action | `Mode.Style` | `theme.*` (item 11) |

**A mode never moves the effort dial, and `Mode.Effort` is `""` on every visible built-in.** The
field exists — PLAN item 7 requires per-mode overrides and arch §9 already lists `mode.code.effort`
as a canonical config key — but shipping it unset gives byte-identical zero-config behaviour to
refusing it, and refusing it would force item 7 to re-open item 6. The reason no built-in sets it is
money: a design where typing `/chat` silently changes what you are billed is a design that lies
about which control did it.

**No per-mode model in v0.x either, and no sticky/learned one.** Per-mode models are the single
most-requested thing in this space (67👍) and Cline ships them, but on a fresh install there is
*one* model and a per-mode model map is a map that must be filled in — a config file on day one.
The hook ships (`Mode.Slot`, `slot.*.model`); the value does not. Roo's sticky-per-mode model is
**rejected**, not deferred: it makes `/model x` silently not apply after a switch, and it writes
state the user never asked for. If it ever ships it must obey §2.5's pin rule.

#### 4.3 Precedence

```
--mode / /mode / shift+tab  >  KOLK_MODE  >  ./.kolk/config  >  user config  >  Builtins()
```

Two rules attached:

1. **A project config that changes mode defaults prints one line on session start, naming the
   file.** Otherwise you get Copilot's *"even when users disable agent mode in settings, Copilot
   still acts like an agent from time to time"* — a settings/behaviour mismatch nobody can debug.
2. **kolk never creates `.kolk/`.** Read-if-present, never write. This is the deliberate override of
   PLAN item 6's "persisted per project (`.kolk/`)" wording (§0.4).

#### 4.4 The zero-config claim, as CI

```go
// TestZeroConfigModes builds the registry with KOLK_CONFIG_DIR pointed at an
// empty temp dir and asserts that chat and code both resolve to runnable modes
// with a zero Slots, zero Caps and an empty Env. This is what actually cannot
// be faked: the arch rule proves internal/engine cannot OPEN a file; this
// proves it does not NEED to.
func TestZeroConfigModes(t *testing.T)
```

---

### 5. Project memory — merge, walk, budget, freeze

#### 5.1 What is wrong today

`agent.go:53-57, 168-179`, all verified:

- **First-match-wins with `break`** (`agent.go:178`) → `AGENTS.md` is **dead code whenever
  `KOLKRABBI.md` exists**. With both files present the KOLKRABBI sentinel reaches the model and the
  AGENTS sentinel does not.
- **cwd only**, via `os.ReadFile(name)` with a relative path. Launch kolk from a subdirectory of
  your own repo and project memory **silently vanishes**. kolk is the only tool surveyed that does
  not walk the tree.
- **`b = b[:maxProjectMemory]` slices bytes, not runes.** A 16 KB+ file can be cut mid-UTF-8 —
  the same defect class item 3 already caught in the stream decoder — after which `json.Marshal`
  substitutes U+FFFD rather than erroring, and the model gets a sentence that stops mid-word with no
  marker.
- **It lands in the system prompt and is re-read on every call** — the cache landmine of §3.4.
- **It never reaches the agents that do the work.** With `KOLKRABBI.md` in cwd, all four agent-mode
  calls report `sysHasMemory=false` — planner, both subagents and synthesis. The user's project
  instructions are invisible in the mode that touches the most files.

#### 5.2 The convention, as of 2026

`AGENTS.md` is the only filename with a governance body behind it: an open spec since August 2025
(OpenAI, Google, Cursor, Factory), **donated to the Linux Foundation's Agentic AI Foundation in
December 2025**, >60,000 repos, native in Codex, Cursor, Copilot, Gemini CLI, Aider, Windsurf, Zed,
Factory, Jules, OpenCode and ~20 more. Claude Code is the notable holdout (`@AGENTS.md` import,
symlink, or `/import`). The spec's monorepo rule is part of the standard: *"agents automatically
read the nearest file in the directory tree, so the closest one takes precedence."*

| | files | multiple? | tree walk | user layer | cap |
|---|---|---|---|---|---|
| Claude Code | `CLAUDE.md`, `.claude/rules/*.md` | **all, concatenated** | cwd **and every dir above** | `~/.claude/CLAUDE.md` | 4 MiB hard skip |
| Codex | `AGENTS.override.md` → `AGENTS.md` | **all, concatenated** | **git root → cwd** | `~/.codex/AGENTS.md` | 32 KiB, skips **whole files** |
| OpenCode | `AGENTS.md`, then `CLAUDE.md` | first-match | up from cwd | `~/.config/opencode/AGENTS.md` | — |
| Gemini CLI | `GEMINI.md` (list configurable) | **all, concatenated** | cwd → project root | `~/.gemini/GEMINI.md` | — |
| **kolk today** | `KOLKRABBI.md`, `AGENTS.md` | **first-match** | **cwd only** | none | 16 KiB, **byte slice** |

Merge is the majority behaviour (3 of 4); **nobody but kolk is cwd-only**; precedence is universally
implemented as **position in the prompt**, broad first, specific last; and nobody truncates
mid-file — Codex and Claude Code both skip whole files.

**Consequence: `KOLKRABBI.md` is now the same category of decision `.cursorrules` was — a private
dialect.** It stays, documented as exactly one thing: *"optional, for kolk-specific overrides; read
last, so it wins."* Not promoted, not generated.

#### 5.3 The decision

**Load both. Merge. Broad → specific. Walk the tree. Budget the total. Freeze per session. Never
write one.**

```
1.  $XDG_CONFIG_HOME/kolk/AGENTS.md          the user layer — personal style
2.  <vcs-root>/AGENTS.md   →   <vcs-root>/KOLKRABBI.md
3.  …every directory down to cwd, same order…
4.  ./AGENTS.md            →   ./KOLKRABBI.md        ← last, therefore wins
```

**Why merge and not first-match.** The failure case is not "only one exists" — both orders work
there. It is **"both exist"**, which is the *deliberate* case: a shared team `AGENTS.md` plus a
small kolk-specific addendum. Today kolk reads `KOLKRABBI.md`, breaks, and **silently discards the
team's baseline**. That is a correctness failure the user cannot see.

**Why AGENTS.md first.** Precedence is prompt position in every shipping tool. Shared baseline
first, kolk-specific override last, falls out of the same rule Codex and Claude Code use.

**Why walk the tree.** The spec's own monorepo rule *is* the walk. Stop at the VCS root, or `$HOME`,
or 16 levels, whichever comes first, so `kolk` in `/tmp/x` never walks toward `/`. Outside a
repository: cwd only, never upward.

**Budget: 16 KiB total, across all files.** Selection walks the candidate list **backwards**
(nearest/most specific first, user file last), adding **whole files** while the running total fits;
emission is **forwards** (broad → specific), each under a `# from <relpath>` heading. Codex fills
root→cwd and stops at the cap, which drops the *most specific* files — the ones that matter most.
Selecting backwards and emitting forwards gets both properties for one reversed loop.

**Never truncate a file. Never slice bytes.** A file that does not fit whole is skipped and
recorded. The single exception: if the *first* (most specific) file alone exceeds the budget, take
its first N complete lines and append `…[truncated, N of M bytes]`.

**The budget is a guardrail we do not apologise for.** There is now a controlled study —
*"Evaluating AGENTS.md: Are Repository-Level Context Files Helpful for Coding Agents?"* (Gloaguen,
Mündler, Müller, Raychev, Vechev; ETH Zurich + LogicStar.ai,
[arXiv:2602.11988](https://arxiv.org/abs/2602.11988)), 138 real tasks across 12 repos plus SWE-bench
Lite — whose abstract reads: *"Surprisingly, we find that providing context files does not generally
improve task success rates, while increasing inference cost by over 20% on average,"* holding across
models, agents, and both LLM-generated and developer-committed files. Their diagnosis: instructions
are followed well; **repository overviews are not helpful.** Loading *more* memory is not a feature.

**Report what loaded and what was dropped** — one dim line on the first turn of a session, once
(item 4 §7.2's anti-nagging rule), never per turn:

```
memory  AGENTS.md (2.1 KB) · internal/engine/AGENTS.md (0.6 KB) · KOLKRABBI.md (1.4 KB)   4.1 KB
memory  AGENTS.md (2.1 KB) · skipped docs/AGENTS.md (19 KB — over the 16 KB budget)
```

`/memory` shows the resolved list; `/memory reload` re-reads and says what it costs.

**A silent drop is the one thing all four reference tools avoid.**

#### 5.4 Frozen per session, and derived — not an element of `Messages`

```go
// internal/session
type Memory struct {
	Digest  string   `json:"digest"`  // the rendered segment C, verbatim
	Sources []string `json:"sources"` // paths in load order — for /memory and doctor
	Bytes   int      `json:"bytes"`
	Hash    string   `json:"hash"`    // of the source files, for the changed-on-disk hint
}
```

Read **once, at session start**, through the new `engine.MemorySource` port (memory loading walks
the filesystem and reads XDG paths; L4 may do neither). Re-read only on `/memory reload`, mirroring
Gemini CLI; Hermes freezes its char-capped memory per session for the same reason.

Two reasons, and the second is the load-bearing one:

1. Re-reading per turn costs a cache write **every time the agent edits the file it is reading** —
   which in code mode is the normal case. Three memory edits in a 40k-token session is $0.41 of pure
   waste, and it scales linearly with context.
2. **The digest is stored as derived data on the session and rendered into position at request
   time — never as an element of `Sess.Messages`.** Item 12 adds auto-compaction with a summary
   strategy; the first compaction summarises the oldest messages, and if memory were `Messages[0]`
   the user's `AGENTS.md` would silently leave the conversation mid-session with no event and no
   line. As derived data, compaction operates on the history and cannot reach it.

Changed on disk → one line, never a silent stale:

```
  ⤷ AGENTS.md changed since this session started · /memory reload to apply (one cache write)
```

#### 5.5 Per-mode behaviour: none. Deliberately.

**Every visible mode loads project memory identically. `Mode.Memory` exists only so a hidden
fast-lane mode (`title`) does not carry 4 KB of project notes to write a six-word title, and it is
never exposed as a per-mode config key.**

The case against loading it in `chat` is real: memory is dominated by build/test commands and
"don't touch" boundaries, none of which is actionable, and the ETH study says agents follow
instructions faithfully — so tool-less compliance would be waste. It loads anyway, for three
reasons, in ascending order of strength:

1. Half of what memory contains is *orientation* (*"this is a Go CLI, stdlib only, no external
   deps"*), which materially improves exactly the answers `chat` exists to give. And `chat` can
   read, so build/test facts are actionable there too.
2. Cursor — the closest prior art and the vendor with the most users — reads `AGENTS.md` **in all
   modes including Chat**. No surveyed product varies memory by mode.
3. **Cache geometry.** Mode-dependent context is precisely what makes switching modes expensive. If
   `chat` and `code` carried different `messages[0]`, every switch would rewrite segment C *and*
   move OpenRouter's sticky-routing key. The saving from dropping memory in chat (a few hundred to
   ~2k tokens, served at **0.1×** when cached) is one to two orders of magnitude smaller than the
   write you pay to get it back.

**This settles item 6's "mode-specific memory" bullet: there is no such thing.** Memory is
engine-level, resolved once per session, identical everywhere. `Mode` has no memory *policy* field,
and `internal/memory` never sees a mode.

#### 5.6 The user layer, and what is explicitly not built

`$XDG_CONFIG_HOME/kolk/AGENTS.md` (else `~/.config/kolk/AGENTS.md`) — byte-identical in location to
OpenCode's, consistent with arch's XDG path strategy. All four reference tools ship one and they
converged on the same shape. It does not exist on a fresh install, and a missing file means no
behaviour change: ~10 lines of code, zero onboarding — North-star rule 5 exactly. Contents are
personal *style* ("short commit messages", "prefer table-driven tests", "answer in Spanish"), never
project facts.

**Not in v0.x:** no `kolk memory` command, no agent-written memory, no `MEMORY.md` index, no
per-project user file, **no `kolk init` that writes a branded file**. Claude Code's auto-memory is an
indexed directory with a 200-line/25 KB index cap and retention exclusions — that is item 12's
territory. v0.x reads files it never writes. Reading what is already there is the zero-config move;
writing a fifth dialect into the world is the opposite.

---

### 6. Visual identity — the contract for item 11

**The engine carries semantics; the surface carries pixels.** No ANSI escape leaves `internal/engine`
again. Today `agent.go:27-33` defines five colour constants and `agent.go:343,362` plus eleven sites
in `orchestrator.go` `Fprintf` them — the direct blocker for arch §7 and for every non-terminal
client. It also produces a live bug: `footer()` prints `a.Mode`, so on the single-task fallback path
it prints `agent` for a turn that ran the code loop.

#### 6.1 The seven clauses

1. **`Style` is semantic.** `Glyph` is exactly one rune of display width 1; `Hue` is an enum, never
   an escape code and never a hex value. `internal/cli/render` and `internal/tui` map `Hue` → ANSI;
   a GUI client maps it → CSS. `arch_test` fails on any `\033[` under `internal/engine` or
   `internal/orchestrator`.
2. **The prompt is bare in the default mode.**
   ```
   ›            code (the default) — the user need not know modes exist
   chat ◇ ›     any non-default mode — the prompt says so, always
   code ▸ → chat ◇ ›    a switch is queued for the next turn
   ```
   The prompt is read on every keystroke and the one thing it must never be wrong about is *"can
   this thing change my files."* It shows reach and only reach — not effort, not model, not cost.
3. **Status-line fields, in this order.** Item 11 owns layout, separators, truncation and theme;
   item 6 fixes the contents and the order:
   `{glyph}{mode} · {effort}→{model} · {reach} · {trust} · {session} · {ctx%} · ${cost}`
   - `reach` renders `read-only` when `!Tools.Mutating(set)`, `vendor tools` when
     `Caps.ExecutesOwnTools`, and **nothing** otherwise.
   - `trust` renders `yolo` only when auto-approving. It is its own field, always, because that is
     the visible expression of the two-dial decision and how a user discovers the dials are
     independent — the thing #5466 proves users need and cannot find.
   - `width` renders `3 subagents` **whenever it exceeds 1 and `delegate` is in the set**, so the
     second meaning of `/effort` is visible rather than inferred.

   ```
   ⟨claude⟩ code ▸ · high(req) → opus · vendor tools · 7d 78%
   chat ◇ · standard → openrouter/auto · read-only · s_01J… · 12% · $0.04
   code ▸ · deep → sonnet · 4 subagents · yolo · s_01J… · 34% · $0.21
   ```
4. **`NO_COLOR` and non-TTY must still distinguish the modes** — hence `Glyph` *and* the name. A
   mode distinguished only by colour is invisible to ~8% of men and to every pipe.
5. **Yolo overrides the hue, never the label.** Mode is a reach fact, yolo is a trust fact; they
   compose visually *because they are separate values*.
6. **One dim line on transition, never per turn.** No box, no colour block:
   `chat ◇ · reads only · standard → x-ai/grok-4`. Item 4 §7.2's anti-nagging rule is inherited
   verbatim: say it once, keep it visible at the line level, make it queryable, never repeat it.
7. **The turn footer names the mode that made the call**, which is correct by construction because
   it is the same value `stats` records.

#### 6.2 The protocol — zero new event types

arch §7 declares the event vocabulary **closed**, and §9 makes each event name a greppable string in
three places (name == schema filename == `type` value). Item 6 adds **no** event type. Every mode
transition, pending transition and suggestion is expressible in what already exists:

| Event | When | Payload added |
|---|---|---|
| `session.updated` | a mode changed, or a switch was queued | `mode: {name, summary, glyph, hue, reach, from, pending, at:"now"\|"turn_end", reason:"flag"\|"slash"\|"key"\|"resume"\|"suggested"\|"deprecated"}` |
| `turn.started` | every turn | `mode`, `model`, `width` — so a client labels a turn without inferring it |
| `permission.requested` / `permission.resolve` | the auto-suggest affordance (§8) | `kind:"mode_switch"`, `from`, `to`, `need` |
| `subagent.started` / `.finished` | a `delegate` call fans out | `mode` (always `"task"` in v0.x; item 14's routed modes ride the same field) |
| `hello` | handshake | `data.modes[] = {name, summary, glyph, hue, visible, available, why}` — so a GUI renders a mode picker it did not hardcode, including modes the user defined |

**Reusing `permission.requested` for the suggestion is not a trick.** It is the same shape — the
engine asks, a human answers, and arch §7 #4 already specifies a **server-side timeout policy for
when no client is attached**, which is exactly the `-p` rule this feature needs. A TTY-only dim line
would make the one genuinely new user-facing behaviour in this item invisible to the daemon, the
desktop and the iPad — precisely what §7 exists to prevent.

`hello.data.modes[].available` and `.why` come straight from `Mode.Check(caps)`, so a client's
picker greys out a mode with the same sentence the CLI prints. One predicate, four surfaces.

---

### 7. Vendor backends that own tools and history (item 4)

The mode table does not change. `Resolve` **projects** it onto the backend's `Capabilities`, and
every loss becomes a `Note` — never a silent degradation, never a second code path.

| Mode | On `claude` (`ExecutesOwnTools`, `!AcceptsToolSchemas`, `HistoryOwned`, `ModelAliasOnly`) | Verdict |
|---|---|---|
| **chat** | `--tools "Read,Glob,Grep"` + `--permission-mode dontAsk` | **Runs, and the guarantee gets *stronger*.** A tool absent from `--tools` is absent from the vendor's context, so "chat changes nothing and sends nothing" is structural at both layers. `WebFetch`/`WebSearch` are excluded to match §1.2. `needs_write` has no vendor mapping and is dropped, so auto-suggest is unavailable here — one line says so, once. |
| **code** | `--tools "Bash,Read,Edit,Write,Glob,Grep,WebFetch,WebSearch,TodoWrite"` (**no `Task`**) + `--permission-mode acceptEdits` | **Runs, degraded, and the degradation is marked per line.** kolk's confirm UX, path jail and hardline blocklist do not apply — item 4 §7.1 #1: the confirm UX is *structurally unreachable*. Every provider-executed call renders `▸ vendor  Bash(rm -rf build/)` from `EventToolCall.ProviderExecuted`. |
| **delegate** | no vendor mapping (item 4 defers the vendor's `Task` to v0.5) | **Dropped from `code`'s resolved set with one Note:** `delegate: off — Claude Agent schedules its own subagents`. ★ **This replaces item 4 §4.2's "v0.x refuses to bind `claude` to agent mode and says why" row entirely** — its refusal path, error message, docs paragraph and `kolk doctor` line all disappear. A missing tool is not a refusal, and that sentence is shorter and truer. |
| **task** | unreachable (no `delegate`) | — |
| **title** | skipped if `slot.fast` resolves here — a Node process spawn per title is 1–3 s | Skipped, never fatal. If the user also has an OpenRouter key, `slot.fast` resolves there and titling keeps working: the payoff of slots being per-mode. |

Five rules `Check` and `Resolve` encode:

1. **The mode paragraph has a delivery channel**, and it is `--append-system-prompt` (never
   `--system-prompt` — item 4's brightest line). Item 4 §4.4 #4 already re-passes the entire argv
   every turn, so a mid-session switch delivers the new paragraph for free. The payload is grepped
   for impersonation strings (item 4 C2).
2. **The memory digest has a delivery channel.** `HistoryOwned` means only the newest user turn goes
   over, so `messages[0]` reaches the vendor **never**. The digest is concatenated into **turn 1's**
   rendered user turn (the vendor then retains it in its own transcript across `--resume`) and
   re-attached on a soft restart with `WarnHistoryLost`. Status: `memory: sent once (vendor owns
   history)`. **The 32 KiB `--append-system-prompt` cap is shared with the mode paragraph: the
   paragraph never drops, memory drops first, and a drop is a Note.** This matters most here,
   because item 4's isolated profile (`--settings claudeMdExcludes:["**"]`) makes the vendor load no
   `CLAUDE.md` at all, so kolk owns the context deterministically or not at all.
3. **`Perm` stays a floor.** kolk's `ask` rules degrade to the vendor's **deny**, never to its
   `auto` classifier — mapping `ask` → `auto` silently *approves* what kolk's rules say to question.
   The status line renders trust as `vendor`.
4. **A mode switch on this backend costs nothing extra and loses nothing.** kolk re-passes its whole
   argv every turn and treats the flag vector as part of `ProviderState`. ⚠ **Item 4 §4.4 #4's
   "the flag vector changed" warning must not fire for a change the user just made and was already
   told about** — the comparison excludes deltas initiated this turn. This is an amendment item 4
   owes.
5. **`kolk` has no cache lever here** (`WarnCacheUnsupported`), so §3.4's arithmetic does not apply.
   A switch costs a changed flag vector and whatever the vendor's own cache does — which kolk
   neither sees nor controls. Say that; do not pretend otherwise.

**What is refused outright:** a mode whose resolved tool-name set cannot be mapped at all. It stays
in the `/mode` list, dimmed, with its reason (§3.1), and `--mode <it>` exits 2 with the same
sentence.

#### 7.1 The item-4 amendment this item owes

**`04-subscription-backends.md` §4.2's mapping table must be re-keyed by the resolved tool-name set,
not by mode name.** Today it has one row per mode name (`mode: chat`, `mode: code`, `mode: agent`).
Every future mode — `plan`, and every user-defined one, which by definition has no row — would
re-open a hardened document to add one, and a user-defined mode could never be mapped at all. The
replacement is one kolk-name→vendor-name map plus "drop unmapped names, emit a Note":

```
read_file→Read  list_dir→Glob  grep→Grep  glob→Glob  bash→Bash
write_file→Write  edit_file→Edit  multi_edit→Edit
web_fetch→WebFetch  web_search→WebSearch
delegate→(unmapped, v0.5)  needs_write→(unmapped)
```

The `--permission-mode` is then derived from the set, not the name: a set with no mutating tool →
`dontAsk`; otherwise → `acceptEdits` (or `bypassPermissions` under `/yolo`, with item 4's typed
confirmation).

---

### 8. Mode inference and auto-suggest

#### 8.1 The refusal: no classifier. Not on the first prompt, not by model, not by regex, not offline.

Ranked evidence, all of it against:

1. **In code mode the model is already a better classifier, for free, per turn.** Ask it about
   mutexes versus channels and it answers without touching the disk. **Every chat/code misroute a
   classifier would prevent, the model already prevents**, at the cost of ~474 tokens sitting in a
   cacheable prefix.
2. **Question form is the strongest signal in the text and it is the wrong one.** A hand-built
   offline heuristic scored 66% correct / **11.7% wrong**, and *all ten* `code→chat` errors were
   driven by `question word + "?"` — on prompts like *"why is TestStreamTools flaky?"* An asymmetric
   tune got wrong-direction errors to ~0% at the price of **abstaining on 39% of prompts**, and the
   abstentions were exactly the prompts where a suggestion would have helped.
3. **The best-resourced attempt in the industry still misses.** Anthropic's Claude Code auto-mode
   classifier sees the *fully-formed tool call* — far more information than a first prompt — and
   publishes **0.4% false positives and 17% false negatives**, stating it *"may allow risky actions
   when user intent is ambiguous."*
4. **The tools that guess get complained about in both directions.** Copilot auto-escalates
   Ask→Agent on "complex" requests and ignores the user's setting
   ([vscode#311893](https://github.com/microsoft/vscode/issues/311893),
   [community#159983](https://github.com/orgs/community/discussions/159983)); AnythingLLM #5520 is
   the inverse — an "auto" mode that never classifies. **Nobody has shipped a prompt→mode classifier
   users are happy with.**
5. **RouteLLM works** because it optimises a scalar (quality vs cost). Mode is not a quality axis;
   there is no ground-truth label to train against.

**The offline heuristic is also rejected for the one-shot path**, deviating from the probe that
built it. Under a code-by-default policy its *only* remaining effect is to sometimes pick `chat`, and
picking chat can only lose: code answers general questions fine and touches nothing. Zero value, one
more thing to explain, one more source of *"why did it do that."* And it would make `kolk "x"` and
`kolk` behave differently for a reason the user cannot see — **a rule you cannot show a user is a
config file you did not let them edit.**

#### 8.2 What ships instead: one tool, fired by evidence

`chat` carries one extra schema (~200 bytes, in the cacheable prefix). It is a **declaration, not a
guess**, and its trigger is a *completed model turn*, so the false-positive rate is structurally
near zero.

```go
{Name: "needs_write", Terminal: true,   // executing it ENDS the turn
 Description: "Call this the moment you conclude the task requires changing a file or " +
   "running a command. Say in one sentence what you would do. Do not apologise and " +
   "do not mention kolk's modes.",
 Parameters: {need: string (required)}}
```

`Terminal: true` matters: the result is rendered, not fed back to the model, so the turn does not
end in `ErrRoundsExhausted` and the model does not narrate around its own tool result.

`needs_write` touches nothing — it asks the *user* a question — so the guarantee is unchanged and is
worded accordingly in `kolk help mode`: **"no tool in chat can read, write or run anything on your
machine beyond reading the files you point it at, and none reaches the network."**

#### 8.3 Exact user-visible wording

First occurrence in a session, TTY only — one line, no dialog, no blocking:

```
  ⤷ this needs to edit internal/bus/bus.go — switch to code and retry? [Y/n]
```

Second occurrence and after — the bare hint, no prompt:

```
  ⤷ chat can't change files. shift+tab, then send it again.
```

Rules, each North-star-derived:

- **`Y` is capitalised.** The model just concluded it; agreeing is the common path (rule 6).
- **Accepting re-runs the same prompt** in `code`. Making the user retype it is the friction the
  feature exists to remove — OpenCode has the bug filed
  ([#6781](https://github.com/anomalyco/opencode/issues/6781): *"User: 'Go ahead and implement the
  plan' ← redundant"*).
- **Accepting truncates the failed turn first.** The user message, the assistant message and the
  `needs_write` result are removed before the prompt is re-submitted. Otherwise the history gains a
  duplicate user turn with a refusal wedged between the two copies, `/rewind` and `kolk export` show
  the same request asked twice, and the model sees its own "I cannot do that" immediately before
  being asked again — a prior it must be talked out of. `session.updated{mode:{reason:"suggested"}}`
  records why.
- **Once per session as a prompt.** The second occurrence degrades to the hint; two declines degrade
  to hint-only for the session. **Never a config key.**
- **Never the reverse direction.** kolk never suggests `chat`. `code` is a superset, there is no
  user harm to prevent, and suggesting downward is exactly what Copilot's bug reports look like.
- **Never under `-p` / non-TTY.** The hint goes to **stderr**, the answer to stdout, exit code
  unchanged. A scripted invocation must never change mode. This falls out of arch §7 #4's no-client
  timeout policy for `permission.requested`.
- **Never auto-applied.** Only a human emits the change.

#### 8.4 The default mode, one-shot vs interactive

**`code`, everywhere, always. One sentence, no table.**

Claude Code splits its default by invocation (`-p` → `default`, terminal → `auto`) and pays for it
with a **six-row table of preconditions** in a 73 KB docs page. That is the smell the North star
exists to prevent.

**★ The rule, stated so item 13 inherits it: reach never varies with TTY-ness. Trust does.** A
non-TTY genuinely cannot answer a prompt; nothing about a pipe changes what the user asked for, and
a script whose blast radius depends on whether stdout is a terminal is unpredictable by
construction.

**This exposes a live bug that item 6 must not ratify.** `internal/cli/cli.go:45` always sets
`in: bufio.NewReader(os.Stdin)` and `run.go:89` passes it unconditionally, so `agent.go:276-278`'s
`In == nil → false` safe default is **unreachable**. Verified: `kolk -p "…" </dev/null` reaches the
first `write_file`, prints `Allow? [y/N]:` into its own answer stream, prints `skipped.`, skips
every write and **exits 0**; from a terminal with an open pipe it blocks. Every downstream item that
shells out to `kolk -p` (saga, hooks, CI) would inherit a no-op that looks like a pass.

> **The rule, for arch §12 step 8:** when stdin is not a TTY and `-y` was not given, the `Decider`
> is **auto-deny returning a typed error naming the tool and `-y`**, and the process exits
> non-zero. `In: a.in` stops being unconditional. Stated as **new behaviour**, not as a description
> of today.

---

### 9. The naming verdict

#### 9.1 `agent` does not survive. Six reasons, none aesthetic.

1. **It is a width, not an intent — and item 7 already owns width.** Nothing in the surface text of
   *"refactor the provider layer"* distinguishes the version that warrants six parallel subagents
   from the version that does not. The user cannot know it before the repo is inspected either. A
   thing you cannot state before stating your problem is a **setting**, not a mode.
2. **The whole product is an agent.** `agent ▸` in kolk's own status line is ambiguous about its own
   subject, and **no surveyed product uses "agent" as a mode name.**
3. **The industry already reversed this exact decision.** Kilo ships `orchestrator` marked
   *"deprecated: Will be removed; agents with full tool access now support subagents natively."*
   Amp, Claude Code, Codex and Gemini all treat fan-out as a **tool** whose width is set by an
   effort dial.
4. **The code proves it was never a mode.** Two system prompts for three names; agent falls into
   `default:`. No per-mode model, no per-mode effort, no per-mode permission policy anywhere. A mode
   with no prompt, no model and no policy of its own is a dispatch flag.
5. **It has two context semantics chosen by a model's JSON output.** Per-turn amnesiac — *except*
   when the planner returns one task, at which point `orchestrator.go:51-58` pops the user message,
   saves the truncated session and re-enters the fully history-aware `runLoop`.
6. **It is structurally cache-hostile**, forever, by design (§2.8). Keeping it as a first-class mode
   means the flagship state is the one that can never warm a cache.

#### 9.2 `chat` and `code` survive; `plan` is reserved; no Norse, no aliases

- **`chat`** — one syllable, universally understood, four letters, and the owner's word. After the
  redefinition it is also *accurate*: you chat, it reads, it changes nothing. Rejected: **`ask`**
  (collides with item 13's `allow`/`ask`/`deny` permission verb — using one word for both a reach
  mode and a trust posture is the exact axis-confusion this design removes); **`read`** (names the
  constraint, not the activity).
- **`code`** — accurate, the default, the word seen most. Unchanged.
- **`plan`** — reserved for item 15 (§1.3). Four letters, shared by Cline, OpenCode, Gemini CLI,
  Codex, Claude Code, Kilo and Roo, so it is already in the muscle memory of anyone who would try
  kolk. Item 4 §4.3 and PLAN item 15 both already say "kolk's own `/plan`"; item 6 ratifies the name
  and owns the row.
- **No Norse names for modes.** `saga` (item 10) earns it — a **product noun**, a thing you run,
  discovered once and remembered. A **state you are in**, printed in the prompt on every keystroke
  and read by someone who has never opened the docs, is the worst possible place for a word that
  must be decoded. `spor`, `hugr`, `spá` and `smíð` were considered; half need diacritics (a typing
  tax on the happy path) and none reads as anything to a non-Icelandic user. North-star rule 6:
  *simple to type beats simple to explain* — `chat` and `code` are both.
- **Zero aliases.** `docs/plan/02-architecture.md:857`, inherited verbatim: *"one word, lowercase,
  ≤ 6 letters, no synonyms."* Shipping `/ask` `/talk` `/build` `/do` would triple the vocabulary on
  day one for no gain. There is **no `Aliases` field** on `Mode`; a rename, if one ever happens, is a
  rename.

#### 9.3 The migration for the owner's mental model

Three mechanisms, all cheap, all landing in migration step M1:

1. **`/agent` and `--mode agent` keep working through v0.3.** They set `code` + `deep` effort and
   print the translation in full once per session, then a one-line echo (§3.5). Removed at v0.4 with
   a `CHANGELOG` line. This is a **deprecation, not an alias**: it is in a `deprecated` map, not in
   `Builtins()`, so it never appears in `/mode`, in completions, in `--mode`'s error text or in
   `hello.data.modes[]`.
2. **The capability is relocated, not lost**, and it ships in the same milestone: `delegate` is in
   `code`'s tool list from the day this refactor lands. `runSubagent` (`orchestrator.go:154-198`) is
   already ~90% of the tool body. **If `delegate` slips, do not land the mode refactor either** —
   shipping `chat`/`code` with no delegation at all is a real product regression, however much
   cleaner the code is.
3. **`stats.jsonl` never fractures.** The log is append-only, so retiring `agent` would split every
   historical model row into two `ByMode` buckets and break the `MODES` column. `stats.Aggregate`
   gets a **read-time alias map**, one literal plus one table test, and no data is rewritten:

   ```go
   var modeAlias = map[string]string{"agent": "code"}
   var roleAlias = map[string]string{ // the retired Role column
       "main": "code", "planner": "code", "synthesis": "code", "subagent": "task"}
   ```

**One analytics simplification rides along.** `stats.Record` carries both `Mode` and `Role` today,
and after this change they would overlap confusingly (`MODE=code / ROLE=code`,
`MODE=code / ROLE=task`). The dashboard is the product's headline feature and must not need a
paragraph before a number can be read. So: **one dimension per call — `mode`, set to the mode that
made *this* call** (`code`, `task`, `title`, and item 14's routed modes). The `Role` field stays in
the struct for reading old rows and is no longer written. The session's mode is a session-level fact
and belongs on the session row.

#### 9.4 The steelman for having no modes at all — accepted in half

**The case.** The industry converged and it is not on capability modes:

| Product | capability modes | permission modes | named agents | effort dial |
|---|---|---|---|---|
| Claude Code | **none** | 6 | subagents | `/effort` |
| Gemini CLI | **none** | 4 (`ApprovalMode`, `MODES_BY_PERMISSIVENESS` — a total order on *trust*) | subagents | — |
| Codex | **none** | approval + sandbox | subagents as config | reasoning effort |
| Amp | **none** | none by default | oracle tool | **modes = low/med/high/ultra**, i.e. kolk's effort dial |
| OpenCode | *was* modes → **renamed agents**; the `mode` option is deprecated | ruleset per agent | build/plan/explore | reasoning variants |
| Kilo / Roo | modes → **agents**; **`orchestrator` deprecated** | per agent | yes | sticky per-mode model |
| Goose | `chat` only | auto/approve/smart_approve | recipes | — |

OpenCode's source proves the record shape by construction: `build` and `plan` differ **by data, not
by code**, and even the fast-lane callers (`title`, `summary`, `compaction`) are agents with
`"*": "deny"`. That is exactly `Visible: false`.

**Accepted in full: the orchestration half collapses.** `agent` dies, width is effort, fan-out is a
tool. That is half of PLAN item 6's original premise, gone.

**Rejected, with evidence: the reach half does not collapse into permissions.**

- **Only reach is hard-enforceable.** A permission rule is checked at call time and can be talked
  past; an absent schema cannot be called. Every leak bug in the survey is a permission-layer mode
  failing softly.
- **Item 4 depends on it.** On `ExecutesOwnTools` backends kolk's permission rules gate nothing;
  `--tools` is the only lever kolk has. Delete reach as a concept and kolk has **no enforcement
  mechanism on the Claude Agent backend at all.**
- **One axis cannot express two wishes** (#5466, 45👍). Reach × trust is a 2-D space, and a **named
  point in that space** is the North-star-compliant way to expose two dials: the status line needs
  one word, and `tools:5 posture:ask` is not it.

**And the decision is falsifiable.** `stats.Record.Mode` makes mode an analytics dimension from day
one. If `chat` is under ~5% of turns after a month of real data, the right move is to delete one row
from `Builtins()` and keep `Mode` as a config-only value — a ~30-line change. **That the demotion
would be a data change is the strongest evidence the record is the right shape regardless of how the
count ages.**

---

### 10. Who owns which dial

The rule: **item 6 owns exactly one axis — reach.** Everything else it merely *carries*.

| Dial | Question it answers | Owner | Where it lives | What item 6 may do |
|---|---|---|---|---|
| **reach** | what may it touch at all? | **item 6** | `Mode.Tools` (names) | own it outright; enforce by omission |
| **trust** | how much does it ask first? | **item 13** | `perm.Ruleset`, `/yolo`, `-y` | carry a **floor** on `Mode.Perm`; **every built-in leaves it zero**. A mode may never make a user's `ask`/`deny` rule unreachable, and the hardline blocklist survives everything. Resolved in `internal/perm`, not in the engine. |
| **depth** | how hard does it think? | **item 7** | the effort dial → model tier, `reasoning.effort` | carry `Mode.Effort`, `""` on every visible built-in |
| **width** | how far does one turn fan out? | **item 7** | `widthFor(effort)` → 2/3/4/6 | carry `Mode.Width` (`0` = derive) and **whether `delegate` is in the tool list at all**. Membership is mode; budget is effort. Effort may never change the tool array. |
| **rounds** | how long may one turn loop? | **item 7** | `roundsFor(effort)` → 8/16/30/60 | carry `Mode.Rounds` (`0` = derive); `chat` pins 4 as a runaway guard, `task` pins 12 |
| **model** | which model? | **item 8** | slots + tiers + the pin | carry `Mode.Slot`. An explicit `/model`/`-m` **pin** beats every slot, tier and computed default; nothing computed or learned ever overrides it. |
| **orchestration** | how is fan-out executed? | **item 14** | `internal/orchestrator` | provide the `task` mode, `engine.Runner`, the per-turn width counter, and `Interactive:false` so subagents auto-deny instead of deadlocking on one stdin |
| **longitudinal progress** | one goal over many chapters | **item 10** | `internal/saga` | provide a **hidden** `chapter` mode whose tool set and `Mode.Perm` floor *are* the unattended allow-list, and exempt hidden modes from the reserved-verb check so `saga` can stay a top-level verb |
| **rendering** | what does it look like? | **item 11** | `render`, `tui`, themes | provide `Style{Glyph, Hue}` and the field list in §6; own no pixel |
| **extensibility** | user-defined modes, MCP tools | **item 16** | `modes/*.md`, `mcp/<server>/*` | provide the record and one trailing-`*` glob; ship the loader off |

**No control appears twice.** The two visible dials a user has are shift+tab (reach) and `/effort`
(depth+width+rounds), with `/yolo` (trust) on its own. `Ctrl+P` is reserved for item 13 if trust
grows a ladder; shift+tab never acquires a second meaning.

---

### 11. Testing

#### 11.1 The five e2e tests

`newTestAgent(t, srv, mode string)` — that `mode` parameter is exactly the seam where a `Mode` goes.

| Test | Fate | Detail |
|---|---|---|
| **`TestE2E_ToolLoopWithPersistenceAndRewind`** (`:40`) | **survives, one edit** | Tool execution on disk, streaming, the cost footer, the tool-result round-trip, 2 stats calls, rating aggregation and rewind are all mode-blind engine behaviour. The single coupling is `:97-99`, which hardcodes *"the system prompt is `Messages[0]`"* as a **count of 5**. It becomes 4 — and it should assert **roles in order** (`user, assistant(tool_calls), tool, assistant`), not a count, so the next format change does not re-break it. |
| **`TestE2E_ChatModeHasNoTools`** (`:141`) | **survives, promoted, renamed** | This *is* the reach assertion. `srv.Tools[0] == 0` becomes `TestReachTable`: a table over the registry asserting tool **names** on the wire — `chat` = the read set + `needs_write`, `code` = the read set + write/edit/bash + `delegate`, `task` = `code` minus `delegate`, `title` = none. **Names, not counts**, so item 13 adding `grep`/`glob` does not turn a policy test red. One line in `internal/enginetest/router.go:91`: `ToolNames [][]string` alongside the existing `Tools []int`. One e2e survives asserting `chat` sends **no mutating schema**. |
| **`TestE2E_OrchestratedAgentMode`** (`:160`) | **rewritten, same scenario** | Scripted steps become: assistant calls `delegate` with 2 tasks → two sub-turns run (one writes the file) → assistant's final message. **Survives verbatim:** the subagent wrote the file (`:189`), and the last request's tool count (`:217`). **Changes:** stats modes become `code:N, task:M` instead of `planner:1 subagent:3 synthesis:1`; the main history becomes `user, assistant(delegate), tool, assistant` — 4 messages, not 3, **and that is the improvement**, because today's transcript cannot explain where its answer came from. **Dies:** the three stdout substrings at `:194` (`"plan (2 tasks)"`, `"subagent 1/2"`, `"subagent 2/2"`) assert `printf`s at `orchestrator.go:60,68`; they become `subagent.started`/`subagent.finished` assertions at arch §12 step 7, a conversion that step **already schedules**, so item 6 rides it rather than paying for it. |
| **`TestE2E_OrchestratorFallsBackOnSingleTask`** (`:224`) | **deleted — and the deletion is the signal** | It encodes exactly the behaviour this item removes: paying for a planner round-trip and then silently becoming code mode, with a history assertion that depends on the pop-then-re-append hack at `orchestrator.go:55`. Replaced by `TestDelegateClampsPerTurn`. |
| **`TestE2E_ResumeRepairsDanglingToolCalls`** (`:246`) | **survives untouched** | Constructed with **no `Mode`** (`:271`), so it exercises the default; asserts only the repair. It silently relies on `New` clobbering `"old system prompt"` at index 0 but never asserts it, so it is indifferent — after the change, `session.Load` demotes that fixture message and the assertion is unaffected. |

**Net at the end: 22 → 27.** The CI floor moves only upward and only in the commit that adds the
tests.

#### 11.2 New offline tests

| Test | What it proves | Cost |
|---|---|---|
| `TestResolveIsPure` | a table over `(mode × effort × capabilities × toolset)` producing `Resolved`, compared with `reflect.DeepEqual` — **no HTTP server, no temp dir, no goroutine, no engine.** This is the point of the design. | free |
| `TestZeroConfigModes` | §4.4 — an empty `KOLK_CONFIG_DIR` yields runnable `chat` and `code` | free |
| `TestReachTable` | §11.1 — tool **names** per mode, on the wire | 1 e2e |
| `TestParseMode` | `--mode banana` → exit 2; `--mode title` (hidden) → exit 2; `--mode agent` → `code` + `deep` + the translation | free |
| `TestResumeRestoresMode` | `kolk -r` on a chat session comes back in chat with chat's tool set | free |
| `TestSwitchKeepsHistoryWithAbsentToolSchemas` | a code turn with a `write_file` call → `/chat` → the next turn succeeds. §3.3's claim, **asserted, not assumed** | 1 e2e |
| `TestDelegateClampsPerTurn` | two `delegate` calls in one assistant message share one budget and the second is refused past zero | free |
| `TestMemoryWalkAndBudget` | `testdata/` tree: merge order, VCS-root stop, whole-file skip, no rune split, the report line | free |
| `TestMemoryFrozen` | editing `AGENTS.md` mid-session does not change the rendered digest until `/memory reload` | free |
| `TestStatsModeAlias` | historical `agent`/`planner`/`subagent` rows aggregate correctly | free |
| `TestVendorProjection` | `Resolve` against `claude`-shaped `Capabilities`: `delegate` dropped with a Note, `ToolNames` populated, `Tools` empty, `Executor == ExecVendor` | free |
| `arch: engine imports no os/config` | §2.9 rule 1 — the North-star claim as a build failure | free |

**What the existing suite does *not* cover is why this restructure is cheap:** no test asserts a
per-mode system prompt, a per-mode model, a per-mode effort or a per-mode permission policy. Those
axes are entirely uncovered because they do not exist.

---

### 12. Implementation checklist, mapped onto arch §12

**Invariant: `scripts/test.sh` is green after every step and the CI test-count floor of 22 holds at
every commit.** No red build window. Exactly two steps touch an existing assertion; both say so.

| # | Step | Rides arch §12 | Tests |
|---|---|---|---|
| **M1** | **Vocabulary and the door.** `internal/engine/mode.go` with `Mode`, `ToolFilter`, `Builtins()`, `Registry`; `--mode`/`-e` validated at parse time against `Visible()`; `/chat` `/code` generated; `kolk mode` verb; the `/agent` deprecation with its translation; `stats.Aggregate` alias map. **No engine behaviour changes** — `Registry.Get(name).Tools` is not read yet. | step 3 (cli split, done) | 22 + 3 |
| **M2** | **Persistence.** `session.Mode`, `session.Effort`, `session.Memory`; `Load` demotes a v0 leading engine-authored system message to a labelled `<prior-system>` user prelude, keyed on the absence of the `mode` field. `testdata/v0-session.json` + the load test **in the same commit** — the fixture is the whole defence. | **step 10** (the on-disk cut) | 22 + 2 |
| **M3** | **The tool set becomes data.** `tools.Set`, `tools.Env`, `tools.Tool` with `Terminal`; `Set.Definitions(names)`; delete `toolsFor` and `orchestrator.go:174`'s direct `tools.Definitions()`. `enginetest/router.go` records `ToolNames`. `TestE2E_ChatModeHasNoTools` → `TestReachTable`. | after step 9 | 22 (1 rewritten) |
| **M4** ★ | **The prompt move — land it alone.** `prompt.go`'s `Render`; `Env{OS,Cwd}` injected by L6; the system prompt leaves `Sess.Messages[0]`; the mode paragraph moves to the tail, re-rendered every turn. **The one step that edits an existing assertion** (`agent_test.go:97-99`, 5 → 4, rewritten to assert roles in order). Put the old count in the commit message. | step 10 | 22 |
| **M5** | **Memory.** `internal/memory` (L5) + the `MemorySource` port + walk/merge/budget/report + `/memory` and `/memory reload`. The `b[:16384]` byte slice is deleted, not fixed. | step 9 | 22 + 2 |
| **M6** | **`Resolve` + one loop.** `ResolveModel`, `Resolve`, `Resolved`; `RunTurn` branches only on `Caps.ExecutesOwnTools`; `runOrchestrated`, `plan`, `parseTaskList`, the synthesis call and the `orchestrator.go:51-58` fallback are deleted. `delegate` ships in the same commit via `internal/orchestrator/delegate.go` + `engine.Runner.Sub` + the per-turn width counter. `TestE2E_OrchestratedAgentMode` rewritten; `TestE2E_OrchestratorFallsBackOnSingleTask` deleted; `TestResolveIsPure` + `TestDelegateClampsPerTurn` added. | step 9 | 22 (−1 +2) |
| **M7** | **The non-TTY decider.** `In: a.in` stops being unconditional; not-a-TTY without `-y` ⇒ auto-deny with a typed error naming the tool and `-y`, non-zero exit. §8.4. | **step 8** | 22 + 1 |
| **M8** | **Identity leaves the engine.** `Style`/`Hue`; the five ANSI constants deleted; `session.updated{mode}`, `turn.started{mode,model,width}`, `hello.data.modes[]`; `subagent.*` gains `mode`. The three stdout substrings become event assertions. `arch_test`: no ANSI under `engine`/`orchestrator`. | **step 7** (the bus) | 22 |
| **M9** | **The zero-config ratchet.** `internal/engine` may import neither `os` nor `internal/config`; the `knownViolations` entry added at M4 is deleted here. `TestZeroConfigModes`. | after step 9 | 22 + 1 |
| **M10** | **The auto-suggest affordance.** `needs_write` (`Terminal`), `permission.requested{kind:"mode_switch"}`, truncate-and-retry, once-per-session, off under `-p`. | after step 11 (`serve/permission.go`) | 22 + 1 |
| **later** | `plan` flips `Visible: true` with item 13's `grep`/`glob` + read-only shell profile. Item 16's `modes/*.md` loader, with `plan` round-tripped through the parser as the format's first test. | — | — |

**Sequencing debt, stated:**

- `provider.Message.Synthetic` must exist **before** M4, or the first synthetic tail message is
  indistinguishable from a user turn on resume.
- M2 must precede M4: the format cut is what makes the demoted v0 system message safe.
- If `delegate` (M6) slips, **hold the whole mode refactor**. Shipping two modes with no delegation
  is a capability regression versus the prototype.

---

## Rationale

**Why a record and not three code paths.** Every serious implementation surveyed converged on the
same shape — *name + prompt + permission ruleset + optional model/temperature/steps*, resolved by
one engine. OpenCode proves it by expressing even its own fast-lane callers (`title`, `summary`,
`compaction`) as agents with `"*": "deny"`. kolk's `RunTurn` branching to `runOrchestrated` vs
`runLoop`, with a second nested dispatch inside the first that mutates history, is the shape
everyone else has already refactored away from. A record also makes the mode system table-testable
with struct literals — no HTTP server, no temp dir — which is the difference between a design whose
correctness is asserted and one whose correctness is hoped for.

**Why two visible modes and not three.** Because the third one was not a mode. `agent` was a width,
and width belongs to a dial the user already has. The evidence is unanimous across the industry
(Kilo's deprecation with matching reasoning; Amp, Claude Code, Codex and Gemini all treating fan-out
as a tool), inside kolk's own source (two prompts for three names), and in the classifier
experiment (nothing in a request's text distinguishes wide work from narrow work; the user cannot
know either, before the repo is inspected). Asking a new user to choose an orchestration strategy
before stating their problem is the single most North-star-hostile thing in the current design.

**Why the second mode reads.** Because a second mode that cannot answer the most common question in
a terminal is abandoned after one try, and three modes then collapse to one in practice. The
guarantee that made a tool-free chat attractive survives the redefinition — "it changes nothing and
sends nothing" is still one sentence, still structural, still stronger on the vendor backend than on
any other — and the mode becomes useful on day one instead of being a downgrade of the default.

**Why the system prompt stops being conversation state.** It is the precondition for item 3's entire
caching design, which already names `agent.go:102-107` and `agent.go:120` by line as the blocker.
Making the prompt derived data turns five separate cache invalidators (process start, `/mode`, a
memory edit, a different cwd, a resume) into one, and it stops a mode switch from moving OpenRouter's
sticky-routing key — which is what makes switching free on every implicit-caching provider.

**Why enforcement is structural.** Every leak bug in the survey came from trusting the prompt. An
absent schema cannot be called; a deny rule at call time produces a model that tries, is refused,
apologises and re-plans, one wasted round each time. And on an `ExecutesOwnTools` backend the tool
list is the *only* lever kolk has — delete reach as a concept and kolk has no enforcement mechanism
there at all.

**Why memory merges, walks and freezes.** First-match-wins silently discards a team's shared
`AGENTS.md` whenever a `KOLKRABBI.md` exists, which is the deliberate case, and the user cannot see
it. cwd-only makes project memory vanish from a subdirectory of the user's own repo. `AGENTS.md` now
has a governance body and >60,000 repos behind it, so reading it is free value on a first run in a
repo written for some other tool. Freezing is what stops a coding agent from invalidating the cache
every time it edits the file it is reading.

**Why no classifier.** The best-resourced attempt in the industry, seeing far more information than
a first prompt, publishes 17% false negatives; a hand-built heuristic's strongest signal is the wrong
one for this product; and the tools that guess are complained about in both directions. Meanwhile
the model in code mode already decides per turn whether to touch anything, for free. The only place
a suggestion carries information is where the model *cannot* act and has to say so — which is a
completed turn, not a prediction.

---

## Alternatives rejected

- **Keep three modes and redefine `agent` as "code plus a `delegate` tool".** Cleaner than today,
  but it keeps a name no surveyed product uses for a mode, inside a product that *is* an agent, for
  a distinction the user cannot make before stating their problem — and it keeps a `/mode` row whose
  summary cannot be written without explaining orchestration.
- **`agent` forced to call `delegate` on round 0 via `tool_choice`.** A bill the user did not agree
  to, on every turn including trivial ones, landing hardest on the fresh-install free model — and it
  rests on the least uniformly supported parameter in the provider surface.
- **A totally-ordered `Reach` scalar (`none < read < write`).** Cannot express "chat with web but no
  files" or "code without bash"; both fall through to call-time deny rules, i.e. soft enforcement,
  and neither is expressible at all through `--tools` on a vendor backend.
- **Tool `Class` as the only filter.** Same failure, plus MCP tools cannot declare a kolk class, so
  every MCP tool would be permanently invisible to every read-only mode.
- **Collapse everything into permission modes (Claude Code / Gemini's shape).** One axis cannot
  express two wishes (#5466, 45👍), only reach is hard-enforceable, and item 4 depends on reach as
  its only lever.
- **`chat` keeps zero tools; add `plan` as the read-only third mode.** Two modes that both refuse to
  read, one of which cannot search until item 13 lands. The most attractively-named mode would ship
  as the least capable one.
- **Ship `plan` now as a visible third mode.** Rejected on capability, not on principle: with no
  `bash`, no `grep` and no `glob` it cannot run `git diff` or find a symbol, and first impressions
  of a third mode are formed once.
- **Per-mode effort defaults, and per-mode models in v0.x.** Typing `/chat` would silently change
  what you are billed, and a model map that must be filled in is a config file on day one. Both
  hooks ship; neither value does.
- **Roo-style sticky per-mode models (learned).** Zero-config and tempting, but it makes `/model x`
  silently not apply after a switch and writes state the user never asked for. PLAN item 8's "never
  silently change a pinned model" is inherited, not renegotiated.
- **Ship the offline mode-inference heuristic for one-shot invocations.** Its only remaining effect
  under a code-by-default policy is to sometimes pick `chat`, which can only lose — and it would make
  `kolk "x"` and `kolk` differ for a reason the user cannot see.
- **Refuse a mid-turn switch.** Makes the product's only keybinding look broken at exactly the moment
  the user wants out. Queue it, show it in the prompt, and let Ctrl+C apply it now.
- **New `mode.changed` / `mode.suggested` protocol events.** `session.updated` and
  `permission.requested{kind:"mode_switch"}` carry the same information inside a vocabulary arch §7
  declares closed, and the latter inherits §7 #4's no-client timeout policy for free.
- **Mode-name aliases (`/ask`, `/build`, `/hird`).** arch §9: no synonyms. Eight ways to say two
  things, in `/help`, in completions and in every doc.
- **Two user-facing words (`role` for the record, `mode` for the visible subset).** The dashboard is
  the product's headline feature; two overlapping columns there would need a paragraph before a
  number could be read. One word: `mode`. Hidden modes are an implementation detail with a field
  name, not a second vocabulary.
- **Filtering the `/mode` list by backend.** A vocabulary that shrinks with state the user did not
  set breaks every doc, tutorial and habit, and a missing row explains nothing. Dim it and give the
  reason.

---

## Risks & open questions

- **Two modes when three were asked for.** → `/agent` keeps working through v0.3 with a printed
  translation; the third state moved to a dial the owner already uses (`/effort deep`); the word
  survives where it is true (kolk *is* an agent). Mitigated, not erased: someone who wanted three
  named states now has two.
- **The forced planner pre-pass is gone**, so the model may decline to fan out on a task that would
  have benefited. → Width is visible in the status line whenever it exceeds 1, and the mode
  paragraph tells the model its budget. If real usage shows under-delegation, the fix is prompt
  wording, not a forced tool call.
- **`chat` changes meaning for an existing user.** "Never touches anything" becomes "never *changes*
  anything." → One printed-once notice stating what chat is now, in one line, and nothing else. The
  escape (`mode.chat.tools`) lives in `kolk help mode`, not in the notice — the product's own
  onboarding line must never read `kolk config set …`.
- **A switch still costs one cache write (~$0.14 at 40k tokens).** → Unavoidable without soft
  enforcement. Priced in §3.4; the design's win is that it is now the *only* invalidator.
- **`delegate` grows the main transcript** by two messages per fan-out where the prototype cost
  zero. → Summary-only returns capped at 2 KiB per task; full sub-run transcripts go to the bus. Net
  a win (resumable, rewindable, exportable), but context grows faster.
- **`Mode` is a wide struct and it is persisted-adjacent.** Items 13, 14, 15 and 16 all want a
  field. → `spec/VERSION` stays `"0"` with additive-only changes; `Mode` itself is **not** in
  `protocol/` — only a seven-field projection reaches `hello`. Keeping the full struct internal is
  what lets it churn. "Add a field to `Mode`" will still be the path of least resistance for the
  next three features, and it will be wrong at least once.
- **Frozen memory can be stale.** → One line when the files change on disk, `/memory reload`, and
  the once-per-session report that names what loaded. A user who ignores all three will conclude
  kolk ignores their rules.
- **shift+tab collides with Claude Code muscle memory**, where it cycles *permission* modes. → The
  status line makes the result instantly visible and `/yolo` is one word away. There is no key that
  satisfies both memories.
- **`plan` ships later, so item 15's flow is blocked on item 13.** → Stated as a sequencing
  dependency, not discovered. Item 15's `/plan` may ship earlier as a *one-turn* form over `chat`
  (`/chat` already has that shape) if the mode row is not ready.
- **Open: whether `chat` should gain `web_fetch` behind item 13's network toggle, or stay local
  forever.** → Decided for v0.x (local only). Revisit when item 13's toggle exists and the guarantee
  can be stated in one sentence *with* the toggle in it.
- **Open: item 16's `modes/*.md` format is designed now and validated at v0.4.** → Mitigated by
  expressing the built-ins in the same struct and round-tripping `plan` through the parser the day
  the loader lands, but a year of compatibility starts from a format nobody has used.
- **Open: whether hidden modes should be listable at all** (`kolk mode --all` for debugging). →
  Deferred to item 9's command surface; `kolk doctor` prints them today.

---

## Sources

Verified against the working tree at commit `8dc1dce` on 2026-08-23 unless noted.

**This repository**
- `internal/engine/agent.go` — `Modes` `:34-42`, `projectMemoryFiles` `:53-57`, `New`'s
  `Messages[0]` write `:102-107`, `SetMode` `:116-125`, `modelFor` `:140-145`, `toolsFor` `:148-153`,
  `systemPrompt` + the memory splice and the byte slice `:155-181`, `repairDanglingToolCalls`
  `:186-213`, `confirm`'s unreachable `In == nil` `:272-287`, `footer` `:305-316`, `RunTurn`'s
  dispatch `:320-329`.
- `internal/engine/orchestrator.go` — `maxTasksFor` `:16-27`, the single-task fallback `:51-58`, the
  planner prompt `:117`, the per-subagent briefing `:157-166`, `tools.Definitions()` `:174`.
- `internal/cli/cli.go:45` and `internal/cli/run.go:89` — the unconditional `bufio.NewReader(os.Stdin)`.
- `internal/cli/flags.go` — `--mode` unvalidated. `internal/session/session.go:21-30` — no `Mode`.
- Tool-schema size measured 2026-08-23 via `json.Marshal(tools.Definitions())`: **1,899 B total**
  (bash 440 · edit_file 540 · write_file 350 · read_file 296 · list_dir 267).
- `docs/plan/02-architecture.md` §2 (tree), §5 (layers, §5.0 ratchet, §5.1 ruling), §7 (bus and the
  closed event vocabulary), §9 (naming, config keys, reserved verbs), §10 (concurrency,
  auto-deny-in-subagents), §11 (budgets, the 22-test floor), §12 (migration).
- `docs/plan/03-provider-layer.md` §1.1 (`Chat`, non-blocking `Capabilities`), §1.4–1.5
  (`Capabilities`, `ExecutesOwnTools`, `HistoryOwned`, `AcceptsToolSchemas`, `ModelSelection`), §5
  (caching, sticky routing), §6.4 (the `agent.go:102-107` blocker), §11.
- `docs/plan/04-subscription-backends.md` §4.2 (flag mapping), §4.3 (`--permission-mode plan` never;
  "kolk's own `/plan` is chat mode plus a read-only tool set"), §4.4 (six argv behaviours), §6
  (capability matrix), §7.1–7.2 (honest limits, the anti-nagging rule).
- `docs/plan/05-auth-keys-secrets.md` §0 (the napkin), §1.5 (the computed keyless screen).
- `docs/research/ecosystem.md` (2026-08-21) — prior-art survey.

**External** (fetched 2026-08-22/23)
- [agents.md](https://agents.md) — the spec, the monorepo rule, "just standard Markdown".
- Linux Foundation Agentic AI Foundation — `AGENTS.md` donation, December 2025.
- [Claude Code memory](https://code.claude.com/docs/en/memory) — *"Claude Code reads CLAUDE.md, not
  AGENTS.md"*; *"CLAUDE.md content is delivered as a user message after the system prompt"*;
  root-down ordering.
- [Codex AGENTS.md](https://learn.chatgpt.com/docs/agent-configuration/agents-md) —
  `project_doc_max_bytes` 32 KiB, whole-file skip, git-root→cwd.
- [OpenCode agents](https://opencode.ai/docs/agents/) and `packages/opencode/src/agent/agent.ts`,
  `src/tool/plan.ts`, `src/session/reminders.ts` — the agent record, `build`/`plan` differing by
  data, `plan_exit` as a question, the synthetic handoff turn, per-turn reminder injection, and the
  docs/source disagreement about `bash` in plan mode.
- [Gemini CLI plan mode](https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/cli/plan-mode.md)
  and `packages/core/src/policy/types.ts` — `ApprovalMode`, `MODES_BY_PERMISSIVENESS`, per-rule
  `modes[]`.
- [Claude Code permission modes](https://code.claude.com/docs/en/permission-modes) — six modes, the
  `-p` vs terminal default split, plan-as-a-prefix.
- [Anthropic — how we built Claude Code auto mode](https://www.anthropic.com/engineering/claude-code-auto-mode)
  — 0.4% false positives, 17% false negatives.
- Claude Code issues [#5466](https://github.com/anthropics/claude-code/issues/5466) (45👍),
  [#38255](https://github.com/anthropics/claude-code/issues/38255) (37👍),
  [#15721](https://github.com/anthropics/claude-code/issues/15721) (67👍),
  [#46634](https://github.com/anthropics/claude-code/issues/46634) (2👍, closed stale).
- Cline [#9518](https://github.com/cline/cline/issues/9518), [#4848](https://github.com/cline/cline/issues/4848),
  [#10497](https://github.com/cline/cline/issues/10497); Copilot
  [vscode#311893](https://github.com/microsoft/vscode/issues/311893),
  [community#159983](https://github.com/orgs/community/discussions/159983);
  OpenCode [#6781](https://github.com/anomalyco/opencode/issues/6781);
  Goose [#4097](https://github.com/block/goose/issues/4097).
- Kilo [custom modes → agents](https://kilo.ai/docs/features/custom-modes) — *"Orchestrator …
  deprecated: Will be removed; agents with full tool access now support subagents natively."*
- [Roo Code modes](https://roocodeinc.github.io/Roo-Code/basic-usage/using-modes) — sticky models,
  `switch_mode` as a model-requested transition.
- [Amp manual](https://ampcode.com/manual) — low/medium/high/ultra as the *effort* dial.
- [OpenRouter prompt caching](https://openrouter.ai/docs/features/prompt-caching) — implicit-caching
  families, cache read/write multipliers, minimum cacheable prefixes; sticky routing key.
- [arXiv:2602.11988](https://arxiv.org/abs/2602.11988) — *"Evaluating AGENTS.md"*, Gloaguen,
  Mündler, Müller, Raychev, Vechev (ETH Zurich + LogicStar.ai): context files do not generally
  improve success, +20% inference cost.
