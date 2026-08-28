package agentcli

import (
	"context"
	"io"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestPromptFromMessagesPreservesConversationRoles(t *testing.T) {
	got, err := promptFromMessages([]provider.Message{
		{Role: "system", Content: "be concise"},
		{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "SYSTEM:\nbe concise\n\nUSER:\nhello"
	if got != want {
		t.Fatalf("prompt = %q, want %q", got, want)
	}
}

// The gateway seam's tool schemas are deliberately ignored on this backend:
// the vendor owns tool execution, and its argv takes names, not JSON Schema.
// They must neither error the turn nor reach the process.
func TestClaudeBackendIgnoresEngineToolSchemas(t *testing.T) {
	backend := ClaudeBackend{
		start: func(context.Context, string, []string) (lineProcess, error) {
			return &fakeLineProcess{lines: claudeTurnFrames("hello")}, nil
		},
	}
	message, _, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "hi"}}, []provider.Tool{{Function: provider.FunctionDef{Name: "bash"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "hello" {
		t.Fatalf("message = %q", message.Content)
	}
}

func TestClaudeBackendStreamsTextThroughEngineCallback(t *testing.T) {
	backend := ClaudeBackend{
		run: func(_ context.Context, executable string, _ []string, stdin io.Reader, onLine func([]byte) error) error {
			if executable != "claude" {
				t.Fatalf("executable = %q", executable)
			}
			_, _ = io.ReadAll(stdin)
			return onLine([]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"hello"}]}}`))
		},
	}
	var tokens string
	message, meta, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, func(token string) {
		tokens += token
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "hello" || tokens != "hello" || meta.Model != "opus" {
		t.Fatalf("message=%+v tokens=%q meta=%+v", message, tokens, meta)
	}
}

func TestClaudeBackendReusesPersistentSession(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"one"}]}}`),
		[]byte(`{"type":"result","result":"one","subtype":"success"}`),
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"two"}]}}`),
		[]byte(`{"type":"result","result":"two","subtype":"success"}`),
	}}
	starts := 0
	backend := ClaudeBackend{
		Effort: "high",
		start: func(context.Context, string, []string) (lineProcess, error) {
			starts++
			return process, nil
		},
	}
	for _, want := range []string{"one", "two"} {
		message, _, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: want}}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if message.Content != want {
			t.Fatalf("message = %q, want %q", message.Content, want)
		}
	}
	if starts != 1 {
		t.Fatalf("session starts = %d, want one process", starts)
	}
}

// A persistent provider process must belong to the Kolkrabbi session, not to
// whichever turn happened to start it. Binding it to a turn context means the
// first Ctrl+C kills Claude for the rest of the session.
func TestClaudeBackendSessionOutlivesOneTurnContext(t *testing.T) {
	turn := func(text string) [][]byte {
		return [][]byte{
			[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"` + text + `"}]}}`),
			[]byte(`{"type":"result","result":"` + text + `","subtype":"success"}`),
		}
	}
	process := &fakeLineProcess{lines: append(turn("one"), turn("two")...)}
	starts := 0
	var processContext context.Context
	backend := &ClaudeBackend{start: func(ctx context.Context, _ string, _ []string) (lineProcess, error) {
		starts++
		processContext = ctx
		return process, nil
	}}

	first, cancelFirst := context.WithCancel(context.Background())
	if _, _, err := backend.StreamChat(first, "opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	cancelFirst()

	if processContext.Err() != nil {
		t.Fatalf("the provider process died with its first turn: %v", processContext.Err())
	}
	if _, _, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "again"}}, nil, nil); err != nil {
		t.Fatalf("second turn after the first was cancelled: %v", err)
	}
	if starts != 1 {
		t.Fatalf("started %d provider processes, want exactly one per session", starts)
	}

	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if processContext.Err() == nil {
		t.Fatal("closing the backend must also release the process context")
	}
}

// An interrupt the session cannot recover from must not end Claude for the rest
// of the Kolkrabbi session: the backend replaces the process and the next turn
// works normally.
func TestClaudeBackendReplacesAnUnusableSession(t *testing.T) {
	starts := 0
	backend := &ClaudeBackend{start: func(context.Context, string, []string) (lineProcess, error) {
		starts++
		if starts == 1 {
			return &stallingLineProcess{lines: claudeTurnFrames("one")[:1], stallAt: 1}, nil
		}
		return &fakeLineProcess{lines: claudeTurnFrames("two")}, nil
	}}

	interrupted, cancel := context.WithCancel(context.Background())
	go cancel()
	if _, _, err := backend.StreamChat(interrupted, "opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil); err == nil {
		t.Fatal("an interrupted turn must report the interruption")
	}

	message, _, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "again"}}, nil, nil)
	if err != nil {
		t.Fatalf("the turn after an unrecoverable interrupt failed: %v", err)
	}
	if message.Content != "two" {
		t.Fatalf("message = %q, want the new session's answer", message.Content)
	}
	if starts != 2 {
		t.Fatalf("started %d processes, want the unusable one replaced exactly once", starts)
	}
}
