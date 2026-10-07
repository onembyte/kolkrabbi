package cli

// Found by the round-3 vendor-recovery verifier: each was red on the tree it
// reviewed (or, for the concurrency check, guards the restore it introduced).

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

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// R3-3, end to end on the real composition, in one kolk process: the session
// holds conversation H-saved (so its Claude process opens with --resume). The
// vendor runs a tool, then its plan limit closes the turn, and the process
// exits while the session waits for the reset. The resume monitor delivers the
// waiting turn: the continuation must go to H-saved, the conversation that
// holds what the vendor did, and never to a new one.
func TestRound3AnInProcessAutoResumeKeepsTheConversationWhenItsProcessExited(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	argv := filepath.Join(bin, "argv.log")
	side := filepath.Join(bin, "side-effects.log")
	resets := time.Now().Add(-time.Minute).Unix()
	script := "#!/bin/sh\n" + probeSidParse + `printf '%s\n' "$*" >> "` + argv + `"
IFS= read -r line
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"deploy"}}]}}'
  echo "deployed in $sid" >> "` + side + `"
  printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}'
  printf '%s\n' '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":` + fmt.Sprint(resets) + `}}'
  printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
  exit 0
fi
case "$line" in *Continue*) echo "continued in $sid" >> "` + side + `";; esac
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"continued"}]}}'
printf '%s\n' "{\"type\":\"result\",\"result\":\"continued\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
cat > /dev/null
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sess := enginetest.NewFakeSession("s_r3_inprocess", "claude-opus")
	sess.SetProviderStateName("H-saved")
	delivered := make(chan error, 1)
	var agent *engine.Agent
	opts := engine.Options{Backend: probeDecorated(t, sess)(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		ResumeWait: func(ctx context.Context, _ time.Duration) error {
			// The session waits for the reset; meanwhile its process exits.
			select {
			case <-time.After(time.Second):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		ProbeLimit: func(context.Context, continuity.Pause) (bool, error) { return true, nil },
		ResumeReady: func(ctx context.Context, pending string) bool {
			delivered <- agent.RunTurn(ctx, pending)
			return true
		}}
	agent = engine.New(opts)
	defer agent.Close()
	err := agent.RunTurn(context.Background(), "deploy the service")
	var paused *engine.PausedError
	if !errors.As(err, &paused) {
		t.Fatalf("setup: the plan limit did not pause: %v", err)
	}
	stop := agent.WatchPauses(context.Background())
	defer stop()
	select {
	case err = <-delivered:
	case <-time.After(20 * time.Second):
		t.Fatal("the monitor never delivered the waiting turn")
	}
	calls, _ := os.ReadFile(argv)
	done, _ := os.ReadFile(side)
	spawns := strings.Split(strings.TrimSpace(string(calls)), "\n")
	run := sess.RunState()
	t.Logf("resume err=%v; side effects:\n%s; journal state=%q phase=%s", err, done, run.Main.ProviderState, run.Phase)
	if len(spawns) < 2 {
		t.Fatalf("setup: spawns = %q", spawns)
	}
	if !strings.Contains(spawns[1], "--resume H-saved") {
		t.Fatalf("IDENTITY: the automatic continuation left H-saved (which holds the vendor's deploy) for a new conversation: %q", spawns[1])
	}
}

// R3-4. The doc: "A continuation whose process could not start therefore
// stays continuable." Same in-process setup; the session's process exited
// during the wait, and when the continuation comes the CLI cannot start at
// all (it is being updated). Nothing reached any vendor, so the journal must
// still name H-saved, confirmed and closed, and a later resume must continue
// H-saved.
func TestRound3AContinuationThatNeverArrivedStaysOnItsConversation(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	resets := time.Now().Add(-time.Minute).Unix()
	script := "#!/bin/sh\n" + probeSidParse + `IFS= read -r line
printf '%s\n' "$line" >> "` + requests + `"
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"deploy"}}]}}'
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}'
printf '%s\n' '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":` + fmt.Sprint(resets) + `}}'
printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	withClaude := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", withClaude)
	sess := enginetest.NewFakeSession("s_r3_never_arrived", "claude-opus")
	sess.SetProviderStateName("H-saved")
	agent := engine.New(engine.Options{Backend: probeDecorated(t, sess)(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
	defer agent.Close()
	var paused *engine.PausedError
	if err := agent.RunTurn(context.Background(), "deploy the service"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	time.Sleep(time.Second) // the session's process exits while it waits
	t.Setenv("PATH", t.TempDir())
	pending, ok := agent.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	resumeErr := agent.RunTurn(context.Background(), pending)
	run := sess.RunState()
	t.Logf("continuation err=%v; journal state=%q confirmed=%v closed=%v inflight=%v neverStarted=%v; session file %q",
		resumeErr, run.Main.ProviderState, run.Main.ProviderConfirmed, run.Main.ProviderTurnClosed, run.Main.ProviderInFlight, run.Main.ProviderNeverStarted, sess.ProviderStateName())
	if run.Main.ProviderState != "H-saved" || !run.Main.ProviderConfirmed || !run.Main.ProviderTurnClosed {
		t.Fatalf("RESTORE: an attempt that reached no vendor changed the journal's conversation from H-saved to %q (confirmed=%v); it is no longer continuable",
			run.Main.ProviderState, run.Main.ProviderConfirmed)
	}
}
