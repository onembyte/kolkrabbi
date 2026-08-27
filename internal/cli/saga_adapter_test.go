package cli

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

type fakeSagaShell struct {
	commands []shell.Cmd
	clean    bool // report a worktree with nothing to commit
}

func (f *fakeSagaShell) Name() string { return "fake" }

func (f *fakeSagaShell) Run(_ context.Context, command shell.Cmd) (shell.Result, error) {
	f.commands = append(f.commands, command)
	switch command.Command {
	case "git rev-parse --short HEAD":
		return shell.Result{Output: "abc123\n"}, nil
	case "git status --porcelain":
		// The ports design asks whether there is anything to verify before it
		// runs a gate or makes a commit. A clean tree is a finished chapter,
		// not a failed one.
		if f.clean {
			return shell.Result{}, nil
		}
		return shell.Result{Output: " M main.go\n"}, nil
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
	if len(sh.commands) != 3 || sh.commands[0].Command != "git status --porcelain" ||
		sh.commands[1].Command != "git add -A && git commit -m 'saga(chapter 1): wire'" {
		t.Fatalf("commands = %#v", sh.commands)
	}
}

func TestVerifySagaChapterRejectsMissingShell(t *testing.T) {
	err := VerifySagaChapter(context.Background(), nil, t.TempDir(), &engine.SagaState{}, 0)
	if err == nil {
		t.Fatal("VerifySagaChapter() error = nil, want missing-shell error")
	}
}

func TestACleanTreeIsNotCommitted(t *testing.T) {
	sh := &fakeSagaShell{clean: true}
	state := &engine.SagaState{Chapters: []engine.Chapter{{Number: 1, Title: "nothing to do", Status: engine.StatusExecuting}}}

	if err := VerifySagaChapter(context.Background(), sh, t.TempDir(), state, 0); err != nil {
		t.Fatalf("VerifySagaChapter() error = %v", err)
	}

	// A chapter that changed nothing is done, and an empty commit recording
	// that would be a revision nobody can learn anything from.
	for _, command := range sh.commands {
		if command.Command != "git status --porcelain" {
			t.Fatalf("a clean tree ran %q", command.Command)
		}
	}
	if state.Chapters[0].Status != engine.StatusDone {
		t.Fatalf("status = %q, want done", state.Chapters[0].Status)
	}
}
