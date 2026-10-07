package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestExecutionJournalRetainsHistoryAndOwnsSnapshots(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "model")
	handle := 3
	run := &continuity.Run{Version: 1, ID: "first", Input: "goal", Phase: "tasks", Tasks: []continuity.Task{{
		Title: "task", Needs: []int{0}, Checkpoint: &handle,
		Messages: []provider.Message{{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "call"}}}},
		Loop:     continuity.ToolLoop{Recent: []string{"done"}},
	}}}
	s.SetRunState(run)
	run.Tasks[0].Messages[0].ToolCalls[0].ID = "changed"
	run.Tasks[0].Needs[0] = 9
	handle = 99
	want := s.RunState()
	got := s.RunState()
	got.Tasks[0].Loop.Recent[0] = "changed"
	if want.Tasks[0].Needs[0] != 0 || *want.Tasks[0].Checkpoint != 3 || want.Tasks[0].Messages[0].ToolCalls[0].ID != "call" {
		t.Fatalf("stored snapshot aliases writer: %+v", want)
	}
	if !reflect.DeepEqual(want, s.RunState()) {
		t.Fatal("returned snapshot aliases stored journal")
	}
	want.Phase = "done"
	s.SetRunState(want)
	s.SetRunState(&continuity.Run{Version: 1, ID: "second", Phase: "plan"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := loaded.ExecutionHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Executions) != 1 || len(history) != 2 || !reflect.DeepEqual(history[0], *want) || loaded.RunState().ID != "second" {
		t.Fatalf("execution history lost on reload: %+v", loaded.Executions)
	}
}

func TestArchivedChildHistoryDoesNotInflateEveryLaterSave(t *testing.T) {
	s := New(t.TempDir(), "model")
	s.SetRunState(&continuity.Run{ID: "first", Phase: "done", Tasks: []continuity.Task{{Messages: []provider.Message{{Role: "tool", Content: strings.Repeat("x", 256*1024)}}}}})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s.SetRunState(&continuity.Run{ID: "second", Phase: "done"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.path())
	if err != nil || info.Size() > 4096 {
		t.Fatalf("hot session retains archived payload: %v %v", info, err)
	}
	archive := filepath.Join(s.executionDir(), executionFile("first"))
	before, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveInterim(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(archive)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("interim save rewrote immutable history")
	}
	history, err := s.ExecutionHistory()
	if err != nil || len(history) != 2 || len(history[0].Tasks[0].Messages[0].Content) != 256*1024 {
		t.Fatalf("archiving dropped child history: %v", err)
	}
}

func TestFailedExecutionArchiveKeepsTheOriginalRecords(t *testing.T) {
	s := New(t.TempDir(), "model")
	s.SetRunState(&continuity.Run{ID: "first", Phase: "done"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.executionDir(), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetRunState(&continuity.Run{ID: "second", Phase: "plan"})
	if err := s.Save(); err == nil {
		t.Fatal("archive failure was hidden")
	}
	if len(s.Executions) != 2 {
		t.Fatal("archive failure discarded in-memory history")
	}
	loaded, err := Load(s.dir, s.ID)
	if err != nil || loaded.RunState().ID != "first" {
		t.Fatalf("archive failure replaced previous durable history: %v", err)
	}
}
