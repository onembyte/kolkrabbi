package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// fakeCheckpointer stands in for the file half of an undo.
type fakeCheckpointer struct {
	restored []string
	err      error
	calls    int
}

func (f *fakeCheckpointer) BeginTurn(context.Context)      {}
func (f *fakeCheckpointer) Record(tool, path string) error { return nil }
func (f *fakeCheckpointer) RewindLastTurn() ([]string, error) {
	f.calls++
	return f.restored, f.err
}

func undoFixture(t *testing.T, ckpt Checkpointer, messages ...provider.Message) *Agent {
	t.Helper()
	sess := enginetest.NewFakeSession("s", "mock/model")
	for _, m := range messages {
		sess.AppendMessage(m)
	}
	return &Agent{Options: Options{Sess: sess, Ckpt: ckpt}}
}

func conversation() []provider.Message {
	return []provider.Message{
		{Role: "system", Content: "you are kolk"},
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "second question"},
		{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "t1"}}},
		{Role: "tool", ToolCallID: "t1", Content: "edited a.go"},
		{Role: "assistant", Content: "second answer"},
	}
}

func TestUndoTakesBackBothTheFilesAndTheConversation(t *testing.T) {
	ckpt := &fakeCheckpointer{restored: []string{"a.go"}}
	agent := undoFixture(t, ckpt, conversation()...)

	result, err := agent.Undo()
	if err != nil {
		t.Fatalf("undo: %v", err)
	}

	if len(result.Files) != 1 || result.Files[0] != "a.go" {
		t.Fatalf("files = %v", result.Files)
	}
	// Restoring the tree while the model still believes it made those edits is
	// a divergence the user cannot see and the model cannot detect.
	left := agent.Sess.GetMessages()
	if len(left) != 3 {
		t.Fatalf("left %d messages, want the system prompt and the first exchange:\n%+v", len(left), left)
	}
	if left[len(left)-1].Content != "first answer" {
		t.Fatalf("trimmed to the wrong point: %+v", left)
	}
	if result.Messages != 4 {
		t.Fatalf("reported %d messages dropped, want 4", result.Messages)
	}
}

func TestAFailedFileRestoreLeavesTheConversationAlone(t *testing.T) {
	ckpt := &fakeCheckpointer{err: errors.New("missing backup for a.go")}
	agent := undoFixture(t, ckpt, conversation()...)

	if _, err := agent.Undo(); err == nil {
		t.Fatal("a failed restore reported success")
	}

	// A half-undo that rewinds history and leaves the edits is the same
	// divergence in the opposite direction.
	if got := len(agent.Sess.GetMessages()); got != 7 {
		t.Fatalf("conversation was trimmed anyway: %d messages left", got)
	}
}

func TestATurnThatChangedNoFilesIsStillATurn(t *testing.T) {
	ckpt := &fakeCheckpointer{}
	agent := undoFixture(t, ckpt,
		provider.Message{Role: "system", Content: "s"},
		provider.Message{Role: "user", Content: "what is go"},
		provider.Message{Role: "assistant", Content: "a language"},
	)

	result, err := agent.Undo()
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("files = %v", result.Files)
	}
	if got := len(agent.Sess.GetMessages()); got != 1 {
		t.Fatalf("left %d messages, want the system prompt", got)
	}
}

func TestUndoingWithNothingToUndoSaysSo(t *testing.T) {
	agent := undoFixture(t, &fakeCheckpointer{}, provider.Message{Role: "system", Content: "s"})

	result, err := agent.Undo()
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if result.Messages != 0 || len(result.Files) != 0 {
		t.Fatalf("result = %+v, want nothing undone", result)
	}
}

func TestUndoNeedsCheckpointing(t *testing.T) {
	agent := undoFixture(t, nil, conversation()...)

	if _, err := agent.Undo(); err == nil {
		t.Fatal("undo ran without a checkpoint store")
	}
	// Without the file half there is no undo, only a history edit that would
	// leave the tree ahead of the conversation.
	if got := len(agent.Sess.GetMessages()); got != 7 {
		t.Fatalf("conversation was trimmed anyway: %d", got)
	}
}

func TestUndoOnlyTakesBackOneTurn(t *testing.T) {
	ckpt := &fakeCheckpointer{}
	agent := undoFixture(t, ckpt, conversation()...)

	if _, err := agent.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Undo(); err != nil {
		t.Fatal(err)
	}

	// Repeated single undos are easier to reason about than a count nobody
	// can picture, and the store is asked once per undo either way.
	if ckpt.calls != 2 {
		t.Fatalf("checkpoint store called %d times", ckpt.calls)
	}
	if got := len(agent.Sess.GetMessages()); got != 1 {
		t.Fatalf("left %d messages, want the system prompt", got)
	}
}
