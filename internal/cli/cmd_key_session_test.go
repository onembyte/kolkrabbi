package cli

import (
	"context"
	"strings"
	"testing"
	"time"
)

// `kolk key -` reads the key from stdin, which is a piping idiom. Inside a
// session the terminal belongs to the session's own reader, so the same read
// competes for the user's keystrokes and the prompt appears to hang.
func TestKeyFromStdinIsRefusedWhileKolkrabbiOwnsTheTerminal(t *testing.T) {
	isolateConnectorState(t)
	a, ag, _ := replFixture(t, "")
	a.terminalOwned = func() bool { return true }
	var errOut strings.Builder
	a.stderr = &errOut

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.slash(context.Background(), ag, "/key -")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("/key - read the terminal and hung the session")
	}

	if !strings.Contains(errOut.String(), "kolk key") {
		t.Fatalf("stderr = %q, want the command to run outside the session", errOut.String())
	}
}

func TestKeyFromStdinStillWorksOutsideASession(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "sk-or-v1-"+strings.Repeat("a", 64)+"\n")

	if code := a.main(context.Background(), []string{"key", "-"}); code != ExitOK {
		t.Fatalf("key - exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "openrouter") {
		t.Fatalf("output = %q", out.String())
	}
}
