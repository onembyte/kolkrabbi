package cli

import (
	"context"
	"os"
	"testing"
)

func TestSagaStatusPrintsSAGAArtifact(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("SAGA.md", []byte("# SAGA: ship\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a, out, errOut := newTestApp("")
	code := a.main(context.Background(), []string{"saga", "status"})
	if code != ExitOK {
		t.Fatalf("kolk saga status exit = %d (stderr: %s)", code, errOut.String())
	}
	if out.String() != "# SAGA: ship\n" {
		t.Fatalf("stdout = %q", out.String())
	}
}
