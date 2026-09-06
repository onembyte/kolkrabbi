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

// CopilotInvocation is one run of the GitHub Copilot CLI on its documented
// programmatic contract (docs read 2026-09-05, V34.4c.2): `-p PROMPT` runs the
// prompt and exits, `-s` prints only the agent's response, `--model` picks
// the model, `--allow-all-tools` lets every tool run, `--add-dir` widens the
// allowed paths. Nothing undocumented is sent. The prompt travels on the
// command line because that is the contract the vendor documents; stdin is
// left empty.
type CopilotInvocation struct {
	Args           []string
	ProcessOptions shell.ProcessOptions
}

// CopilotBinary is the executable the vendor installs (`npm install -g
// @github/copilot`, `brew install --cask copilot-cli`).
const CopilotBinary = "copilot"

// BuildCopilotInvocationWithOptions shapes the argv. Every tool is allowed
// only under kolk's full-auto in a tool mode; without it the CLI keeps its
// own permission gate, and chat mode grants nothing at all. An empty model
// leaves the vendor's default in place.
func BuildCopilotInvocationWithOptions(model, mode, prompt string, options ExecutionOptions) (CopilotInvocation, error) {
	options, err := normalizeExecutionOptions(options)
	if err != nil {
		return CopilotInvocation{}, err
	}
	if strings.TrimSpace(prompt) == "" {
		return CopilotInvocation{}, fmt.Errorf("copilot prompt cannot be empty")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	args := []string{"-p", prompt, "-s"}
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "--model", model)
	}
	if options.BypassPermissions && mode != "chat" {
		args = append(args, "--allow-all-tools")
	}
	for _, directory := range options.AdditionalDirs {
		args = append(args, "--add-dir", directory)
	}
	return CopilotInvocation{Args: args, ProcessOptions: shell.ProcessOptions{Dir: options.Workspace}}, nil
}

// CopilotBackend runs one Copilot CLI process per turn on the documented
// contract. There is no session handle to resume and no event stream: `-s`
// prints the agent's response and nothing else, so every printed line is a
// delta of one assistant message. Usage and cost are not reported by the CLI
// in this mode; the reply is billed as the plan's turn.
type CopilotBackend struct {
	Model     string
	Mode      string
	execution ExecutionOptions
	run       lineRunner
	mu        sync.Mutex
}

// NewCopilotBackendWithOptions validates the envelope once, as the other
// handovers do.
func NewCopilotBackendWithOptions(model, mode string, options ExecutionOptions) (*CopilotBackend, error) {
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
	return &CopilotBackend{Model: strings.TrimSpace(model), Mode: strings.ToLower(strings.TrimSpace(mode)), execution: options}, nil
}

// ProviderHandle is empty: Copilot's programmatic mode has no resumable
// session on the pages read, so every turn is a fresh process.
func (b *CopilotBackend) ProviderHandle() string { return "" }

func (b *CopilotBackend) Close() error { return nil }

func (b *CopilotBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return b.StreamChatObserved(ctx, model, messages, tools, onToken, nil)
}

// StreamChatObserved is StreamChat with optional typed provider boundaries.
func (b *CopilotBackend) StreamChatObserved(ctx context.Context, model string, messages []provider.Message, _ []provider.Tool, onToken func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	prompt, err := promptFromMessages(messages)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	b.mu.Lock()
	invocation, err := BuildCopilotInvocationWithOptions(model, b.Mode, prompt, b.execution)
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
	events := make([]Event, 0, 8)
	progressPending := make(map[string]string)
	err = run(ctx, CopilotBinary, invocation.Args, strings.NewReader(""), func(line []byte) error {
		event := Event{Kind: EventMessageDelta, Text: string(line) + "\n"}
		events = append(events, event)
		observeProviderEvent(observe, event, progressPending)
		if onToken != nil {
			onToken(event.Text)
		}
		return nil
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
