package agentcli

// The codex adapter runs the user's own signed-in `codex` binary, one-shot per
// turn, and translates its JSONL event stream into the same events the Claude
// adapter speaks. Unlike Claude, codex mints its own conversation id — kolk
// cannot pre-claim one — so the handle arrives with thread.started and every
// later turn resumes it with `codex exec resume <thread_id>`.
//
// Non-negotiables from docs/plan/04-subscription-backends.md §8.1: spawn the
// user's binary, never touch its OAuth, never read ~/.codex/auth.json, never
// set CODEX_HOME, never pass a credential kolk obtained. Codex runs its own
// tools under its own sandbox; every tool event here is a record.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// CodexInvocation is the safe argv envelope for one Codex subscription turn.
// Prompt is fed on stdin: an argv prompt would publish every request to the
// process table for the length of the turn.
type CodexInvocation struct {
	Args   []string
	Prompt string
}

// RunCodex translates one provider-owned codex process into safe events.
func RunCodex(ctx context.Context, invocation CodexInvocation, onEvent func(Event)) error {
	return runCodex(ctx, invocation, shell.RunLines, onEvent)
}

func runCodex(ctx context.Context, invocation CodexInvocation, run lineRunner, onEvent func(Event)) error {
	if onEvent == nil {
		return fmt.Errorf("codex event handler is required")
	}
	return run(ctx, "codex", invocation.Args, strings.NewReader(invocation.Prompt+"\n"), func(line []byte) error {
		events, err := TranslateCodex(line)
		if err != nil {
			return err
		}
		for _, event := range events {
			onEvent(event)
		}
		return nil
	})
}

// codexEfforts is the closed set the vendor accepts through
// `-c model_reasoning_effort=…` (verified against codex-cli 0.149.1). kolk
// refuses an unknown level here, where the error names the valid set, instead
// of letting the vendor fail the turn with its own stack trace.
var codexEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true}

// CodexEffortValid reports whether one level belongs to the vendor's closed
// effort set.
func CodexEffortValid(effort string) bool {
	if effort == "" {
		return true
	}
	return codexEfforts[strings.ToLower(strings.TrimSpace(effort))]
}

// codexModeSandbox turns kolk's session mode into the vendor sandbox that
// enforces it. "chat cannot touch your files" is the vendor's read-only
// sandbox; code mode works because the vendor's own tool loop runs inside its
// workspace-write sandbox, which is where a file the code touches has to live
// (verified: a workspace-write run did the write; the default read-only one
// could not, exactly as promised). A mode change therefore changes the next
// process: codex exec replays no argv, because there is no persistent process.
func codexModeSandbox(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "chat":
		return "read-only", nil
	// Agent mode takes the workspace-write sandbox, the same as code mode.
	//
	// This was refused until each subagent got its own backend. The reason was
	// concrete: one CodexBackend holds one thread id and every turn resumes it
	// with `codex exec resume <thread>`, so several subagents on one backend
	// would interleave their turns into a single vendor transcript — one
	// conversation where there should be several. It was never the data race it
	// was once reported as; the field is guarded.
	//
	// A backend per subagent removes the sharing, so the reason is gone.
	case "", "code", "agent":
		return "workspace-write", nil
	default:
		return "", fmt.Errorf("unknown mode %q (chat|code|agent)", mode)
	}
}

// codexModelAliases maps nothing today — codex takes the plan catalog's ids
// verbatim (gpt-5.6-sol was accepted unaliased). The hook stays so the Claude
// adapter's shape has a counterpart and a future alias lands in one place.
func codexModelAlias(model string) string {
	return strings.TrimSpace(model)
}

// BuildCodexInvocation builds the documented non-interactive codex exec line.
// A thread id resumes that conversation through the vendor's `exec resume`
// subcommand; an empty one opens a thread the vendor names itself. The prompt
// rides on stdin, so a long request never sits in the process table.
func BuildCodexInvocation(model, mode, effort, handle string, resume bool, prompt string) (CodexInvocation, error) {
	sandbox, err := codexModeSandbox(mode)
	if err != nil {
		return CodexInvocation{}, err
	}
	if effort != "" && !CodexEffortValid(effort) {
		return CodexInvocation{}, fmt.Errorf("codex has no %q effort level; use low, medium, high or xhigh", effort)
	}
	if strings.TrimSpace(prompt) == "" {
		return CodexInvocation{}, fmt.Errorf("codex prompt cannot be empty")
	}
	// Verified by capture: `codex exec [--json --skip-git-repo-check] [-s
	// sandbox] [-m model] [-c model_reasoning_effort=…] [resume <id>]` with the
	// prompt on stdin. --skip-git-repo-check is what lets kolk work outside a
	// git repository without the vendor refusing to start.
	args := []string{"exec", "--json", "--skip-git-repo-check", "-s", sandbox}
	if model = codexModelAlias(model); model != "" {
		args = append(args, "-m", model)
	}
	if effort = strings.ToLower(strings.TrimSpace(effort)); effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+effort)
	}
	if handle != "" && resume {
		args = append(args, "resume", handle)
	}
	return CodexInvocation{Args: args, Prompt: prompt}, nil
}

// TranslateCodex converts one codex JSONL frame into allow-listed events. Non
// -JSON lines are real codex output on a shimmed machine (the version manager
// announces itself before the first frame), so an unparseable line is skipped,
// never an error: see spec/testdata/foreign/README.md. Unknown object shapes
// are dropped the same way — codex ships new item types between releases, and
// a translator that dies on them takes the turn with them.
func TranslateCodex(line []byte) ([]Event, error) {
	line = bytesTrimmed(line)
	if len(line) == 0 || line[0] != '{' {
		return nil, nil
	}
	var frame codexFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		return nil, nil //nolint:nilerr // a line that is not JSON is vendor noise, not a stream fault
	}
	switch frame.Type {
	case "thread.started":
		if frame.ThreadID == "" {
			return nil, nil
		}
		return []Event{{Kind: EventInit, SessionID: frame.ThreadID}}, nil
	case "turn.completed":
		if frame.Usage == nil {
			return nil, nil
		}
		return []Event{{Kind: EventUsage,
			InputTokens:   frame.Usage.InputTokens,
			CacheRead:     frame.Usage.CachedInputTokens,
			CacheCreation: frame.Usage.CacheWriteInputTokens,
			// codex reports the model's own answer tokens separately from its
			// reasoning tokens; both were produced by this turn, so both are
			// this turn's completion work.
			OutputTokens: frame.Usage.OutputTokens + frame.Usage.ReasoningOutputTokens,
		}}, nil
	case "error", "turn.failed":
		text := codexErrorText(frame)
		if text == "" {
			return nil, nil
		}
		return []Event{{Kind: EventError, Error: secret.Scrub(text)}}, nil
	case "item.started", "item.completed":
		// An agent_message only ever arrives completed; announcing it twice
		// would stream the answer twice.
		if frame.Item == nil || (frame.Item.Type == "agent_message" && frame.Type == "item.started") {
			return nil, nil
		}
		return codexItemEvents(frame.Item, frame.Type == "item.started"), nil
	default:
		// turn.started and shapes kolk has never seen. Dropping unknown frames
		// keeps a vendor release from breaking a session mid-turn.
		return nil, nil
	}
}

// codexItemEvents projects one codex item. An agent_message arrives whole
// (there are no deltas on this stream), a command_execution reports its output
// and exit, a file_change lists the paths it touched — each under its own
// announced type, so no id pairing is required the way Claude's tool_use/
// tool_result split needs one. Started is reported because a completed
// command_execution carries its command line too: the distinction lives in the
// frame type, not in which fields happen to be set.
func codexItemEvents(item *codexItem, started bool) []Event {
	switch item.Type {
	case "agent_message":
		if item.Text == "" {
			return nil
		}
		text := secret.Scrub(item.Text)
		// Streamed as a delta so the answer arrives while the turn is still
		// open; the completed event that follows makes it the final answer.
		// Codex interleaves prose between tool runs, and each later message
		// becomes the vendor's latest word.
		return []Event{{Kind: EventMessageDelta, Text: text}, {Kind: EventMessageCompleted, Text: text}}
	case "command_execution":
		if started {
			// The announcement: what the vendor is about to run. The outcome
			// trails it under the same id.
			return []Event{{Kind: EventTool, ToolName: "shell", ToolCallID: item.ID,
				ToolInput: secret.Scrub(item.Command)}}
		}
		output := ""
		if item.AggregatedOutput != "" {
			output = secret.Scrub(item.AggregatedOutput)
		}
		return []Event{{Kind: EventTool, ToolCallID: item.ID,
			ToolOutput: output, ToolIsError: item.ExitCode != nil && *item.ExitCode != 0}}
	case "file_change":
		detail := make([]string, 0, len(item.Changes))
		for _, change := range item.Changes {
			detail = append(detail, change.Kind+" "+secret.Scrub(change.Path))
		}
		if started {
			return []Event{{Kind: EventTool, ToolName: "file-change", ToolCallID: item.ID,
				ToolInput: strings.Join(detail, ", ")}}
		}
		return []Event{{Kind: EventTool, ToolCallID: item.ID, ToolOutput: strings.Join(detail, ", ")}}
	case "error":
		if item.Message == "" {
			return nil
		}
		// An error item is a warning prose line on an otherwise live turn (the
		// codex-error.jsonl fixture opens with one). The terminal turn.failed
		// that follows carries the cause; recording this one as the failure
		// would cut the turn short on a vendor's own deprecation notice.
		return []Event{{Kind: EventTool, ToolName: "codex-warning", ToolCallID: item.ID,
			ToolInput: secret.Scrub(item.Message)}}
	}
	return nil
}

// codexErrorText unwraps the vendor's real error. It nests the API error as a
// JSON-encoded string inside the prose; decoding it names the failure ("The
// 'gpt-4.1' model is not supported when using Codex with a ChatGPT account.")
// instead of a wall of escaped braces — codex-error.jsonl shows the shape.
func codexErrorText(frame codexFrame) string {
	encoded := frame.Message
	if encoded == "" && frame.Error != nil {
		encoded = frame.Error.Message
	}
	if encoded == "" {
		return ""
	}
	var nested struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(encoded), &nested); err == nil && nested.Error.Message != "" {
		return nested.Error.Message
	}
	return encoded
}

func bytesTrimmed(line []byte) []byte {
	return []byte(strings.TrimSpace(string(line)))
}

type codexFrame struct {
	Type     string     `json:"type"`
	ThreadID string     `json:"thread_id"`
	Item     *codexItem `json:"item"`
	Message  string     `json:"message"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
	Usage *codexUsage `json:"usage"`
}

type codexItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Message is only set on type:"error" items.
	Message string `json:"message"`
	// Text is only set on type:"agent_message" items.
	Text string `json:"text"`
	// Command and AggregatedOutput/ExitCode belong to command_execution items;
	// Changes and Status to file_change ones.
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
	ExitCode         *int   `json:"exit_code"`
	Status           string `json:"status"`
	Changes          []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`
}

type codexUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	CacheWriteInputTokens int `json:"cache_write_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

// CodexBackend adapts the user's codex CLI to the engine chat seam. Unlike the
// Claude backend there is no process to keep between turns: codex exec already
// costs one process per turn, so the backend carries only model, mode, effort
// and the thread handle across them.
type CodexBackend struct {
	Model  string
	Mode   string
	Effort string
	// thread is the vendor conversation this backend drives, once the vendor
	// has minted one. An empty one opens a thread; every later turn resumes
	// the one reported by thread.started. kolk mints nothing here, because
	// codex names its own threads and accepts no claim on them.
	thread string
	run    lineRunner
	mu     sync.Mutex
}

// CodexKnowsModel reports whether this adapter can spawn a model.
//
// Codex takes the plan catalogue's ids verbatim — it has no alias table — so
// the answer is whether the id is one of the codex rungs kolk ranks. Asked
// before a spawn rather than after, because a model the vendor rejects costs a
// whole turn to discover.
func CodexKnowsModel(model string) bool {
	for _, rung := range codexRungs {
		if strings.EqualFold(strings.TrimSpace(model), rung) {
			return true
		}
	}
	return false
}

// codexRungs are the codex models kolk knows how to ask for. It mirrors the
// codex ladder in the engine; the two are checked against each other by
// TestEverySpawnableRungIsAModelItsAdapterAccepts. OpenAI documents Sol as the
// flagship, Terra as the balanced tier, and Luna as the cost-efficient tier.
var codexRungs = []string{"gpt-5.6-pro", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}

// NewCodexBackendFromHandle creates a backend that resumes one vendor thread
// (resume true, handle non-empty) or opens a brand-new one the vendor names.
func NewCodexBackendFromHandle(model, mode, effort, handle string, resume bool) (*CodexBackend, error) {
	if _, err := codexModeSandbox(mode); err != nil {
		return nil, err
	}
	if effort != "" && !CodexEffortValid(effort) {
		return nil, fmt.Errorf("codex has no %q effort level; use low, medium, high or xhigh", effort)
	}
	return &CodexBackend{
		Model:  model,
		Mode:   strings.ToLower(strings.TrimSpace(mode)),
		Effort: effort,
		thread: strings.TrimSpace(handle),
	}, nil
}

// ProviderHandle reports the vendor thread this backend has driven most
// recently; "" until the vendor has minted one.
func (b *CodexBackend) ProviderHandle() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.thread
}

// Close is part of the backend seam. One-shot turns leave nothing running.
func (b *CodexBackend) Close() error { return nil }

func (b *CodexBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	// The tool schemas the gateway seam passes are deliberately ignored: codex
	// owns tool execution behind its sandbox and never sees kolk's schemas.
	prompt, err := promptFromMessages(messages)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	b.mu.Lock()
	invocation, err := BuildCodexInvocation(model, b.Mode, b.Effort, b.thread, b.thread != "", prompt)
	b.mu.Unlock()
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	start := time.Now()
	events := make([]Event, 0, 8)
	// The trail streams as the vendor works: each tool run is named when it
	// starts and its outcome under the same id, so kolk neither executes nor
	// pretends to have executed anything.
	pending := make(map[string]string)
	runner := b.run
	run := RunCodex
	if runner != nil {
		run = func(ctx context.Context, invocation CodexInvocation, onEvent func(Event)) error {
			return runCodex(ctx, invocation, runner, onEvent)
		}
	}
	err = run(ctx, invocation, func(event Event) {
		events = append(events, event)
		if event.SessionID != "" {
			b.mu.Lock()
			b.thread = event.SessionID
			b.mu.Unlock()
		}
		if onToken == nil {
			return
		}
		switch event.Kind {
		case EventMessageDelta:
			onToken(event.Text)
		case EventTool:
			if line := toolTrail(event, pending); line != "" {
				onToken(line)
			}
		}
	})
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, err
	}
	message, meta, err := Collect(events, time.Since(start))
	if meta.Model == "" {
		meta.Model = model
	}
	return message, meta, err
}
