package engine

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func continuationLimit() enginetest.Step {
	return enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800",
		ErrorBody: `{"error":{"message":"rate limited"}}`}
}

type continuationBackend func(context.Context, []provider.Message) (provider.Message, provider.Meta, error)

func (f continuationBackend) StreamChat(ctx context.Context, _ string, msgs []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	return f(ctx, msgs)
}

type pausedBatchTools struct {
	mu      sync.Mutex
	started chan struct{}
	paused  chan struct{}
	calls   []string
}

func (b *pausedBatchTools) Definitions() []provider.Tool { return nil }
func (b *pausedBatchTools) Execute(ctx context.Context, name, _ string) (string, bool, error) {
	b.mu.Lock()
	b.calls = append(b.calls, name)
	first := len(b.calls) == 1
	b.mu.Unlock()
	if first {
		close(b.started)
		select {
		case <-b.paused:
		case <-ctx.Done():
			return "", true, ctx.Err()
		}
	}
	return "action completed", true, nil
}

func TestConcurrentPauseFinishesInFlightToolAndRetainsRestOfBatch(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: `[{"title":"batch","kind":"research"},{"title":"limited","kind":"research"}]`, Cost: 0.01}, enginetest.Step{Text: "all complete", Cost: 0.02})
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 2
	batch := &pausedBatchTools{started: make(chan struct{}), paused: make(chan struct{})}
	a.ExtraTools = batch
	var once sync.Once
	a.Subagents = func(s SubagentStatus) {
		if s.Index == 2 && s.State == SubagentWaiting {
			once.Do(func() { close(batch.paused) })
		}
	}
	limited := false
	a.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
		return continuationBackend(func(ctx context.Context, msgs []provider.Message) (provider.Message, provider.Meta, error) {
			if strings.Contains(msgs[1].Content, "limited") {
				if !limited {
					limited = true
					select {
					case <-batch.started:
					case <-ctx.Done():
						return provider.Message{}, provider.Meta{}, ctx.Err()
					}
					return provider.Message{}, provider.Meta{Cost: 0.07}, provider.Limit{Kind: provider.LimitAccountQuota}
				}
			} else if len(msgs) == 2 {
				return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{
					{ID: "first", Function: provider.FunctionCall{Name: "github__first", Arguments: `{}`}},
					{ID: "second", Function: provider.FunctionCall{Name: "github__second", Arguments: `{}`}},
				}}, provider.Meta{}, nil
			} else if len(msgs) != 5 {
				t.Errorf("resumed tool conversation = %+v, want both results before next provider call", msgs)
			}
			return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{}, nil
		}), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var paused *PausedError
	if err := a.RunTurn(ctx, "finish both"); !errors.As(err, &paused) {
		t.Fatalf("expected coordinated pause: %v", err)
	}
	if got := batch.calls; len(got) != 1 || got[0] != "github__first" {
		t.Fatalf("calls before resumption = %v", got)
	}
	run := sess.RunState()
	if len(run.Tasks[0].Messages) != 4 || len(pendingToolCalls(run.Tasks[0].Messages)) != 1 || math.Abs(run.Spend.USD-0.08) > 1e-9 {
		t.Fatalf("pause lost tool boundary or charged work: %+v", run)
	}
	id, seq := run.Tasks[0].ID, run.Tasks[0].Sequence
	pending, _ := a.Resume()
	if err := a.RunTurn(ctx, pending); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := batch.calls; len(got) != 2 || got[0] != "github__first" || got[1] != "github__second" {
		t.Fatalf("resumption replayed the batch: %v", got)
	}
	if done := sess.RunState(); done.Tasks[0].ID != id || done.Tasks[0].Sequence <= seq || math.Abs(done.Spend.USD-0.1) > 1e-9 {
		t.Fatalf("resume lost identity, sequence, or cost: %+v", done)
	}
}

func TestPausedWriterRetainsItsWorktreeUntilResumed(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"edit one","kind":"edit"},{"title":"edit two","kind":"edit"}]`},
		continuationLimit(), enginetest.Step{Text: "first done"}, enginetest.Step{Text: "second done"}, enginetest.Step{Text: "all done"},
	)
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	iso := &fakeIsolator{}
	a.Isolator, a.MaxConcurrentTasks = iso, 1
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "two edits"); !errors.As(err, &paused) {
		t.Fatal(err)
	}
	if isolated, landed, released := iso.counts(); isolated != 1 || landed != 0 || released != 0 {
		t.Fatalf("paused tree: isolated=%d landed=%d released=%d", isolated, landed, released)
	}
	saved := sess.RunState().Tasks[0].Workspace
	pending, _ := a.Resume()
	if err := a.RunTurn(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if isolated, landed, released := iso.counts(); isolated != 2 || landed != 2 || released != 2 || iso.landed[0] != saved {
		t.Fatalf("resumed tree: isolated=%d landed=%d released=%d paths=%v", isolated, landed, released, iso.landed)
	}
}

type continuationConversation struct {
	continuationBackend
	handle string
}

func (b continuationConversation) ProviderHandle() string { return b.handle }

// It behaves as the real adapters do after a plan limit: the vendor confirmed
// the conversation, closed the turn with the limit's result frame, and a
// confirmed conversation continues by its handle. A handle alone would say
// none of that.
func (b continuationConversation) ProviderHandleConfirmed() bool { return b.handle != "" }
func (continuationConversation) TurnClosed() bool                { return true }
func (continuationConversation) ResumesConversation() bool       { return true }

func TestResumedChildUsesItsOwnProviderConversation(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: `[{"title":"first","kind":"research"},{"title":"second","kind":"research"}]`}, enginetest.Step{Text: "all done"})
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 1
	sess.SetProviderStateName("parent-conversation")
	opens := 0
	a.SubagentBackend = func(_ context.Context, _, _, _ string, capabilities SubagentCapabilities) (ChatBackend, error) {
		opens++
		attempt := opens
		if attempt <= 2 && capabilities.ProviderState != "" || attempt == 3 && capabilities.ProviderState != "child-second" {
			t.Errorf("attempt %d inherited wrong handle %q", attempt, capabilities.ProviderState)
		}
		handle := "child-first"
		if attempt > 1 {
			handle = "child-second"
		}
		return continuationConversation{handle: handle, continuationBackend: func(_ context.Context, msgs []provider.Message) (provider.Message, provider.Meta, error) {
			if attempt == 2 {
				return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitAccountQuota}
			}
			if attempt == 3 && !strings.Contains(msgs[len(msgs)-1].Content, "do not repeat completed actions") {
				t.Error("provider resumption did not distinguish continuing from starting over")
			}
			return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{}, nil
		}}, nil
	}
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "finish both"); !errors.As(err, &paused) {
		t.Fatal(err)
	}
	pending, _ := a.Resume()
	if err := a.RunTurn(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if opens != 3 || sess.ProviderStateName() != "parent-conversation" {
		t.Fatalf("opens=%d, parent handle=%q", opens, sess.ProviderStateName())
	}
}

func TestStartedResumeRetiresThePreviousSafePauseBoundary(t *testing.T) {
	srv := enginetest.New(continuationLimit())
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeCode)
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "continue the work"); !errors.As(err, &paused) {
		t.Fatal(err)
	}
	pending, _ := a.Resume()
	a.SetSessionBackend(continuationBackend(func(context.Context, []provider.Message) (provider.Message, provider.Meta, error) {
		// A process killed here must not reopen as the old, fully settled
		// allowance pause. Work may have happened since that boundary.
		if run := sess.RunState(); run.LastPause != nil {
			t.Error("active resumed work still advertises the previous safe pause boundary")
		}
		return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{}, nil
	}))
	if err := a.RunTurn(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
}

func TestCancellationRacingALimitStopsTheJournal(t *testing.T) {
	srv := enginetest.New()
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeCode)
	ctx, cancel := context.WithCancel(context.Background())
	a.SetSessionBackend(continuationBackend(func(context.Context, []provider.Message) (provider.Message, provider.Meta, error) {
		cancel()
		return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitAccountQuota}
	}))
	if err := a.RunTurn(ctx, "old request"); err == nil {
		t.Fatal("cancelled work returned success")
	}
	if run := sess.RunState(); run.Phase != "stopped" || sess.Paused() != nil {
		t.Fatalf("cancellation left an unfinished request: %+v", run)
	}
	a.SetSessionBackend(&answeringBackend{})
	if err := a.RunTurn(context.Background(), "new request"); err != nil {
		t.Fatalf("cancelled request blocks new input: %v", err)
	}
}

func TestDiscardPendingCancelsDeliveryWithoutRevivingThePause(t *testing.T) {
	a, _ := pausedLifecycleAgent(t)
	entered, done := make(chan struct{}), make(chan struct{})
	a.ResumeWait = func(context.Context, time.Duration) error { return nil }
	a.ResumeReady = func(ctx context.Context, _ string) bool {
		close(entered)
		<-ctx.Done()
		close(done)
		return false
	}
	cleanup := a.WatchPauses(context.Background())
	awaitResumeSignal(t, entered)
	if !a.DiscardPending() {
		t.Fatal("could not abandon pending delivery")
	}
	awaitResumeSignal(t, done)
	cleanup()
	if a.Sess.Paused() != nil || a.Sess.RunState().Phase != "stopped" {
		t.Fatal("declined stale delivery revived the abandoned request")
	}
	if _, ok := a.Resume(); ok {
		t.Fatal("abandoned request remains resumable")
	}
}

func TestMissingSavedTreeCanBeAbandonedWithoutDeletingHistory(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: `[{"title":"edit","kind":"edit"},{"title":"review","kind":"explain"}]`}, continuationLimit())
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.Isolator, a.MaxConcurrentTasks = &fakeIsolator{}, 1
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "edit and review"); !errors.As(err, &paused) {
		t.Fatal(err)
	}
	run := sess.RunState()
	run.Tasks[0].Workspace = "/missing/saved/tree"
	sess.SetRunState(run)
	pending, _ := a.Resume()
	if err := a.RunTurn(context.Background(), pending); err == nil || !strings.Contains(err.Error(), "/resume discard") {
		t.Fatalf("missing tree has no recovery instruction: %v", err)
	}
	if !a.DiscardPending() || sess.RunState().Tasks[0].Workspace != "/missing/saved/tree" {
		t.Fatal("discard did not preserve the saved record")
	}
	a.SetSessionBackend(&answeringBackend{})
	if err := a.RunTurn(context.Background(), "a different request"); err != nil {
		t.Fatalf("discard left the session wedged: %v", err)
	}
}

func TestResumeHonorsALoweredRunBudgetWithoutRepeatingFinishedWork(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"first","kind":"research"},{"title":"second","kind":"research"},{"title":"third","kind":"research"}]`, Cost: 0.01},
		enginetest.Step{Text: "saved result", Cost: 0.05}, continuationLimit(), enginetest.Step{Text: "budget summary"},
	)
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks, a.MaxRunCostUSD = 1, 1
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "three tasks"); !errors.As(err, &paused) {
		t.Fatal(err)
	}
	a.MaxRunCostUSD = 0.05
	pending, _ := a.Resume()
	if err := a.RunTurn(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	run := sess.RunState()
	if len(srv.Requests) != 4 || run.Spend.Limit != 0.05 || run.Tasks[0].Result != "saved result" || run.Tasks[1].Status != statusOverBudget.String() || run.Tasks[2].Status != statusOverBudget.String() {
		t.Fatalf("lower budget did not preserve finished work and stop admission: %+v", run)
	}
}

func TestCancelledBeforeResumedTurnStartsReturnsTheClaim(t *testing.T) {
	srv := enginetest.New(continuationLimit(), enginetest.Step{Text: "done"})
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeCode)
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "pending request"); !errors.As(err, &paused) {
		t.Fatal(err)
	}
	pending, _ := a.Resume()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.RunTurn(ctx, pending); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if sess.Paused() == nil || len(srv.Requests) != 1 {
		t.Fatal("unstarted delivery lost its pause or sent work")
	}
	pending, ok := a.Resume()
	if !ok {
		t.Fatal("unstarted delivery kept an unrecoverable claim")
	}
	if err := a.RunTurn(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
}

func TestSynthesisResumeKeepsCompletedTasks(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"first task"},{"title":"second task"}]`},
		enginetest.Step{Text: "first result"}, enginetest.Step{Text: "second result"},
		continuationLimit(), enginetest.Step{Text: "combined answer"},
	)
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 1
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "finish both tasks"); !errors.As(err, &paused) {
		t.Fatalf("initial turn did not pause: %v", err)
	}
	pending, ok := a.Resume()
	if !ok {
		t.Fatal("missing pending turn")
	}
	if err := a.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("resume should only synthesize saved results: %v", err)
	}
	if len(srv.Requests) != 5 {
		t.Fatalf("requests = %d, want planner, two children, paused synthesis and resumed synthesis", len(srv.Requests))
	}
	last := srv.Requests[4]
	if len(last) < 2 || !strings.Contains(last[1].Content, "first result") || !strings.Contains(last[1].Content, "second result") {
		t.Fatalf("saved results did not reach synthesis: %+v", last)
	}
}

func TestChildLimitPausesTheWholePlan(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"limited task","needs":[]},{"title":"later task","needs":[]}]`},
		continuationLimit(), enginetest.Step{Text: "should not start"}, enginetest.Step{Text: "should not synthesize"},
	)
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 1
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "finish both tasks"); !errors.As(err, &paused) || sess.Paused() == nil {
		t.Fatalf("child limit did not pause its plan: %v", err)
	}
	if len(srv.Requests) != 2 {
		t.Fatalf("started more work after the limit: %d requests", len(srv.Requests))
	}
}
