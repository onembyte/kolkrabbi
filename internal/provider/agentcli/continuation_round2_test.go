package agentcli

// Verifier v3 probes (read-only overlay).

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// P3 (unit). The drain after an interrupted read consumes this turn's frames
// and keeps closure and the handle, but a tool the vendor started there never
// reaches observe, so the journal cannot know it is unfinished.
func TestRound2ADrainedToolReachesTheObserver(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	effect, sends := filepath.Join(t.TempDir(), "effect"), 0
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return &lostOutputProcess{effect: effect, sends: &sends, drain: true}, nil
	}
	var observed []provider.ProgressEvent
	_, _, _ = backend.StreamChatObserved(provider.WithToolProgress(context.Background()), "claude-opus",
		[]provider.Message{{Role: "user", Content: "deploy"}}, nil, func(string) {},
		func(event provider.ProgressEvent) { observed = append(observed, event) })
	if !backend.TurnClosed() {
		t.Fatal("setup: the drained result should close the turn")
	}
	for _, event := range observed {
		if event.Kind == provider.ProgressToolStarted && event.ID == "done" {
			return
		}
	}
	t.Fatalf("DRAIN: the turn is reported closed (continuable) but the tool %q the vendor started in the drain never reached observe: %+v", "done", observed)
}

// fakeClaudeScript writes an executable that behaves as the n-th spawn says.
func fakeClaudeScript(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	count := filepath.Join(dir, "spawns")
	argv := filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + argv + "\"\n" +
		"n=$(( $(cat \"" + count + "\" 2>/dev/null || echo 0) + 1 ))\n" +
		"echo $n > \"" + count + "\"\n" + body
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path, argv
}

func realStart(path string) startLineProcess {
	return func(ctx context.Context, _ string, args []string) (lineProcess, error) {
		return shell.StartLinesProcess(ctx, path, args)
	}
}

// Item 2. TestAnExpiredPlanLoginLeavesTheSessionUsable now uses
// fakeLineProcess.exitWhenDrained, whose Send fails once the process has
// exited. The production LinesProcess.Send never returns an error for an
// exited child (it drops the line and returns nil). Driven through a real
// process, the same scenario: does the first turn after a re-login answer?
func TestRound2ExpiredLoginWithARealProcess(t *testing.T) {
	path, argv := fakeClaudeScript(t, `IFS= read -r line
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Invalid API key · Please run /login"}'
  exit 1
fi
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"signed back in"}]}}'
printf '%s\n' '{"type":"result","result":"signed back in","subtype":"success"}'
cat > /dev/null
`)
	backend := &ClaudeBackend{start: realStart(path)}
	defer backend.Close()
	if _, err := oneTurn(t, backend, "before"); err == nil {
		t.Fatal("setup: the expired login answered")
	}
	// Signing in again takes the person a while; by then the refusing child
	// has long exited. Wait for that, as the next turn would. (A turn sent in
	// the instant the child dies counts as delivered, and fails safe.)
	exited, ok := backend.session.process.(interface{ Exited() bool })
	if !ok {
		t.Fatal("the real process cannot say whether it exited")
	}
	for deadline := time.Now().Add(5 * time.Second); !exited.Exited() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	msg, err := oneTurn(t, backend, "after")
	spawns, _ := os.ReadFile(argv)
	if err != nil || !strings.Contains(msg.Content, "signed back in") {
		t.Fatalf("FAKE INFIDELITY: with a real process the first turn after re-login fails (%v); the exitWhenDrained fake hides it. spawns:\n%s", err, spawns)
	}
}

// Item 2, control. A stored handle that resumes dead, with a real process:
// the turn fails without resending, and the next turn opens a fresh
// conversation (no wedge).
func TestRound2DeadResumeWithARealProcessDoesNotWedge(t *testing.T) {
	path, argv := fakeClaudeScript(t, `if [ "$n" = "1" ]; then
  echo "No conversation found with session ID" >&2
  exit 1
fi
IFS= read -r line
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"fresh"}]}}'
printf '%s\n' '{"type":"result","result":"fresh","subtype":"success"}'
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "dead-h", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	_, firstErr := oneTurn(t, backend, "one")
	neverStarted := backend.TurnNeverStarted()
	msg, secondErr := oneTurn(t, backend, "two")
	raw, _ := os.ReadFile(argv)
	spawns := strings.Split(strings.TrimSpace(string(raw)), "\n")
	t.Logf("first turn err=%v neverStarted=%v; second turn %q err=%v; spawns=%d", firstErr, neverStarted, msg.Content, secondErr, len(spawns))
	if secondErr != nil || msg.Content != "fresh" {
		t.Fatalf("WEDGE: the turn after a dead resume did not answer: %v", secondErr)
	}
	last := strings.Fields(spawns[len(spawns)-1])
	if slices.Contains(last, "--resume") || slices.Contains(last, "dead-h") {
		t.Fatalf("WEDGE: the later spawn resumed the dead handle: %q", spawns)
	}
}
