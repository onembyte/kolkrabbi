package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// The bash tool carries the policy through to the shell. With one attached
// that cannot be established -- here a root that does not exist, which no
// enforcer can resolve, and on platforms without an enforcer the probe itself
// -- the command does not run and the model is told why and how to switch it
// off. With none attached, it runs.
func TestBashRefusesWhenTheSandboxCannotBeEstablished(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "does-not-exist")
	out, err := Execute(context.Background(), "bash", `{"command":"echo ran","description":"probe"}`, Options{
		Root:    dir,
		Sandbox: &shell.Sandbox{Root: gone, Temp: t.TempDir(), Network: shell.NetworkAllow},
	})
	if err != nil {
		t.Fatalf("a refusal must not abort the turn: %v", err)
	}
	if strings.Contains(out, "ran") {
		t.Fatalf("the command ran: %q", out)
	}
	for _, want := range []string{"sandbox could not be established", "/sandbox off"} {
		if !strings.Contains(out, want) {
			t.Errorf("tool result = %q, want %q in it", out, want)
		}
	}
}

func TestBashRunsWhenNoSandboxPolicyIsAttached(t *testing.T) {
	out, err := Execute(context.Background(), "bash", `{"command":"echo ran","description":"probe"}`, Options{Root: t.TempDir()})
	if err != nil || !strings.Contains(out, "ran") {
		t.Fatalf("err=%v out=%q", err, out)
	}
}
