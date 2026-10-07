package engine

// From the round-6 vendor-recovery verifier (green on the tree it reviewed):
// agent-mode resume from each phase on a vendor main session.

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type round6FlakyChild struct{ calls *atomic.Int32 }

func (c round6FlakyChild) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	if c.calls.Add(1) == 1 {
		return provider.Message{}, provider.Meta{}, round5Limit()
	}
	return provider.Message{Role: "assistant", Content: "task done"}, provider.Meta{}, nil
}

func round6Last(msgs []provider.Message) string { return msgs[len(msgs)-1].Content }

func TestRound6AgentResumeFromEveryPhase(t *testing.T) {
	plan2 := `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`
	plan1 := `[{"title":"only","kind":"explain"}]`
	cases := []struct {
		name     string
		attempts []attemptScript
		child    func() ChatBackend
		check    func(t *testing.T, sent [][]provider.Message)
	}{
		{name: "tasks", attempts: []attemptScript{{closed: true, reply: plan2}, {closed: true, reply: "all done"}, {closed: true, reply: "extra"}},
			check: func(t *testing.T, sent [][]provider.Message) {
				for i, msgs := range sent[1:] {
					if strings.Contains(round6Last(msgs), "Continue this unfinished request") {
						t.Errorf("call %d after a child-only stop was sent as a continuation", i+2)
					}
				}
			}},
		{name: "direct", attempts: []attemptScript{{closed: true, reply: plan1}, {closed: true, stream: "half", err: round5Limit()}, {closed: true, reply: "done"}, {closed: true, reply: "extra"}},
			check: func(t *testing.T, sent [][]provider.Message) {
				if len(sent) < 3 || !strings.Contains(round6Last(sent[2]), "Continue this unfinished request") {
					t.Errorf("the stopped direct call was not continued: %d calls", len(sent))
				}
			}},
		{name: "plan-then-single", attempts: []attemptScript{{closed: true, err: round5Limit()}, {closed: true, reply: plan1}, {closed: true, reply: "done"}, {closed: true, reply: "extra"}},
			check: func(t *testing.T, sent [][]provider.Message) {
				if len(sent) < 3 || !strings.Contains(round6Last(sent[1]), "Continue this unfinished request") {
					t.Errorf("the stopped planner was not continued")
				}
				if len(sent) >= 3 && strings.Contains(round6Last(sent[2]), "Continue this unfinished request") {
					t.Errorf("the direct call after a continued planner was sent as a continuation")
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r6_"+c.name, "parent")}
			main := &scriptedAttemptsMain{attempts: c.attempts}
			var childCalls atomic.Int32
			opts := Options{Backend: main, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					return round6FlakyChild{calls: &childCalls}, nil
				}}
			first := New(opts)
			err := first.RunTurn(context.Background(), "two things")
			_ = first.Close()
			var paused *PausedError
			if !errors.As(err, &paused) {
				t.Fatalf("setup: not paused: %v", err)
			}
			run := sess.RunState()
			t.Logf("journal: phase=%s state=%q inflight=%v closed=%v", run.Phase, run.Main.ProviderState, run.Main.ProviderInFlight, run.Main.ProviderTurnClosed)
			second := New(opts)
			defer second.Close()
			pending, ok := second.Resume()
			if !ok {
				t.Fatal("nothing to resume")
			}
			rerr := second.RunTurn(context.Background(), pending)
			main.mu.Lock()
			sent := append([][]provider.Message(nil), main.sent...)
			main.mu.Unlock()
			planners := 0
			for i, msgs := range sent {
				last := round6Last(msgs)
				t.Logf("call %d ends %.90q", i+1, last)
				for _, m := range msgs {
					if strings.Contains(m.Content, "Decompose the request") {
						planners++
						break
					}
				}
			}
			t.Logf("resume err=%v; planner calls=%d", rerr, planners)
			// Verbatim resend: any call whose whole message list equals an earlier call's.
			for i := 1; i < len(sent); i++ {
				for j := 0; j < i; j++ {
					if round6Same(sent[i], sent[j]) {
						t.Errorf("RESENT: call %d repeats call %d verbatim on the vendor conversation", i+1, j+1)
					}
				}
			}
			c.check(t, sent)
		})
	}
}

func round6Same(a, b []provider.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content {
			return false
		}
	}
	return true
}
