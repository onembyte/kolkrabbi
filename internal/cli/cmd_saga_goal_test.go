package cli

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSagaGoalPersistsSAGAArtifact(t *testing.T) {
	t.Chdir(t.TempDir())
	a, out, errOut := newTestApp("")
	code := a.main(context.Background(), []string{"saga", "fix", "all", "tests"})
	if code != ExitOK {
		t.Fatalf("kolk saga goal exit = %d (stderr: %s)", code, errOut.String())
	}
	body, err := os.ReadFile("SAGA.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "- **Goal**: fix all tests") {
		t.Fatalf("artifact = %q", body)
	}
	if !strings.Contains(out.String(), "saga goal set: fix all tests") {
		t.Fatalf("stdout = %q", out.String())
	}
}
