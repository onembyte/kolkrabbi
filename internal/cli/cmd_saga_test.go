package cli

import (
	"context"
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
