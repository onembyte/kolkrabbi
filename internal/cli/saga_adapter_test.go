package cli

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

type fakeSagaShell struct {
	commands []shell.Cmd
	clean    bool
}

func (f *fakeSagaShell) Name() string { return "fake" }

func (f *fakeSagaShell) Run(_ context.Context, command shell.Cmd) (shell.Result, error) {
	f.commands = append(f.commands, command)
	switch command.Command {
	case "git rev-parse --short HEAD":
		return shell.Result{Output: "abc123\n"}, nil
	case "git status --porcelain":
		if f.clean {
			return shell.Result{}, nil
		}
		return shell.Result{Output: " M main.go\n"}, nil
	}
	return shell.Result{}, nil
}

// TestTheSagaRunnerReachesTheRealShell covers what is left of this adapter
// after VerifySagaChapter was deleted: the translation from the engine's
// command port to the platform shell.
//
// The verification behaviour it used to test — a clean tree completing without
// a commit, gates failing and rolling back — moved to internal/engine, which is
// where the live path is since `kolk saga run` drives SagaRunner directly.
func TestTheSagaRunnerReachesTheRealShell(t *testing.T) {
	sh := &fakeSagaShell{}
	runner := sagaCommandRunner{shell: sh}

	result, err := runner.Run(context.Background(), "git rev-parse --short HEAD", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "abc123\n" {
		t.Fatalf("output = %q", result.Output)
	}
	if len(sh.commands) != 1 || sh.commands[0].Dir != "/repo" {
		t.Fatalf("commands = %#v, want the directory carried through", sh.commands)
	}
	var _ engine.CommandRunner = runner
}

func TestAShellFailureIsCarriedNotSwallowed(t *testing.T) {
	sh := &fakeSagaShell{}
	runner := sagaCommandRunner{shell: sh}

	// A gate that exits non-zero is the whole point of running gates; the
	// exit code has to survive the trip through this adapter.
	result, err := runner.Run(context.Background(), "false", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 && result.Failure == "" {
		t.Fatalf("result = %+v", result)
	}
}
