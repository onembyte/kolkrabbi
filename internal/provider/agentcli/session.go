package agentcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type lineProcess interface {
	Send([]byte) error
	Next(context.Context) ([]byte, error)
	Close() error
	// HardExit reports that the child was killed rather than allowed to end its
	// own turn, which invalidates the vendor's conversation. See §2.5.
	HardExit() bool
}

type startLineProcess func(context.Context, string, []string) (lineProcess, error)

// ClaudeSession owns one persistent provider CLI process for one Kolkrabbi
// session. Turns are serialized because the provider stream is ordered.
type ClaudeSession struct {
	process  lineProcess
	mu       sync.Mutex
	model    string
	effort   string
	closed   bool
	unusable bool
	// providerID is the vendor's own conversation handle, reported on
	// system/init and on the result frame. It is the --resume handle and the
	// only piece of vendor state worth carrying across process boundaries.
	providerID string
	// resumed records that this process opened the conversation with --resume
	// rather than claiming a --session-id: a resume that produces nothing
	// before dying usually means the vendor no longer keeps the handle, which
	// the caller wants to know before retrying with the same dead one.
	resumed bool
	// The provider reports usage for the whole session, so the running totals
	// already charged are kept to turn each report into one turn's own cost.
	spentCost          float64
	spentInput         int
	spentOutput        int
	spentCacheRead     int
	spentCacheCreation int
	// rejectedLimit holds the last rate_limit_event rejection: the vendor hands
	// over the cause before the failure, and the failure's own prose is
	// usually a bare "credit balance too low". The zero Event carries no Kind,
	// which is how "nothing recorded" reads.
	rejectedLimit Event
}

// ProviderHandle reports the vendor conversation this process has been
// driving, or "" before the vendor has confirmed one.
func (s *ClaudeSession) ProviderHandle() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.providerID
}

// Unusable reports that the provider stream can no longer be trusted, so the
// caller must replace this session rather than send another turn through it.
func (s *ClaudeSession) Unusable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unusable
}

// HardExit reports that the provider process was killed rather than allowed to
// end its own turn. §2.5: the vendor resumes an *unfinished* turn on --resume,
// so a conversation whose process died this way must never be resumed.
func (s *ClaudeSession) HardExit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.process.HardExit()
}

// Resumed reports whether this process opened a conversation the vendor was
// already keeping (--resume) rather than claiming a fresh one.
func (s *ClaudeSession) Resumed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumed
}

// newClaudeSession mints a fresh conversation name and opens the persistent
// process with it. Backends that own a resume flow spawn through getSession
// instead, which can reuse a handle the session file carried.
func newClaudeSession(ctx context.Context, model, mode, effort string, start startLineProcess) (*ClaudeSession, error) {
	handle := NewVendorHandle()
	args, err := BuildClaudeSessionArgs(model, mode, effort, handle, false)
	if err != nil {
		return nil, err
	}
	process, err := start(ctx, "claude", args)
	if err != nil {
		return nil, err
	}
	return &ClaudeSession{process: process, model: model, effort: effort}, nil
}

// BuildClaudeSessionArgs returns the persistent stream-json command line. The
// model is part of the spawn contract, not a per-turn request field: a
// stream-json process replays no argv, so a different model means a different
// process, and the effort dial's own restart rule follows from the same fact.
// A handle either claims a new conversation (--session-id) or resumes a known
// one (--resume).
func BuildClaudeSessionArgs(model, mode, effort, handle string, resume bool) ([]string, error) {
	return claudeArgs(mode, model, effort, handle, resume, true)
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
	// The tool ids this turn has seen, so a tool_result — which names only the
	// id — can be reported under the tool's name.
	pending := make(map[string]string)
	for {
		line, err := s.process.Next(ctx)
		if err != nil {
			return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, s.abandonTurn(ctx, explainEarlyExit(err))
		}
		translated, err := Translate(line)
		if err != nil {
			return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, s.abandonTurn(ctx, err)
		}
		for _, event := range translated {
			// init and result both carry the conversation handle; the result
			// frame is the latest word, and either arriving first still lands.
			if event.SessionID != "" {
				s.providerID = event.SessionID
			}
		}
		// The whole frame is consumed before collecting: a result frame carries
		// its usage *after* the completion event, so returning on sight of the
		// completion threw every turn's cost away.
		completed := false
		for _, event := range translated {
			events = append(events, event)
			switch {
			case event.Kind == EventMessageDelta && onToken != nil:
				onToken(event.Text)
			case event.Kind == EventTool && onToken != nil:
				// The vendor ran this tool already — the trail says what
				// happened, to whom it happened, and that kolk did not do it.
				if line := toolTrail(event, pending); line != "" {
					onToken(line)
				}
			case event.Kind == EventLimit && !event.LimitRejected:
				if onToken != nil {
					onToken(limitTrail(event))
				}
			case event.Kind == EventLimit && event.LimitRejected:
				s.rejectedLimit = event
			}
			if event.Kind == EventMessageCompleted {
				completed = true
			}
		}
		if completed {
			message, meta, collectErr := Collect(events, time.Since(start))
			if collectErr != nil {
				collectErr = s.classifyLimitFailure(collectErr)
			}
			if meta.Model == "" {
				meta.Model = model
			}
			s.chargeTurn(&meta)
			return message, meta, collectErr
		}
	}
}

// toolTrail renders one provider-executed tool event for the stream: the run
// when it starts, and its outcome when the vendor reports back. It is a record,
// never a request — by this point the vendor has already done the work, and
// kolk neither approved nor executed it.
func toolTrail(event Event, pending map[string]string) string {
	switch {
	case event.ToolName != "":
		if event.ToolCallID != "" {
			pending[event.ToolCallID] = event.ToolName
		}
		if detail := oneLine(event.ToolInput, 100); detail != "" {
			return fmt.Sprintf("\n· %s: %s\n", event.ToolName, detail)
		}
		return "\n· " + event.ToolName + "\n"
	case event.ToolCallID != "":
		mark := "→ ok"
		if event.ToolIsError {
			mark = "→ error:"
		}
		return fmt.Sprintf("  %s %s\n", mark, oneLine(event.ToolOutput, 100))
	}
	return ""
}

// limitTrail renders a rate_limit_event warning for the stream: how much of
// the plan's window is gone while a turn can still be spent on it, and when
// the window refills.
func limitTrail(event Event) string {
	var out strings.Builder
	out.WriteString("\n· plan limit: ")
	if event.LimitUtilization > 0 {
		fmt.Fprintf(&out, "%.0f%% of the %s window used", event.LimitUtilization*100, limitWindowName(event.LimitWindow))
	} else {
		out.WriteString("the " + limitWindowName(event.LimitWindow) + " window is nearly used")
	}
	if event.LimitResets > 0 {
		out.WriteString(" · resets " + time.Unix(event.LimitResets, 0).UTC().Format("2006-01-02 15:04 MST"))
	}
	out.WriteString("\n")
	return out.String()
}

// retirementTrail explains a conversation kolk had to retire after the provider
// was killed mid-turn.
//
// It describes what changed rather than what was lost, because nothing was:
// kolk replays its whole transcript on every turn, so the vendor's own copy is
// the only casualty and the user's history is untouched. Saying "history lost"
// here would be both alarming and false.
func retirementTrail() string {
	return "\n· the provider stopped before it finished, so its own record of this" +
		" conversation was discarded and the next turn starts a fresh one." +
		" Your transcript is intact.\n"
}

// limitWindowName names a rate_limit_type the way a person says it.
func limitWindowName(window string) string {
	if window == "" {
		return "plan"
	}
	return strings.ReplaceAll(window, "_", "-")
}

// classifyLimitFailure turns the vendor's terminal failure into what it is
// when a rejection preceded it: the plan window ran out, and the answer to
// "when can I go again" is a timestamp the vendor already gave.
func (s *ClaudeSession) classifyLimitFailure(err error) error {
	if s.rejectedLimit.Kind != EventLimit {
		return err
	}
	cause := ""
	// errors.As, not a type assertion: the vendor's failure reaches here
	// through the run and translate layers, and the moment either wraps it the
	// assertion stops matching and the message silently loses its cause.
	var pe *providerError
	if errors.As(err, &pe) {
		cause = pe.Error()
	}
	message := fmt.Sprintf("claude plan limit reached: the %s window is fully used",
		limitWindowName(s.rejectedLimit.LimitWindow))
	if s.rejectedLimit.LimitResets > 0 {
		message += "; it resets " + time.Unix(s.rejectedLimit.LimitResets, 0).UTC().Format("2006-01-02 15:04 MST")
	}
	if cause != "" {
		message += " (" + cause + ")"
	}
	return fmt.Errorf("%s", message)
}

// oneLine is the first line of s, rune-capped at max.
func oneLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if runes := []rune(s); len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return s
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
