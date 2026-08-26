# 4. Subscription backends — Claude Max via the vendor's own CLI

Status: hardened on 2026-08-22 · supersedes: — · PLAN.md item 4

## Decision (the short version)

kolk ships **one** subscription backend in v0.x: registry key **`claude`**, user-visible label
**"Claude Agent"**, package `internal/provider/agentcli`. It spawns the user's **own, unmodified,
self-logged-in `claude` binary** as a session-scoped persistent process using
`--input-format stream-json`, with prompts on stdin, in a cleared environment, and translates the vendor's NDJSON into
`provider.Event`. kolk never sees, stores, proxies, reads or refreshes a credential — and that is
enforced by the *shape* of the code (the login path is an L0 handover with no pipe; the login-state
type has no field an identity can land in; a CI source denylist fails the build on the words) rather
than promised in a README. It implements item 3's `Chat`/`Stream`/`Capabilities` **unchanged**, with
`ExecutesOwnTools:true`, `HistoryOwned:true`, `IdempotentConnect:false`, `ModelSelection:ModelAliasOnly`
— the four fields §8 of `03-provider-layer.md` was cut for.

**In this backend kolk is a frontend and a recorder, not the agent.** Claude Code runs its own tools;
kolk's permission rules, path jail, hardline blocklist and confirm UX do **not** gate them, and the
UI says so on every provider-executed tool line, not once per session. Tokens are reported exactly;
**money is not**. The vendor's `total_cost_usd` reproduces to the last digit from the token counts at
list prices (verified on both committed fixtures), so it is a *counterfactual API-equivalent price*,
not a charge — a Max subscriber's marginal spend is zero. kolk labels it `API-equiv.`, never pools it
with a metered row, and makes `rate_limit_event.utilization` (0.78 of the seven-day window in both
fixtures) the primary cost series for this backend.

**Codex and Antigravity ship on identical bones** (registry keys `codex` ["Codex Agent"] and `antigravity` / `agy` ["Antigravity Agent"]), spawning the user's self-authenticated local CLI (`codex` and `agy`/`antigravity`) via the same `agentcli` adapter without touching credentials. **Gemini never ships as a spawn backend** — Google
names account suspension in the prohibition text and offers no carve-out; it is an API-key backend
through the normal provider path, full stop.

Five things are **cut or deferred** on purpose: the `--permission-prompt-tool` MCP permission bridge
(v0.6 at the earliest, and only if item 16 makes MCP exist for its own reasons),
`--include-partial-messages` token-level streaming (v0.5, gated on a fixture),
inheriting the user's project/local Claude Code settings (v0.5, gated on a per-directory trust
record), fan-out of parallel vendor children (v0.5, gated on a quota guard), and the vendor's
`Task`-based agent mode (v0.5). A smaller feature that is certainly permitted and certainly honest
beats a larger one that is neither.

★ **`PLAN.md` line 108 says a `"claude-code"` provider. That string must not ship** — it is on
Anthropic's own *"Not permitted"* list for product and feature naming. Registry key `claude`, label
"Claude Agent", package `agentcli`.

---

## Spec

### 1. The permitted envelope

#### 1.1 The shape, in one diagram

```
kolk (L6 cli) → engine (L4) → provider.Chat = agentcli.chat          (L3, stdlib only)
                                    │
                                    └─ provider.Spawner (L0 internal/shell)
                                            │  argv: no prompt, no secret, no --bare
                                            │  env : ALLOW-LIST over a CLEARED environment
                                            │  dir : kolk's session cwd (there is no --cwd flag)
                                            ▼
                                    claude 2.1.240  ── reads ITS OWN credentials ──► api.anthropic.com
                                            │           (kolk is not on this wire, ever)
                                            ▼ stdout NDJSON
                                    claude.Translate(line, *State) []provider.Event   ← PURE
```

No HTTP call to Anthropic. No OAuth code. No token. No credential file read. Ever.

#### 1.2 The exact sentences that permit it

All re-fetched **2026-08-22** and quoted verbatim.

> "Nor does it prevent **an end user from signing in to the unmodified Claude Code binary with their
> own Claude subscription**, including where a platform hosts Claude Code as described under *Can
> customers offer Claude Code in their products?* above."
> — [Claude Code — Legal and compliance](https://code.claude.com/docs/en/legal-and-compliance),
> § *Authentication and credential use*

> "To drive the same agent loop from another language, **run the CLI as a subprocess** with the `-p`
> flag and `--output-format json`."
> — [Agent SDK overview](https://code.claude.com/docs/en/agent-sdk/overview)

> "Set `ANTHROPIC_API_KEY` before running it, **because bare mode doesn't use your subscription
> login** […] In bare mode, Claude Code never reads OAuth credentials or the system keychain."
> — [Run Claude Code programmatically](https://code.claude.com/docs/en/headless)
> (⇒ plain, non-`--bare` `claude -p` **does** run on the subscription login. This is why `--bare` is
> forbidden in this backend and belongs only to a future API-key backend.)

**The policy softened in kolk's favour between February and August 2026.** The February text banned
OAuth tokens "in any other product, tool, or service — including the Agent SDK" (quoted verbatim in
[HN 47069300](https://news.ycombinator.com/item?id=47069300), 2026-02-19). The live text replaces
that with a *credential-handling* prohibition plus the affirmative carve-out above. This raises
`docs/research/subscription-auth.md`'s confidence in shape (iii) from **medium-high to high** — and
is itself the argument for the risk note, because a page that changed twice in six months can change
again.

**The empirical argument is stronger than the textual one.** In the January 2026 enforcement wave
Anthropic rejected OAuth-authenticated `/v1/messages` calls with *"This credential is only authorized
for use with Claude Code and cannot be used for other API requests"*, fingerprinting the **request**
— users in [opencode#7410](https://github.com/anomalyco/opencode/issues/7410) bisected it live and
found the discriminator was the **tool names**. In the same hour, on the same subscriptions, a user
in that thread reported: *"Claude Code provider doesn't use OAuth method but directly calls 'claude'
binary"* — and it kept working. Cline ships that shape today (`claudeCodePath`, placeholder
`Default: claude`). OpenCode's token-reuse path was removed under
[legal letters](https://github.com/anomalyco/opencode/pull/18186) in March 2026.
**kolk is proposing the shape that empirically survived the enforcement, not the one that was killed.**

#### 1.3 The exact sentences that prohibit the neighbours

> "Moreover, **developers may not collect, store, or intermediate Claude.ai credentials or session
> tokens** — sign-in to a Claude account must complete through Anthropic's own flow."

> "Anthropic does not permit third-party developers to offer Claude.ai login into their own
> applications, or to route requests through Free, Pro, or Max plan credentials on behalf of their
> users."

> "**Customers may not pay for, resell, or intermediate Claude usage on their end users' behalf.**"

> "**The Claude Code binary must not be modified.** Claude Code must be installed and run as
> published by Anthropic, and customers may not remove, disable, or restrict any authentication
> method built into it (including methods that permit signing in with a Claude account or the user's
> own API key)."

> "Anthropic reserves the right to take measures to enforce these restrictions and may do so
> **without prior notice**."

**The line is credential contact and auth-method restriction, not subprocess spawning.** Concretely:

| Variant | Verdict |
|---|---|
| Spawn `claude`; it reads its own credentials, which kolk never sees | **Permitted** — the carve-out |
| Read `~/.claude/.credentials.json` or the macOS Keychain | **Prohibited** — "collect, store" |
| Implement OAuth PKCE against Anthropic's endpoints | **Prohibited** — "offer Claude.ai login" |
| Run `claude setup-token` and capture its stdout | **Prohibited** — it prints a one-year OAuth token. ★ The sharpest trap: it looks like a CI convenience feature. |
| Set `CLAUDE_CODE_OAUTH_TOKEN` from a value kolk obtained or stored | **Prohibited** — intermediating |
| Set `CLAUDE_CONFIG_DIR` | **Prohibited in effect** — it relocates the credential store |
| Pass `--settings '{"apiKeyHelper":""}'` | **Prohibited** — an affirmative instruction to disable a configured auth method |
| A shared/team/hosted mode where one subscription serves several people | **Prohibited** — enumerated condition |

★ **`03-provider-layer.md` §8.4 (line ~2518) currently mandates
`--settings '{"apiKeyHelper":"", …}'` and must be amended to delete that key.** Blanking
`apiKeyHelper` is exactly "restrict an authentication method built into it", and it also defeats an
organisation's managed policy — which `--safe-mode` preserves *by design* and kolk must not try to
defeat. `"env": null` and the rest of the blob stay. The replacement is **detect and report**: if the
first `system/init` reports `apiKeySource` of `apiKeyHelper` or `/login managed key`, the turn aborts
with `KindPermission` and the message *"your organisation routes Claude Code through an API key; kolk
cannot use a subscription here."*

#### 1.4 The five conditions, each enforced by structure — not promised

| Condition | kolk's binding | Enforced by |
|---|---|---|
| **Binary unmodified; no auth method removed** | spawn from `PATH` as published. Never patch, vendor, repackage or shim. **Never `--bare`.** Never `apiKeyHelper:""`. Every isolation flag that suppresses an auth path is a **user-reversible default** with a line in `kolk doctor`. | `TestArgv_BannedFlags` (`--bare`, `--system-prompt`, `--continue`, `--permission-mode plan`, `setup-token` unreachable in the argv builder); `TestNoVendoredVendor` |
| **No credential contact** | login is `shell.Handover` — inherited stdio, **no pipes, no `io.Reader` in the call**. Login state decodes into a type with no identity field. | `TestHandoverHasNoPipes`, `TestLoginState_HasNoIdentityFields` (reflect-walk), `TestCredentialDenylist` |
| **No pay / resell / intermediate** | single-tenant, one local process, the user's own plan, billed directly to them. No server, no shared mode, no key — structurally absent. | no code exists to test; recorded as a PLAN gate (§1.6) |
| **Branding ≠ "Claude Code"** | key `claude`, label "Claude Agent", kolk's own TUI chrome in every backend, `--append-system-prompt` only. | `TestBrandStrings` (binary string table), `TestArgv_NoImpersonation` (**payload**, not just flag — §1.5) |
| **Honest cost & quota** | `cost_source='vendor_estimate'`, `measurement='estimated'`, rendered `API-equiv.`, never summed with a meter. | `TestTranslate_CostIsVendorEstimate`, dashboard view tests |

#### 1.5 Compliance invariants — the CI tests, listed once

These are the load-bearing ones. Each is a `go test` failure, not a review comment.

| # | Invariant | Test |
|---|---|---|
| **C1** | **Nothing from the vendor reaches disk, the bus or a log unredacted.** `auth_status` frames are dropped unconditionally and never become `EventRaw`. `Event.Raw` / `Usage.Raw` / `Error.Raw` are built from a per-frame field **allow-list**, never a verbatim frame copy. Every byte that can reach `Raw`, `Error.Message` or the stderr tail passes `secret.Redact` (`sk-ant-`, `Bearer `, `access_token`, `refresh_token`, `oauth`, JWT-shaped runs) first. | `TestRedact_NoTokenEscapes` — splice `sk-ant-XXXX` into every field of every fixture frame; assert it appears in no Event, no session round-trip, no stats row |
| **C2** | **The impersonation line is guarded at the payload, not the flag.** The fully-resolved `--append-system-prompt` string (and every `--agents` prompt, if ever used) is grepped for `Claude Code`, `Anthropic's official`, `official CLI for Claude`, `anthropic_spoof`, `anthropic-20250930`. | `TestArgv_NoImpersonation` |
| **C3** | **No credential path exists in the package source**: `.credentials.json`, `auth.json`, `Keychain`, `security find-generic-password`, `secret-tool`, `CLAUDE_CODE_OAUTH_TOKEN`, `setup-token`, `--with-api-key`, `--with-access-token`, `chatgptAuthTokens`, `CLAUDE_CONFIG_DIR`, `app_EMoamEEZ73f0CkXaXp7hrann`. | `TestCredentialDenylist` (source grep; the denylist file is the only permitted occurrence) |
| **C4** | **`agentcli` imports no `os`, no `os/exec`, no `net/http`, no `syscall`.** Adding credential handling does not compile past CI. | `internal/arch/arch_test.go` (already fails on `os/exec`; extend the per-package allow-list) |
| **C5** | **Subscription mode is a five-fact conjunction, asserted mid-stream, failing closed.** `apiKeySource == "none"` alone is *not sufficient* — the vendor's own schema says `"none"` also covers *"a bearer token, or a third-party cloud provider"*. | `TestSubscriptionMode_Conjunction` (32-row truth table) |
| **C6** | **`--verbose` always accompanies `--output-format stream-json`.** Verified live 2026-08-22: without it, exit **1**, **zero stdout bytes**, stderr `Error: When using --print, --output-format=stream-json requires --verbose`. | `TestArgv_VerboseImplied` |
| **C7** | **The env is an allow-list over a cleared environment**, ~11 entries in, ~35 named credential/routing vars asserted out. | `TestEnv_AllowListOnly` |
| **C8** | **The prompt never appears in argv.** | `TestArgv_NoPromptInArgv` |

```go
// SubscriptionMode reports whether THIS turn is billed to the user's own claude.ai
// subscription. All signals must agree. apiKeySource=="none" alone would tell a user
// billing an enterprise Bedrock account that they are on their Max plan.
func SubscriptionMode(init *InitFrame, auth LoginState, envCleared bool, res *ResultFrame) BillingMode {
	if init == nil {
		return BillingUnknown // ★ a stream that terminated with no system/init is
	}                         //    UNKNOWN, recorded and warned — never assumed subscription
	switch {
	case init.APIKeySource == "apiKeyHelper", init.APIKeySource == "/login managed key":
		return BillingVendorAPIKeyManaged
	case init.APIKeySource == "ANTHROPIC_API_KEY":
		return BillingVendorAPIKey
	case init.APIKeySource == "none" &&
		auth.LoggedIn && auth.AuthMethod == "claude.ai" &&
		auth.APIProvider == "firstParty" && auth.Subscription != "" &&
		envCleared &&
		(res == nil || res.AllModelProvidersAre("firstParty")):
		return BillingSubscription
	}
	return BillingUnknown
}
```

`BillingVendorAPIKey*` at `system/init` **aborts the turn** at `PhaseConnect` with
`KindPermission` and *"kolk will not bill your API account when you asked for your subscription — run
`kolk doctor claude`"*. Nothing had been published, so aborting is safe; billing the wrong account
silently is not. The mode is recorded on every span as `billing_mode`.

#### 1.6 Distribution gate — a human action, not a code gate

The Commercial-ToS trigger is *"**preinstalling or running** Claude Code in your products or
services"*. kolk does not preinstall; it does run. The conservative reading — comply with all
conditions regardless — is nearly free and is adopted. But two things cannot be asserted by a test
and are therefore recorded as acceptance criteria on PLAN.md item 4:

1. **The project owner has reviewed Anthropic's Commercial Terms and recorded a decision before any
   release tags this backend.**
2. **The release checklist re-fetches `code.claude.com/docs/en/legal-and-compliance` and compares a
   section hash stored in `docs/research/subscription-auth.md`.** A changed hash blocks the release
   until the risk note is re-read. `kolk doctor claude` prints the verification date, so staleness is
   visible to the user and not only to the maintainer.

**Any hosted, bundled or preinstalled distribution of kolk-with-`claude` is a separate decision**
requiring Commercial ToS review. It is never inherited from v0.x. The local, free, OSS,
user-installs-both-binaries path is what ships.

---

### 2. The Go design

Layer **L3**. Stdlib only; **no `os/exec`**, no `os`, no `net/http`, no `syscall`, no environment
read. Every side effect arrives through an injected port. `internal/arch/arch_test.go` makes each of
those a CI failure.

```
internal/provider/agentcli/
├── agentcli.go        chat · Stream · Close · the pump · env allow-list · redaction   (side effects)
├── vendor.go          the per-CLI DATA table — two values will ever exist
├── claude.go          Translate + State + argv builder                                ★ PURE
├── codex.go           v0.4; absent from v0.x
├── frames.go          tolerant per-frame decode structs (no DisallowUnknownFields anywhere)
├── detect.go          PURE: (exitCode, statusJSON) → LoginState
├── caps.go            the compiled floor + the CapProbe upgrade
├── translate_test.go  replays spec/testdata/foreign/** — offline forever, no binary, no account
├── argv_test.go       golden argv · env allow-list · the eight refusal assertions
└── denylist_test.go   the credential + impersonation source/payload greps
```

#### 2.1 The vendor bundle: data plus three pure functions

```go
// vendor is one agent CLI, expressed as data plus pure functions. claude.go and
// (at v0.4) codex.go each declare exactly one value. agentcli.go owns every side
// effect there is; the tripwire for having put the seam in the wrong place is any
// `switch v.Bin` outside vendor.go.
type vendor struct {
	Key   string // registry key: "claude". NEVER "claude-code".
	Label string // user-visible: "Claude Agent". Branding rule, §1.4.
	Bin   string // "claude"

	MinVersion  [3]int   // ★ 2.1.205 — below this, --safe-mode/--effort do not exist
	VersionArgv []string // {"--version"}          — free, exit 0, prints "2.1.240 (Claude Code)"
	AuthArgv    []string // {"auth","status","--json"} — free, documented quota-free
	LoginArgv   []string // {"auth","login"}        — run through Handover, never Spawn

	EnvAllow []string
	Argv     func(*provider.Request, Options) (SpawnPlan, []provider.Warning, error) // PURE
	Detect   func(stdout []byte, exitCode int) LoginState                            // PURE
	Translate func(line []byte, st *State) []provider.Event                          // PURE
	Floor    func(model string) provider.Capabilities
}

// SpawnPlan is the fully-resolved description of one child process. Building it is
// pure; executing it is chat.Stream's only side effect.
type SpawnPlan struct {
	Args   []string // argv[1:]. The prompt is NEVER here (C8).
	Stdin  []byte   // the prompt, and nothing else.
	Dir    string   // kolk owns the working directory. `claude` has NO --cwd flag:
	                // the PROCESS cwd is the working root. Verified on 2.1.240.
	Env    []string // ALLOW-LIST over a cleared environment (C7).
	Cancel provider.CancelPolicy
	Assert InitAssertions // what the first system/init MUST report
}
```

#### 2.2 `Chat` — three methods, no widening

```go
type chat struct {
	v    vendor
	sp   provider.Spawner
	clk  provider.Clock
	tm   provider.Timeouts
	opt  Options            // resolved by internal/cli from flags > env > project > user

	// Process-scoped memos. NOT conversation state, so rule 1 ("nothing that varies
	// per session may live on the Chat value") holds: these vary per MACHINE.
	once   sync.Once
	path   string
	ver    [3]int
	login  LoginState
	perr   *provider.Error

	mu     sync.RWMutex
	probed map[string]provider.Capabilities // key: model + "@" + version (CapProbe tier)
}

var _ provider.Chat = (*chat)(nil)

// Stream: five preflight checks, one spawn, one wrap. Everything before the spawn is
// PhasePreflight — nothing happened upstream and L4 may safely retry. Everything after
// is PhaseConnect, and L4 must NOT connect-retry: Capabilities.IdempotentConnect is
// false because system/init mutates vendor-side session state (Decide rule R2).
func (c *chat) Stream(ctx context.Context, req *provider.Request) (provider.Stream, error) {
	path, ver, err := c.resolve(ctx)                 // LookPath + `--version`, memoised, free
	if err != nil {
		return nil, err                              // KindBackendMissing, PhasePreflight
	}
	if ver.Less(c.v.MinVersion) {                    // ★ THE VERSION GATE
		return nil, &provider.Error{Kind: provider.KindBackendMissing, Phase: provider.PhasePreflight,
			Backend: c.v.Key, Message: fmt.Sprintf("claude %s is too old for kolk (needs >= %s) — upgrade it",
				ver, c.v.MinVersion)}
	}
	if st := c.loginState(ctx); !st.LoggedIn {
		return nil, &provider.Error{Kind: provider.KindBackendLogin, Phase: provider.PhasePreflight,
			Backend: c.v.Key, Message: "not signed in — run: kolk login claude"}
	}

	plan, warns, err := c.v.Argv(req, c.opt)         // PURE; golden-tested; validates --effort
	if err != nil {                                  // and REFUSES an empty rendered prompt
		return nil, err                              // KindInvalidRequest, PhasePreflight
	}

	proc, err := c.sp.Spawn(ctx, provider.SpawnCmd{
		Path: path, Args: plan.Args, Dir: plan.Dir,
		Env: plan.Env, Stdin: plan.Stdin, Cancel: plan.Cancel, StderrRing: 64 << 10,
	})
	if err != nil {
		return nil, spawnFailed(c.v, err)            // KindTransport, PhaseConnect
	}
	return newStream(c.v, proc, plan, c.clk, c.tm, warns), nil
}
```

#### 2.3 The `Capabilities` value, every field, with its source

`Capabilities` must not block on the network and must not spawn (item 3 §1.1). It answers from a
compiled **`CapPreset` floor**, upgraded to **`CapProbe`** from the first `result` frame, cached on
`(model, vendorVersion)`.

```go
func floor(model string) provider.Capabilities {
	return provider.Capabilities{
		Backend: "claude", Model: model,

		Streaming:        provider.Yes,     // observed in both fixtures
		Tools:            provider.Yes,     // it HAS tools — they are just not yours.
		                                    // CapProbe: len(init.tools) > 0 (30 with --safe-mode)
		ParallelTools:    provider.Unknown, // the vendor batches; UNOBSERVED. Unknown is a
		                                    // legitimate answer (§1.5) and beats a coin flip.
		ToolChoice:       provider.No,      // no flag exists
		Vision:           provider.Yes,     // --file, and image blocks in tool results
		StructuredOutput: provider.Yes,     // ★ CORRECTS §8.2 (`No`). --json-schema exists, so do
		                                    // result.structured_output and the dedicated failure
		                                    // subtype error_max_structured_output_retries.

		Reasoning: provider.ReasoningSupport{ // ★ CORRECTS §8.2 (ReasoningNone + WarnEffortDropped).
			Style:   provider.ReasoningEffort, // `--effort <level>  Effort level for the current
			Efforts: []provider.Effort{        //  session (low, medium, high, xhigh, max)`
				provider.EffortMax, provider.EffortXHigh, provider.EffortHigh,
				provider.EffortMedium, provider.EffortLow},
			Default: provider.EffortHigh,   // vendor default. NOT read from init: `effort` is
		},                                  // Remote-Control-only and is ABSENT from every -p
		                                    // stream — verified against both fixtures.
		Cache: provider.CacheSupport{Mode: provider.CacheModeNone}, // the child manages its own;
		                                    // 12 327 / 9 693 1h-cache writes it placed itself

		MaxInputTokens:  200_000,  // CapPreset floor → 1_000_000 from result.modelUsage[m].contextWindow
		MaxOutputTokens:  32_000,  // CapPreset floor →    64_000 from result.modelUsage[m].maxOutputTokens
		                           // ★ §5.7 rotation filter #4 is unevaluable without these, and
		                           // MaxInputTokens is what lets the engine fail a 4 MB tool result
		                           // LOCALLY instead of writing an unsendable message to disk.

		UsageReported: provider.Yes,               // ★ CORRECTS §8.2 (`No` — "no per-token counts,
		CostSource:    provider.CostVendorEstimate,//   ever"). modelUsage[k] carries EXACT
		Measurement:   provider.MeasureEstimated,  //   inputTokens/outputTokens/cacheRead/cacheWrite.
		                                           //   Only the MONEY is an estimate — which is
		                                           //   exactly why these are two separate axes.

		// ── the backend-shape facts. Declared, never inferred from a silent zero.
		ExecutesOwnTools:    true,   // the premise; the engine's tool branch is unreachable here
		AcceptsToolSchemas:  false,  // --allowedTools takes NAMES and rule strings
		HistoryOwned:        true,   // only the newest user turn goes over; continuity is --resume
		AcceptsFallbackList: true,   // ★ CORRECTS §8.2 (`false`). `--fallback-model` help, verbatim:
		                             //   "Accepts a comma-separated list to try each in order."
		EchoesReasoning:     false,  // we never resend history, so nothing to round-trip
		IdempotentConnect:   false,  // ★ system/init mutates vendor state → Decide R2
		ModelSelection:      provider.ModelAliasOnly, // alias OR full id; this is also what
		                             // disables rotation via the §5.7 filters, so
		                             // Policy.MaxRotations = 0 needs no config field nobody reads.

		Pricing: provider.Pricing{}, // ALL ZERO, Free:false, deliberately. §5.8 forbids re-pricing a
		                             // CostVendorEstimate row; a populated Pricing would only tempt
		                             // a call site into computing a second, contradictory number.
		Source: provider.CapPreset,
	}
}
```

**Six rows correct `03-provider-layer.md` §8.2.** `UsageReported: Yes` is the most consequential:
`modelUsage["claude-opus-5"]` in the tool-use fixture carries
`inputTokens 4 · outputTokens 275 · cacheReadInputTokens 47 060 · cacheCreationInputTokens 9 693`.
Those are exact. `total_cost_usd` then reconstructs from them to the last digit at Opus-5 list rates
on **both** fixtures (0.1313315 and 0.1273550), which proves it is a local table lookup, not a
measurement. **Tokens metered, money estimated.** Keeping `No` would blind item 17 to the one thing
this backend reports accurately.

`StructuredOutput: Yes` is honest as a capability row, but v0.x config **does not route the planner
role here by default** — the shape of `result.structured_output` under `stream-json` is unverified,
and an `error_max_structured_output_retries` burns subscription quota to discover something a 2¢
fixture will settle. That is a config default, not a capability lie.

#### 2.4 The pump — `Stream`, single-goroutine, and the eleven rules that make it safe

```go
type stream struct {
	v    vendor
	proc provider.Proc
	rd   *bufio.Reader // 1 MiB buffer, HARD 16 MiB per-line cap.
	                   // NEVER bufio.Scanner: its 64 KiB ErrTooLong surfaces as a
	                   // mystifying transport error the first time a tool_result
	                   // carries a large file read.
	st   State
	q    []provider.Event
	cur  provider.Event
	err  *provider.Error
	done bool
	clk  provider.Clock
}

var _ provider.Stream = (*stream)(nil)
```

**No goroutines and no channels in L3.** The cancel ladder and the stderr drain both live in L0
behind `SpawnCmd.Cancel` (data, not a callback) and `Proc.StderrTail()`. This is item 3 §1.1's
contract verbatim (*"Every method is called from ONE goroutine, the caller's. There is no internal
pump and no channel"*) and §8.1's *"No goroutine"* — and it is not stylistic: L0 already owns `Wait`,
so it can serialise signal-vs-reap under one lock. An L3 watcher goroutine calling `Proc.Signal`
concurrently with the read path's `Proc.Wait` can deliver SIGKILL to a **recycled PID** and kill an
unrelated process on the user's machine.

| # | Rule | Why |
|---|---|---|
| **P1** | **Only newline-terminated lines are translated.** `bufio.Reader.ReadString('\n')` returns partial data *together with* `io.EOF` (verified in Go). On EOF with a non-empty unterminated remainder: discard it, record its byte length in a `Warning`, jump straight to the terminal path. | Otherwise a half-written frame from a SIGKILL becomes `KindTransport`, overwriting the honest `KindCanceled`/`KindTruncated` and losing `KeepPartial` and the exit code |
| **P2** | **Exit status is observed on the READ path.** EOF with `!st.SawResult` → `Proc.Wait()` **there**, then `EventUsage`(all nil, `CostUnknown`, `MeasureUnknown`, local TTFT/Elapsed) + `EventError{KindTruncated, Message:"claude exited N: <redacted stderr tail>"}`. | `defer s.Close(); return r, s.Err()` evaluates return operands **before** the deferred call, so a crashed child reports a successful empty turn (§8.1's own warning) |
| **P3** | **The terminal event is never overwritten.** Once the terminal path builds `s.q`, the read loop does not assign `s.q = evs` again. | Angle A's sketch had exactly this bug: the EOF branch's terminal events were discarded two lines later |
| **P4** | **Non-JSON stdout lines are tolerated at ANY position, on a budget** — 16 lines / 64 KiB per stream, each a deduped `WarnUnknownFrame`. Only exceeding the budget is `KindTransport`. | npm/bun/nvm/corepack shims print `ExperimentalWarning` and `npm notice` **mid-stream**, not only at startup. A pre-first-brace-only exception kills healthy turns after the vendor has already edited files |
| **P5** | **Per-line cap failure is non-fatal.** Over 16 MiB: discard to the next newline, `WarnToolCallDropped`, **continue**. The vendor already ran that tool and the turn is still valid. Only an over-cap on the `result` frame degrades — to `KindTruncated` with a nil usage row, never `KindTransport`. | `ReadString` is otherwise unbounded (an OOM, not an error); a hard failure at the `result` frame destroys the usage row and the session handle at the worst possible moment |
| **P6** | **Do not kill on `result`.** Emit the terminal event, then **drain without translating** on a bounded 3 s deadline, then walk the cancel ladder against the process **group**. L0 closes the stdout **read end** once the ladder completes. | The vendor drains queued output for up to 30 s if the consumer is slow (truncating large responses if killed early); and a background `Bash` grandchild inherits the stdout **write end**, so an unbounded drain never sees EOF and hangs the CLI forever after a complete answer |
| **P7** | **Two watchdogs.** `Timeouts.Idle` (120 s, armed only around the blocking read, reset by **every** frame including hooks, `keep_alive`, `tool_progress` and unknown frames) **and** a new `Timeouts.Turn` wall-clock deadline (30 min default, one `time.AfterFunc` at spawn). Both fire `context.WithCancelCause(errStalled)` → `KindStalled`, never `KindCanceled`. | `tool_progress` heartbeats every 30 s keep the idle watchdog alive **forever** behind a hung `npm install`; `--max-budget-usd` is spend-based and `--max-turns` is round-based, so neither bounds wall time. Today `Timeouts` has no wall-clock field at all |
| **P8** | **`result.result` is the runtime fallback for the message body**, not merely a test assertion. If the assembled text differs from `result.result`, **adopt `result.result`** and emit `WarnUnknownFrame`. | §1.8's empty-turn backstop requires "no text AND no tool calls AND no reported output tokens" — it cannot fire when the vendor moves content to a frame kolk skips, because `modelUsage.outputTokens` is non-zero. Without P8, a vendor upgrade silently produces successful *empty* turns that are appended to the session and fed back to the model |
| **P9** | **A provisional `EventResponseMeta` is emitted immediately after `EventStart`**, carrying the *requested* model; `system/init` then **corrects** it (§1.3 already permits `ResponseMeta` to repeat). | `system/init` is documented as arriving *"normally"* first — not always. **Verified live 2026-08-22: an empty prompt produces 8 hook frames, then exit 1, with no `init` and no `result` at all.** Without P9 a pre-init content frame yields text with no model attributed |
| **P10** | **`EventUsage` is N ≥ 1 on every terminal path.** One row per `modelUsage` key — **and exactly one all-nil row when `modelUsage` is `{}`.** | ★ Verified live: an error result carries `"modelUsage": {}`, so "one row per key" alone emits **zero** rows and breaks the invariant that no call is invisible to the dashboard |
| **P11** | **stderr is drained continuously by L0 into a bounded 64 KiB head+tail ring**, passed through `strings.ToValidUTF8` and `secret.Redact`, never parsed, never classified; attached to `Error.Message` only on an error terminal. | A full stderr pipe **deadlocks the child**, and the vendor is chatty there (`--effort` warnings, `--add-dir` warnings, `[claude-code:unrecognized_model]`) |

#### 2.5 Cancellation — SIGINT first, and why

Vendor semantics from the headless docs, both confirmed:

| Rung | Signal | Vendor behaviour | kolk's reason |
|---|---|---|---|
| 1 | `SIGINT`, grace **5 s** | *"To end the turn instead, send SIGINT."* The turn ends gracefully and **a `result` frame is still produced**. | ★ A cancelled turn is still **accounted** — `modelUsage` arrives, so Ctrl-C is not a hole in the dashboard, and `Finish.ProviderState` is publishable |
| 2 | `SIGTERM`, grace **2 s** | exit **143**; the turn is left **unfinished with no result recorded**; kills the `Bash` process tree; runs `SessionEnd` hooks. | Last chance at a clean teardown |
| 3 | `SIGKILL` | — | — |

★ **THE RESULT FRAME IS THE ONLY AUTHORITY FOR CONTINUITY.** `ProviderState` may be published only by
a terminal path that actually observed a `result`. **A SIGTERM/SIGKILL exit invalidates the vendor
session**, because the vendor documents that *resuming continues the unfinished turn* — so a
`--resume` after SIGTERM makes the vendor silently execute the tool calls kolk told the user were
cancelled, editing files after a "cancelled" turn, and permanently diverging kolk's transcript from
the vendor's. On a SIGTERM/SIGKILL exit the next turn mints a fresh `--session-id` and replays kolk's
own retained transcript as a labelled `<prior-conversation>` prelude with `WarnHistoryLost`.

`KindStalled` skips rung 1 — 120 s of total silence will not answer a SIGINT. Ctrl-C is
`KindCanceled`, `KeepPartial:true`, `Silent:true`, exit 130 (Decide R0).

#### 2.6 The item-3 amendment PR — a prerequisite of M11

All three designs independently discovered the same list, which is strong evidence it is real. It
lands **once, deliberately, before M11**, so `03-provider-layer.md` and the code never disagree.

| # | Amendment | Why |
|---|---|---|
| **A1** | **`EventUsage` becomes N ≥ 1**, not "exactly one". §1.1 and §1.4 currently contradict each other (§1.4 says *"a turn may produce several: `claude -p`'s result frame carries a per-model breakdown"*). Reword §1.1: *"at least one `EventUsage` on every terminal path; never zero; a turn may carry several, one per model."* | Merging Opus-for-the-loop with Haiku-for-compaction makes item 17's leaderboard wrong, which §1.4 already says |
| **A2** | **`Warnings` may also ride an `EventResponseMeta`**, which §1.3 already permits to repeat. Zero type changes; one doc-comment sentence. | agentcli produces genuine **mid-stream** warnings (`api_retry`, quota warnings, `compact_boundary`, `model_refusal_fallback`, hook failures). Flushing at `EventFinish` means a Max user learns they are at 78 % of the weekly window *after* the turn |
| **A3** | **`provider.Error` gains `ProviderState json.RawMessage`**, and `Collect` prefers `Finish.ProviderState`, falling back to it. | Today `ProviderState` is representable **only on `Finish`**, so every *error* terminal — the common case, after the vendor already edited files — permanently loses the session handle. kolk mints the uuid pre-spawn, so it is populatable on every path |
| **A4** | **`provider.Message` gains `ToolsExecutedBy string`**, plus a request-builder rule that is the sibling of the existing `ReasoningModel` rule (invariant (c)): **any message whose tool calls were provider-executed by a different backend is flattened to prose** (`[Claude Agent ran Bash: go test ./… → ok]`) before it is sent. | Switch from `claude` to OpenRouter mid-session and the history contains `TodoWrite`/`Task` tool calls kolk has no schema for. The model calls them again → unknown-tool loop, or the provider rejects the message array outright |
| **A5** | **`Timeouts` gains `Turn time.Duration`** (0 = off). | P7 |
| **A6** | **`provider.Proc` gains `Signal(Sig) error`** (group-directed, `SigInterrupt`/`SigTerminate`/`SigKill` — a small L3 enum so no `syscall` crosses the layer); **`SpawnCmd` gains `Cancel CancelPolicy`** (data, inspectable in a table test) **and `StderrRing int`**; **L0 writes `Stdin` asynchronously, ignores `EPIPE`, closes the pipe immediately, and its write error may never outrank the exit code + stderr in classification**; **L0 closes the stdout read end once the ladder completes**; **L0 gains `Handover(ctx, SpawnCmd) error`** — inherited stdio, **no pipes**, documented as *"a Handover'd command's output is never available to kolk."* | §2.5, P6, P11, §5. Without the async stdin write, a 200 KB prompt against a child that exits on a bad flag blocks past the 64 KiB pipe buffer and reports "broken pipe" as the cause, discarding the real diagnosis |
| **A7** | **Three new closed `Warn*` codes**: `WarnUnknownFrame`, `WarnQuotaWarning`, `WarnHistoryReplayed`. | Silence is worse than three constants |
| **A8** | **§8.2's six capability rows corrected** (`StructuredOutput`, `Reasoning`, `AcceptsFallbackList`, `UsageReported`, `MaxInput/OutputTokens`); **§8.1's spawn line gains `--verbose`**; **`system/api_retry` stops being an `EventError`**; **the `{"type":"error"}` row is deleted** (no such member exists in the vendor's 42-member union); **§8.4's `apiKeyHelper:""` is deleted** and its `auth status --json` field list corrected. | §1.3, §2.3, §3 |
| **A9** | **arch §5 rule 3 is narrowed.** It names `agentcli` as the one package outside `engine/events.go` allowed to construct a `protocol.Event`. **It must not** — like every adapter it returns `provider.Event`. §2's tree comment (*"vendor NDJSON → protocol.Event"*) needs the same edit. | The exception is unnecessary and would be the second path to a UI that §1.3 forbids |
| **A10** | **Spec deltas**: `tool.requested`/`tool.started`/`tool.output`/`tool.finished` gain `executor: "kolk"\|"provider"`; `spec/stdio.md` states that `tool.requested{executor:"provider"}` **has already executed** and will never be preceded by a `permission.requested`. `usage.reported` gains `measurement`, `billing_mode`, `executor`. | Additive, and arch §7 already mandates clients ignore unknown fields. Decide it now, while the vocabulary is being written — otherwise the first GUI client renders an approve/deny affordance for an action that completed minutes ago |

---

### 3. Vendor frame → `provider.Event`

`claude.Translate(line []byte, st *State) []provider.Event` — **pure**: no clock (the stream stamps
`st.Now` from the injected `Clock`), no I/O, no goroutine, no package-level state. Replaying a fixture
twice with the same seed `State` yields `reflect.DeepEqual` slices.

**Legend:** ✅ present in the committed fixtures and pinned by a test today · ◆ **verified live at
zero quota this session** · ○ schema-known, synthesised fixture · ⬚ deferred to v0.5.

#### 3.1 Frames the fixtures contain

| # | Frame | Events | Notes |
|---|---|---|---|
| **T1** | *(synthetic, pre-read)* | `EventStart{Warnings}` then a **provisional** `EventResponseMeta{Model: req.Model}` | Exactly one `EventStart`, first, carrying the argv-time drop list. P9. |
| **T2** ✅ | `system/hook_started` ×4 | **none**; resets both watchdogs | Both fixtures open with 4 of these **before `system/init`** — an adapter asserting `init` is frame #1 breaks on a real machine. Not content, not usage, not failure. |
| **T3** ✅ | `system/hook_response{outcome:"success"}` ×4 | **none** | All four `started` precede all four `response` — hooks run in **parallel**; pair by `hook_id`, never by adjacency. |
| **T4** ○ | `system/hook_response{outcome:"error"\|"cancelled"}` | **none** + `Warning{WarnParamDropped, Field:"hook:"+name, Detail:"exit N"}` | A *user's* hook failing is the user's problem; the vendor continues. **Never** an `EventError`. |
| **T5** ✅◆ | `system/init` (first) | `EventResponseMeta{Model:.model, ProviderName:"claude", Attempt:1}`; **assert `plan.Assert`** (§1.5 C5) | `st.SessionID = .session_id` is held for `Finish.ProviderState`. `st.Version = .claude_code_version` goes on every span. ★ **`GenID` stays EMPTY** — §1.3 reserves that namespace for a *gateway* generation id and `Catalog.Reconcile` is `ErrUnsupported` here; a vendor uuid there is the exact collision §1.2 warns about. |
| **T6** ○ | `system/init` **re-emitted** | `EventResponseMeta` again **iff `.model` changed** | The vendor emits init at the start of each turn and logs `(re-emit)`. Init is **not** once-per-process; `!st.SawInit` must never be an assertion. |
| **T7** ✅ | `assistant`, `content[i].type=="text"` | `EventTextStart{ID}` → `EventTextDelta{ID,Text}` → `EventTextEnd{ID}` | ★ **`ID = message.id + "#" + itoa(i)`.** Consecutive assistant frames legitimately share `message.id` (one frame per completed content block); collapsing on it merges two blocks into one. Whole-block delivery in v0.x. |
| **T7e** ◆ | `assistant` with **`is_api_error_message: true`** | **no text events**; the text is held for the terminal `EventError` | ★ **Verified live**: an unknown model produced `assistant{error:"model_not_found", is_api_error_message:true}` whose `content[0].text` is the *error prose*. Without this rule kolk renders an API error as the model's answer, then emits `EventError` after it. |
| **T8** ✅ | `assistant`, `content[i].type=="tool_use"` | `EventToolCall{ProviderExecuted:true, Tool:{ID:.id, Type:"function", Function:{Name:.name, Arguments:<RAW .input bytes>}}}` | `st.Pending[.id] = .name`. **No `EventToolInputStart`/`Delta`** — arguments arrive complete. `Arguments` is the **raw JSON bytes**, never unmarshalled-and-remarshalled (§1.2 invariant (a) generalises). The fixture's `caller:{type:"direct"}` and every other unknown block key is dropped. |
| **T9** ○ | `assistant`, `content[i].type=="thinking"` | `EventReasoningStart/Delta/End` | `Finish.ReasoningDetails` stays **nil** — the child owns its own continuity, and echoing a signed thinking block to a different upstream is §1.2(c)'s permanent 400. |
| **T10** ✅ | `assistant` → `message.usage` | **DROPPED, deliberately** | ★ **The finding that changes the adapter.** Summing per-frame usage over the tool-use fixture gives output **24**; `result` says **275** — 11.5× low. The vendor documents it: *"`message.usage` is not final — the turn's total usage arrives on the result message."* The plain fixture hides the bug (4 vs 4, single frame). **`result` is the only accounting frame.** |
| **T11** ✅ | `assistant` → `message.stop_reason` | **none** | `null` on **every** frame in both fixtures, including the last. |
| **T12** ✅ | `user`, `content[j].type=="tool_result"` | `EventToolResult{CallID:.tool_use_id, Name:st.Pending[CallID], Output:<content>, IsError:.is_error}` | The vendor ran the tool. **`Name` must be recovered from `st.Pending`** — the frame does not carry it, and an empty `Name` renders as an anonymous blob. Unknown id → `Name:""` + `Warning{WarnToolIDRewritten}`; **never drop the event**. `content` may be a **string** (as in the fixture) **or an array of blocks** — both flatten to the same `Output`. |
| **T13** ✅ | `user` → sibling `tool_use_result` | **none** (allow-listed into `Raw` under `IncludeRaw`) | Per-tool structured shape (`{stdout,stderr,interrupted,isImage,noOutputExpected}` for Bash). `provider.ToolResult` has no slot, and typing 30 per-tool shapes inside L3 is the coupling §8.5 warns against. |
| **T14** ○ | `user` with `isSynthetic:true` | **none** | CLI-injected, not model output. |
| **T15** ✅ | `rate_limit_event{status:"allowed"}` | **none**; `st.RateLimit = info` | Neither content nor error (fixtures README finding #2). |
| **T16** ✅ | `rate_limit_event{status:"allowed_warning"}` | **none** + `Warning{WarnQuotaWarning, Was:"seven_day", Detail:"utilization 0.78, resets <t>"}` on the next `EventResponseMeta` (A2) | ★ Both fixtures carry exactly this, **interleaved mid-turn** (frame 10 of 12; mid-tool-loop in the other), not at the end. Silence here means a Max user hits a weekly wall with no prior signal from kolk. **Also writes a `RateAccount` cooldown when `utilization` crosses threshold** (§7). |
| **T17** ○ | `rate_limit_event{status:"rejected"}` | **none directly**; `st.Rejected = info`; **writes `Cooldowns.Mark("key:claude:plan", resetsAt)`** | A *cause* delivered before its effect: it **classifies the subsequent terminal frame** into `Error{Kind:KindQuotaExhausted, RateScope:RateAccount, ResetAt:time.Unix(resetsAt,0), Retryable:false}`. §4.2's row (no rotation, cooldown until `ResetAt`) is already exactly right. `errorCode:"credits_required"` ⇒ surface, **never retry**. |
| **T18** ✅ | `result{subtype:"success", is_error:false}` | **`EventUsage` × N** → **`EventFinish`** | Usage strictly before Finish. `Finish.ProviderState = {"backend":"claude","session_id":st.SessionID,"cli_version":st.Version,"argv_hash":…}`. |
| **T19** ◆ | `result{subtype:"success", **is_error:true**}` | `EventUsage` × N (**N=1, all-nil, when `modelUsage` is `{}`**) → **`EventError`** | ★ **Verified live, not merely schema-derived**: `{"subtype":"success","is_error":true,"terminal_reason":"api_error","api_error_status":404,"total_cost_usd":0,"modelUsage":{},"result":"There's an issue with the selected model…"}`, exit 1. **Branch on `is_error`, never on `subtype` alone**, or every failed turn is recorded as a success. |
| **T20** ○ | `result{subtype: error_during_execution \| error_max_turns \| error_max_budget_usd \| error_max_structured_output_retries}` | `EventUsage` × N → `EventError` | These carry `errors: []string` and **no `result` field** — so `result.result` must never be read unconditionally. `error_max_turns` → `KindTruncated` + **`ActContinue`** (a partial answer, not a failure); `error_max_budget_usd` → `KindBudgetExhausted`; the rest → `KindServer` / `KindInvalidRequest`. |

#### 3.2 Frames not in the fixtures that the adapter must handle from day one

| Frame | Events | Note |
|---|---|---|
| `system/api_retry` | **none** + `Warning`; resets watchdogs | ★ **§8.1's mapping to `EventError{KindOverloaded, PhaseCommitted}` is wrong and is amended (A8).** `EventError` is folded by `Collect` into a non-nil error and drives `Decide` — it **ends the turn**. But the vendor is retrying *itself* (`attempt`, `max_retries`, `retry_delay_ms`) and usually succeeds. As specified, a transient 529 aborts a turn that would have completed. |
| `system/permission_denied` | `EventToolResult{CallID, Name:.tool_name, Output:.message, IsError:true}` + `Warning{WarnToolCallDropped}` | Renders the denial attached to the tool, where the user expects it. Under bare `-p`, **denials are how the permission system speaks**: an "ask" decision with no `--permission-prompt-tool` is a terminal denial. `result.permission_denials[]` is the authoritative count; this frame is the live UX. |
| `system/compact_boundary` | **none** + `Warning{WarnHistoryTruncated, Detail:"compacted N→M (<trigger>)"}` | The vendor discarded **its own** history. Pre-declared by `HistoryOwned`, but `/rewind` and the user both need the line. |
| `system/model_refusal_fallback` | `EventResponseMeta{Model:.fallback_model, Attempt:n+1}` + `Warning{WarnModelRotated}` | The server-side fallback firing. `scope:"session"` updates `st.Model`; `"local"` does not. This is what makes `AcceptsFallbackList:true` safe — without it the UI keeps naming a model that did not answer. |
| `system/model_refusal_no_fallback` | `EventError{KindRefusal}` on the terminal path | §4.2: never retry, never rotate. |
| `system/thinking_tokens` | **none** | Vendor: *"not the authoritative billed output_tokens."* Feeding it into `Usage.ReasoningTokens` fabricates a metered count. Spinner only. |
| `tool_progress` | **none**; resets the idle watchdog | ★ The liveness signal during a five-minute `Bash` call (30 s heartbeat). Ignoring it kills healthy turns; **honouring it alone** is why P7's wall-clock deadline is also required. |
| `keep_alive` | **none**; resets watchdogs | Vendor: *"receivers must ignore it."* |
| `system/task_*`, `background_tasks_changed` | **none** in v0.x | Vendor subagent lifecycle — the natural home for item 14's `subagent.started/finished`, but not until the engine can represent a **foreign** subagent tree. `task_notification.usage` allow-listed into `Raw`. |
| `system/informational` | `Warning` when `level ∈ {warning, suggestion}`, **surfaced immediately at `level:"warning"`** (§3.5) | Hook block reasons arrive here, **not** as errors. |
| `stream_event` | ⬚ v0.5 | Only with `--include-partial-messages`, which v0.x does not pass (§4.3). |
| `assistant` with `aborted:true` | contributes `Message.Truncated = true` | *"stop_reason was never received and the content may end mid-word"* — exactly §1.2's `Truncated`. |
| `assistant` with `supersedes:[uuid…]` | emit the replacement normally + `Warning{WarnToolCallDropped, Detail:"vendor retracted N earlier messages"}`; uuids allow-listed into `Raw` | ⚠ **No clean answer, and this doc does not pretend otherwise.** The vendor asks consumers to *evict* named earlier messages; kolk's bus is append-only with monotonic seq (arch §7), so retraction is structurally unrepresentable and evicting would corrupt the replay log for every attached client. **Decision: do not evict; record and warn.** Flagged to item 19 as an open protocol question. |
| **`auth_status`** | **DROPPED UNCONDITIONALLY. Never `EventRaw`, never `Raw`.** `.error != ""` → `EventError{KindBackendLogin}` with a **fixed** message, not the frame's prose. | ★ **C1.** It carries `{isAuthenticating, output[], error?}` — an `output[]` array from an auth flow is precisely where a bearer or OAuth token would appear, and `IncludeRaw` would write it to `~/.local/state/kolk/sessions/*.json`, publish it to every attached protocol client and hand it to SQLite. |
| `bash_command`, `prompt_suggestion`, `conversation_reset`, `command_lifecycle`, `tool_use_summary`, `system/{status, local_command_output, commands_changed, session_state_changed, memory_recall, files_persisted, mirror_error, elicitation_complete, plugin_install, worker_shutting_down, control_request_progress, code_change_published, vcs_state_changed, feedback_draft_queued}` | **none** | Not model output, not accounting, not failure. Allow-listed `Raw` under `IncludeRaw` only. |

**★ v0.x ships 6 mapped frames plus tolerate-all plus 3 warning frames.** `system/init`,
`assistant`(text), `assistant`(tool_use), `user`(tool_result), `result`, `rate_limit_event`, plus
`api_retry` and `permission_denied` as warnings. The remaining ~26 rows above are *specified* so the
translator's shape is right, but each resolves to `none`-plus-a-string and none blocks the first
cut — the deduped `WarnUnknownFrame` line **is** the maintenance mechanism, and it makes drift
self-reporting rather than speculative.

#### 3.3 `result` → `Usage` — which field is authoritative

The vendor ranks its own three overlapping accounting fields:

| Field | Vendor's words | Verdict |
|---|---|---|
| `usage` | *"**MAIN AGENT LOOP ONLY** — excludes Task subagent, sidechain, and auxiliary model calls. **Prefer `modelUsage`.**"* | `Raw` only |
| `usage.iterations[]` | undocumented | **Unusable.** One entry for a two-round turn, whose `output_tokens:31` matches neither assistant frame (20, 4) nor the total (275). **Dropped.** |
| **`modelUsage`** | *"main loop, Task subagents, sidechains, and internal calls such as compaction… **the correct field for token/cost accounting; treat it as an estimate, not a billing statement.**"* | ★ **THE SOURCE.** One `provider.Usage` row per key, **never merged** |

| `result` field | → | Note |
|---|---|---|
| `modelUsage[k].{inputTokens, outputTokens, cacheReadInputTokens, cacheCreationInputTokens}` | `Usage.{InputTokens, OutputTokens, CachedInputTokens, CacheWriteTokens}` | **Exact.** Not estimates. |
| `modelUsage[k].costUSD` | `Usage.CostUSD` + `CostVendorEstimate` + `MeasureEstimated` | |
| `modelUsage[k].canonicalModel` / `.provider` | `Usage.Model` (fall back to `k`) / `Usage.ProviderName` | ★ `provider != "firstParty"` ⇒ **not subscription-billed**; free post-hoc confirmation (C5 signal 5) |
| `modelUsage[k].contextWindow` / `.maxOutputTokens` | `Capabilities.MaxInputTokens` / `MaxOutputTokens`, tier `CapProbe` | 1 000 000 / 64 000 in both fixtures — an undocumented, free per-turn capability probe |
| `total_cost_usd` | **`Meta.Cost` only, never a row** | `== Σ modelUsage[*].costUSD`, verified exactly twice. Mapping both double-counts. |
| `usage.output_tokens_details.thinking_tokens` | `Usage.ReasoningTokens` on the **main-loop row only** (the row whose model `== init.model`); else `nil` | main-loop scoped; `modelUsage` has no reasoning field |
| `stop_reason` + `terminal_reason` | `Finish.Reason` (**terminal_reason first** — it is richer), `Finish.Raw` | `terminal_reason` is effectively **open** (`api_error_*` values are constructible), so match with `strings.HasPrefix`, never an exact table |
| `is_error` | ★ the error **branch**, never `subtype` | T19 |
| `api_error_status` | **int** → `provider.StatusKind(n)` | 404 verified live. The *same concept* on an `assistant` frame is the **string** enum in `.error` — two shapes, one concept |
| `errors[]` (error subtypes only) | `Error.Message` (joined, redacted) | absent on `success` |
| `session_id` | ★ `Finish.ProviderState` (only when a `result` was seen — §2.5) | the `--resume` handle |
| `result` (string) | **runtime fallback for the message body** (P8) + a test cross-check | |
| `permission_denials[]` | count → `Meta.Warnings`; rows → item 17 | authoritative. `tool_input` is arbitrary user data — **hash or opt-in** per dashboard §7 |
| `duration_api_ms`, `ttft_ms`, `ttft_stream_ms`, `time_to_request_ms`, `num_turns`, `subagent_stats` | dashboard columns only (§7) | ★ `Usage.TTFT` is **kolk's own clock**, never the vendor's (§1.4: measured locally, to the first *content* event). In the tool-use fixture `ttft_ms(4504) > ttft_stream_ms(1344)` and the semantics are unverified — never pool with an HTTP TTFT. `num_turns` is the **tool-loop round count** (1 and 2 in the fixtures), *not* a retry count: putting it in `Meta.Attempt` makes "a fallback fired" read true on every tool call |

★ **A hazard, now live and handled.** `result` usage is **cumulative across turns** in
`--input-format stream-json` sessions. This document originally recorded that v0.x used one process
per turn, so kolk was safe, and warned that "the day anyone adopts stream-json input to amortise the
1–3 s of Node startup, every `EventUsage` becomes a running total and must be diffed — or item 17's
cost chart grows quadratically."

**That day was 2026-08-26.** At the owner's direction the Claude backend now keeps one process alive
for the whole Kolkrabbi session (checkpoint B12.5) and does use `--input-format stream-json`, so the
one-process-per-turn assumption above no longer holds anywhere in this document. `ClaudeSession`
therefore keeps the totals it has already charged and reports each turn as the difference
(`chargeTurn`, checkpoint B12.11). A report smaller than the running total means the provider
restarted its own accounting: kolk takes that report at face value and rebases rather than charging
a negative turn.

The same checkpoint fixed a second consequence of the switch: a `result` frame carries its usage
*after* its completion event, and the turn loop returned on sight of the completion, so every
session turn recorded `$0`.

#### 3.4 `Finish.Reason` / `Kind`, from `terminal_reason` first

`completed`+`end_turn` → `FinishStop` · `completed`+`tool_use` → `FinishToolCalls` ·
`aborted_streaming`/`aborted_tools` → `FinishCancelled`/`KindCanceled` ·
`prompt_too_long` → `KindContextOverflow` ·
`blocking_limit`/`rapid_refill_breaker` → `KindQuotaExhausted`+`RateAccount` ·
`budget_exhausted` → `KindBudgetExhausted` ·
`model_error`/`api_error`/`api_error_*` → from `api_error_status` via `StatusKind` ·
`malformed_tool_use_exhausted` → `KindServer` ·
`structured_output_retry_exhausted`/`image_error` → `KindInvalidRequest` ·
`turn_setup_failed` → `KindServer` at **`PhaseConnect`** ·
`max_turns`/`stop_hook_prevented`/`hook_stopped`/`background_requested`/`tool_deferred` → `FinishOther` ·
unknown → `FinishOther` + `WarnUnknownFrame`.

The **api-error string enum** (`assistant.error`, `system/api_retry.error`) →
`authentication_failed` → **`KindBackendLogin`** (the remedy is `kolk login claude`, never a key) ·
`oauth_org_not_allowed`, `account_on_hold` → `KindPermission` **and disable the backend for the
session** (§3.5) · `billing_error` → `KindCredits` · `rate_limit` → `KindRateLimit`, or
`KindQuotaExhausted`+`RateAccount` if T17 fired · `overloaded` → `KindOverloaded` ·
`invalid_request` → `KindInvalidRequest` · `model_not_found` → `KindModelNotFound` ·
`server_error` → `KindServer` · `max_output_tokens` → `KindOutputLimit` (`ActContinue`) ·
`unknown` → `KindUnknown` · `dlp_request_denied` → `KindPermission`, and the message is **marked so
resume never re-sends it**.

Then §1.8's terminal-condition rule still applies — **after** P8's `result.result` adoption, so it
catches a genuinely empty turn rather than one whose content moved to an unmodelled frame.

#### 3.5 Unknown-frame policy — **fail open for protocol, fail CLOSED for policy**

The vendor's `type` union has 42 members today and had fewer a year ago. It states the rule for four
of its own fields (`capabilities`: *"Open set — ignore unknown values"*), and arch §7 states it for
kolk's own protocol (*"clients MUST ignore unknown event types and unknown fields"*).

1. **An unrecognised `type`, or an unrecognised `system` subtype, produces ZERO events and NEVER an
   error.** Forward-compatible by construction.
2. **It is never silent.** The first occurrence of each `type`/`subtype` **pair** per stream emits
   `Warning{WarnUnknownFrame, Was:"<type>/<subtype>", Detail:"claude 2.1.240"}`, deduped in
   `st.SeenFrame` — a 400-frame flood of `system/task_progress` is one line, not four hundred.
3. **It always resets both watchdogs.** An unknown frame is proof of liveness; a future
   `system/still_thinking` must not be killed as a stall.
4. **A known `type` with unknown *fields* is always fine.** Decoding uses per-frame structs carrying
   only the listed fields; **there is no `DisallowUnknownFields` anywhere in the package**, and a
   test asserts it. Nor may a *declared* type be trusted — `init.memory_paths` is `{}` in the
   vendor's own schema and `[]` on the wire in both fixtures — so every optional field decodes into
   `json.RawMessage` or a tolerant type, and a shape mismatch drops the field, never errors.
5. ★ **THE POLICY EXCEPTION.** Any frame whose `type`/`subtype` matches
   `/(policy|terms|compliance|deprecat|unauthoriz|forbidden|not_permitted)/`, and every
   `system/informational` at `level:"warning"`, is **surfaced to the user immediately and verbatim**
   (after `secret.Redact`) rather than folded into a deduped warning. `oauth_org_not_allowed` and
   `account_on_hold` **disable this backend for the remainder of the session** with a message naming
   the vendor as the source.
   *Rationale:* the rule that makes the adapter robust against **protocol** drift would otherwise make
   it deaf to **policy** drift. Anthropic tightened this position once already and shipped the signal
   on the wire before the docs. kolk must not keep driving a backend the vendor just told it to stop
   driving, on an account that is the user's, with the notice swallowed into a one-line warning that
   may render after the turn. Cost: one regexp and one bool.

#### 3.6 Ordering, limits and redaction, summarised

```
EventStart                          ← exactly one, first, carries all pre-spawn Warnings
EventResponseMeta (provisional)     ← P9: requested model, so ordering is stable
[EventResponseMeta]                 ← at system/init (correction), and on model_refusal_fallback;
                                      also the carrier for mid-stream Warnings (A2)
[ text | tool_use | tool_result … ] ← wire order, Seq monotonic
EventUsage × N                      ← N >= 1 ALWAYS, one per modelUsage key, one all-nil if {}
EventFinish | EventError            ← exactly one, last
```

Frames may legitimately arrive **after** `result` (task notifications, session-state changes), so the
adapter emits its terminal event on `result` and then **drains to EOF without translating** on a
bounded deadline (P6). Reader buffer 1 MiB; hard per-line cap 16 MiB, non-fatal (P5). Junk lines
tolerated at any position on a 16-line / 64 KiB budget (P4). `strings.ToValidUTF8` at every ring and
truncation boundary — a split rune in the stderr tail otherwise reaches the session file and every
attached protocol client. `secret.Redact` on every byte that can reach `Raw`, `Error.Message` or the
stderr tail (C1).

---

### 4. kolk → vendor flag mapping

#### 4.1 The constant spine, on every turn

```
claude
  -p
  --verbose                       ★ MANDATORY and undocumented. Verified live 2026-08-22:
                                    without it, exit 1, ZERO stdout bytes, stderr
                                    "Error: When using --print, --output-format=stream-json
                                    requires --verbose". §8.1's spawn line omits it and would
                                    have failed 100 % of the time.
  --output-format stream-json
  --input-format  text
  --safe-mode                     ★ NOT --bare.
  --setting-sources ""            ★ the reversible isolation default (§4.4)
  --settings '{"claudeMdExcludes":["**"],"disableAllHooks":true,"enabledPlugins":{},"env":null}'
                                    ★ NOTE: no "apiKeyHelper":"" — see §1.3. "env": null must
                                    replace the WHOLE block; per-key nulls are materialised as
                                    environment entries by Claude Code and can shadow OAuth.
  --strict-mcp-config
  --session-id <uuid kolk minted>   (turn 1)   |   --resume <uuid>   (turn >= 2)
                                    + the ENTIRE argv re-passed every turn (§4.4 #4)
                                  ← prompt on STDIN, written async, pipe closed immediately
```

**`--safe-mode` verified live to do what this design needs** (2026-08-22, zero quota — the run failed
at model lookup): with `--safe-mode --setting-sources ""` the **8 hook frames disappeared**,
`plugins: 0`, `mcp_servers: 0`, and `permissionMode` reported **`default`** — i.e. kolk's flag won
over the capturing machine's `~/.claude/settings.json`, which sets `defaultMode:"auto"`. `skills`
survived (16), which is worth knowing and harmless. **This is the proof that the committed fixtures
cannot be produced by kolk's production argv** — see §10.

★ **`--bare` is forbidden here and it is the one flag that would break everything quietly.** Its own
`--help` text: *"skip hooks, LSP, plugin sync, attribution, auto-memory, background prefetches,
**keychain reads**, and CLAUDE.md auto-discovery. Sets `CLAUDE_CODE_SIMPLE=1`."* It either fails for
want of a key or bills the user's API account. It belongs to a future API-key backend.

★ **The env scrub is correctness, not hygiene.** The vendor's own env-vars page on
`ANTHROPIC_API_KEY`: *"When set, this key is used **instead of** your Claude Pro, Max, Team, or
Enterprise subscription **even if you are logged in**. In non-interactive mode (`-p`), the key is
always used when present."* Without the scrub, "use my Claude Max plan" quietly bills the user's API
account and the **only** visible difference is one field in one frame.

#### 4.2 The mapping table

| kolk concept | `claude` | Note |
|---|---|---|
| **mode: chat** | `--tools ""` + `--permission-mode dontAsk` | ★ Read-only becomes **structural**: with no built-in tools in context, "chat cannot touch your files" is a fact, not a prompt instruction |
| **mode: code** | `--tools "Bash,Read,Edit,Write,Glob,Grep,WebFetch,WebSearch,TodoWrite"` (**no `Task`**) + `--permission-mode acceptEdits` | The working default. Dropping `Task` keeps code mode single-threaded, as it is natively |
| **mode: agent** | ⬚ **v0.5.** v0.x refuses to bind `claude` to agent mode and says why | kolk's orchestrator, per-role models and parallel panes are all off here; the vendor's `Task` schedules its own subagents and the bus cannot represent a foreign subagent tree (item 14) |
| **effort** | `--effort low\|medium\|high\|xhigh\|max` | ★ **Deletes `WarnEffortDropped` from §8.3.** kolk's `EffortLow…EffortMax` map 1:1; `EffortNone`/`EffortMinimal` clamp to `low` with **`WarnEffortClamped`**. ⚠ **Not parse-validated** — verified live: `--effort bogus` prints `Warning: Unknown --effort value 'bogus' — ignoring it and using the default effort` **on stderr and runs anyway**. kolk captures stderr, so the dial would silently no-op — item 7's headline failure mode. **`argv.go` validates against the closed set and returns `KindInvalidRequest` at `PhasePreflight`.** |
| **effort → rounds** | `--max-turns` (low 8 · medium 16 · high 30 · max 60) | Hit ⇒ `error_max_turns` ⇒ `KindTruncated` ⇒ `ActContinue`: a partial answer, not a failure |
| **model** | `--model <alias\|full-id>` | Aliases (`opus`, `sonnet`, `haiku`, `fable`) **or** full ids. **Fire-and-check**: verified live, an unknown model exits 1 with `[claude-code:unrecognized_model]` on stderr **and** a clean `result{is_error:true, api_error_status:404}` — so kolk classifies `KindModelNotFound` at **zero quota cost**. Ground truth is `init.model` |
| **fallbacks** | `--fallback-model "a,b,c"` | ★ `--help`, verbatim: *"Accepts a comma-separated list to try each in order."* `-p` only. **Corrects `AcceptsFallbackList:false`.** A fired fallback surfaces via `system/model_refusal_fallback` and >1 `modelUsage` key |
| **permissions (default)** | `--permission-mode acceptEdits` | reads, edits, and `mkdir`/`touch`/`mv`/`cp`-class commands run without asking |
| **permissions (locked down)** | `--permission-mode dontAsk` + `--allowedTools` | runs **only** what `--allowedTools` covers plus reads; everything else is **denied, never prompted** |
| **kolk's deny + hardline rules** | `--disallowedTools "<one comma-separated string>"` | Pushed down **even under `--yolo`** — vendor deny rules stay in force under `bypassPermissions`, so the floor **degrades rather than vanishing** |
| **kolk's allow rules** | `--allowedTools "<one comma-separated string>"` | ⚠ kolk **does not translate its own permission grammar**. Config carries a **vendor-native** list (`claude.allowed_tools`) passed through verbatim, and kolk's own rules print as inert once per session. A lossy translation would be *a claim of enforcement kolk cannot make*: the vendor strips `timeout`/`nice`/`xargs` before matching but **not** `npx`/`docker exec`/`devbox run`, so `Bash(devbox run *)` authorises `devbox run rm -rf .`. **kolk's rule translator refuses to emit a prefix allow rule for any of those wrappers and says why.** |
| **kolk's `ask` rules** | **degrade to DENY**, with a message naming the rule and the fix | The alternative (mapping `ask` → the vendor's `auto` classifier) silently *approves* things kolk's rules say to ask about, which is worse. Honest friction beats silent approval |
| **yolo (`-y` / `/yolo`)** | `--permission-mode bypassPermissions` (never `--dangerously-skip-permissions`, so the debug log names what it does) | ★ **Typed confirmation, once per session**, naming the loss: kolk's hardline blocklist — which survives `/yolo` on every other backend — **does not exist here**. Even this cannot auto-approve `rm`/`rmdir` on a critical path, explicit `ask` rules, or MCP tools marked `requiresUserInteraction` |
| **system-prompt addition** | `--append-system-prompt` (cap 32 KiB; `--append-system-prompt-file` if it outgrows argv) | ★ **`--system-prompt` is unreachable in the argv builder**, and the *payload* is grepped for impersonation strings (C2). Replacing the vendor's prompt would break its own tool prompting **and** is one step from `anthropic_spoof.txt`. **The brightest line in the item.** In the isolated profile the vendor loads no `CLAUDE.md`, so kolk owns the context deterministically |
| **cwd** | `SpawnCmd.Dir` | ★ **No `--cwd` flag exists** — the *process* cwd is the working root. `--add-dir` only *adds* access |
| **extra roots** | `--add-dir "a,b"` | ⚠ Warns-and-runs on a nonexistent path in 2.1.240, contradicting its own help text. kolk validates paths itself |
| **session (turn 1)** | `--session-id <uuid>` | ★ **kolk mints the UUID**, so it owns the handle before the process starts — closing the "crashed before init, so we have no id" hole entirely |
| **session (turn ≥2)** | `--resume <uuid>` | Works from any directory (2.1.223+). Unknown id ⇒ exit 1 + `No conversation found with session ID` ⇒ **soft restart** (§4.4 #6) |
| **session fork** | `--resume <uuid> --fork-session` | §1.2's `session.fork` clears `ProviderState`; this is the vendor-side equivalent when the user forks *within* the backend |
| **budget** | `--max-budget-usd <Request.Budget.RemainingUSD>` | ⚠ **Relabelled at every surface.** The flag's own help says *"Maximum dollar amount to spend on **API calls**"* — on a subscription there are none. It is a stop condition, not a spend limit: rendered `≤ $2.00 API-equivalent (does not limit your plan usage)`, and `KindBudgetExhausted`'s message on this backend never contains the word "spent" |
| **structured output** | `--json-schema '<schema>'` when `Request.Format.Kind == "json_schema"` | Result in `result.structured_output`; dedicated failure subtype. **Used opportunistically, never depended on** — absent output falls back to `result.result` prose |
| **`Request.SessionID`** | **never sent** | kolk's ids look like `20260822-150405-1f3a`; `--session-id` demands a UUID. Only `ProviderState` reaches the vendor. Asserted in `argv_test.go` |

#### 4.3 Deliberately not mapped

| Request field | Reason | Warning |
|---|---|---|
| `Temperature`, `TopP`, `Stop`, `MaxOutputTokens` | **no flags exist.** Output and thinking budgets are env-only (`CLAUDE_CODE_MAX_OUTPUT_TOKENS`, `MAX_THINKING_TOKENS`) and kolk's env is deliberately cleared | one `WarnParamDropped` each |
| `Tools` (schemas) | `--allowedTools` takes **names**, not JSON Schema | `WarnToolsDropped` |
| `Cache` | the child manages its own (1 h ephemeral writes visible in both fixtures); kolk has no lever and claims none | `WarnCacheUnsupported` |
| `Routing` | no gateway here | `WarnParamDropped` |
| `--include-partial-messages` | ⬚ **v0.5.** Not passed in v0.x | — |
| `--permission-prompt-tool` | ⬚ **v0.6 at the earliest** (§7) | — |
| `--continue` | **never.** Ambient and directory-scoped; it can silently attach to a session a human started in that folder, and it *excludes* `-p` sessions unless combined with `-p` | — |
| `--permission-mode plan` | **never.** There is no way to approve a plan under `-p`, so it is a dead end that looks safe. kolk's own `/plan` (item 15) is chat mode plus a read-only tool set | — |
| `--bare`, `setup-token`, `CLAUDE_CONFIG_DIR`, `--betas` | §1.3 / API-key-only | — |
| `--forward-subagent-text`, `--agents` | ⬚ v0.5 with agent mode | — |

#### 4.4 Six behaviours the argv builder must handle

1. **The variadic footgun.** `--allowedTools`, `--disallowedTools`, `--tools`, `--add-dir`,
   `--mcp-config`, `--file`, `--betas` are **variadic**: they consume every following bare token until
   the next `--flag`. `claude --allowedTools Read myprompt` silently registers `myprompt` as a tool
   name. **Rule: exactly one comma-separated string per variadic flag, and the prompt always on
   stdin** — which C8 mandates anyway for a different, equally good reason.
2. **`--effort` and `--add-dir` warn-and-continue rather than erroring.** kolk validates both itself
   before spawning. Organisations may also cap effort **server-side, silently, with no warning at
   all** under stream-json output — so `/effort` echoes `effort max (requested)`, and the
   parenthetical is **permanent on this backend**. `system/init.effort` is *not* the ground truth
   people expect: the field is Remote-Control-only and is **absent from every `-p` stream** (verified
   against both committed fixtures).
3. **An empty rendered prompt is `KindInvalidRequest` at `PhasePreflight`.** Verified live: an empty
   stdin yields exit 1, `Error: Input must be provided…`, **8 hook frames and no `result`** — which
   without this guard becomes `KindTruncated`, whose `Decide` row retries **twice**: three no-op
   process spawns for a stray Enter.
4. **Resume does not restore the flag vector.** `--mcp-config`, `--settings`, `--plugin-dir`,
   `--fallback-model` and `--add-dir` are not restored; `plan` and `bypassPermissions` modes are never
   restored. kolk **re-passes its entire argv every turn** and treats the flag vector — not just the
   uuid — as part of `ProviderState` (`{session_id, cli_version, argv_hash}`), warning when it changed
   mid-session.
5. **The reversible auth default.** `--setting-sources ""` suppresses a user-configured
   `apiKeyHelper` in `~/.claude/settings.json`, and the env allow-list suppresses
   `ANTHROPIC_API_KEY`. Both are **defaults the user chose by naming the subscription backend**, and
   both are **visible and reversible**:
   - `kolk config set claude.auth helper` → `--setting-sources user` (user-level settings only, where
     `apiKeyHelper` lives) and `Assert.APIKeySource = "apiKeyHelper"`. **This restores the auth method
     without restoring untrusted project/local hooks** — which is why it, and not a blanket
     `inherit`, is the v0.x reversal.
   - `kolk config set claude.auth console` → adds `ANTHROPIC_API_KEY` to the allow-list and flips
     `Assert.APIKeySource = "ANTHROPIC_API_KEY"`.
   - `kolk doctor claude` prints both lines unconditionally, so a user whose org mandates a secret
     broker sees that it was bypassed instead of discovering it on a bill.
   ⬚ Full `claude.settings inherit` (project + local) is **v0.5, gated on a per-directory trust
   record**: a `-p` session runs a project's `.claude/settings.json` hooks and connects its `.mcp.json`
   servers **in a folder the user never trusted, with no trust dialog** — the interactive binary shows
   one and headless does not, so shipping `inherit` without kolk's own prompt makes kolk a
   workspace-trust bypass.
6. **An unrecognised `--resume` id is a SOFT RESTART, not a failure.** The vendor expires transcripts
   after 30 days (`cleanupPeriodDays`). Match `No conversation found with session ID` on stderr with
   exit 1, drop `ProviderState`, emit the already-existing `WarnHistoryLost`, and re-spawn **once**
   with a fresh `--session-id` plus kolk's retained transcript as a labelled `<prior-conversation>`
   prelude. §1.2 already prescribes exactly this. Without it, a month-old kolk session takes the
   `KindTruncated` path and **retries twice with the same dead uuid** before failing with a
   misleading message.

**History on a backend switch.** `HistoryOwned:true` means only the newest user turn goes over. If
`ProviderState` is empty but the kolk session already has turns, the adapter renders the prior
transcript into **one explicitly delimited `<prior-conversation>` prelude** (with provider-executed
tool calls flattened to prose per A4), emits `WarnHistoryReplayed`, and the UI prints one line.
Refusing to switch is user-hostile; passing it **unlabelled** would let the vendor read kolk's
transcript as if the user had typed it.

---

### 5. `kolk login claude` / `kolk doctor` — and the structural guarantee

#### 5.1 The guarantee, as five mechanisms

| # | Guarantee | Mechanism — why a promise is not required |
|---|---|---|
| **G1** | kolk **cannot capture** the login flow's output | `kolk login` calls `shell.Handover(ctx, cmd)`, a **different L0 entry point from `Spawn`**. It inherits the real stdin/stdout/stderr, creates **no pipes at all**, and on POSIX is `syscall.Exec` where possible — kolk's process is *replaced*. There is no `io.Reader` in the call. A future contributor cannot scrape a token from a stream that does not exist. `TestHandoverHasNoPipes`. |
| **G2** | kolk **cannot forward** an unknown credential | `SpawnCmd.Env` is an allow-list over a cleared environment (C7): `PATH HOME USER LOGNAME SHELL TMPDIR TZ LANG LC_ALL LC_CTYPE` + `TERM=dumb` (**not** the user's TERM, and no `COLUMNS`/`LINES` — a TTY-shaped env invites ANSI chrome into stderr and makes runs non-reproducible across window sizes) + `CLAUDE_CODE_ENTRYPOINT=kolk`. Deny-lists forget; allow-lists cannot. |
| **G3** | kolk **cannot read** a credential store | `TestCredentialDenylist` (C3) fails CI on 12 strings anywhere in the package. ★ The sharpest trap it catches is `claude setup-token`: it *looks* like a CI convenience feature and it prints a one-year OAuth token. It is unreachable by construction. **`CLAUDE_CONFIG_DIR` is never set** — relocating the credential store is intermediation, and it would break the very login this backend depends on. |
| **G4** | kolk **cannot hold** an identity | `LoginState` has **no field an identity can land in**, and a reflect-walking test fails on any added one. Redaction is the fallback; *not having the value* is the mechanism. |
| **G5** | kolk **verifies** it is on the subscription, mid-stream | C5's five-fact conjunction, asserted on the first `system/init`, failing closed, aborting at `PhaseConnect`. |

★ **`CLAUDE_CODE_ENTRYPOINT=kolk` is a named decision with a stated downside.** Identifying yourself
to the vendor is the right side of every line in this item (OpenAI explicitly *asks* integrators to
do it via `clientInfo.name`). It also makes kolk traffic trivially fingerprintable, and the January
2026 enforcement was request fingerprinting. kolk sets it anyway: a backend that survives only by
being unidentifiable is not a backend this project wants.

```go
// LoginState is a PREDICATE. `claude auth status --json` returns SEVEN keys —
// verified live on 2.1.240: {loggedIn, authMethod, apiProvider, email, orgId,
// orgName, subscriptionType}. THREE OF THEM ARE PII. There is nowhere here to
// put them.
//
// ★ CORRECTS 03-provider-layer.md §8.4, which says this call returns
// `apiKeySource`. It does NOT. That field exists only on system/init, which is
// why the subscription assertion is mid-stream and not a preflight check.
type LoginState struct {
	LoggedIn     bool   `json:"loggedIn"`
	AuthMethod   string `json:"authMethod"`       // claude.ai | console
	APIProvider  string `json:"apiProvider"`      // firstParty|bedrock|vertex|foundry|anthropicAws|mantle|gateway
	Subscription string `json:"subscriptionType"` // pro | max | team | enterprise
}
```

Exit 0 when signed in, **1 when not**, and the call is documented quota-free — so it can run on every
`kolk doctor` at no cost. `detect.go` is **pure**: `Detect(stdout []byte, exitCode int) LoginState`,
so `detect_test.go` runs a table (signed in, signed out, malformed JSON, empty output, exit 1, a
future field) offline forever.

#### 5.2 `kolk login claude`

```
$ kolk login claude

  Claude Agent runs the `claude` command you installed yourself, signed in with your
  own Claude subscription. kolk never sees, stores or forwards your credentials.

  binary   claude 2.1.240        /Users/you/.local/bin/claude
  login    not signed in                                    (this check costs no usage)

  kolk will now hand you to Anthropic's own sign-in. It opens in your browser, it is
  run by Anthropic's binary, and kolk creates no pipe to it — kolk cannot read it.

  Continue? [Y/n] y
  ─── handing off to: claude auth login ──────────────────────────────────────────
  <the vendor's own flow owns this terminal from here>
  ────────────────────────────────────────────────────────────────────────────────
  claude reports: signed in · claude.ai · firstParty · max

  ┌ Before you use this backend ────────────────────────────────────────────────┐
  │ Every request spends your own Claude plan's usage limits.                   │
  │ Claude Agent runs its own tools — kolk's permission prompts, checkpoints    │
  │   and /rewind do not gate them.                                             │
  │ Costs shown are an API-equivalent estimate, never your bill.                │
  │ Anthropic can change or enforce this policy "without prior notice", and has │
  │   done so before. We cannot promise this keeps working.                     │
  │ Zero-policy-risk alternative: kolk's API-key backends.                      │
  │                                    full note: docs/backends/claude-agent.md │
  └─────────────────────────────────────────────────────────────────────────────┘

  Try it:  kolk --backend claude "explain this repo"
```

Already signed in ⇒ print the state and exit 0. Not installed ⇒ print **the vendor's own install
URL** and exit 4. **kolk never installs, updates or downloads the vendor binary** — that would make
kolk the distributor and pull the Commercial-ToS conditions squarely onto it. The full risk note is
printed **once**, here, and never again on any turn.

**Attribution is explicit everywhere.** `claude reports: signed in`, not `✓ signed in`. The vendor
command is printed verbatim before it runs. And the preflight failure path prints a *command for the
user to run* — **kolk never renders a sign-in control of its own**, because *"Anthropic does not
permit third-party developers to offer Claude.ai login into their own applications"* is a sentence
about surfaces as much as about tokens.

#### 5.3 `kolk doctor` and `kolk doctor claude`

```
$ kolk doctor
  Backends
    openrouter    ok    key present · 421 models · catalog 3 h old
    claude        ok    Claude Agent · claude 2.1.240 · signed in (max, firstParty)
    codex         n/a   v0.4 — fixtures not yet captured
    gemini        n/a   API key only — Google prohibits third-party use of Gemini CLI OAuth

  claude: what changes in this backend        kolk doctor claude
```

```
$ kolk doctor claude
  Claude Agent  (backend: claude)
    binary        claude 2.1.240 · /Users/you/.local/bin/claude          (>= 2.1.205 required)
    sign-in       claude reports: claude.ai · firstParty · max            (probe cost: none)
    billing       your Claude plan, billed directly to you by Anthropic
    settings      not loaded (--safe-mode --setting-sources "")
                    → your ~/.claude/settings.json, CLAUDE.md, hooks, MCP servers and plugins
                      are NOT in effect, including any apiKeyHelper.
                      restore the auth helper:  kolk config set claude.auth helper
    environment   cleared to 11 vars · 35 credential/routing vars confirmed absent
                    → your ANTHROPIC_API_KEY is NOT passed through.
                      use it instead:  kolk config set claude.auth console
    quota         7-day window 78 % used · resets 2026-08-25 00:00Z        (from the last turn)

  What kolk does NOT control here
    tools         Claude runs its own. kolk's permission rules, path jail and hardline
                  blocklist do not apply. kolk's `ask` rules become denials.
    confirms      kolk cannot gate a tool call: by the time kolk sees it, the file is
                  written. `--permission-mode` and `--disallowedTools` are the levers.
    checkpoints   per TURN, into a shadow git repo — not per write. /rewind rewinds a
                  whole turn and cannot undo anything outside this directory.
    cost          an API-equivalent estimate, never your bill. Tokens ARE exact.
    models        alias or full id only; no catalogue, no rotation.
    sampling      no temperature / top_p / stop / max_tokens.

  Policy        our reading is that this sits inside Anthropic's stated policy, but
                Anthropic can change it and has, and enforces "without prior notice".
                policy text verified 2026-08-22 · full note: kolk help claude-agent
```

`kolk doctor` is the **only** place that nags.

---

### 6. Per-backend capability matrix

| | **`claude`** (Claude Agent) | **`codex`** (v0.4) | **`gemini`** | **`openrouter`** |
|---|---|---|---|---|
| **Auth** | your subscription, **vendor's own login**, kolk holds nothing | your ChatGPT plan, vendor's own login | **API key only** | kolk-held key |
| **Who runs tools** | **the vendor** | the vendor | kolk | kolk |
| **kolk permission rules** | deny ✓ · allow ✓ · **ask → deny** | deny via `--sandbox` | ✓ full | ✓ full |
| **Confirm UX / path jail** | **✗** | **✗** | ✓ | ✓ |
| **Checkpoints** | **per turn** (shadow git) | per turn | ✓ per write | ✓ per write |
| **`/changes`** | ✓ (from the shadow repo) | ✓ | ✓ | ✓ |
| **Conversation `/rewind`** | **✗ — replayed into a fresh vendor session** (`WarnHistoryReplayed`) | ✗ | ✓ | ✓ |
| **File `/rewind`** | ✓ per turn, typed confirmation, cannot undo outside the root | ✓ per turn | ✓ | ✓ |
| **Streaming** | ✓ per content block (token-level ⬚ v0.5) | ✓ JSONL | ✓ | ✓ token-level SSE |
| **Token counts** | ✓ **exact**, per model | *(unverified)* | ✓ | ✓ exact |
| **Cost fidelity** | **API-equivalent estimate** (`vendor_estimate`/`estimated`) | estimate | metered | **metered, authoritative** |
| **Quota visibility** | ✓ `rate_limit_event.utilization` — the real scarce resource | ✗ | ✗ | `/key` credits |
| **Model choice** | alias or full id, **no catalogue, no rotation** | `-m`, no rotation | catalogue | 421 models, rotation ✓ |
| **Effort dial** | ✓ 5 levels, 1:1 on the reasoning knob only | `-c model_reasoning_effort=` *(unverified)* | ✓ | ✓ per-model vocabulary |
| **Context window** | 1 000 000 (probed from `modelUsage`) | *(unverified)* | catalogue | catalogue |
| **Structured output** | ✓ `--json-schema` (opportunistic) | ✓ `--output-schema <FILE>` | ✓ | ✓ |
| **Sessions / resume** | ✓ `--session-id`/`--resume`, **invalidated by SIGTERM** | ✓ `exec resume` | ✓ | ✓ |
| **Budget enforcement** | ✓ pushed into the vendor (as a *stop condition*) | kolk-side | kolk-side | kolk-side |
| **Subagents** | ⬚ v0.5, vendor's `Task`, no per-task routing | vendor's | kolk's, routed | kolk's, routed |
| **Policy status** | permitted with conditions (**high** confidence) | permitted (**high**) | **prohibited → API key only** | n/a |

---

### 7. Honest limits, and how the UI and dashboard say them

#### 7.1 What kolk genuinely loses

**1. Per-call tool gating. Gone.** The vendor announces a tool in an `assistant` frame and reports the
outcome in a `user` frame; in the committed tool-use fixture `/work/hello.txt` **already existed** by
the time kolk saw a single byte about it. kolk's permission engine, path jail, dangerous-command
heuristics and **hardline blocklist** do not apply — not "apply differently". The confirm UX is
structurally unreachable. `--permission-prompt-tool` is the only mechanism that could restore
pre-execution gating, and it is **cut from v0.x on purpose**: it requires kolk to run an MCP server
(item 16, unhardened), it **suppresses `system/permission_denied` frames entirely** — so switching
gating *on* removes the denial signal the current UX depends on — it cannot approve MCP tools marked
`requiresUserInteraction`, and its failure mode (bridge dead, parent killed, socket closed) is
unspecified by the vendor. Shipping a half-restored confirm UX whose guarantees differ from every
other backend's, with an undefined degradation path, is worse than shipping none.
**If you need kolk to gate every tool call, use an API-key backend.** `kolk doctor` says exactly that.

**2. Conversation rewind.** `HistoryOwned` is literal: kolk cannot edit, truncate, replay or inject
history, and cannot prevent auto-compaction (it learns afterwards via `compact_boundary`).
`/rewind N` of the *conversation* starts a **fresh vendor session** with kolk's retained transcript
as a prelude, emits `WarnHistoryReplayed`, and **says so** — the cache miss is real and a rewind that
silently costs a full re-prompt is the kind of thing users discover from a quota wall.

**3. Checkpoints are per turn.** kolk owns `SpawnCmd.Dir`, so it snapshots the working tree into a
**shadow git repo** (`GIT_DIR` under `~/.local/state/kolk/shadow/<hash-of-cwd>.git`, `GIT_WORK_TREE`
= the session cwd) before and after every turn. That never touches the user's `.git` and works
outside a repo. Residual holes, stated: files written **outside** `Dir` (kolk snapshots each
`--add-dir` root too), and anything destroyed under `bypassPermissions`.
★ `/rewind` on any `ExecutesOwnTools` backend **requires a typed confirmation** stating the file
count, the turn duration, and explicitly what it cannot restore (*"commands that touched anything
outside `<root>` — `npm install`, `docker`, a migration, a `git push` — are not undone"*). Same verb,
same keystroke, two orders of magnitude of blast radius: different blast radius earns different
ceremony.

**4. Cost is not money.** `total_cost_usd` reproduces exactly as
`in·$5 + cache_write·$10 + cache_read·$0.50 + out·$25` per MTok on both fixtures. It is a compiled-in
price-table computation, and Anthropic says *"Do not bill end users or trigger financial decisions
from these fields."* But on a subscription it is stranger than "an estimate": **the user did not spend
$0.13, they paid a flat monthly fee.** It is a *counterfactual* — what this turn would have cost on
the API — which is genuinely useful (it is how you compare a Max plan against metered backends) and
is **not spend**.

**5. Model selection and sampling.** No catalogue, no `/models` list for this backend beyond the four
aliases, no rotation, no `--temperature`/`--top-p`/`--stop`/`--max-tokens`/`--seed`.

**6. Effort is only the reasoning knob.** Item 7's dial is model tier + reasoning effort + tool rounds
+ subagent width + verification passes + context budget. Here: model ✓, reasoning ✓, rounds ✓, width
✗ (the vendor schedules its own), critic ✗, context budget ✗ (`--autocompact` moves a threshold;
nothing disables compaction). `/effort` prints the **projection**, not the label:
`max → claude-opus-5 · xhigh (requested) · ≤60 rounds · width: vendor-controlled · critic: off`.

**7. Concurrency.** kolk defaults to **one vendor child at a time, machine-wide** (a lock file, not a
per-process ring), because two kolk sessions in one repo means two uncoordinated agents editing the
same files and one subscription quota draining twice as fast against a plan Anthropic documents as
assuming *"ordinary, individual usage"*. Parallelism is v0.5, opt-in, prints the quota consequence
once, refuses to fan out above `utilization > 0.90`, and stops entirely on
`rate_limit_event{status:"rejected"}`.

**8. Vendor-side opacity.** Org policy survives `--safe-mode` by design; org effort caps clamp
**silently**; the `auto`-mode classifier, background subagent scheduling and auto-update checks are
outside kolk entirely. A background task the vendor started may be killed by P6's bounded drain — one
warning line says so when it happens.

#### 7.2 How the UI says it — the anti-nagging rule

**Say it once, keep it visible at the line level, make it queryable, never repeat it per turn.**

- **Once, at `kolk login claude`:** the full risk note.
- **Once per session, on the first turn:** one dim line, no box, no colour —
  `Claude Agent runs its own tools · checkpoints per turn · cost is an API-equivalent estimate · kolk doctor claude`
- **★ Permanently, at the line level:** every provider-executed tool call is marked **on its own
  line**, from `EventToolCall.ProviderExecuted` at render time, never from session state:
  `▸ vendor  Bash(rm -rf build/)`. Session-level honesty does not survive scrollback, `/session`
  replay, `kolk export`, or a second person reading the transcript — and "my `deny bash(rm -rf *)`
  rule was checked" is the single most dangerous misunderstanding this feature can create. Six
  characters per line, and `executor` rides the protocol's `tool.*` events (A10) so the first GUI
  client cannot render an approve/deny affordance for an action that finished minutes ago.
- **Status line:** `⟨claude⟩ code · high(req) → opus · vendor tools · 7d 78%` — quota, not dollars.
- **Contextual, only where a user reaches for a thing that is gone:** `/rewind` (typed confirm),
  `/yolo` (typed confirm), a config file with permission rules on session start, `/model` on a
  backend with no catalogue, entering agent mode. Once each.
- **At the moment it happens, one line:** a denial, a compaction, a quota-threshold crossing, a model
  fallback, a killed background task. That is the event, not nagging.
- **On demand:** `/why`, `kolk doctor claude`, `kolk help claude-agent`.
- **Never:** a per-turn warning, a banner, a repeated policy lecture. The user chose this backend.

#### 7.3 Dashboard (item 17) — schema and view rules

```sql
-- spans
ALTER TABLE spans ADD COLUMN measurement   TEXT;  -- metered|estimated|local|unknown
ALTER TABLE spans ADD COLUMN executor      TEXT;  -- 'kolk' | 'vendor'
ALTER TABLE spans ADD COLUMN billing_mode  TEXT;  -- subscription|vendor_apikey|vendor_cloud|unknown
ALTER TABLE spans ADD COLUMN backend_version TEXT;
ALTER TABLE spans ADD COLUMN vendor_request_id TEXT;   -- assistant.request_id, first per turn
ALTER TABLE spans ADD COLUMN vendor_ttft_ms    INTEGER; -- result.ttft_ms — NEVER pooled with HTTP TTFT
ALTER TABLE spans ADD COLUMN vendor_api_ms     INTEGER; -- duration_api_ms: 6084 of 6803 ms in the fixture
ALTER TABLE spans ADD COLUMN vendor_startup_ms INTEGER; -- time_to_request_ms: 53–54 ms
ALTER TABLE spans ADD COLUMN service_tier      TEXT;
-- turns
ALTER TABLE turns ADD COLUMN rounds             INTEGER; -- result.num_turns — the TOOL-LOOP count
ALTER TABLE turns ADD COLUMN permission_denials INTEGER;
ALTER TABLE turns ADD COLUMN subagent_stats     TEXT;    -- JSON, aggregate only
ALTER TABLE turns ADD COLUMN quota_utilization  REAL;    -- rate_limit_info.utilization
ALTER TABLE turns ADD COLUMN quota_window       TEXT;    -- five_hour|seven_day|seven_day_opus|…
-- and the enum widens:
--   cost_source: openrouter | price_table | vendor_estimate | header | followup | free | unknown
-- and the dollar column for this backend is named:
ALTER TABLE spans ADD COLUMN est_api_equivalent_usd REAL; -- NOT cost_usd
```

Four recording rules and four view rules, all enforced in the query layer rather than by convention:

1. **One `llm_call` span per `modelUsage` key, never merged**; one `tool_call` span per vendor tool
   with `executor='vendor'`, `tool_ok` from `is_error`, duration from the announce→result gap.
2. **`cost_source='vendor_estimate'`, `measurement='estimated'` on every row**, never re-priced —
   re-pricing would re-derive the same arithmetic with a staler table.
3. **`gen_id` is NULL.** There is no gateway generation id and `Catalog.Reconcile` is
   `ErrUnsupported`, so §5.8's `cost_source='followup'` backfill lane does not exist here. The
   nearest join key is `assistant.request_id` (per API call, several per turn) — recorded as
   `vendor_request_id`.
4. **`quota_utilization` is the primary cost series for this backend.** Dollars are a labelled
   counterfactual. This is the one place the dashboard is *more* informative than on a metered
   backend, and it arrives free in every turn.
5. ★ **The leaderboard GROUPS BY `(model, executor)`.** Two permanent rows,
   `claude-opus-5 (Claude Agent)` and `claude-opus-5 (kolk)`, **never merged and never hidden behind
   a measurement filter.** Merging them measures two harnesses, not two models — different system
   prompt, different tools, the vendor's own compaction, and ~12 k tokens of context the user never
   wrote. *Hiding* them is worse: it hides exactly the backend the user is trying to evaluate
   ("is my Max plan worth it?").
6. **Token and latency views include everything but always group by `measurement`**, so 1–3 s of Node
   startup is never averaged into an HTTP TTFT.
7. **No dollar view sums an `estimated` row into a total a `metered` row also feeds.**
8. **Every chart states which measurement classes it includes.** That sentence is the whole reason
   `Measurement` is a separate axis from `CostSource`.

---

### 8. Codex and Gemini

#### 8.1 `codex` — permitted, higher confidence than Claude, ships at **v0.4**

OpenAI *documents* third-party embedding. From
[developers.openai.com/codex/app-server](https://developers.openai.com/codex/app-server) (fetched
2026-08-22), verbatim:

> "Codex app-server is the interface Codex uses to power rich clients… **Use it when you want a deep
> integration inside your own product: authentication, conversation history, approvals, and streamed
> agent events.**"

with ChatGPT OAuth as a first-class auth mode and an experimental `chatgptAuthTokens` mode explicitly
*"intended for host apps that already own the user's ChatGPT auth lifecycle"* — OpenAI blessing what
Anthropic forbids. Its only ask of integrators is **self-identification**: *"Use `clientInfo.name` to
identify your client… If you are developing a new Codex integration intended for enterprise use,
please contact OpenAI."* [codex/auth](https://developers.openai.com/codex/auth) contains no
prohibition on third-party clients at all — only recommendations (*"API keys are still the
recommended default for automation"*).

Verified on this machine 2026-08-22 (`codex-cli 0.147.0`, free): `codex exec --json`
(*"Print events to stdout as JSONL"*), `-C/--cd <DIR>`, `-m/--model`, `-s/--sandbox`,
`--output-schema <FILE>`, `codex exec resume`, `codex login status` → `Logged in using ChatGPT`,
exit 0 — **no `--json`**, so `codexBackend.Detect` keys on the **exit code** first and treats the
prose as advisory, reporting `Unknown` rather than guessing.

**Conditions, non-negotiable.** Spawn the user's own binary. **Never** implement OpenAI's OAuth flow,
**never** reuse Codex's client ID, **never** read `~/.codex/auth.json`, **never** set `CODEX_HOME`,
**never** use `--with-api-key`/`--with-access-token` for a value kolk obtained. **Do not build on
`app-server` in v0.x** — it is marked `[experimental]` and its token mode requires kolk to *hold* a
credential, which this posture rejects even where a vendor permits it. `--output-schema` takes a
**file**, and L3 cannot write one, so `internal/cli` materialises it and passes the path.

★ **Do not copy OpenCode's ChatGPT path.** Its source implements OAuth PKCE itself, hardcoding the
Codex CLI's own client ID `app_EMoamEEZ73f0CkXaXp7hrann`, binding Codex's callback port 1455, and
calling `chatgpt.com/backend-api/codex/responses` directly. That is shape (i) — client impersonation
— against a vendor that has not yet enforced. **Tolerance is not permission**, and the Anthropic
timeline shows exactly how fast tolerance ends.

**Why v0.4 and not v0.x**, despite the *stronger* policy standing: `codex.go` cannot be tested
offline until `spec/testdata/foreign/codex-*.jsonl` are committed, and a second **untested**
translator does not prove the `vendor` table is the right seam — it just doubles the surface. Ship it
one release after `claude`, with the same 18-line fake `Spawner`, once the fixtures land (~2¢).
Accepted risk: the `vendor` struct is designed against a sample size of one, and Codex's shape
(`-C/--cd` rather than a process cwd, `--sandbox` rather than `--permission-mode`, effort via `-c`
rather than a flag, no `--json` on `login status`) may force a field or two. A small refactor at v0.4
beats a speculative abstraction now.

#### 8.2 `gemini` — never a spawn backend

Re-fetched 2026-08-22 from `google-gemini/gemini-cli` `docs/resources/tos-privacy.md`, verbatim:

> "Directly accessing the services powering Gemini CLI (for example, the Gemini Code Assist service)
> using third-party software, tools, or services (for example, using OpenClaw with Gemini CLI OAuth)
> **is a violation of applicable terms and policies. Such actions may be grounds for suspension or
> termination of your account.**"

Google is the only one of the three that names **account termination inside the prohibition text**,
and there is no unmodified-binary carve-out to lean on — its sentence is about *accessing the
services*, not about credential handling. **`agentcli` ships no `gemini.go`, ever.** Gemini is
reached with an API key through the normal `openaicompat`/native provider path, and `kolk doctor`
says so when asked.

★ One correction to `docs/research/subscription-auth.md`: drop the weaker supporting claim that
Gemini's docs route headless mode to API keys. The fetched page does not say it, and the prohibition
alone is sufficient — defending an unsupported claim weakens a sound argument.

#### 8.3 `antigravity` (`agy`) — permitted, first-class agent CLI backend

Spawns the user's own unmodified, authenticated **Antigravity CLI** (`agy` / `antigravity` binary) under `internal/provider/agentcli` (registry key `antigravity`, alias `agy`, label **"Antigravity Agent"**).

- **Authentication / Login:** `kolk login antigravity` (or `kolk login agy`) hands over the terminal to `agy login` or detects the active session without inspecting or storing tokens.
- **Invocation & Streaming:** Runs `agy` in headless subprocess mode, translating the structured NDJSON output stream into canonical `provider.Event` envelopes.
- **Invariants:** Identical strict rules apply — zero credential handling, no credential-file inspection, no impersonation, and process isolation in an allow-listed environment.
- **Tiers & Quota:** Aligns effort levels to Antigravity model profiles and tracks API-equivalent token usage in the local stats log.

---

### 9. The risk note — verbatim, ready to paste

Ships as `docs/backends/claude-agent.md`, printed in full once on a successful `kolk login claude`,
and reachable forever from `kolk help claude-agent`.

---

> ### Using your Claude subscription with kolk
>
> This backend does not give kolk your Claude account. It runs **the `claude` command you installed
> yourself**, as a subprocess, exactly as Anthropic publishes it. You sign in through Anthropic's own
> browser flow, run by Anthropic's own binary. kolk never sees, reads, stores, forwards or refreshes
> your credentials, and never talks to Anthropic's servers on your behalf. Your usage is billed to
> your account, under your own agreement with Anthropic.
>
> **What this costs you.** Every request in this mode consumes your own Claude plan's usage limits.
> Anthropic states that *"Advertised usage limits for Pro and Max plans assume ordinary, individual
> usage of Claude Code and the Agent SDK."* Automating heavy or unattended workloads through this
> backend can exhaust your limits faster than interactive use. kolk shows your remaining window in
> the status line, and runs one Claude Agent process at a time by default for this reason.
>
> **What kolk cannot control here.** In this mode Claude Code runs its own tools — it reads, writes
> and executes on your machine through its own permission system, not kolk's. kolk passes your
> settings through (`--permission-mode`, `--disallowedTools`) and shows you what happened, and every
> tool line the vendor ran is marked `vendor` in the transcript. But **kolk's own permission prompts,
> path jail, hardline blocklist and per-write checkpoints do not gate these actions**, and kolk's
> `ask` rules become denials. Checkpoints are taken per turn instead of per write, so `/rewind`
> rewinds a whole turn and cannot undo anything that happened outside this directory. If you need
> kolk to gate every tool call, use an API-key backend instead.
>
> **Costs shown are an estimate of a price you may not be paying.** The per-request figure comes from
> the vendor CLI's own `total_cost_usd`, which Anthropic documents as a *"client-side estimate"* that
> *"can differ from your actual bill"* — and on a subscription you are not billed per request at all.
> kolk labels it **API-equivalent**: what this turn would have cost on the API. It is useful for
> comparing your plan against metered backends. It is not money you spent, it is never treated as
> billing truth, and it is never added into a total that a metered backend also feeds. Token counts,
> by contrast, are exact.
>
> **The risk, stated plainly.** kolk's reading is that this integration sits inside Anthropic's stated
> policy: Anthropic's documentation says its credential restrictions do not prevent *"an end user from
> signing in to the unmodified Claude Code binary with their own Claude subscription,"* and Anthropic
> documents driving that binary from another language as a subprocess. **But this is our reading of a
> policy Anthropic can change, and Anthropic has changed it before.** Anthropic states it *"reserves
> the right to take measures to enforce these restrictions and may do so without prior notice."* In
> January 2026 Anthropic blocked a class of third-party subscription access with no warning and
> mid-session. That block targeted tools that reused OAuth tokens directly — which kolk does not do,
> and structurally cannot do — but **we cannot promise this backend will keep working, and we cannot
> promise your account will not be affected.** You are using your own subscription at your own risk.
>
> **If you want zero policy risk**, use kolk's API-key backends (OpenRouter, or a local model). They
> are the default and the first-class path. No kolk feature is available only in this backend, and
> a fresh install never routes through it.
>
> *Policy text quoted from [Anthropic — Claude Code Legal and compliance](https://code.claude.com/docs/en/legal-and-compliance)
> and [Agent SDK overview](https://code.claude.com/docs/en/agent-sdk/overview), verified 2026-08-22.
> Re-verify before relying on it; `kolk doctor claude` prints the verification date.*

---

**Codex variant:** identical structure; swap the policy paragraph for: *"OpenAI documents signing in
to Codex with a ChatGPT plan and driving it non-interactively, and publishes an integration protocol
explicitly intended for embedding Codex in third-party products. OpenAI's terms do not currently
address third-party harnesses driving your own local `codex` binary. We read that as permitted; it is
not guaranteed."*

**Gemini:** there is no note, because there is no backend. If a user asks, the docs say: *"Google
prohibits third-party access to the services powering Gemini CLI and names account suspension or
termination as a consequence. kolk supports Gemini with an API key only."*

---

### 10. Testing

The entire harness is an **18-line `Spawner`** returning `strings.NewReader(script)` for stdout, a
scripted exit code and a canned `auth status` payload. **No vendor binary is ever invoked, no account
is ever needed, no subscription quota is ever consumed** — in CI, forever, on a machine that has never
heard of `claude`. `internal/mockagent` (real fake binaries) is needed **only** for the L0
`shell.Spawn`/`shell.Handover` integration test — not for a single translation test.

#### 10.1 Three defects in the committed fixtures, and one reclassification

| # | Defect | Evidence | Action |
|---|---|---|---|
| **A1** | The README says `claude-tool-use.ndjson` was captured with `--allowedTools "Write"`; the captured `tool_use` block is **`name:"Bash"`** running `printf 'hi\n' > /work/hello.txt && cat -A /work/hello.txt`. | `jq '.message.content[0].name'` | Fix the README. **`scripts/capture-foreign.sh` must write argv verbatim into a sidecar `.cmd` file** — a fixture whose provenance is misdescribed cannot anchor a regression test. |
| **A2** | The README omits `--include-hook-events`, yet hook frames are present. | README vs frames | Unresolved and **harmless** — the adapter tolerates hook frames unconditionally by invariant. Settled once A1 lands. |
| **A3** | ★ **The redaction corrupted a control character.** `tool_result.content` is the literal escape `␊` — U+240A SYMBOL FOR LINE FEED — where real tool output carries `\n`. Verified byte-for-byte with `od`. | `grep -o '"content": *"hi[^"]*"' \| od -c` | A test asserting `Output == "hi␊"` would **enshrine a redaction artifact**. **The redactor must leave control characters alone** (they are not identifying), and one extra fixture must carry genuine `\n`, `\t`, `\r\n` and a lone-surrogate pair in tool output. |
| **A4** | ★ **Reclassification.** Both committed fixtures were captured **without** `--safe-mode --setting-sources ""`: they carry 8 hook frames and `permissionMode:"auto"` (leaked from the capturing machine's `~/.claude/settings.json`), neither of which kolk's production argv can produce — proved live 2026-08-22, where the same flags yielded 0 hook frames and `permissionMode:"default"`. | this session | Say it in the fixture README: **the committed files are TOLERANCE fixtures**, and they are exactly the right ones for that job (they prove the adapter survives a user's real machine). Capture **one `claude-isolated.ndjson` with the exact production argv** as the CONTRACT fixture (~2¢). |

#### 10.2 The suite

```
── translation, against the committed fixtures ────────────────────────────────
Plain / ToolUse                    exact []Event, exact Seq, exact block ids
UsageIsResultOnly                  Σassistant.usage(24) ignored; EventUsage.OutputTokens == 275
CostIsVendorEstimate               CostVendorEstimate+MeasureEstimated; Σrows == total_cost_usd exactly
ModelUsageMultiModel               2 keys → 2 rows, never merged, per-row Model
ModelUsageEmpty                    modelUsage {} → exactly ONE all-nil row              [P10]
IterationsIgnored                  iterations contradicting modelUsage changes nothing
HooksBeforeInit                    + a zero-hook variant + an init RE-EMIT mid-stream
NoInitAtAll                        8 hook frames, exit 1 → billing_mode=unknown, warned  [P9]
SharedMessageID                    two blocks, one message.id → two distinct text ids
ApiErrorMessageIsNotContent        assistant{is_api_error_message:true} emits NO text     [T7e]
RateLimit{Allowed,Warning,Rejected} 0 events / 1 Warning / KindQuotaExhausted+ResetAt+cooldown
SuccessWithIsError                 {"subtype":"success","is_error":true} → EventError     [T19]
ErrorSubtypes                      4 subtypes → Kind, EventUsage still emitted FIRST
TerminalReasons                    19 values + an api_error_* value → FinishReason/Kind
ApiRetryIsNotAnError               3 api_retry frames then a clean result → Err() == nil
PermissionDenied                   → EventToolResult{IsError:true}; count matches result
CompactBoundary                    → WarnHistoryTruncated
ToolResult{BlockArray,ControlChars} string and []block → same Output; real \n \t \r\n survive
ResultTextIsTheFallback            assembled text != result.result → adopt result.result  [P8]
UnknownFrames                      spliced at EVERY position × {unknown type, unknown subtype}:
                                   byte-identical events + exactly one WarnUnknownFrame each
UnknownFieldsTolerated             every init scalar → [] {} null 0 "" — 26×5 decodes, 0 errors
PolicyFramesFailClosed             system/informational{level:"warning"} and a policy-shaped
                                   unknown type are surfaced immediately, not deduped       [3.5]
AuthStatusIsDropped                auth_status never becomes an Event and never reaches Raw  [C1]
Deterministic                      same input twice, same seed state → reflect.DeepEqual
ExecutesOwnToolsBothWays           the §8.1 two-way invariant, across every fixture

── process lifecycle, against the fake Spawner ────────────────────────────────
UnterminatedLineAtEOF              partial line + io.EOF → discarded + Warning, never Translate [P1]
EOFNoResult                        usage(all-nil) + KindTruncated{"claude exited N"}          [P2]
TerminalNotOverwritten             the EOF terminal survives the loop's next iteration        [P3]
JunkLinesAnywhere                  npm notice mid-stream → skipped on a budget, not KindTransport [P4]
HugeToolResult                     a 20 MB tool_result line → dropped + Warning, turn continues [P5]
HugeResultFrame                    a 20 MB result frame → KindTruncated, nil usage, not Transport
ResultThenGarbage / DoubleResult   exactly one terminal event, ever
DrainIsBounded                     a grandchild holding the write end → drain deadline → group kill [P6]
Cancel_SIGINTYieldsResult          ladder rung 1 → a scripted result arrives → usage row survives
Cancel_SIGTERMInvalidatesSession   rung 2 → ProviderState is NOT published; next turn replays  [2.5]
Stall_SkipsSIGINT                  idle > 120 s → KindStalled, ladder starts at SigTerminate
TurnDeadline                       tool_progress every 30 s forever → KindStalled at Timeouts.Turn [P7]
WatchdogResetByAnyFrame            hooks / tool_progress / keep_alive / unknown all reset it
ResumeIdUnknown                    "No conversation found" + exit 1 → WarnHistoryLost, ONE respawn

── argv, env and safety, pure table tests ─────────────────────────────────────
Argv_VerboseImplied                stream-json ⇒ --verbose, always                             [C6]
Argv_NoPromptInArgv                argv contains no substring of the prompt                    [C8]
Argv_BannedFlags                   --bare, --system-prompt, --continue, --permission-mode plan,
                                   setup-token, CLAUDE_CONFIG_DIR, apiKeyHelper: all unreachable
Argv_NoImpersonation               the RESOLVED --append-system-prompt payload is grepped       [C2]
Argv_EffortValidatedLocally        out-of-vocabulary effort → KindInvalidRequest, no spawn
Argv_EmptyPromptRefused            whitespace-only rendered prompt → KindInvalidRequest         [4.4#3]
Argv_VariadicIsCommaJoined         one token per variadic flag
Argv_ResumeRepassesEverything      --resume also carries model/effort/permission/safe-mode/tools
Argv_AuthReversals                 claude.auth=helper → --setting-sources user; =console → env+ [4.4#5]
Env_AllowListOnly                  11 allowed, 35 named denied absent; TERM=dumb; no COLUMNS    [C7]
VersionGate                        2.1.100 → KindBackendMissing at PhasePreflight, named version
LoginState_HasNoIdentityFields     reflect-walk; fails on any field outside the four            [G4]
LoginState_DropsPII                an email+orgId fixture appears in no Event, session or row
Redact_NoTokenEscapes              sk-ant- spliced into every field of every frame              [C1]
CredentialDenylist                 12 forbidden strings appear only in the denylist file        [C3]
NoOsExec / NoOsImport / NoHTTP     arch: agentcli imports none of them                          [C4]
HandoverHasNoPipes                 the login path never sets Stdout/Stderr                      [G1]
SubscriptionMode_Conjunction       32-row truth table incl. no-init → BillingUnknown            [C5]
FuzzTranslate                      arbitrary NDJSON → never panics, never blocks, never errors
```

`internal/mockagent` must fake exactly four things, all for the L0 test and none for translation:
(1) a binary that prints a fixed `--version` string; (2) a binary that prints a fixed
`auth status --json` payload and exits 0 or 1; (3) a binary that streams a script to stdout, writes to
stderr, and exits with a given code; (4) a binary that **ignores SIGINT** and one that spawns a
grandchild holding the stdout write end — the only way to test P6 and the ladder honestly.

#### 10.3 Fixtures still to capture — exact commands, ranked

All in a scratch dir, **never the repo**; redaction re-run each time; **argv recorded verbatim**
(A1). Priority 1 costs **$0** — none of these reaches a model.

| Pri | Fixture | Command | Buys | Cost |
|---|---|---|---|---|
| **1** | `claude-isolated.ndjson` | `claude -p "Reply with exactly: ok" --verbose --output-format stream-json --safe-mode --setting-sources "" --permission-mode acceptEdits --model opus --effort high --session-id $(uuidgen)` | ★ **the CONTRACT fixture** — the exact production argv (A4) | ~2¢ |
| **1** | `claude-error-badflag.ndjson` | `claude -p "x" --output-format stream-json; echo "exit=$?"` | the `--verbose` parse error end to end. **Already verified live: exit 1, 0 stdout bytes.** | **$0** |
| **1** | `claude-error-emptyprompt.ndjson` | `printf '' \| claude -p --output-format stream-json --verbose; echo "exit=$?"` | ★ **8 hook frames, no init, no result, exit 1** — the `NoInitAtAll` + empty-prompt tests. **Already verified live.** | **$0** |
| **1** | `claude-error-badmodel.ndjson` | `claude -p "x" --verbose --output-format stream-json --safe-mode --setting-sources "" --model no-such-model-xyz; echo "exit=$?"` | ★ **`result{subtype:"success", is_error:true, api_error_status:404, modelUsage:{}}` + `assistant{is_api_error_message:true}` + exit 1 with a clean result.** Four separate tests. **Already verified live.** | **$0** |
| **1** | `claude-error-notloggedin.ndjson` | `env -i PATH=$PATH HOME=/tmp/emptyhome claude -p "hi" --verbose --output-format stream-json; echo "exit=$?"` | the `KindBackendLogin` shape: frame, stderr, or bare exit | **$0** |
| **1** | `claude-init-apikey.ndjson` | `ANTHROPIC_API_KEY=sk-ant-invalid claude -p "hi" --verbose --output-format stream-json` | ★ `apiKeySource:"ANTHROPIC_API_KEY"` — the **negative case** for C5 and a direct test of the silent-billing footgun | ~$0 |
| **2** | `claude-cancelled.ndjson` | `(claude -p "count slowly to 500, one per line" --verbose --output-format stream-json & sleep 3; kill -INT %1); echo "exit=$?"` | `aborted_streaming`, `aborted:true`, **whether a `result` still arrives**, the exit code. The whole SIGINT-first ladder is theory until this lands | ~1¢ |
| **2** | `claude-permission-denied.ndjson` | `claude -p "run: rm -rf /" --verbose --output-format stream-json --permission-mode default --disallowedTools "Bash"` | `system/permission_denied` **and** a non-empty `result.permission_denials[]` | ~2¢ |
| **2** | `claude-maxturns.ndjson` | `claude -p "List every file, one Read per file, do not stop" --verbose --output-format stream-json --max-turns 2 --allowedTools "Read"` | `error_max_turns`, `terminal_reason:"max_turns"`, `errors[]`, whether `is_error` is true | ~5¢ |
| **2** | `claude-budget-exhausted.ndjson` | `claude -p "Write a 5000-word essay" --verbose --output-format stream-json --max-budget-usd 0.01` | `error_max_budget_usd` + `budget_exhausted`; proves budget pushes down | ~1¢ by construction |
| **2** | `claude-effort-thinking.ndjson` | `claude -p "Think step by step: what is 17×23?" --verbose --output-format stream-json --effort high` | `thinking` blocks, `system/thinking_tokens`, `output_tokens_details.thinking_tokens > 0` | ~5¢ |
| **3** | `claude-resume.ndjson` (×2) | `S=$(uuidgen); claude -p "Remember: 42" --session-id $S …; claude -p "What did I say?" --resume $S …` | the `ProviderState` round trip; whether `total_cost_usd` really restarts on resume | ~4¢ |
| **3** | `claude-multimodel.ndjson` | a prompt long enough to force auto-compaction | ★ the **only** way to test the multi-key `modelUsage` fold against real data; also `compact_boundary` | ~20¢ |
| **3** | `claude-structured-output.ndjson` | `claude -p "name three colours" --verbose --output-format stream-json --json-schema '{"type":"object","properties":{"colours":{"type":"array","items":{"type":"string"}}},"required":["colours"]}'` | `result.structured_output`'s real shape under stream-json | ~2¢ |
| **3** | `claude-partial-messages.ndjson` | add `--include-partial-messages` to the isolated command | ⬚ **gates the v0.5 token-streaming decision**; settles the double-emit risk | ~2¢ |
| **4** | `codex-plain.jsonl` / `codex-tool-use.jsonl` | `codex exec --json "Reply with exactly: ok"` | ⬚ **gates v0.4** | ~2¢ |

**Priority 1 costs $0 in total and four of its six are already observed.** Not worth provoking, and
**synthesised by hand from the schema** into `spec/testdata/foreign/synthetic/` (clearly separated
from real captures): a live `rate_limit_event{status:"rejected"}` (it needs an exhausted Max weekly
quota), `supersedes`, `dlp_request_denied`, `oauth_org_not_allowed`.

---

### 11. Ordered implementation checklist

Slots onto `02-architecture.md` §12 **step 14** and `03-provider-layer.md` §11 **M11**.
**Invariant: `go build ./... && go test ./...` is green after every step, and the 22 existing tests
stay passing.** No red build window.

| # | Step | Anchor | Prereqs | Green after |
|---|---|---|---|---|
| **S0** | **The item-3 amendment PR** (§2.6 A1–A10): `EventUsage` N≥1 · `Warnings` on `EventResponseMeta` · `Error.ProviderState` + `Collect` fallback · `Message.ToolsExecutedBy` + the flattening rule · `Timeouts.Turn` · `Proc.Signal`/`SpawnCmd.Cancel`/`StderrRing`/`Handover`/async stdin/close-read-end · 3 new `Warn*` codes · §8.2's six capability rows, §8.1's `--verbose` and `api_retry`, §8.4's `apiKeyHelper` and `auth status` list · arch §5 rule 3 narrowed · spec `executor` field. **Doc + type edits only, no agentcli code.** | before M11 | M1–M8 | 22 + ~6 |
| **S1** | **Fixture hygiene** — README A1/A2/A4 corrections, redactor stops mangling control characters, `scripts/capture-foreign.sh` writes a verbatim `.cmd` sidecar. Capture the **six priority-1 fixtures ($0)** and `claude-isolated.ndjson` (~2¢). | before M11 | — | 22 |
| **S2** | **L0: `internal/shell`** — `Handover` (no pipes, `syscall.Exec` on unix), `Spawn` with the cancel ladder against the process **group**, the stderr ring, async stdin + `EPIPE` tolerance, close-the-read-end-after-the-ladder. `internal/mockagent`'s four fake binaries + the L0 integration test. | §12 step 5 extension | S0 | 22 + ~8 |
| **S3** | **`agentcli` pure core** — `frames.go`, `claude.go` (`Translate` + `State`), `detect.go`, `caps.go`, and `translate_test.go` replaying every fixture. **Nothing spawns yet.** This is the whole translation table (§3) and roughly two-thirds of the tests. | §12 step 14 / M11 | S0, S1 | 22 + ~35 |
| **S4** | **`argv.go` + the safety tests** — the pure argv builder (§4), `argv_test.go`, `denylist_test.go`, the env allow-list, the arch-test extensions (C2/C3/C4/C7/C8), `TestRedact_NoTokenEscapes` (C1). | M11 | S3 | 22 + ~15 |
| **S5** | **`agentcli.go` — the wiring**: `chat`, `Stream`, the pump (P1–P11), the version gate, the login memo, `Close`. Register `claude`. `cmd/kolk` blank-imports it. | M11 | S2, S3, S4 | 22 + ~14 |
| **S6** | **`kolk login claude` + `kolk doctor claude`** (§5), the risk note in `docs/backends/claude-agent.md`, the machine-wide concurrency lock, `Cooldowns` writes from `rate_limit_event`. | §12 step 14 | S5 | 22 + new |
| **S7** | **UI honesty** — the per-line `vendor` marker from `EventToolCall.ProviderExecuted`, the once-per-session dim line, the quota status-line segment, the `/rewind` and `/yolo` typed confirmations, `/effort`'s projection print, `/model`'s backend-qualified ids. | §12 step 14 | S5 | 22 |
| **S8** | **Checkpoints + `/changes`** — the shadow git repo per session cwd, per-turn snapshots, `/rewind --turn`. | item 13 | S5 | 22 + new |
| **S9** | **Dashboard** — the §7.3 columns, `est_api_equivalent_usd`, the `(model, executor)` leaderboard grouping, the measurement-class footers. | §12 step 12 / item 17 | S5 | 22 + new |
| **S10** | ⬚ **v0.4 — `codex.go`**, gated on `codex-*.jsonl`. | — | S5 | — |
| **S11** | ⬚ **v0.5 — `--include-partial-messages`** (gated on `claude-partial-messages.ndjson`), **agent mode** (gated on item 14's foreign-subagent representation), **fan-out** (gated on the quota guard), **`claude.settings inherit`** (gated on a per-directory trust record). | — | S5 | — |
| **S12** | ⬚ **v0.6 at the earliest — `--permission-prompt-tool`**, and only if item 16 makes MCP exist for its own reasons, and only with a captured bridge-failure fixture proving it **fails closed**. | item 16 | — | — |

**S3 is the largest single unit and the one to start with**, because it is entirely pure, entirely
offline, and every fixture it needs is already committed.

---

## Rationale

**Why the general chat/code backend and not the delegation-only shape.** PLAN.md item 4's own words
are *"instead of using an Anthropic API KEY, uses claude code"*. A design that makes the vendor
reachable only as an agent-mode worker slot — however elegant its `Delegable` admission test, and it
is the best guardrail any of the candidate designs produced — does not answer that. It also blocks
item 4 behind item 14, the single largest unhardened item, and by its own admission degrades to "a
slot that writes to your tree" if item 14 is descoped. **`Delegable` is kept as a guardrail**: a
config that routes the planner, synthesis, judge or fast role to a backend with `ExecutesOwnTools`
or `HistoryOwned` fails at wire-up with a sentence, not at runtime with a JSON parse error. It just
is not the entry point.

**Why "frontend and recorder", declared rather than papered over.** Every structural mismatch here is
a fact about the vendor, not a gap in kolk: the child runs the tools, owns the history and prices its
own turns. Item 3 cut `ExecutesOwnTools`, `HistoryOwned`, `ProviderExecuted`, `ProviderState` and
`IdempotentConnect` specifically so those facts are *declared before the turn* rather than discovered
after it. The interface needed **no change** to absorb this backend — six capability *values* were
wrong, which is the strongest available evidence that the seam is in the right place.

**Why the compliance argument is structural.** A promise not to touch credentials is worth nothing;
a design in which touching them requires deleting a test is worth something. The login path has no
pipe. The login-state type has no field. The env is an allow-list, not a deny-list. Twelve strings
fail CI. And the one place a promise could still hide — the redaction of `Raw`, `Error.Message` and
the stderr tail — is now the *first* non-negotiable, because `02-architecture.md` line 179 already
assigned "redaction" to `agentcli.go` and all three candidate designs quietly dropped it while
scoping redaction to three PII fields from a JSON status call.

**Why cost is relabelled rather than merely flagged.** The arithmetic settles it: `total_cost_usd`
reconstructs exactly from the token counts at list prices on both fixtures. It is not a measurement
of anything that happened to the user's wallet; it is a price the user did not pay. A `~` prefix
distinguishes *precision*, not *existence*, and rendering `~$0.13` in the slot where OpenRouter
renders a real `$0.004` tells a Max subscriber they spent $13 today when their marginal spend was
zero. Meanwhile the resource that **is** scarce and **is** measured exactly —
`rate_limit_info.utilization`, 0.78 of the seven-day window in both fixtures — appeared in none of
the running UIs proposed. Swapping which of those two numbers is primary is the single highest-value
honesty change in this document.

**Why the permission bridge is cut.** It is the only mechanism that could restore pre-execution
gating, and cutting it means kolk's honesty here is partly a *choice* rather than purely a
constraint — which is worth saying out loud. But it requires kolk to run an MCP server, it
**suppresses the `system/permission_denied` frames the current UX depends on**, it cannot approve the
interactions that matter most, and its behaviour when the bridge dies is unspecified by the vendor.
The dangerous failure is not "mystifying denials"; it is the vendor falling back to
`--permission-mode` while kolk's UI still claims it gates — a case a user would never detect. A
post-hoc diff review over a per-turn shadow-git snapshot is a smaller, honest promise that kolk can
actually keep.

**Why one process per Kolkrabbi session.** `--input-format stream-json` amortises 1–3 s of Node startup
(`time_to_request_ms` is only 53 ms, so the cost is Node's, not the vendor's). It is rejected for
v0.x because it requires stdin readable for the session's lifetime, it makes every `result`'s usage a
**running cumulative total** that must be diffed or item 17's cost chart grows quadratically, and
cancellation would depend on a `control_request` wire format the vendor documents only indirectly and
does not publish as a CLI contract. Per-turn spawn is simple, cancellable with a signal, and its cost
is measured on every span as `vendor_startup_ms` — so the decision to change it will be made from
kolk's own data.

**Why the unknown-frame policy is asymmetric.** Failing open on protocol drift is what makes the
adapter survive a weekly-shipping vendor. Failing open on *policy* drift is what makes it deaf to the
one message that matters most, on an account that belongs to the user, from a vendor that has
tightened this position before and shipped the signal on the wire ahead of the docs. One regexp buys
the difference.

---

## Alternatives rejected

- **Token reuse / reading `~/.claude/.credentials.json` / implementing OAuth PKCE / `setup-token`** —
  explicitly prohibited ("collect, store, or intermediate"), empirically enforced in January 2026,
  and the shape that drew legal letters. On a CI denylist so it cannot be re-added.
- **The Agent SDK on a subscription login** — prohibited unless previously approved; the SDK is
  Python/TypeScript only anyway, and Anthropic's own documented path for Go is the subprocess.
- **Delegation-only (`claude-agent` as an item-14 worker slot)** — does not answer the user's ask,
  blocks item 4 on item 14, and makes cold-spawn fan-out the flagship use of the workload shape
  furthest from *"ordinary, individual usage"*. Its `Delegable` admission test is kept as a guardrail.
- **`--bare`** — sets `CLAUDE_CODE_SIMPLE=1` and never reads OAuth or the keychain; structurally
  incompatible with a subscription backend and would silently bill an API account.
- **`--settings '{"apiKeyHelper":""}'`** (currently in §8.4) — an affirmative instruction to disable a
  configured auth method, which is the one enumerated ToS condition written as a bullet. Replaced with
  detect-and-report.
- **A blanket `native` / `--claude.inherit-config` profile in v0.x** — a `-p` session runs an
  untrusted project's hooks and MCP servers with **no trust dialog**, making kolk a workspace-trust
  bypass. Deferred behind kolk's own per-directory trust record; the *auth* reversal
  (`--setting-sources user`) ships now because that is the ToS-relevant half.
- **`--permission-prompt-tool` in v0.x** — see Rationale. v0.6 at the earliest, and only with a
  fixture proving it fails closed.
- **`--include-partial-messages` in v0.x** — the committed fixtures were captured without it, so the
  `stream_event` path would be the one part of the translator with no offline test, in a design whose
  central promise is "tested offline forever". With partials on, the *complete* `assistant` frame
  still follows, so a mistake double-renders every token. v0.5, gated on a 2¢ fixture.
- **`--continue`, `--permission-mode plan`, `CLAUDE_CONFIG_DIR`, `--system-prompt`** — each is
  ambient, unapprovable, credential-relocating or impersonation-adjacent. Unreachable in the argv
  builder, asserted by a test.
- **A `gemini` spawn backend** — Google names account termination in the prohibition itself and
  offers no carve-out. Never.
- **`codex app-server`** — `[experimental]`, and its `chatgptAuthTokens` mode requires kolk to hold a
  token, which this posture rejects even where the vendor permits it.
- **Hiding vendor rows from the leaderboard behind a `measurement='metered'` filter** — it hides
  exactly the backend the user is trying to evaluate. Group by `(model, executor)` instead.
- **A goroutine-based cancel watcher inside `agentcli`** — violates item 3 §1.1's single-goroutine
  contract and races `Signal` against `Wait`, which can deliver a signal to a recycled PID.

---

## Risks & open questions

| Risk | Mitigation |
|---|---|
| **Anthropic reverses the policy, without notice.** It has enforced once (Jan 2026) and amended the page twice in six months. | Architectural, not legal: this backend is one adapter behind one interface, it is **never the default**, it is **never on the fresh-install path**, and **no kolk feature is available only here**. If the policy reverses, kolk deletes a package rather than losing a product. Plus §1.6's release-time hash check and §3.5's fail-closed policy frames. |
| **The Commercial-ToS conditions attach and nobody executed them.** | §1.6 makes it a named human acceptance criterion on PLAN.md item 4, not a code gate that cannot express it. |
| **Everything marked ○ in §3 is schema-derived, not observed** — the four error subtypes, `permission_denied`, `compact_boundary`, `model_refusal_fallback`, the SIGINT exit code, `structured_output`'s shape. | S1's priority-1 fixtures cost **$0** and four are already observed; priority 2 costs under 15¢ and settles the rest. Capture before the translator ships. |
| **`--effort` may be clamped silently by org policy**, with no wire signal at all under stream-json. | `/effort` prints `(requested)`, permanently, on this backend. `system/init.effort` is **not** available in a `-p` stream and must not be relied on. |
| **The exit code after SIGINT is undocumented.** | The ladder degrades correctly either way (rung 2 fires on the grace timer), and `claude-cancelled.ndjson` (~1¢) settles it before shipping. |
| **`terminal_reason` is effectively an open enum** (the vendor constructs `api_error_*` values). | `strings.HasPrefix` matching plus a `FinishOther` + `WarnUnknownFrame` default, never an exact table. |
| **`supersedes` is unrepresentable** on an append-only bus. | Do not evict; emit the replacement, warn, record the retracted uuids for offline reconciliation. **Open, flagged to item 19.** |
| **The `vendor` struct is designed against a sample size of one.** | Accepted. Codex at v0.4 will force a field or two; a small refactor then beats a speculative abstraction now. |
| **Two kolk sessions in one repo, or a future fan-out, produce uncoordinated agents and drain one quota N times faster.** | Machine-wide concurrency default of 1; `rate_limit_event` writes `RateAccount` cooldowns to the existing process-durable `Cooldowns` port; fan-out refuses above `utilization > 0.90`. |
| **The committed fixtures cannot be produced by kolk's production argv.** | Reclassified as **tolerance** fixtures (which is exactly what they are good for), with `claude-isolated.ndjson` as the contract fixture. Said plainly in the fixture README. |
| **Open: does `--json-schema` populate `result.structured_output` under `stream-json`?** Documented only for `--output-format json`. | Used opportunistically with a prose fallback; the planner role does **not** route here by default in v0.x. A 2¢ fixture settles it. |
| **Open: does `--agents` accept a per-agent `model` key?** Decides whether item 14's per-task routing survives at all in v0.5's agent mode. | A free parse-only probe settles it when agent mode is scoped. |

---

## Sources

**Vendor policy, all fetched or re-fetched 2026-08-22:**
- [Claude Code — Legal and compliance](https://code.claude.com/docs/en/legal-and-compliance) — the
  authentication/credential section, the two "offer Claude Code in your products" conditions, and the
  "Using the Claude Code name and logo" paragraph.
- [Claude Agent SDK — overview](https://code.claude.com/docs/en/agent-sdk/overview) — the
  no-third-party-login sentence, the subprocess instruction, and the branding allow/deny lists.
- [Run Claude Code programmatically (headless)](https://code.claude.com/docs/en/headless) — `--bare`
  vs the subscription login, cost estimates, `-p`'s Manual default, `system/init.capabilities`,
  SIGINT/SIGTERM, the stream drain.
- [Claude Code — Authentication](https://code.claude.com/docs/en/authentication),
  [Environment variables](https://code.claude.com/docs/en/env-vars) (the `ANTHROPIC_API_KEY`
  precedence sentence), [Permissions](https://code.claude.com/docs/en/permissions),
  [Sessions](https://code.claude.com/docs/en/sessions),
  [Agent SDK cost tracking](https://code.claude.com/docs/en/agent-sdk/cost-tracking).
- [Anthropic Consumer Terms](https://www.anthropic.com/legal/consumer-terms) — effective 2025-10-08.
- [Codex app-server](https://developers.openai.com/codex/app-server),
  [Codex Authentication](https://developers.openai.com/codex/auth),
  [OpenAI Terms of Use](https://openai.com/policies/terms-of-use/) (retrieved via a text-extraction
  proxy; openai.com 403s direct fetches — flagged as one hop removed).
- [gemini-cli `docs/resources/tos-privacy.md`](https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/resources/tos-privacy.md).

**Enforcement precedent:** [opencode#7410](https://github.com/anomalyco/opencode/issues/7410)
(2026-01-09, the rejection string and the live tool-name bisection) ·
[opencode#18186](https://github.com/anomalyco/opencode/pull/18186) (2026-03-19, "anthropic legal
requests") · [opencode providers docs](https://opencode.ai/docs/providers) (the current notice) ·
[HN 46549823](https://news.ycombinator.com/item?id=46549823),
[46602689](https://news.ycombinator.com/item?id=46602689),
[47069300](https://news.ycombinator.com/item?id=47069300) (the February wording, quoted verbatim by a
third party — secondary source, flagged) · Cline's shipping `claudeCodePath` provider.

**Measured on this machine, 2026-08-22, at zero subscription quota** (`--help`, parse-time errors and
one 404 model lookup; `total_cost_usd: 0`, `modelUsage: {}`): `claude 2.1.240`; the `--verbose`
requirement (exit 1, zero stdout); `--effort`'s five levels and its warn-and-continue behaviour;
`--fallback-model`'s comma-separated list; `--permission-mode`'s six documented choices;
`--safe-mode --setting-sources ""` actually suppressing hooks/plugins/MCP and overriding
`permissionMode`; `claude auth status --json`'s **seven** keys including three PII fields and **no**
`apiKeySource`; the empty-prompt shape (8 hook frames, no init, no result, exit 1); the unknown-model
shape (`result{is_error:true, api_error_status:404, modelUsage:{}}` +
`assistant{is_api_error_message:true}` + exit 1 with a clean result); `codex-cli 0.147.0` with
`exec --json`, `-C/--cd`, `-m`, `-s/--sandbox`, `--output-schema <FILE>`, `exec resume`, and
`codex login status` → `Logged in using ChatGPT`, exit 0, no `--json`.

**Repo inputs:** `docs/research/subscription-auth.md` · `docs/plan/02-architecture.md` §2, §5, §7,
§12 · `docs/plan/03-provider-layer.md` §1, §4, §5, §8, §10, §11 ·
`spec/testdata/foreign/{claude-plain.ndjson, claude-tool-use.ndjson, README.md}` (26 real frames,
Claude Code 2.1.240, `apiKeySource:"none"`, captured 2026-08-22) · `docs/research/dashboard.md` §4, §7.
