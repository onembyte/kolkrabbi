package cli

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

type fakeSagaShell struct {
	commands []shell.Cmd
}

func (f *fakeSagaShell) Name() string { return "fake" }

func (f *fakeSagaShell) Run(_ context.Context, command shell.Cmd) (shell.Result, error) {
	f.commands = append(f.commands, command)
	if command.Command == "git rev-parse --short HEAD" {
		return shell.Result{Output: "abc123\n"}, nil
	}
	return shell.Result{}, nil
}

func TestVerifySagaChapterWiresShellAndArtifactWriter(t *testing.T) {
	sh := &fakeSagaShell{}
	dir := t.TempDir()
	state := &engine.SagaState{Chapters: []engine.Chapter{{Number: 1, Title: "wire", Status: engine.StatusExecuting}}}
	if err := VerifySagaChapter(context.Background(), sh, dir, state, 0); err != nil {
		t.Fatalf("VerifySagaChapter() error = %v", err)
	}
	if state.Chapters[0].Commit != "abc123" {
		t.Fatalf("commit = %q, want abc123", state.Chapters[0].Commit)
	}
	if len(sh.commands) != 2 || sh.commands[0].Command != "git add -A && git commit -m 'saga(chapter 1): wire'" {
		t.Fatalf("commands = %#v", sh.commands)
	}
}

func TestVerifySagaChapterRejectsMissingShell(t *testing.T) {
	err := VerifySagaChapter(context.Background(), nil, t.TempDir(), &engine.SagaState{}, 0)
	if err == nil {
		t.Fatal("VerifySagaChapter() error = nil, want missing-shell error")
	}
}
