package cli

// Found by the round-5 vendor-recovery verifier. The decorator here takes its
// note from noteOnCurrentSession, as run.go builds it, so the probes exercise
// the production binding rather than a copy of the old one.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

// round5Script: every request gets init + result on the conversation it was
// spawned with; a request containing "LIMIT" ends with a plan-limit result
// (vendor-closed turn) whose reset is in the past.
func round5Script(argv, requests, side string) string {
	resets := time.Now().Add(-time.Minute).Unix()
	return "#!/bin/sh\n" + probeSidParse + `printf '%s\n' "$*" >> "` + argv + `"
while IFS= read -r line; do
printf '%s\n' "$line" >> "` + requests + `"
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
case "$line" in *LIMIT*)
  case "$line" in *Continue*) ;; *)
  echo "limited request ran in $sid" >> "` + side + `"
  printf '%s\n' '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":` + fmt.Sprint(resets) + `}}'
  printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
  continue;;
  esac;;
esac
case "$line" in *Continue*) echo "continued in $sid" >> "` + side + `";; *) echo "request ran in $sid" >> "` + side + `";; esac
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
done
`
}

// round5Decorated is the real Claude adapter inside the plan decorator, with
// the note run.go gives it: it follows the agent to whatever session it is on.
func round5Decorated(t *testing.T, startup *enginetest.FakeSession, agent **engine.Agent) engine.ChatBackend {
	state := startup.ProviderStateName()
	inner, err := agentcli.NewClaudeBackendFromHandleWithOptions("claude-opus", engine.ModeCode, "high", state, state != "",
		agentcli.ExecutionOptions{BypassPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	return &verifyingBackend{inner: inner, note: noteOnCurrentSession(startup, func() *engine.Agent { return *agent }),
		explain: func(error) {}, confirm: func(context.Context) {}}
}

func round5Setup(t *testing.T) (argv, requests, side string) {
	bin := t.TempDir()
	argv, requests, side = filepath.Join(bin, "argv.log"), filepath.Join(bin, "requests.log"), filepath.Join(bin, "side.log")
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(round5Script(argv, requests, side)), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return
}

// R5-P1. run.go:364-369 binds the startup backend's note to the startup
// session object. After /new (ReplaceSession), the same backend keeps
// noting into the old session: the new session's file never learns its own
// vendor conversation, and the old session's object is told the new one's.
func TestRound5NewSessionNotesItsOwnHandle(t *testing.T) {
	_, _, side := round5Setup(t)
	sessA := enginetest.NewFakeSession("s_r5_A", "claude-opus")
	sessA.SetProviderStateName("H-A")
	var agent *engine.Agent
	agent = engine.New(engine.Options{Backend: round5Decorated(t, sessA, &agent), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sessA, Root: t.TempDir(), Out: io.Discard})
	defer agent.Close()
	if err := agent.RunTurn(context.Background(), "hello in A"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	sessB := enginetest.NewFakeSession("s_r5_B", "claude-opus")
	agent.ReplaceSession(sessB, nil)
	if err := agent.RunTurn(context.Background(), "session B work"); err != nil {
		t.Fatalf("setup B: %v", err)
	}
	lines := round4Lines(side)
	bConv := strings.TrimPrefix(lines[len(lines)-1], "request ran in ")
	t.Logf("side=%q; A file=%q B file=%q; B ran in %q; agent.Sess==sessB %v", lines, sessA.ProviderStateName(), sessB.ProviderStateName(), bConv, agent.Sess == engine.SessionPort(sessB))
	if bConv == "H-A" {
		t.Fatal("setup: B ran in A's conversation")
	}
	if sessA.ProviderStateName() != "H-A" || sessB.ProviderStateName() != bConv {
		t.Fatalf("SESSION FILE MISROUTED: after /new, A's session holds %q (want H-A) and B's holds %q (want %q, the conversation B's request ran in)",
			sessA.ProviderStateName(), sessB.ProviderStateName(), bConv)
	}
}

// R5-P1b. The consequence: session B pauses at a plan limit after the vendor
// closed the turn (rule 3: continuable). After a restart, B's backend is built
// from B's session file. The saved continuation must continue on B's
// conversation; it is refused instead, because B's file never learned it.
func TestRound5NewSessionPauseSurvivesARestart(t *testing.T) {
	argv, requests, side := round5Setup(t)
	sessA := enginetest.NewFakeSession("s_r5b_A", "claude-opus")
	sessA.SetProviderStateName("H-A")
	var first *engine.Agent
	opts := engine.Options{Backend: round5Decorated(t, sessA, &first), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sessA, Root: t.TempDir(), Out: io.Discard}
	first = engine.New(opts)
	if err := first.RunTurn(context.Background(), "hello in A"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	sessB := enginetest.NewFakeSession("s_r5b_B", "claude-opus")
	first.ReplaceSession(sessB, nil)
	var paused *engine.PausedError
	if err := first.RunTurn(context.Background(), "session B LIMIT work"); !errors.As(err, &paused) {
		t.Fatalf("setup: B not paused: %v", err)
	}
	run := sessB.RunState()
	t.Logf("B journal: state=%q confirmed=%v closed=%v inflight=%v; B file=%q A file=%q", run.Main.ProviderState, run.Main.ProviderConfirmed,
		run.Main.ProviderTurnClosed, run.Main.ProviderInFlight, sessB.ProviderStateName(), sessA.ProviderStateName())
	_ = first.Close()
	// Restart: the backend is rebuilt from B's own session file (run.go:364).
	var second *engine.Agent
	opts.Sess, opts.Backend = sessB, round5Decorated(t, sessB, &second)
	second = engine.New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	t.Logf("resume err=%v; side=%q; spawns=%d; requests=%d", err, round4Lines(side), len(round4Lines(argv)), len(round4Lines(requests)))
	if err != nil {
		t.Fatalf("CONTINUATION LOST AFTER /new + RESTART: B's vendor-closed, confirmed turn is refused: %v", err)
	}
}
