package engine

// Found by the round-4 vendor-recovery verifier.

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// P-E1. The doc: a held continuation (its model cannot start) stops "until
// /resume can open it". In the same kolk process, once the model can start
// again, /resume must offer the held run.
func TestRound4AHeldContinuationCanBeResumedInTheSameProcess(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r4_held_same", "parent")}
	opts := Options{Backend: editPlanner{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return resumableVendorChild{vendorChild{handle: "accepted-h1", confirmed: true, closed: true, err: vendorLimit, events: finishedTool}}, nil
		}}
	first := New(opts)
	var paused *PausedError
	if err := first.RunTurn(context.Background(), "two edits"); !errors.As(err, &paused) {
		t.Fatalf("not paused: %v", err)
	}
	_ = first.Close()
	signedOut := true
	opts.SubagentBackend = func(_ context.Context, _, _, _ string, caps SubagentCapabilities) (ChatBackend, error) {
		if signedOut {
			return nil, errors.New("the connector is signed out")
		}
		return resumableVendorChild{vendorChild{handle: caps.ProviderState, confirmed: true, closed: true}}, nil
	}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("setup: nothing to resume")
	}
	if err := second.RunTurn(context.Background(), pending); err == nil || !strings.Contains(err.Error(), "cannot start now") {
		t.Fatalf("setup: not held: %v", err)
	}
	signedOut = false
	pending, ok = second.Resume()
	run := sess.RunState()
	t.Logf("second /resume ok=%v; run phase=%s recovery=%q task0=%q", ok, run.Phase, run.Recovery, run.Tasks[0].ProviderState)
	if !ok {
		other := second.RunTurn(context.Background(), "something else")
		t.Fatalf("STUCK: the held run is retained (recovery %q) but /resume in the same process offers nothing; a new request says: %v", run.Recovery, other)
	}
	if err := second.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("resume of the held run: %v", err)
	}
}

// P-E2. Main session, same process: a continuation whose process could not
// start (N2). The journal keeps the conversation; can /resume offer it again
// without a restart?
func TestRound4MainContinuationThatNeverArrivedCanBeResumedInProcess(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r4_main_same", "mock/model")}
	main := &scriptedAttemptsMain{attempts: []attemptScript{
		{closed: true, err: errors.New("the vendor stopped")},
		{neverStarted: true, err: errors.New("claude could not start")},
		{closed: true},
	}}
	opts := Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard}
	agent := New(opts)
	defer agent.Close()
	if err := agent.RunTurn(context.Background(), "do the thing"); err == nil {
		t.Fatal("setup")
	}
	pending, ok := agent.Resume()
	if !ok {
		t.Fatal("setup: first /resume")
	}
	if err := agent.RunTurn(context.Background(), pending); err == nil {
		t.Fatal("setup: continuation that never arrived succeeded")
	}
	run := sess.RunState()
	pending, ok = agent.Resume()
	t.Logf("journal kept: state=%q confirmed=%v closed=%v recovery=%q; second /resume ok=%v", run.Main.ProviderState, run.Main.ProviderConfirmed, run.Main.ProviderTurnClosed, run.Recovery, ok)
	if !ok {
		other := agent.RunTurn(context.Background(), "a different request")
		t.Fatalf("STUCK: retained, continuable work is not offered by /resume in the same process; a new request says: %v", other)
	}
	if err := agent.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("the second /resume did not continue the request: %v", err)
	}
	main.mu.Lock()
	defer main.mu.Unlock()
	if last := main.sent[len(main.sent)-1]; !strings.Contains(last[len(last)-1].Content, "Continue this unfinished request") {
		t.Fatalf("the second /resume sent %q; want a continuation of the saved conversation", last[len(last)-1].Content)
	}
}

// P-E3 (characterization of the workarounds for P-E2): retyping the exact
// request continues it in the same process; a new agent (restart) offers it.
func TestRound4StuckResumeWorkarounds(t *testing.T) {
	for _, how := range []string{"retype", "restart"} {
		t.Run(how, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r4_workaround", "mock/model")}
			main := &scriptedAttemptsMain{attempts: []attemptScript{
				{closed: true, err: errors.New("the vendor stopped")},
				{neverStarted: true, err: errors.New("claude could not start")},
				{closed: true},
			}}
			opts := Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard}
			agent := New(opts)
			defer agent.Close()
			_ = agent.RunTurn(context.Background(), "do the thing")
			pending, _ := agent.Resume()
			_ = agent.RunTurn(context.Background(), pending)
			var err error
			if how == "retype" {
				err = agent.RunTurn(context.Background(), "do the thing")
			} else {
				next := New(opts)
				defer next.Close()
				p, ok := next.Resume()
				if !ok {
					t.Fatal("restart offers nothing")
				}
				err = next.RunTurn(context.Background(), p)
			}
			main.mu.Lock()
			last := main.sent[len(main.sent)-1]
			main.mu.Unlock()
			t.Logf("%s: err=%v calls=%d last=%q", how, err, main.calls, last[len(last)-1].Content)
			if err != nil || main.calls != 3 || !strings.Contains(last[len(last)-1].Content, "Continue this unfinished request") {
				t.Fatalf("%s did not continue", how)
			}
		})
	}
}
