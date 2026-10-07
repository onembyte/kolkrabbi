package engine

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// failedRecoveryHoldsJSON runs a turn whose recovery write fails and reports
// whether any ordinary JSON write happened after that failure.
func failedRecoveryHoldsJSON(t *testing.T, sess *reviewSession, run func() error) {
	t.Helper()
	var atDurable, atInterim atomic.Int32
	atDurable.Store(-1)
	sess.at = func(string, *continuity.Run) {
		d, i := sess.SaveCounts()
		atDurable.Store(int32(d))
		atInterim.Store(int32(i))
	}
	var lost *RecoverySaveError
	if err := run(); !errors.As(err, &lost) {
		t.Fatalf("RunTurn = %v, want the failed recovery write", err)
	}
	if atDurable.Load() < 0 {
		t.Fatal("no recovery write was attempted")
	}
	if d, i := sess.SaveCounts(); d != int(atDurable.Load()) || i != int(atInterim.Load()) {
		t.Fatalf("the turn flushed JSON after its recovery write failed: durable %d->%d, interim %d->%d",
			atDurable.Load(), d, atInterim.Load(), i)
	}
}

// A recovery write that failed is not followed by a plain JSON copy of the
// same boundary on the way out of the turn, whichever path failed: the copy
// would look like the recovery point the user was just told is missing.
func TestAFailedRecoveryWriteIsNeverFollowedByAJSONFlush(t *testing.T) {
	disk := errors.New("recovery volume is full")
	limit := provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Model: "child", ResetAt: time.Now().Add(time.Hour)}
	for _, c := range []struct {
		name  string
		fail  string
		model string
		build func(t *testing.T, sess *reviewSession) *Agent
	}{
		{"an ordinary provider error", "error", "mock/model", func(t *testing.T, sess *reviewSession) *Agent {
			return New(Options{Backend: partialFailureBackend{partial: "half", err: errors.New("malformed")},
				Model: "mock/model", Mode: ModeCode, Effort: EffortMedium, Permission: PermissionFullAuto,
				Sess: sess, Root: t.TempDir(), Out: io.Discard})
		}},
		{"a native tool error", "error", "mock/model", func(t *testing.T, sess *reviewSession) *Agent {
			root := t.TempDir()
			calls := 0
			return New(Options{Backend: captureMessagesBackend(func([]provider.Message) (provider.Message, error) {
				calls++
				if calls == 1 {
					return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "missing",
						Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"` + root + `/missing"}`}}}}, nil
				}
				return provider.Message{Role: "assistant", Content: "finished"}, nil
			}), Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard})
		}},
		{"a child error at the scheduler", "error", "parent", func(t *testing.T, sess *reviewSession) *Agent {
			var n atomic.Int32
			return New(Options{Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent,
				Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					if n.Add(1) == 1 {
						return childCaptureBackend{text: "partial", err: errors.New("child failed")}, nil
					}
					return childCaptureBackend{text: "second"}, nil
				}})
		}},
		{"the budget boundary", "limit", "mock/model", func(t *testing.T, sess *reviewSession) *Agent {
			return New(Options{Backend: &captureBudgetBackend{}, Model: "mock/model", Mode: ModeAgent, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxRunCostUSD: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					return &answeringBackend{}, nil
				}})
		}},
		{"a vendor tool failure then an answer", "error", "mock/model", func(t *testing.T, sess *reviewSession) *Agent {
			return New(Options{Backend: toolEventBackend{events: []provider.ProgressEvent{{Kind: provider.ProgressToolFinished, ID: "bad", Name: "Read", Error: true}}},
				Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
		}},
		{"a planner vendor tool failure", "error", "mock/model", func(t *testing.T, sess *reviewSession) *Agent {
			return New(Options{Backend: &capturePhaseErrorBackend{target: "planner"}, Model: "mock/model", Mode: ModeAgent,
				Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					return &answeringBackend{}, nil
				}})
		}},
		{"a failed pause", "pause", "parent", func(t *testing.T, sess *reviewSession) *Agent {
			return New(Options{Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent,
				Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					return childCaptureBackend{text: "half", err: limit}, nil
				}})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_hold", c.model), fail: map[string]error{c.fail: disk}}
			agent := c.build(t, sess)
			failedRecoveryHoldsJSON(t, sess, func() error { return agent.RunTurn(context.Background(), "go") })
		})
	}
}

// The hold ends with the turn: the state stays in memory, and the next
// ordinary save after the turn writes it, without any boundary's claim.
func TestOrdinaryWritesResumeAfterTheTurnWhoseRecoveryFailed(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_hold_ends", "mock/model"),
		fail: map[string]error{"error": errors.New("recovery volume is full")}}
	agent := New(Options{Backend: partialFailureBackend{partial: "half", err: errors.New("malformed")},
		Model: "mock/model", Mode: ModeCode, Effort: EffortMedium, Permission: PermissionFullAuto,
		Sess: sess, Root: t.TempDir(), Out: io.Discard})
	var lost *RecoverySaveError
	if err := agent.RunTurn(context.Background(), "go"); !errors.As(err, &lost) {
		t.Fatalf("RunTurn = %v, want the failed recovery write", err)
	}
	before, _ := sess.SaveCounts()
	agent.saveFor(saveChainSwitch)
	if after, _ := sess.SaveCounts(); after != before+1 {
		t.Fatalf("a save after the turn wrote %d times, want once: the hold outlived its turn", after-before)
	}
}

type cancelOnToolRoundBackend struct {
	cancel context.CancelFunc
	root   string
	calls  int
}

func (b *cancelOnToolRoundBackend) StreamChat(ctx context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	if b.calls > 1 {
		return provider.Message{}, provider.Meta{}, ctx.Err()
	}
	// The user cancels while the tool round is being dispatched.
	b.cancel()
	return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "gone",
		Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"` + b.root + `/missing"}`}}}}, provider.Meta{}, nil
}

type cancelAfterVendorFailureBackend struct{ cancel context.CancelFunc }

func (b cancelAfterVendorFailureBackend) StreamChat(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return b.StreamChatObserved(ctx, model, msgs, tools, onToken, nil)
}

func (b cancelAfterVendorFailureBackend) StreamChatObserved(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	if observe != nil {
		observe(provider.ProgressEvent{Kind: provider.ProgressToolStarted, ID: "bad", Name: "Read"})
		observe(provider.ProgressEvent{Kind: provider.ProgressToolFinished, ID: "bad", Name: "Read", Error: true})
	}
	b.cancel()
	return provider.Message{Role: "assistant", Content: "answered anyway"}, provider.Meta{}, nil
}

// A cancel the user asked for stays terminal on the tool-failure paths too:
// a failed native tool or a failed vendor tool the cancel lands on writes no
// recovery point for work the user withdrew.
func TestACancelOnAToolFailureWritesNoRecovery(t *testing.T) {
	t.Run("a native tool error", func(t *testing.T) {
		root := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_cancel_tool", "mock/model")}
		agent := New(Options{Backend: &cancelOnToolRoundBackend{cancel: cancel, root: root}, Model: "mock/model", Mode: ModeCode,
			Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard})
		_ = agent.RunTurn(ctx, "read")
		if reasons, _ := sess.written(); len(reasons) != 0 {
			t.Fatalf("a user cancel wrote recovery points %v", reasons)
		}
	})
	t.Run("a vendor tool failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_cancel_vendor", "mock/model")}
		agent := New(Options{Backend: cancelAfterVendorFailureBackend{cancel: cancel}, Model: "mock/model", Mode: ModeCode,
			Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
		_ = agent.RunTurn(ctx, "read")
		if reasons, _ := sess.written(); len(reasons) != 0 {
			t.Fatalf("a user cancel wrote recovery points %v", reasons)
		}
	})
}
