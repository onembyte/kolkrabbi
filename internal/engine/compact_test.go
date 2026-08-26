package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func bulkyTurn(user, output string) []provider.Message {
	return []provider.Message{
		{Role: "user", Content: user},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{
			ID: "call-" + user, Type: "function",
			Function: provider.FunctionCall{
				Name:      "bash",
				Arguments: `{"command":"` + strings.Repeat("go test ./... && ", 250) + `true"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "call-" + user, Content: output},
		{Role: "assistant", Content: "done with " + user},
	}
}

func longSession() []provider.Message {
	messages := []provider.Message{{Role: "system", Content: "you are kolk"}}
	for _, name := range []string{"one", "two", "three", "four"} {
		messages = append(messages, bulkyTurn(name, strings.Repeat("output ", 2000))...)
	}
	return messages
}

// Every stage has to leave a conversation a provider will accept. A tool result
// without its call, or a call without its result, is a request that fails
// validation before the model ever sees it.
func assertWellFormed(t *testing.T, messages []provider.Message) {
	t.Helper()
	pending := map[string]bool{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			pending[call.ID] = true
		}
		if message.Role == "tool" {
			if !pending[message.ToolCallID] {
				t.Fatalf("tool result %q has no matching call", message.ToolCallID)
			}
			delete(pending, message.ToolCallID)
		}
	}
	if len(pending) != 0 {
		t.Fatalf("tool calls left without results: %v", pending)
	}
}

func TestCompactDropsOldToolOutputFirst(t *testing.T) {
	messages := longSession()
	result, err := CompactMessages(messages, 2, 12_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertWellFormed(t, result.Messages)

	if result.Stage != StageToolResults {
		t.Fatalf("stage = %q, want the cheapest sacrifice first", result.Stage)
	}
	// The oldest turn's output is gone, and says so rather than vanishing.
	if !strings.Contains(result.Messages[3].Content, "tool output dropped") {
		t.Fatalf("oldest tool result = %q", result.Messages[3].Content)
	}
	if strings.Contains(result.Messages[3].Content, "output output") {
		t.Fatal("the oldest tool output survived")
	}
}

func TestCompactKeepsTheRecentTurnsVerbatim(t *testing.T) {
	messages := longSession()
	result, err := CompactMessages(messages, 2, 12_000, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The last two turns are what the model needs most; they are never touched.
	last := result.Messages[len(result.Messages)-2]
	if !strings.Contains(last.Content, "output output") {
		t.Fatalf("a recent tool result was compacted: %q", last.Content)
	}
}

func TestCompactAlwaysKeepsTheSystemPrompt(t *testing.T) {
	result, err := CompactMessages(longSession(), 1, 10, func([]provider.Message) (string, error) {
		return "summary", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) == 0 || result.Messages[0].Role != "system" {
		t.Fatalf("system prompt lost: %+v", result.Messages)
	}
}

func TestCompactCollapsesOldToolCallsWhenOutputIsNotEnough(t *testing.T) {
	messages := longSession()
	// Reachable only once the arguments go too, not by dropping output alone.
	result, err := CompactMessages(messages, 1, 5_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertWellFormed(t, result.Messages)

	if result.Stage != StageToolCalls {
		t.Fatalf("stage = %q, want tool calls collapsed", result.Stage)
	}
	joined := ""
	for _, message := range result.Messages {
		joined += message.Content + " "
	}
	// What ran is still recorded, so the model knows work happened.
	if !strings.Contains(joined, "bash") {
		t.Fatalf("collapsed history lost what ran: %q", joined)
	}
}

func TestCompactStopsAsSoonAsItFits(t *testing.T) {
	messages := longSession()
	summarised := false
	result, err := CompactMessages(messages, 2, 12_000, func([]provider.Message) (string, error) {
		summarised = true
		return "summary", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Dropping output was enough, so nothing more is sacrificed and no model
	// call is made.
	if summarised {
		t.Fatal("a summary was generated when dropping tool output already fit")
	}
	if result.Stage != StageToolResults {
		t.Fatalf("stage = %q", result.Stage)
	}
}

func TestCompactReportsWhatItDid(t *testing.T) {
	result, err := CompactMessages(longSession(), 2, 12_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replaced == 0 || result.FreedTokens <= 0 {
		t.Fatalf("result = %+v, want what was given up to be countable", result)
	}
}

func TestCompactLeavesAShortSessionAlone(t *testing.T) {
	messages := []provider.Message{
		{Role: "system", Content: "you are kolk"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	result, err := CompactMessages(messages, 2, 100_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stage != StageNone || result.Replaced != 0 {
		t.Fatalf("result = %+v, want an untouched session", result)
	}
	if len(result.Messages) != len(messages) {
		t.Fatalf("messages changed: %+v", result.Messages)
	}
}

func TestCompactFallsBackToASummaryAndKeepsItValid(t *testing.T) {
	messages := longSession()
	asked := 0
	result, err := CompactMessages(messages, 1, 100, func(span []provider.Message) (string, error) {
		asked++
		if len(span) == 0 {
			t.Fatal("the summarizer was handed nothing to summarise")
		}
		return "built the thing, edited two files, tests pass", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWellFormed(t, result.Messages)

	if result.Stage != StageSummary || asked != 1 {
		t.Fatalf("stage = %q after %d summaries", result.Stage, asked)
	}
	if result.Messages[0].Role != "system" {
		t.Fatalf("system prompt lost: %+v", result.Messages[0])
	}
	if !strings.Contains(result.Messages[1].Content, "summarised") {
		t.Fatalf("the summary is not labelled as one: %q", result.Messages[1].Content)
	}
	if !strings.Contains(result.Messages[1].Content, "tests pass") {
		t.Fatalf("the summary content was lost: %q", result.Messages[1].Content)
	}
}

func TestCompactSurfacesASummarizerFailure(t *testing.T) {
	_, err := CompactMessages(longSession(), 1, 100, func([]provider.Message) (string, error) {
		return "", errors.New("provider refused")
	})
	if err == nil || !strings.Contains(err.Error(), "provider refused") {
		t.Fatalf("error = %v, want the cause preserved", err)
	}
}

type stubSummaryBackend struct{ calls int }

func (b *stubSummaryBackend) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	return provider.Message{Role: "assistant", Content: "goal: ship it. edited two files."}, provider.Meta{}, nil
}

func compactionAgent(t *testing.T, window, lastPromptTokens int) (*Agent, *enginetest.FakeSession, *strings.Builder) {
	t.Helper()
	session := enginetest.NewFakeSession("s1", "vendor/model")
	session.SetMessages(longSession())
	var out strings.Builder
	agent := &Agent{Options: Options{
		Out: &out, Mode: ModeCode, Sess: session,
		ContextWindow: window, Backend: &stubSummaryBackend{},
	}}
	agent.lastPromptTokens = lastPromptTokens
	return agent, session, &out
}

func TestCompactIfNeededLeavesAWindowWithRoomAlone(t *testing.T) {
	agent, session, out := compactionAgent(t, 128_000, 1_000)
	before := len(session.GetMessages())

	agent.compactIfNeeded(context.Background())

	if len(session.GetMessages()) != before {
		t.Fatal("a session well under the threshold was compacted")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want silence", out.String())
	}
}

func TestCompactIfNeededShrinksAndSaysSo(t *testing.T) {
	agent, session, out := compactionAgent(t, 20_000, 19_000)

	agent.compactIfNeeded(context.Background())

	if estimateTokens(session.GetMessages()) >= estimateTokens(longSession()) {
		t.Fatal("the session did not shrink")
	}
	got := out.String()
	if !strings.Contains(got, "compacted") || !strings.Contains(got, "freeing about") {
		t.Fatalf("output = %q, want a visible statement of what was given up", got)
	}
	assertWellFormed(t, session.GetMessages())
}

func TestCompactIfNeededIsReversible(t *testing.T) {
	agent, session, _ := compactionAgent(t, 20_000, 19_000)
	before := session.GetMessages()

	agent.compactIfNeeded(context.Background())
	if len(session.GetMessages()) == len(before) &&
		estimateTokens(session.GetMessages()) == estimateTokens(before) {
		t.Fatal("nothing was compacted, so reversal proves nothing")
	}

	if !agent.RestoreCompaction() {
		t.Fatal("a compaction that happened must be undoable")
	}
	if estimateTokens(session.GetMessages()) != estimateTokens(before) {
		t.Fatal("restoring did not put the conversation back")
	}
	// Only the most recent compaction is held, so a second undo is a no-op
	// rather than a lie.
	if agent.RestoreCompaction() {
		t.Fatal("there was nothing left to restore")
	}
}

func TestCompactIfNeededNeverRunsWithoutAKnownWindow(t *testing.T) {
	agent, session, out := compactionAgent(t, 0, 500_000)
	before := estimateTokens(session.GetMessages())

	agent.compactIfNeeded(context.Background())

	if estimateTokens(session.GetMessages()) != before {
		t.Fatal("an unknown window compacted anyway")
	}
	if out.String() != "" {
		t.Fatalf("output = %q", out.String())
	}
}
