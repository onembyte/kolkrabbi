package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// A session resumes at the width of effort and on the connector it was left
// running at: those two lines of state are what "same session" means to a
// subscription-backed run.
func TestEffortAndConnectorRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "claude-opus")
	s.SetEffort("high")
	s.SetConnector("claude")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Effort != "high" || got.Connector != "claude" {
		t.Fatalf("effort = %q connector = %q, want high/claude", got.Effort, got.Connector)
	}
	if got.SessionEffort() != "high" || got.ConnectorName() != "claude" {
		t.Fatalf("accessors do not agree with the fields")
	}
}

// Sessions written before these fields existed must load exactly as before.
func TestOldSessionFileWithoutEffortLoadsEmpty(t *testing.T) {
	dir := t.TempDir()
	body := `{"id":"old-one","model":"claude-opus","title":"t","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","messages":[]}`
	if err := os.WriteFile(filepath.Join(dir, "old-one.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "old-one")
	if err != nil {
		t.Fatal(err)
	}
	if got.Effort != "" || got.Connector != "" {
		t.Fatalf("absent fields must stay absent, got effort=%q connector=%q", got.Effort, got.Connector)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "test/model")
	s.SetTitleFromInput("  fix   the\nlogin bug  ")
	s.SetMessages([]provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "fix the login bug"},
		{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "bash", Arguments: `{"command":"ls"}`}}}},
		{Role: "tool", ToolCallID: "c1", Content: "file.go"},
	})
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := Load(dir, s.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Title != "fix the login bug" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Model != "test/model" {
		t.Errorf("model = %q", got.Model)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(got.Messages))
	}
	pmsgs := got.GetMessages()
	if len(pmsgs) != 4 {
		t.Fatalf("GetMessages = %d, want 4", len(pmsgs))
	}
	if pmsgs[2].ToolCalls[0].Function.Name != "bash" {
		t.Errorf("tool call did not survive roundtrip: %+v", pmsgs[2])
	}
	// dir must be rehydrated so a resumed session saves to the right place
	if err := got.Save(); err != nil {
		t.Fatalf("save after load: %v", err)
	}
}

func TestLoadV0SessionFixture(t *testing.T) {
	s, err := Load("testdata", "v0-session")
	if err != nil {
		t.Fatalf("Load v0-session fixture: %v", err)
	}

	if s.ID != "s_01J00000000000000000000000" {
		t.Errorf("ID = %q, want s_01J00000000000000000000000", s.ID)
	}
	if s.Model != "anthropic/claude-3.5-sonnet" {
		t.Errorf("Model = %q, want anthropic/claude-3.5-sonnet", s.Model)
	}
	if s.Title != "build the user login workflow" {
		t.Errorf("Title = %q, want build the user login workflow", s.Title)
	}
	if len(s.Messages) != 5 {
		t.Fatalf("len(Messages) = %d, want 5", len(s.Messages))
	}

	pmsgs := s.GetMessages()
	if len(pmsgs) != 5 {
		t.Fatalf("len(GetMessages) = %d, want 5", len(pmsgs))
	}

	// Verify message 0: system
	if pmsgs[0].Role != "system" || pmsgs[0].Content != "You are Kolkrabbi, an expert pair programmer." {
		t.Errorf("msg[0] mismatch: %+v", pmsgs[0])
	}
	// Verify message 1: user
	if pmsgs[1].Role != "user" || pmsgs[1].Content != "build the user login workflow" {
		t.Errorf("msg[1] mismatch: %+v", pmsgs[1])
	}
	// Verify message 2: assistant with tool calls
	if pmsgs[2].Role != "assistant" || len(pmsgs[2].ToolCalls) != 1 {
		t.Fatalf("msg[2] mismatch: %+v", pmsgs[2])
	}
	tc := pmsgs[2].ToolCalls[0]
	if tc.ID != "call_01J00000000000000000000001" || tc.Function.Name != "read_file" || tc.Function.Arguments != `{"path":"internal/auth/login.go"}` {
		t.Errorf("tool_call mismatch: %+v", tc)
	}
	// Verify message 3: tool result
	if pmsgs[3].Role != "tool" || pmsgs[3].ToolCallID != "call_01J00000000000000000000001" {
		t.Errorf("msg[3] mismatch: %+v", pmsgs[3])
	}
	// Verify message 4: assistant
	if pmsgs[4].Role != "assistant" || pmsgs[4].Content != "Login implementation looks solid." {
		t.Errorf("msg[4] mismatch: %+v", pmsgs[4])
	}

	// Re-save to temp directory to verify serialization fidelity
	tmp := t.TempDir()
	s.dir = tmp
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loadedAgain, err := Load(tmp, s.ID)
	if err != nil {
		t.Fatalf("Load re-saved fixture: %v", err)
	}
	if len(loadedAgain.Messages) != 5 {
		t.Fatalf("re-saved fixture message count = %d, want 5", len(loadedAgain.Messages))
	}
}

func TestLatestAndList(t *testing.T) {
	dir := t.TempDir()

	older := New(dir, "m")
	if err := older.Save(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // ensure distinct UpdatedAt
	newer := New(dir, "m")
	if err := newer.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Latest(dir)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil || got.ID != newer.ID {
		t.Errorf("latest = %v, want %s", got, newer.ID)
	}

	all, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 || all[0].ID != newer.ID {
		t.Errorf("list order wrong: %v", all)
	}

	if err := Delete(dir, older.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	all, _ = List(dir)
	if len(all) != 1 {
		t.Errorf("after delete, %d sessions remain, want 1", len(all))
	}
}

func TestLatestEmptyDir(t *testing.T) {
	got, err := Latest(t.TempDir())
	if err != nil || got != nil {
		t.Errorf("Latest on empty dir = (%v, %v), want (nil, nil)", got, err)
	}
}
