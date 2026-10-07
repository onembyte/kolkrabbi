package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type captureToolFailureBackend struct {
	path                     string
	calls                    int
	sess                     *reviewSession
	continuedWithoutRecovery bool
}

func (b *captureToolFailureBackend) StreamChat(_ context.Context, _ string, messages []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	if b.calls == 1 {
		args, _ := json.Marshal(map[string]string{"path": b.path})
		return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "read-missing", Function: provider.FunctionCall{Name: "read_file", Arguments: string(args)}}}}, provider.Meta{}, nil
	}
	foundError := false
	for _, m := range messages {
		if m.Role == "tool" && strings.HasPrefix(m.Content, "Error:") {
			foundError = true
		}
	}
	if !foundError {
		return provider.Message{}, provider.Meta{}, errors.New("probe setup: missing native tool error")
	}
	reasons, _ := b.sess.written()
	b.continuedWithoutRecovery = len(reasons) == 0
	return provider.Message{Role: "assistant", Content: "handled the missing file"}, provider.Meta{}, nil
}

func TestNativeToolErrorsAreRecoveryBoundaries(t *testing.T) {
	for _, mode := range []string{ModeCode, ModeAgent} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_native_probe", "mock/model")}
			backend := &captureToolFailureBackend{path: root + "/does-not-exist", sess: sess}
			opts := Options{Backend: backend, Model: "mock/model", Mode: mode, Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard}
			var children []*captureToolFailureBackend
			if mode == ModeAgent {
				opts.Backend = reviewPlanner{titles: []string{"first", "second"}}
				opts.MaxConcurrentTasks = 1
				opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					child := &captureToolFailureBackend{path: root + "/does-not-exist", sess: sess}
					children = append(children, child)
					return child, nil
				}
			}
			agent := New(opts)
			if err := agent.RunTurn(context.Background(), "read a missing file then explain"); err != nil {
				t.Fatal(err)
			}
			reasons, _ := sess.written()
			if mode == ModeCode && backend.continuedWithoutRecovery {
				t.Error("main called provider again before saving the failed tool result")
			}
			if len(reasons) == 0 {
				t.Fatalf("native tool failed and model then succeeded, but no recovery boundary was written; main continuation without recovery=%v, child count=%d", backend.continuedWithoutRecovery, len(children))
			}
		})
	}
}

func TestMainHandleComesFromActualRoute(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_route_probe", "alternate/model")}
	agent := New(Options{
		Backend: childCaptureBackend{text: "unused", handle: "wrong-default-handle", confirmed: true},
		Routes:  map[string]ChatBackend{"alternate": childCaptureBackend{text: "routed partial", handle: "actual-route-handle", confirmed: true, err: errors.New("routed malformed response")}},
		Model:   "alternate/model", Mode: ModeCode, Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
	})
	if err := agent.RunTurn(context.Background(), "continue routed work"); err == nil {
		t.Fatal("expected routed error")
	}
	_, runs := sess.written()
	if len(runs) != 1 {
		t.Fatalf("got %d recovery writes", len(runs))
	}
	main := runs[0].Main
	if main.PartialOutput != "routed partial" {
		t.Fatalf("route setup failed: %+v", main)
	}
	if main.ProviderState != "actual-route-handle" || !main.ProviderConfirmed {
		t.Fatalf("actual route's partial saved with handle=%q confirmed=%v; want actual-route-handle true", main.ProviderState, main.ProviderConfirmed)
	}
}

// An error result is a committed native-tool boundary, including when a
// second tool in the same response has not run yet.
func TestMainToolErrorSavesBeforeTheNextToolAndStopsOnSaveFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			root := t.TempDir()
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_batch", "mock/model")}
			if fail {
				sess.fail = map[string]error{"error": errors.New("disk unavailable")}
			}
			var calls int
			backend := captureMessagesBackend(func(messages []provider.Message) (provider.Message, error) {
				calls++
				if calls == 1 {
					return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{
						{ID: "missing", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"` + root + `/missing"}`}},
						{ID: "write", Function: provider.FunctionCall{Name: "write_file", Arguments: `{"path":"` + root + `/created","content":"done"}`}},
					}}, nil
				}
				return provider.Message{Role: "assistant", Content: "finished"}, nil
			})
			sess.at = func(reason string, run *continuity.Run) {
				if reason != "error" || run.Main.Rounds != 1 || run.Main.Loop.Last == "" || run.Main.ProviderInFlight || run.Main.SafeBoundary != "tool results committed" {
					t.Errorf("incomplete tool boundary: reason=%q main=%+v", reason, run.Main)
				}
				msgs := sess.GetMessages()
				last := msgs[len(msgs)-1]
				if last.Role != "tool" || last.ToolCallID != "missing" || !strings.HasPrefix(last.Content, "Error:") {
					t.Errorf("failed result missing at write: %+v", last)
				}
				if pending := pendingToolCalls(msgs); len(pending) != 1 || pending[0].ID != "write" {
					t.Errorf("pending calls=%+v", pending)
				}
				if _, err := os.Stat(root + "/created"); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("next tool ran before snapshot: %v", err)
				}
			}
			agent := New(Options{Backend: backend, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
				Sess: sess, Root: root, Permission: PermissionFullAuto, Out: io.Discard})
			err := agent.RunTurn(context.Background(), "read then write")
			reasons, _ := sess.written()
			if len(reasons) != 1 {
				t.Errorf("writes=%v", reasons)
			}
			if fail {
				var lost *RecoverySaveError
				if !errors.As(err, &lost) || calls != 1 {
					t.Errorf("failed write admitted more work: err=%v calls=%d", err, calls)
				}
				if _, err := os.Stat(root + "/created"); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("failed snapshot admitted write tool: %v", err)
				}
			} else if err != nil || calls != 2 {
				t.Errorf("successful save failed to continue: %v calls=%d", err, calls)
			}
		})
	}
}

type captureMessagesBackend func([]provider.Message) (provider.Message, error)

func (b captureMessagesBackend) StreamChat(_ context.Context, _ string, messages []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	msg, err := b(messages)
	return msg, provider.Meta{}, err
}

// The repairing child stays live while a sibling finishes. Its native tool
// error must close admission then, not only when that child eventually returns.
func TestChildToolErrorClosesAdmissionUntilTheDrainedSave(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			root := t.TempDir()
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_drain", "mock/model")}
			if fail {
				sess.fail = map[string]error{"error": errors.New("disk unavailable")}
			}
			repairing, releaseRepair, siblingClosed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var opened atomic.Int32
			var early atomic.Bool
			sess.at = func(reason string, run *continuity.Run) {
				select {
				case <-releaseRepair:
				default:
					t.Error("wrote recovery with child still running")
				}
				if opened.Load() != 2 || len(run.Tasks) != 3 || run.Tasks[2].State != continuity.TaskQueued {
					t.Errorf("admitted queued work before save: opened=%d run=%+v", opened.Load(), run)
				}
				for i := 0; i < 2; i++ {
					if run.Tasks[i].State != continuity.TaskSettled || run.Tasks[i].Result == "" {
						t.Errorf("child %d not settled: %+v", i, run.Tasks[i])
					}
				}
				if !strings.Contains(fmt.Sprint(run.Tasks[0].Messages)+fmt.Sprint(run.Tasks[1].Messages), "Error:") {
					t.Error("lost failed native-tool result")
				}
			}
			agent := New(Options{Backend: reviewPlanner{titles: []string{"repair", "sibling", "queued"}}, Model: "mock/model", Mode: ModeAgent,
				Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard, MaxConcurrentTasks: 2,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					n := opened.Add(1)
					if n == 1 {
						calls := 0
						return captureMessagesBackend(func(_ []provider.Message) (provider.Message, error) {
							calls++
							if calls == 1 {
								return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "missing", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"` + root + `/missing"}`}}}}, nil
							}
							close(repairing)
							select {
							case <-releaseRepair:
							case <-ctx.Done():
								return provider.Message{}, ctx.Err()
							}
							return provider.Message{Role: "assistant", Content: "repaired"}, nil
						}), nil
					}
					if n == 2 {
						return &captureClosingBackend{captureMessagesBackend: captureMessagesBackend(func(_ []provider.Message) (provider.Message, error) {
							select {
							case <-repairing:
							case <-ctx.Done():
								return provider.Message{}, ctx.Err()
							}
							return provider.Message{Role: "assistant", Content: "sibling done"}, nil
						}), closed: siblingClosed}, nil
					}
					if reasons, _ := sess.written(); len(reasons) == 0 {
						early.Store(true)
					}
					return &answeringBackend{}, nil
				},
			})
			done := make(chan error, 1)
			go func() { done <- agent.RunTurn(ctx, "three independent tasks") }()
			select {
			case <-siblingClosed:
			case <-ctx.Done():
				t.Fatal("sibling failed to finish")
			}
			// Wait until the scheduler commits the fast sibling. Its next
			// iteration must leave the queued task alone while repair is live.
			for {
				run := sess.RunState()
				if run != nil && (run.Tasks[0].Status != "" || run.Tasks[1].Status != "") {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("scheduler never recorded sibling")
				default:
					runtime.Gosched()
				}
			}
			// Let the scheduler execute its next admission pass before releasing
			// the slow child; only the negative assertion needs this grace period.
			time.Sleep(20 * time.Millisecond)
			close(releaseRepair)
			err := <-done
			if early.Load() {
				t.Error("queued task opened while tool-error child was still draining")
			}
			reasons, _ := sess.written()
			if len(reasons) != 1 {
				t.Errorf("writes=%v", reasons)
			}
			if fail {
				var lost *RecoverySaveError
				if !errors.As(err, &lost) || opened.Load() != 2 {
					t.Errorf("save failure admitted work: err=%v opened=%d", err, opened.Load())
				}
			} else if err != nil || opened.Load() != 3 {
				t.Errorf("save failed to reopen admission: err=%v opened=%d", err, opened.Load())
			}
		})
	}
}

type captureClosingBackend struct {
	captureMessagesBackend
	closed chan struct{}
}

func (b *captureClosingBackend) Close() error { close(b.closed); return nil }

func TestRoutedPlannerAndSynthesisRetainTheirOwnHandle(t *testing.T) {
	for _, phase := range []string{"plan", "synthesis"} {
		t.Run(phase, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_routed_phase", "alternate/model")}
			backend := &capturePhaseBackend{failAt: phase}
			agent := New(Options{Backend: childCaptureBackend{handle: "wrong-default", confirmed: true},
				Routes: map[string]ChatBackend{"alternate": backend}, Model: "alternate/model", Mode: ModeAgent,
				Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					return &answeringBackend{}, nil
				},
			})
			if err := agent.RunTurn(context.Background(), "two things"); err == nil {
				t.Fatal("expected phase error")
			}
			_, runs := sess.written()
			if len(runs) != 1 {
				t.Fatalf("writes=%d", len(runs))
			}
			run := runs[0]
			if run.Phase != phase || run.Main.ProviderState != phase+"-handle" || !run.Main.ProviderConfirmed || run.Main.PartialOutput != phase+" partial" {
				t.Fatalf("wrong routed phase state: %+v", run)
			}
		})
	}
}

type capturePhaseBackend struct{ failAt, handle string }

func (b *capturePhaseBackend) ProviderHandle() string        { return b.handle }
func (b *capturePhaseBackend) ProviderHandleConfirmed() bool { return b.handle != "" }
func (b *capturePhaseBackend) StreamChat(_ context.Context, _ string, messages []provider.Message, _ []provider.Tool, token func(string)) (provider.Message, provider.Meta, error) {
	phase := "synthesis"
	for _, message := range messages {
		if strings.Contains(message.Content, "Decompose the request") {
			phase = "plan"
		}
	}
	b.handle = phase + "-handle"
	if phase == b.failAt {
		token(phase + " partial")
		return provider.Message{}, provider.Meta{}, errors.New("broken " + phase)
	}
	return provider.Message{Role: "assistant", Content: `[{"title":"one","kind":"explain"},{"title":"two","kind":"explain"}]`}, provider.Meta{}, nil
}

func TestNativeRouteCannotInheritAVendorHandle(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_native_route", "alternate/model")}
	sess.SetProviderStateName("stale-session-handle")
	agent := New(Options{Backend: childCaptureBackend{handle: "wrong-default", confirmed: true},
		Routes: map[string]ChatBackend{"alternate": partialFailureBackend{partial: "native partial", err: errors.New("native failed")}},
		Model:  "alternate/model", Mode: ModeCode, Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
	})
	if err := agent.RunTurn(context.Background(), "native work"); err == nil {
		t.Fatal("expected failure")
	}
	_, runs := sess.written()
	if len(runs) != 1 {
		t.Fatalf("writes=%d", len(runs))
	}
	if main := runs[0].Main; main.ProviderState != "" || main.ProviderConfirmed {
		t.Fatalf("native call inherited vendor handle: %+v", main)
	}
}

func TestMainHandleRetainsActualModelBinding(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_model_binding", "mock/ceiling")}
	a := New(Options{Backend: childCaptureBackend{text: "unused", handle: "default", confirmed: true}, Routes: map[string]ChatBackend{"alternate": childCaptureBackend{text: "alternate planning partial", handle: "alternate-thread", confirmed: true, err: io.ErrUnexpectedEOF}}, Model: "mock/ceiling", ConnectorName: func(string) string { return "alternate-vendor" }, Slots: map[string]string{SlotOrchestrator: "alternate/planner"}, Mode: ModeAgent, Effort: EffortHigh, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
	if err := a.RunTurn(context.Background(), "plan two tasks"); err == nil {
		t.Fatal("expected planning error")
	}
	_, runs := sess.written()
	if len(runs) != 1 {
		t.Fatalf("runs=%d", len(runs))
	}
	run := runs[0]
	if run.Main.ProviderState != "alternate-thread" {
		t.Fatalf("setup wrong handle %+v", run.Main)
	}
	if run.Main.Model != "alternate/planner" || run.Main.Vendor != "alternate-vendor" || run.Main.Effort != EffortHigh {
		t.Fatalf("actual model binding missing: ceiling=%q main.Model=%q main.Vendor=%q handle=%q", run.Model, run.Main.Model, run.Main.Vendor, run.Main.ProviderState)
	}
}

type captureDrainingBackend struct {
	entered, release chan struct{}
	fail             bool
}

func (b *captureDrainingBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	if b.fail {
		return provider.Message{}, provider.Meta{}, errors.New("broken vendor frame")
	}
	return provider.Message{Role: "assistant", Content: "edited"}, provider.Meta{}, nil
}
func (b *captureDrainingBackend) Close() error { close(b.entered); <-b.release; return nil }
func TestChildErrorsCloseAdmissionBeforeBackendClose(t *testing.T) {
	for _, landing := range []bool{false, true} {
		t.Run(fmt.Sprint(landing), func(t *testing.T) { testChildErrorClosesAdmissionBeforeCleanup(t, landing) })
	}
}

func testChildErrorClosesAdmissionBeforeCleanup(t *testing.T, landing bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered, release, third := make(chan struct{}), make(chan struct{}), make(chan struct{})
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_final_close", "parent")}
	a := New(Options{Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 2, SubagentBackend: func(_ context.Context, model, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
		switch model {
		case "failed":
			return &captureDrainingBackend{entered: entered, release: release, fail: !landing}, nil
		case "sibling":
			return contextChildBackendFunc(func(ctx context.Context, _ func(string)) (provider.Message, provider.Meta, error) {
				select {
				case <-entered:
				case <-ctx.Done():
					return provider.Message{}, provider.Meta{}, ctx.Err()
				}
				return provider.Message{Role: "assistant", Content: "sibling finished"}, provider.Meta{}, nil
			}), nil
		case "queued":
			close(third)
			return &answeringBackend{}, nil
		default:
			return nil, errors.New("unexpected model")
		}
	}})
	a.beginExecution("test", "test")
	tasks := []Task{{Title: "failing", Kind: KindExplain, Model: "failed", Effort: EffortMedium}, {Title: "sibling", Kind: KindExplain, Model: "sibling", Effort: EffortMedium}, {Title: "queued", Kind: KindExplain, Model: "queued", Effort: EffortMedium}}
	if landing {
		tasks[0].Kind = KindEdit
		a.Isolator = &fakeIsolator{failLanding: 1}
	}
	a.setExecutionPlan(tasks)
	a.runSpend = &spend{}
	sess.at = func(_ string, run *continuity.Run) {
		if run.Tasks[0].State != continuity.TaskSettled || run.Tasks[1].State != continuity.TaskSettled || run.Tasks[2].State != continuity.TaskQueued {
			t.Errorf("incorrect drained state: %+v", run)
		}
		select {
		case <-third:
			t.Error("queued child opened before recovery")
		default:
		}
	}
	done := make(chan error, 1)
	go func() { _, err := a.runTasks(ctx, "test", tasks); done <- err }()
	early := false
	select {
	case <-third:
		early = true
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		t.Error("timeout before admission decision")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if early {
		t.Fatal("queued child admitted after ordinary child error while backend Close still blocked and no recovery saved")
	}
	reasons, _ := sess.written()
	if len(reasons) != 1 {
		t.Errorf("writes=%v", reasons)
	}
}

func TestBudgetBoundaryIsCaptured(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_final_budget", "mock/model")}
	a := New(Options{Backend: &answeringBackend{}, Model: "mock/model", Mode: ModeAgent, Effort: EffortMedium, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxRunCostUSD: 1})
	a.beginExecution("work", "work")
	tasks := []Task{{Title: "over budget", Kind: KindExplain, Model: "mock/model", Effort: EffortMedium}, {Title: "also over budget", Kind: KindExplain, Model: "mock/model", Effort: EffortMedium}}
	a.setExecutionPlan(tasks)
	a.runSpend = &spend{usd: 2, limit: 1, calls: 1, worst: 2}
	outcomes, err := a.runTasks(context.Background(), "work", tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 || outcomes[0].Status != statusOverBudget || outcomes[1].Status != statusOverBudget {
		t.Fatalf("probe setup: %+v", outcomes)
	}
	reasons, runs := sess.written()
	if len(reasons) != 1 || reasons[0] != "limit" {
		t.Fatal("budget limit exposed as task outcome but no recovery snapshot written before synthesis")
	}
	if len(runs[0].Tasks) != 2 || runs[0].Tasks[0].Status != statusOverBudget.String() || runs[0].Tasks[1].Status != statusOverBudget.String() {
		t.Fatalf("wrong budget boundary %+v", runs[0])
	}
}

func TestBudgetRecoveryControlsSynthesis(t *testing.T) {
	for _, mode := range []string{"saved", "failed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_budget_synthesis", "mock/model")}
			backend := &captureBudgetBackend{}
			if mode == "failed" {
				sess.fail = map[string]error{"limit": errors.New("disk unavailable")}
			}
			if mode == "cancelled" {
				backend.cancel = cancel
			}
			agent := New(Options{Backend: backend, Model: "mock/model", Mode: ModeAgent, Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxRunCostUSD: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					t.Error("budget admitted child")
					return &answeringBackend{}, nil
				}})
			err := agent.RunTurn(ctx, "two tasks")
			reasons, _ := sess.written()
			switch mode {
			case "saved":
				if err != nil || backend.calls != 2 || len(reasons) != 1 || reasons[0] != "limit" {
					t.Errorf("successful boundary err=%v calls=%d writes=%v", err, backend.calls, reasons)
				}
			case "failed":
				var lost *RecoverySaveError
				if !errors.As(err, &lost) || backend.calls != 1 || len(reasons) != 1 {
					t.Errorf("save failure admitted synthesis: err=%v calls=%d writes=%v", err, backend.calls, reasons)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) || backend.calls != 1 || len(reasons) != 0 {
					t.Errorf("cancel persisted/restarted work: err=%v calls=%d writes=%v", err, backend.calls, reasons)
				}
			}
		})
	}
}

type captureBudgetBackend struct {
	calls  int
	cancel context.CancelFunc
}

func (b *captureBudgetBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	if b.cancel != nil {
		b.cancel()
	}
	if b.calls == 1 {
		return provider.Message{Role: "assistant", Content: `[{"title":"one","kind":"explain"},{"title":"two","kind":"explain"}]`}, provider.Meta{Cost: 2}, nil
	}
	return provider.Message{Role: "assistant", Content: "budget stopped the tasks"}, provider.Meta{}, nil
}
