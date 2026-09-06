package agentcli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// CopilotInvocation is one run of the GitHub Copilot CLI on the contract
// observed live on 2026-09-06 (CLI 1.0.82, Free plan; V34.4c.2b): `-p PROMPT`
// runs one prompt and exits; `--output-format json` streams JSONL events;
// `-s` keeps stats off stdout; `--resume <id>` continues the session the
// terminal `result` named; `--model` names a model or `auto` (the Free plan
// accepts only `auto`); `--effort` is honoured on an explicit model and
// refused on `auto`; `--allow-all-tools` is what lets a non-interactive run
// act — without it every tool is denied and the model answers anyway. The
// four privacy flags keep the session on this machine: no export to
// github.com, no remote control, no auto-update, no colour.
type CopilotInvocation struct {
	Args           []string
	ProcessOptions shell.ProcessOptions
}

// CopilotBinary is the executable the vendor installs (`npm install -g
// @github/copilot`, `brew install --cask copilot-cli`).
const CopilotBinary = "copilot"

// copilotEfforts is the vendor's own dial as its help lists it (1.0.82).
var copilotEfforts = map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// copilotProviderEffort projects kolk's rung onto the vendor's dial: the
// vendor has both xhigh and max, so kolk's max is xhigh and ultra is max.
func copilotProviderEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "max":
		return "xhigh"
	case "ultra":
		return "max"
	}
	return strings.ToLower(strings.TrimSpace(effort))
}

// BuildCopilotInvocationWithOptions shapes the argv. Every tool is allowed
// only under kolk's full-auto in a tool mode; chat mode grants nothing. An
// empty model is the vendor's `auto`, and an effort travels only with an
// explicit model the vendor lists it for.
func BuildCopilotInvocationWithOptions(model, mode, effort, handle, prompt string, options ExecutionOptions) (CopilotInvocation, error) {
	options, err := normalizeExecutionOptions(options)
	if err != nil {
		return CopilotInvocation{}, err
	}
	if strings.TrimSpace(prompt) == "" {
		return CopilotInvocation{}, fmt.Errorf("copilot prompt cannot be empty")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	model = strings.TrimSpace(model)
	args := []string{"-p", prompt, "-s", "--output-format", "json",
		"--no-remote-export", "--no-remote", "--no-auto-update", "--no-color"}
	if model != "" && model != "auto" {
		args = append(args, "--model", model)
		if effort = copilotProviderEffort(effort); effort != "" {
			if !effortAllowed(effort, options.Efforts, copilotEfforts) {
				return CopilotInvocation{}, fmt.Errorf("copilot has no %q effort level", effort)
			}
			args = append(args, "--effort", effort)
		}
	}
	if handle = strings.TrimSpace(handle); handle != "" {
		args = append(args, "--resume", handle)
	}
	if options.BypassPermissions && mode != "chat" {
		args = append(args, "--allow-all-tools")
	}
	for _, directory := range options.AdditionalDirs {
		args = append(args, "--add-dir", directory)
	}
	return CopilotInvocation{Args: args, ProcessOptions: shell.ProcessOptions{Dir: options.Workspace}}, nil
}

// CopilotBackend runs one Copilot CLI process per turn and resumes the
// session the previous turn's `result` named, so the vendor keeps the
// conversation the way it keeps the login.
type CopilotBackend struct {
	Model     string
	Mode      string
	Effort    string
	session   string
	execution ExecutionOptions
	run       lineRunner
	mu        sync.Mutex
}

// NewCopilotBackendWithOptions validates the envelope once, as the other
// handovers do. The effort is projected here so a rung the vendor refuses
// is refused before a process starts.
func NewCopilotBackendWithOptions(model, mode, effort, handle string, options ExecutionOptions) (*CopilotBackend, error) {
	options, err := normalizeExecutionOptions(options)
	if err != nil {
		return nil, err
	}
	if options.Provider == "" {
		options.Provider = "copilot"
	}
	if err := validateExecutionOptions(options); err != nil {
		return nil, err
	}
	model = strings.TrimSpace(model)
	effort = copilotProviderEffort(effort)
	if model != "" && model != "auto" && !effortAllowed(effort, options.Efforts, copilotEfforts) {
		return nil, fmt.Errorf("copilot has no %q effort level", effort)
	}
	return &CopilotBackend{Model: model, Mode: strings.ToLower(strings.TrimSpace(mode)), Effort: effort,
		session: strings.TrimSpace(handle), execution: options}, nil
}

// ProviderHandle is the session id the vendor's terminal event named, empty
// before the first answered turn.
func (b *CopilotBackend) ProviderHandle() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.session
}

func (b *CopilotBackend) Close() error { return nil }

func (b *CopilotBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return b.StreamChatObserved(ctx, model, messages, tools, onToken, nil)
}

// ErrCopilotToolsDenied is the turn's error when the non-interactive CLI
// could not ask and denied its tools while the model answered as if it had
// done the work — observed live, and not a success.
var ErrCopilotToolsDenied = fmt.Errorf("copilot denied its tools: a non-interactive copilot cannot ask for permission, so nothing it tried to do happened; run kolk with -P full-auto for copilot to act, or use chat mode")

// StreamChatObserved is StreamChat with optional typed provider boundaries.
func (b *CopilotBackend) StreamChatObserved(ctx context.Context, model string, messages []provider.Message, _ []provider.Tool, onToken func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	prompt, err := promptFromMessages(messages)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	b.mu.Lock()
	invocation, err := BuildCopilotInvocationWithOptions(model, b.Mode, b.Effort, b.session, prompt, b.execution)
	run := b.run
	b.mu.Unlock()
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	if run == nil {
		run = func(ctx context.Context, executable string, args []string, stdin io.Reader, onLine func([]byte) error) error {
			return shell.RunLinesWithOptions(ctx, executable, args, stdin, onLine, invocation.ProcessOptions)
		}
	}
	start := time.Now()
	events := make([]Event, 0, 16)
	progressPending := make(map[string]string)
	err = run(ctx, CopilotBinary, invocation.Args, strings.NewReader(""), func(line []byte) error {
		translated, err := TranslateCopilot(line)
		if err != nil {
			return err
		}
		for _, event := range translated {
			events = append(events, event)
			if event.SessionID != "" {
				b.mu.Lock()
				b.session = event.SessionID
				b.mu.Unlock()
			}
			observeProviderEvent(observe, event, progressPending)
			if onToken != nil && event.Kind == EventMessageDelta {
				onToken(event.Text)
			}
		}
		return nil
	})
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, err
	}
	if CopilotToolsDenied(events) {
		return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, ErrCopilotToolsDenied
	}
	message, meta, err := Collect(events, time.Since(start))
	if meta.Model == "" {
		meta.Model = model
	}
	return message, meta, err
}

// CopilotKnowsModel reports whether a name is one the Copilot CLI accepts as
// a model: `auto`, its own routing word, which a plan's answered turn then
// verifies with the model the vendor actually chose as the exact id. Named
// models are the vendor's to list per plan; the Free plan refuses them
// (observed 2026-09-06), so kolk does not claim them.
func CopilotKnowsModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), "auto")
}
