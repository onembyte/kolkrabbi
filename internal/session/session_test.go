package session

import (
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "test/model")
	s.SetTitleFromInput("  fix   the\nlogin bug  ")
	s.Messages = []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "fix the login bug"},
		{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "bash", Arguments: `{"command":"ls"}`}}}},
		{Role: "tool", ToolCallID: "c1", Content: "file.go"},
	}
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
	if got.Messages[2].ToolCalls[0].Function.Name != "bash" {
		t.Errorf("tool call did not survive roundtrip: %+v", got.Messages[2])
	}
	// dir must be rehydrated so a resumed session saves to the right place
	if err := got.Save(); err != nil {
		t.Fatalf("save after load: %v", err)
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
