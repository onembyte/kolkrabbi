package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"io"
	"testing"
)

func TestVendorFailedToolWritesRecovery(t *testing.T) {
	for _, mode := range []string{ModeCode, ModeAgent} {
		t.Run(mode, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_vendor_error", "mock/model")}
			backend := toolEventBackend{events: []provider.ProgressEvent{
				{Kind: provider.ProgressToolStarted, ID: "failed_read", Name: "Read"},
				{Kind: provider.ProgressToolFinished, ID: "failed_read", Name: "Read", Error: true, Detail: "file absent"},
				{Kind: provider.ProgressToolFinished, ID: "later_success", Name: "Read"},
			}}
			opts := Options{Backend: backend, Model: "mock/model", Mode: mode, Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard}
			wantWrites := 1
			if mode == ModeAgent {
				wantWrites = 2
				opened := 0
				opts.Backend = reviewPlanner{titles: []string{"first", "second"}}
				opts.MaxConcurrentTasks = 1
				opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					if reasons, _ := sess.written(); len(reasons) != opened {
						t.Errorf("opened child %d before previous vendor-tool failure snapshot: %v", opened, reasons)
					}
					opened++
					return backend, nil
				}
			}
			a := New(opts)
			if err := a.RunTurn(context.Background(), "read missing file, then explain"); err != nil {
				t.Fatal(err)
			}
			reasons, _ := sess.written()
			if len(reasons) != wantWrites {
				t.Fatal("exposed failed vendor tool followed by successful response wrote no recovery snapshot")
			}
		})
	}
}

type capturePhaseErrorBackend struct {
	target string
	calls  int
}

func (b *capturePhaseErrorBackend) StreamChat(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return b.StreamChatObserved(ctx, model, msgs, tools, onToken, nil)
}
func (b *capturePhaseErrorBackend) StreamChatObserved(_ context.Context, _ string, msgs []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	b.calls++
	phase := "synthesis"
	if b.calls == 1 {
		phase = "planner"
	}
	if phase == b.target && observe != nil {
		observe(provider.ProgressEvent{Kind: provider.ProgressToolStarted, ID: "bad", Name: "Read"})
		observe(provider.ProgressEvent{Kind: provider.ProgressToolFinished, ID: "bad", Name: "Read", Error: true, Detail: "file missing"})
		// A later success must not erase the need for the error boundary.
		observe(provider.ProgressEvent{Kind: provider.ProgressToolStarted, ID: "good", Name: "Read"})
		observe(provider.ProgressEvent{Kind: provider.ProgressToolFinished, ID: "good", Name: "Read"})
	}
	text := "synthesis done"
	if phase == "planner" {
		text = `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`
	}
	return provider.Message{Role: "assistant", Content: text}, provider.Meta{}, nil
}
func TestVendorFailedToolPlannerSynthesisRecovery(t *testing.T) {
	for _, phase := range []string{"planner", "synthesis"} {
		t.Run(phase, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_vendor_phase", "mock/model")}
			b := &capturePhaseErrorBackend{target: phase}
			sess.at = func(_ string, run *continuity.Run) {
				if phase == "planner" && (run.Phase != "tasks" || len(run.Tasks) != 2 || run.Tasks[0].State != continuity.TaskQueued) {
					t.Errorf("planner failure snapshot after work admission: %+v", run)
				}
				if phase == "synthesis" {
					messages := sess.GetMessages()
					if messages[len(messages)-1].Content != "synthesis done" || run.Main.ProviderInFlight {
						t.Errorf("synthesis response not committed at recovery: %+v", run)
					}
				}
			}
			a := New(Options{Backend: b, Model: "mock/model", Mode: ModeAgent, Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1, SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
				return &answeringBackend{}, nil
			}})
			if err := a.RunTurn(context.Background(), "two separate tasks"); err != nil {
				t.Fatal(err)
			}
			reasons, _ := sess.written()
			if len(reasons) == 0 {
				t.Fatal("failed vendor tool in " + phase + " followed success wrote no recovery")
			}
		})
	}
}

func TestVendorToolFailureSaveFailureStopsFurtherWork(t *testing.T) {
	for _, phase := range []string{"planner", "synthesis", "child", "direct"} {
		t.Run(phase, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_vendor_failed_write", "mock/model"), fail: map[string]error{"error": fmt.Errorf("disk unavailable")}}
			b := &capturePhaseErrorBackend{target: phase}
			opts := Options{Backend: b, Model: "mock/model", Mode: ModeAgent, Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1}
			opened := 0
			vendor := toolEventBackend{events: []provider.ProgressEvent{{Kind: provider.ProgressToolFinished, ID: "bad", Name: "Read", Error: true}}}
			opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
				opened++
				if phase == "child" {
					return vendor, nil
				}
				return &answeringBackend{}, nil
			}
			if phase == "direct" {
				opts.Mode = ModeCode
				opts.Backend = vendor
			}
			err := New(opts).RunTurn(context.Background(), "work")
			var lost *RecoverySaveError
			if !errors.As(err, &lost) {
				t.Fatalf("lost write error not returned: %v", err)
			}
			reasons, _ := sess.written()
			if len(reasons) != 1 {
				t.Errorf("writes=%v", reasons)
			}
			wantChildren := map[string]int{"planner": 0, "synthesis": 2, "child": 1, "direct": 0}[phase]
			if opened != wantChildren {
				t.Errorf("opened %d children, want %d", opened, wantChildren)
			}
			if (phase == "planner" || phase == "child") && b.calls != 1 {
				t.Errorf("failed write admitted synthesis: %d calls", b.calls)
			}
		})
	}
}
