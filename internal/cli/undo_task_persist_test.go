package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// /undo task appends a message telling the model what was taken back. If that
// message lives only in memory, quitting before the next autosave leaves a
// transcript on disk that still believes the edits exist -- and the next resume
// acts on files that are not there. The reconciliation is persisted at once.
func TestUndoTaskPersistsTheReconciliationMessage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for the shadow store")
	}
	project := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = project
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "kept.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = project
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	ctx := context.Background()
	a, ag, out := replFixture(t, "")
	sessDir := t.TempDir()
	sess := session.New(sessDir, "mock/model")
	ag.Sess = sess
	store, err := checkpoint.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.UseShadow(ctx, project)
	if store.Strategy() != checkpoint.StrategyShadow {
		t.Skipf("shadow store unavailable: %s", store.Notice())
	}
	ag.Ckpt = store
	store.BeginTurn(ctx)
	handle := store.BeginTask(ctx, "task one")
	if err := os.WriteFile(filepath.Join(project, "made.txt"), []byte("by the task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.EndTask(ctx, handle)

	a.undoTask(ctx, ag, "1")
	if !strings.Contains(out.String(), "took back subagent 1") {
		t.Fatalf("the undo did not happen:\n%s", out)
	}
	loaded, err := session.Load(sessDir, sess.SessionID())
	if err != nil {
		t.Fatalf("the session was not on disk after /undo task: %v", err)
	}
	msgs := loaded.GetMessages()
	if len(msgs) == 0 || !strings.Contains(msgs[len(msgs)-1].Content, "took back subagent 1") {
		t.Fatalf("the reconciliation message is not in the saved session: %+v", msgs)
	}
}
