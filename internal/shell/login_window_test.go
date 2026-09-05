package shell

import (
	"context"
	"errors"
	"fmt"
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

// The own-window login is the same child one process further away: kolk
// starts the terminal emulator, the emulator starts `sh -c`, and the vendor's
// login runs inside. On Linux the emulator inherits kolk's environment and
// hands it straight down, so the same denylist has to apply here or the
// handover's proof would have a hole the size of a window. A fake $TERMINAL
// that execs whatever follows -e stands in for the emulator.
func TestLoginWindowNeverInheritsASentinelSecret(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "myterm")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n[ \"$1\" = -e ] && shift\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "env.txt")
	sentinels := []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AWS_SECRET_ACCESS_KEY", "GITHUB_PAT", "REGISTRY_AUTHTOKEN"}
	var probe strings.Builder
	probe.WriteString("#!/bin/sh\nprintf '%s' \"$GOFLAGS\" > \"$KOLK_TEST_WINDOW_OUT\"\n")
	for _, name := range sentinels {
		fmt.Fprintf(&probe, "printf '|%%s' \"$%s\" >> \"$KOLK_TEST_WINDOW_OUT\"\n", name)
	}
	login := filepath.Join(dir, "vendor-login")
	if err := os.WriteFile(login, []byte(probe.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range sentinels {
		t.Setenv(name, name+"-canary")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("KOLK_TEST_WINDOW_OUT", out)
	t.Setenv("PATH", dir+":/bin:/usr/bin")
	t.Setenv("TERMINAL", "myterm")
	t.Setenv("TERM_PROGRAM", "")

	if err := LoginWindow(context.Background(), login, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "-mod=mod" + strings.Repeat("|", len(sentinels))
	if string(got) != want {
		t.Fatalf("login window child environment = %q, want %q", got, want)
	}
}
