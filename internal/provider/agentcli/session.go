package agentcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	process  lineProcess
	mu       sync.Mutex
	effort   string
	closed   bool
	unusable bool
	// The provider reports usage for the whole session, so the running totals
	// already charged are kept to turn each report into one turn's own cost.
	spentCost          float64
	spentInput         int
	spentOutput        int
	spentCacheRead     int
	spentCacheCreation int
}

// Unusable reports that the provider stream can no longer be trusted, so the
// caller must replace this session rather than send another turn through it.
func (s *ClaudeSession) Unusable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unusable
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

// resyncGrace bounds how long Kolkrabbi waits for the tail of an interrupted
// turn before declaring the stream position unknowable.
const resyncGrace = 5 * time.Second

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
	if s.unusable {
		return provider.Message{}, provider.Meta{Model: model}, fmt.Errorf("claude session cannot be reused after an interrupted turn; start a new session")
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
			return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, s.abandonTurn(ctx, explainEarlyExit(err))
		}
		translated, err := Translate(line)
		if err != nil {
			return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, s.abandonTurn(ctx, err)
		}
		// The whole frame is consumed before collecting: a result frame carries
		// its usage *after* the completion event, so returning on sight of the
		// completion threw every turn's cost away.
		completed := false
		for _, event := range translated {
			events = append(events, event)
			if event.Kind == EventMessageDelta && onToken != nil {
				onToken(event.Text)
			}
			if event.Kind == EventMessageCompleted {
				completed = true
			}
		}
		if completed {
			message, meta, collectErr := Collect(events, time.Since(start))
			if meta.Model == "" {
				meta.Model = model
			}
			s.chargeTurn(&meta)
			return message, meta, collectErr
		}
	}
}

// explainEarlyExit turns a bare end-of-stream into something the user can act
// on. "EOF" alone says nothing about a provider CLI that quit, most often
// because it is not signed in.
func explainEarlyExit(err error) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("claude exited before finishing the turn; run `claude` in a terminal to check that it is signed in: %w", err)
	}
	return err
}

// chargeTurn converts the provider's session-cumulative usage into this turn's
// own usage. A persistent `--input-format stream-json` process reports running
// totals, so recording them verbatim would make every turn contain the turns
// before it and grow a cost chart quadratically.
func (s *ClaudeSession) chargeTurn(meta *provider.Meta) {
	cost, input, output := meta.Cost, meta.PromptTokens, meta.CompletionTokens
	cacheRead, cacheCreation := meta.CacheReadTokens, meta.CacheCreationTokens
	if cost >= s.spentCost && input >= s.spentInput && output >= s.spentOutput &&
		cacheRead >= s.spentCacheRead && cacheCreation >= s.spentCacheCreation {
		meta.Cost = cost - s.spentCost
		meta.PromptTokens = input - s.spentInput
		meta.CompletionTokens = output - s.spentOutput
		meta.CacheReadTokens = cacheRead - s.spentCacheRead
		meta.CacheCreationTokens = cacheCreation - s.spentCacheCreation
	}
	// Anything smaller than the running total means the provider restarted its
	// own accounting. Take the report at face value rather than charging a
	// negative turn, and rebase on it either way.
	s.spentCost, s.spentInput, s.spentOutput = cost, input, output
	s.spentCacheRead, s.spentCacheCreation = cacheRead, cacheCreation
}

// abandonTurn resynchronizes the provider stream after a turn ends early.
// The provider keeps emitting the frames it had already produced for that turn,
// and handing them to the next turn would answer the previous question. The
// caller still holds s.mu, so no other turn can interleave with the drain.
func (s *ClaudeSession) abandonTurn(ctx context.Context, cause error) error {
	// The turn's context is usually already cancelled — that is why the turn is
	// being abandoned — so the drain detaches from its cancellation while
	// keeping its values.
	resync, cancel := context.WithTimeout(context.WithoutCancel(ctx), resyncGrace)
	defer cancel()
	for {
		line, err := s.process.Next(resync)
		if err != nil {
			// The rest of the interrupted turn never arrived, so where the next
			// turn's frames begin is unknowable. Refuse to guess.
			s.unusable = true
			return cause
		}
		events, translateErr := Translate(line)
		if translateErr != nil {
			continue
		}
		for _, event := range events {
			if event.Kind == EventMessageCompleted {
				return cause
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
