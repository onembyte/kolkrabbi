package agentcli

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

type lineProcess interface {
	Send([]byte) error
	Next(context.Context) ([]byte, error)
	Close() error
}

type startLineProcess func(context.Context, string, []string) (lineProcess, error)

// ClaudeSession owns one persistent provider CLI process for one Kolkrabbi
// session. Turns are serialized because the provider stream is ordered.
type ClaudeSession struct {
	process lineProcess
	mu      sync.Mutex
	effort  string
	closed  bool
}

// NewClaudeSession starts one provider-owned Claude process.
func NewClaudeSession(ctx context.Context, effort string) (*ClaudeSession, error) {
	return newClaudeSession(ctx, effort, func(ctx context.Context, executable string, args []string) (lineProcess, error) {
		return shell.StartLinesProcess(ctx, executable, args)
	})
}

func newClaudeSession(ctx context.Context, effort string, start startLineProcess) (*ClaudeSession, error) {
	process, err := start(ctx, "claude", BuildClaudeSessionArgs(effort))
	if err != nil {
		return nil, err
	}
	return &ClaudeSession{process: process, effort: effort}, nil
}

// BuildClaudeSessionArgs returns the persistent stream-json command line.
func BuildClaudeSessionArgs(effort string) []string {
	args := []string{"-p", "--verbose", "--output-format", "stream-json", "--input-format", "stream-json", "--safe-mode", "--setting-sources", ""}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return args
}

type claudeInput struct {
	Type    string           `json:"type"`
	Message provider.Message `json:"message"`
}

// Turn sends one request through the existing process and waits for its result.
func (s *ClaudeSession) Turn(ctx context.Context, messages []provider.Message, model string, onToken func(string)) (provider.Message, provider.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return provider.Message{}, provider.Meta{}, fmt.Errorf("claude session is closed")
	}
	prompt, err := promptFromMessages(messages)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	request, err := json.Marshal(claudeInput{Type: "user", Message: provider.Message{Role: "user", Content: prompt}})
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	if err := s.process.Send(request); err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	start := time.Now()
	events := make([]Event, 0, 8)
	for {
		line, err := s.process.Next(ctx)
		if err != nil {
			return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, err
		}
		translated, err := Translate(line)
		if err != nil {
			return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, err
		}
		for _, event := range translated {
			events = append(events, event)
			if event.Kind == EventMessageDelta && onToken != nil {
				onToken(event.Text)
			}
			if event.Kind == EventMessageCompleted {
				message, meta, collectErr := Collect(events, time.Since(start))
				if meta.Model == "" {
					meta.Model = model
				}
				return message, meta, collectErr
			}
		}
	}
}

func (s *ClaudeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.process.Close()
}
