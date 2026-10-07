package engine

// Found by the round-6 vendor-recovery verifier: a stopped vendor call was
// continued on whatever backend routing chose after a restart, not the one
// that drives the saved conversation.

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type round6RecordingBackend struct {
	mu   sync.Mutex
	sent [][]provider.Message
}

func (b *round6RecordingBackend) StreamChat(_ context.Context, _ string, msgs []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.mu.Lock()
	b.sent = append(b.sent, append([]provider.Message(nil), msgs...))
	b.mu.Unlock()
	return provider.Message{Role: "assistant", Content: `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`}, provider.Meta{}, nil
}

// R6-L. A vendor main's planner call stops after the vendor closed it (rule
// 3: continue on that conversation). Before /resume (after a restart) the
// orchestrator slot routes planning elsewhere. ownsSavedConversation checks
// the backend of the model the journal saved (Main.Model), but the
// continuation is sent to orchestrationModel(), which is not re-checked: the
// "continue the saved conversation" message reaches a backend that never saw
// the vendor's planner turn.
func TestRound6PlannerContinuationStaysOnTheSavedConversation(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r6_route", "mock/model")}
	main := &scriptedAttemptsMain{attempts: []attemptScript{{closed: true, err: round5Limit()}, {closed: true, reply: "plan?"}, {closed: true, reply: "all done"}}}
	alt := &round6RecordingBackend{}
	opts := Options{Backend: main, Model: "mock/model", Mode: ModeAgent, Effort: EffortMedium,
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
	t.Logf("journal: phase=%s model=%q state=%q inflight=%v closed=%v", run.Phase, run.Main.Model, run.Main.ProviderState, run.Main.ProviderInFlight, run.Main.ProviderTurnClosed)
	// Restart with the orchestrator slot routed to another backend.
	opts.Slots = map[string]string{SlotOrchestrator: "alt/planner"}
	opts.Routes = map[string]ChatBackend{"alt": alt}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	alt.mu.Lock()
	defer alt.mu.Unlock()
	main.mu.Lock()
	defer main.mu.Unlock()
	t.Logf("resume err=%v; main calls=%d; alt calls=%d", err, main.calls, len(alt.sent))
	for i, msgs := range alt.sent {
		last := msgs[len(msgs)-1].Content
		t.Logf("alt call %d ends %.100q", i+1, last)
		if strings.Contains(last, "Continue this unfinished request") {
			t.Fatalf("WRONG CONVERSATION: the stopped vendor planner turn (on %q) was continued on another backend that never saw it", run.Main.ProviderState)
		}
	}
	if resumed := main.sent[1]; !strings.Contains(resumed[len(resumed)-1].Content, "Continue this unfinished request") {
		t.Fatalf("the saved conversation did not get the continuation; its second call ended %q", resumed[len(resumed)-1].Content)
	}
}

// R6-L2. The same for a code-mode (direct) request: the tier for the
// session's effort is routed elsewhere before /resume after a restart.
func TestRound6DirectContinuationStaysOnTheSavedConversation(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r6_route2", "mock/model")}
	main := &scriptedAttemptsMain{attempts: []attemptScript{{closed: true, err: round5Limit()}, {closed: true, reply: "done"}}}
	alt := &round6RecordingBackend{}
	opts := Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard}
	first := New(opts)
	var paused *PausedError
	if err := first.RunTurn(context.Background(), "deploy the service"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	_ = first.Close()
	run := sess.RunState()
	t.Logf("journal: phase=%s model=%q state=%q inflight=%v closed=%v", run.Phase, run.Main.Model, run.Main.ProviderState, run.Main.ProviderInFlight, run.Main.ProviderTurnClosed)
	opts.Tiers = map[string]string{EffortMedium: "alt/coder"}
	opts.Routes = map[string]ChatBackend{"alt": alt}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	alt.mu.Lock()
	defer alt.mu.Unlock()
	main.mu.Lock()
	defer main.mu.Unlock()
	t.Logf("resume err=%v; main calls=%d; alt calls=%d", err, main.calls, len(alt.sent))
	for i, msgs := range alt.sent {
		last := msgs[len(msgs)-1].Content
		t.Logf("alt call %d ends %.100q", i+1, last)
		if strings.Contains(last, "Continue this unfinished request") {
			t.Fatalf("WRONG CONVERSATION: the stopped vendor turn (on %q) was continued on another backend that never saw it", run.Main.ProviderState)
		}
	}
	if err != nil || main.calls != 2 {
		t.Fatalf("resume = %v with %d calls on the saved conversation; want it continued there", err, main.calls)
	}
	if resumed := main.sent[1]; !strings.Contains(resumed[len(resumed)-1].Content, "Continue this unfinished request") {
		t.Fatalf("the saved conversation did not get the continuation; its second call ended %q", resumed[len(resumed)-1].Content)
	}
}

// The same for a synthesis the vendor closed: restarted with the orchestrator
// slot routed elsewhere, the synthesis continues on the saved conversation.
func TestRound6SynthesisContinuationStaysOnTheSavedConversation(t *testing.T) {
	plan := `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r6_route3", "mock/model")}
	main := &scriptedAttemptsMain{attempts: []attemptScript{{closed: true, reply: plan}, {closed: true, err: round5Limit()}, {closed: true, reply: "all done"}}}
	alt := &round6RecordingBackend{}
	opts := Options{Backend: main, Model: "mock/model", Mode: ModeAgent, Effort: EffortMedium,
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
	if run := sess.RunState(); run.Phase != "synthesis" || !run.Main.ProviderInFlight {
		t.Fatalf("setup: saved phase %q, in flight %v", run.Phase, run.Main.ProviderInFlight)
	}
	opts.Slots = map[string]string{SlotOrchestrator: "alt/planner"}
	opts.Routes = map[string]ChatBackend{"alt": alt}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	alt.mu.Lock()
	defer alt.mu.Unlock()
	main.mu.Lock()
	defer main.mu.Unlock()
	if len(alt.sent) != 0 {
		t.Fatalf("the stopped synthesis was continued on another backend (%d calls)", len(alt.sent))
	}
	if err != nil || main.calls != 3 {
		t.Fatalf("resume = %v with %d calls on the saved conversation; want the synthesis continued there", err, main.calls)
	}
	if resumed := main.sent[2]; !strings.Contains(resumed[len(resumed)-1].Content, "Continue this unfinished request") {
		t.Fatalf("the saved conversation did not get the continuation; the synthesis ended %q", resumed[len(resumed)-1].Content)
	}
}
