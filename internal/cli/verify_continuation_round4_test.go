package cli

// Found by the round-4 vendor-recovery verifier, on the real composition: the
// real Claude adapter inside the real plan decorator, a fake `claude` on PATH.

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

func round4Lines(path string) []string {
	raw, _ := os.ReadFile(path)
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// P-C1 (rebuild of R3-3 with a SIGKILL during the wait). The session holds
// H-saved; the vendor runs a tool, its plan limit closes the turn, and the
// process is killed while the session waits. The in-process monitor delivers
// the continuation: spawn 2 must resume H-saved, and the request must finish.
func TestRound4AutoResumeAfterKillDuringWait(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	argv := filepath.Join(bin, "argv.log")
	side := filepath.Join(bin, "side.log")
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
  ( sleep 0.3; kill -9 $$ ) &
  cat > /dev/null
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
	sess := enginetest.NewFakeSession("s_r4_kill_wait", "claude-opus")
	sess.SetProviderStateName("H-saved")
	delivered := make(chan error, 1)
	var agent *engine.Agent
	opts := engine.Options{Backend: probeDecorated(t, sess)(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		ResumeWait: func(ctx context.Context, _ time.Duration) error {
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
		t.Fatalf("setup: not paused: %v", err)
	}
	if run := sess.RunState(); !run.Main.ProviderTurnClosed || !run.Main.ProviderConfirmed || run.Main.ProviderState != "H-saved" || !run.Main.ProviderDelivered {
		t.Fatalf("setup: journal at pause = %+v", run.Main)
	}
	stop := agent.WatchPauses(context.Background())
	defer stop()
	select {
	case err = <-delivered:
	case <-time.After(20 * time.Second):
		t.Fatal("monitor never delivered")
	}
	spawns := round4Lines(argv)
	reqs := round4Lines(requests)
	run := sess.RunState()
	t.Logf("err=%v spawns=%d side=%q phase=%s state=%q", err, len(spawns), round4Lines(side), run.Phase, run.Main.ProviderState)
	if err != nil || len(spawns) != 2 || !strings.Contains(spawns[1], "--resume H-saved") {
		t.Fatalf("IDENTITY/RECOVERY: err=%v spawns=%q", err, spawns)
	}
	if len(reqs) != 2 || !strings.Contains(reqs[1], "Continue this unfinished request") {
		t.Fatalf("requests=%q", reqs)
	}
}

// P-C2 (N2 end to end, then recovery). The continuation's CLI is missing:
// the journal must keep H-saved, confirmed, closed. When the CLI is back, a
// /resume continues H-saved with one continuation; the original request is
// not resent and no new conversation is opened.
func TestRound4MissingCLIThenBack(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	argv := filepath.Join(bin, "argv.log")
	resets := time.Now().Add(-time.Minute).Unix()
	script := "#!/bin/sh\n" + probeSidParse + `printf '%s\n' "$*" >> "` + argv + `"
IFS= read -r line
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":` + fmt.Sprint(resets) + `}}'
  printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
  exit 0
fi
printf '%s\n' "{\"type\":\"result\",\"result\":\"continued\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
cat > /dev/null
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	withClaude := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", withClaude)
	sess := enginetest.NewFakeSession("s_r4_missing", "claude-opus")
	sess.SetProviderStateName("H-saved")
	agent := engine.New(engine.Options{Backend: probeDecorated(t, sess)(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
	defer agent.Close()
	var paused *engine.PausedError
	if err := agent.RunTurn(context.Background(), "deploy the service"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	time.Sleep(time.Second)
	t.Setenv("PATH", t.TempDir())
	pending, ok := agent.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	firstErr := agent.RunTurn(context.Background(), pending)
	run := sess.RunState()
	t.Logf("missing: err=%v state=%q confirmed=%v closed=%v inflight=%v delivered=%v neverStarted=%v recovery=%q phase=%s",
		firstErr, run.Main.ProviderState, run.Main.ProviderConfirmed, run.Main.ProviderTurnClosed, run.Main.ProviderInFlight,
		run.Main.ProviderDelivered, run.Main.ProviderNeverStarted, run.Recovery, run.Phase)
	if run.Main.ProviderState != "H-saved" || !run.Main.ProviderConfirmed || !run.Main.ProviderTurnClosed || run.Main.ProviderNeverStarted {
		t.Fatalf("N2: journal lost the conversation: %+v", run.Main)
	}
	t.Setenv("PATH", withClaude)
	pending, ok = agent.Resume()
	if !ok {
		t.Fatal("nothing to resume after the CLI came back")
	}
	secondErr := agent.RunTurn(context.Background(), pending)
	spawns := round4Lines(argv)
	reqs := round4Lines(requests)
	t.Logf("back: err=%v spawns=%q requests=%d", secondErr, spawns, len(reqs))
	if secondErr != nil || len(reqs) != 2 || !strings.Contains(reqs[1], "Continue this unfinished request") || !strings.Contains(spawns[len(spawns)-1], "--resume H-saved") {
		t.Fatalf("RECOVERY after the CLI came back: err=%v requests=%q spawns=%q", secondErr, reqs, spawns)
	}
}

// P-C3 (NEW, C3). Two ordinary requests in one session, nothing interrupted.
// The second is a fresh request, not a continuation: it must not tell the
// vendor that "the previous attempt stopped before this request finished".
func TestRound4AFreshRequestIsNotAContinuation(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	script := "#!/bin/sh\n" + probeSidParse + `while IFS= read -r line; do
printf '%s\n' "$line" >> "` + requests + `"
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' "{\"type\":\"result\",\"result\":\"done\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
done
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sess := enginetest.NewFakeSession("s_r4_fresh", "claude-opus")
	agent := engine.New(engine.Options{Backend: probeDecorated(t, sess)(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
	defer agent.Close()
	if err := agent.RunTurn(context.Background(), "first request"); err != nil {
		t.Fatal(err)
	}
	if err := agent.RunTurn(context.Background(), "second request"); err != nil {
		t.Fatal(err)
	}
	reqs := round4Lines(requests)
	for i, r := range reqs {
		t.Logf("request %d: ...%s", i+1, r[max(0, len(r)-220):])
	}
	last := ""
	for _, r := range reqs {
		if strings.Contains(r, `USER:\nsecond request`) {
			last = r
		}
	}
	if last == "" {
		t.Fatalf("setup: the second request was never sent")
	}
	if strings.Contains(last, "previous attempt stopped") {
		t.Fatalf("CONTINUATION TEXT ON A FRESH REQUEST: the second, uninterrupted request was sent as a continuation")
	}
}

// claudeScriptC4 builds the script for P-C4. exitDuringWait decides whether
// the first process is gone when the continuation comes (the retry path) or
// still alive (the first-process path).
func claudeScriptC4(requests, argv, side string, resets int64, exitDuringWait bool) string {
	afterLimit := ":"
	if exitDuringWait {
		afterLimit = "exit 0"
	}
	return "#!/bin/sh\n" + probeSidParse + `printf '%s\n' "$*" >> "` + argv + `"
while IFS= read -r line; do
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"deploy"}}]}}'
  echo "deploy done in $sid" >> "` + side + `"
  printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}'
  printf '%s\n' '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":` + fmt.Sprint(resets) + `}}'
  printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
  ` + afterLimit + `
  continue
fi
if [ "$n" = "2" ]; then
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"migrate-db"}}]}}'
  echo "migrate-db STARTED (unfinished) in $sid" >> "` + side + `"
  sleep 0.2
  kill -9 $$
fi
echo "request $n in $sid" >> "` + side + `"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
done
`
}

// P-C4 (NEW, C1 end to end). The continuation runs on the adapter's retry
// process (the first one exited during the wait) and that process is killed
// mid-tool. The /resume is refused (correct), the person discards, and asks
// for something new. The killed conversation must be retired, as it is when
// the first process is the one killed; otherwise the next request resumes the
// conversation whose turn was left unfinished.
func TestRound4KilledContinuationOnTheRetryProcessIsRetired(t *testing.T) {
	for _, c := range []struct {
		name           string
		exitDuringWait bool
	}{{"first-process-killed(control)", false}, {"retry-process-killed", true}} {
		t.Run(c.name, func(t *testing.T) {
			bin := t.TempDir()
			requests := filepath.Join(bin, "requests.log")
			argv := filepath.Join(bin, "argv.log")
			side := filepath.Join(bin, "side.log")
			resets := time.Now().Add(-time.Minute).Unix()
			if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(claudeScriptC4(requests, argv, side, resets, c.exitDuringWait)), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			sess := enginetest.NewFakeSession("s_r4_c4", "claude-opus")
			sess.SetProviderStateName("H-saved")
			agent := engine.New(engine.Options{Backend: probeDecorated(t, sess)(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
				Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
			defer agent.Close()
			var paused *engine.PausedError
			if err := agent.RunTurn(context.Background(), "deploy the service"); !errors.As(err, &paused) {
				t.Fatalf("setup: not paused: %v", err)
			}
			time.Sleep(time.Second)
			pending, ok := agent.Resume()
			if !ok {
				t.Fatal("nothing to resume")
			}
			contErr := agent.RunTurn(context.Background(), pending)
			run := sess.RunState()
			t.Logf("continuation err=%v; journal state=%q closed=%v inflight=%v delivered=%v; session file %q",
				contErr, run.Main.ProviderState, run.Main.ProviderTurnClosed, run.Main.ProviderInFlight, run.Main.ProviderDelivered, sess.ProviderStateName())
			if contErr == nil {
				t.Fatal("setup: the killed continuation succeeded")
			}
			pending, ok = agent.Resume()
			if ok {
				refuse := agent.RunTurn(context.Background(), pending)
				t.Logf("resume after kill: %v", refuse)
				if refuse == nil || !strings.Contains(refuse.Error(), "repeat what the vendor already did") {
					t.Fatalf("REPLAY: a killed continuation was resumed: %v", refuse)
				}
			}
			if !agent.DiscardPending() {
				t.Fatal("setup: nothing to discard")
			}
			nextErr := agent.RunTurn(context.Background(), "show me the status")
			spawns := round4Lines(argv)
			t.Logf("next err=%v; side effects=%q; spawns:", nextErr, round4Lines(side))
			for i, s := range spawns {
				f := strings.Fields(s)
				t.Logf("  spawn %d: %s", i+1, strings.Join(f[len(f)-4:], " "))
			}
			last := spawns[len(spawns)-1]
			if strings.Contains(last, "--resume H-saved") {
				t.Fatalf("HARD-EXIT NOT RETIRED: after the continuation's process was killed mid-tool, the next request resumed H-saved, the conversation whose turn was left unfinished: %q", last)
			}
		})
	}
}

// P-C5 (C5, pre-existing?). The first process is killed mid-tool: retired in
// memory. After a restart, the backend is rebuilt from the session file. Does
// the retirement survive, or does the next request resume H-saved?
func TestRound4RetirementSurvivesARestart(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	argv := filepath.Join(bin, "argv.log")
	side := filepath.Join(bin, "side.log")
	script := "#!/bin/sh\n" + probeSidParse + `printf '%s\n' "$*" >> "` + argv + `"
while IFS= read -r line; do
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "2" ]; then
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"migrate-db"}}]}}'
  echo "migrate-db STARTED (unfinished) in $sid" >> "` + side + `"
  sleep 0.2
  kill -9 $$
fi
echo "request $n in $sid" >> "` + side + `"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
done
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sess := enginetest.NewFakeSession("s_r4_c5", "claude-opus")
	opts := engine.Options{Backend: probeDecorated(t, sess)(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard}
	first := engine.New(opts)
	if err := first.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h := sess.ProviderStateName()
	killErr := first.RunTurn(context.Background(), "migrate the database")
	_ = first.Close()
	t.Logf("kill err=%v; session file handle %q (first conversation %q); journal %q", killErr, sess.ProviderStateName(), h, sess.RunState().Main.ProviderState)
	opts.Backend = probeDecorated(t, sess)()
	second := engine.New(opts)
	defer second.Close()
	if !second.DiscardPending() {
		t.Fatal("setup: nothing to discard")
	}
	nextErr := second.RunTurn(context.Background(), "show me the status")
	spawns := round4Lines(argv)
	last := spawns[len(spawns)-1]
	t.Logf("next err=%v side=%q last spawn ...%s", nextErr, round4Lines(side), last[len(last)-60:])
	if strings.Contains(last, "--resume "+h) {
		t.Fatalf("RETIREMENT LOST ON RESTART: the killed conversation %s is resumed after a restart: %q", h, last)
	}
}

// P-C6 (pre-existing?). Session A is paused at a plan limit on conversation
// H-A, with a continuation pending. The person runs /new (ReplaceSession) and
// works in session B. B's requests must not land in H-A, the conversation
// where A's saved work waits; and B's handle must be noted in B, not in A.
func TestRound4NewSessionDoesNotUseThePausedSessionsConversation(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	argv := filepath.Join(bin, "argv.log")
	side := filepath.Join(bin, "side.log")
	resets := time.Now().Add(time.Hour).Unix()
	script := "#!/bin/sh\n" + probeSidParse + `printf '%s\n' "$*" >> "` + argv + `"
while IFS= read -r line; do
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":` + fmt.Sprint(resets) + `}}'
  printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
  continue
fi
case "$line" in *"session B work"*) echo "session B's request ran in $sid" >> "` + side + `";; esac
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
done
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sessA := enginetest.NewFakeSession("s_r4_A", "claude-opus")
	sessA.SetProviderStateName("H-A")
	agent := engine.New(engine.Options{Backend: probeDecorated(t, sessA)(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sessA, Root: t.TempDir(), Out: io.Discard})
	defer agent.Close()
	var paused *engine.PausedError
	if err := agent.RunTurn(context.Background(), "deploy the service"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	sessB := enginetest.NewFakeSession("s_r4_B", "claude-opus")
	agent.ReplaceSession(sessB, nil)
	errB := agent.RunTurn(context.Background(), "session B work")
	spawns := round4Lines(argv)
	t.Logf("B err=%v side=%q; A file handle %q; B file handle %q; spawns=%d", errB, round4Lines(side), sessA.ProviderStateName(), sessB.ProviderStateName(), len(spawns))
	for _, s := range round4Lines(side) {
		if strings.Contains(s, "H-A") {
			t.Fatalf("WRONG CONVERSATION: after /new, session B's request ran in H-A, the conversation holding session A's paused work: %q", s)
		}
	}
}
