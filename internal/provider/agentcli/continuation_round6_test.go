package agentcli

// Found by the round-6 vendor-recovery verifier, kept as a fail-safe pin.

import (
	"strings"
	"testing"
	"time"
)

// R6-K3, the P-A12 class with a kill. Turn one is closed by the vendor's
// result frame; the process prints one more frame (an idle status line) and is
// SIGKILLed while idle. The reader is parked on that unread trailing line, so
// the next prompt counts as delivered, the turn reads the stale frame and
// meets the kill, and the conversation is retired. That fails safe: the
// continuation is refused (delivered, not closed) and never reaches another
// conversation. What must never happen is a move or a continuable read.
func TestRound6KillAfterResultWithATrailingFrameNeverMovesTheContinuation(t *testing.T) {
	retired, runs := 0, 5
	for i := 0; i < runs; i++ {
		path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok $n\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"system","subtype":"status","status":"idle"}'
  sleep 0.2
  kill -9 $$
fi
cat > /dev/null
`)
		backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		backend.start = realStart(path)
		if _, err := oneTurn(t, backend, "one"); err != nil {
			t.Fatalf("setup: %v", err)
		}
		time.Sleep(600 * time.Millisecond) // the child is dead (SIGKILL) before the next turn
		_, turnErr := oneTurn(t, backend, "continue")
		spawns := round4Spawns(t, argv)
		for j, spawn := range spawns {
			if !strings.Contains(spawn, "--resume H-saved") {
				t.Fatalf("run %d: spawn %d %q left the saved conversation", i, j+1, spawn)
			}
		}
		t.Logf("run %d: err=%v delivered=%v closed=%v handle=%q retired=%v spawns=%d", i, turnErr, !backend.TurnNeverStarted(),
			backend.TurnClosed(), backend.ProviderHandle(), backend.ProviderHandleRetired(), len(spawns))
		if backend.ProviderHandleRetired() {
			retired++
		}
		if turnErr != nil && (backend.TurnNeverStarted() || backend.TurnClosed()) {
			t.Fatalf("run %d: a failed continuation reads never-started=%v closed=%v; it must read delivered and open, so the journal refuses it", i, backend.TurnNeverStarted(), backend.TurnClosed())
		}
		_ = backend.Close()
	}
	t.Logf("conversation retired (fail-safe) in %d of %d runs", retired, runs)
}
