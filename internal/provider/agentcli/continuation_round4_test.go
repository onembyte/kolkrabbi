package agentcli

// Found by the round-4 vendor-recovery verifier: independent rebuilds of the
// round-3 scenarios plus the new findings, each red on the tree it reviewed
// unless marked as a rebuild or a control.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func round4Spawns(t *testing.T, argv string) []string {
	t.Helper()
	raw, _ := os.ReadFile(argv)
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func round4WaitExit(t *testing.T, backend *ClaudeBackend) {
	t.Helper()
	backend.mu.Lock()
	session := backend.session
	backend.mu.Unlock()
	if session == nil {
		t.Fatal("setup: no live session")
	}
	exited := session.process.(interface{ Exited() bool })
	for deadline := time.Now().Add(5 * time.Second); !exited.Exited() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if !exited.Exited() {
		t.Fatal("setup: the child never exited")
	}
}

const round4Sid = `sid=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--session-id" ] || [ "$prev" = "--resume" ]; then sid="$a"; fi
  prev="$a"
done
`

// P-A1 (rebuild of N1, resumed). A process opened with --resume H-saved
// answers a turn and exits idle. The next turn must go to H-saved, and the
// backend must still report H-saved as confirmed and the turn as delivered
// and closed.
func TestRound4IdleResumedExitKeepsHandle(t *testing.T) {
	path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok $n\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then exit 0; fi
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "one"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	round4WaitExit(t, backend)
	msg, err := oneTurn(t, backend, "continue")
	spawns := round4Spawns(t, argv)
	t.Logf("spawns=%q msg=%q err=%v handle=%q confirmed=%v neverStarted=%v closed=%v", spawns, msg.Content, err,
		backend.ProviderHandle(), backend.ProviderHandleConfirmed(), backend.TurnNeverStarted(), backend.TurnClosed())
	if len(spawns) != 2 || !strings.Contains(spawns[1], "--resume H-saved") {
		t.Fatalf("IDENTITY: spawn 2 did not resume H-saved: %q", spawns)
	}
	if err != nil || backend.ProviderHandle() != "H-saved" || !backend.ProviderHandleConfirmed() || backend.TurnNeverStarted() || !backend.TurnClosed() {
		t.Fatalf("facts wrong after retry")
	}
}

// P-A2 (rebuild of N1, killed idle). The process (itself opened with
// --resume) is SIGKILLed between turns. Spawn 2 must resume H-saved.
func TestRound4IdleResumedKilledKeepsHandle(t *testing.T) {
	path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok $n\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then sleep 0.2; kill -9 $$; fi
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "one"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	round4WaitExit(t, backend)
	if !backend.session.HardExit() {
		t.Fatal("setup: not a hard exit")
	}
	msg, err := oneTurn(t, backend, "continue")
	spawns := round4Spawns(t, argv)
	t.Logf("spawns=%q msg=%q err=%v handle=%q confirmed=%v", spawns, msg.Content, err, backend.ProviderHandle(), backend.ProviderHandleConfirmed())
	if len(spawns) != 2 || !strings.Contains(spawns[1], "--resume H-saved") || err != nil {
		t.Fatalf("IDENTITY: spawn 2 did not resume H-saved: %q", spawns)
	}
}

// P-A3 (rebuild of N2 at the adapter). The process exited idle; the
// replacement cannot be started (binary gone). Nothing was delivered, the
// handle and its confirmation are unchanged.
func TestRound4ReplacementCannotStartKeepsHandle(t *testing.T) {
	path, _ := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
exit 0
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "one"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	round4WaitExit(t, backend)
	_ = os.Remove(path)
	_, err = oneTurn(t, backend, "continue")
	t.Logf("err=%v handle=%q confirmed=%v neverStarted=%v", err, backend.ProviderHandle(), backend.ProviderHandleConfirmed(), backend.TurnNeverStarted())
	if err == nil || !backend.TurnNeverStarted() || backend.ProviderHandle() != "H-saved" || !backend.ProviderHandleConfirmed() {
		t.Fatal("N2 at adapter: facts lost")
	}
}

// P-A4 (rebuild). Codex whose binary cannot start: never started, thread kept.
func TestRound4CodexCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	backend, err := NewCodexBackendFromHandleWithOptions("gpt-5.6-sol", "code", "", "th-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, runErr := backend.StreamChat(t.Context(), "gpt-5.6-sol", []provider.Message{{Role: "user", Content: "continue"}}, nil, nil)
	if runErr == nil || !backend.TurnNeverStarted() || backend.ProviderHandle() != "th-saved" || !backend.ProviderHandleConfirmed() {
		t.Fatalf("codex: err=%v neverStarted=%v handle=%q", runErr, backend.TurnNeverStarted(), backend.ProviderHandle())
	}
}

// P-A4b. Codex that DID start and exited 127 without output: must count as
// delivered (NotStartedError must not leak from a process that ran).
func TestRound4CodexStartedAndFailedIsDelivered(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\ncat >/dev/null\nexit 127\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	backend, err := NewCodexBackendFromHandleWithOptions("gpt-5.6-sol", "code", "", "th-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, runErr := backend.StreamChat(t.Context(), "gpt-5.6-sol", []provider.Message{{Role: "user", Content: "continue"}}, nil, nil)
	if runErr == nil || backend.TurnNeverStarted() {
		t.Fatalf("LEAK: a codex that ran is proven unstarted: err=%v", runErr)
	}
}

// P-A4c. Claude one-shot path: no binary -> never started; a binary that ran
// and failed -> delivered.
func TestRound4ClaudeOneShotProofs(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		backend := &ClaudeBackend{}
		_, err := oneTurn(t, backend, "hi")
		if err == nil || !backend.TurnNeverStarted() {
			t.Fatalf("one-shot missing binary: err=%v neverStarted=%v", err, backend.TurnNeverStarted())
		}
	})
	t.Run("ran", func(t *testing.T) {
		bin := t.TempDir()
		if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\ncat >/dev/null\nexit 2\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		backend := &ClaudeBackend{}
		_, err := oneTurn(t, backend, "hi")
		if err == nil || backend.TurnNeverStarted() {
			t.Fatalf("LEAK: one-shot that ran is unstarted: err=%v", err)
		}
	})
	t.Run("cancelled-before-start", func(t *testing.T) {
		bin := t.TempDir()
		marker := filepath.Join(bin, "ran")
		if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\ntouch "+marker+"\ncat >/dev/null\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		backend := &ClaudeBackend{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := backend.StreamChat(ctx, "opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
		time.Sleep(100 * time.Millisecond)
		_, statErr := os.Stat(marker)
		t.Logf("err=%v neverStarted=%v ran=%v", err, backend.TurnNeverStarted(), statErr == nil)
		if statErr == nil && backend.TurnNeverStarted() {
			t.Fatal("LEAK: the process ran but the turn is proven unstarted")
		}
	})
}

// P-A5 (NEW, C1). The retry's replacement process is killed mid-turn after
// it confirmed the conversation and started a tool. §2.5 (backend.go) says a
// killed delivered turn is retired, because the vendor continues an
// unfinished turn on the next --resume. Does that hold for the replacement?
func TestRound4ReplacementKilledMidTurnRetiresHandle(t *testing.T) {
	bin := t.TempDir()
	side := filepath.Join(bin, "side.log")
	path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "2" ]; then
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_9","name":"Bash","input":{"command":"rm -rf build"}}]}}'
  echo "spawn 2 started rm in $sid" >> "`+side+`"
  sleep 0.2
  kill -9 $$
fi
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok $n\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then exit 0; fi
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "one"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	round4WaitExit(t, backend)
	_, secondErr := oneTurn(t, backend, "two")
	afterKill := backend.ProviderHandle()
	_, thirdErr := oneTurn(t, backend, "three")
	spawns := round4Spawns(t, argv)
	t.Logf("second err=%v; handle after kill=%q; third err=%v; spawns=%q", secondErr, afterKill, thirdErr, spawns)
	if len(spawns) < 3 {
		t.Fatalf("setup: spawns=%q", spawns)
	}
	if !strings.Contains(spawns[1], "--resume H-saved") {
		t.Fatalf("setup: retry not on H-saved: %q", spawns[1])
	}
	if strings.Contains(spawns[2], "--resume H-saved") {
		t.Fatalf("HARD-EXIT NOT RETIRED: the retry's process was killed mid-turn (tool started, no result), yet the next turn resumes the same conversation: %q (handle after kill %q)", spawns[2], afterKill)
	}
}

// P-A5 control: the same kill on the first (non-retry) process retires it.
func TestRound4FirstProcessKilledMidTurnRetiresHandle(t *testing.T) {
	path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_9","name":"Bash","input":{"command":"rm -rf build"}}]}}'
  sleep 0.2
  kill -9 $$
fi
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok $n\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	_, firstErr := oneTurn(t, backend, "one")
	_, secondErr := oneTurn(t, backend, "two")
	spawns := round4Spawns(t, argv)
	t.Logf("first err=%v second err=%v spawns=%q", firstErr, secondErr, spawns)
	if len(spawns) < 2 || strings.Contains(spawns[1], "--resume H-saved") {
		t.Fatalf("control: first-process kill not retired: %q", spawns)
	}
}

// P-A6 (NEW, C2). The vendor names a conversation different from the handle
// kolk passed (e.g. a resume that forks). ProviderHandle reports the vendor's
// name, which is what the journal and the session file record. When the
// process exits idle, the retry respawns with b.handle instead.
func TestRound4RetryUsesTheVendorsOwnName(t *testing.T) {
	path, argv := fakeClaudeScript(t, `IFS= read -r line
printf '%s\n' '{"type":"system","subtype":"init","model":"opus","session_id":"H-vendor"}'
printf '%s\n' '{"type":"result","result":"ok","subtype":"success","session_id":"H-vendor"}'
if [ "$n" = "1" ]; then exit 0; fi
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-passed", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "one"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	recorded := backend.ProviderHandle()
	round4WaitExit(t, backend)
	betweenTurns := backend.ProviderHandle()
	_, _ = oneTurn(t, backend, "continue")
	spawns := round4Spawns(t, argv)
	t.Logf("recorded=%q betweenTurns=%q spawns=%q", recorded, betweenTurns, spawns)
	if recorded != "H-vendor" {
		t.Fatalf("setup: recorded %q", recorded)
	}
	if !strings.Contains(spawns[1], "--resume "+recorded) {
		t.Fatalf("IDENTITY: the conversation recorded (journal, session file) is %q, but the retry resumed: %q", recorded, spawns[1])
	}
}

// P-A7 (NEW, C4). Copilot resets its closure flag after the prompt is built,
// so a prompt error reports the previous turn's closure.
func TestRound4CopilotPromptErrorReportsStaleClosure2(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "copilot"), []byte(`#!/bin/sh
printf '%s\n' '{"type":"result","sessionId":"cp-1","exitCode":0}'
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	backend, err := NewCopilotBackendWithOptions("auto", "code", "", "", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = backend.StreamChat(t.Context(), "auto", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
	if !backend.TurnClosed() {
		t.Fatal("setup: first turn not closed")
	}
	_, _, perr := backend.StreamChat(t.Context(), "auto", []provider.Message{{Role: "user", Content: ""}}, nil, nil)
	if perr == nil {
		t.Fatal("setup: empty prompt accepted")
	}
	if backend.TurnClosed() {
		t.Fatalf("STALE: a turn that failed building its prompt reports the previous turn's closure (err=%v)", perr)
	}
}

// P-A8. Dead resume where the process takes stdin before dying: the turn
// counts as delivered (conservative), the handle is retired, and the next
// turn is fresh (no wedge).
func TestRound4DeadResumeAfterQueue(t *testing.T) {
	path, argv := fakeClaudeScript(t, `if [ "$n" = "1" ]; then
  IFS= read -r line
  echo "No conversation found with session ID" >&2
  exit 1
fi
IFS= read -r line
printf '%s\n' '{"type":"result","result":"fresh","subtype":"success","session_id":"new"}'
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "dead-h", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	_, firstErr := oneTurn(t, backend, "one")
	delivered := !backend.TurnNeverStarted()
	msg, secondErr := oneTurn(t, backend, "two")
	spawns := round4Spawns(t, argv)
	t.Logf("first err=%v delivered=%v; second %q err=%v; spawns=%q", firstErr, delivered, msg.Content, secondErr, spawns)
	if secondErr != nil || strings.Contains(spawns[len(spawns)-1], "dead-h") {
		t.Fatalf("WEDGE: %q", spawns)
	}
}

// P-A9 (pre-existing?, C6). The person cancels a turn while the vendor runs a
// tool and never answers. The drain gives up, the session is dropped, and
// Close kills the child after its grace: a killed delivered turn. The handle
// was judged before the kill, so the next turn resumes the conversation whose
// turn was left unfinished.
func TestRound4CancelledTurnKilledByCloseIsNotRetired(t *testing.T) {
	bin := t.TempDir()
	side := filepath.Join(bin, "side.log")
	path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_9","name":"Bash","input":{"command":"long-migration"}}]}}'
  echo "long-migration started in $sid" >> "`+side+`"
  trap '' INT TERM
  sleep 30
fi
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok $n\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	_, _, firstErr := backend.StreamChat(ctx, "opus", []provider.Message{{Role: "user", Content: "migrate"}}, nil, func(tok string) {
		if strings.Contains(tok, "long-migration") {
			cancel()
		}
	})
	first := backend.ProviderHandle()
	_, secondErr := oneTurn(t, backend, "something else")
	spawns := round4Spawns(t, argv)
	t.Logf("first err=%v after %s; handle after cancel=%q; second err=%v; spawns=%d", firstErr, time.Since(start).Round(time.Millisecond), first, secondErr, len(spawns))
	if len(spawns) >= 2 && first != "" && strings.Contains(spawns[1], "--resume "+first) {
		t.Fatalf("KILLED-TURN RESUMED: the cancelled turn's process was killed on Close while its tool ran, and the next turn resumes %s: %q", first, spawns[1])
	}
}

// P-A10 (C7). A process killed right after the vendor's result frame closed
// the turn (e.g. a plan limit): the adapter retires the conversation although
// the vendor closed the turn, so the closed turn cannot be continued.
func TestRound4KillAfterResultFrameRetiresAClosedTurn(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return &fakeLineProcess{hardExit: true, lines: [][]byte{
			[]byte(`{"type":"system","subtype":"init","model":"opus","session_id":"H-saved"}`),
			[]byte(`{"type":"result","subtype":"success","is_error":true,"result":"You've hit your limit","session_id":"H-saved"}`),
		}}, nil
	}
	_, _, turnErr := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "deploy"}}, nil, nil)
	t.Logf("err=%v closed=%v delivered=%v handle=%q confirmed=%v", turnErr, backend.TurnClosed(), !backend.TurnNeverStarted(), backend.ProviderHandle(), backend.ProviderHandleConfirmed())
	if backend.TurnClosed() && (backend.ProviderHandle() != "H-saved" || !backend.ProviderHandleConfirmed()) {
		t.Fatalf("CLOSED TURN RETIRED: the vendor closed the turn, but a hard exit after it retired the conversation; the journal can no longer continue it")
	}
}

// P-A11 (kills V1). Two turns on one live process: the first closes, the
// second runs a tool and the process exits before its result. The second
// turn must read open, whatever the first one said.
func TestRound4SecondTurnOnTheSameProcessIsNotClosedByTheFirst(t *testing.T) {
	path, _ := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' "{\"type\":\"result\",\"result\":\"one\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
IFS= read -r line
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"deploy"}}]}}'
exit 3
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "one"); err != nil || !backend.TurnClosed() {
		t.Fatalf("setup: %v closed=%v", err, backend.TurnClosed())
	}
	_, secondErr := oneTurn(t, backend, "two")
	t.Logf("second err=%v closed=%v delivered=%v", secondErr, backend.TurnClosed(), !backend.TurnNeverStarted())
	if secondErr == nil || backend.TurnNeverStarted() {
		t.Fatal("setup: second turn")
	}
	if backend.TurnClosed() {
		t.Fatal("STALE CLOSURE: a turn the vendor left open (tool started, process exited) reads closed because the previous turn on the same process closed")
	}
}

// P-A12, kept as a watchpoint. The vendor prints one more frame after its
// result, then exits idle. The reader is parked handing that frame over, so
// the process does not yet read as exited and the continuation counts as
// delivered: it fails, and the journal refuses it rather than continuing.
// That is safe. What must never happen is the continuation reaching another
// conversation.
func TestRound4TrailingFrameAfterResultNeverMovesTheContinuation(t *testing.T) {
	path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok $n\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"system","subtype":"status","status":"idle"}'
  exit 0
fi
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "one"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	msg, err := oneTurn(t, backend, "continue")
	spawns := round4Spawns(t, argv)
	t.Logf("msg=%q err=%v delivered=%v closed=%v spawns=%q", msg.Content, err, !backend.TurnNeverStarted(), backend.TurnClosed(), spawns)
	for i, spawn := range spawns {
		if !strings.Contains(spawn, "--resume H-saved") {
			t.Fatalf("spawn %d %q left the saved conversation", i+1, spawn)
		}
	}
	if err != nil && (backend.TurnNeverStarted() || backend.TurnClosed()) {
		t.Fatalf("a failed continuation reads never-started=%v closed=%v; it must read delivered and open, so the journal refuses it", backend.TurnNeverStarted(), backend.TurnClosed())
	}
}

// P-A13 (kills V4). The process exited idle; the next turn is cancelled
// before it is sent. No replacement may be spawned and handed the prompt.
func TestRound4CancelledTurnIsNotRetriedOnAFreshProcess(t *testing.T) {
	path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok $n\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then exit 0; fi
cat > /dev/null
`)
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = realStart(path)
	defer backend.Close()
	if _, err := oneTurn(t, backend, "one"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	round4WaitExit(t, backend)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _ = backend.StreamChat(ctx, "opus", []provider.Message{{Role: "user", Content: "withdrawn"}}, nil, nil)
	if spawns := round4Spawns(t, argv); len(spawns) != 1 {
		t.Fatalf("CANCELLED TURN SENT: a withdrawn turn was handed to a fresh process: %d spawns", len(spawns))
	}
}
