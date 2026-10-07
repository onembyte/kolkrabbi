package engine

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

type recordedRecovery struct {
	reason string
	run    *continuity.Run
}

type captureRecoverySession struct {
	*enginetest.FakeSession
	mu      sync.Mutex
	err     error
	before  func(string, *continuity.Run)
	records []recordedRecovery
}

func (s *captureRecoverySession) SaveRecovery(reason string) error {
	run := s.RunState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.before != nil {
		s.before(reason, run)
	}
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, recordedRecovery{reason: reason, run: run})
	return nil
}

func (s *captureRecoverySession) saved() []recordedRecovery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRecovery(nil), s.records...)
}

type partialFailureBackend struct {
	partial string
	err     error
}

func (b partialFailureBackend) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	for _, part := range []string{b.partial[:len(b.partial)/2], b.partial[len(b.partial)/2:]} {
		if onToken != nil {
			onToken(part)
		}
	}
	return provider.Message{}, provider.Meta{Model: "mock/model"}, b.err
}

func TestAnExceptionalRecoveryRetainsTheCompletePartialMainStream(t *testing.T) {
	sess := &captureRecoverySession{FakeSession: enginetest.NewFakeSession("s_capture", "mock/model")}
	wantPartial := strings.Repeat("unfinished ", 20000)
	agent := New(Options{
		Backend: partialFailureBackend{partial: wantPartial, err: errors.New("provider stream broke")},
		Model:   "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
	})

	err := agent.RunTurn(context.Background(), "finish this")
	if err == nil || !strings.Contains(err.Error(), "provider stream broke") {
		t.Fatalf("RunTurn error = %v, want the provider error", err)
	}
	records := sess.saved()
	if len(records) != 1 || records[0].reason != "error" {
		t.Fatalf("recoveries = %+v, want one error boundary", records)
	}
	run := records[0].run
	if run == nil || run.Phase != "direct" || run.Main.State != continuity.TaskWaiting || !run.Main.ProviderInFlight || run.Main.SafeBoundary != "" {
		t.Fatalf("saved run = %+v, want a waiting direct continuation", run)
	}
	if run.Main.PartialOutput != wantPartial {
		t.Fatalf("partial output length = %d, want %d complete bytes", len(run.Main.PartialOutput), len(wantPartial))
	}
}

type childCaptureBackend struct {
	text      string
	err       error
	handle    string
	confirmed bool
}

func (b childCaptureBackend) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	if onToken != nil && b.text != "" {
		onToken(b.text)
	}
	if b.err != nil {
		return provider.Message{}, provider.Meta{Model: "child"}, b.err
	}
	return provider.Message{Role: "assistant", Content: b.text}, provider.Meta{Model: "child"}, nil
}

func (b childCaptureBackend) ProviderHandle() string        { return b.handle }
func (b childCaptureBackend) ProviderHandleConfirmed() bool { return b.confirmed }

type mintedChildBackend struct{ handle string }

func (b mintedChildBackend) ProviderHandle() string { return b.handle }
func (b mintedChildBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, nil
}

// Deliberately no ProviderHandleConfirmed method: a handle alone is not an
// acknowledgement, which is the distinction Claude's locally minted id needs.
func TestAMintedChildHandleIsNotRecordedAsConfirmed(t *testing.T) {
	sess := enginetest.NewFakeSession("s_minted_child", "parent")
	agent := New(Options{Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent,
		Sess: sess, Root: t.TempDir(), Out: io.Discard})
	agent.beginExecution("request", "request")
	agent.setExecutionPlan([]Task{{Title: "child", Model: "child", Effort: EffortMedium}})
	agent.saveChildConversation(0, []provider.Message{{Role: "user", Content: "work"}}, 0, doomLoop{},
		mintedChildBackend{handle: "locally-minted"})

	saved := agent.executionTask(0)
	if saved.ProviderState != "locally-minted" || saved.ProviderConfirmed {
		t.Fatalf("saved provider state = %+v, want an unconfirmed minted handle", saved)
	}
}

func TestAChildFailureIsSavedBeforeTheSchedulerAdmitsMoreWork(t *testing.T) {
	sess := &captureRecoverySession{FakeSession: enginetest.NewFakeSession("s_child_capture", "parent")}
	secondStarted := false
	sess.before = func(reason string, run *continuity.Run) {
		if reason != "error" {
			t.Errorf("recovery reason = %q, want error", reason)
		}
		if secondStarted {
			t.Error("the next child started before the failed child's recovery point")
		}
		if run == nil || len(run.Tasks) != 2 {
			t.Fatalf("saved run = %+v, want two tasks", run)
		}
		if run.Main.State != continuity.TaskWaiting {
			t.Errorf("main state = %q, want waiting while children run", run.Main.State)
		}
		failed, queued := run.Tasks[0], run.Tasks[1]
		if failed.State != continuity.TaskSettled || failed.Status != "failed" {
			t.Errorf("failed task = %+v, want an explicit settled failure", failed)
		}
		if failed.PartialOutput != "partial child output" || failed.ProviderState != "vendor-child-1" || !failed.ProviderConfirmed || !failed.ProviderInFlight || failed.SafeBoundary != "" {
			t.Errorf("failed task continuation = %+v, want partial output and confirmed vendor handle", failed)
		}
		if queued.State != continuity.TaskQueued {
			t.Errorf("next task state = %q, want queued", queued.State)
		}
	}
	agent := New(Options{
		Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		MaxConcurrentTasks: 1,
		SubagentBackend: func(_ context.Context, model, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
			switch model {
			case "child-one":
				return childCaptureBackend{text: "partial child output", err: errors.New("child provider failed"), handle: "vendor-child-1", confirmed: true}, nil
			case "child-two":
				secondStarted = true
				return childCaptureBackend{text: "second done"}, nil
			default:
				return nil, errors.New("unexpected child model " + model)
			}
		},
	})
	agent.lastTurnID = "turn_capture"
	agent.beginExecution("request", "request")
	tasks := []Task{
		{Title: "first", Kind: KindExplain, Model: "child-one", Effort: EffortMedium},
		{Title: "second", Kind: KindExplain, Model: "child-two", Effort: EffortMedium},
	}
	agent.setExecutionPlan(tasks)
	agent.storeExecution()
	agent.runSpend = &spend{}
	outcomes, err := agent.runTasks(context.Background(), "request", tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 || outcomes[0].Status != statusFailed || outcomes[1].Status != statusDone {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	if !secondStarted || len(sess.saved()) != 1 {
		t.Fatalf("second started=%v, recoveries=%d; want work to continue after one checkpoint", secondStarted, len(sess.saved()))
	}
}

type contextChildBackendFunc func(context.Context, func(string)) (provider.Message, provider.Meta, error)

func (f contextChildBackendFunc) StreamChat(ctx context.Context, _ string, _ []provider.Message, _ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return f(ctx, onToken)
}

func executionPauseRecorded(ctx context.Context) bool { return pauseGate(ctx).stopped() != nil }

func TestAChildErrorRecoveryDrainsAnInflightSiblingBeforeSaving(t *testing.T) {
	sess := &captureRecoverySession{FakeSession: enginetest.NewFakeSession("s_child_drain", "parent")}
	var recovered, thirdStarted atomic.Bool
	secondDone := make(chan struct{})
	sess.before = func(reason string, run *continuity.Run) {
		select {
		case <-secondDone:
		default:
			t.Error("ordinary-error recovery did not drain the in-flight sibling")
		}
		if thirdStarted.Load() {
			t.Error("ordinary-error recovery admitted the third child before saving")
		}
		if reason != "error" || run == nil || len(run.Tasks) != 3 ||
			run.Tasks[0].State != continuity.TaskSettled || run.Tasks[1].State != continuity.TaskSettled ||
			run.Tasks[2].State != continuity.TaskQueued {
			t.Errorf("quiescent recovery = %+v, want two settled tasks and one queued task", run)
		}
		recovered.Store(true)
	}
	agent := New(Options{
		Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		MaxConcurrentTasks: 2,
		SubagentBackend: func(_ context.Context, model, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
			switch model {
			case "failed-child":
				return childCaptureBackend{text: "partial", err: errors.New("first failed")}, nil
			case "inflight-child":
				return contextChildBackendFunc(func(ctx context.Context, _ func(string)) (provider.Message, provider.Meta, error) {
					for {
						run := sess.RunState()
						if run != nil && len(run.Tasks) == 3 && run.Tasks[0].Status == "failed" {
							close(secondDone)
							return provider.Message{Role: "assistant", Content: "second settled"}, provider.Meta{}, nil
						}
						select {
						case <-ctx.Done():
							return provider.Message{}, provider.Meta{}, ctx.Err()
						default:
							runtime.Gosched()
						}
					}
				}), nil
			case "third-child":
				thirdStarted.Store(true)
				if !recovered.Load() {
					t.Error("third child opened before the error recovery completed")
				}
				return childCaptureBackend{text: "third settled"}, nil
			default:
				return nil, errors.New("unexpected child model " + model)
			}
		},
	})
	agent.lastTurnID = "turn_child_drain"
	agent.beginExecution("request", "request")
	tasks := []Task{
		{Title: "fail", Kind: KindExplain, Model: "failed-child", Effort: EffortMedium},
		{Title: "in flight", Kind: KindExplain, Model: "inflight-child", Effort: EffortMedium},
		{Title: "third", Kind: KindExplain, Model: "third-child", Effort: EffortMedium},
	}
	agent.setExecutionPlan(tasks)
	agent.storeExecution()
	agent.runSpend = &spend{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	outcomes, err := agent.runTasks(ctx, "request", tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 3 || outcomes[0].Status != statusFailed || outcomes[1].Status != statusDone || outcomes[2].Status != statusDone {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	if !recovered.Load() || !thirdStarted.Load() || len(sess.saved()) != 1 {
		t.Fatalf("recovered=%v third=%v writes=%d", recovered.Load(), thirdStarted.Load(), len(sess.saved()))
	}
}

func TestAPauseRecoveryWaitsForEveryInflightChildAndKeepsNewWorkQueued(t *testing.T) {
	sess := &captureRecoverySession{FakeSession: enginetest.NewFakeSession("s_pause_capture", "parent")}
	secondStarted := make(chan struct{})
	limitSeen := make(chan struct{})
	secondDone := make(chan struct{})
	thirdStarted := false
	sess.before = func(reason string, run *continuity.Run) {
		if reason != "pause" {
			t.Errorf("recovery reason = %q, want pause", reason)
		}
		select {
		case <-secondDone:
		default:
			t.Errorf("pause recovery ran before the other in-flight child settled: reason=%q run=%+v", reason, run)
		}
		if thirdStarted {
			t.Error("a queued child started after the limit closed admission")
		}
		// Never started: it stays queued. Waiting is a continuation's state.
		if run == nil || run.Tasks[2].State != continuity.TaskQueued {
			t.Errorf("third task = %+v, want it still queued", run.Tasks[2])
		}
	}
	agent := New(Options{
		Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		MaxConcurrentTasks: 2,
		SubagentBackend: func(_ context.Context, model, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
			switch model {
			case "limit-child":
				return childCaptureBackendFunc(func(onToken func(string)) (provider.Message, provider.Meta, error) {
					<-secondStarted
					close(limitSeen)
					return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitSubscriptionAllowance, ResetAt: time.Now().Add(time.Hour)}
				}), nil
			case "settling-child":
				return contextChildBackendFunc(func(ctx context.Context, _ func(string)) (provider.Message, provider.Meta, error) {
					close(secondStarted)
					<-limitSeen
					for {
						if executionPauseRecorded(ctx) {
							close(secondDone)
							return provider.Message{Role: "assistant", Content: "settled"}, provider.Meta{}, nil
						}
						select {
						case <-ctx.Done():
							return provider.Message{}, provider.Meta{}, ctx.Err()
						default:
							runtime.Gosched()
						}
					}
				}), nil
			case "queued-child":
				thirdStarted = true
				return childCaptureBackend{text: "must not run"}, nil
			default:
				return nil, errors.New("unexpected child model " + model)
			}
		},
	})
	agent.lastTurnID = "turn_pause_capture"
	agent.beginExecution("request", "request")
	tasks := []Task{
		{Title: "limit", Kind: KindExplain, Model: "limit-child", Effort: EffortMedium},
		{Title: "settle", Kind: KindExplain, Model: "settling-child", Effort: EffortMedium},
		{Title: "queued", Kind: KindExplain, Model: "queued-child", Effort: EffortMedium},
	}
	agent.setExecutionPlan(tasks)
	agent.storeExecution()
	agent.runSpend = &spend{}
	_, err := agent.runTasks(context.Background(), "request", tasks)
	if err == nil {
		t.Fatal("runTasks returned nil, want the child's limit")
	}
	if run := sess.RunState(); run == nil || run.Tasks[2].State != continuity.TaskQueued {
		t.Fatalf("scheduler returned with queued work no longer queued: %+v", run)
	}
	paused, saveErr, ok := agent.pauseIfWaitingHelps(context.Background(), err, "request")
	if !ok || saveErr != nil || paused.Pause.PendingTurn != "request" {
		t.Fatalf("pause = %+v, save error = %v, matched=%v", paused, saveErr, ok)
	}
	if thirdStarted || len(sess.saved()) != 1 {
		t.Fatalf("third started=%v, recoveries=%d", thirdStarted, len(sess.saved()))
	}
}

type childCaptureBackendFunc func(func(string)) (provider.Message, provider.Meta, error)

func (f childCaptureBackendFunc) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return f(onToken)
}

func TestAFailedPauseRecoveryPublishesNoDurablePause(t *testing.T) {
	b := newTestBus(t)
	var attempted []string
	sess := &captureRecoverySession{
		FakeSession: enginetest.NewFakeSession("s_failed_pause_capture", "mock/model"),
		err:         errors.New("recovery volume is full"),
		before:      func(reason string, _ *continuity.Run) { attempted = append(attempted, reason) },
	}
	// A limit as a vendor reports it: an account allowance on the model in
	// use, the kind that pauses. A bare one never reaches the pause at all.
	agent := New(Options{
		Backend: partialFailureBackend{err: provider.Limit{Kind: provider.LimitSubscriptionAllowance,
			Scope: provider.ScopeAccount, Model: "mock/model", ResetAt: time.Now().Add(time.Hour)}},
		Bus: b, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
	})

	err := agent.RunTurn(context.Background(), "wait for reset")
	var paused *PausedError
	if err == nil || errors.As(err, &paused) || !strings.Contains(err.Error(), "recovery volume is full") {
		t.Fatalf("RunTurn error = %v, want a visible recovery failure and no durable-pause claim", err)
	}
	if len(attempted) != 1 || attempted[0] != "pause" {
		t.Fatalf("recovery writes attempted %q, want the one pause write", attempted)
	}
	if sess.Paused() != nil {
		t.Fatalf("failed recovery armed an automatic pause: %+v", sess.Paused())
	}
	if run := sess.RunState(); run != nil && run.LastPause != nil {
		t.Fatalf("failed recovery left a pause in the run: %+v", run.LastPause)
	}
	if durable, interim := sess.SaveCounts(); durable != 0 || interim != 0 {
		t.Fatalf("failed compressed recovery fell back to %d durable/%d interim JSON saves", durable, interim)
	}
	events := bReplay(t, b)
	for _, got := range limitEvents(t, events) {
		if got.Action == "pause" {
			t.Fatalf("failed recovery published a pause: %+v", got)
		}
	}
	for _, env := range events {
		if env.Type == protocol.EventTurnFinished && strings.Contains(string(env.Data), `"paused"`) {
			t.Fatalf("failed recovery published a paused turn: %+v", env)
		}
	}
}
