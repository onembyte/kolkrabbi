//go:build darwin || linux

package cli

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

// The whole point, end to end: a provider login runs inside the session, on a
// terminal of its own, and the session comes back.
//
// This is the exact pair tuiRepl wires into a.loginInSession. It runs a
// harmless child rather than a real vendor CLI — nothing here touches a
// credential — but it exercises the seam that made the login possible at all.
func TestALoginRunsInsideTheSessionOnItsOwnTerminal(t *testing.T) {
	input, keys := io.Pipe()
	defer keys.Close()
	var screen strings.Builder
	runtime := tui.NewRuntime(tui.RuntimeOptions{Input: input, Output: &screen,
		Status: tui.Status{Mode: "code", Lifecycle: "ready"}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runtime.Run(ctx) }()

	inSession := func(ctx context.Context, executable string, args []string) error {
		return runtime.RunAttached(ctx, func(in io.Reader, out io.Writer, width, height int) error {
			return shell.RunInSession(ctx, executable, args, in, out, width, height)
		})
	}
	if err := inSession(ctx, "sh", []string{"-c", "tty; stty size"}); err != nil {
		t.Fatalf("the in-session login failed: %v", err)
	}

	got := screen.String()
	// A vendor CLI like `claude auth login` is a full-screen UI that refuses to
	// run without a terminal. If this is not true, the login cannot work.
	if !strings.Contains(got, "/dev/tty") {
		t.Errorf("the child was not given a terminal:\n%s", got)
	}
	// And the session has to still be there afterwards.
	if !strings.Contains(got, "kolkrabbi") {
		t.Errorf("the session did not come back after the login:\n%s", got)
	}
}

// The runner has to be preferred over every other path when a session is up,
// because a window kolk opens is a second place to look — and on a stock macOS
// there is no emulator binary on PATH for it to open at all, which is how a
// login failed outright with "no terminal emulator found".
func TestAnInSessionRunnerIsPreferredOverAWindow(t *testing.T) {
	dirs := isolateHome(t)
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs

	windowUsed, sessionUsed := false, false
	a.handoverWindow = func(context.Context, string, []string) error {
		windowUsed = true
		return nil
	}
	a.terminalOwned = func() bool { return true }
	a.loginInSession = func(_ context.Context, executable string, args []string) error {
		sessionUsed = true
		if executable != "claude" || strings.Join(args, " ") != "auth login" {
			t.Errorf("ran %q %v, want the claude login subcommand", executable, args)
		}
		return nil
	}

	if err := a.runPlanLogin(context.Background(), []string{"anthropic", "Claude", "Max"}); err != nil {
		t.Fatalf("runPlanLogin: %v", err)
	}
	if !sessionUsed {
		t.Error("the login did not run inside the session")
	}
	if windowUsed {
		t.Error("a separate window was opened even though the session could host the login")
	}
	if a.pendingLogin != nil {
		t.Error("the login was deferred to after the session ended")
	}
	// The connector is still recorded, exactly as the other runners do.
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Connectors) == 0 {
		t.Errorf("no connector was recorded; output was %q", out.String())
	}
}

// Without a session there is nothing to run inside, so the old paths must still
// be reachable.
func TestWithoutASessionTheWindowPathStillRuns(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	a.loginInSession = nil
	a.terminalOwned = func() bool { return true }

	windowUsed := false
	a.handoverWindow = func(context.Context, string, []string) error {
		windowUsed = true
		return nil
	}
	if err := a.runPlanLogin(context.Background(), []string{"anthropic", "Claude", "Max"}); err != nil {
		t.Fatalf("runPlanLogin: %v", err)
	}
	if !windowUsed {
		t.Error("with no in-session runner, the window path was not used")
	}
}
