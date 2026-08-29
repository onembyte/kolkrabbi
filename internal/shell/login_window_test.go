package shell

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The login window's script is the only thing between the user and the
// vendor's login, so each part must hold: the command runs exactly as given,
// its exit status decides the message, and the window closes without help.
func TestLoginScriptRunsTheLoginAndPreservesItsStatus(t *testing.T) {
	script := loginScript("claude", []string{"auth", "login"})
	if !strings.HasPrefix(script, "'claude' 'auth' 'login'\nst=$?") {
		t.Fatalf("script = %q, want the quoted login as its first command", script)
	}
	if !strings.HasSuffix(script, "exit \"$st\"\n") {
		t.Fatalf("script = %q, want the login's exit status preserved", script)
	}
	for _, want := range []string{"Finished", "error", "sleep"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script = %q, want it to mention %q before closing", script, want)
		}
	}
}

// An executable name is data, never syntax: no argument may turn the script
// from a login into something else.
func TestLoginScriptQuotesItsWords(t *testing.T) {
	script := loginScript("weird-'emulator", []string{"login", "as'; rm -rf ~"})
	if !strings.HasPrefix(script, `'weird-'\''emulator' `) {
		t.Fatalf("script = %q, want the quote in the name quoted away", script)
	}
	cmd := exec.Command("sh", "-n", "-c", script)
	if err := cmd.Run(); err != nil {
		t.Fatalf("script does not parse as shell: %v\n%s", err, script)
	}
}

// The emulator comes from the user's own environment first — including any
// arguments they configured with it — and only then from the known list.
func TestTerminalEmulatorPrefersTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "myterm")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TERMINAL", "myterm --hold")
	t.Setenv("TERM_PROGRAM", "")

	term, prefix, ok := TerminalEmulator()
	if !ok || filepath.Base(term) != "myterm" {
		t.Fatalf("emulator = %q (%v), want myterm from $TERMINAL", term, ok)
	}
	if len(prefix) != 1 || prefix[0] != "--hold" {
		t.Fatalf("prefix args = %v, want the user's own options kept", prefix)
	}
}

// A $TERMINAL that names a program this machine does not have is not a
// terminal: the caller needs to know, not to be handed a broken path.
func TestTerminalEmulatorIgnoresAnUninstalledEnvironmentValue(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TERMINAL", "does-not-exist-9x8")
	t.Setenv("TERM_PROGRAM", "")

	if _, _, ok := TerminalEmulator(); ok {
		t.Fatal("a missing $TERMINAL program was reported as usable")
	}
	if err := LoginWindow(context.Background(), "claude", nil); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("LoginWindow = %v, want ErrNoTerminal", err)
	}
}
