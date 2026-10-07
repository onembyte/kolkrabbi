package agentcli

// Found by the round-5 vendor-recovery verifier, each red on the tree it
// reviewed.

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// R5-P4a. Round-4 #6 says a kill after the vendor's result frame retires
// nothing, and the author removed the judgement of usable sessions. The retry
// path still judges its replacement unconditionally (backend.go:225): a
// replacement that closed the turn (plan limit) and was then killed retires
// the closed conversation, so the journal can no longer continue it.
func TestRound5ReplacementKilledAfterResultRetiresNothing(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spawns := 0
	var argvs [][]string
	backend.start = func(_ context.Context, _ string, args []string) (lineProcess, error) {
		spawns++
		argvs = append(argvs, args)
		if spawns == 1 {
			// Answers turn one, then exits idle: the next prompt is refused.
			return &fakeLineProcess{exitWhenDrained: true, lines: [][]byte{
				[]byte(`{"type":"system","subtype":"init","model":"opus","session_id":"H-saved"}`),
				[]byte(`{"type":"result","subtype":"success","result":"one","session_id":"H-saved"}`),
			}}, nil
		}
		// The retry's process: the vendor closes the turn (plan limit), then
		// the process is killed.
		return &fakeLineProcess{hardExit: true, lines: [][]byte{
			[]byte(`{"type":"system","subtype":"init","model":"opus","session_id":"H-saved"}`),
			[]byte(`{"type":"result","subtype":"success","is_error":true,"result":"You've hit your limit","session_id":"H-saved"}`),
		}}, nil
	}
	if _, _, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "one"}}, nil, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var streamed string
	_, _, turnErr := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "deploy"}}, nil, func(s string) { streamed += s })
	t.Logf("spawns=%d err=%v closed=%v delivered=%v handle=%q confirmed=%v retired=%v streamed=%q", spawns, turnErr, backend.TurnClosed(),
		!backend.TurnNeverStarted(), backend.ProviderHandle(), backend.ProviderHandleConfirmed(), backend.ProviderHandleRetired(), streamed)
	if spawns != 2 || !strings.Contains(strings.Join(argvs[1], " "), "--resume H-saved") {
		t.Fatalf("setup: the retry did not run on H-saved: %d %q", spawns, argvs)
	}
	if !backend.TurnClosed() {
		t.Fatal("setup: the vendor's result frame did not close the turn")
	}
	if backend.ProviderHandle() != "H-saved" || !backend.ProviderHandleConfirmed() || backend.ProviderHandleRetired() {
		t.Fatalf("CLOSED TURN RETIRED ON THE RETRY PATH: the replacement closed the turn, a kill after it retired the conversation (handle %q, confirmed %v, retired %v)",
			backend.ProviderHandle(), backend.ProviderHandleConfirmed(), backend.ProviderHandleRetired())
	}
}

// R5-P4b. The same with real processes: spawn 1 exits idle after turn one;
// spawn 2 (the retry, --resume H-saved) prints the vendor's closing result
// frame and is SIGKILLed at once. Reports how often the closed conversation is
// retired (timing-dependent).
func TestRound5ReplacementKilledAfterResultRealProcess(t *testing.T) {
	retired := 0
	const runs = 10
	for i := 0; i < runs; i++ {
		path, argv := fakeClaudeScript(t, round4Sid+`IFS= read -r line
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' "{\"type\":\"result\",\"result\":\"one\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
  exit 0
fi
printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You've hit your limit\",\"session_id\":\"$sid\"}"
kill -9 $$
`)
		backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		backend.start = realStart(path)
		if _, err := oneTurn(t, backend, "one"); err != nil {
			t.Fatalf("setup: %v", err)
		}
		round4WaitExit(t, backend)
		_, turnErr := oneTurn(t, backend, "deploy")
		spawns := round4Spawns(t, argv)
		if len(spawns) != 2 || !backend.TurnClosed() {
			t.Logf("run %d: setup off: spawns=%d closed=%v err=%v", i, len(spawns), backend.TurnClosed(), turnErr)
		}
		if backend.TurnClosed() && (backend.ProviderHandle() != "H-saved" || backend.ProviderHandleRetired()) {
			retired++
		}
		_ = backend.Close()
	}
	t.Logf("closed conversation retired in %d of %d real runs", retired, runs)
	if retired > 0 {
		t.Fatalf("CLOSED TURN RETIRED ON THE RETRY PATH in %d/%d real-process runs", retired, runs)
	}
}

// R5-W1 (watchpoint, F4-adjacent). Close() keeps b.session; a later turn on
// the closed backend is refused by TurnObserved before the per-turn facts are
// reset, and noteTurn copies the PREVIOUS turn's delivered/closed facts.
func TestRound5ATurnOnAClosedBackendProvesNothingDelivered(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "H-saved", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return &fakeLineProcess{lines: [][]byte{
			[]byte(`{"type":"system","subtype":"init","model":"opus","session_id":"H-saved"}`),
			[]byte(`{"type":"result","subtype":"success","result":"one","session_id":"H-saved"}`),
		}}, nil
	}
	if _, _, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "one"}}, nil, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_ = backend.Close()
	_, _, turnErr := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "two"}}, nil, nil)
	t.Logf("err=%v neverStarted=%v closed=%v", turnErr, backend.TurnNeverStarted(), backend.TurnClosed())
	if turnErr != nil && (!backend.TurnNeverStarted() || backend.TurnClosed()) {
		t.Fatalf("STALE FACTS: a turn refused by a closed session reports delivered=%v closed=%v (the previous turn's)", !backend.TurnNeverStarted(), backend.TurnClosed())
	}
}
