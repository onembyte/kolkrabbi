package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSagaStatusNoActiveSaga(t *testing.T) {
	t.Chdir(t.TempDir())
	a, out, errOut := newTestApp("")
	code := a.main(context.Background(), []string{"saga", "status"})
	if code != ExitOK {
		t.Fatalf("kolk saga status exit = %d (stderr: %s)", code, errOut.String())
	}

	if !strings.Contains(out.String(), "no active saga") {
		t.Errorf("stdout = %q, want 'no active saga'", out.String())
	}

}

func TestSagaNoArgsReturnsUsage(t *testing.T) {
	a, _, errOut := newTestApp("")
	code := a.main(context.Background(), []string{"saga"})
	if code != ExitUsage {
		t.Fatalf("kolk saga exit = %d, want ExitUsage", code)
	}
	if !strings.Contains(errOut.String(), "usage: kolk saga") {
		t.Errorf("stderr = %q, want usage string", errOut.String())
	}
}

func TestSagaGoalSetsGoal(t *testing.T) {
	t.Chdir(t.TempDir())
	a, out, errOut := newTestApp("")
	code := a.main(context.Background(), []string{"saga", "fix", "all", "tests"})
	if code != ExitOK {
		t.Fatalf("kolk saga goal exit = %d (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "saga goal set: fix all tests") {
		t.Errorf("stdout = %q, want goal set message", out.String())
	}
}

func TestSagaSubcommands(t *testing.T) {
	// Without an isolated working directory these commands read, and the goal
	// test writes, the SAGA.md of whatever project is running the suite.
	t.Chdir(t.TempDir())
	tests := []struct {
		subcommand string
		want       string
	}{
		{"resume", "no saga to resume"},
		{"stop", "no running saga to stop"},
		{"rewind", "no saga chapters to rewind"},
	}

	for _, tt := range tests {
		a, out, errOut := newTestApp("")
		code := a.main(context.Background(), []string{"saga", tt.subcommand})
		if code != ExitOK {
			t.Fatalf("kolk saga %s exit = %d (stderr: %s)", tt.subcommand, code, errOut.String())
		}
		if !strings.Contains(out.String(), tt.want) {
			t.Errorf("kolk saga %s stdout = %q, want %q", tt.subcommand, out.String(), tt.want)
		}
	}
}

// projectTree builds a repository with a nested working directory and chdirs
// into the nested one, which is where a saga command is most likely to be run
// from by accident.
func projectTree(t *testing.T) (root, nested string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested = filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	return root, nested
}

func TestSagaGoalWritesTheArtifactAtTheProjectRoot(t *testing.T) {
	root, nested := projectTree(t)
	a, _, errOut := newTestApp("")

	if code := a.main(context.Background(), []string{"saga", "fix", "all", "tests"}); code != ExitOK {
		t.Fatalf("saga goal exit = %d, stderr = %q", code, errOut.String())
	}

	if _, err := os.Stat(filepath.Join(root, "SAGA.md")); err != nil {
		t.Fatalf("the saga artifact is not at the project root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nested, "SAGA.md")); err == nil {
		t.Fatal("running saga from a subdirectory littered that subdirectory with SAGA.md")
	}
}

func TestSagaStatusReadsTheProjectRootArtifactFromAnySubdirectory(t *testing.T) {
	root, _ := projectTree(t)
	if err := os.WriteFile(filepath.Join(root, "SAGA.md"), []byte("# SAGA: ship it\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, out, errOut := newTestApp("")

	if code := a.main(context.Background(), []string{"saga", "status"}); code != ExitOK {
		t.Fatalf("saga status exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ship it") {
		t.Fatalf("status from a subdirectory = %q, want the project's saga", out.String())
	}
}

func TestSagaSubcommandsReportTheRealStateOfAnActiveSaga(t *testing.T) {
	root, _ := projectTree(t)
	a, _, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"saga", "ship", "the", "thing"}); code != ExitOK {
		t.Fatalf("saga goal exit = %d, stderr = %q", code, errOut.String())
	}

	// `resume` is deliberately not exercised here any more. Since S10.6 it
	// works the chapters, which means building an agent and calling a
	// provider — and this test did exactly that, reaching OpenRouter from
	// `make check`. The loop's behaviour is covered offline in
	// internal/engine's executor and planner tests, with fakes.
	for _, tt := range []struct{ subcommand, want string }{
		{"status", "ship the thing"},
		{"rewind", "ship the thing"},
	} {
		a, out, errOut := newTestApp("")
		if code := a.main(context.Background(), []string{"saga", tt.subcommand}); code != ExitOK {
			t.Fatalf("saga %s exit = %d, stderr = %q", tt.subcommand, code, errOut.String())
		}
		if !strings.Contains(out.String(), tt.want) {
			t.Fatalf("saga %s said %q while a saga is in progress", tt.subcommand, out.String())
		}
	}

	a, out, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"saga", "stop"}); code != ExitOK {
		t.Fatalf("saga stop exit = %d, stderr = %q", code, errOut.String())
	}
	if strings.Contains(out.String(), "no running saga") {
		t.Fatalf("saga stop denied a saga that is in progress: %q", out.String())
	}
	body, err := os.ReadFile(filepath.Join(root, "SAGA.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "stopped") {
		t.Fatalf("stopping a saga did not record it: %q", body)
	}
}
