package agentcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type fakeLineProcess struct {
	sent  []byte
	lines [][]byte
}

func (p *fakeLineProcess) Send(line []byte) error {
	p.sent = append([]byte(nil), line...)
	return nil
}
func (p *fakeLineProcess) Next(context.Context) ([]byte, error) {
	if len(p.lines) == 0 {
		return nil, io.EOF
	}
	line := p.lines[0]
	p.lines = p.lines[1:]
	return line, nil
}
func (p *fakeLineProcess) Close() error { return nil }

func TestClaudeSessionReusesProcessAndStreamsTurn(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"hello"}]}}`),
		[]byte(`{"type":"result","result":"hello","subtype":"success"}`),
	}}
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var tokens string
	message, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, "opus", func(token string) {
		tokens += token
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "hello" || tokens != "hello" || len(process.sent) == 0 {
		t.Fatalf("message=%+v tokens=%q sent=%q", message, tokens, process.sent)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

// The vendor runs its own tool loop. kolk neither executed nor approved the
// tool, so the trail is a record of what happened — streamed live, and counted
// in the turn's meta — while the final message keeps only the answer text.
func TestClaudeSessionStreamsTheVendorToolLoopAsATrail(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"README.md"}}]}}`),
		[]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"README.md: 12 lines"}]}}`),
		[]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"the readme has 12 lines"}]}}`),
		[]byte(`{"type":"result","result":"the readme has 12 lines","subtype":"success"}`),
	}}
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var tokens string
	message, meta, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "how long is the readme?"}}, "opus", func(token string) {
		tokens += token
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "the readme has 12 lines" {
		t.Fatalf("final message = %+v, want only the answer text", message)
	}
	if meta.ToolCalls != 1 {
		t.Fatalf("tool calls = %d, want the one run the vendor executed", meta.ToolCalls)
	}
	if !strings.Contains(tokens, "· Read") || !strings.Contains(tokens, "→ ok README.md: 12 lines") {
		t.Fatalf("tool trail missing from the stream: %q", tokens)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

// A failing tool is still part of the record: the trail marks it as an error
// and quotes the vendor's complaint.
func TestClaudeSessionTrailMarksAFailedToolRun(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"npm test"}}]}}`),
		[]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"2 tests failed","is_error":true}]}}`),
		[]byte(`{"type":"result","result":"two tests failed","subtype":"success"}`),
	}}
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var tokens string
	if _, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "run the tests"}}, "opus", func(token string) {
		tokens += token
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tokens, "→ error: 2 tests failed") {
		t.Fatalf("trail = %q, want the failure marked", tokens)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

// A warning surfaces while the turn can still make something of it; a
// rejection turns the failure that follows into "when can I go again".
func TestClaudeSessionWarnsThenClassifiesThePlanLimit(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","rateLimitType":"seven_day","utilization":0.78,"resetsAt":1788220800}}`),
		[]byte(`{"type":"result","result":"fine","subtype":"success"}`),
		[]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"seven_day","resetsAt":1788220800}}`),
		[]byte(`{"type":"result","subtype":"error_during_execution","errors":["credit balance too low"],"is_error":true}`),
	}}
	session, ctxCancel := func() (*ClaudeSession, func()) {
		ctx, cancel := context.WithCancel(context.Background())
		s, err := newClaudeSession(ctx, "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
			return process, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return s, cancel
	}()
	var tokens string
	if _, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, "opus", func(token string) {
		tokens += token
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tokens, "78% of the seven-day window used") ||
		!strings.Contains(tokens, "resets 2026-09-01 00:00 UTC") {
		t.Fatalf("warning missing from the stream: %q", tokens)
	}
	_, _, turnErr := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "again"}}, "opus", nil)
	if turnErr == nil {
		t.Fatal("a rejected plan limit must fail the turn")
	}
	if !strings.Contains(turnErr.Error(), "seven-day window is fully used") ||
		!strings.Contains(turnErr.Error(), "resets 2026-09-01 00:00 UTC") {
		t.Fatalf("error = %v, want the window and the reset time", turnErr)
	}
	// The vendor's own words stay as a parenthetical — they may carry detail
	// kolk does not know — but never as the headline.
	if !strings.HasPrefix(turnErr.Error(), "claude plan limit reached") {
		t.Fatalf("error = %v, want the plan limit speaking first", turnErr)
	}
	ctxCancel()
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

// stallingLineProcess waits for cancellation at one chosen point in the
// stream, which is what a real provider looks like when the user interrupts a
// turn: the frames it had already queued are still in the pipe afterwards.
type stallingLineProcess struct {
	lines   [][]byte
	index   int
	stallAt int
	closed  bool
}

func (p *stallingLineProcess) Send([]byte) error { return nil }

func (p *stallingLineProcess) Next(ctx context.Context) ([]byte, error) {
	if p.index == p.stallAt {
		p.stallAt = -1
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.index >= len(p.lines) {
		return nil, io.EOF
	}
	line := p.lines[p.index]
	p.index++
	return line, nil
}

func (p *stallingLineProcess) Close() error {
	p.closed = true
	return nil
}

func claudeTurnFrames(text string) [][]byte {
	return [][]byte{
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"` + text + `"}]}}`),
		[]byte(`{"type":"result","result":"` + text + `","subtype":"success"}`),
	}
}

func TestClaudeSessionDoesNotServeAnInterruptedTurnToTheNextOne(t *testing.T) {
	process := &stallingLineProcess{
		lines:   append(claudeTurnFrames("one"), claudeTurnFrames("two")...),
		stallAt: 1,
	}
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	interrupted, cancel := context.WithCancel(context.Background())
	go cancel()
	if _, _, err := session.Turn(interrupted, []provider.Message{{Role: "user", Content: "first"}}, "opus", nil); err == nil {
		t.Fatal("an interrupted turn must report the interruption")
	}

	message, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "second"}}, "opus", nil)
	if err != nil {
		t.Fatalf("the turn after an interrupt failed: %v", err)
	}
	if message.Content != "two" {
		t.Fatalf("second turn answered %q — the interrupted turn's frames leaked into it", message.Content)
	}
}

func TestClaudeSessionReportsItselfUnusableWhenItCannotResynchronize(t *testing.T) {
	// Only the interrupted turn's opening frame is ever available, so the
	// completion frame that would resynchronize the stream never arrives.
	process := &stallingLineProcess{lines: claudeTurnFrames("one")[:1], stallAt: 1}
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	interrupted, cancel := context.WithCancel(context.Background())
	go cancel()
	if _, _, err := session.Turn(interrupted, []provider.Message{{Role: "user", Content: "first"}}, "opus", nil); err == nil {
		t.Fatal("an interrupted turn must report the interruption")
	}

	if !session.Unusable() {
		t.Fatal("a session that cannot resynchronize must declare itself unusable")
	}
	_, _, err = session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "second"}}, "opus", nil)
	if err == nil {
		t.Fatal("an unusable session must refuse the next turn instead of answering from a desynchronized stream")
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("refusal = %v, want it to name the interrupted turn", err)
	}
}

func TestClaudeSessionExplainsAProviderThatExitsMidTurn(t *testing.T) {
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return &fakeLineProcess{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, "opus", nil)
	if err == nil {
		t.Fatal("a provider that exits mid-turn must be reported")
	}
	// "EOF" on its own tells the user nothing they can act on.
	if !strings.Contains(err.Error(), "claude exited before finishing") {
		t.Fatalf("error = %v, want an explanation of what happened", err)
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("error = %v, want the command the user can run to check", err)
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("error = %v, want the underlying cause preserved", err)
	}
}

// A persistent --input-format stream-json process reports usage and cost as
// running totals for the whole session. Recording them verbatim makes every
// turn contain all the turns before it, so a cost chart grows quadratically.
func TestClaudeSessionReportsPerTurnUsageFromCumulativeTotals(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"one"}],"usage":{"input_tokens":100,"output_tokens":10}}}`),
		[]byte(`{"type":"result","result":"one","subtype":"success","total_cost_usd":0.10,"usage":{"input_tokens":100,"output_tokens":10}}`),
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"two"}],"usage":{"input_tokens":150,"output_tokens":15}}}`),
		[]byte(`{"type":"result","result":"two","subtype":"success","total_cost_usd":0.30,"usage":{"input_tokens":250,"output_tokens":25}}`),
	}}
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	_, first, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "one"}}, "opus", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.PromptTokens != 100 || first.CompletionTokens != 10 || math.Abs(first.Cost-0.10) > 1e-9 {
		t.Fatalf("first turn = %d/%d/%v", first.PromptTokens, first.CompletionTokens, first.Cost)
	}

	_, second, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "two"}}, "opus", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.PromptTokens != 150 {
		t.Fatalf("second turn prompt tokens = %d, want 150 (250 total minus the first turn)", second.PromptTokens)
	}
	if second.CompletionTokens != 15 {
		t.Fatalf("second turn completion tokens = %d, want 15", second.CompletionTokens)
	}
	if math.Abs(second.Cost-0.20) > 1e-9 {
		t.Fatalf("second turn cost = %v, want 0.20 (0.30 total minus the first turn)", second.Cost)
	}
}

func TestClaudeSessionRebasesWhenTheProviderResetsItsTotals(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"result","result":"one","subtype":"success","total_cost_usd":0.50,"usage":{"input_tokens":500,"output_tokens":50}}`),
		[]byte(`{"type":"result","result":"two","subtype":"success","total_cost_usd":0.05,"usage":{"input_tokens":40,"output_tokens":4}}`),
	}}
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "one"}}, "opus", nil); err != nil {
		t.Fatal(err)
	}
	_, second, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "two"}}, "opus", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Smaller than the running total means the provider restarted its own
	// accounting. Never report a negative charge.
	if second.PromptTokens != 40 || math.Abs(second.Cost-0.05) > 1e-9 {
		t.Fatalf("rebased turn = %d tokens / %v cost, want the reported values", second.PromptTokens, second.Cost)
	}
}

func TestClaudeSessionDiffsCacheTokensToo(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"result","result":"one","subtype":"success","usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":1000,"cache_creation_input_tokens":200}}`),
		[]byte(`{"type":"result","result":"two","subtype":"success","usage":{"input_tokens":150,"output_tokens":15,"cache_read_input_tokens":2500,"cache_creation_input_tokens":200}}`),
	}}
	session, err := newClaudeSession(context.Background(), "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "one"}}, "opus", nil); err != nil {
		t.Fatal(err)
	}
	_, second, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "two"}}, "opus", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.CacheReadTokens != 1500 {
		t.Fatalf("cache read = %d, want 1500 (2500 total minus the first turn)", second.CacheReadTokens)
	}
	if second.CacheCreationTokens != 0 {
		t.Fatalf("cache creation = %d, want 0 (the total did not move)", second.CacheCreationTokens)
	}
}

// The vendor's cause must survive being wrapped on the way to classification.
// `Collect` returns *providerError bare today, so a type assertion happens to
// work — but one `fmt.Errorf("…: %w", err)` anywhere on that path would drop
// the cause silently, and the limit message would still read as complete with
// the reason missing from it. That is the specific way this fails while still
// looking like it works, so it is asserted rather than left to the next edit.
func TestClaudeSessionClassifiesAWrappedProviderCause(t *testing.T) {
	session := &ClaudeSession{rejectedLimit: Event{
		Kind:        EventLimit,
		LimitWindow: "seven_day",
	}}
	wrapped := fmt.Errorf("stream ended: %w", &providerError{message: "credit balance too low"})

	err := session.classifyLimitFailure(wrapped)

	if err == nil {
		t.Fatal("a rejected limit must classify into an error")
	}
	if !strings.Contains(err.Error(), "credit balance too low") {
		t.Fatalf("err = %v, want the wrapped vendor cause preserved", err)
	}
	if !strings.Contains(err.Error(), "seven-day") {
		t.Fatalf("err = %v, want the limit window named", err)
	}
}
