package engine

// Found by the round-5 vendor-recovery verifier: agent-mode planner and
// synthesis calls on a vendor main session, stopped after the vendor closed
// the turn, were sent again instead of continued.

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func round5Limit() error {
	return vendorLimitError{provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude",
		ResetAt: time.Now().Add(time.Hour), Message: "plan limit", Source: "vendor-frame"}}
}

// R5-P3. Rule 3: a vendor-closed main turn continues with a NEW message on
// the same conversation; the stopped turn is never sent again. In agent mode
// the main vendor serves the planner (with its own tools: claude.go:120-124).
func TestRound5AgentPlannerStopIsContinuedNotResent(t *testing.T) {
	for _, stopAt := range []string{"planner", "synthesis"} {
		t.Run(stopAt, func(t *testing.T) {
			plan := `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`
			var attempts []attemptScript
			if stopAt == "planner" {
				attempts = []attemptScript{{closed: true, err: round5Limit()}, {closed: true, reply: plan}, {closed: true, reply: "all done"}}
			} else {
				attempts = []attemptScript{{closed: true, reply: plan}, {closed: true, stream: "half a summary", err: round5Limit()}, {closed: true, reply: "all done"}}
			}
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r5_phase", "parent")}
			main := &scriptedAttemptsMain{attempts: attempts}
			opts := Options{Backend: main, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					return childCaptureBackend{text: "task done"}, nil
				}}
			first := New(opts)
			var paused *PausedError
			if err := first.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
				t.Fatalf("setup: not paused: %v", err)
			}
			_ = first.Close()
			run := sess.RunState()
			t.Logf("journal: phase=%s state=%q confirmed=%v closed=%v inflight=%v delivered=%v", run.Phase, run.Main.ProviderState,
				run.Main.ProviderConfirmed, run.Main.ProviderTurnClosed, run.Main.ProviderInFlight, run.Main.ProviderDelivered)
			second := New(opts)
			defer second.Close()
			pending, ok := second.Resume()
			if !ok {
				t.Fatal("nothing to resume")
			}
			err := second.RunTurn(context.Background(), pending)
			main.mu.Lock()
			defer main.mu.Unlock()
			stopped := main.sent[map[string]int{"planner": 0, "synthesis": 1}[stopAt]]
			resumedIdx := map[string]int{"planner": 1, "synthesis": 2}[stopAt]
			if len(main.sent) <= resumedIdx {
				t.Fatalf("resume err=%v; calls=%d", err, main.calls)
			}
			resumed := main.sent[resumedIdx]
			lastStopped, lastResumed := stopped[len(stopped)-1].Content, resumed[len(resumed)-1].Content
			t.Logf("resume err=%v calls=%d\n  stopped call ended: %.120q\n  resumed call ended: %.120q", err, main.calls, lastStopped, lastResumed)
			if lastResumed == lastStopped && !strings.Contains(lastResumed, "Continue") {
				t.Fatalf("RESENT: the %s turn the vendor closed (and may have acted in) was sent again verbatim on the same conversation, not continued", stopAt)
			}
			// Only the call that stopped is continued: once the planner has
			// answered, the synthesis it leads to is an ordinary request.
			if stopAt == "planner" {
				if len(main.sent) < 3 {
					t.Fatalf("calls = %d; want the synthesis after the continued planner", len(main.sent))
				}
				synthesis := main.sent[2]
				if last := synthesis[len(synthesis)-1].Content; strings.Contains(last, "Continue this unfinished request") {
					t.Fatal("the synthesis after a continued planner was sent as a continuation too")
				}
			}
		})
	}
}
