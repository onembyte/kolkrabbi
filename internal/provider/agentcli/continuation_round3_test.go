package agentcli

// Found by the round-3 vendor-recovery verifier: each was red on the tree it
// reviewed (or, for the concurrency check, guards the restore it introduced).

// Round-3 verifier probes (read-only overlay).

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func r3WaitExited(t *testing.T, backend *ClaudeBackend) {
	t.Helper()
	backend.mu.Lock()
	session := backend.session
	backend.mu.Unlock()
	if session == nil {
		t.Fatal("setup: no live session to wait on")
	}
	exited, ok := session.process.(interface{ Exited() bool })
	if !ok {
		t.Fatal("setup: the real process cannot say whether it exited")
	}
	for deadline := time.Now().Add(5 * time.Second); !exited.Exited() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if !exited.Exited() {
		t.Fatal("setup: the child never exited")
	}
}

// R3-1. A process opened with --resume H answers a turn (the vendor closes
// it), then exits between turns. The next turn's Queue is false, so the
// adapter retries; but its "dead resume" test (Resumed && !Received) reads
// the per-turn received flag, so it forgets H, and the retry opens a brand
// new conversation. A continuation sent now reaches a conversation that
// never saw the vendor's work.
func TestRound3AnIdleResumedProcessThatExitedKeepsItsConversation(t *testing.T) {
	path, argv := fakeClaudeScript(t, `sid=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--session-id" ] || [ "$prev" = "--resume" ]; then sid="$a"; fi
  prev="$a"
done
IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"ok"}]}}'
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then exit 0; fi
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "first"); err != nil {
		t.Fatalf("setup: turn 1: %v", err)
	}
	r3WaitExited(t, backend)
	_, _, secondErr := backend.StreamChat(t.Context(), "opus", []provider.Message{
		{Role: "user", Content: "first"}, {Role: "assistant", Content: "ok"},
		{Role: "user", Content: "The previous attempt stopped before this request finished. Continue this unfinished request"}}, nil, nil)
	raw, _ := os.ReadFile(argv)
	spawns := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(spawns) < 2 {
		t.Fatalf("setup: spawns = %q (err=%v)", spawns, secondErr)
	}
	if !strings.Contains(spawns[1], "--resume H-saved") {
		t.Fatalf("IDENTITY: the retry after an idle resumed process exited left conversation H-saved and sent the continuation to a new one: %q (err=%v, handle now %q)",
			spawns[1], secondErr, backend.ProviderHandle())
	}
}

// R3-2. The same, for a process opened with --session-id and killed by a
// signal between turns (its last turn closed). The kill is only noticed in
// the next turn; HardExit retires the handle, and the Queue-false retry
// opens a new conversation.
func TestRound3AnIdleProcessKilledBetweenTurnsKeepsItsConversation(t *testing.T) {
	path, argv := fakeClaudeScript(t, `sid=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--session-id" ] || [ "$prev" = "--resume" ]; then sid="$a"; fi
  prev="$a"
done
IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"ok"}]}}'
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then sleep 0.3; kill -9 $$; fi
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "first"); err != nil {
		t.Fatalf("setup: turn 1: %v", err)
	}
	first := backend.ProviderHandle()
	r3WaitExited(t, backend)
	_, secondErr := oneTurn(t, backend, "second")
	raw, _ := os.ReadFile(argv)
	spawns := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(spawns) < 2 {
		t.Fatalf("setup: spawns = %q (err=%v)", spawns, secondErr)
	}
	if !strings.Contains(spawns[1], "--resume "+first) {
		t.Fatalf("IDENTITY: after an idle process was killed, the retry left conversation %s for a new one: %q (err=%v)", first, spawns[1], secondErr)
	}
}

// R3-6 (fail-closed check). A codex continuation whose binary cannot start
// never reached a process. Does the adapter prove it, as the v3 text says
// ("the process could not start")?
func TestRound3ACodexProcessThatCannotStartIsProvenUnstarted(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	backend, err := NewCodexBackendFromHandleWithOptions("gpt-5.6-sol", "code", "", "th-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, runErr := backend.StreamChat(t.Context(), "gpt-5.6-sol", []provider.Message{{Role: "user", Content: "continue"}}, nil, nil)
	if !backend.TurnNeverStarted() {
		t.Fatalf("UNPROVEN: codex could not start (%v), yet the turn is recorded as delivered; its saved closure will be overwritten", runErr)
	}
}
