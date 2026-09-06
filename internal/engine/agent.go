// Package engine ties the API client, tools, session persistence, file
// checkpoints and local stats together, and implements the release modes:
//
//	chat  — plain conversation, no tools
//	code  — the classic agentic coding loop (tools until a final answer)
package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/mcp"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"github.com/onembyte/kolkrabbi/internal/tools"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

const (
	colorReset = "\033[0m"
	colorDim   = "\033[2m"
	colorCyan  = "\033[36m"
	colorYel   = "\033[33m"
	colorMag   = "\033[35m"
)

// Modes and efforts.
const (
	ModeChat  = "chat"
	ModeCode  = "code"
	ModeAgent = "agent"
)

var Modes = []string{ModeChat, ModeCode, ModeAgent}

// Posture is the internal purpose of an agent run. It is separate from Mode:
// mode selects the tool loop, while posture tells the system prompt which
// durable workflow owns the turn. A posture is never a user-selectable model
// or provider value.
type Posture string

const (
	PostureSaga Posture = "saga"
)

const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
	EffortMax    = "max"
	// EffortUltra is the fifth rung (V34.4b, owner decision 2026-09-05): above
	// max on every dimension the dial governs, and the rung through which a
	// vendor's own `ultra` is reached. It was a legacy spelling of max before.
	EffortUltra = "ultra"
)

var CanonicalEfforts = []string{EffortLow, EffortMedium, EffortHigh, EffortMax, EffortUltra}

var Efforts = []string{EffortLow, EffortMedium, EffortHigh, EffortMax, "xhigh", "quick", "standard", "deep", "ultra"}

// NormalizeEffort maps any valid effort name, alias, or numeric string (1..4)
// to its canonical value ("low", "medium", "high", "max"). It returns false
// if the input is not recognized.
func NormalizeEffort(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low", "l", "1", "quick":
		return EffortLow, true
	case "medium", "med", "m", "2", "standard":
		return EffortMedium, true
	case "high", "h", "3", "deep":
		return EffortHigh, true
	case "max", "xhigh", "x", "4":
		return EffortMax, true
	case "ultra", "u", "5":
		return EffortUltra, true
	default:
		return "", false
	}
}

// MaxRoundsFor returns the maximum tool rounds permitted for a single turn,
// governed by mode and active effort level.
func MaxRoundsFor(mode string, effort string) int {
	eff, ok := NormalizeEffort(effort)
	if !ok {
		eff = EffortMedium
	}
	if mode == ModeChat {
		switch eff {
		case EffortLow:
			return 2
		case EffortHigh:
			return 12
		case EffortMax:
			return 20
		case EffortUltra:
			return 30
		default: // EffortMedium
			return 6
		}
	}
	// ModeCode / ModeAgent
	switch eff {
	case EffortLow:
		return 4
	case EffortHigh:
		return 24
	case EffortMax:
		return 50
	case EffortUltra:
		return 80
	default: // EffortMedium
		return 12
	}
}

// TimeoutForEffort returns the bash command timeout for a given effort level.
func TimeoutForEffort(effort string) time.Duration {
	eff, ok := NormalizeEffort(effort)
	if !ok {
		eff = EffortMedium
	}
	switch eff {
	case EffortLow:
		return 30 * time.Second
	case EffortHigh:
		return 300 * time.Second
	case EffortMax:
		return 600 * time.Second
	case EffortUltra:
		return 900 * time.Second
	default: // EffortMedium
		return 120 * time.Second
	}
}

// projectMemoryFiles are looked up in the working directory, first match
// wins; their content is appended to the system prompt (like CLAUDE.md).
var projectMemoryFiles = []string{"KOLKRABBI.md", "AGENTS.md"}

// maxMemoryBytes caps each memory layer. Notes are prepended to every request,
// so an unbounded file is a permanent tax on the window that the user never
// sees being charged.
const maxMemoryBytes = 16 * 1024

const emptyCompletionRecovery = "The previous response was empty. Continue the original user request now. Use tools when needed, and finish the requested concrete step or explain a specific blocker."

const (
	activityThinking     = "thinking"
	activityPlanning     = "planning"
	activityWorking      = "working"
	activitySynthesizing = "synthesizing"
)

// ActivityIndicator observes the lifetime of one logical provider call. Start
// must return promptly; its stop function may join any indicator-owned work so
// the engine can guarantee that presentation is gone before output continues.
type ActivityIndicator interface {
	Start(context.Context, string) func()
}

// PersistentActivityIndicator opts into keeping the activity indicator visible
// while visible model tokens are being written. Redraw-based surfaces can keep
// the spinner in their owned status row without interleaving terminal bytes;
// legacy line renderers must omit this interface so their spinner cannot race
// streamed text on the same output line.
type PersistentActivityIndicator interface {
	ActivityIndicator
	KeepActivityDuringOutput() bool
}

// WorkIndicator presents one local tool action independently from provider
// waiting. Surfaces may render it ephemerally; the engine still writes one
// durable, human-readable activity line to Out.
type WorkIndicator interface {
	StartWork(context.Context, string) func()
}

// Options configures an Agent; zero values get sensible defaults.
type Options struct {
	Client  *provider.Client
	Backend ChatBackend
	// Routes are the backends that answer for an owned model-id prefix
	// (ownedPrefixes), keyed by the prefix without its slash. A host Ollama is
	// registered here as "ollama" and reached as ollama/<model>. Separate from
	// Backend so the plan-or-gateway swap never disturbs them (E2).
	Routes map[string]ChatBackend
	Model  string // the session's base model
	Mode   string // chat | code | agent (default code)
	Effort string // quick | standard | deep | ultra (default standard)
	// Permission is how much may happen without asking. It never removes the
	// floor: no tier allows an action the hardline rules refuse.
	Permission Permission
	// Sandbox confines bash commands when set (plan 13 §7.2); nil is off,
	// which is the default by the owner's decision. It is built by the CLI
	// from Root, so the two cannot disagree.
	Sandbox *shell.Sandbox
	// Rules are the user's standing answers, consulted before the tier and
	// after the floor. The last matching rule wins.
	Rules Rules
	// ExtraSystem is a standing instruction appended to the system prompt,
	// used by plan mode to say what the session is for. Empty most of the time.
	ExtraSystem string
	// Posture is an internal workflow marker. PostureSaga adds its short
	// progression directive to system construction without putting workflow
	// instructions into user messages or durable conversation turns.
	Posture Posture
	// Isolator gives each writing subagent a tree of its own (plan 36). Nil
	// keeps every subagent in the shared tree, one writer at a time.
	Isolator Isolator
	// MaxConcurrentTasks is how many orchestrated tasks may run at once. Three
	// is small enough that the output of that many agents can still be read,
	// and rate limits rather than CPU are the binding constraint. One is
	// sequential, which is what an unlabelled plan gets anyway.
	MaxConcurrentTasks int
	// Slots maps a named orchestration role (orchestrator, worker, explore,
	// fast) to a model. Anything unset falls back to the session model, so an
	// empty map is today's behaviour rather than a broken configuration.
	Slots map[string]string
	// MaxRunCostUSD stops an orchestrated run once it has cost this much.
	// Zero means no ceiling, which is the default: a limit nobody chose would
	// be a surprise the first time it truncated real work.
	MaxRunCostUSD float64
	// Root confines file tools. Empty disables confinement, which only tests
	// and scripts should ever want.
	Root     string
	Sess     SessionPort
	Ckpt     Checkpointer  // may be nil (checkpointing disabled)
	In       *bufio.Reader // shared stdin reader; may be nil when nothing asks
	Out      io.Writer     // defaults to os.Stdout
	Recorder Recorder      // records stats/ratings; nil disables stats
	Clock    Clock         // nil defaults to time.Now
	// RetryWait is the cancellable wait used between bounded provider retries.
	// Nil selects the real timer; tests inject it to keep retry gates instant.
	RetryWait func(context.Context, time.Duration) error
	Activity  ActivityIndicator
	Work      WorkIndicator
	Decider   Decider
	// SubagentBackend opens a provider for one orchestrated task, inside the
	// declared envelope, so each subagent gets its own vendor process instead
	// of sharing the session's. Nil means they share, which is what happens
	// today for a host that has not wired it.
	SubagentBackend SubagentBackend
	// SubagentCapabilities is the host-verified execution envelope. Network
	// access is decided per task from SubagentNetwork; an empty envelope is
	// not a permission to fall back to the parent process's directory.
	SubagentCapabilities SubagentCapabilities
	// SubagentNetwork is the network policy for delegated children: auto
	// (research tasks only, plus vendors with no switch), on, or off. Empty
	// means auto. The envelope's NetworkAccess is decided per task from this,
	// never copied from SubagentCapabilities.
	SubagentNetwork string
	// RungAvailable reports whether a vendor can run one of its cheaper
	// models on this machine. Nil means none can, so a run stays on the
	// session's own model — which is what happens today.
	RungAvailable RungAvailable
	// Ask puts a fixed-option question to the person running the session. Nil
	// means nobody can be asked, and the model is told to decide and say what
	// it assumed rather than to wait for an answer that cannot come.
	Ask Chooser
	// Tiers maps effort level -> model id. Missing tiers fall back to Model,
	// so everything works zero-config and tiers are a pure optimization.
	// OnFreeExhausted is free (default), paid or stop: what a run does when no
	// free model it can reach will answer (B12.13).
	OnFreeExhausted string
	// OnSubscriptionLimit is ask (default), switch or stop: what a run does
	// when the subscription behind it runs out of allowance mid-turn.
	OnSubscriptionLimit string
	// MeteredModel names the per-token model to fall back to when that
	// happens. Nil, or an empty answer, means there is no metered fallback and
	// a limit ends the run. The surface supplies it because only the surface
	// knows which providers were actually configured.
	MeteredModel func() string
	// Cooldowns remembers classified limits across calls and sessions (plan 35
	// §2.1). Nil means no memory: every limit is met as if for the first time.
	Cooldowns *Cooldowns
	// ConnectorName says which connector a model runs through, for the cooldown
	// keys; nil means everything is the keyed endpoint.
	ConnectorName func(model string) string
	// ExtraTools are tools beyond the built-ins — MCP servers' — namespaced
	// <server>__<tool>, merged into the tool set outside chat, and asked for
	// permission through the same rules as every other tool (plan 16 §3).
	ExtraTools ExtraTools
	// Candidates lists what could continue the work when the session's model
	// hits a limit (plan 35 §2.3): the surface assembles them, the engine ranks
	// and shows them, nothing is applied. Nil means the block only says what
	// stopped and that kolk resumes.
	Candidates func() []continuity.Candidate
	// Select is how the chain is chosen when the session continues (plan 35
	// §2.4): auto (the default) walks the equivalents in the owner's order;
	// preferred walks Preferred, the person's own list, in their order. The
	// settings that feed these are V35.5's.
	Select    string
	Preferred []string
	// Order is the billing groups to try in order (plan 35 §2.4); nil is
	// the owner's default, subscriptions, then paid, then free.
	Order []string
	// ContinuityMode is off (default) or on: whether a pause walks the chain
	// by itself (V35.6).
	ContinuityMode string
	// Switch moves the session to a candidate (plan 35 §2.5): the surface owns
	// the backends, so it performs the switch and returns the label it settled
	// on. Nil means the chain cannot be walked and /continue says so.
	Switch func(context.Context, continuity.Candidate) (string, error)
	// ResumePolicy is auto (default) or manual: whether a paused session comes
	// back on its own once the monitor sees the limit lifted, or waits for
	// /resume (plan 35 §2.2). The monitor spends no tokens either way.
	ResumePolicy string
	// ProbeLimit confirms a limit has lifted without spending tokens. Nil means
	// the built-in probe: key status for the keyed gateway, /models for a
	// compatible endpoint, the sign-in check for a handover.
	ProbeLimit func(context.Context, continuity.Pause) (bool, error)
	// ResumeReady hands a resumed turn back to the surface, which runs it on
	// the same model. Nil means the pause is lifted and the turn is announced
	// as waiting, never run behind the user's back.
	ResumeReady func(pending string)
	// ResumeWait is the cancellable wait the resume monitor uses between the
	// pause and its probe; separate from RetryWait so the two are never
	// confused. Nil means a plain timer.
	ResumeWait func(context.Context, time.Duration) error
	// HandoverSignedIn says whether a vendor connector is still signed in, the
	// quota-free check a handover has. Nil trusts the reset clock.
	HandoverSignedIn func(connector string) bool
	Tiers            map[string]string
	Bus              *bus.Bus
	PinnedModel      bool
	FreeModels       []string
	// ContextWindow is the active model's advertised context size, or zero when
	// it is unknown. Surfaces resolve it from the catalog; the engine never
	// guesses one, because compaction is destructive and a guessed limit would
	// throw away conversation on no evidence.
	ContextWindow int
	// ModelRatings is what this machine has thought of each model, from its own
	// usage log. Empty means no opinion, which is the normal state.
	ModelRatings map[string]ModelRating

	// Catalog is the live model catalogue, when the host has one. Empty means
	// slot selection has nothing to choose from and the effort model stands.
	Catalog []provider.ModelInfo

	// Agents is told how many subagents are running whenever that changes, so a
	// surface can show it. Nil means nobody is watching.
	Agents func(running int)
	// Subagents receives the typed lifecycle of each orchestrated task. Unlike
	// Agents, it can render which task is working, on which model and effort.
	// Callers must be concurrency-safe: several task goroutines publish here.
	Subagents func(SubagentStatus)

	// PostWrite is called after a file-modifying tool succeeded, so the host
	// can run hooks. It cannot veto: the work is already done, which is what
	// keeps a hook from being a second permission system.
	PostWrite func(tool, path string)

	// DirtyFiles reports paths with uncommitted changes, or nil when this
	// project is not a repository and nil when the host supplies no way to
	// look. The engine touches no OS, so the host provides this.
	DirtyFiles func(context.Context) []string

	// UserMemoryFile is the user's own standing notes, applied to every
	// project. Empty means none; surfaces resolve the path.
	UserMemoryFile string
	// ArchiveCompaction stores the conversation a compaction replaced and
	// reports where. Surfaces own the filesystem; nil simply means undo lives
	// only as long as the process.
	ArchiveCompaction func([]provider.Message) (string, error)
}

// ChatBackend is the engine's provider seam. The existing OpenRouter client
// remains the default, while provider-owned CLIs can implement the same turn
// contract without changing orchestration.
type ChatBackend interface {
	StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error)
}

type Agent struct {
	Options
	lastTurnID string
	// loadExtraOnce starts the extra tool servers on the first tool listing.
	loadExtraOnce sync.Once
	// hopsThisRun bounds the automatic chain per run, and askedThisRun keeps
	// `select ask` to one question per run (plan 35 §2.4).
	hopsThisRun  int
	askedThisRun bool
	// resume is the monitor watching this session's pause, nil when nothing is
	// paused; resumeMu guards its hand-over between the pause path, /resume
	// and Close.
	resumeMu   sync.Mutex
	resume     *resumeMonitor
	resumeBase context.Context
	// mainWork serializes the parent turn's durable work ledger. Subagent work
	// has one sequence per task under subagentMu; the parent has one sequence
	// per turn and never carries child coordinates.
	mainWorkMu       sync.Mutex
	mainWorkTurn     string
	mainWorkSequence uint64
	// subagentIDs pairs a task index with the id both of its lifecycle events
	// carry, and subagentIDTurn is the turn they belong to. Both are under
	// subagentMu: the per-task goroutines mint ids concurrently.
	subagentIDs    map[int]string
	subagentStatus map[int]SubagentStatus
	subagentIDTurn string
	// subagentRunning is how many subagents are working, written from a
	// goroutine per task and read by whatever is drawing the screen.
	subagentMu      sync.Mutex
	subagentRunning int
	// slotChoice remembers the model picked for each slot, so a plan ranks the
	// catalogue once per slot rather than once per task.
	slotMu     sync.Mutex
	slotChoice map[string]string
	// A subscription's allowance belongs to the session, not to a task: eight
	// subagents hitting the same limit must raise one question, not eight.
	// limitDecided records that it has been settled and limitModel what was
	// settled on, empty meaning the run stops.
	limitMu      sync.Mutex
	limitDecided bool
	limitModel   string
	// lastPromptTokens is what the provider reported reading on the most recent
	// main turn, which is the only measured view of how full the window is.
	// Atomic because the status footer samples it from a drawing goroutine
	// while the turn loop writes it (see Agent.Context).
	lastPromptTokens atomic.Int64
	// modelMu guards Model and Backend, the only two Options fields that change
	// after construction. See session_model.go for why they need it.
	modelMu    sync.RWMutex
	preCompact []provider.Message
	// runSpend accumulates the cost of the orchestrated run in progress, and
	// is nil the rest of the time.
	runSpend *spend
	// sessionSpend accumulates every call this session has made. The footer
	// answers "what did that cost"; this is the only thing that can answer
	// "what has this session cost", which is the question that decides whether
	// to keep going.
	sessionSpend spend
	// statsWarnOnce keeps the stats warning to one line even when several
	// subagents hit the same broken recorder at the same moment.
	statsWarnOnce sync.Once
	// rulesMu guards Rules: a rule kept from a confirmation is written while
	// tool calls may still be reading it, and phase F runs several at once.
	rulesMu     sync.RWMutex
	lastArchive string
	saveWarned  bool
}

// Close releases resources owned by the configured backend, when it exposes
// an optional lifecycle.
func (a *Agent) Close() error {
	var first error
	a.stopResumeMonitor()
	// Routes first: a host server kolk started is the thing most worth
	// stopping, and it must stop even if the session backend's Close fails.
	for _, route := range a.Routes {
		if closer, ok := route.(io.Closer); ok {
			if err := closer.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	if closer, ok := a.sessionBackend().(io.Closer); ok {
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// New wires up an agent around an existing (possibly resumed) session. The
// session's system prompt is (re)generated so cwd and project memory are
// always current, and any dangling tool calls from an interrupted run are
// repaired so the next API call is valid.
func New(o Options) *Agent {
	if o.Backend == nil {
		o.Backend = o.Client
	}
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.Mode == "" {
		o.Mode = ModeCode
	}
	if o.Permission == "" {
		// The safe tier is the default. Every other tier is something the user
		// asked for out loud.
		o.Permission = DefaultPermission
	}
	if o.Root == "" {
		// Confinement is on unless a caller deliberately clears it, so the
		// default is the directory Kolkrabbi was started in.
		if cwd, err := os.Getwd(); err == nil {
			o.Root = cwd
		}
	}
	if o.Effort == "" {
		o.Effort = EffortMedium
	} else if canonical, ok := NormalizeEffort(o.Effort); ok {
		o.Effort = canonical
	}
	if o.Tiers == nil {
		o.Tiers = map[string]string{}
	}
	if o.RetryWait == nil {
		o.RetryWait = waitForRetry
	}
	if o.ResumeWait == nil {
		o.ResumeWait = waitForRetry
	}
	if o.Decider == nil && o.In != nil {
		o.Decider = NewTerminalDecider(o.In, o.Out)
	}
	a := &Agent{Options: o}

	if o.Sess != nil {
		sys := provider.Message{Role: "system", Content: a.systemPrompt(o.Mode)}
		msgs := o.Sess.GetMessages()
		if len(msgs) == 0 {
			o.Sess.AppendMessage(sys)
		} else {
			msgs[0] = sys
			o.Sess.SetMessages(msgs)
		}
		a.repairDanglingToolCalls()
		if o.Sess.ModelName() == "" {
			o.Sess.SetModelName(o.Model)
		}
	}
	return a
}

// SetMode switches mode and refreshes the system prompt accordingly.
// SetSandbox turns the OS sandbox on (a policy) or off (nil) for the rest of
// the session. It is a plain setter on purpose: whether a policy CAN be
// enforced is the CLI's question to ask before calling this, so the refusal
// happens at `/sandbox on`, not silently at the next command.
func (a *Agent) SetSandbox(policy *shell.Sandbox) { a.Sandbox = policy }

func (a *Agent) SetMode(mode string) error {
	for _, m := range Modes {
		if m == mode {
			a.Mode = mode
			if a.Sess != nil {
				// Written on switch, so a resume lands in this mode (plan 06 §3).
				a.Sess.SetMode(mode)
				msgs := a.Sess.GetMessages()
				if len(msgs) > 0 {
					msgs[0] = provider.Message{Role: "system", Content: a.systemPrompt(mode)}
					a.Sess.SetMessages(msgs)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("unknown mode %q (chat|code|agent)", mode)
}

// SetPosture changes the internal workflow purpose of the current agent and
// refreshes the existing session's system message. The empty posture is the
// ordinary session; SAGA is the only named workflow posture. Restricting the
// values here keeps a caller from smuggling arbitrary user text into system
// construction while still letting an inline SAGA request reuse one session.
func (a *Agent) SetPosture(posture Posture) error {
	switch posture {
	case Posture(""), PostureSaga:
		// accepted internal values
	default:
		return fmt.Errorf("unknown posture %q", posture)
	}
	a.Posture = posture
	a.refreshSystemPrompt()
	return nil
}

func (a *Agent) refreshSystemPrompt() {
	if a.Sess == nil {
		return
	}
	msgs := a.Sess.GetMessages()
	if len(msgs) == 0 || msgs[0].Role != "system" {
		return
	}
	msgs[0] = provider.Message{Role: "system", Content: a.systemPrompt(a.Mode)}
	a.Sess.SetMessages(msgs)
	a.save()
}

// SetEffort validates, normalizes and sets the effort level.
func (a *Agent) SetEffort(effort string) error {
	if canonical, ok := NormalizeEffort(effort); ok {
		a.Effort = canonical
		return nil
	}
	return fmt.Errorf("invalid effort %q: expected low (1), medium (2), high (3), max (4), or ultra (5)", effort)
}

// ModelForEffort resolves the model for an effort level (accepting any canonical,
// numeric, or legacy alias): configured tier if set, otherwise the session's base model.
func (a *Agent) ModelForEffort(effort string) string {
	return a.modelFor(effort)
}

// modelFor resolves the model for an effort level: configured tier if set,
// otherwise the session's base model. Zero-config always works.
func (a *Agent) modelFor(effort string) string {
	eff, ok := NormalizeEffort(effort)
	if !ok {
		eff = effort
	}
	// 1. Direct canonical tier hit
	if m, ok := a.Tiers[eff]; ok && m != "" {
		return m
	}
	// 2. Legacy tier key fallback if user config used "quick", "standard", etc.
	// "ultra" is no longer max's legacy spelling: it is the fifth rung's own
	// tier and was matched above.
	switch eff {
	case EffortLow:
		if m, ok := a.Tiers["quick"]; ok && m != "" {
			return m
		}
	case EffortMedium:
		if m, ok := a.Tiers["standard"]; ok && m != "" {
			return m
		}
	case EffortHigh:
		if m, ok := a.Tiers["deep"]; ok && m != "" {
			return m
		}
	}
	return a.SessionModel()
}

// ExtraTools is a tool set beyond the built-ins. Execute reports handled
// false for a name that is not its own.
type ExtraTools interface {
	Definitions() []provider.Tool
	Execute(ctx context.Context, name, args string) (result string, handled bool, err error)
}

// toolsFor returns the tool set for a mode: chat gets none; the others get
// the built-ins and whatever extra tools the surface attached.
func (a *Agent) toolsFor(ctx context.Context, mode string) []provider.Tool {
	if mode == ModeChat {
		return nil
	}
	defs := tools.Definitions()
	if a.ExtraTools != nil {
		if loader, ok := a.ExtraTools.(interface {
			Load(context.Context) []mcp.Report
		}); ok {
			a.loadExtraOnce.Do(func() {
				for _, r := range loader.Load(ctx) {
					if r.Err != nil {
						fmt.Fprintf(a.Out, "◆ mcp %s: %v\n", r.Name, r.Err)
					}
				}
			})
		}
		defs = append(defs, a.ExtraTools.Definitions()...)
	}
	return defs
}

func (a *Agent) systemPrompt(mode string) string {
	cwd, _ := os.Getwd()
	var sys string
	switch mode {
	case ModeChat:
		sys = fmt.Sprintf(`You are Kolkrabbi, in chat mode: a concise, direct assistant in a terminal on %s (working directory %s). You have no tools in this mode: answer from knowledge, and if a task needs file access or shell commands, say so and suggest switching to code mode (/mode code).`, runtime.GOOS, cwd)
	default:
		sys = fmt.Sprintf(`You are Kolkrabbi, a fast, terminal-based coding agent running on %s in the directory %s.

You have tools to read/write/edit files, list directories, and run shell commands. Use them proactively to accomplish what the user asks instead of just describing what you would do. Prefer small, verifiable steps: read before you edit, run tests/builds after changing code when reasonable.

Be concise in your prose responses. Do not narrate every tool call at length; let the tool results speak for themselves and summarize only what matters.`, runtime.GOOS, cwd)
		sys += `

When asked to build or continue a project, inspect the relevant plan and checkpoint, select one concrete unfinished checkpoint, and carry it through implementation and verification. Do not stop after inspection: keep using tools until that checkpoint is complete, or state a concrete blocker and the evidence for it.

Some decisions are the user's rather than yours: which framework or database to build on, which of several designs to take, what to do about work you were not asked for. When one of those would change what you build and the code does not already answer it, call ask_user with the alternatives and put your recommendation first. Ask once, early, before building on a guess -- an assumption discovered three files later costs more than a question. Everything else you decide yourself: do not ask for permission to continue, for confirmation of something you can read, or for anything you would answer the same way whatever they said.`
	}

	// The user's own notes first, the project's second: a project statement wins
	// a contradiction by being nearer the task.
	if body, ok := readMemory(a.UserMemoryFile); ok {
		sys += fmt.Sprintf("\n\nYour notes (from %s):\n%s", a.UserMemoryFile, body)
	}
	for _, name := range projectMemoryFiles {
		body, ok := readMemory(name)
		if !ok {
			continue
		}
		sys += fmt.Sprintf("\n\nProject notes (from %s):\n%s", name, body)
		break
	}
	if extra := strings.TrimSpace(a.ExtraSystem); extra != "" {
		// Last, so a posture the user just chose wins a contradiction with
		// anything standing.
		sys += "\n\n" + extra
	}
	if a.Posture == PostureSaga {
		// This is deliberately a fixed internal system directive. Repeating it
		// in each SAGA user prompt would spend context and could be lost during
		// compaction; placing it here makes the posture durable and inspectable.
		sys += "\n\n" + sagaPostureInstruction
	}
	return sys
}

const sagaPostureInstruction = `SAGA posture is active. Advance one bounded chapter at a time, verify it with the configured quality gates, record an honest outcome, and stop at the wake boundary. Do not claim completion without evidence.`

// SetExtraSystem changes the standing instruction appended to the system
// prompt and rebuilds it in the running session.
//
// Mutating the system prompt mid-session costs the provider's prompt cache,
// which is why loop wakeups are injected as user turns instead. This is the
// exception: it happens when a person deliberately changes what the session is
// for, at most twice per plan, and the alternative — telling the model its
// posture in a user message — puts an instruction in the transcript that
// compaction may later summarise away.
func (a *Agent) SetExtraSystem(extra string) {
	a.ExtraSystem = extra
	if a.Sess == nil {
		return
	}
	msgs := a.Sess.GetMessages()
	if len(msgs) == 0 || msgs[0].Role != "system" {
		return
	}
	msgs[0] = provider.Message{Role: "system", Content: a.systemPrompt(a.Mode)}
	a.Sess.SetMessages(msgs)
	a.save()
}

// readMemory loads one memory file, capped at a line boundary.
//
// Cutting at a byte offset can split a UTF-8 rune, which puts invalid bytes in
// every request the session makes: a corrupt prompt rather than a long one. The
// cut is also announced, because notes that silently stop being followed
// halfway down the file are impossible to debug from the outside.
func readMemory(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	raw, err := os.ReadFile(name)
	if err != nil || len(raw) == 0 {
		return "", false
	}
	if len(raw) <= maxMemoryBytes {
		return string(raw), true
	}
	cut := raw[:maxMemoryBytes]
	if boundary := bytes.LastIndexByte(cut, '\n'); boundary > 0 {
		cut = cut[:boundary]
	}
	return string(cut) + fmt.Sprintf("\n[truncated: %s is larger than %d bytes]", name, maxMemoryBytes), true
}

// repairDanglingToolCalls handles a session that was interrupted between an
// assistant message with tool_calls and its tool results: the API rejects a
// history where tool calls have no results, so we append synthetic ones.
func (a *Agent) repairDanglingToolCalls() {
	if a.Sess == nil {
		return
	}
	msgs := a.Sess.GetMessages()
	last := -1
	for i, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			last = i
		}
	}
	if last == -1 {
		return
	}
	answered := map[string]bool{}
	for _, m := range msgs[last+1:] {
		if m.Role != "tool" {
			return // a non-tool message follows, so this block was completed
		}
		answered[m.ToolCallID] = true
	}
	for _, tc := range msgs[last].ToolCalls {
		if !answered[tc.ID] {
			msgs = append(msgs, provider.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    "Interrupted before this tool ran. Re-issue the call if it is still needed.",
			})
		}
	}
	a.Sess.SetMessages(msgs)
}

func (a *Agent) save() {
	if a.Sess == nil {
		return
	}
	// Through Out, not os.Stderr: in a session Out is the terminal renderer,
	// which owns the screen, and anything printed around it lands outside the
	// rows it manages and scribbles over the composer.
	if err := a.Sess.Save(); err != nil && !a.saveWarned {
		fmt.Fprintf(a.Out, "\nwarning: could not save session: %v\n", err)
		a.saveWarned = true
	}
}

// record appends a stats line; never fatal, warn once.
func (a *Agent) record(role string, meta provider.Meta, toolCalls int) {
	a.recordAtEffort(role, meta, toolCalls, a.Effort)
}

// recordAtEffort records work that intentionally runs at a different effort
// from the parent session, as orchestrated subagents do.
func (a *Agent) recordAtEffort(role string, meta provider.Meta, toolCalls int, effort string) {
	// Accounted before the recorder is consulted: what a run costs is true
	// whether or not stats are being written anywhere.
	a.runSpend.add(meta.Cost)
	a.sessionSpend.add(meta.Cost)
	a.sessionSpend.noteBilling(meta.Billing)

	if a.Recorder == nil || a.Sess == nil {
		return
	}
	err := a.Recorder.RecordCall(CallRecord{
		Session:             a.Sess.SessionID(),
		Turn:                a.lastTurnID,
		Mode:                a.Mode,
		Effort:              effort,
		Role:                role,
		Model:               meta.Model,
		PromptTokens:        meta.PromptTokens,
		CompletionTokens:    meta.CompletionTokens,
		CacheReadTokens:     meta.CacheReadTokens,
		CacheCreationTokens: meta.CacheCreationTokens,
		Cost:                meta.Cost,
		Billing:             meta.Billing,
		Ms:                  meta.Elapsed.Milliseconds(),
		ToolCalls:           toolCalls,
	})
	if err != nil {
		a.statsWarnOnce.Do(func() {
			fmt.Fprintf(a.Out, "\nwarning: could not record stats: %v\n", err)
		})
	}
}

// RateLast attaches a 1–5 rating to the most recent turn.
func (a *Agent) RateLast(rating int) error {
	if rating < 1 || rating > 5 {
		return fmt.Errorf("rating must be 1-5")
	}
	if a.lastTurnID == "" {
		return fmt.Errorf("nothing to rate yet")
	}
	if a.Recorder == nil || a.Sess == nil {
		return fmt.Errorf("stats are disabled")
	}
	return a.Recorder.RecordRating(a.Sess.SessionID(), a.lastTurnID, rating)
}

func (a *Agent) confirm(ctx context.Context, confirmation Confirmation) (bool, protocol.PermissionDecision) {
	if a.Decider == nil {
		return false, protocol.PermissionDecisionDeny
	}

	action, detail := confirmation.Action, confirmation.Detail
	permID := xid.New(xid.Call)
	turnID := a.lastTurnID
	if turnID == "" {
		turnID = xid.New(xid.Turn)
	}

	if a.Bus != nil {
		reqData, _ := json.Marshal(protocol.PermissionRequestedData{
			ID:     permID,
			Tool:   action,
			Detail: detail,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: turnID,
			Type: protocol.EventPermissionRequested,
			Data: reqData,
		})
	}

	var decision protocol.PermissionDecision
	if pd, ok := a.Decider.(PolicyDecider); ok {
		decision = pd.Decide(ctx, confirmation)
	} else if a.Decider.Confirm(ctx, confirmation) {
		decision = protocol.PermissionDecisionAllow
	} else {
		decision = protocol.PermissionDecisionDeny
	}

	allowed := (decision == protocol.PermissionDecisionAllow || decision == protocol.PermissionDecisionAllowSession)

	if a.Bus != nil {
		resData, _ := json.Marshal(protocol.PermissionResolvedData{
			ID:       permID,
			Decision: decision,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: turnID,
			Type: protocol.EventPermissionResolved,
			Data: resData,
		})
	}

	return allowed, decision
}

// Judge decides one action the way this session would: the floor first, then
// the user's standing rules, then the tier. It is exported because the rules a
// session is running under are worth being able to inspect from outside it.
func (a *Agent) Judge(r tools.Request) (Verdict, string) {
	a.rulesMu.RLock()
	rules := a.Rules
	a.rulesMu.RUnlock()
	return a.Permission.judgeWith(rules, r)
}

// guard applies the session's permission tier to one tool action.
//
// Every outcome is visible: a refusal says why, and an action taken without
// asking because the tier allows it still announces itself when it leaves the
// project. Autonomy nobody can read afterwards is not autonomy anyone can
// trust.
func (a *Agent) guard(ctx context.Context, out io.Writer) tools.Guard {
	return func(r tools.Request) bool {
		verdict, reason := a.Judge(r)
		switch verdict {
		case VerdictDeny:
			fmt.Fprintf(out, "%s✗ refused: %s%s\n", colorDim, reason, colorReset)
			return false
		case VerdictAllow:
			if r.Outside {
				a.noteReachingOutside(out, r, reason)
			}
			return true
		default:
			rule := suggestRule(r)
			allowed, decision := a.confirm(ctx, Confirmation{
				Action: actionLabel(r), Detail: r.Detail, Rule: rule,
			})
			if allowed && decision == protocol.PermissionDecisionAllowSession {
				a.keepRule(out, rule)
			}
			return allowed
		}
	}
}

// subagentGuard is the policy for work running without a person watching.
//
// A subagent has no terminal: prompting from one either deadlocks, or shows the
// user a question they read as coming from the main session and answer about
// the wrong work. So anything the tier would ask about is refused instead, and
// the refusal says how to allow it — by choosing a tier, which is a decision
// the user makes once and can review, rather than by answering a prompt nobody
// saw.
func (a *Agent) subagentGuard(ctx context.Context, out io.Writer) tools.Guard {
	main := a.guard(ctx, out)
	return func(r tools.Request) bool {
		verdict, reason := a.Judge(r)
		if verdict != VerdictAsk {
			return main(r)
		}
		fmt.Fprintf(out, "%s✗ subagent refused: %s — subagents cannot ask; use /auto-approve or /full-auto to widen what they may do%s\n",
			colorDim, reason, colorReset)
		return false
	}
}

// keepRule adds a rule the user accepted at the prompt to this session.
//
// It goes into the same list /permissions shows, rather than into a private
// cache, so "always" is something the user can look at and take back. It is not
// written to disk: the prompt says how to make it permanent, and a rule that
// silently outlives the session someone approved it in is one nobody consented
// to.
func (a *Agent) keepRule(out io.Writer, line string) {
	parsed, err := ParseRule(line)
	if err != nil {
		return
	}
	a.rulesMu.Lock()
	defer a.rulesMu.Unlock()
	for _, existing := range a.Rules {
		if existing.Source == line {
			return
		}
	}
	a.Rules = append(a.Rules, parsed)
	fmt.Fprintf(out, "%s  kept for this session: %s — /permissions to review, add `always` to keep it for good%s\n",
		colorDim, line, colorReset)
}

// noteReachingOutside records, in the transcript, that Kolkrabbi went outside
// the project without asking, and what for. In full-auto this line is the only
// account of it anyone will have.
func (a *Agent) noteReachingOutside(out io.Writer, r tools.Request, reason string) {
	purpose := strings.TrimSpace(r.Summary)
	if purpose == "" {
		purpose = "no description given"
	}
	fmt.Fprintf(out, "%s◆ outside the project: %s %s — %s%s\n",
		colorDim, r.Tool, r.Display, purpose, colorReset)
	if a.Bus != nil {
		data, _ := json.Marshal(map[string]string{
			"tool": r.Tool, "path": r.Display, "purpose": purpose, "reason": reason,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: a.lastTurnID,
			Type: protocol.EventToolOutput,
			Data: data,
		})
	}
}

// actionLabel names an action for a confirmation prompt.
func actionLabel(r tools.Request) string {
	switch r.Tool {
	case "bash":
		return "Run shell command"
	case "write_file":
		return "Write file " + r.Display
	case "edit_file":
		return "Edit file " + r.Display
	case "read_file":
		return "Read file " + r.Display
	case "list_dir":
		return "List directory " + r.Display
	default:
		return r.Tool
	}
}

// preWrite is the checkpoint hook handed to tools.Execute.
func (a *Agent) preWrite(tool, path string) error {
	if a.Ckpt == nil {
		return nil
	}
	return a.Ckpt.Record(tool, path)
}

func (a *Agent) responseLabel() string { return "kolk-" + a.Mode }

func describeToolCall(tc provider.ToolCall) string {
	var args struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		Path        string `json:"path"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "Using tool — " + compactToolText(secret.Scrub(tc.Function.Name))
	}
	args.Command = secret.Scrub(args.Command)
	args.Description = secret.Scrub(args.Description)
	args.Path = secret.Scrub(args.Path)
	path := compactToolPath(args.Path)
	switch tc.Function.Name {
	case "bash":
		detail := compactToolText(args.Description)
		if detail == "" {
			detail = compactToolText(args.Command)
		}
		if detail != "" {
			return "Running command — " + detail
		}
	case "read_file":
		if path != "" {
			return "Reading file — " + path
		}
	case "write_file":
		if path != "" {
			return "Writing file — " + path
		}
	case "edit_file":
		if path != "" {
			return "Editing file — " + path
		}
	case "list_dir":
		if path == "" {
			path = "."
		}
		return "Listing directory — " + path
	}
	return "Using tool — " + compactToolText(secret.Scrub(tc.Function.Name))
}

// activityWidth is how much of one tool line is shown. Long enough to be
// useful, short enough not to wrap on a normal terminal.
const activityWidth = 120

func compactToolText(value string) string {
	value = sanitizeToolText(value)
	runes := []rune(value)
	if len(runes) > activityWidth {
		// A command reads left to right: what it is matters more than its last
		// flag, so this keeps the beginning.
		value = string(runes[:activityWidth]) + "…"
	}
	return value
}

// compactToolPath shortens a path by dropping its middle.
//
// The end of a path is what says which file this is. Cutting there — which is
// what happened until a macOS runner produced a long enough temp directory to
// show it — leaves a person approving "somewhere under /private/var", with the
// filename the one part they cannot see.
func compactToolPath(value string) string {
	value = sanitizeToolText(value)
	runes := []rune(value)
	if len(runes) <= activityWidth {
		return value
	}
	// Enough of the head to know where it is, the rest given to the tail.
	const head = 24
	tail := activityWidth - head - 1
	return string(runes[:head]) + "…" + string(runes[len(runes)-tail:])
}

func sanitizeToolText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

// executeTool runs one tool for the main session, where a person can answer.
func (a *Agent) executeTool(ctx context.Context, tc provider.ToolCall) (string, error) {
	return a.executeToolWith(ctx, tc, a.Out, a.Effort, a.guard, true, a.Root)
}

// executeSubagentTool runs one tool for an orchestrated subagent, where nobody
// can.
// executeSubagentTool runs one tool for a subagent, rooted at the tree the
// task runs in: its own worktree when it has one, the session's root
// otherwise. An empty root means the session's.
func (a *Agent) executeSubagentTool(ctx context.Context, tc provider.ToolCall, out io.Writer, effort, root string) (string, error) {
	if root == "" {
		root = a.Root
	}
	return a.executeToolWith(ctx, tc, out, effort, a.subagentGuard, false, root)
}

func (a *Agent) executeToolWith(ctx context.Context, tc provider.ToolCall, out io.Writer, effort string, guard func(context.Context, io.Writer) tools.Guard, mayAsk bool, root string) (string, error) {
	// Answered before any of the machinery below: a question waits on a person,
	// so a spinner saying "working" and a confinement guard over a path neither
	// applies nor makes sense.
	if tc.Function.Name == toolAskUser {
		return a.askUser(ctx, tc.Function.Arguments, out, mayAsk)
	}
	description := describeToolCall(tc)
	fmt.Fprintf(out, "%s  → %s%s\n", colorDim, description, colorReset)
	stopWork := func() {}
	if a.Work != nil {
		if stop := a.Work.StartWork(ctx, description); stop != nil {
			stopWork = stop
		}
	}
	defer stopWork()
	toolCtx := ctx
	if tc.Function.Name == "bash" {
		var cancel context.CancelFunc
		toolCtx, cancel = context.WithTimeout(ctx, TimeoutForEffort(effort))
		defer cancel()
	}
	if a.ExtraTools != nil && isMCPTool(tc.Function.Name) {
		// An MCP tool asks the same way bash does: through the rules, then
		// the person. Its arguments are the preview.
		if g := guard(toolCtx, out); g != nil && !g(tools.Request{Tool: tc.Function.Name, Summary: "run " + tc.Function.Name, Detail: tc.Function.Arguments}) {
			return "", fmt.Errorf("%s: not permitted", tc.Function.Name)
		}
		result, handled, err := a.ExtraTools.Execute(toolCtx, tc.Function.Name, tc.Function.Arguments)
		if handled {
			return secret.Scrub(result), err
		}
	}
	// A write in a task's own tree is not a write in the user's tree: the
	// landing is what changes that, under its own snapshot, so the file-level
	// record for /undo is kept only for writes to the session's root.
	preWrite := a.preWrite
	if root != a.Root {
		preWrite = nil
	}
	result, err := tools.Execute(toolCtx, tc.Function.Name, tc.Function.Arguments, tools.Options{
		Root:      root,
		Sandbox:   a.Sandbox,
		Guard:     guard(toolCtx, out),
		PreWrite:  preWrite,
		PostWrite: a.PostWrite,
	})
	// One chokepoint for every tool. A result goes into the conversation, the
	// session file on disk and every later request to the provider, so a
	// credential that survives this line is a credential Kolkrabbi has copied
	// and kept. The event bus scrubs separately; the conversation is the copy
	// that leaves the machine.
	return secret.Scrub(result), err
}

func (a *Agent) footer(meta provider.Meta) {
	if a.Bus != nil {
		inToks := int64(meta.PromptTokens)
		outToks := int64(meta.CompletionTokens)
		ttft := int64(meta.Elapsed.Milliseconds())
		cost := meta.Cost
		usageData, _ := json.Marshal(protocol.UsageReportedData{
			Model:            meta.Model,
			ProviderName:     "openrouter",
			RequestModel:     meta.Model,
			InputTokens:      &inToks,
			OutputTokens:     &outToks,
			CostUSD:          &cost,
			CostSource:       protocol.UsageCostReported,
			Measurement:      protocol.UsageMeasurementMetered,
			TTFTMilliseconds: &ttft,
			Attempt:          1,
			Role:             "main",
			Effort:           a.Effort,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: a.lastTurnID,
			Type: protocol.EventUsageReported,
			Data: usageData,
		})
	}
	cost := ""
	if meta.Cost > 0 {
		cost = fmt.Sprintf(" · $%.4f", meta.Cost)
	}
	toks := ""
	if meta.PromptTokens+meta.CompletionTokens > 0 {
		toks = fmt.Sprintf(" · %d tok", meta.PromptTokens+meta.CompletionTokens)
	}
	// How full the window is, whenever the model's window is known. A user who
	// can watch it fill can see a compaction coming instead of being surprised
	// by the model forgetting something.
	window := ""
	if label := a.contextUsage(meta.PromptTokens).Label(); label != "" {
		window = " · " + label
	}
	fmt.Fprintf(a.Out, "%s  [%s · %s%s%s%s · %dms]%s\n",
		colorDim, a.Mode, meta.Model, toks, window, cost, meta.Elapsed.Milliseconds(), colorReset)
}

// SessionCostUSD is what every call in this session has cost so far,
// orchestrated subagents included.
func (a *Agent) SessionCostUSD() float64 { return a.sessionSpend.total() }

// SessionBilling is how this session's calls have been billed: one of the
// provider.Billing* modes, "mixed", or empty before any call.
func (a *Agent) SessionBilling() string { return a.sessionSpend.billingMode() }

// Context is how full the window is, measured the same way the turn footer
// measures it. Exported because the status line is where someone looks before
// deciding whether to compact, and making them run a command to find out is
// the surface failing at its one job.
func (a *Agent) Context() ContextUsage { return a.contextUsage(int(a.lastPromptTokens.Load())) }

// contextUsage measures the active model's window against what the provider
// last reported reading.
func (a *Agent) contextUsage(lastPromptTokens int) ContextUsage {
	var messages []provider.Message
	if a.Sess != nil && lastPromptTokens <= 0 {
		messages = a.Sess.GetMessages()
	}
	return MeasureContext(a.window(), lastPromptTokens, messages)
}

// windowReporter is what a routed backend implements when it knows a model's
// window better than the catalogue does — a host server, whose effective
// window is whatever it loaded the model with.
type windowReporter interface {
	ContextWindow(model string) int
}

// window is the active model's context size: the session's own when it has
// one, else whatever the routed backend can say, else unknown. A known window
// is never overridden by a route — the gateway catalogue is the truth for
// gateway models — and a route that cannot say leaves it unknown.
func (a *Agent) window() int {
	if a.ContextWindow > 0 {
		return a.ContextWindow
	}
	backend, wire, err := a.backendFor(a.SessionModel())
	if err != nil {
		return 0
	}
	if reporter, ok := backend.(windowReporter); ok {
		return reporter.ContextWindow(wire)
	}
	return 0
}

// RunTurn dispatches a user message according to the current mode.
func (a *Agent) RunTurn(ctx context.Context, userInput string) error {
	// A paused session spends nothing until its limit lifts (plan 35 §2.2).
	if paused := a.stillPaused(); paused != nil {
		return paused
	}
	// The rung travels with the turn so a keyed vendor client can say it in
	// the vendor's word; the gateway and compatible endpoints ignore it.
	ctx = provider.WithEffort(ctx, a.Effort)
	pending := userInput
	a.lastTurnID = xid.New(xid.Turn)
	a.resetMainWork()
	if a.Ckpt != nil {
		a.Ckpt.BeginTurn(ctx)
	}
	if a.Sess != nil {
		a.Sess.SetTitleFromInput(userInput)
	}
	// Beside the turn rather than in the system prompt: dirty state changes
	// every turn, and the system prompt is the one thing that must not.
	if preamble := a.dirtyTreePreamble(ctx); preamble != "" {
		userInput = preamble + "\n\n" + userInput
	}
	a.compactIfNeeded(ctx)

	if a.Bus != nil {
		startedData, _ := json.Marshal(protocol.TurnStartedData{
			Input:  userInput,
			Model:  a.modelFor(a.Effort),
			Mode:   a.Mode,
			Effort: a.Effort,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: a.lastTurnID,
			Type: protocol.EventTurnStarted,
			Data: startedData,
		})
	}

	var err error
	if a.Mode == ModeAgent {
		err = a.runOrchestrated(ctx, userInput)
	} else {
		err = a.runLoop(ctx, userInput)
	}

	// After the turn, never before it: naming is a nicety, and making the user
	// wait on it to read the answer they asked for gets the priority backwards.
	if err == nil {
		a.titleSessionIfNeeded(ctx)
	}
	// A limit that waiting will lift pauses the session instead of failing the
	// turn; the pause owns its own terminal events. Any other classified limit
	// is a stop, published as one; the finished event below still follows.
	if err == nil {
		a.hopsThisRun, a.askedThisRun = 0, false
	}
	if err != nil {
		if paused, ok := a.pauseIfWaitingHelps(ctx, err, pending); ok {
			// With continuity on, the chain is walked by itself (V35.6): the
			// session moves to the next equivalent and the waiting turn runs
			// there, in this same turn, so the person sees the answer.
			if next, ok := a.autoContinue(ctx, paused.Pause); ok {
				return a.RunTurn(ctx, next)
			}
			return &paused
		}
		if limit, ok := provider.Classify(err); ok && ctx.Err() == nil {
			a.publishLimit(limit, "stop")
			a.printRecommendation(limit)
		}
	}
	if a.Mode == ModeAgent {
		state := protocol.WorkStateDone
		step := "completed"
		if err != nil {
			state = protocol.WorkStateFailed
			step = "failed: " + err.Error()
			if ctx.Err() != nil {
				step = "cancelled"
			}
		}
		a.publishMainWork(state, protocol.WorkPhaseComplete, step, a.orchestrationModel(), a.Effort)
	}
	if a.Bus != nil {
		if err != nil {
			if ctx.Err() != nil {
				cancelledData, _ := json.Marshal(protocol.TurnCancelledData{Reason: "cancelled"})
				_, _ = a.Bus.Publish(bus.Event{
					Turn: a.lastTurnID,
					Type: protocol.EventTurnCancelled,
					Data: cancelledData,
				})
			} else {
				// An ordinary error is the third way a turn ends, and a subscriber
				// that saw turn.started must see exactly one terminal event for it
				// (V34.2d). The finished reason vocabulary is open by contract:
				// "error", with the scrubbed message as the raw reason.
				failedData, _ := json.Marshal(protocol.TurnFinishedData{Reason: "error", RawReason: secret.Scrub(err.Error())})
				_, _ = a.Bus.Publish(bus.Event{
					Turn: a.lastTurnID,
					Type: protocol.EventTurnFinished,
					Data: failedData,
				})
			}
		} else {
			finishedData, _ := json.Marshal(protocol.TurnFinishedData{Reason: "stop"})

			_, _ = a.Bus.Publish(bus.Event{
				Turn: a.lastTurnID,
				Type: protocol.EventTurnFinished,
				Data: finishedData,
			})
		}
	}
	return err
}

// runLoop is the chat/code path: stream the reply, execute any tool calls,
// loop until the model returns a plain answer. The session is saved as it
// goes.
func (a *Agent) runLoop(ctx context.Context, userInput string) error {
	if a.Sess != nil {
		a.Sess.AppendMessage(provider.Message{Role: "user", Content: userInput})
		a.save()
	}

	model := a.modelFor(a.Effort)
	toolset := a.toolsFor(ctx, a.Mode)
	var requestMessages []provider.Message
	if a.Sess != nil {
		requestMessages = a.Sess.GetMessages()
	} else {
		requestMessages = []provider.Message{
			{Role: "system", Content: a.systemPrompt(a.Mode)},
			{Role: "user", Content: userInput},
		}
	}
	emptyCompletions := 0
	// One turn, one counter: asking for the same thing in two different turns
	// is a person repeating themselves, which is allowed.
	var loop doomLoop
	overflowRecovered := false
	toolRounds := 0
	maxRounds := MaxRoundsFor(a.Mode, a.Effort)
	var observeProvider func(provider.ProgressEvent)
	if a.Mode == ModeAgent {
		observeProvider = a.mainProviderProgress(model, a.Effort)
	}

	for {
		fmt.Fprintf(a.Out, "%s%s%s ", colorCyan, a.responseLabel(), colorReset)
		msg, meta, err := a.streamChatObserved(ctx, activityThinking, model, requestMessages, toolset, func(tok string) {
			if a.Bus != nil {
				deltaData, _ := json.Marshal(protocol.MessageDeltaData{Text: tok})
				_, _ = a.Bus.Publish(bus.Event{
					Turn: a.lastTurnID,
					Type: protocol.EventMessageDelta,
					Data: deltaData,
				})
			}
			fmt.Fprint(a.Out, tok)
		}, observeProvider)
		if err != nil {
			fmt.Fprintln(a.Out)
			// A refusal for length is recoverable exactly once: compact what the
			// session is carrying and ask again, rather than losing the turn.
			if a.Sess != nil && !overflowRecovered && provider.IsContextOverflow(err) {
				overflowRecovered = true
				if a.recoverFromOverflow(ctx) {
					requestMessages = a.Sess.GetMessages()
					continue
				}
			}
			return err
		}
		fmt.Fprintln(a.Out)
		if a.Bus != nil && msg.Content != "" {
			completedData, _ := json.Marshal(protocol.MessageCompletedData{Text: msg.Content})
			_, _ = a.Bus.Publish(bus.Event{
				Turn: a.lastTurnID,
				Type: protocol.EventMessageCompleted,
				Data: completedData,
			})
		}
		a.lastPromptTokens.Store(int64(meta.PromptTokens))
		// A provider that runs its own tool loop returns a message with none of
		// the calls in it — the backend's meta is the only count there is.
		toolRuns := len(msg.ToolCalls)
		if meta.ToolCalls > toolRuns {
			toolRuns = meta.ToolCalls
		}
		a.record("main", meta, toolRuns)
		if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
			emptyCompletions++
			if emptyCompletions >= 2 {
				return fmt.Errorf("model returned two empty responses; try `/model` to select another model")
			}
			fmt.Fprintln(a.Out, colorDim+"  (empty model response; retrying once)"+colorReset)
			if a.Sess != nil {
				requestMessages = appendMessage(a.Sess.GetMessages(), provider.Message{
					Role: "user", Content: emptyCompletionRecovery,
				})
			} else {
				requestMessages = appendMessage(requestMessages, provider.Message{
					Role: "user", Content: emptyCompletionRecovery,
				})
			}
			continue
		}

		if a.Sess != nil {
			a.Sess.AppendMessage(msg)
			a.save()
		}

		if len(msg.ToolCalls) == 0 {
			a.footer(meta)
			return nil // final answer for this turn
		}
		emptyCompletions = 0
		toolRounds++
		if toolRounds > maxRounds {
			return fmt.Errorf("exceeded maximum tool rounds (%d) for %s effort", maxRounds, a.Effort)
		}

		for _, tc := range msg.ToolCalls {
			owner := a.mainToolWork(model, a.Effort)
			a.publishKolkToolRequested(tc, owner)
			// Checked before the call runs, because a call that has proved
			// three times over that it changes nothing should not be made a
			// fourth time. The result half of the rule comes from the two that
			// already settled.
			var result string
			var err error
			executed := false
			skipped := false
			if loop.wouldRepeat(tc.Function.Name, tc.Function.Arguments) {
				denial, stop := a.answerDoomLoop(ctx, &loop, tc, false)
				if stop != nil {
					return stop
				}
				result = denial
				skipped = result != ""
			}
			if result == "" {
				executed = true
				a.publishKolkToolStarted(tc, owner)
				result, err = a.executeTool(ctx, tc)
				if err != nil {
					result = "Error: " + err.Error()
				}
				loop.observe(tc.Function.Name, tc.Function.Arguments, result)
			}
			if skipped {
				a.publishKolkToolSkipped(tc, owner)
			}
			a.publishKolkToolOutput(tc, result, owner)
			if executed {
				a.publishKolkToolFinished(tc, err, owner)
			}
			if a.Sess != nil {
				a.Sess.AppendMessage(provider.Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    result,
				})
			}
		}
		if a.Sess != nil {
			a.save()
			requestMessages = a.Sess.GetMessages()
		}
		// loop: send tool results back to the model for its next step
	}
}

func appendMessage(messages []provider.Message, message provider.Message) []provider.Message {
	copyOfMessages := make([]provider.Message, len(messages), len(messages)+1)
	copy(copyOfMessages, messages)
	return append(copyOfMessages, message)
}

// Rewind undoes the file changes of the most recent turn (files only; the
// conversation itself is untouched). Returns the restored paths.
func (a *Agent) Rewind(ctx context.Context) ([]string, error) {
	if a.Ckpt == nil {
		return nil, fmt.Errorf("checkpointing is not enabled")
	}
	return a.Ckpt.RewindLastTurn(ctx)
}
