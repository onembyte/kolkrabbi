# 3. Provider layer — one interface, three adapters, N presets

Status: hardened on 2026-08-22 · supersedes: — · PLAN.md item 3

## Decision (the short version)

`internal/provider` exposes **one interface** — `Chat{Stream, Capabilities, Close}` returning a
closable, pull-based `Stream` of a **flat `Event` union** — implemented by exactly **three
adapters**: `openrouter` (HTTP+SSE, the primary), `openaicompat` (the shared OpenAI-compatible
engine driven by a data-only `Dialect` table: Ollama · LM Studio · vLLM · llama.cpp · LiteLLM ·
Vercel AI Gateway · generic), and `agentcli` (spawns the user's **own** logged-in `claude`/`codex`
through the injected L0 `Spawner`). Everything else is **data**: presets, dialects, model profiles,
the retry table. Retry / rotation / budget / recording live in **L4 `internal/engine`**, driven by
one **pure** `provider.Decide()` decision table — nothing in L3 sleeps, records a span, reads a
file, or knows what a turn is.

Six properties are load-bearing and are the reason this shape was chosen over the two alternatives:
**(1)** a turn's opaque reasoning bytes enter a `Message` *only* on the terminal `EventFinish`, so
an aborted stream can never brick a session on disk; **(2)** content and tool-argument deltas are
concatenated as **raw JSON-escaped bytes** and unmarshalled once at block close, so a rune split
across two SSE frames cannot be silently corrupted into `U+FFFD`; **(3)** every count is a pointer
and every cost carries a `CostSource` + `Measurement`, so *unknown*, *zero* and *free* are three
different facts; **(4)** `Capabilities` is a value on the interface carrying `ExecutesOwnTools`,
`HistoryOwned`, `ModelSelection`, `IdempotentConnect` — so `agentcli` is expressible without a
second interface and without silent zero values; **(5)** a stream that already emitted content is
**never replayed**, only rotated as a new step or surfaced; **(6)** exactly one `EventUsage` is
emitted per attempt by every adapter on **every** terminal path — success, mid-stream error, stall,
cancel — so no call is invisible to the dashboard.

**Native provider keys (Anthropic/OpenAI/Google direct) are out of v0.x**, but the hole is pre-cut
(`Request.Extra`, `Message.Blocks`, `ContentPart.Cache`, `Tool.Cache`) so adding `provider/anthropic/`
later is an addition, not a re-cut. **A second gateway is a preset, not an adapter.** **Per-model
quirks live in the catalog, not the binary** — a cached HTTP document plus a generated `//go:embed`
seed, refreshed by `kolk models --refresh` on a 12 h mtime TTL, never on the startup path.

> **Verification honesty.** No OpenRouter API key exists in this environment, so
> **`/chat/completions` was never called**. Every streaming claim below comes from
> `https://openrouter.ai/docs/openapi/openapi.yaml` (the authority; the prose pages lag it) and the
> doc pages, cross-checked against live **unauthenticated** probes of `GET /api/v1/models`
> (421 models, 22 free, 2026-08-22) and `GET https://ai-gateway.vercel.sh/v1/models` (352 models).
> Everything that could not be observed is listed in **Risks & open questions** and is marked
> *unverified* at the point of use, not smoothed over.

---

## Spec

### 1. The Go interface and type set

Paste-ready. Files map 1:1 onto §2's layout. Doc comments carry the invariants that are not
recoverable from the signature.

#### 1.1 `internal/provider/provider.go` — the contract

```go
// Package provider is the seam between kolk's engine and every model backend:
// the OpenRouter gateway, any OpenAI-compatible base URL (hosted or local), and
// external agent CLIs that run their own tool loop.
//
// Layer L3 (docs/plan/02-architecture.md §5): stdlib only, no os/exec, no file
// or environment access, and NO telemetry types — no Span, no Recorder, no turn
// or role concepts. Everything this package needs from the outside arrives as an
// injected port: Spawner, CatalogStore, Overlay, Cooldowns, Clock.
//
// Three rules govern every adapter in this package:
//
//  1. An adapter is stateless with respect to the conversation. Parallel
//     subagents share one Chat across goroutines, so nothing that varies per
//     session or per turn may live on the Chat value. (eino deprecated
//     ChatModel.BindTools for exactly this race.)
//  2. An adapter never decides policy. It classifies failures into Kind and
//     reports capability; retry, rotation, budget and recording are L4's job.
//  3. An adapter never publishes to the bus. It returns Events; only
//     internal/engine/events.go constructs a protocol.Event.
package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Chat is the entire backend contract. Two verbs and a teardown; anything a
// backend can do that is not "run one turn" is a separate interface obtained
// from the registry (Catalog), never a widening of this one and never an
// optional interface that call sites must type-assert.
type Chat interface {
	// Stream starts one assistant turn. It returns as soon as the backend has
	// committed to producing output — HTTP response headers received, or the
	// child process spawned — or fails outright. All content arrives on the
	// returned Stream. A non-nil error here is always a *Error and means
	// nothing was streamed; note that it does NOT always mean nothing happened
	// upstream (see Capabilities.IdempotentConnect).
	Stream(ctx context.Context, req *Request) (Stream, error)

	// Capabilities reports what this backend can do with this model. It MUST
	// NOT block on the network: adapters answer from the injected Catalog, the
	// compiled-in Overlay, a cached behavioural probe, or Unknown. Unknown is a
	// legitimate answer and a first-class UX state, never a coin flip.
	Capabilities(ctx context.Context, model string) Capabilities

	// Close releases backend-owned resources: idle HTTP connections, or a
	// spawned agent-CLI child and its process group. Idempotent. It is on the
	// base interface deliberately, so no call site ever type-asserts for a
	// lifecycle method (the mistake fantasy's kronk provider and langchaingo's
	// ReasoningModel both make).
	Close() error
}

// Stream is a pull cursor over one turn, in the bufio.Scanner shape.
//
// Concurrency contract — read this before touching it:
//
//	Every method is called from ONE goroutine, the caller's. There is no
//	internal pump and no channel. Close() is NOT safe to call concurrently with
//	Next() and must not be documented as an unblocking mechanism: net/http's
//	body.Read holds b.mu across the blocking read (transfer.go), and Close takes
//	the same mutex, so a "concurrent Close to unblock a parked Next" deadlocks
//	until TCP gives up. THE ONLY THING THAT UNBLOCKS A PARKED READ IS CANCELLING
//	THE REQUEST CONTEXT. Ctrl-C, the idle watchdog and a budget stop all resolve
//	to ctx cancellation; Close() then runs on the owning goroutine after Next()
//	has returned false.
//
// Delivery contract:
//
//	- Next reports false exactly once, after the last Event.
//	- Err is nil on a clean finish and *Error otherwise. It is normal to have
//	  consumed real Events and then get a non-nil Err: partial output is always
//	  delivered, never discarded.
//	- Exactly one EventUsage is emitted per stream, on EVERY terminal path
//	  (success, mid-stream error, stall, cancel, spawn failure), even when every
//	  count is nil. A turn with no usage frame is a bug.
//	- Close is idempotent and safe from a defer at any point on the owning
//	  goroutine.
type Stream interface {
	Next() bool
	Event() Event
	Err() error
	Close() error
}

// Catalog is the model-metadata service. It is a separate interface obtained
// from the registry by name (provider.NewCatalog), not an optional interface on
// Chat: some backends have no catalog at all, and `kolk models` must not
// type-assert. Backends without one return ErrUnsupported from every method.
type Catalog interface {
	// Models serves cache-first. It performs network I/O only when
	// CatalogOptions.Refresh is set or the cache is missing.
	Models(ctx context.Context, opt CatalogOptions) ([]ModelInfo, error)

	// Endpoints returns per-endpoint routing stats for one model: throughput,
	// latency, uptime and per-endpoint price. This is what makes item 8's fast
	// lane a latency decision rather than a price heuristic wearing a latency
	// label. Fetched lazily, only for fast-lane candidates, cached 1 h.
	Endpoints(ctx context.Context, modelID string) ([]Endpoint, error)

	// Credits reports the account's spend/limit state, so a 402 or a free-tier
	// cap can be surfaced with a number instead of a guess.
	Credits(ctx context.Context) (*Credits, error)

	// Reconcile resolves the authoritative usage for a call by its generation
	// id, after the fact. It is the ONLY recovery path when a stream dies before
	// its usage frame arrives, and it is why GenID is captured on every call.
	// Backends without a generation record return ErrUnsupported.
	Reconcile(ctx context.Context, genID string) (Usage, error)
}
```

#### 1.2 `internal/provider/provider.go` — Request and conversation types

```go
// Request is everything one turn can ask for. Every field is a REQUEST:
// adapters translate what they can, drop what they cannot, and MUST announce
// each drop as a Warning on EventStart. Nothing here is ever stored on the Chat.
type Request struct {
	Model string
	// Fallbacks is a priority-ordered alternates list the BACKEND tries itself
	// (OpenRouter's `models` array) — one HTTP request, a server-side hop.
	// Backends without it drop it and warn; L4's client-side rotation is the
	// portable equivalent and solves a different problem (see §4).
	Fallbacks []string

	Messages   []Message
	Tools      []Tool
	ToolChoice ToolChoice

	Reasoning       *Reasoning
	MaxOutputTokens int
	Temperature     *float64 // nil = provider default; 0 is a real temperature
	TopP            *float64
	Stop            []string
	Format          *ResponseFormat

	Cache   CachePolicy
	Routing *Routing

	// SessionID is the prompt-cache / sticky-routing key ONLY (OpenRouter
	// `session_id`). It is kolk's own session id and is NEVER a vendor
	// conversation handle: kolk session ids look like 20060102-150405-1f3a and
	// `claude --session-id` wants a UUID. Overloading these two breaks agentcli
	// on turn one. The vendor handle is ProviderState.
	SessionID string

	// ProviderState is an opaque, adapter-owned continuation handle round-tripped
	// through internal/session as bytes and never interpreted above L3:
	// agentcli's vendor session uuid (--resume), and later a Responses-API
	// conversation id. Empty on the first turn.
	//
	// Lifecycle rules, which are part of the contract:
	//   - session.fork CLEARS it (two kolk sessions must never write into one
	//     vendor conversation).
	//   - session export REDACTS it.
	//   - a handle the backend does not recognise (expired, or the session was
	//     resumed on a second machine) is a SOFT restart: the adapter drops it,
	//     emits Warning{Code: WarnHistoryLost} and proceeds. Never a hard error.
	ProviderState json.RawMessage

	// Budget is the per-run spend/step envelope, consulted by L4 before each
	// Stream. Exhaustion is KindBudgetExhausted so it flows through the same
	// decision table as a 402 instead of being special-cased at every call site.
	Budget *Budget

	// Extra is the per-adapter escape hatch: keys are adapter names, values are
	// merged into that adapter's request body. Present from v0.1 so that adding
	// a native Anthropic adapter later is an addition, not a re-cut. RULE: only
	// internal/cli may populate it, from config. The engine never does.
	Extra map[string]json.RawMessage

	IncludeRaw bool // emit EventRaw per undecoded wire frame; --debug only
}

// Message is the canonical in-memory conversation type. Its JSON tags happen to
// match the OpenAI wire shape, which is a convenience for the scripted mock, not
// a contract: the real wire structs are unexported inside each adapter, and the
// on-disk transcript type becomes session.Message at architecture §12 step 10.
//
// NOTE ON TAGS: no field here is tagged `json:"-"`. While provider.Message is
// still the on-disk session type, a `json:"-"` reservation silently drops
// whatever an adapter puts in it on Save() and it never comes back on resume —
// which is the only reason the slot is wanted in the first place.
type Message struct {
	Role       string     `json:"role"` // system | user | assistant | tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`

	// Parts, when non-nil, supersedes Content: images, PDFs, and per-block
	// prompt-cache breakpoints. Adapters that cannot express parts flatten the
	// text parts and emit WarnParamDropped.
	Parts []ContentPart `json:"parts,omitempty"`

	// Reasoning is display text. ReasoningDetails is the provider's own
	// reasoning-block array, held as RECEIVED BYTES and re-emitted byte-for-byte.
	//
	// OpenRouter's rule, verbatim: "When providing reasoning_details blocks, the
	// entire sequence of consecutive reasoning blocks must match the outputs
	// generated by the model during the original request; you cannot rearrange or
	// modify the sequence of these blocks."
	//
	// Three invariants follow, and all three are enforced, not hoped for:
	//
	//  a. NEVER unmarshal-into-struct-and-remarshal. `"text": null`,
	//     `"signature": null` and an absent `index` do not survive that trip, and
	//     re-emitting `""` where the model sent `null` is a modification of the
	//     block. json.RawMessage in, json.RawMessage out, from wire to disk and
	//     back.
	//  b. This field is assigned ONLY from the terminal EventFinish, never
	//     accumulated from mid-stream deltas. A stream that did not reach a clean
	//     finish yields NO reasoning_details — an unsigned or half-encrypted block
	//     echoed back is a permanent upstream 400 written to disk, which bricks
	//     the session for every future resume.
	//  c. ReasoningModel records which model produced these bytes. Any request
	//     builder MUST strip Reasoning and ReasoningDetails from every message
	//     whose ReasoningModel differs from the model now being called. Signed
	//     Anthropic thinking blocks replayed to a different upstream are invalid
	//     by the provider's own stated rule; model rotation and server-side
	//     `models[]` fallback both trip this.
	Reasoning        string          `json:"reasoning,omitempty"`
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
	ReasoningModel   string          `json:"reasoning_model,omitempty"`

	// Blocks is an ordered, opaque, PERSISTED list of the backend's own native
	// content blocks, filled and re-emitted verbatim by adapters that need it.
	// Content / ToolCalls / ReasoningDetails are the derived flat view for
	// adapters that do not.
	//
	// It exists because three parallel bags cannot reconstruct an interleaved
	// thinking → text → tool_use → thinking → tool_use sequence, which is the
	// NORMAL shape of an agentic Anthropic turn under the
	// interleaved-thinking-2025-05-14 beta. No adapter writes it in v0.1; it is
	// the slot that makes a native Anthropic adapter additive instead of a
	// re-cut. BlocksFormat names the dialect that produced them so a reader
	// never misinterprets one dialect's array as another's.
	Blocks       []json.RawMessage `json:"blocks,omitempty"`
	BlocksFormat string            `json:"blocks_format,omitempty"`

	// Truncated marks an assistant message whose stream did not finish cleanly.
	// A truncated assistant message is persisted (the user already saw the text),
	// but it must never be the last message of the NEXT request unmarked — see
	// §3 step 12.
	Truncated bool `json:"truncated,omitempty"`
}

type ContentPart struct {
	Kind  string          `json:"kind"` // text | image_url | image_b64 | file | opaque
	Text  string          `json:"text,omitempty"`
	URL   string          `json:"url,omitempty"`
	MIME  string          `json:"mime,omitempty"`
	Ref   string          `json:"ref,omitempty"` // blob digest; bytes live beside the session, never inline
	Cache CacheHint       `json:"cache,omitempty"`
	Raw   json.RawMessage `json:"raw,omitempty"` // pre-encoded part, passed through untouched
}

// ToolCall is the request/response shape. Index is a STREAM-ONLY reassembly
// cursor: OpenRouter's ChatToolCall has no index field and sending one is a wire
// lie, so adapters strip it on the way out. It is `json:"-"` here because it is
// meaningless on disk and `omitempty` would drop index 0 while keeping index 1+,
// producing an asymmetric transcript no reader can explain.
type ToolCall struct {
	Index    int          `json:"-"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"` // "function"
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // JSON-encoded string, as on the wire
}

type Tool struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
	// Cache marks this tool definition as a prompt-cache breakpoint. Since the
	// tools array must be resent on every round of a tool loop, the last tool
	// definition is a legitimate breakpoint placement for adapters that need
	// explicit markers (native Anthropic). Gateway adapters that have an
	// automatic top-level mode ignore it.
	Cache CacheHint `json:"cache,omitempty"`
}

type FunctionDef struct {
	Name        string          `json:"name"` // <=64 chars, [A-Za-z0-9_-]
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema, DRAFT-7 EXPRESSIBLE
	Strict      bool            `json:"strict,omitempty"`
}

type ToolChoice struct {
	Mode string // "" (provider default) | none | auto | required | function
	Name string // when Mode == "function"
}

// Effort is the unified reasoning dial. The vocabulary is deliberately
// OpenRouter's and the Vercel AI SDK v4's, character for character.
type Effort string

const (
	EffortUnset   Effort = ""
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

// EffortOrder is DESCENDING, matching OpenRouter's supported_efforts ordering.
// It is the axis ReasoningSupport.Project walks when a model does not accept the
// requested level.
var EffortOrder = []Effort{EffortMax, EffortXHigh, EffortHigh, EffortMedium,
	EffortLow, EffortMinimal, EffortNone}

type Reasoning struct {
	Effort    Effort
	MaxTokens int  // mutually exclusive with Effort on the wire
	Exclude   bool // reason, but do not return the trace
}

// CacheHint marks one breakpoint. CacheAuto at the CachePolicy level is the
// correct answer for an agent loop: the gateway advances the breakpoint as the
// conversation grows, and a hand-rolled per-block scheme blows the cache on
// every tool round. Per-block hints exist because the native Anthropic Messages
// API has no auto mode — placement there is the caller's decision, and an
// adapter that received only a boolean would have to invent a placement policy
// inside L3 with no view of the session.
type CacheHint uint8

const (
	CacheNone CacheHint = iota
	CacheEphemeral
	CacheEphemeral1h
)

type CachePolicy struct {
	Auto      CacheHint // top-level automatic breakpoint (gateway adapters)
	MinTokens int       // suppress caching below the model's minimum (1024/2048/4096)
}

// Routing carries gateway preferences in KOLK's vocabulary, never a gateway's
// wire enum. This is not fussiness: OpenRouter sorts by price|throughput|latency|
// exacto and Vercel AI Gateway by cost|ttft|tps, so passing one's word through to
// the other is a WRONG VALUE IN A FIELD THE SERVER READS, not an ignored extra.
// Each adapter translates or warns.
type Routing struct {
	Prefer          RoutePreference
	Partition       string // "model" (default: fallbacks stay fallbacks) | "none"
	Only, Ignore    []string
	MaxPricePerMTok float64
	RequireZDR      bool
	DenyTraining    bool
	RequireParams   bool
}

type RoutePreference uint8

const (
	RouteDefault RoutePreference = iota
	RouteCheapest
	RouteFastest
	RouteLowestLatency
	RouteMostReliable
)

type ResponseFormat struct {
	Kind   string // "text" | "json_object" | "json_schema"
	Name   string
	Schema json.RawMessage
	Strict bool
}

// Budget is the per-run envelope. Nil means unbounded.
type Budget struct {
	RemainingUSD      *float64
	RemainingRequests *int
	RemainingTokens   *int64
}
```

#### 1.3 `internal/provider/event.go` — the stream union

```go
package provider

// EventType is the closed vocabulary every adapter emits. It is the Vercel AI
// SDK's v2→v4 stream-part list (unchanged across three majors, independently
// reproduced by charmbracelet/fantasy) plus two members that exist so a backend
// which runs its own tools is expressible rather than exceptional.
//
// Every member maps onto a protocol event in 02-architecture §7, so
// internal/engine/events.go is a switch, not a translation layer. THERE IS NO
// SECOND PATH TO A UI: if a member is not worth publishing, it is not worth
// being in this union. A renderer never reads a provider.Event.
type EventType uint8

const (
	EventStart          EventType = iota + 1 // exactly one, first. Carries Warnings.
	EventResponseMeta                        // model / provider / gen id, as soon as known
	EventTextStart
	EventTextDelta
	EventTextEnd
	EventReasoningStart
	EventReasoningDelta
	EventReasoningEnd
	EventToolInputStart // id + name known, arguments still streaming  → tool.delta
	EventToolInputDelta // one fragment of the arguments JSON          → tool.delta
	EventToolCall       // COMPLETE and json.Valid                     → tool.requested
	EventToolResult     // outcome of a tool the BACKEND ran           → tool.output/finished
	EventUsage          // one accounting ROW; a turn may carry several
	EventFinish         // exactly one, last, on a clean stream
	EventError
	EventRaw // only when Request.IncludeRaw
)

// Event is one frame of a turn. Flat with a type tag, deliberately: a sealed Go
// interface costs a hand-written MarshalJSON per member plus a global type
// registry — fantasy pays 28 KB for exactly that in its content package and then
// keeps its own stream part flat to avoid paying it twice on the hot path.
// Hot-path fields are values; cold-path payloads are pointers, so a text delta
// allocates nothing beyond its string.
type Event struct {
	Type EventType

	// Seq is a per-stream monotonic counter assigned by the adapter. It is what
	// makes an interleaved reasoning/text/tool stream replayable in order and
	// what the bus's own seq derives from.
	Seq int

	// ID is block identity: text block, reasoning block, or tool-call id. Deltas
	// of one block share it. Empty when the backend has no block identity (most
	// OpenAI-compatible servers), in which case type ordering is the grouping.
	ID string

	// Text carries EventTextDelta, EventReasoningDelta and EventToolInputDelta.
	// It is ALWAYS a whole number of runes: the adapter holds back an incomplete
	// trailing UTF-8 sequence or JSON escape until the next frame completes it
	// (see jsonstr.go). The authoritative text is assembled separately from raw
	// bytes; this field is for display.
	Text string

	Tool     *ToolCall     // EventToolInputStart, EventToolCall
	Result   *ToolResult   // EventToolResult
	Usage    *Usage        // EventUsage
	Finish   *Finish       // EventFinish
	Err      *Error        // EventError
	Response *ResponseMeta // EventResponseMeta
	Warnings []Warning     // EventStart

	// ProviderExecuted marks an EventToolCall the BACKEND already ran. The
	// engine must render and record it and must NOT call tools.Execute on it.
	// Its outcome arrives later as an EventToolResult with the same ID. Two
	// distinct members are required because `claude -p` separates the
	// announcement from the outcome by the tool's entire runtime — a single
	// combined member either lies with an empty output or hides a 30-second
	// command behind a spinner.
	ProviderExecuted bool

	Raw json.RawMessage // EventRaw, and the verbatim frame under IncludeRaw
}

// ToolResult is the OUTCOME of a tool the backend executed itself.
type ToolResult struct {
	CallID  string
	Name    string
	Output  string
	IsError bool
}

// ResponseMeta is emitted as soon as it is known, not folded into Finish —
// which is the only way a silent server-side fallback ("you asked for model A,
// model B answered") reaches the UI before the answer does, and the only way
// cost is attributed to the model that actually ran.
type ResponseMeta struct {
	Model        string // the model that actually served the request
	ProviderName string // X-Provider-Name / openrouter_metadata
	GenID        string // X-Generation-Id — the join key for cost reconciliation.
	                    // ONE NAMESPACE ONLY: this is a gateway generation id and
	                    // nothing else. A vendor conversation handle goes in
	                    // Finish.ProviderState.
	Attempt      int    // >1 means a server-side fallback fired
}

// Finish ends a clean turn.
type Finish struct {
	Reason FinishReason
	Raw    string // the provider's own string, kept beside the normalised one.
	                // Ollama passes done_reason through untouched, so "load" and
	                // "unload" reach the client: this is an OPEN enum.

	// ReasoningDetails is the complete, in-order, byte-exact reasoning block
	// array for the assistant message the engine is about to persist. It exists
	// ONLY here, never on a delta — see Message.ReasoningDetails invariant (b).
	ReasoningDetails json.RawMessage

	// ProviderState is the backend's updated continuation handle, persisted by
	// internal/session as opaque bytes and fed back on the next Request.
	ProviderState json.RawMessage
}

type FinishReason uint8

const (
	FinishUnknown FinishReason = iota
	FinishStop
	FinishToolCalls
	FinishLength // truncated by max output tokens — NOT a complete answer
	FinishContentFilter
	FinishError
	FinishCancelled
	FinishOther
)

// Warning is how a backend says "I did not do what you asked". It is a
// first-class part rather than a log line because it is the only honest way for
// agentcli to report that it dropped the tool list and the model choice, and for
// a local server to report that the effort dial did nothing here. Every Warning
// becomes exactly one protocol `log{level:"warn"}` frame with a CLOSED code, a
// field name and a was→became pair — never free-form JSON a client must guess at.
type Warning struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Was    string `json:"was,omitempty"`
	Became string `json:"became,omitempty"`
	Detail string `json:"detail,omitempty"`
}

const (
	WarnToolsDropped     = "tools_dropped"      // backend cannot accept tool schemas
	WarnToolsUnverified  = "tools_unverified"   // Tri==Unknown; sending anyway
	WarnModelIgnored     = "model_ignored"      // backend picked its own model
	WarnModelRotated     = "model_rotated"      // L4 rotated after a failure
	WarnEffortClamped    = "effort_clamped"     // projected onto this model's vocabulary
	WarnEffortDropped    = "effort_unsupported" // no reasoning dial on this backend
	WarnCacheUnsupported = "cache_unsupported"
	WarnHistoryTruncated = "history_truncated"  // backend reads only the last user turn
	WarnHistoryLost      = "history_lost"       // ProviderState not recognised; soft restart
	WarnFallbackIgnored  = "fallback_ignored"   // no server-side models[] here
	WarnUsageUnavailable = "usage_unavailable"
	WarnCostUnavailable  = "cost_unavailable"
	WarnParamDropped     = "param_dropped"
	WarnToolCallDropped  = "tool_call_truncated" // args were not valid JSON at stream end
	WarnToolIDRewritten  = "tool_id_rewritten"   // empty or colliding id, adapter assigned one
)
```

#### 1.4 `internal/provider/usage.go` — unknown ≠ zero ≠ free

```go
package provider

// CostSource says HOW CostUSD was obtained. It exists because the dashboard must
// never chart "not reported" as $0.00, and because a leaderboard ranked on
// cost-per-outcome would otherwise rate every local model as infinitely
// efficient. CostFree is a separate value from CostUnknown on purpose: local
// models and :free models are exactly the models item 8's fresh-install defaults
// are built from, and a genuine zero must be chartable.
type CostSource uint8

const (
	CostUnknown        CostSource = iota // nobody told us — render "—"
	CostReported                         // in-stream usage.cost (OpenRouter)
	CostHeader                           // response header (LiteLLM x-litellm-response-cost)
	CostFollowup                         // needs Catalog.Reconcile to be exact
	CostPriceTable                       // computed locally from catalog pricing
	CostVendorEstimate                   // an agent CLI's client-side total_cost_usd
	CostFree                             // genuinely zero: local endpoint or 0/0 pricing
)

// Measurement is the comparability tag, and it applies to LATENCY and TOKENS as
// well as cost. Without it the leaderboard silently pools an agentcli span's
// 1–3 s of Node process startup with an HTTP TTFT while dropping its NULL token
// counts — the same model appears in one view and not another with no visible
// reason, which is the classic way a local dashboard loses the user's trust.
// Every dashboard view states which measurement classes it includes.
type Measurement uint8

const (
	MeasureUnknown   Measurement = iota
	MeasureMetered               // provider-reported per-token counts + metered cost
	MeasureEstimated             // vendor lump sum / locally derived
	MeasureLocal                 // local endpoint: real tokens, no money
)

// Usage is ONE accounting row. A turn may produce several: `claude -p`'s result
// frame carries a per-model breakdown because one turn routinely spans Sonnet
// for the main loop plus Haiku for compaction and titling, and an OpenRouter
// server-side fallback changes model mid-turn. Attributing a sub-model's spend
// to the main one makes item 17's leaderboard simply wrong, so Model is per row
// and rows are never merged.
//
// Every count is a pointer because "the provider did not report cached tokens"
// and "the provider reported zero cached tokens" are different facts that item
// 17 charts differently. This is the exact correction the Vercel AI SDK shipped
// between its v2 and v3 provider specs.
type Usage struct {
	Model        string // WHICH model these counts belong to
	ProviderName string
	GenID        string

	InputTokens       *int64
	CachedInputTokens *int64
	CacheWriteTokens  *int64
	OutputTokens      *int64
	ReasoningTokens   *int64
	TotalTokens       *int64

	CostUSD     *float64
	CostSource  CostSource
	Measurement Measurement

	// TTFT is measured LOCALLY, to the first CONTENT event — not the first byte.
	// Keep-alive comments, role-only chunks and stream_start are not first
	// tokens. Both clocks come from the injected Clock so enginetest can pin them.
	TTFT    time.Duration
	Elapsed time.Duration

	Raw json.RawMessage // the provider's own usage object, untouched
}

func I64(v int64) *int64     { return &v }
func F64(v float64) *float64 { return &v }
func Int(p *int64) int       { if p == nil { return 0 }; return int(*p) }
func Float(p *float64) float64 { if p == nil { return 0 }; return *p }

// Meta is the FLATTENED accounting view: what the cost footer prints and what
// stats.Record stores today. It is DERIVED from []Usage, never the other way
// round. The first five fields are frozen — the prototype's footer() and
// stats.Append read them by name and three e2e tests assert on them.
//
// Deprecated by design: Meta.Cost cannot express "not reported" and will read
// 0 for a local model. It is deleted at architecture §12 step 12, when
// internal/stats becomes a bus subscriber and reads usage.reported directly.
// Until then it is a documented, dated redundancy — put the deletion in the
// step-12 PR description, not in a TODO.
type Meta struct {
	Model            string // as REQUESTED
	PromptTokens     int
	CompletionTokens int
	Cost             float64
	Elapsed          time.Duration

	ResponseModel string // as SERVED; differs when a fallback fired
	ProviderName  string
	GenID         string
	TTFT          time.Duration
	Finish        FinishReason
	RawFinish     string
	ErrorKind     Kind
	Attempt       int
	Rotated       []string
	Warnings      []Warning
	Usage         []Usage // the pointer-typed truth, one row per model
}
```

#### 1.5 `internal/provider/capability.go` — tri-state and the effort projection

```go
package provider

// Tri is three-valued because capability genuinely is. A vLLM or llama.cpp
// server CANNOT tell you whether tool calling works: it is a server launch flag
// (--enable-auto-tool-choice --tool-call-parser), not a model property, and
// /v1/models carries no signal for it. Unknown must be a first-class UX state —
// "I'll try; if the server isn't configured for tools I'll say so" — not a bool
// that guesses.
type Tri uint8

const (
	Unknown Tri = iota
	No
	Yes
)

func (t Tri) OK() bool { return t == Yes }

// CapSource records where a fact came from, so `kolk doctor` can explain itself
// and a behavioural probe can outrank a stale catalog. Higher wins.
type CapSource uint8

const (
	CapNone     CapSource = iota
	CapPreset             // adapter default, compiled in
	CapEmbedded           // //go:embed seed (models.dev projection)
	CapCatalog            // the live, disk-cached gateway catalog
	CapProbe              // observed from this endpoint, this session
	CapUser               // config override — always wins
)

type ModelSelection uint8

const (
	ModelFree      ModelSelection = iota // any catalog id
	ModelAliasOnly                       // claude -p --model sonnet|opus|haiku|<full-name>
	ModelFixed                           // whatever the backend is logged into
)

type ReasoningStyle uint8

const (
	ReasoningNone ReasoningStyle = iota
	ReasoningToggle
	ReasoningEffort
	ReasoningBudget
)

// ReasoningSupport is the per-model effort vocabulary, and it is DATA. The live
// data shows at least eight distinct vocabularies plus {"type":"toggle"} (1,123
// models) and {"type":"budget_tokens"} (204) — catwalk hardcodes
// ["low","medium","high"] for every reasoning model and is wrong for most of
// them. kolk's four-level dial therefore cannot be sent through verbatim.
type ReasoningSupport struct {
	Style     ReasoningStyle
	Efforts   []Effort // DESCENDING; nil with ReasoningEffort means "all accepted"
	Default   Effort
	Mandatory bool // the model REJECTS effort "none" — never send it
	MinTokens int
	MaxTokens int
	// Field is which JSON key this backend puts reasoning text in: "reasoning"
	// (OpenRouter) or "reasoning_content" (llama.cpp, vLLM, DeepSeek). models.dev
	// calls this `interleaved`; 867 models carry it and nobody else models it.
	Field string
}

// Project maps a requested effort onto what THIS model accepts and reports
// whether the projection was lossy, so the caller emits WarnEffortClamped. It
// lives in L3 because the layer that clamps must be the layer that warns: if L4
// clamps first, the adapter never learns what was asked for and the warning is
// structurally unreachable — which is exactly item 7's headline failure mode
// ("the dial silently no-ops on the majority of models with no diagnostic").
//
// Pure and table-tested. budget is non-zero only for ReasoningBudget models.
func (r ReasoningSupport) Project(want Effort) (got Effort, budget int, lossy bool)

type CacheMode uint8

const (
	CacheModeNone     CacheMode = iota
	CacheModeImplicit           // OpenAI >=1024 tok, Gemini 2.5+, DeepSeek, Groq — nothing to send
	CacheModeExplicit           // needs cache_control markers
)

type CacheSupport struct {
	Mode      CacheMode
	Auto      Tri // the gateway places and advances the breakpoint itself
	MinTokens int // below this a breakpoint is a no-op (Anthropic: 1024/2048/4096)
	MaxTTL    time.Duration
}

// Capabilities is the per-model quirk profile, returned as a VALUE on the
// interface. Never an optional interface: widening Chat for capability forces a
// type assertion at every call site (fantasy's kronk Provider, langchaingo's
// ReasoningModel). And never ONLY a separately-injected catalog: agentcli and a
// bare Ollama have no catalog and must still be able to answer "do you run your
// own tools, do you own the history, can I choose the model, do you report
// tokens, do you report cost".
type Capabilities struct {
	Backend string
	Model   string

	Streaming        Tri
	Tools            Tri
	ParallelTools    Tri
	ToolChoice       Tri
	StructuredOutput Tri
	Vision           Tri

	Reasoning ReasoningSupport
	Cache     CacheSupport

	// MaxInputTokens MUST be populated wherever the catalog knows it. It is what
	// lets the engine fail a 4 MB tool result LOCALLY with a typed error instead
	// of writing an unsendable message to disk and 400ing forever.
	MaxInputTokens  int
	MaxOutputTokens int

	UsageReported Tri
	CostSource    CostSource
	Measurement   Measurement

	// ── the backend-shape facts. Every other backend leaves these at their zero
	// value, which is the ordinary case; agentcli is the one that needs them.
	// They are declared, never inferred from a silent zero.

	// ExecutesOwnTools inverts the tool contract: the backend runs its own tools
	// and will refuse yours. The engine reads exactly this one flag to decide
	// whether to run a tool loop or act as an observer, and it MUST NOT branch on
	// len(msg.ToolCalls) != 0 as agent.go:runLoop does today.
	ExecutesOwnTools bool
	// AcceptsToolSchemas is false when the backend takes tool NAMES at best
	// (claude -p --allowedTools) and cannot take a JSON-Schema list.
	AcceptsToolSchemas bool
	// HistoryOwned means the backend owns conversation state and reads only the
	// newest user turn; Request.Messages is advisory. The engine needs this
	// BEFORE it builds the request — a warning at stream start is too late to
	// decide whether resuming a kolk session on this backend is meaningful.
	HistoryOwned bool
	// AcceptsFallbackList: does Request.Fallbacks reach the server?
	AcceptsFallbackList bool
	// EchoesReasoning: must the caller round-trip Message.ReasoningDetails?
	EchoesReasoning bool
	// IdempotentConnect: is re-issuing a request that failed BEFORE any content
	// part safe? True for HTTP backends. FALSE for agentcli, which mutates
	// vendor-side state at system/init — "nothing was published yet" does not
	// mean "nothing happened". L4 must not connect-retry when this is false.
	IdempotentConnect bool

	ModelSelection ModelSelection

	Pricing   Pricing
	Source    CapSource
	FetchedAt time.Time
}

type Pricing struct {
	PromptUSDPerTok     float64
	CompletionUSDPerTok float64
	CacheReadUSDPerTok  float64
	CacheWriteUSDPerTok float64
	ReasoningUSDPerTok  float64
	Free                bool
}
```

#### 1.6 `internal/provider/errors.go` — the typed taxonomy

```go
package provider

// Kind is kolk's normalised failure vocabulary. Every adapter maps its own
// dialect into it — OpenRouter's 27-value `error_type` enum, llama.cpp's
// {code:int, message, type}, a claude subprocess exit status — and the policy
// table below branches ONLY on this. Nothing above L3 ever pattern-matches an
// error string, and NOTHING ANYWHERE regex-matches provider prose: fantasy
// carries three growing context-overflow regexes and Crush then compares an
// English message literal inside its agent loop. If a fact is not typed on the
// wire it belongs in the capability overlay, not in a regex.
type Kind uint8

const (
	KindUnknown Kind = iota
	KindCanceled        // the user cancelled; exit 130, never an error the user is told about
	KindStalled         // OUR idle watchdog fired. Distinguished from Canceled via
	                    // context.WithCancelCause — otherwise a provider hang is
	                    // reported as "interrupted" and the retry path is unreachable.
	KindAuth            // 401, or an agent CLI that is not logged in
	KindPermission      // 403 region / data-policy / ZDR exclusion
	KindCredits         // 402 insufficient credits. Terminal. Never rotated.
	KindRateLimit       // 429, ENDPOINT-scoped (upstream capacity). Rotation helps.
	KindQuotaExhausted  // 429, ACCOUNT-scoped (free rpm/rpd). Rotation does NOT help.
	KindOverloaded      // 503 / 529
	KindUnavailable     // 502
	KindTimeout         // 504
	KindTransport       // dial / TLS / reset / unexpected EOF / http2
	KindContextOverflow // the prompt does not fit
	KindOutputLimit     // max_tokens hit as a request-level error
	KindTruncated       // stream ended with no terminal frame, or finished with nothing
	KindModelNotFound   // slug gone, deprecated, free variant ended
	KindNoEndpoints     // 404: no provider can serve this under these constraints
	KindInvalidRequest  // 400 we caused
	KindModeration      // content policy violation
	KindRefusal
	KindToolsUnsupported // tools were sent to an endpoint that cannot run them
	KindBudgetExhausted  // kolk's own per-run budget, not the provider's
	KindBackendMissing   // no `claude` on PATH
	KindBackendLogin     // `claude auth status --json` says loggedIn:false
	KindServer           // 500 / unmapped
)

// Phase is the single most important field on an error.
type Phase uint8

const (
	PhasePreflight Phase = iota // nothing was sent
	PhaseConnect                // sent; headers/spawn done; NO content part escaped yet
	PhaseCommitted              // at least one content part reached the caller
)

// RateScope splits the one status code that most needs splitting. OpenRouter's
// free-tier limits (20 req/min always; 50/day under $10 of lifetime purchases,
// 1000/day at >=$10) are billed to the KEY across all :free variants, so
// rotating between free models cannot evade them. Upstream capacity exhaustion
// is per endpoint, and rotation is exactly the fix. Both arrive as HTTP 429 with
// the same body shape.
type RateScope uint8

const (
	RateNone RateScope = iota
	RateAccount
	RateEndpoint
)

type Error struct {
	Kind    Kind
	Phase   Phase
	Status  int    // HTTP status, 0 when there was none
	Code    string // the provider's own code, verbatim (error_type, availability.code, …)
	Message string

	Backend  string
	Model    string
	Provider string
	GenID    string

	Retryable  bool // the PROVIDER's own verdict when it has one
	                // (error.availability.retryable); otherwise derived from Kind.
	RetryAfter time.Duration
	RateScope  RateScope
	ResetAt    time.Time // X-RateLimit-Reset, when present

	// Alternates are model slugs the provider itself suggests
	// (error.availability.fallback_models). Hints, not guarantees: they ORDER
	// L4's rotation chain, they do not replace it.
	Alternates []string

	// ExcludedBy is the fixed vocabulary from error.availability.excluded_by:
	// geo, data_region, data_policy:zdr, data_policy:training, max_price,
	// context_length, require_parameters, quantization, allowed_providers.
	ExcludedBy []string

	ContextUsed int // populated on KindContextOverflow wherever the provider
	ContextMax  int // reports it, so the compactor knows how much to shed.

	// Partial is the assistant message assembled before the failure, non-nil
	// whenever Phase == PhaseCommitted. It is RETURNED, not discarded: the user
	// already watched that text appear, and a transcript that disagrees with the
	// terminal is a data-integrity bug.
	Partial *Message

	Headers map[string]string
	Raw     json.RawMessage // the provider's error object, verbatim
	cause   error
}

// Error keeps the numeric status IN THE STRING on purpose: a human reading a log
// wants it, and internal/provider/client_test.go asserts err.Error() mentions
// "401". Format: "openrouter: HTTP 401 (authentication): invalid api key".
func (e *Error) Error() string
func (e *Error) Unwrap() error
func (e *Error) Is(target error) bool // sentinels compare on Kind only

func KindOf(err error) Kind    // errors.As, then .Kind
func AsError(err error) *Error // wraps anything into *Error, never nil for non-nil input

// Sentinels for errors.Is. Kind-only comparison.
var (
	ErrAuth, ErrCredits, ErrRateLimited, ErrQuotaExhausted, ErrContextOverflow,
	ErrModeration, ErrTruncated, ErrCanceled, ErrStalled, ErrToolsUnsupported,
	ErrBudgetExhausted, ErrBackendMissing, ErrBackendLogin, ErrUnsupported *Error
)

// DefaultRetryable is the ONE policy table all adapters defer to. Adapters parse
// their own error shapes; the transient/permanent decision is made here, once.
func DefaultRetryable(k Kind) bool

// StatusKind is the last resort when a body carries no typed code.
func StatusKind(status int) Kind
```

#### 1.7 `internal/provider/policy.go` — the decision table as a pure function

```go
package provider

// Action is what L4 does next. The LOOP is in internal/engine, not here: only
// the engine knows whether tokens already reached the user, whether this role
// may rotate, which models this turn has tried, and what the budget is. L3
// decides, L4 acts. Consequently NOTHING in this package sleeps, retries,
// records a span, or wraps a Chat in another Chat.
//
// DO NOT ADD provider.Retry(Chat) LATER. A retrying decorator re-runs the whole
// step from scratch; fantasy's own docs warn that "the retried response is
// appended to the partial content from the failed attempt" unless the consumer
// resets. kolk publishes to an append-only bus with monotonic seq, so a silent
// replay corrupts the event log and every resumed client.
type Action uint8

const (
	ActFail     Action = iota // surface to the user; stop the turn
	ActRetry                  // same model, after Delay
	ActRotate                 // next candidate model, as a NEW step
	ActCompact                // shrink the context, then retry once
	ActContinue               // keep the partial answer and end the turn softly
)

// AttemptState is everything Decide needs about where we are. Pure input.
type AttemptState struct {
	Attempt           int  // 1-based, for this model
	Rotations         int  // models tried so far this turn
	Committed         bool // a content part already escaped to the caller
	Pinned            bool // the user chose this model explicitly (-m / /model)
	IdempotentConnect bool // from Capabilities; gates connect-phase retry
	Compacted         bool // context was already shrunk once this turn
	HasAlternates     bool // a rotation candidate exists that passes the filters
}

type Policy struct {
	MaxAttempts  int           // per model; 1 disables retry (the enginetest value)
	MaxRotations int           // 0 disables rotation
	BaseDelay    time.Duration // 400ms
	MaxDelay     time.Duration // 20s
	// MaxWait caps how long kolk will sit on a server hint. A Retry-After: 45
	// freezes a terminal for 45 seconds; past this we rotate instead of sleeping.
	MaxWait time.Duration // 8s
	// Rand supplies FULL JITTER. nil = no jitter, which is what enginetest uses.
	// fantasy has no jitter at all, which synchronises retries across parallel
	// subagents into a thundering herd.
	Rand func() float64
	// AllowPaidEscalation permits rotating from a :free chain onto a paid model.
	// It is a MONEY decision, so it is opt-in and never true on a fresh install.
	AllowPaidEscalation bool
}

// DefaultPolicy: MaxAttempts 3, MaxRotations 3, 400ms base, 20s cap, 8s MaxWait,
// jitter on, no paid escalation. The ZERO value is {MaxAttempts:0} = no retry,
// no rotation — so a test that forgets to configure a policy gets one attempt.
func DefaultPolicy() Policy

type Decision struct {
	Act      Action
	Delay    time.Duration
	Reason   string   // shown to the user unless Silent
	Silent   bool     // first-attempt backoff on a short bucket: say nothing
	Prefer   []string // ordering hint for rotation, from Error.Alternates
	Cooldown time.Duration
	Scope    string // cooldown scope key: "model:x" | "endpoint:p/x" | "key:openrouter:free"
	// KeepPartial: persist the partial assistant message, marked Truncated.
	KeepPartial bool
}

// Decide is the entire retry/rotation matrix (§4) as one pure, table-tested
// function. No clock read, no sleep, no goroutine, no I/O.
func Decide(err error, st AttemptState, p Policy) Decision
```

#### 1.8 `internal/provider/collect.go` — the one fold

```go
package provider

// Result is what a drained Stream becomes.
type Result struct {
	// Messages is the transcript to persist, and it is a SLICE because a backend
	// that ran its own tools must persist a faithful
	//   assistant(tool_calls) → tool(result) → … → assistant(text)
	// sequence. A single-message fold silently loses the entire tool history of
	// the one backend that has one: zero tool_calls in the session file, nothing
	// for /rewind, and an agentcli turn that looks like a pure text completion to
	// the dashboard.
	Messages []Message

	// Usage is one row PER ACCOUNTING FRAME, each with its own Model. Rows are
	// never merged.
	Usage []Usage

	Meta Meta
}

// Collect drains a Stream into Result. sink (may be nil) sees every Event before
// it is folded in — that is where internal/engine publishes bus events, so the
// fold itself is written exactly once, here.
//
// req seeds Meta.Model with the REQUESTED model, because several servers report
// no model at all (the scripted mock sets it only on the final usage chunk;
// llama.cpp reports the -m file path) and an empty Meta.Model flows straight into
// stats and prints "[code ·  · 1240ms]".
//
// Collect ALWAYS returns the partial Result alongside a non-nil error. It closes
// the stream on every path with defer, and it reads Err() BEFORE returning —
// never `return r, s.Err()` after `defer s.Close()`, because Go evaluates return
// operands before deferred calls run and a crashed child would be reported as a
// successful empty turn.
//
// Terminal-condition rule, applied here so it is written once for all three
// adapters: a stream that ends with no EventFinish is KindTruncated; and a
// stream that finishes with FinishStop, no text, no tool calls and no reported
// output tokens is ALSO KindTruncated. That one rule covers a killed `claude`
// child, a dropped SSE body, and Ollama's done_reason:"load" warm-up response —
// all three of which otherwise render as a successful empty turn.
func Collect(s Stream, req *Request, sink func(Event)) (Result, error)
```

#### 1.9 `internal/provider/registry.go` — construction and injected ports

```go
package provider

// Config is everything an adapter needs, injected. internal/provider reads no
// file and no environment variable; internal/cli resolves all of this from
// flags > env > project config > user config > defaults and hands it over.
type Config struct {
	Backend string // registry key: openrouter | openaicompat | claude | codex
	Preset  string // openaicompat dialect: ollama|lmstudio|vllm|llamacpp|litellm|vercel|generic
	BaseURL string
	APIKey  string

	// OpenRouter attribution. AppURL becomes HTTP-Referer and is REQUIRED for
	// any attribution at all; AppName becomes X-OpenRouter-Title.
	AppName string
	AppURL  string

	HTTPClient *http.Client
	Spawner    Spawner // agentcli only; nil elsewhere
	Store      CatalogStore
	Overlay    Overlay
	Cooldowns  Cooldowns
	Clock      Clock

	Timeouts Timeouts

	Overrides map[string]Capabilities // CapUser tier, applied last
}

// Timeouts are PER PRESET. A cold Ollama model load or a vLLM CUDA-graph capture
// blows past a 60 s first-byte budget that is correct remotely.
type Timeouts struct {
	Connect   time.Duration // dial + TLS
	FirstByte time.Duration // 60s remote, 600s local
	// Idle is the between-frames liveness signal, and it is armed ONLY around
	// the blocking transport read — never around the whole Next(). A tablet on a
	// bad link or a slow SQLite ingest must not be able to cancel a healthy
	// stream, which would also abort the upstream generation and record a
	// spurious timeout against an innocent model. OpenRouter's
	// ": OPENROUTER PROCESSING" comment lines reset it, which is what they are
	// for. It fires with context.WithCancelCause(errStalled) so a provider hang
	// is KindStalled and never KindCanceled.
	Idle time.Duration
}

// Spawner is the L0 process port, satisfied by internal/shell. It is an
// interface so internal/provider never imports os/exec — which arch_test.go
// makes a CI failure, not a convention — and so agentcli's translation tests run
// with no vendor binary installed.
type Spawner interface {
	Spawn(ctx context.Context, cmd SpawnCmd) (Proc, error)
	LookPath(name string) (string, error)
}

type SpawnCmd struct {
	Path string
	Args []string
	Dir  string
	// Env is an EXPLICIT ALLOW-LIST over a CLEARED environment. Never
	// os.Environ(). About 35 variables must be actively absent —
	// ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL,
	// CLAUDE_CODE_USE_BEDROCK/_VERTEX/_FOUNDRY, AWS_*,
	// GOOGLE_APPLICATION_CREDENTIALS, GCLOUD_PROJECT — or the child silently
	// leaves the subscription path and bills the user's API account instead of
	// the Max plan they asked kolk to use. This is an L0 API REQUIREMENT on
	// internal/shell, not a convenience.
	Env []string
	// Stdin carries the prompt. NEVER argv: argv is world-readable in the
	// process list and this tool gets pointed at private repos.
	Stdin []byte
}

type Proc interface {
	Stdout() io.Reader
	Stderr() io.Reader
	// Wait reaps and returns the exit status. It is called on the READ path when
	// the stream ends, not only from Close.
	Wait() (exitCode int, err error)
	Kill() error // kills the process GROUP
}

// CatalogStore is the injected byte cache. L3 does not know what a file is; L5
// (internal/config) implements this over paths + lock + atomicfile. Errors from
// it are ALWAYS advisory: being offline is routine and the fallback is sound.
type CatalogStore interface {
	Load(ctx context.Context, key string) (data []byte, modTime time.Time, err error)
	Save(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error // corrupt-cache self-heal
}

// Overlay supplies the two facts no live gateway catalog carries: the per-model
// effort VOCABULARY and which JSON field a server puts reasoning in. Compiled in
// from a generated projection of models.dev; see §5.
type Overlay interface {
	Lookup(model string) (ModelInfo, bool)
}

// Cooldowns is process-DURABLE rate-limit state, keyed by SCOPE and not by
// model. kolk is a ~10 ms fork-exec-run CLI: a per-process rotation ring forgets
// a daily free-tier cap the instant it prints, and every subsequent invocation
// burns one request rediscovering it. Scopes:
//
//	"model:<id>"                  a specific slug is unusable
//	"endpoint:<provider>/<id>"    one upstream endpoint is at capacity
//	"key:<backend>:free"          the ACCOUNT-wide free rpm/rpd cap — which a
//	                              model-keyed cooldown cannot express at all
//
// Cooldowns are ADVISORY and BOUNDED: never applied to an explicitly pinned
// model, capped at the server-stated ResetAt with a 1 h ceiling when none was
// given, listed by `kolk doctor`, cleared by `kolk doctor --clear-cooldowns`.
type Cooldowns interface {
	Until(scope string) (time.Time, bool)
	Mark(scope string, until time.Time, reason string)
	List() []Cooldown
	Clear(scope string) // "" clears all
}

type Cooldown struct {
	Scope  string
	Until  time.Time
	Reason string
}

// Clock is injected so TTFT, elapsed and the idle watchdog are deterministic
// under test. Without it the fake Clock that architecture §5 rule 1 mandates
// stops at the L3 boundary and ttfc_ms — a column item 17 charts and a signal
// item 8's fast lane selects on — can only ever be asserted as > 0.
// Sleeping is L4's job; there is no Sleep here.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) (<-chan time.Time, func() bool)
}

type Factory func(Config) (Chat, error)
type CatalogFactory func(Config) (Catalog, error)

type Entry struct {
	Name       string
	NewChat    Factory
	NewCatalog CatalogFactory // nil ⇒ NewCatalog returns ErrUnsupported
	// Baseline is what Capabilities returns before any catalog lookup: the
	// HONEST FLOOR for a backend, e.g. agentcli's AcceptsToolSchemas:false.
	Baseline Capabilities
	Doc      string
}

// Adapters self-register from init(), so package provider imports none of them
// and there is no import cycle. internal/cli blank-imports the adapters a given
// binary carries — database/sql's shape, and what lets bind/kolkmobile assert it
// never links agentcli.
func Register(e Entry)
func RegisterPreset(name string, c Config) // a preset is DATA, not a package
func Lookup(name string) (Entry, bool)
func Names() []string
func New(cfg Config) (Chat, error)
func NewCatalog(cfg Config) (Catalog, error)
```

#### 1.10 `internal/provider/catalog.go` — the quirk record

```go
package provider

// ModelInfo is one catalog row, deliberately a superset of what any single
// backend returns. THIS OBJECT IS THE PER-MODEL QUIRK PROFILE — the answer to
// "where do per-model quirks live and how are they refreshed without shipping a
// new binary" is "here, in a cached HTTP document".
type ModelInfo struct {
	ID          string
	Name        string
	Description string
	Canonical   string
	AliasTarget string

	ContextLength   int
	MaxOutputTokens int

	SupportedParameters []string
	InputModalities     []string
	OutputModalities    []string

	Reasoning ReasoningSupport
	Cache     CacheSupport
	Pricing   Pricing

	Moderated       bool
	Deprecated      bool
	KnowledgeCutoff string
	ExpiresAt       string

	FetchedAt time.Time
	Source    CapSource
	Raw       json.RawMessage // untouched upstream row
}

func (m ModelInfo) Caps() Capabilities

// Endpoint is one upstream serving a model. This is the fast lane's input.
type Endpoint struct {
	Provider           string
	ContextLength      int
	MaxOutputTokens    int
	Pricing            Pricing
	ThroughputTokPerS  float64 // throughput_last_30m
	LatencyMs          float64 // latency_last_30m (TTFT)
	Uptime30m          float64
	SupportsTools      Tri
	Quantization       string
}

type CatalogOptions struct {
	// Refresh forces a network fetch even when the cache is fresh
	// (`kolk models --refresh`). Everything else serves cache-first: kolk's
	// cold-start budget is 30 ms (CI-enforced) and a conditional GET on every
	// invocation is a 100x regression.
	Refresh bool
	Offline bool
}

type Credits struct {
	LimitUSD     *float64
	RemainingUSD *float64
	UsageUSD     float64
	FreeTier     bool
	ResetAt      *time.Time
}
```

#### 1.11 `internal/provider/jsonstr.go` — the split-rune fix

```go
package provider

// Acc accumulates a JSON string that arrives in fragments, WITHOUT ever
// round-tripping a fragment through encoding/json.
//
// THE BUG THIS EXISTS TO PREVENT. Every SSE frame carries a fragment of
// `delta.content` or `function.arguments`. Decoding each fragment into a Go
// string and concatenating destroys data in two ways, both verified on go1.26.4:
//
//	json.Unmarshal(`"\ud83d"`)      → "�"      (lone surrogate half)
//	json.Unmarshal("\"\xf0\x9f\"")  → "��" (split UTF-8 bytes)
//
// The bytes are destroyed INSIDE json.Unmarshal, so concatenating afterwards can
// never recover them. For prose it is a mangled emoji; for a tool-call
// `arguments` fragment it is a corrupted `path` or `command`, i.e. write_file
// creating a file named with U+FFFD or bash running a mangled command. No test
// in the current suite would catch it: the scripted mock only ever emits ASCII.
//
// THE FIX. Adapters decode `content` and `arguments` as json.RawMessage, which
// preserves the source bytes verbatim including invalid UTF-8 and lone escapes.
// Acc appends the ESCAPED interior of each fragment (the bytes between the
// quotes) and unmarshals ONCE, at block close, when the sequence is complete.
// Both failure modes then round-trip correctly: `\ud83d` + `\ude80` unmarshals to
// U+1F680, and "\xf0\x9f" + "\x9a\x80" is valid UTF-8 by the time json sees it.
type Acc struct{ /* escaped bytes + a display cursor */ }

// Append takes one JSON string TOKEN (with its surrounding quotes) exactly as it
// appeared on the wire.
func (a *Acc) Append(raw json.RawMessage) error

// String unmarshals the whole accumulated token once. Call at block close.
func (a *Acc) String() (string, error)

// Flush returns the display-safe text produced since the last Flush: everything
// up to the last COMPLETE rune and the last COMPLETE escape sequence. An
// incomplete trailing escape (`\`, `\u`, `\ud83d` awaiting its low surrogate) or
// an incomplete UTF-8 sequence is held back for the next frame. This is what
// makes Event.Text always a whole number of runes.
func (a *Acc) Flush() string

func (a *Acc) Len() int
```

#### 1.12 `internal/provider/fake.go` — why policy is HTTP-free to test

```go
package provider

// Fake is a scripted Transport in the NON-test build, so internal/enginetest and
// internal/engine can use it too. A policy test is a table of []Event and
// []error, not an httptest.Server, a goroutine or a wall clock.
//
// Calls is guarded by a mutex: item 14's parallel subagents share one Chat, and
// an unguarded append is a data race as well as nondeterminism.
type Fake struct {
	Script [][]Event
	Fails  []error // parallel to Script; non-nil fails that call at connect time
	Caps   Capabilities

	mu    sync.Mutex
	Calls []*Request
}

var _ Chat = (*Fake)(nil)
```

---

### 2. File-by-file package layout

Consistent with `02-architecture.md` §2. Everything at the top level is a **sibling file inside
package `provider`** — naming the files the tree already implies — plus **one additive leaf**
(`catalog/`) and **two amendments to §2**, both stated openly at the end.

```
internal/provider/                     L3 · stdlib only · no os/exec · no file/env · no telemetry types
├── provider.go      Chat · Stream · Catalog ifaces · Request · Message/ContentPart/Tool/ToolCall ·
│                    Effort · Reasoning · CachePolicy/CacheHint · Routing · Budget.  §1.1–1.2
├── event.go         EventType (16) · Event · ToolResult · ResponseMeta · Finish · Warning codes.  §1.3
├── usage.go         CostSource · Measurement · Usage (all-pointer) · Meta (flat, dated).  §1.4
├── capability.go    Tri · CapSource · ModelSelection · ReasoningSupport.Project · CacheSupport ·
│                    Capabilities · Pricing.  §1.5
├── errors.go        Kind (26) · Phase · RateScope · *Error · sentinels · KindOf/AsError ·
│                    DefaultRetryable · StatusKind.  §1.6
├── policy.go        Action · AttemptState · Policy · Decision · Decide().  THE table, pure.  §1.7
├── collect.go       Result · Collect(Stream, *Request, sink).  The one fold.  §1.8
├── registry.go      Config · Spawner/SpawnCmd/Proc · CatalogStore · Overlay · Cooldowns · Clock ·
│                    Entry · Register/RegisterPreset/New/NewCatalog.  init()-registration ⇒ no cycles.  §1.9
├── catalog.go       ModelInfo · Endpoint · CatalogOptions · Credits · profile merge (5 tiers).  §1.10
├── jsonstr.go       Acc — the escaped-bytes accumulator. THE split-rune fix.  §1.11
├── sse.go           Spec-compliant SSE reader, SHARED by both HTTP adapters: skips ':' comment
│                    keep-alives (and resets the idle watchdog on them), JOINS MULTI-LINE `data:`
│                    fields with '\n' before parsing, terminates on [DONE], and uses
│                    bufio.Reader.ReadString — never bufio.Scanner, whose 8 MB ceiling kills a whole
│                    stream with an untyped "token too long" on a 12 MB write_file argument.
├── fake.go          Scripted Chat, non-test build, mutex-guarded.  §1.12
├── provider_test.go Decide table (every Kind × Phase × RateScope × attempt) · Project (all 8 live
│                    effort vocabularies + toggle + budget) · Acc (surrogate + split-UTF-8 fixtures) ·
│                    Collect (multi-message fold, terminal-condition rule).
│
├── catalog/         ← ADDITIVE LEAF (amendment 2 below). Exists only because //go:embed resolves
│   ├── embed.go       relative to the embedding file's directory and I will not put an `embed`
│   │                  import in the package that defines Chat.
│   └── profiles.json  generated by tools/cmd/modelgen from models.dev api.json (MIT):
│                      {id, tool_call, reasoning, reasoning_options, interleaved,
│                       structured_output, limit, cache_min} — nothing else.
│
├── openaicompat/    ★ THE SHARED ENGINE. One OpenAI-compatible HTTP+SSE implementation.
│   ├── client.go      Chat impl: build body, do HTTP, classify non-200, hand the body to stream.go.
│   ├── encode.go      Request → wire body. Content as string|parts; reasoning_details spliced in as
│   │                  RAW BYTES; ToolCall.Index stripped; per-Dialect field stripping (§7).
│   ├── stream.go      Stream impl: chunk → Event. Tool-call reassembly, reasoning buffering, TTFT
│   │                  stamp at the first CONTENT event, mid-stream error → EventError{Committed}.
│   ├── dialect.go     ★ THE SEAM: Dialect{Name, StrictBody, ForceParallelTools, ReasoningField,
│   │                  ToolCallKey, CatalogPath, CatalogDecode, CostFrom, Encode, MapFinish,
│   │                  Classify, Headers, Timeouts}.
│   ├── presets.go     DATA-ONLY Dialect values: generic · ollama · lmstudio · vllm · llamacpp ·
│   │                  litellm · vercel. A table, not seven code paths.
│   ├── models.go      Six catalog decoders (/api/tags · /api/v1/models · /model_group/info ·
│   │                  /props · Vercel /v1/models · bare id list).
│   ├── probe.go       Behavioural tools-unsupported detection (§5), cached per endpoint per session.
│   ├── errors.go      llama.cpp {code:int,message,type} · LiteLLM · bare OpenAI → Kind.
│   └── stream_test.go ← WAS internal/provider/client_test.go. ASSERTIONS UNCHANGED.
│
├── openrouter/      ~300 lines of hooks + catalog over openaicompat. NOT a parallel client.
│   ├── client.go      init()→Register. Builds an openaicompat client with the OpenRouter Dialect:
│   │                  headers, session_id, top-level cache_control, provider routing, models[],
│   │                  reasoning. Captures X-Generation-Id + X-Provider-Name off the response.
│   ├── stream.go      The OpenRouter-only stream extras: reasoning_details merge, openrouter_metadata.
│   ├── models.go      GET /api/v1/models → []ModelInfo (20 top-level fields, 13 pricing keys, the
│   │                  per-model reasoning{} block). Cache-first through CatalogStore.
│   ├── endpoints.go   GET /api/v1/models/{author}/{slug}/endpoints → []Endpoint. Item 8's fast lane.
│   ├── pricing.go     13 pricing keys → Pricing; per-model cache minimums; free detection.
│   ├── generation.go  GET /api/v1/generation?id= → Catalog.Reconcile. Also GET /api/v1/key → Credits.
│   ├── errors.go      The 27-value error_type enum + error.availability (11 codes, retryable,
│   │                  retry_after, fallback_models, excluded_by) + Retry-After + X-RateLimit-* →
│   │                  Kind/RateScope. error_type is read from ALL THREE documented locations (§6).
│   └── oauth.go       PKCE device flow (item 5 owns the UX; the bytes live here).
│
└── agentcli/        ★ spawns the user's OWN logged-in binary, via the injected Spawner.
    ├── agentcli.go    Chat impl: argv, env allow-list, --settings isolation, prompt on stdin,
    │                  >=1 MB NDJSON reader, exit status observed on the READ path, Close→Kill+Wait.
    ├── claude.go      claude stream-json → []Event, as a PURE FUNCTION. No process, no I/O, no clock.
    ├── codex.go       same shape for `codex … --json`.
    ├── detect.go      PURE: given a LookPath result + `claude auth status --json` bytes, decide
    │                  installed / logged-in / version.  ← amendment 1 below
    └── translate_test.go  replays spec/testdata/foreign/**. Offline forever, no vendor binary.
```

**L5 implementations of the injected ports** (they touch disk, so they cannot be in L3):

| Port | Implementation | Location |
|---|---|---|
| `provider.CatalogStore` | flock + atomic temp+rename over `paths.Cache()` | `internal/config/catalogstore.go` |
| `provider.Cooldowns` | JSON file under `paths.Cache()`, flock-guarded | `internal/config/cooldowns.go` |
| `provider.Overlay` | thin wrapper over `internal/provider/catalog` | `internal/config/overlay.go` |
| `provider.Clock` | `time` (prod) / `enginetest.Clock` (test) | `internal/cli`, `internal/enginetest/fakes.go` |
| `provider.Spawner` | `internal/shell` (L0) | `internal/shell/shell.go` |

**Two amendments to `02-architecture.md` §2, both deliberate:**

1. **`agentcli/detect_unix.go` + `detect_windows.go` are dropped in favour of a pure, untagged
   `detect.go`.** §2 lists them, but §1's purity rule says *"no `*_windows.go` / `*_darwin.go` file
   and no `os/exec` import exists anywhere in L1–L5"* and `check-purity.sh` enforces it — so as
   listed, step 14 would fail CI. The OS-divergent half (LookPath, process-group kill, Windows job
   objects) is already `internal/shell`'s job per §8. This is a one-line edit to §2's tree, not a
   redesign, and it is better flagged now than discovered by CI at step 14.
2. **`internal/provider/catalog/` is a new leaf** holding the one `embed` import below L6. `embed`
   is stdlib and the blob is a compile-time constant, so nothing touches the OS at runtime — but the
   layer table is a data file precisely so it is edited deliberately, and `internal/arch/layers.go`
   gets one line for it. (The zero-change alternative is `modelgen` emitting a Go source file with a
   compressed `const`, which is uglier and equivalent.)

**One deviation from what §2's ordering implies, stated openly:** §2 describes `openaicompat/` as
"Ollama / LiteLLM / vLLM behind `--base-url`" and gives `openrouter/` the full file list. This spec
**inverts the weight**: `openaicompat` becomes the shared engine and `openrouter` becomes a hook set
plus a catalog. Same two directories, same filenames, opposite dependency arrow. It is fantasy's
proven decomposition (its `openrouter`, `vercel` and `azure` providers are ~100–120 lines each over
one `openai` engine), and it is what makes LiteLLM, vLLM, llama.cpp, LM Studio and the Vercel AI
Gateway **presets rather than packages**. Tripwire for having put the seam in the wrong place:
**more than two `switch dialect.Name` statements outside `presets.go`.**

---

### 3. Request lifecycle — one streaming tool-calling turn, traced

Code mode, OpenRouter, an Anthropic model with reasoning, one tool call. `→` marks where a bus
event is published; **the engine publishes, the provider never does** (`02-architecture` §5 rule 3).

**T0 · engine builds the Request (L4).**

```go
caps := ch.Capabilities(ctx, model)
if !caps.ExecutesOwnTools && caps.Tools == provider.No && modeExecutesTools {
    return refuseRole(caps)          // §5: refuse the ROLE, not the model
}
msgs := sess.Wire(model)             // strips Reasoning/ReasoningDetails from every message
                                     // whose ReasoningModel != model, and appends a synthetic
                                     // user continuation if the last message is Truncated (T12)
if est := estTokens(msgs); caps.MaxInputTokens > 0 && est > caps.MaxInputTokens {
    return &provider.Error{Kind: provider.KindContextOverflow,
        ContextUsed: est, ContextMax: caps.MaxInputTokens, Phase: provider.PhasePreflight}
}
req := &provider.Request{
    Model: model, Fallbacks: chain, Messages: msgs,
    Tools:     tools.Specs().For(caps),          // omitted entirely when !AcceptsToolSchemas
    Reasoning: &provider.Reasoning{Effort: dial}, // RAW dial: the adapter clamps AND warns
    Cache:     provider.CachePolicy{Auto: provider.CacheEphemeral, MinTokens: caps.Cache.MinTokens},
    SessionID: sess.ID,
    ProviderState: sess.ProviderState,
    Budget:    run.Budget,
}
```

The raw dial is passed down on purpose. If L4 clamps first, the adapter never learns what was asked
for and `WarnEffortClamped` is structurally unreachable — which is precisely item 7's headline
failure mode.

**T1 · `Stream()` establishes transport (L3, `openrouter/client.go`).** Marshals the body, sets the
headers of §6, `POST /chat/completions`. Derives a cancellable context with
`context.WithCancelCause` and arms a `time.AfterFunc(cfg.FirstByte, …)` that cancels with the
sentinel `errStalled` — so a provider hang and a Ctrl-C are distinguishable at classification time
instead of both arriving as `context.Canceled`.

- **Non-200** → read the body (capped at 64 KB, not the current unbounded `io.ReadAll`),
  `errors.go` classifies it (§6), return `(nil, *Error{Phase: PhasePreflight})`. **No `Stream` is
  created.** This is the only failure L4 may freely re-issue, and only when
  `caps.IdempotentConnect`.
- **200** → capture `X-Generation-Id`, `X-Provider-Name`, `Retry-After`, `X-RateLimit-*`; return an
  `*orStream` holding the body, an `sse.Reader`, an `Acc` per open block, a tool-call assembler and
  a reasoning buffer. **No goroutine.** `Next()` reads synchronously on the caller's goroutine, so
  `engine_test.go`'s `runtime.NumGoroutine()` delta-zero assertion holds by construction and there
  is no pump to leak when a turn is abandoned.

**T2 · who does what.**

| Job | Where | Why there |
|---|---|---|
| SSE framing (multi-line `data:`, `:` comments, `[DONE]`) | `provider/sse.go` | shared verbatim by both HTTP adapters |
| escaped-byte accumulation of `content` / `arguments` | `provider/jsonstr.go` | one implementation, one fuzz target |
| tool-call fragment reassembly | `openaicompat/stream.go` | the reassembly KEY differs per backend |
| `reasoning_details` merge | `openrouter/stream.go` | OpenRouter-specific shape |
| assembling the transcript | `provider/collect.go` | one fold for engine, orchestrator and tests |
| publishing bus events | `internal/engine/events.go` | §5 rule 3 |
| retry / rotate / compact / budget / record | `internal/engine` turn loop, via `provider.Decide` | only the engine knows whether tokens reached the user |

**T3 · first frames.** `EventStart{Warnings}` — the accumulated translation losses, **before any
content** → one `log{level:"warn"}` per warning. Then `EventResponseMeta{Model, ProviderName,
GenID}` as soon as the first chunk's `model` field arrives → folded into the attempt's `Usage` row
and, when `Model` differs from what was requested, a `session.updated`. **From this point the turn
is PINNED to the served model**: rounds 2 and 3 must not carry round 1's reasoning to a third model.

**T4 · content.** `delta.content` arrives as `json.RawMessage`; `Acc.Append` stores the escaped
bytes; `Acc.Flush()` yields the rune-complete display prefix → `EventTextDelta` → `message.delta`.
The authoritative text is `Acc.String()` at block close, unmarshalled once.

**T5 · reasoning.** `delta.reasoning` → `EventReasoningDelta` → `reasoning.delta` (display only).
Each `delta.reasoning_details[]` item is appended to an ordered `[]json.RawMessage` buffer **as
bytes**. Two adjacent entries are merged only when they share the same non-nil `(id, index, type)`
triple, concatenating `text` / `summary` / `data`; otherwise appended. This rule is safe under both
readings of an under-specified spec — the docs say *"the complete reasoning sequence is built by
concatenating all chunks in order"* without stating whether `index` is a stable per-block key or a
per-chunk counter. **Unverified; see Risks.** The buffer is emitted **only** on `EventFinish`.

**T6 · tool calls.** A header chunk `{index, id, type, function:{name}}` →
`EventToolInputStart{Tool:{ID, Function:{Name}}}` → `tool.delta`. Each
`{index, function:{arguments:"frag"}}` → `EventToolInputDelta` → `tool.delta`, so a client can
render arguments live and `Last-Event-ID` replay reconstructs them. Three prototype bugs are fixed
here:

- accumulation keys on `index`, **falling back to `id` and then arrival order** when `index` is
  absent (llama.cpp and several gateways omit it; today two parallel index-less calls collapse into
  one — `name:"read_fileread_file"`, `args:"{…}{…}"`);
- `Function.Name` is **set-if-empty, never `+=`** (Mistral and several vLLM parsers repeat the full
  name in every delta; today that produces `"bashbash"` → `unknown tool: bashbash` at
  `tools.go:233`);
- on drain the adapter **guarantees ids**: an empty id becomes `call_<streamSeq>`, and an id
  colliding with one already emitted on this stream is rewritten, both with
  `Warning{WarnToolIDRewritten}`. Without this, two `role:"tool"` messages share a
  `tool_call_id`, and `repairDanglingToolCalls`' `answered map[string]bool` records a genuinely
  unanswered call as answered — permanently invalid history that repair can never fix.

`EventToolCall` (complete, `ProviderExecuted:false`) is emitted at `finish_reason`/`[DONE]`, and
**only if `json.Valid(args)`**; anything still truncated is dropped with
`Warning{WarnToolCallDropped}`. Anything emitted is persisted and must be answerable forever;
anything dropped never reaches disk. → `tool.requested{executed_by:"kolk"}`.

**T7 · usage and finish.** The usage chunk (empty `choices`) → `EventUsage` with all ~15 fields
including `prompt_tokens_details.{cached_tokens,cache_write_tokens}` and
`completion_tokens_details.reasoning_tokens`, `CostSource: CostReported`,
`Measurement: MeasureMetered`, `Model` = the served model → `usage.reported`. Then
`EventFinish{Reason, Raw, ReasoningDetails, ProviderState}` → `turn.finished`.

**T8 · the fold.** `Collect` returns `Result{Messages, Usage, Meta}`. The engine appends the
assistant message (content + tool calls + `ReasoningDetails` + `ReasoningModel` = the **served**
model), saves, then — **only if `!caps.ExecutesOwnTools`** — runs each call through `tools.Execute`
behind the permission `Decider`, and **saves after each tool result** rather than after the loop.

**T9 · the tool round.** One `{role:"tool", tool_call_id, content}` message per call — exactly those
three fields, no `name` (it is not in OpenRouter's `ChatToolMessage` schema). `tools` is resent every
round (OpenRouter validates the schema on each call, which is also why the last tool definition is a
legitimate cache breakpoint). The assistant turn is re-sent as
`{content, tool_calls, reasoning_details}` with the reasoning array **byte-identical**. Dropping it
between a `tool_use` and its `tool_result` on an Anthropic-family model is an upstream 400, not soft
degradation — and it is structurally impossible today, since neither `Message` nor `streamChunk` has
the field.

**T10 · cancellation.** Ctrl-C → `cli` resolves the turn id → `engine`'s
`map[turnID]context.CancelFunc` → the request context is cancelled with no cause → the in-flight
`Read` returns → `Next()` false → `Err()` = `&Error{Kind: KindCanceled, Phase: PhaseCommitted}` →
`Collect` returns the partial `Result` plus that error → `defer s.Close()` closes the response body.
**Closing the body is load-bearing beyond hygiene:** OpenRouter documents that aborting the
connection stops model processing *and billing* on OpenAI, Azure, Anthropic, Fireworks, Together,
DeepInfra, DeepSeek, XAI and Cloudflare. A Ctrl-C that merely stops rendering keeps paying. Exit 130.

**T11 · mid-stream error.** HTTP is already 200 and committed; the error rides inside the stream:

```
data: {"id":"gen-abc","model":"openai/gpt-4o","provider":"OpenAI",
       "error":{"code":429,"message":"Rate limit exceeded",
                "metadata":{"error_type":"rate_limit_exceeded","provider_code":"rate_limited"}},
       "choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}
```

The stream: (1) has already emitted every text and tool-input delta — **the caller has them, they
are on the bus, on screen and in the session; nothing is discarded**; (2) emits
`EventError{Err: &Error{Kind: KindRateLimit, Status: 429, Phase: PhaseCommitted, RateScope,
RetryAfter, Alternates, Partial: &partialMsg}}` → `error`; (3) emits the **mandatory** `EventUsage`
with whatever counts arrived, `GenID` set (the only recovery path when a stream dies before usage —
reconcilable later via `Catalog.Reconcile`); (4) emits `EventFinish{Reason: FinishError}`; (5)
`Next()` returns false and `Err()` returns the same `*Error`.

Both error locations are read: the top-level `error` object **and** `choices[].error` — the
prototype handles a fraction of the first and **silently ignores** the second, returning `err=nil`
with truncated content. `finish_reason:"length"` becomes `KindTruncated` (today a cut-off answer is
indistinguishable from a complete one, to the user *and* the dashboard), and a stream that ends
without `[DONE]` and without a terminal `finish_reason` is `KindTruncated` too (today a dropped
connection returns a *successful* partial answer with zero usage, which is then appended to the
session and fed back to the model as if the assistant had finished).

`Phase == PhaseCommitted` forces `Decide` to `ActRotate` or `ActFail` — **never `ActRetry`**.

**T12 · what happens to the partial.** The engine persists the partial assistant message with
`Truncated: true` — the user already watched that text appear, and a transcript that disagrees with
the terminal is a data-integrity bug. But a `Truncated` assistant message must **never** be the last
message of the next request unmarked: several OpenAI-compatible servers 400 on a trailing assistant
message when `tools` is present, and Anthropic (via OpenRouter) rejects a final assistant block that
ends in trailing whitespace, which a mid-sentence truncation very often does. `repairDanglingToolCalls`
does not help — it returns early because that message has no `tool_calls`.

**Decision: `sess.Wire()` right-trims a `Truncated` assistant message and appends a synthetic user
continuation** — `(the previous reply was cut off — continue from where you stopped)`. Prefill was
rejected because it is not portable: `agentcli` cannot express it at all. Asserted by a resume test.

**T13 · per-attempt accounting.** `internal/engine` writes **one span per physical attempt**, from a
`defer` on every terminal path — success, error, stall, cancel — carrying the attempt number, the
served model, `GenID`, `CostSource`, `Measurement`, `FinishReason`, `ErrorKind` and locally-measured
TTFT. A backoff sleep is never inside a latency measurement. A turn-level row is separate. This is
in L4 by design: putting a `Span`/`Recorder` in L3 both duplicates `engine.Recorder` and makes item
17's schema churn edit the provider layer forever.

**T14 · resume honesty.** Because the session is saved after **each** tool result and a
`{role:"tool", pending:true}` placeholder is written **before** execution, resume can distinguish
"never started" from "started, outcome unknown". `repairDanglingToolCalls` is amended accordingly:
it scans the **whole** message list (not just the last assistant block, which today leaves an older
dangling block permanently invalid while reporting success), and its synthetic text becomes honest —
`"This tool may have already run before the interruption; verify before repeating."` for a pending
call, and the existing text only for a call that never started. Today it states flatly *"Interrupted
before this tool ran"*, which is false for a `git push` that completed, and the model dutifully
re-issues it.

---

### 4. Error taxonomy and the retry / fallback / rotation decision table

#### 4.1 Mapping every dialect into one vocabulary

**OpenRouter** publishes a stable 27-value `ApiErrorType` enum (OpenAPI `ApiErrorType`,
*"Canonical OpenRouter error type, stable across all API formats"*). It hides in **three** places
and a robust parser checks all three, in this order:

1. `error.metadata.error_type` — the documented chat-completions location (provider errors,
   mid-stream chunks, non-streaming interrupted generations);
2. `error.error_type` — the flatter form used by model-availability payloads, alongside
   `error.http_status` and `error.availability`;
3. `choices[].error` — non-streaming requests where a provider error interrupted generation embed
   the error *inside the choice*, with partial content preserved.

| `error_type` | `Kind` |
|---|---|
| `context_length_exceeded`, `token_limit_exceeded`, `string_too_long` | `KindContextOverflow` |
| `max_tokens_exceeded` | `KindOutputLimit` |
| `authentication` | `KindAuth` |
| `permission_denied` | `KindPermission` |
| `payment_required` | `KindCredits` |
| `rate_limit_exceeded` | `KindRateLimit` / `KindQuotaExhausted` (see §4.3) |
| `provider_overloaded` | `KindOverloaded` |
| `provider_unavailable` | `KindUnavailable` |
| `timeout` | `KindTimeout` |
| `not_found` | `KindModelNotFound` |
| `content_policy_violation` | `KindModeration` |
| `refusal` | `KindRefusal` |
| `invalid_request`, `invalid_prompt`, `unprocessable`, `payload_too_large`, `precondition_failed`, all `*image*` | `KindInvalidRequest` |
| `server`, `unmapped` | `KindServer` |

**`error.availability` is a second, additive sub-protocol** and it is consumed directly:
`{code, retryable, retry_after, requested_models[], affected_providers, excluded_by, fallback_models,
constraint{field,detail}, docs_url}`. Codes and their documented retryability:
`model_not_found`(400,no) · `wrong_endpoint`(400,no) · `no_endpoints`(404,no) ·
`model_deprecated`(404,no) · `model_unavailable_upstream`(404,no) · `region_restricted`(403,no) ·
`privacy_restricted`(404,no) · `constraint_filtered`(404,no) · `free_variant_ended`(404,no) ·
**`capacity_exhausted`(429,yes)** · **`temporarily_unavailable`(503,yes)**. OpenRouter's own guidance
is *"Always switch on `availability.retryable`, not just the code"*, and unknown codes are treated as
retryable — so `Error.Retryable` prefers the provider's verdict and falls back to
`DefaultRetryable(Kind)`. `fallback_models` populates `Error.Alternates`; `excluded_by` populates
`Error.ExcludedBy` and is printed verbatim on a `KindPermission`.

**Other dialects, same target.** llama.cpp emits `{code:<int>, message, type}` with
`exceed_context_size_error`(400) / `unavailable_error` / `not_supported_error` — note `code` is a
**number** there and a string in OpenAI, which is why classification is per adapter. LiteLLM re-maps
upstream into OpenAI shape but **raises by default on unsupported params**. Local servers have no 402
and no 429 at all. `agentcli` maps a missing binary to `KindBackendMissing` and
`claude auth status --json → loggedIn:false` to `KindBackendLogin`.

**Transport errors are the ONE permitted string match, and it is confined to
`openaicompat/errors.go` with a comment saying so.** Go's stdlib bundles its own copy of the http2
package whose error types are unexported, so `stream error:` / `connection error:` / `GOAWAY`
fragments plus `errors.Is(err, io.ErrUnexpectedEOF)` are all that is available. It is recorded as an
exception precisely so it does not get copied elsewhere.

#### 4.2 The decision table

`provider.Decide(err, AttemptState, Policy) Decision` — pure, table-tested with no HTTP, no clock
and no goroutine.

**Three rules override every row, and they live in code, not data:**

| # | Condition | Effect |
|---|---|---|
| **R0** | `Kind == KindCanceled` | short-circuit: `ActFail`, `KeepPartial:true`, `Silent:true`, exit 130 |
| **R1** | `Phase == PhaseCommitted` | `Retry` is forced **false** and `KeepPartial` forced **true**. Rotate as a NEW step if the role may rotate and an alternate passes the filters; otherwise fail. Never replay. |
| **R2** | `Phase == PhaseConnect && !st.IdempotentConnect` | `Retry` forced **false** → rotate or fail. A subprocess mutates vendor-side state at `system/init`; *"nothing was published yet"* does not mean *"nothing happened"*. |

| Kind | HTTP | Retry | Max att. | Backoff | Rotate | Cooldown | Surface | Keep partial |
|---|---|---|---|---|---|---|---|---|
| `KindCanceled` | — | ✗ | — | — | ✗ | — | silent (exit 130) | ✓ |
| `KindStalled` | — | ✓ | 2 | none | ✗ | — | after retries: *"provider stalled after Ns"* | ✓ |
| `KindAuth` | 401 | ✗ | — | — | ✗ | — | ✓ `kolk login` / `config set-key` | — |
| `KindCredits` | 402 | ✗ | — | — | **✗ never** | — | ✓ with the balance from `/key` | — |
| `KindPermission` | 403 | ✗ | — | — | ✓ | 1 h (`model:`) | ✓ print `excluded_by` verbatim | — |
| `KindModeration` / `KindRefusal` | 403/400 | ✗ | — | — | **✗ never** | — | ✓ `reasons` + truncated `flagged_input` | — |
| `KindRateLimit` (RateEndpoint) | 429 | ✓ | 3 | hint, else exp+jitter | **✓ first** | 5 min (`endpoint:`) | after the chain | — |
| `KindQuotaExhausted` (RateAccount) | 429 | see §4.3 | 2 | `ResetAt` | **✗ among peers** | until `ResetAt`, ≤1 h default (`key:`) | ✓ with the real remedy | — |
| `KindOverloaded` | 503/529 | ✓ | 3 | hint, else exp+jitter | ✓ after retries | 2 min (`endpoint:`) | after the chain | — |
| `KindUnavailable` | 502 | ✓ | 3 | exp+jitter | ✓ after retries | 2 min | after the chain | — |
| `KindTimeout` | 504 | ✓ | 2 | exp+jitter | ✓ after retries | — | after the chain | — |
| `KindTransport` | — | ✓ | 3 | 250 ms → 1 s → 4 s ±jitter | ✗ | — | after retries | — |
| `KindServer` | 500 | ✓ | 3 | exp+jitter | ✓ after retries | — | after the chain | — |
| `KindContextOverflow` | 400/413 | ✗ | — | — | ✗ | — | ✓ *"context full; compacted N messages"* | **`ActCompact`**, then retry once; if `st.Compacted` → `ActFail` |
| `KindOutputLimit` | 400 | ✗ | — | — | ✗ | — | ✓ | `ActContinue` — engine may raise `max_tokens` or continue |
| `KindTruncated` | — | ✓ **only if `!Committed`** | 2 | exp+jitter | ✗ | — | ✓ *"output truncated"* | ✓ |
| `KindModelNotFound` / `KindNoEndpoints` | 404 | ✗ | — | — | ✓ | 24 h + catalog refresh | if the chain empties | — |
| `KindInvalidRequest` | 400 | ✗ | — | — | ✗ | — | ✓ name the offending parameter — our bug | — |
| `KindToolsUnsupported` | — | ✗ | — | — | suggest only | 1 h (`endpoint:`) | ✓ *"this endpoint has no tool support; try `-m …`"* | — |
| `KindBudgetExhausted` | — | ✗ | — | — | ✗ | — | ✓ exit 3 | ✓ |
| `KindBackendMissing` / `KindBackendLogin` | — | ✗ | — | — | ✗ | — | ✓ `kolk login claude` / `kolk doctor` | — |
| `KindUnknown` | — | ✓ | 2 | exp+jitter | ✓ | — | after the chain | ✓ |

**Backoff:** `BaseDelay(400 ms) << (attempt-1)`, capped at `MaxDelay(20 s)`, with **full jitter**.
Server hints are honoured in this order: `error.availability.retry_after` (the minimum across all
attempted endpoints) → the `Retry-After` header (429 **and** 503) → exponential. A hint longer than
`Policy.MaxWait` (8 s) **cancels the retry and hands the error to rotation** — a `Retry-After: 45`
freezes a terminal for 45 seconds and rotation is almost always the better answer.

**Rotation filters — every candidate must pass all five:**

1. not in this turn's tried-set;
2. not under an active `Cooldowns` entry for `model:` or `endpoint:`;
3. `Capabilities.Tools == Yes` when the role executes tools (three of today's 22 free models have no
   tools, and rotating into one produces a mystifying failure one round later);
4. `Capabilities.MaxInputTokens >= estTokens(history)` — a 402/429 on a 200k-context paid model 40
   turns in must not silently drop onto a 16k free model, which immediately overflows and then
   mutates the user's history to fit a model they never chose;
5. paid ⇔ paid, free ⇔ free, unless `Policy.AllowPaidEscalation` (a **money** decision: opt-in,
   announced, never true on a fresh install).

**`Prefer` from `Error.Alternates` ORDERS the chain; it does not replace it** — OpenRouter's own
words are *"suggestions are hints, not guarantees"*.

**Rotation never overrides an explicit pin.** `-m` / `/model` is a promise; `st.Pinned` forces
`ActFail` with the reason instead of a silent substitution. And every rotation emits
`Warning{WarnModelRotated, Was, Became, Detail}` → one `log{level:"warn"}` line
(`⚠ deepseek-r1:free at capacity → qwen3-coder:free`). **Never silent.**

**Rotation strips reasoning.** Whatever mutates `Request.Model` after the first assistant message
MUST strip `Reasoning` and `ReasoningDetails` from every prior assistant message whose
`ReasoningModel` differs. Signed Anthropic thinking blocks replayed to a different upstream are
invalid by the provider's own rule; this is a 400, not soft degradation. `sess.Wire(model)` does it
centrally so no call site can forget.

**Server-side vs client-side fallback — two mechanisms, and knowing which is the design.**

| | Server-side (`Request.Fallbacks` → OpenRouter `models[]`) | Client-side (`ActRotate`) |
|---|---|---|
| Cost | one HTTP request, zero added latency | a fresh request |
| Fixes | context-length validation errors · moderation flags · rate-limiting · downtime, per OpenRouter's documented trigger list. Provider-level failover *within* one model happens silently first. | the **account** or the **slug** is the problem — which `models[]` cannot fix |
| Detection | `openrouter_metadata.attempt > 1` (opt-in header), or response `model` ≠ requested | `Meta.Attempt` / `Meta.Rotated` |
| Cap | none documented (no `maxItems` in the OpenAPI). The 3-entry cap is for the **Anthropic-skin `fallbacks`** parameter on `/api/v1/messages`, which kolk does not use. Do not conflate them. | `Policy.MaxRotations` (3) |

**`route` is DEPRECATED and kolk must never send it.** The OpenAPI marks `DeprecatedRoute` as
*"Use providers.sort.partition instead"*. The replacement is
`provider: {sort: {by: …, partition: "model"|"none"}}`; `partition:"model"` (default) tries all
endpoints of model #1 before model #2, `partition:"none"` sorts every endpoint of every listed model
together — the "fastest of these N models" knob.

#### 4.3 The free-tier 429, worked through

This is the case the plan asks about explicitly, and the naive answer is wrong.

**The facts.** Free models are capped at **20 req/min always**, and **50 req/day** below $10 of
lifetime purchases or **1000/day** at ≥ $10. These are **per account, across all `:free` variants** —
rotating between free models cannot evade them. Separately, an individual free endpoint can run out
of upstream capacity. Both arrive as HTTP 429 with the same body shape.

**The discriminator, and the default is the opposite of the intuitive one.** *Only platform-limit
429s carry `X-RateLimit-Limit` / `-Remaining` / `-Reset`; successful responses never do.* So:

```
X-RateLimit-* present  &&  Reset  > 60s   → KindQuotaExhausted, RateAccount
X-RateLimit-* present  &&  Reset <= 60s   → KindRateLimit,      RateAccount  (the per-minute bucket)
availability.code == "capacity_exhausted" → KindRateLimit,      RateEndpoint
NEITHER SIGNAL                            → KindRateLimit,      RateEndpoint   ← the default
```

The last line is the correction. Defaulting a signal-less `:free` 429 to "account cap" makes kolk
print *"Free-tier daily limit reached (50/50, resets 00:00 UTC). Add $10 of credit…"* to a user who
has made three requests today — a confidently false diagnostic that sends them to a payment page for
a problem money will not fix, while disabling the one branch that would have worked. Absence of the
headers means **not a platform limit**, so: rotate.

**Transcript — `kolk -e quick` on the free chain `[glm:free, qwen3:free, llama:free]`:**

1. **429, `X-RateLimit-Reset` in 8 s.** Per-minute bucket → `ActRetry`, delay 8 s, `Silent:true` on
   the first attempt. The user sees nothing but a slower first token. Rotating here would burn a
   *second* request against the same 20 rpm key bucket and make it worse.
2. **429 again, reset now 4 h out.** `KindQuotaExhausted` → `ActFail`, and the message names the
   actual remedy, with the numbers taken from `GET /api/v1/key` (`is_free_tier`, `limit_remaining`,
   `usage_daily`) rather than guessed:
   `Free-tier daily limit reached (50/50 requests, resets 03:00 UTC). $10 of lifetime credit raises
   this to 1000/day, or pick a paid model: kolk model anthropic/claude-haiku-4.5`.
   `Cooldowns.Mark("key:openrouter:free", resetAt, "quota_exhausted")` — **persisted**, so the next
   `kolk` invocation does not burn another request rediscovering it. No rotation is attempted,
   because none can succeed. Free → paid is the one legitimate rotation here and it is opt-in.
3. **429 with `availability.code == "capacity_exhausted"`, `fallback_models:["qwen/qwen3:free"]`.**
   `ActRotate` with `Prefer` from the server's own suggestion. `Cooldowns.Mark("endpoint:…", +5m)`;
   the engine records `glm:free` in the tried-set, re-resolves through the five filters, retries
   immediately with `Meta.Attempt = 2`, and prints one line.
4. **Chain exhausted** → the *first* error is surfaced together with `Meta.Rotated`.

**The general rule this encodes:** rotation is for *"this endpoint cannot serve me"*; backoff is for
*"not right now"*; surfacing is for *"no amount of trying will help"*. A 429 is the only status that
can be all three, which is why `RateScope` is a field rather than something inferred at the call site.

---

### 5. Capability and pricing model

#### 5.1 Where quirks live: nowhere in the binary

**The catalog *is* the profile table.** Live probe of `GET /api/v1/models`, 2026-08-22, no key
required: 421 models, each row carrying `supported_parameters` (`tools` 352, `structured_outputs`
335, `reasoning` 288, `reasoning_effort` 144, `parallel_tool_calls` **5**),
`top_provider{context_length, max_completion_tokens, is_moderated}`,
`architecture.input_modalities`, **13 pricing keys** (`input_cache_read` 251, `input_cache_write` 74,
`input_cache_write_1h` 32, `internal_reasoning` 30, …), `knowledge_cutoff`, `expiration_date`,
`alias_target`, `default_parameters`, `per_request_limits`, `benchmarks`, and — new since
`docs/research/openrouter.md` was written — a per-model **`reasoning` object**:

```json
"reasoning": { "mandatory": true, "supported_efforts": ["xhigh","high","medium","low","minimal"],
               "default_effort": "medium", "default_enabled": true, "supports_max_tokens": true }
```

Live counts over those 421 rows: `mandatory` 289, `supported_efforts` 141, `default_enabled` 123,
`supports_max_tokens` 10. Semantics, verbatim from the docs: `supported_efforts` is **descending**;
`null` means all values accepted; **omitted means no effort selection**; `mandatory:true` means *"do
not send `effort:"none"` — the model rejects it."* That is item 7's clamp table, and it ships in a
cached HTTP document, not in a Go file.

Today `ModelInfo` decodes **3 of 20 top-level fields and 2 of 13 pricing keys**, which is why item
8's free-model defaults cannot be computed at all — of the 22 free models live today, **3 do not
support tools**, and that fact is currently invisible.

#### 5.2 Five tiers, merged highest-wins

| Tier | `CapSource` | Source | Refresh |
|---|---|---|---|
| 5 | `CapUser` | `~/.config/kolk/models.json`, dotted keys (`model."x/y".tools=false`) | the user edits it |
| 4 | `CapProbe` | behavioural verdicts observed this session | on observation; session-scoped, **not persisted** |
| 3 | `CapCatalog` | the live gateway catalog, disk-cached | 12 h mtime TTL · background · `--refresh` |
| 2 | `CapEmbedded` | `//go:embed catalog/profiles.json` | ships with the binary |
| 1 | `CapPreset` | the adapter's compiled-in floor (`Entry.Baseline`) | — |
| 0 | `CapNone` | `Tri.Unknown` everywhere | — |

`Capabilities.Source` is on every value **specifically so `kolk doctor` can print why**, and the
probe verdict is deliberately session-scoped rather than persisted: an operator who restarts vLLM
with `--tool-call-parser` must not be shadowed by yesterday's observation.

#### 5.3 Catalog fetch, per backend

| Backend | Endpoint | What it actually carries |
|---|---|---|
| OpenRouter | `GET /api/v1/models` | authoritative: 20 fields + 13 pricing keys + `reasoning{}` |
| OpenRouter | `GET /api/v1/models/{author}/{slug}/endpoints` | `throughput_last_30m`, `latency_last_30m`, `uptime_last_*`, per-endpoint pricing |
| Vercel AI Gateway | `GET /v1/models` | an OpenRouter-class catalog under different names (`context_window`, `pricing.input`, `tags[]`) — 352 models, HTTP 200, **unauthenticated**, verified 2026-08-22 |
| Ollama | `GET /api/tags` (**not** `/v1/models`) | `capabilities[]` (incl. `tools`) + `details.context_length` |
| LM Studio | `GET /api/v1/models` | `max_context_length`, `quantization`, `capabilities.trained_for_tool_use` |
| LiteLLM | `GET /model_group/info` | `max_input_tokens`, costs, `supports_function_calling`, `supported_openai_params` |
| llama.cpp | `GET /props` | `default_generation_settings.n_ctx`, `modalities`, `chat_template_caps` |
| vLLM | `GET /v1/models` | `max_model_len` and nothing else |

**`/v1/models` is not a catalog on five of six targets.** One `Catalog` interface, six decoders, and
**never** a code path that assumes `/v1/models` carries a context length.

#### 5.4 Cache mechanics

- **Location:** `paths.Cache()/kolk/catalog/<backend>-<host>.json`; endpoint stats at
  `…/catalog/endpoints/<slug>.json`. Reached only through the injected `CatalogStore` — L3 does not
  know what a file is.
- **TTL:** 12 h by mtime for models, 1 h for endpoints. Upstream itself signals
  `cache-control: public, max-age=300, stale-while-revalidate=3600, stale-if-error=3600` on a
  **689 KB** payload; 12 h on disk with an explicit `--refresh` is the right trade for a CLI.
- **Never on the startup path.** Crush does a conditional GET inside a 45-second budget on every
  start; it is a long-lived TUI and can afford it. kolk boots in ~10 ms against a **30 ms CI-enforced
  budget** (`scripts/check-budgets.sh`) — a per-invocation round trip is a 100× regression. Serve
  from cache always; refresh in the background **after** a turn completes, or on `--refresh`.
- **ETag is computed client-side over the cached file's bytes**, so there is no second file to
  desync.
- **Atomic temp + rename under a `flock`, with a re-check under the lock.** Several `kolk` processes
  start independently and race; a truncating write lets one read a half-written 689 KB catalog.
  `internal/lock` is already carved for this.
- **Corrupt JSON self-heals**: delete the cache file and fall through, rather than fall over.
- **Refresh errors are logged and advisory, never fatal** — being offline is routine and the
  fallback is sound. An **empty** upstream result *is* an error, because empty is a signal, not a
  network condition.
- **Offline behaviour:** live → disk cache (served even when stale) → embedded seed →
  `Tri.Unknown`. There is no path to "no models".

#### 5.5 The embedded seed, and why models.dev is an overlay and not a source of truth

`tools/cmd/modelgen` (a nested module, never in the CLI's `go.mod`) reads models.dev `api.json`
(4.3 MB, MIT, 193 providers, 7,251 provider×model rows, hourly sync, ETag-revalidated) and projects
it to `{id, tool_call, reasoning, reasoning_options, interleaved, structured_output, limit,
cache_min}` in `internal/provider/catalog/profiles.json`.

models.dev adds exactly **two** things no live gateway catalog carries, and they are both needed:

- **`reasoning_options`** (5,022 rows) — the per-model effort *vocabulary*. The live distribution:
  `{"type":"toggle"}` 1,123 · `["low","medium","high"]` 656 · `["low","medium","high","xhigh","max"]`
  277 · `["minimal","low","medium","high"]` 218 · `{"type":"budget_tokens"}` 204 ·
  `["none","low","medium","high","xhigh"]` 169 · `["none","low","medium","high","max"]` 155 ·
  `["none","high"]` 143. **At least eight distinct vocabularies plus toggle and budget** — this is
  the hard evidence that a four-level dial cannot be sent through verbatim, and why
  `ReasoningSupport.Project` exists and warns.
- **`interleaved`** (867 rows) — *which JSON field this model's server puts reasoning in*
  (`reasoning` vs `reasoning_content` vs `reasoning_text`). llama.cpp, vLLM and DeepSeek all use
  `reasoning_content`. This is the canonical local-model quirk and nobody else models it.

Coverage against the live OpenRouter catalog: **359 of 421**, with 61 of the 62 misses being
`:batch` variants and the only real miss being `openrouter/auto-beta`; **22/22 free models covered**.
But models.dev is owned by `anomalyco`, the **same org as opencode**, and opencode itself consumes
its own mirror rather than models.dev directly — so kolk **vendors a generated snapshot** and treats
it as an overlay, never a hard dependency, never fetched at build time.

Resolution: **live catalog for pricing / context / supported params · models.dev overlay for the
effort vocabulary and the reasoning field name · user config on top.**

#### 5.6 Do not filter at ingestion; do not hardcode effort levels

catwalk drops every model lacking `tools` and everything under 20k context **at generation time**.
That is right for Crush and wrong for kolk: chat mode needs no tools, the fast lane wants tiny cheap
models, and item 8's free defaults must see all 22 free ids. **Filter at query time**
(`kolk models --tools`); keep the catalog complete. Likewise catwalk hardcodes
`["low","medium","high"]` for every reasoning model — §5.5 is the evidence that this is wrong for
most of them.

#### 5.7 A model that lacks tools

**Detection ladder, cheapest first:**

1. **Gateway catalog** — `supported_parameters ∋ "tools"` (OpenRouter), `tags ∋ "tool-use"`
   (Vercel). Free, authoritative, already fetched. → `Yes` / `No`.
2. **Local native catalog** — one extra call per server (§5.3). → `Yes` / `No`.
3. **No signal exists** for vLLM and llama.cpp, structurally: tools are a **server launch flag**.
   → `Unknown`.

**Policy: refuse the ROLE, never the model, and never silently pseudo-tool.**

- **chat / planner / synthesizer / critic / fast lane** send no tools; nothing degrades; a no-tools
  model is perfectly good in all of them. **Capability is per role, not per session.**
- **code / agent worker** refuse at turn start with an actionable message and a suggested substitute
  (`KindToolsUnsupported`). Three reasons: the vendors themselves document prompted tool calling as
  unreliable (LM Studio: *"models that were not trained for tool use may output improperly formatted
  tool calls"*; vLLM: *"arguments may occasionally be malformed or violate the function's parameter
  schema"*) and kolk's code-mode tools **mutate the user's filesystem**; item 17's entire premise is
  comparing models on cost-per-good-outcome, and a silently different execution path poisons the
  leaderboard; and the degradation already exists one layer down (LM Studio's "default" tool mode,
  llama.cpp's Generic handler), implemented by people who own the chat template — re-implementing it
  wraps a prompt around a prompt.
- **Prompted tools are opt-in only**: `--pseudo-tools`, one strict JSON protocol, **max 1 call per
  turn**, permission forced to *ask* even under `--yolo`, and a **mandatory `tools=prompted` stats
  tag** so the dashboard never pools prompted and native runs.
- **`Unknown`** → send them with `Warning{WarnToolsUnverified}`, then detect **behaviourally** in
  `openaicompat/probe.go`: tools were sent, `tool_calls` is empty, `finish_reason == "stop"`, and
  `content` matches a known tool syntax (a fenced `json` block with `name`/`arguments`, `<tool_call>` tags,
  pythonic `[fn(a=1)]`) ⇒ `KindToolsUnsupported`, cached per endpoint for the session, **with the
  reason**: *"this vLLM server was started without `--enable-auto-tool-choice --tool-call-parser`."*
  That verdict is a capability fact and a stat — never a regex over model output used as control flow.

#### 5.8 A model that reports no usage or no cost

`Capabilities.UsageReported` and `CostSource` say so **before** the turn. The mandatory `EventUsage`
is still emitted, with every count `nil`, `CostSource: CostUnknown`, and only `TTFT`/`Elapsed` filled.
Downstream:

- The footer prints `[code · llama3 · 1240ms]` with no token or cost segment — today's `footer()`
  already guards on `> 0`, so **no change**.
- `stats.Record` writes `omitempty` columns; the dashboard renders **"—", never `$0.00`**.
- If pricing is known and counts are known but cost is not → compute locally, `CostPriceTable`,
  `MeasureEstimated`. **The price table is keyed on `(backend, model)`, never on the model id
  alone** — `claude-sonnet-4-5-20260514` and `llama3.1:8b` can collide with catalog rows after alias
  resolution, and fabricating a priced cost for a subscription-billed or local turn is the same
  leaderboard poisoning by a different route. A row already sourced `CostVendorEstimate` is **never**
  re-priced.
- A local endpoint with 0/0 pricing → `CostFree` + `MeasureLocal`, so a genuine zero is chartable and
  distinct from an unreported one. Otherwise the flagship rating-per-$ view silently excludes exactly
  the models a zero-config install runs on.
- `GenID` is captured on **every** call and stored on the stats row. It is the join key for
  `Catalog.Reconcile` → `GET /api/v1/generation?id=`, which is the only source of `provider_name`,
  `native_finish_reason`, `native_tokens_cached/reasoning`, `cache_discount`, and the
  `latency` vs `generation_time` split that is exactly the TTFT-vs-total pair the dashboard wants —
  **and the only recovery path when a stream dies before its usage frame.** `internal/dash` runs a
  backfill pass over rows whose `cost_source` is `followup`.

---

### 6. OpenRouter adapter specifics

#### 6.1 Headers

**Request, always:**

| Header | Value | Note |
|---|---|---|
| `Authorization` | `Bearer <key>` | |
| `Content-Type` | `application/json` | |
| `Accept` | `text/event-stream` | |
| `HTTP-Referer` | `https://github.com/onembyte/kolkrabbi` | **REQUIRED for any attribution at all.** Today `NewClient` sets `AppName` but nothing ever assigns `AppURL`, so `HTTP-Referer` is never sent and kolk's attribution is 100% non-functional. |
| `X-OpenRouter-Title` | `kolk` | The current name. `X-Title` is still accepted for back-compat but is not what kolk sends. For localhost referers the title header is *also* required. |

**Request, conditionally:**

| Header | When |
|---|---|
| `x-anthropic-beta: structured-outputs-2025-11-13` | only when `Tool.Strict` is set. Without it OpenRouter strips `strict` from tools and routes normally. (For `response_format.type:"json_schema"` the header is applied automatically — strict *response format* is free, strict *tools* needs the opt-in.) |
| `x-anthropic-beta: interleaved-thinking-2025-05-14` | reserved; not sent in v0.x |
| `X-OpenRouter-Metadata: enabled` | under `--debug`, or when the engine needs to detect that a server-side fallback fired. Adds `openrouter_metadata{requested, strategy, attempt, endpoints{…}}` on the final chunk before `[DONE]`. **Never present on cache hits or 500s.** (The legacy alias `X-OpenRouter-Experimental-Metadata` is still accepted; kolk sends the current name.) |

**Response, captured on every call:** `X-Generation-Id`, `X-Provider-Name` (both CORS-exposed:
`access-control-expose-headers: X-Generation-Id,X-Provider-Name,request-id,cf-ray`, verified live),
`Retry-After`, `X-RateLimit-Limit/-Remaining/-Reset`. None of these is read today.

#### 6.2 The request body

**Always sent:** `model`, `messages`, `stream: true`, **`session_id`**.

`session_id` is the whole prompt-caching strategy's other half. A cache lives on one provider
endpoint, and OpenRouter pins subsequent requests to it. The **default** sticky key is a hash of
*the first system/developer message + the first non-system message* — and kolk's system prompt is
rebuilt on every construction from live `os.Getwd()` and project-memory file contents
(`agent.go:102-107`), so the default key changes whenever cwd or `KOLKRABBI.md` changes and the
cache silently never hits. `session_id` (top-level body field, ≤ 256 chars; header `x-session-id`
also accepted, body wins) **overrides** the hash. With it, stickiness activates on the first
successful request rather than only after a hit is observed. Sticky sessions expire after 10 minutes
of inactivity, reset per successful request, and are **disabled entirely when `provider.order` is
set** — which is one more reason `Routing.Only` is exposed but `order` is not.

**Sent when non-empty:** `models` (fallback array), `tools`, `tool_choice`, `reasoning`,
`provider` (routing), `cache_control` (top level), `max_tokens`, `temperature`, `top_p`, `stop`,
`response_format`.

**NEVER sent, and each deletion removes a real defect:**

| Field | Why not |
|---|---|
| `usage: {include: true}` | **Deprecated, no effect.** Verbatim: *"The `usage: { include: true }` and `stream_options: { include_usage: true }` parameters are deprecated and have no effect. Full usage details are now always included automatically in every response."* There is no `usage` property on `ChatRequest` in the OpenAPI at all. |
| `stream_options: {include_usage: true}` | same; `ChatStreamOptions.include_usage` carries `deprecated: true`. |
| **the `strings.Contains(BaseURL,"openrouter.ai")` sniff that gated them** | deleting the two fields deletes L3's **only** implicit knowledge of which gateway it is talking to — a sniff that fails for a corporate proxy, a custom domain or a LiteLLM front-end, and the thing the "adapter or base URL?" decision must replace. |
| `route` | **deprecated** → `provider.sort.partition` (§4.2). |
| `parallel_tool_calls` | only **5 of 421** models advertise it in `supported_parameters`. The prose default is `true` for most models. Sending `false` risks `require_parameters` filtering for no gain; **omit it and take the default.** |
| `name` on a `role:"tool"` message | not in OpenRouter's `ChatToolMessage` schema (`required: [role, content, tool_call_id]`). The Python sample in the docs sends it; the TS sample sends camelCase `toolCallId`, which is SDK-only. kolk sends exactly the three schema fields. `content` is **required** — an empty tool result is `""`, never omitted or null. |
| `index` on a request-side `tool_calls` entry | `ChatToolCall` has no `index` field. |

**Schema dialect:** OpenRouter validates returned `arguments` against `tools[].function.parameters`
with `@cfworker/json-schema` pinned to **JSON Schema Draft 7**, using native V8 `RegExp` for
`pattern`. Draft 2019-09/2020-12 keywords (`unevaluatedProperties`, `$dynamicRef`) are **not
enforced**. kolk's tool schemas must therefore be Draft-7-expressible; anything richer is silently
unvalidated and pollutes the model's Tool Call Error Rate. `function.name` is capped at **64 chars**,
charset `[A-Za-z0-9_-]`.

**`tools` is resent on every round of the loop** — verbatim: *"The `tools` parameter must be included
in every request … so the router can validate the tool schema on each call."* **Auto Exacto runs by
default on every tool-bearing request** (provider ordering re-ranked by tool-call success rate); kolk
gets it for free and does not fight it with a hand-written `provider.order` unless the user asked.

#### 6.3 `reasoning` and the `reasoning_details` round-trip

**Request param** (full, current):

```jsonc
"reasoning": {
  "effort": "max"|"xhigh"|"high"|"medium"|"low"|"minimal"|"none",  // XOR with max_tokens
  "max_tokens": 2000,        // XOR with effort; min 1024, max 128000
  "exclude": false,
  "enabled": true,           // alone ⇒ reasoning at "medium"
  "context": "auto"|"all_turns"|"current_turn",  // GPT-5.6+ ONLY
  "mode": "standard"|"pro",                      // GPT-5.6+, OpenAI/Azure only
  "summary": "auto"|"concise"|"detailed"
}
```

kolk sends `{effort}` or `{max_tokens}` after `ReasoningSupport.Project`, never both, never
`effort:"none"` to a model whose catalog row says `reasoning.mandatory:true`, and **enforces
`max_tokens > budget_tokens`** — the Anthropic path computes
`budget = max(min(max_tokens * ratio, 128000), 1024)` with ratios max/xhigh 0.95, high 0.8, medium
0.5, low 0.2, minimal 0.1, and the docs are explicit that *"max_tokens must be strictly higher than
the reasoning budget"* or the request 400s.

**`reasoning_details` item shapes** (OpenAPI `ReasoningDetailUnion`, discriminator `type`) — **four**
variants, not the three the prose page still lists:

| `type` | required | optional |
|---|---|---|
| `reasoning.summary` | `type`, `summary` | `id`, `format`, `index` |
| `reasoning.encrypted` | `type`, `data` | `id`, `format`, `index` |
| `reasoning.text` | `type` **only** | `text` (**nullable**), `signature` (**nullable**), `id`, `format`, `index` |
| `reasoning.server_tool_call` | `type`, `tool_name`, `arguments`, `result` | `tool_call_id`, `id`, `format`, `index` |

`format` enum: `unknown | openai-responses-v1 | azure-openai-responses-v1 |
bedrock-openai-responses-v1 | bedrock-xai-responses-v1 | xai-responses-v1 | meta-responses-v1 |
anthropic-claude-v1 | google-gemini-v1 | null`.

**The five rules kolk follows.**

1. **Capture** from `choices[].delta.reasoning_details` (streaming) /
   `choices[].message.reasoning_details` (non-streaming), as `json.RawMessage`, in arrival order.
2. **Merge** two *adjacent* entries only when they share the same non-nil `(id, index, type)` triple,
   concatenating `text`/`summary`/`data`; otherwise append. Safe under both readings of the
   under-specified `index` semantics. **Unverified — see Risks.**
3. **Emit once**, on `EventFinish`. Never on a delta. (`Message.ReasoningDetails` invariant (b).)
4. **Echo verbatim.** The assistant turn is re-sent as one object carrying `content` + `tool_calls` +
   `reasoning_details`, followed by one `role:"tool"` message per call. The binding constraint,
   verbatim: *"When providing `reasoning_details` blocks, the entire sequence of consecutive
   reasoning blocks must match the outputs generated by the model during the original request; you
   cannot rearrange or modify the sequence of these blocks."* Store as `json.RawMessage`, re-emit the
   **bytes**. Do **not** unmarshal-into-struct-and-remarshal: `signature: null`, `text: null` and an
   absent `index` do not survive it, and re-emitting `""` where the model sent `null` is a
   modification of the block.
5. **Always echo when present, for every model.** The doc claims uniformity across reasoning models;
   the *need* is sharpest on Anthropic (signature-verified thinking blocks) and OpenAI encrypted
   items. Echoing is never wrong; the alternative requires a per-provider table.

**Interaction with `repairDanglingToolCalls`:** repair appends **only** `role:"tool"` messages and
**must never edit the assistant message**. Under that invariant, synthetic repair and byte-exact
reasoning echo are compatible. If repair ever needs to *drop* a tool call, it must drop the **whole
assistant turn**, because a partial `tool_calls` array no longer matches the reasoning sequence.
`reasoning.context: "current_turn"` (GPT-5.6+) is the escape hatch when history grows.

#### 6.4 Prompt-cache breakpoint placement

**Two placements exist; kolk uses the automatic one and reserves the explicit one.**

| Mode | Shape | kolk |
|---|---|---|
| **Automatic (top-level)** | `"cache_control": {"type":"ephemeral"}` (optionally `"ttl":"1h"`) as a **sibling of `model`/`messages`** | **This is what kolk sends.** *"The system automatically applies the cache breakpoint to the last cacheable block and advances it forward as conversations grow."* Supported on Anthropic, Google Vertex AI, Azure, Amazon Bedrock. A hand-rolled per-block scheme in an agent loop is the classic way to blow the cache, since every tool round appends messages. |
| **Explicit per-block** | `cache_control` on one content part, or on a `tools[]` entry | Reserved via `ContentPart.Cache` and `Tool.Cache`. **Max four breakpoints.** Needed by a future native Anthropic adapter, which has **no auto mode** — which is why placement is expressible per block and per tool rather than as a lone boolean. |

**Conditions kolk applies:** send `cache_control` only when `caps.Cache.Mode == CacheModeExplicit`
**and** `estTokens(prompt) >= caps.Cache.MinTokens` (per-model: **4096** for Claude Opus 4.5/4.6/4.7/4.8
and Haiku 4.5, **2048** for Haiku 3.5, **1024** for Sonnet 4/4.5/4.6 and Opus 4/4.1 — prompts below
the minimum are simply not cached). `ttl:"1h"` for `saga` runs (2× input write, 0.1× read), the 5-min
default (1.25× write) for interactive chat. Families with implicit caching (OpenAI ≥1024 tokens,
Gemini 2.5+, DeepSeek, Groq, Moonshot, Grok, Z.AI) get `CacheModeImplicit` and **nothing is sent**.

**The precondition kolk does not yet meet, recorded here so it is not forgotten:** caching is
defeated one layer up by `agent.go:102-107` regenerating the system prompt from live `os.Getwd()`
and file contents on every construction. `CachePolicy` cannot help until that prompt is stable.
Making it stable belongs to item 6 (modes) and item 12 (memory), and this doc is the citation.

#### 6.5 Provider-routing options exposed to config

Kolk's `Routing` vocabulary → the OpenRouter `provider` object. Config keys under `provider.routing.*`:

| kolk config key | `Routing` field | OpenRouter wire |
|---|---|---|
| `prefer = default\|cheapest\|fastest\|latency\|reliable` | `Prefer` | `sort.by = price\|throughput\|latency\|exacto` (`exacto` is the fourth, newer value) |
| `partition = model\|none` | `Partition` | `sort.partition` — `model` keeps fallbacks as fallbacks; `none` sorts every endpoint of every listed model together |
| `only = [...]` / `ignore = [...]` | `Only` / `Ignore` | `only` / `ignore` |
| `max_price_per_mtok = 3.0` | `MaxPricePerMTok` | `max_price` |
| `zdr = true` | `RequireZDR` | `zdr` |
| `deny_training = true` | `DenyTraining` | `data_collection: "deny"` |
| `require_parameters = true` | `RequireParams` | `require_parameters` |

**`require_parameters` is deliberately NOT set by default for tool calling.** A *soft* preference
already applies to exactly three parameters — `tools`, `response_format` (incl. structured outputs)
and `verbosity`: if some providers of a model support them and others do not, only the supporting
ones are eligible; if none do, the request still goes through with the parameter ignored. Critically,
*"this preference never removes a model from your request's candidate list (such as the `models`
fallback list)."* So the soft preference does the right thing without risking a 404
`constraint_filtered`. `require_parameters: true` is exposed for users who want the hard version.

**`provider.order` is deliberately NOT exposed.** Setting it **disables sticky routing entirely**,
which silently destroys prompt caching — the opposite of what a user reaching for it wants.

**Variant suffixes** pass through as part of the model id the user typed: `:free`, `:nitro`
(= `sort:throughput` **plus** priority service-tier endpoints — a strict superset of the sort),
`:floor` (= `sort:price` plus flex tier), `:exacto`, `:online`, `:extended`. **`:thinking` is no
longer supported for Anthropic models** — use `reasoning`. Router slugs present in the live catalog:
`openrouter/auto`, `openrouter/auto-beta`, `openrouter/free`, `openrouter/fusion`,
`openrouter/pareto-code`, `openrouter/bodybuilder`.

#### 6.6 Endpoints used

| Endpoint | Used for | Cache |
|---|---|---|
| `POST /api/v1/chat/completions` | every turn | — |
| `GET /api/v1/models` | the profile table (§5) | 12 h |
| `GET /api/v1/models/{author}/{slug}/endpoints` | item 8's fast lane: throughput / latency / uptime / per-endpoint price | 1 h, lazy |
| `GET /api/v1/key` | credits, `is_free_tier`, `limit_remaining`, `usage_daily` — so 402 and the free cap come with a number | 5 min |
| `GET /api/v1/generation?id=<X-Generation-Id>` | `Catalog.Reconcile`: authoritative cost, `provider_name`, `native_finish_reason`, `native_tokens_*`, `cache_discount`, `latency` vs `generation_time` | not cached |
| `https://openrouter.ai/auth` + `POST /api/v1/auth/keys` | OAuth PKCE (item 5 owns the UX) | — |

kolk stays on `/chat/completions`. The Responses API (`/api/v1/responses`) has a distinct error shape
and sometimes transforms `context_length_exceeded` into a *success* with `finish_reason:"length"`;
out of scope.

---

### 7. `openaicompat` — the quirk table and the field-stripping rules

**Verdict first: none of the six needs its own adapter.** They are `Dialect` rows in
`openaicompat/presets.go`. Evidence: in models.dev's own data every gateway except Vercel and
OpenRouter is tagged `npm: "@ai-sdk/openai-compatible"` — the ecosystem's own answer to "adapter or
base URL?" is *base URL*. And Vercel AI Gateway's OpenAI-compatible surface is **live, complete and
unauthenticated** (352 models with `context_window`, `supported_parameters`, `pricing`, `tags`,
verified 2026-08-22); its *native* protocol is the Vercel AI SDK's own `LanguageModelV4` spec on the
wire, valuable only to AI SDK consumers and worth a fourth code path to nobody.

#### 7.1 The per-target quirk table

Sources: official docs plus upstream source read 2026-08-22. **No chat-completions call was made
against any of these targets** — none is installed here and no gateway key exists.

| | **Ollama** | **LM Studio** | **vLLM** | **llama.cpp server** | **LiteLLM proxy** | **Vercel AI Gateway** |
|---|---|---|---|---|---|---|
| **Streaming tool calls** | yes, **never fragmented** — each call arrives complete in one delta (structural: args are an ordered map, not a string) | yes, **fragmented** OpenAI-style | yes, per-parser — **only if the operator passed `--enable-auto-tool-choice --tool-call-parser X`** | yes: id+name header delta, then arg fragments | passthrough | passthrough |
| **tool-call `index`** | often absent | present | present | present | upstream | upstream |
| **parallel tools** | n/a (no `tool_choice` at all) | model-dependent | default **true** | **disabled by default — must send `parallel_tool_calls: true`** | upstream | upstream |
| **usage** | `prompt/completion/total` only, no cost | tokens only (needs ≥ 0.3.19) | + `prompt_tokens_details.cached_tokens`; vLLM-only `continuous_usage_stats` puts usage on *every* chunk | + `cached_tokens` + a non-standard `timings{}` object | tokens in body, **cost in HTTP headers** | tokens in body, **cost via `GET /v1/generation`** |
| **unknown request fields** | **silently ignored** (plain `encoding/json`, no `DisallowUnknownFields`) | **undocumented — must probe** | **accepted and ignored** (`ConfigDict(extra="allow")`, logs at DEBUG) | **silently ignored** (pull-based schema) | **RAISES by default.** `drop_params` is a *server-side* setting the client cannot set | accepts its own extensions; **`provider.sort` COLLIDES** |
| **catalog** | `/api/tags` | `/api/v1/models` | `/v1/models` → `max_model_len` only | `/props`; `/v1/models` returns **one** element whose id is the `-m` path | `/model_group/info` | `/v1/models` (rich) |
| **reasoning field** | `reasoning` | model-dependent | `reasoning_content` | `reasoning_content` | upstream | `reasoning` |
| **signature quirks** | `done_reason` passes through untouched → **`load` / `unload` reach the client as `finish_reason`**; `system_fingerprint:"fp_ollama"`; model unload on idle ⇒ cold-load latency | "default" tool mode **injects a prompted pseudo-tool system prompt**; malformed calls land in `content` | tools sent with no parser = **silent failure**: tool syntax in `content`, `finish_reason:"stop"`, no error. `tool_choice:"required"` compiles an FSM (seconds of first-call latency) | errors are `{code:<int>, message, type}` with `exceed_context_size_error`(400); `--sleep-idle-seconds` sleeps the server | model names are **arbitrary config aliases** — every id heuristic (`sonnet`, `:free`, `:nitro`) is dead; cost headers **only on non-streaming** | `models` fallback array; `cache_control` on the **message** object; generation id *"injected into the first content chunk"* |

#### 7.2 Field-stripping rules — `Dialect` as data

```go
type Dialect struct {
    Name string
    // StrictBody: send ONLY canonical OpenAI fields. LiteLLM raises on
    // unsupported params by default, so `provider`, `models`, `transforms`,
    // `session_id`, `cache_control` and `reasoning` are stripped unless the
    // model group advertises them.
    StrictBody bool
    // ForceParallelTools: llama.cpp disables parallel tool calls unless the
    // request explicitly sets parallel_tool_calls:true — the one place kolk
    // ADDS a field rather than stripping one.
    ForceParallelTools bool
    ReasoningField string // "reasoning" | "reasoning_content"
    ToolCallKey    string // "index" | "id" | "order"
    CatalogPath    string
    CatalogDecode  func([]byte) ([]provider.ModelInfo, error)
    CostFrom       func(http.Header, json.RawMessage) (*float64, provider.CostSource)
    Encode         func(*provider.Request, map[string]any)  // preset-specific fixups
    MapFinish      func(raw string) provider.FinishReason   // OPEN enum: "load"/"unload"
    Classify       func(int, http.Header, []byte) *provider.Error
    Headers        map[string]string
    Timeouts       provider.Timeouts
}
```

| Preset | Strips | Adds | Cost | Timeouts (first byte / idle) |
|---|---|---|---|---|
| `generic` | OpenRouter extras (`provider`, `models`, `session_id`, `cache_control`, `transforms`) | — | none | 60 s / 90 s |
| `ollama` | as generic, + `tool_choice` (unsupported) | — | `CostFree`/`MeasureLocal` | **600 s** / 120 s (cold model load) |
| `lmstudio` | as generic | — | `CostFree`/`MeasureLocal` | 600 s / 120 s |
| `vllm` | as generic | — | `CostFree`/`MeasureLocal` | **600 s** / 120 s (CUDA-graph capture) |
| `llamacpp` | as generic | **`parallel_tool_calls: true`** | `CostFree`/`MeasureLocal` | 600 s / 120 s |
| `litellm` | **everything non-canonical** (`StrictBody`) | — | header `x-litellm-response-cost` → `CostHeader`; **non-streaming only** | 60 s / 90 s |
| `vercel` | OpenRouter `provider` object (**collision**), `session_id`, `cache_control` top-level | `provider.sort` translated `cost\|ttft\|tps`; `models` passes through | `GET /v1/generation` → `CostFollowup` | 60 s / 90 s |

**Why Vercel is a `Dialect` row and not an adapter, precisely:** `provider.sort` takes
`cost|ttft|tps` where OpenRouter takes `price|throughput|latency|exacto`. Sending an OpenRouter-shaped
`provider` object is **not an ignored extra — it is a wrong value in a field the server reads.**
That is exactly why `provider.Routing` is kolk's own typed enum that each dialect *translates*, and
why the collision costs one table row rather than one package. Vercel also needs a second catalog
decoder (`context_window` not `context_length`, `pricing.input` not `pricing.prompt`, plus `tags[]`)
and a follow-up cost strategy — both already `Dialect` fields.

**LiteLLM is a base URL plus a profile, not an adapter**: wire format, streaming, tools and usage are
plain OpenAI, so there is nothing to re-implement. What makes it non-bare is exactly four things, and
all four are `Dialect` fields: `StrictBody`, `CostFrom` (headers), `CatalogPath`
(`/model_group/info`), and the fact that its model ids are arbitrary aliases — so **the catalog is
the only truth** and every id-pattern heuristic is dead there.

**`finish_reason` is an OPEN enum.** Ollama passes `done_reason` through, so `load` and `unload`
reach the client. `Finish.Raw` keeps the provider's string beside the normalised value, and the
terminal-condition rule of §1.8 turns a `load` warm-up (clean finish, no content, no tool calls, no
output tokens) into `KindTruncated` rather than a blank successful answer.

---

### 8. The `agentcli` seam (item 4 builds on this)

`agentcli` implements `provider.Chat` and `provider.Stream` **unchanged**. There is no second
interface. Three things carry the entire load, and all three are in `provider.go` from v0.1 rather
than retrofitted — if they were absent, this design would be wrong.

| Mechanism | What it absorbs |
|---|---|
| `Capabilities.ExecutesOwnTools` + `AcceptsToolSchemas` | the backend runs its own tools and cannot take a schema list |
| `Event.ProviderExecuted` on `EventToolCall` + a separate `EventToolResult` | announcement and outcome are separated by the tool's entire runtime |
| `Request.ProviderState` ⇄ `Finish.ProviderState` | `--resume`; without it every turn starts cold and resume-after-crash is unrepresentable |

#### 8.1 What it implements, and how

| `Chat` method | agentcli |
|---|---|
| `Stream` | `Spawner.LookPath("claude")` → `claude auth status --json` (zero quota cost) → spawn `claude -p --output-format stream-json --include-partial-messages` with the prompt on **stdin** → pump NDJSON through the **pure** `claude.Translate(frame, *state) []Event`. No goroutine; ≥ 1 MB reader buffer. |
| `Capabilities` | the honest floor below; no probe, no network |
| `Close` | kills the process **group** and reaps. This is why `Close()` is on the base interface: fantasy had to declare a *wider* `Provider` in its `kronk` package for exactly this and forced a type assertion at every call site. Three no-op methods here remove an entire class of leak. |

**Frame mapping:**

| `claude -p --output-format stream-json` | Event |
|---|---|
| — (before the first frame) | `EventStart{Warnings}` — the drop list of §8.3 |
| `{"type":"system","subtype":"init"}` | `EventResponseMeta{Model}`; the vendor session uuid is held for `Finish.ProviderState` |
| `content_block_delta / text_delta` | `EventTextDelta` |
| `content_block_delta / thinking_delta` | `EventReasoningDelta` (`Finish.ReasoningDetails` stays nil: the child owns its own continuity) |
| `{"type":"assistant"}` `content[].text` | `EventTextDelta` (whole-block fallback without `--include-partial-messages`) |
| `{"type":"assistant"}` `content[].tool_use` | **`EventToolCall{ProviderExecuted:true}`** — the announcement |
| `{"type":"user"}` `content[].tool_result` | **`EventToolResult{CallID, Output, IsError}`** — the outcome |
| `{"type":"system","subtype":"api_retry"}` | `EventError{Kind: KindOverloaded, Phase: PhaseCommitted}` + a `log` |
| `{"type":"result"}` | `EventUsage{CostUSD, CostSource: CostVendorEstimate, Measurement: MeasureEstimated}` then `EventFinish{ProviderState: <session uuid>}` |
| `{"type":"result", is_error:true}` / `{"type":"error"}` | `EventError` |
| EOF or non-zero exit with no `result` | `EventUsage` (all nil) + `EventError{Kind: KindTruncated}` |

**The exit status is observed on the READ path.** `Next()` detects EOF-without-a-terminal-`result`,
calls `Proc.Wait()` there, and sets `KindTruncated` with the exit code in `Error.Message`. `Close()`
kills **only** when the stream did not end cleanly. Getting this wrong is subtle and fatal: with
`defer s.Close()` plus `return r, s.Err()`, Go evaluates the return operands *before* the deferred
call runs, so a crashed child returns `(partial, meta, nil)` — a successful truncated turn, appended
to the session and fed back to the model as if the assistant had finished.

**The invariant a test asserts, in both directions:** if `ExecutesOwnTools` then the stream MUST
never emit `EventToolCall{ProviderExecuted:false}`; if not, it MUST never emit
`EventToolCall{ProviderExecuted:true}`. The engine's tool-execution branch is therefore unreachable
for agentcli **by construction**, which matters because `claude` has already edited the files by the
time kolk sees the event.

#### 8.2 The honest floor

```go
Capabilities{
    Backend: "claude", Streaming: Yes,
    Tools: Yes,                       // it HAS tools; they are just not yours
    ExecutesOwnTools:   true,
    AcceptsToolSchemas: false,
    HistoryOwned:       true,         // only the newest user turn is sent
    AcceptsFallbackList: false,       // --fallback-model takes exactly one entry
    EchoesReasoning:    false,        // we never resend history
    IdempotentConnect:  false,        // ★ system/init mutates vendor state
    ModelSelection:     ModelAliasOnly,
    StructuredOutput:   No,           // ★ see below
    Reasoning:          ReasoningSupport{Style: ReasoningNone},
    Cache:              CacheSupport{Mode: CacheModeNone},
    UsageReported:      No,           // no per-token counts, ever
    CostSource:         CostVendorEstimate,
    Measurement:        MeasureEstimated,
    Source:             CapPreset,
}
```

`StructuredOutput: No` is the row that bites first in practice: `orchestrator.plan()` asks for a
strict JSON array, so the **planner role must resolve to a different `Chat`**. That is fine — kolk
already resolves models per role (items 8 and 14) — but it means **one session can hold more than one
`Chat`**, which the registry supports and the engine must not assume away.

#### 8.3 Exactly what degrades, and where it is declared

Every degradation is a `Warning` on `EventStart`, never a surprise:

| `Request` field | agentcli | Warning |
|---|---|---|
| `Tools` | **dropped** (`--allowedTools` takes *names*, not schemas) | `WarnToolsDropped` — *"the Claude Agent backend runs its own tools; kolk's tool set, permission rules and checkpoints do not apply"* |
| `Messages` | **only the newest user turn**; continuity via `--resume` | `WarnHistoryTruncated` |
| `Fallbacks` | dropped | `WarnFallbackIgnored` |
| `Reasoning` | dropped (no flag exists in 2.1.x) | `WarnEffortDropped` |
| `MaxOutputTokens`, `Temperature`, `TopP`, `Stop`, `Format`, `Routing`, `Cache` | dropped | one `WarnParamDropped` each |
| `SessionID` | **not sent** — it is kolk's id, not a UUID; only `ProviderState` reaches `--resume`/`--session-id` | — |
| per-token `Usage` | unavailable; all count pointers `nil` | `WarnUsageUnavailable` (and `Capabilities` said so before the turn) |
| unrecognised `ProviderState` | soft restart | `WarnHistoryLost` |

**Product consequences, stated plainly.** In this backend kolk is a **frontend and a recorder**, not
the agent. `/rate`, titles, the cost footer and the leaderboard still work. Per-token efficiency
comparisons against an OpenRouter model do **not**, which is exactly why `CostSource` and
`Measurement` exist: item 17 must **refuse to pool** an estimate with a meter rather than silently
produce a nonsense chart. kolk's permission rules, path jail and hardline blocklist do **not** apply
to tools the child runs — only `--permission-mode` and `--allowedTools`/`--disallowedTools` do — and
checkpointing degrades to git-based per-turn snapshots (item 13) or is disabled with a loud warning.
`kolk doctor` must say all of this. Rotation is disabled for this backend
(`Policy.MaxRotations = 0`), enforced by `ModelSelection != ModelFree` in the rotation filters — not
by a config field nobody reads.

#### 8.4 Two safety properties the seam enforces

- **Prompt on stdin, never argv** (`SpawnCmd.Stdin`), so private-repo content never appears in `ps`.
- **A cleared environment plus an explicit allow-list** (`SpawnCmd.Env`), never `cmd.Environ()`, plus
  `--settings '{"apiKeyHelper":"","claudeMdExcludes":["**"],"disableAllHooks":true,
  "enabledPlugins":{},"env":null}'`. `"env": null` must replace the **whole** block, because per-key
  nulls are materialised as environment entries by Claude Code and can shadow OAuth. If kolk inherits
  `ANTHROPIC_API_KEY`, the "use my Claude Max plan" backend quietly bills the user's **API account**
  instead — silent, expensive and invisible. Roughly 35 variables must be absent:
  `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`,
  `CLAUDE_CODE_USE_BEDROCK/_VERTEX/_FOUNDRY`, `AWS_*`, `GOOGLE_APPLICATION_CREDENTIALS`,
  `GCLOUD_PROJECT`, … **This is an L0 API requirement on `internal/shell`.**
- kolk never sees, stores or proxies a token. Login detection is `claude auth status --json` →
  `{loggedIn, authMethod, apiProvider, apiKeySource}`, which consumes no quota and can therefore run
  on every `kolk doctor`.

#### 8.5 The one place the fit is imperfect, said out loud

`Request.Messages` is a lie for agentcli: the adapter reads only the last user message. A design that
took this fully seriously would make `Request.Input` a union of "full history" and "next turn only".
That was rejected because it complicates the 99% case (every HTTP backend) to be honest about the 1%.
`Capabilities.HistoryOwned` plus `WarnHistoryTruncated` makes the lie **visible and pre-declared**
rather than silent. If a second subscription backend ever needs a different shape, that is the field
to revisit.

---

### 9. Decisions on the open questions

| Question | Decision | Why the alternative lost |
|---|---|---|
| **Native provider keys (Anthropic / OpenAI / Google direct) in v0.x?** | **OUT.** Three adapters, N presets. | They multiply code paths for capabilities OpenRouter already exposes uniformly (unified `reasoning`, `cache_control`, `reasoning_details`, one key, one bill). **But deferring by leaving a hole in the interface is the mistake this design refuses**: `Request.Extra`, `Message.Blocks`/`BlocksFormat`, `ContentPart.Cache`, `Tool.Cache` and `ProviderState` all ship in v0.1 precisely so `provider/anthropic/` later is an *addition*, not a re-cut. The AI SDK's `providerOptions` and fantasy's `ProviderOptions` exist for the same reason. |
| **Second gateway: adapter or base URL?** | **Base URL + a `Dialect` preset.** Vercel AI Gateway, LiteLLM, Requesty, Helicone, ZenMux, Kilo are all `openaicompat` presets. | An adapter buys a fourth code path for a protocol nobody but the AI SDK consumes. Vercel's OpenAI surface is live, unauthenticated and complete (352 models with full metadata). The one genuinely disqualifying fact — the `provider.sort` vocabulary **collision** — is handled by `Routing` being kolk's own enum that each dialect translates, i.e. one table row. |
| **Where do per-model quirks live?** | **In the catalog**, on a TTL, plus a generated `//go:embed` seed and a user override file. **kolk ships no quirk table.** | A Go quirk table needs a binary release to fix a wrong `max_tokens`. The live catalog already carries `supported_parameters`, `top_provider.max_completion_tokens`, `architecture.input_modalities`, the per-model `reasoning{}` block and 13 pricing keys; models.dev adds the effort vocabulary and the reasoning field name. Hardcoding (catwalk's `["low","medium","high"]`) is provably wrong for the majority of models. |
| **Retry/backoff, timeouts, keep-alives, idempotency on resume** | Pure `Decide` in L3, loop in L4. Per-preset first-byte timeouts (60 s remote / 600 s local) plus an **idle-between-frames** watchdog armed only around the transport read. `: OPENROUTER PROCESSING` comments reset it. **Idempotency needs no request key**: kolk never re-sends a request whose output was published. | A retrying `Chat` decorator replays a committed stream into an append-only event log. Wrapping the whole `Next()` in the watchdog lets a slow consumer cancel a healthy stream. And the honest caveat is recorded rather than claimed away: a *connect-phase* retry **may** be billed twice upstream, which is why both attempts get a span. |
| **Free-model rate limits** | Rotate on `RateEndpoint`; back off on the short account bucket; **fail with the real remedy** on the daily cap; persist cooldowns keyed by scope. | Rotating on an account-scoped cap burns one request per model against the very cap that is exhausted, then fails anyway — the trap orcli's free-tier chain walks into. |
| **Local models** | Presets `ollama`, `lmstudio`, `vllm`, `llamacpp` with real timeouts, native catalog decoders, tri-state tools and behavioural probing. **The `APIKey == ""` hard failure is deleted** for non-OpenRouter base URLs — today you cannot run kolk against a keyless local Ollama at all (`client.go:136-138`, `cmd/kolk/main.go:108-116`). | — |

---

### 10. Testing plan

`go test ./...` is green at **22** today, verified this session:
`checkpoint 4 · engine 5 · provider 2 · session 3 · stats 3 · tools 5`.

#### 10.1 How each existing test stays green

| Test file | Tests | Change | Assertions |
|---|---|---|---|
| `internal/provider/client_test.go` → `openaicompat/stream_test.go` | 2 | `NewClient("k")` + `c.BaseURL=` → the dialect constructor; `mkChunk`'s hand-written **anonymous** choice struct → the named `streamChoice`; `[]ToolCall{{Index:0,…}}` → `[]toolCallDelta` | **unchanged** — content, streamed tokens, `call_abc123`, `bash`, the fragmented-args string, usage 120/40, cost 0.0042, `Elapsed>0`, and `strings.Contains(err.Error(),"401")` |
| `internal/engine/agent_test.go` | 5 | 2 lines in the test helper: `provider.NewClient` → the registry constructor; `Options.Client` is now `provider.Chat`, which `*Client` satisfies | **unchanged** |
| `internal/enginetest/router.go` | — | +~15 lines: its own `toolCallDelta{Index,ID,Type,Function}` (because `provider.ToolCall` correctly stops serialising `Index`); matcher-based dispatch (§10.3); stale `// Package mockrouter` doc fixed; the now-false comment *"as real providers send with `stream_options.include_usage`"* deleted | it has no assertions; `Requests [][]provider.Message` and `Tools []int` stay exactly as they are |
| `internal/session/session_test.go` | 3 | two `[]provider.Message` literal-type edits **at §12 step 10**, alongside the committed `testdata/v0-session.json` | **unchanged** |
| `tools_test.go` · `checkpoint_test.go` · `stats_test.go` | 12 | none | — |

**Three specific hazards and their defences.**

1. **`client_test.go:42-56` hand-writes the *anonymous struct literal* of `streamChunk.Choices`.**
   Adding **any** field to a choice — a per-choice `error`, `reasoning_details`, `logprobs` — changes
   that anonymous type and the test **fails to compile**, not to assert. Naming the struct is
   therefore step **M0**, before anything else: otherwise the first real wire edit costs you the most
   valuable test in the suite while you debug something unrelated.
2. **`agent_test.go:79` asserts `len(srv.Requests) == 2` and `:212` asserts
   `planner:1 subagent:3 synthesis:1`.** Retry and rotation live in the **engine loop**, and
   `enginetest` wires `Policy{MaxAttempts:1, MaxRotations:0}` — the **zero value**, so a test that
   forgets to configure a policy gets exactly one attempt. Without this, retrying the mock's
   `"no more scripted steps"` 500 (`router.go:94`) spins forever.
3. **`agent_test.go:74` asserts `strings.Contains(out.String(), "$0.0020")`.** `Meta.Cost float64`
   and `footer()`'s `$%.4f` are untouched; `Usage.CostUSD *float64` is the new truth and `Meta.Cost`
   is its flattening. This compounds architecture §12 step 7's byte-identity requirement — do not
   reorder the footer.

**`tools.Definitions() []provider.Tool` is kept as-is in v0.x.** `provider.Tool` is already neutral
now that the OpenAI wire structs live inside the adapters — it is a name, a description and a JSON
Schema, which is the intersection of every backend's tool vocabulary; agentcli drops it with a
`Warning`. Changing it would break the build at `agent.go:152` and `orchestrator.go:174` for no gain.
**But `Definitions()` has zero test coverage** (`grep Definitions internal/tools/tools_test.go` →
nothing), so **a test for it is added at step M0**, before anything touches its type or its consumers.

#### 10.2 What the scripted mock must grow

All additive: `Step` gains zero-valued optional fields, so every existing script produces
byte-identical output.

```go
type Step struct {
    Text             string
    ToolCalls        []provider.ToolCall
    PromptTokens, CompletionTokens int
    Cost             float64

    // ── new, all optional
    Match            func(msgs []provider.Message, tools int) bool // §10.3
    Role             string          // "planner" | "subagent" | "synthesis" | "main"
    FinishReason     string          // "stop"|"tool_calls"|"length"|"content_filter"|"error"|"load"
    Error            *provider.Error // mid-stream error frame, AFTER n content fragments
    ErrorAfter       int
    ReasoningDeltas  []json.RawMessage // reasoning_details items, streamed
    PromptDetails    *struct{ Cached, CacheWrite int }
    CompletionDetails *struct{ Reasoning int }
    Keepalives       int   // ": OPENROUTER PROCESSING" comment lines interleaved
    MultiLineData    bool  // emit one event as a multi-line `data:` field
    NoTerminal       bool  // close the body with no [DONE] and no finish_reason
    OmitToolIndex    bool  // Ollama/llama.cpp shape
    RepeatToolName   bool  // Mistral/vLLM shape: full name in every delta
    SplitRune        bool  // split a multi-byte rune across two fragments
    Headers          map[string]string // e.g. x-litellm-response-cost, X-RateLimit-*
}
```

**Fixtures to add** (all offline, all replayable, one per family):

| Family | Asserts |
|---|---|
| `openrouter/reasoning` | `reasoning_details` deltas merge byte-exactly and are echoed unmodified on round 2 (`signature:null` survives) |
| `openrouter/midstream-error` | partial content is **returned**, `Phase == PhaseCommitted`, `Decide` refuses `ActRetry`, usage frame still arrives with `GenID` |
| `openrouter/choice-error` | the error nested in `choices[].error` is **not** silently ignored |
| `openrouter/truncated` | no `[DONE]`, no terminal `finish_reason` → `KindTruncated`, not a successful partial |
| `openrouter/keepalive+multiline` | `:` comments skipped and resetting the watchdog; multi-line `data:` joined with `\n` |
| `openrouter/429-account` vs `429-endpoint` | `X-RateLimit-*` present/absent → `KindQuotaExhausted` vs `KindRateLimit`, and the **default is rotate** |
| `utf8/split-rune` | 🚀 split across two fragments survives; the same for a tool `arguments` `path` |
| `utf8/lone-surrogate` | `"\ud83d"` + `"\ude80"` reassembles to U+1F680, not `U+FFFD` |
| `ollama` | one complete tool call in a single delta, **no `index`**; `finish_reason:"load"` → `KindTruncated`; empty-`choices` usage chunk |
| `lmstudio` | fragmented name+args — **the existing reassembly assertions must pass unchanged** |
| `vllm` | tools sent, zero `tool_calls`, tool syntax in `content`, `finish_reason:"stop"` → `KindToolsUnsupported` with the operator-flag reason; `continuous_usage_stats` usage on every chunk |
| `llamacpp` | id+name header then arg fragments; final chunk with `usage` **and** `timings`; `{"error":{"code":400,"type":"exceed_context_size_error"}}` |
| `litellm` | 200 with `x-litellm-response-cost` → `CostHeader`; 400 on an unsupported param → `StrictBody` was required |
| `vercel` | response `model` ≠ request `model`; generation id in the first content chunk |
| `spec/testdata/foreign/claude-*.ndjson` | real captured `claude -p --output-format stream-json`: init, thinking deltas, `tool_use`, `tool_result`, `api_retry`, `result`, `error`, and an EOF-with-no-result |
| `spec/testdata/foreign/codex-*.ndjson` | the same shapes for `codex … --json` |

`agentcli`'s whole offline harness is an 18-line `Spawner` returning `strings.NewReader(script)`, so
`internal/mockagent` (real fake binaries) is needed **only** for the L0 `shell.Spawn` integration
test, not for a single agentcli translation test. Neither vendor binary is ever invoked and no
subscription quota is consumed.

#### 10.3 Two structural changes to the harness, before parallelism ships

- **Matcher-based dispatch.** `router.go` currently serves steps in **arrival order**
  (`step := s.steps[s.i]; s.i++`). The moment item 14's subagents run concurrently, which subagent
  gets which script is a race — and item 14's core feature becomes the one feature with no
  deterministic offline test. `Step.Match` (on the last user message, or `Role`) selects; arrival
  order remains the fallback when no matcher is set, so every existing script is unaffected.
- **Every shared in-memory fake is mutex-guarded** (`enginetest.Server.Requests` already is;
  `provider.Fake.Calls` is from day one).

**New test targets, roughly:** `Decide` table (~30 cases: Kind × Phase × RateScope × attempt ×
Pinned), `ReasoningSupport.Project` (all 8 live vocabularies + toggle + budget + `mandatory`),
`Acc` (surrogate/split-UTF-8/escape-boundary + a **fuzz** target), `Collect` (multi-message fold,
terminal-condition rule, partial-on-error), the tool-call assembler (index / id / arrival-order keys,
name-repeat, id collision, invalid-JSON drop), the OpenRouter error classifier (all three
`error_type` locations, `availability`, header parsing), the `agentcli` translator, and the
`ExecutesOwnTools` two-way invariant.

---

### 11. Ordered migration checklist

Slots into `02-architecture.md` §12's gap — steps 5–9 contain **no provider step at all**, while §2's
tree and §4's file table assume the split has happened. **Invariant: `go build ./... && go test ./...`
is green after every step.** No red build window.

| # | Step | §12 anchor | Breaks | Green after |
|---|---|---|---|---|
| **M0** | **Name the anonymous choice struct** in `provider/client.go:92-99` and `enginetest/router.go:60-66`; update `client_test.go:42-56` and `router.go:68-77` to use it. Fix the three stale package doc comments (`// Package api` on `package provider`, `// Package agent` on `package engine`, `// Package mockrouter` on `package enginetest`). **Add the missing `tools.Definitions()` test.** Zero behaviour change, zero assertion change. | before step 5 | nothing | 22 + 1 |
| **M1** | **Additive types, nothing reads them yet**: `event.go`, `usage.go`, `capability.go`, `errors.go`, `policy.go`, `jsonstr.go` (+ its tests and fuzz target). Enrich `Meta` additively (`ResponseModel, ProviderName, GenID, TTFT, Finish, RawFinish, ErrorKind, Attempt, Usage`) and mirror as `omitempty` columns on `stats.Record`. | before step 5 | nothing | 22 + ~12 |
| **M2** | **Wire correctness inside the existing client** — the six defects that are pure bugs today: spec-compliant SSE (`sse.go`: multi-line `data:`, `:` comments, `bufio.Reader` not `Scanner`); `Acc` for content and arguments; tool-call keying `index → id → arrival` with set-if-empty names, id guarantees and `json.Valid` gating; terminal-condition detection; typed errors on the HTTP and mid-stream paths (both locations) **returning the partial**; `context.WithCancelCause` so stall ≠ cancel; idle watchdog around the transport read only; drop the `APIKey==""` hard failure for non-OpenRouter base URLs. Delete `usage:{include}`, `stream_options.include_usage` and the `strings.Contains(BaseURL,…)` sniff. | before step 5 | nothing | 22 + ~14 |
| **M3** | **The `Stream`/`Event` cut.** Rewrite `StreamChat`'s body as `Stream()` + `Collect()`; `StreamChat` keeps its exact signature and semantics as a shim. Mock gains the new `Step` fields and matcher dispatch. | before step 5 | nothing | 22 + new |
| **M4** | **The interface = architecture step 9's `Provider` port.** `engine.Options.Client *provider.Client` → `provider.Chat`. `*Client` satisfies it, so `agent_test.go:20,263` compile **unchanged**. Inject `Clock` and `Cooldowns` ports; `enginetest/fakes.go` supplies both. | **step 9** | nothing | 22 |
| **M5** | **Registry + package split.** `provider/{provider,registry,catalog,collect,fake}.go`; `openaicompat/` becomes the engine; `openrouter/` becomes hooks + catalog; `presets.go` as data. **Exported types STAY in package `provider`** — moving them into `openrouter/` would break 4 files and 3 tests. `client_test.go` → `openaicompat/stream_test.go`, assertions verbatim. `cmd/kolk` blank-imports the adapters (**state this in the PR**, or the registry is empty at runtime and five e2e tests fail at run time, not compile time — the worst failure mode). | between steps 9 and 10 | nothing | 22 |
| **M6** | **Catalog + profiles.** Full `ModelInfo`, `CatalogStore` (L5, flock+atomic+ETag+TTL), `Overlay` + `tools/cmd/modelgen` + `catalog/profiles.json`, `kolk models --refresh`, `/key` credits, `/endpoints`. One new line in `internal/arch/layers.go` for the `embed` import. Only `cmd/kolk/main.go:520-538` changes. | steps 9 → 11 | nothing | 22 + new |
| **M7** | **★ `session.Message` — architecture step 10, and item 3 forces it EARLIER than §12's ordering implies.** Commit `internal/session/testdata/v0-session.json` **and** a load test in the same commit as `session.Message` with frozen tags + conversion at the store boundary. This must land **before** M8, or shipping reasoning is a silent on-disk format change with no fixture defending it. | **step 10** | the one place a silent data regression is possible — the fixture is the whole defence | 22 + 1 |
| **M8** | **Reasoning round-trip + `ProviderState` + `ReasoningModel`.** Send `reasoning`; capture/merge/echo `reasoning_details` byte-exactly; persist `ProviderState`; `sess.Wire(model)` strips reasoning across model changes and appends the synthetic continuation after a `Truncated` turn. Amend `repairDanglingToolCalls` (whole-list scan, pending markers, honest text) and save after **each** tool result. | after step 10 | session format — defended by M7's fixture | 22 + new |
| **M9** | **Retry / rotation / budget / per-attempt spans in the engine loop.** `Decide` is already written and tested from M1; this is the loop, the tried-set, the persisted cooldowns and the span-per-attempt `defer`. `enginetest` keeps `Policy{}`'s zero value. | step 14 | request-count assertions **iff** the defaults are wrong — they are not | 22 + new |
| **M10** | **Prompt caching.** `session_id` on every OpenRouter call; top-level `cache_control` gated on `CacheMode`/`MinTokens`. Blocked on making the system prompt stable (items 6/12) for full effect, but `session_id` alone already fixes the sticky key. | after step 10 | nothing | 22 |
| **M11** | **`provider/agentcli` + `internal/mockagent` + `spec/testdata/foreign/`.** Requires M3's Events, M5's registry and M8's `ProviderState`. Capture the fixtures **before** writing the translator. | **step 14** | nothing | 22 + new |

**Spec deltas item 3 requires**, all additive and free while `spec/VERSION == "0"` — land them at M4
so the engine has something to publish into:

1. **`usage.reported` widens to the `spans` column set** of `docs/research/dashboard.md` §4:
   `+ provider_name, request_model, response_model, cache_write_tokens, reasoning_tokens,
   cost_source, measurement, finish_reason, error_type, gen_id, attempt, role, effort`. Otherwise
   every field this design fought to decode is dropped on the floor at the bus boundary and SQLite
   receives six columns. Write the `provider.Usage → usage.reported` map into `spec/` as a table.
2. **`tool.delta`** — a new event carrying `{id, name?, arguments_delta}`, so streamed tool arguments
   are replayable via `Last-Event-ID` instead of reaching a renderer through a second, private
   channel.
3. **`tool.requested` gains `executed_by: "kolk" | "provider"`**, so one tool lifecycle serves every
   backend and a client never needs to know which adapter it is attached to.
4. **`log`'s payload is declared**: `{level, code, field, was, became, message}` with `code` from the
   closed `Warn*` list — no free-form JSON on the one event type nobody thought to close.

---

## Rationale

**Why one two-verb interface and not a wider one.** Three very different backends had to fit: an
HTTP+SSE gateway, a keyless local server that cannot say whether tools work, and a subprocess that
runs its own tool loop, owns its own history, cannot take a tool schema and reports cost as a lump
estimate. Every widening the field has tried makes the *consumer* pay: fantasy's `kronk` provider
declares `fantasy.Provider + Close` and forces a type assertion at every call site; langchaingo's
`ReasoningModel` does the same for one boolean. Putting `Close()` on the base interface costs three
no-op methods and removes a leak class, and putting capability in a **returned value** removes the
assertions entirely. The one flag the engine branches on — `ExecutesOwnTools` — is evaluated once, in
one place, and there is no `if backend == "claude"` anywhere in `internal/engine`.

**Why a flat `Event` with a type tag.** A sealed Go interface costs a hand-written
`MarshalJSON`/`UnmarshalJSON` per member plus a global type registry every adapter must `init()`
into. fantasy pays exactly that — 28 KB in `content_json.go` plus a `sync.Map` registry — and then
deliberately keeps its *stream* part flat to avoid paying it twice on the hot path. The vocabulary is
the Vercel AI SDK's, which survived `v2 → v3 → v4` with **zero removals or renames** and was
independently reproduced by fantasy; and it was chosen to map 1:1 onto `02-architecture` §7 so
`engine/events.go` is a switch, not a translation layer.

**Why `Decide` is pure in L3 and the loop is in L4.** This is the one place all three candidate
designs disagreed, and the adversarial pass settled it in both directions at once. A retrying `Chat`
decorator (Design 2's `Retrier`, Design 3's `WithRetry`) re-runs the whole step from scratch —
fantasy's own docs warn that *"the retried response is appended to the partial content from the
failed attempt"* unless the consumer resets — and kolk publishes to an append-only bus with a
monotonic `seq`, so a silent replay corrupts the log and every resumed client. Meanwhile Design 3's
seven always-on middleware put `record` and `meter` **outside** `retry` and `fallback`, where a span
per attempt is structurally impossible and a backoff sleep lands inside the latency measurement — the
exact capability it advertised. Both failures vanish when L3 decides and L4 acts: only the engine
knows whether tokens reached the user, whether the model is pinned, what the budget is, and what has
been tried; and per-attempt spans fall out for free because the engine already owns the loop.

**Why the reasoning array only exists on the terminal frame.** This is the single worst failure mode
in the whole review, and it is not a stream bug — it is a **disk** bug. A Claude turn that streams
3 KB and half a thinking block, then drops, yields a `reasoning_details` array whose last
`anthropic-claude-v1` block has no `signature` or half an encrypted blob. A design that folds
reasoning from mid-stream deltas persists it, and the rule *"always echo whenever present"* then
re-sends it on every subsequent request in that session. Anthropic rejects an invalid signature with
a 400, so **every** `kolk resume` and every new turn fails identically, and the only recovery is hand-
editing `~/.config/kolk/sessions/*.json`. Assignment from `EventFinish` alone makes it unreachable.

**Why content is concatenated as escaped bytes.** Verified on go1.26.4: `json.Unmarshal("\"\\ud83d\"")`
→ `U+FFFD`, and raw split UTF-8 (`"\xf0\x9f"` + `"\x9a\x80"`) → four replacement runes. The bytes are
destroyed *inside* `json.Unmarshal`, so concatenating decoded Go strings can never recover them. For
prose that is a mangled emoji; for a `function.arguments` fragment it is a corrupted `path` or
`command` — `write_file` creating a file named with `U+FFFD`, or `bash` running a mangled command.
Fragmented arguments are not an edge case on the model family kolk will use most: OpenRouter sets
`eager_input_streaming: true` on every user-defined tool for **every** `stream:true` Anthropic
request. And no existing test could catch it, because the scripted mock only ever emits ASCII. The
fix is ~40 lines in one place.

**Why unknown, zero and free are three values.** Item 17's flagship view is rating-per-dollar.
`Meta{Cost float64}` today conflates *free* with *unreported*, so every local model ranks as
infinitely efficient — or, with the opposite convention, disappears from the view for lack of a
denominator. That would exclude exactly the models item 8's zero-config defaults are built from. The
Vercel AI SDK made this same correction between its v2 and v3 provider specs; taking it now is one
line, and taking it later is a schema migration.

**Why the catalog is the profile table.** `GET /api/v1/models` is key-free, live, 689 KB, and already
carries `supported_parameters`, `top_provider.max_completion_tokens`, `architecture.input_modalities`,
13 pricing keys and a per-model `reasoning{mandatory, supported_efforts, default_effort,
supports_max_tokens}` block. Today `ModelInfo` decodes 3 of 20 fields, which is why item 8's free
defaults cannot be computed at all (3 of the 22 free models have no tools, invisibly). Shipping a Go
quirk table means a binary release to fix a wrong `max_tokens`; catwalk hardcodes
`["low","medium","high"]` for every reasoning model and the live data shows at least eight distinct
vocabularies plus toggle and budget forms. Crush's three-tier resolution (remote → disk → embedded,
never "no providers") is the right shape; its missing TTL is the one thing kolk cannot copy, because
a 45-second conditional GET on every start is fine for a long-lived TUI and a 100× regression for a
10 ms CLI with a 30 ms CI-enforced budget.

**Why the second gateway is a preset.** In models.dev's own data, every gateway except Vercel and
OpenRouter is tagged `npm: "@ai-sdk/openai-compatible"`. Vercel's OpenAI surface is live,
unauthenticated and complete. The single fact that looked like it demanded an adapter — the
`provider.sort` vocabulary collision (`price|throughput|latency|exacto` vs `cost|ttft|tps`) — is
handled by making `Routing` kolk's own typed enum that each dialect translates, which is one table
row. That also removes the `strings.Contains(BaseURL,"openrouter.ai")` sniff, which was L3's only
implicit knowledge of which gateway it was talking to and which fails for a corporate proxy.

**Why native keys are deferred but the hole is pre-cut.** Anthropic-direct multiplies code paths for
capabilities OpenRouter already unifies. But the review found a concrete, non-hypothetical shape that
the flat three-bag message cannot represent: an interleaved
`thinking(sig) → text → tool_use → thinking(sig2) → tool_use2` assistant turn, which is the *normal*
shape under `interleaved-thinking-2025-05-14`. Order and thinking↔tool_use adjacency are
unrecoverable from `Content` + `ToolCalls` + `ReasoningDetails`. `Message.Blocks []json.RawMessage`
with a `BlocksFormat` tag, persisted (not `json:"-"`), is the slot that keeps a native adapter an
addition rather than a re-cut — and it costs nothing while no adapter writes it.

---

## Alternatives rejected

- **A retrying `Chat` decorator (`provider.Retry(Chat)`)** — replays a committed stream into an
  append-only event log; fantasy documents the hazard as a *caller obligation*, which is the wrong
  place for it. `policy.go` carries a comment saying so, because a contributor's instinct in six
  months will be to add one.
- **Seven always-on middleware (record · meter · fallback · retry · capability · cache · timeout)** —
  the ordering that makes per-attempt telemetry possible is the opposite of the one that makes
  "metered against the model that actually ran" possible, and the design that proposed it contradicted
  itself on exactly that point. Elegant; wrong at the seam that matters.
- **`iter.Seq[Event]`** — cannot be closed and cannot carry a terminal error, so an abandoned stream
  leaks the HTTP body (and keeps paying OpenRouter, which stops billing only on abort). kolk cancels
  turns by design.
- **A concurrent `Close()` that unblocks a parked `Next()`** — `net/http`'s `body.Read` holds `b.mu`
  across the blocking read and `Close` takes the same mutex, so it deadlocks until TCP gives up.
  Cancelling the request context is the only mechanism.
- **A second `AgentBackend` interface for agentcli** — would force `engine`, `orchestrator`, `saga`,
  `cli`, `serve` and `enginetest` to type-switch at every call site, and `Decide`, the event
  vocabulary, the stats mapping and the bus translation would all be written twice. One declared flag
  on a returned value beats a second interface.
- **Overloading `SessionID` as the vendor session handle** — kolk session ids are
  `20060102-150405-<hex>`; `claude --session-id` wants a UUID with its own lifetime and its own store.
  One field cannot be both, and the failure is on turn one.
- **A single combined `ToolResult` event for backend-run tools** — `claude -p` separates the
  announcement from the outcome by the tool's entire runtime, so one member forces either an
  empty-`Output` lie or a hidden 30-second stall.
- **Filtering the catalog at ingestion (catwalk's shape)** — right for Crush, wrong for kolk: chat
  mode needs no tools, the fast lane wants tiny cheap models, and item 8's defaults must see all 22
  free ids. Filter at query time.
- **Silent prompted pseudo-tools when a model lacks tool support** — the vendors themselves document
  it as unreliable, kolk's code-mode tools mutate the filesystem, item 17's leaderboard would pool two
  different execution paths, and the degradation already exists one layer down implemented by people
  who own the chat template. Opt-in only, with a mandatory stats tag.
- **Rotating free models on an account-scoped 429** — free rpm/rpd is billed to the *key* across all
  `:free` variants, so rotation burns one request per model against the very cap that is exhausted and
  then fails anyway. Conversely, defaulting a *signal-less* `:free` 429 to "account cap" prints a
  confidently false "add $10" message and disables the branch that would have worked.
- **Rotating on a 402** — free models are overwhelmingly small-context, so a 200k-turn conversation
  immediately overflows and compaction then mutates history the user never agreed to change. 402 is
  terminal, surfaced with the balance from `/key`.
- **`provider.order` exposed in config** — setting it disables sticky routing entirely and silently
  destroys prompt caching, which is the opposite of what a user reaching for it wants.
- **A Vercel-native adapter** — its wire protocol is the AI SDK's own `LanguageModelV4` spec, valuable
  only to AI SDK consumers, and its OpenAI surface is complete.
- **`Span`/`Recorder` inside `internal/provider`** — duplicates `engine.Recorder` (already declared at
  L4), writes telemetry out-of-band from the bus so a client resuming at `?from=seq` gets deltas but no
  usage, and makes item 17's schema churn edit L3 forever.
- **Keeping `usage:{include:true}` / `stream_options.include_usage`** — both are documented no-ops now
  and usage is always returned; keeping them keeps the base-URL sniff alive for nothing.

---

## Risks & open questions

**Unverified because no OpenRouter API key exists here and `/chat/completions` was never called.**
Everything in §6 comes from the OpenAPI spec and doc pages, not from observed bytes. In descending
order of what it would cost to be wrong:

1. **Is `reasoning_details.index` a stable per-block key or a per-chunk counter?** Nothing states it,
   and "the complete reasoning sequence is built by concatenating all chunks in order" is consistent
   with both. `ChatStreamDelta.reasoning_details` reuses the *non-streaming* `ReasoningDetailUnion`
   rather than declaring a delta type (unlike tool calls, which do have `ChatStreamToolCall`), which
   weakly favours "each chunk is a whole block, append them". The merge rule in §6.3 is safe under
   both readings, but "safe under both" is not "correct". → **One live call settles it. Do it before
   M8 ships.** A scripted mock cannot decide this.
2. **Do streamed `reasoning.encrypted` blocks arrive as `[REDACTED]`?** The docs warn they "may" and
   do not say what to do. If they do, `stream:true` + encrypted reasoning + tools is a genuine
   capability hole with no client-side fix; the fallback would be a non-streamed path for those models
   or accepted degraded continuity. → same live call.
3. **Does the top-level auto `cache_control` breakpoint really advance past `role:"tool"` messages in
   a real loop?** This is the single fact that decides whether §6.4's caching strategy saves money.
   → measurable with one paid session and a look at `prompt_tokens_details.cached_tokens`.
4. **Are free-tier 429s reliably distinguishable by the `X-RateLimit-*` headers?** §4.3's entire
   branch rests on it. If they are not, the *default* (rotate) is wrong in the safe direction — it
   burns a request rather than printing a false diagnostic.
5. **Is `X-Provider-Name` set on streaming responses?** Headers commit before the provider is
   necessarily final under fallback. Plausible, unconfirmed. `ProviderName` is `omitempty` everywhere,
   so an absence degrades to "unknown", not to a wrong value.
6. **The real cap on the `models` array.** No `maxItems` in the OpenAPI and no prose limit; the
   3-entry cap belongs to the Anthropic-skin `fallbacks` parameter kolk does not use. A 20-entry array
   is untested. `Policy.MaxRotations` bounds the client side regardless.
7. **How often do providers actually split a rune across SSE frames?** The corruption is verified;
   the *frequency* is not. The fix is cheap and the failure is silent, so it ships regardless.
8. **LM Studio's behaviour on unknown request fields is undocumented** — probe before relying on it;
   until then it inherits `generic`'s conservative stripping.
9. **Vercel AI Gateway `stream_options.include_usage`** is not in its documented parameter list.
10. Whether `X-OpenRouter-Metadata` or the legacy `X-OpenRouter-Experimental-Metadata` is
    authoritative today — the OpenAPI names the former; the errors page still uses the latter in a
    worked example. kolk sends the current name and treats an absent `openrouter_metadata` as "no
    information", which is the only safe reading either way.

**Design risks, with mitigations:**

- **`Meta`'s flat `PromptTokens/CompletionTokens/Cost` duplicate `Usage`.** Two sources of truth for
  one number, kept only so the footer prints `$0.0020` byte-identically and `stats.Append` compiles.
  → **Deletion is scheduled at architecture §12 step 12** and belongs in that PR description, not a
  TODO. The drift will otherwise be someone reading `Meta.Cost == 0` and concluding "free".
- **`Request.Extra` is an untyped hole** that will accumulate undocumented keys. → Rule, stated in
  `registry.go`: only `internal/cli` may populate it, from config; the engine never does.
- **`Message.Content` is still a `string` in v0.1.** It cannot express `content: null` (the documented
  shape for a tool-call-only assistant turn), so the client always emits `"content":""` — tolerated by
  OpenAI/OpenRouter, treated as a real empty text block by some strict vLLM chat templates. `Parts`
  is the reserved slot. → Defensible **only** because the recommended caching strategy is the
  gateway's automatic top-level breakpoint. The day vision ships this must change, and it is a session-
  format migration.
- **Capability is a five-way merge, and merges hide bugs.** A stale catalog can mark a model as
  tool-capable after the endpoint dropped it. → `CapSource` on every value so `kolk doctor` prints
  *why*, and probe verdicts are session-scoped rather than persisted.
- **Cooldowns are a new file kolk writes on a rate-limit path**, in a ~10 ms CLI. → small,
  append-shaped, flock-guarded, and **measured** at M9 rather than assumed.
- **Context-window accounting is an estimate.** kolk is stdlib-only and has no tokenizer, so
  `estTokens` is `len(bytes)/4` with a 15 % margin. It catches the gross case (a 4 MB tool result, a
  rotation candidate that cannot hold the history) and will not catch the marginal one — which is
  fine, because `KindContextOverflow` from the server still routes to compaction. Stated as an
  estimate, never as a guarantee.
- **A connect-phase retry may be billed twice upstream.** "Committed" is measured client-side (did we
  see bytes) while billing is decided upstream (did the model run). No gateway here honours an
  idempotency key. → The honest statement, written in `policy.go`: *a retried request may be billed
  twice, and kolk records both attempts.* This is why "we never replay a committed stream" is **not**
  offered as a complete answer to idempotency-on-resume; the tool-side answer is §3 T14.
- **Prompt caching is defeated one layer up**, by `agent.go:102-107` rebuilding the system prompt from
  live `os.Getwd()` and file contents on every construction. `session_id` fixes the *sticky key*;
  making the prompt itself stable belongs to items 6 and 12, and this doc is the citation.
- **`docs/research/openrouter.md` is stale and should be corrected in the same commit as M1.** Every
  URL moved (`/docs/features/*`, `/docs/use-cases/*`, `/docs/api-reference/*` → `/docs/guides/*`,
  `/docs/api_reference/*`; index at `/docs/llms.txt`, spec at `/docs/openapi/openapi.yaml`). Two of
  its claims are now **wrong**: `usage:{include:true}` is a deprecated no-op, and `route` is
  deprecated in favour of `provider.sort.partition`. Its live counts (422 models / 23 free) are also
  drift — 421/22 today — which is itself the argument for never hard-coding them.
- **`02-architecture.md` needs two one-line edits** (§2's `agentcli/detect_{unix,windows}.go` → a pure
  `detect.go`; `internal/provider/catalog/` added to §2 and to `internal/arch/layers.go`) and **one
  ordering amendment**: §12 step 10 must precede the reasoning round-trip, which §12's table does not
  currently say.

**Still genuinely open, deferred with a named unblocker:**

- **Whether `agentcli` should ever run a *kolk* tool.** Today: no, by construction. Revisit only if
  `claude -p` grows an MCP-style external tool surface. Unblocker: item 4 + a vendor capability.
- **Whether `provider/anthropic` lands in v0.5 or later.** Unblocker: a user who needs
  `thinking.budget_tokens` control or the Responses API `store` flag that OpenRouter does not expose.
  The slots are cut; the decision is a demand question, not a design one.

---

## Sources

Doc pages and the machine-readable spec were fetched as raw markdown/YAML on **2026-08-22**; live
probes are unauthenticated catalog endpoints only, same date. **No chat-completions call was made
against any provider.**

- OpenRouter OpenAPI spec — `https://openrouter.ai/docs/openapi/openapi.yaml` (1.36 MB, 39,093
  lines; **the authority** — the prose pages lag it). Types cited: `ChatRequest`, `ChatModelNames`,
  `DeprecatedRoute`, `ChatStreamChunk`, `ChatStreamDelta`, `ChatStreamToolCall`, `ChatToolCall`,
  `ChatToolMessage`, `ChatToolChoice`, `ChatFunctionTool`, `ChatUsage`, `CostDetails`,
  `ChatStreamOptions`, `GenerationResponse`, `ReasoningDetailUnion`, `ApiErrorType`,
  `AnthropicCacheControlDirective`, `ChatContentCacheControl`, `PromptCacheOptions`,
  `ChatFinishReasonEnum`, `ChatFormatJsonSchemaConfig`.
- OpenRouter docs index — `https://openrouter.ai/docs/llms.txt`; pages under `/docs/guides/*` and
  `/docs/api_reference/*` (model-fallbacks, provider-selection, reasoning-tokens, tool-calling,
  structured-outputs, prompt-caching, usage-accounting, errors-and-debugging, streaming, limits,
  auto-exacto).
- Live: `GET https://openrouter.ai/api/v1/models` — 421 models, 22 free, 352 with `tools`, 335 with
  `structured_outputs`, 288 with `reasoning`, 5 with `parallel_tool_calls`; `reasoning{}` present on
  289/141/123/10 rows; `cache-control: public, max-age=300, stale-while-revalidate=3600`.
- Live: `GET https://ai-gateway.vercel.sh/v1/models` — HTTP 200, unauthenticated, 352 models with
  `context_window`, `supported_parameters`, `pricing`, `tags`, `reasoning_options`,
  `supported_specifications`.
- Ollama — docs.ollama.com/api/openai-compatibility; source `openai/openai.go`, `middleware/openai.go`,
  `api/types.go`, `types/model/capability.go`.
- LM Studio — lmstudio.ai/docs (openai-compat endpoints, tools, chat-completions, models, REST list,
  API changelog).
- vLLM — docs.vllm.ai (tool_calling, openai_compatible_server); source
  `entrypoints/openai/*/protocol.py`.
- llama.cpp — `tools/server/README.md`, `docs/function-calling.md`, `tools/server/server-task.cpp`,
  `server-schema.cpp`, `server-common.h`.
- LiteLLM — docs.litellm.ai (user_keys, drop_params, provider_specific_params, response_headers,
  usage); source `litellm/types/router.py`, `model_prices_and_context_window.json` (3,111 entries).
- Vercel AI Gateway — vercel.com/docs/ai-gateway (openai-chat-completions, streaming, advanced, usage,
  observability).
- Prior art read via `gh api`, not cloned: **charmbracelet/fantasy** (`provider.go`, `model.go`,
  `retry.go`, `providers/openai/error.go`, `providers/kronk`), **charmbracelet/catwalk**
  (`pkg/catwalk`, `cmd/openrouter/main.go`, `.github/workflows/update.yml`), **charmbracelet/crush**
  (`internal/config/catwalk.go`, `internal/agent/agent.go`), **anomalyco/opencode**
  (`packages/core/src/models-dev.ts`, `plugin/provider/*`), **Vercel AI SDK**
  (`packages/provider/src/language-model/{v2,v3,v4}`, `packages/gateway`), **tmc/langchaingo**,
  **cloudwego/eino**, **openai/openai-go** (`packages/ssestream`), **Syngnat/GoNavi**
  (`internal/ai/provider/claude_cli.go`, 1,006 lines).
- models.dev — `https://models.dev/api.json` (4.3 MB, 193 providers, 7,251 rows, MIT, hourly sync),
  `models.json` (289 KB, 355 entries), `catalog.json`; all ETag-revalidated,
  `cache-control: public, max-age=0, must-revalidate`.
- Local, verified this session on go1.26.4 darwin/arm64: `json.Unmarshal` destroying lone surrogates
  and split UTF-8; `net/http` `transfer.go` `body.Read`/`body.Close` sharing `b.mu`; the current
  22-test breakdown (`checkpoint 4 · engine 5 · provider 2 · session 3 · stats 3 · tools 5`).
- In-repo: `PLAN.md` §0 and items 3, 4, 7, 8, 12, 14, 17 · `docs/plan/02-architecture.md` §§1, 2, 4,
  5, 7, 10, 11, 12 · `docs/research/openrouter.md` (with the corrections listed above) ·
  `docs/research/subscription-auth.md` · `docs/research/ecosystem.md` · `docs/research/dashboard.md`
  §4 · `docs/research/orcli.md` · the prototype: `internal/provider/{client.go,client_test.go}`,
  `internal/engine/{agent.go,orchestrator.go,agent_test.go}`, `internal/session/session.go`,
  `internal/tools/tools.go`, `internal/stats/stats.go`, `internal/enginetest/router.go`.
