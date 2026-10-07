package engine

// Found by the round-3 vendor-recovery verifier: each was red on the tree it
// reviewed (or, for the concurrency check, guards the restore it introduced).

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// R3-5. Two children continue their saved conversations at the same time.
// Child 1's continuation provably never arrives; child 2's is delivered and
// closed by another limit. The restore for child 1 must leave its journal as
// it was (handle, closure, open turn), and must not touch child 2.
func TestRound3ConcurrentChildrenRestoreOnlyTheAttemptThatNeverArrived(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r3_concurrent", "parent")}
	root := t.TempDir()
	opts := Options{Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard, MaxConcurrentTasks: 2}
	var mu sync.Mutex
	n := 0
	// Both children's calls are sent before either meets its limit, so both
	// are interrupted vendor turns. Without the barrier the first limit can
	// close the pause gate before the second call leaves, and that task was
	// never in flight at all.
	var sent sync.WaitGroup
	sent.Add(2)
	opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		handle := "h1"
		if n == 2 {
			handle = "h2"
		}
		return resumableVendorChild{vendorChild{handle: handle, confirmed: true, closed: true, err: vendorLimit, events: finishedTool,
			record: func([]provider.Message) { sent.Done(); sent.Wait() }}}, nil
	}
	first := New(opts)
	var paused *PausedError
	if err := first.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	_ = first.Close()
	before := sess.RunState()
	for i := range before.Tasks {
		if before.Tasks[i].Status != "" || !before.Tasks[i].ProviderTurnClosed || before.Tasks[i].ProviderState == "" || !before.Tasks[i].ProviderInFlight {
			t.Fatalf("setup: task %d = %+v", i+1, before.Tasks[i])
		}
	}
	opts.SubagentBackend = func(_ context.Context, _, _, _ string, caps SubagentCapabilities) (ChatBackend, error) {
		if caps.ProviderState == before.Tasks[0].ProviderState {
			// Its continuation's process could not take the prompt.
			return resumableVendorChild{vendorChild{handle: caps.ProviderState, neverStarted: true, err: vendorLimit}}, nil
		}
		return resumableVendorChild{vendorChild{handle: caps.ProviderState, confirmed: true, closed: true, err: vendorLimit}}, nil
	}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	after := sess.RunState()
	a0, b0 := after.Tasks[0], before.Tasks[0]
	t.Logf("resume err=%v", err)
	if a0.ProviderState != b0.ProviderState || !a0.ProviderConfirmed || !a0.ProviderTurnClosed || !a0.ProviderInFlight ||
		a0.ProviderNeverStarted || !a0.ProviderDelivered || a0.Status != "" {
		t.Fatalf("RESTORE: task 1 changed although nothing reached its vendor:\nbefore %+v\nafter  %+v", b0, a0)
	}
	a1 := after.Tasks[1]
	if !a1.ProviderDelivered || a1.ProviderNeverStarted || !a1.ProviderTurnClosed || a1.ProviderState != before.Tasks[1].ProviderState {
		t.Fatalf("task 2 lost its own facts: %+v", a1)
	}
}
