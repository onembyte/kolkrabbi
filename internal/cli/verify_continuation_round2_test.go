package cli

// Verifier v3 probes (read-only overlay), end to end on the real composition:
// a real Claude adapter inside the real plan decorator drives a fake claude.

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
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

const probeSidParse = `sid=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--session-id" ] || [ "$prev" = "--resume" ]; then sid="$a"; fi
  prev="$a"
done
`

func probeDecorated(t *testing.T, sess *enginetest.FakeSession) func() engine.ChatBackend {
	return func() engine.ChatBackend {
		state := sess.ProviderStateName()
		inner, err := agentcli.NewClaudeBackendFromHandleWithOptions("claude-opus", engine.ModeCode, "high", state, state != "",
			agentcli.ExecutionOptions{BypassPermissions: true})
		if err != nil {
			t.Fatal(err)
		}
		return &verifyingBackend{inner: inner, note: sess.SetProviderStateName, explain: func(error) {}, confirm: func(context.Context) {}}
	}
}

// P3 end to end. A realistic Claude turn (init, a tool with a side effect,
// its result, a success result frame) is preceded by one non-JSON stdout line
// (a version-manager shim notice). The adapter abandons the turn at that line
// and drains the rest: closure and the handle are kept, the tool is not. The
// journal then says no vendor tool was reported. When a later continuation's
// process cannot start (positively never delivered), rule 1 starts the request
// over, and the vendor is asked to do again what it already did.
func TestRound2ADrainedVendorActionIsNeverStartedOver(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	side := filepath.Join(bin, "side-effects.log")
	script := "#!/bin/sh\n" + probeSidParse + `IFS= read -r line
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
if [ "$n" = "1" ]; then
  echo "shim: using node v22.1.0"
  printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"deploy"}}]}}'
  echo "deployed by $sid (request 1)" >> "` + side + `"
  printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"deployed"}]}}'
  printf '%s\n' "{\"type\":\"result\",\"result\":\"deployed\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
  cat > /dev/null
  exit 0
fi
echo "deployed by $sid (request $n)" >> "` + side + `"
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"deployed"}]}}'
printf '%s\n' "{\"type\":\"result\",\"result\":\"deployed\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
cat > /dev/null
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := os.Getenv("PATH")
	withClaude := bin + string(os.PathListSeparator) + orig
	t.Setenv("PATH", withClaude)

	sess := enginetest.NewFakeSession("s_probe_drain", "claude-opus")
	root := t.TempDir()
	decorated := probeDecorated(t, sess)
	opts := engine.Options{Backend: decorated(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard}
	first := engine.New(opts)
	firstErr := first.RunTurn(context.Background(), "deploy the service")
	_ = first.Close()
	run := sess.RunState()
	if firstErr == nil || run == nil {
		t.Fatalf("setup: turn 1 err=%v run=%v", firstErr, run)
	}
	t.Logf("turn 1: err=%v closed=%v confirmed=%v tools=%+v", firstErr, run.Main.ProviderTurnClosed, run.Main.ProviderConfirmed, run.Main.VendorTools)

	// Resume 1: rule 3 admits the closed turn; the continuation's process
	// cannot start (the CLI is momentarily absent), a positive never-started.
	t.Setenv("PATH", t.TempDir())
	opts.Backend = decorated()
	second := engine.New(opts)
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing offered for resume 1")
	}
	secondErr := second.RunTurn(context.Background(), pending)
	_ = second.Close()
	run = sess.RunState()
	t.Logf("resume 1: err=%v neverStarted=%v inflight=%v state=%q", secondErr, run.Main.ProviderNeverStarted, run.Main.ProviderInFlight, run.Main.ProviderState)

	// Resume 2, with the CLI back.
	t.Setenv("PATH", withClaude)
	opts.Backend = decorated()
	third := engine.New(opts)
	defer third.Close()
	pending, ok = third.Resume()
	if !ok {
		t.Fatal("nothing offered for resume 2")
	}
	thirdErr := third.RunTurn(context.Background(), pending)
	sent, _ := os.ReadFile(requests)
	done, _ := os.ReadFile(side)
	lines := strings.Split(strings.TrimSpace(string(sent)), "\n")
	if len(lines) > 1 && !strings.Contains(lines[len(lines)-1], "Continue") {
		t.Fatalf("REPLAY: the vendor had already acted (%q); resume 2 (err=%v) started the request over:\n%s",
			strings.TrimSpace(string(done)), thirdErr, lines[len(lines)-1])
	}
	t.Logf("resume 2: err=%v requests=%d", thirdErr, len(lines))
}

// Item 3, the positive path, end to end and automatic: a Claude plan limit
// (rate_limit_event rejected, then the vendor's error result frame) pauses
// the session; after a restart the resume monitor delivers the waiting turn
// by itself, and it continues the same conversation with --resume and a
// continuation message.
func TestRound2APlanLimitPausesAndAutoResumesTheSameConversation(t *testing.T) {
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
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"step one"}}]}}'
  printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}'
  printf '%s\n' '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":` + fmt.Sprint(resets) + `}}'
  printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
  cat > /dev/null
  exit 0
fi
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"continued"}]}}'
printf '%s\n' "{\"type\":\"result\",\"result\":\"continued\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
cat > /dev/null
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sess := enginetest.NewFakeSession("s_probe_plan_limit", "claude-opus")
	root := t.TempDir()
	decorated := probeDecorated(t, sess)
	opts := engine.Options{Backend: decorated(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard}
	first := engine.New(opts)
	err := first.RunTurn(context.Background(), "do the long job")
	_ = first.Close()
	var paused *engine.PausedError
	if !errors.As(err, &paused) {
		t.Fatalf("the plan limit did not pause: %v", err)
	}
	handle := sess.ProviderStateName()
	if run := sess.RunState(); run == nil || !run.Main.ProviderTurnClosed || !run.Main.ProviderConfirmed || handle == "" {
		t.Fatalf("journal main = %+v, handle %q", run.Main, handle)
	}

	delivered := make(chan error, 1)
	var second *engine.Agent
	opts.Backend = decorated()
	opts.ResumeWait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	opts.ProbeLimit = func(context.Context, continuity.Pause) (bool, error) { return true, nil }
	opts.ResumeReady = func(ctx context.Context, pending string) bool {
		delivered <- second.RunTurn(ctx, pending)
		return true
	}
	second = engine.New(opts)
	stop := second.WatchPauses(context.Background())
	defer func() { stop(); _ = second.Close() }()
	select {
	case err := <-delivered:
		if err != nil {
			t.Fatalf("the automatic resume failed: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the monitor never delivered the waiting turn")
	}
	sent, _ := os.ReadFile(requests)
	calls, _ := os.ReadFile(argv)
	lines := strings.Split(strings.TrimSpace(string(sent)), "\n")
	spawns := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "Continue this unfinished request") {
		t.Fatalf("requests = %q; want the original and one continuation", lines)
	}
	if len(spawns) != 2 || !strings.Contains(spawns[1], "--resume "+handle) {
		t.Fatalf("spawns = %q; want the second to resume %s", spawns, handle)
	}
}
