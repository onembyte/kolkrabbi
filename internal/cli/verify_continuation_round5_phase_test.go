package cli

// Found by the round-5 vendor-recovery verifier: an agent-mode planner turn on
// the real Claude adapter inside the plan decorator, stopped by a plan limit
// after the vendor ran a tool and closed the turn, was sent again verbatim.

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
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

type round5Child struct{}

func (round5Child) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{Role: "assistant", Content: "task done"}, provider.Meta{}, nil
}

func TestRound5AgentPlannerTurnIsNotResentOnTheRealAdapter(t *testing.T) {
	bin := t.TempDir()
	requests, argv, side := filepath.Join(bin, "requests.log"), filepath.Join(bin, "argv.log"), filepath.Join(bin, "side.log")
	resets := time.Now().Add(-time.Minute).Unix()
	script := "#!/bin/sh\n" + probeSidParse + `printf '%s\n' "$*" >> "` + argv + `"
while IFS= read -r line; do
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
case "$line" in *Decompose*)
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_p'$n'","name":"Bash","input":{"command":"git stash"}}]}}'
  echo "planner turn $n ran git stash in $sid" >> "` + side + `"
  printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_p'$n'","content":"ok"}]}}'
  if [ "$n" = "1" ]; then
    printf '%s\n' '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":` + fmt.Sprint(resets) + `}}'
    printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
    continue
  fi
  printf '%s\n' '{"type":"result","subtype":"success","result":"[{\"title\":\"first\",\"kind\":\"explain\"},{\"title\":\"second\",\"kind\":\"explain\"}]","session_id":"'"$sid"'"}'
  continue;;
esac
printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"all done\",\"session_id\":\"$sid\"}"
done
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sess := enginetest.NewFakeSession("s_r5_agent_plan", "claude-opus")
	sess.SetProviderStateName("H-saved")
	build := func() engine.ChatBackend {
		state := sess.ProviderStateName()
		inner, err := agentcli.NewClaudeBackendFromHandleWithOptions("claude-opus", engine.ModeAgent, "high", state, state != "",
			agentcli.ExecutionOptions{BypassPermissions: true})
		if err != nil {
			t.Fatal(err)
		}
		return &verifyingBackend{inner: inner, note: sess.SetProviderStateName, explain: func(error) {}, confirm: func(context.Context) {}}
	}
	opts := engine.Options{Backend: build(), Model: "claude-opus", Mode: engine.ModeAgent, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
		SubagentBackend: func(context.Context, string, string, string, engine.SubagentCapabilities) (engine.ChatBackend, error) {
			return round5Child{}, nil
		}}
	first := engine.New(opts)
	var paused *engine.PausedError
	if err := first.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	run := sess.RunState()
	t.Logf("journal: phase=%s state=%q confirmed=%v closed=%v inflight=%v tools=%+v", run.Phase, run.Main.ProviderState, run.Main.ProviderConfirmed,
		run.Main.ProviderTurnClosed, run.Main.ProviderInFlight, run.Main.VendorTools)
	_ = first.Close()
	opts.Backend = build()
	second := engine.New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	reqs := round4Lines(requests)
	spawns := round4Lines(argv)
	t.Logf("resume err=%v; side=%q; spawns=%d", err, round4Lines(side), len(spawns))
	for i, r := range reqs {
		t.Logf("request %d: %.160s", i+1, r)
	}
	if len(reqs) >= 2 && reqs[1] == reqs[0] && !strings.Contains(reqs[1], "Continue") {
		t.Fatalf("RESENT: the planner turn the vendor closed after running a tool was sent again verbatim on %q", spawns[len(spawns)-1][len(spawns[len(spawns)-1])-60:])
	}
}
