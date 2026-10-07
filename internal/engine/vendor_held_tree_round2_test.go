package engine

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// P6 (control for mutant X14). A task held because its saved conversation
// cannot open keeps its own tree: nothing is landed or released.
func TestRound2AHeldContinuationKeepsItsTree(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_probe_held_tree", "parent")}
	iso := &fakeIsolator{}
	root := t.TempDir()
	opts := Options{Backend: editPlanner{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Isolator: iso, Root: root, Out: io.Discard, MaxConcurrentTasks: 1,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return resumableVendorChild{vendorChild{handle: "accepted-h1", confirmed: true, closed: true, err: vendorLimit, events: finishedTool}}, nil
		}}
	first := New(opts)
	var paused *PausedError
	if err := first.RunTurn(context.Background(), "two edits"); !errors.As(err, &paused) {
		t.Fatalf("not paused: %v", err)
	}
	_ = first.Close()
	run := sess.RunState()
	tree := run.Tasks[0].Workspace
	if tree == "" {
		t.Fatalf("setup: task 1 has no tree: %+v", run.Tasks[0])
	}
	_, landedBefore, releasedBefore := iso.counts()
	opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
		return nil, errors.New("the connector is signed out")
	}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	if err == nil {
		t.Fatal("setup: the held run was not stopped")
	}
	iso.mu.Lock()
	defer iso.mu.Unlock()
	for _, dir := range iso.released[releasedBefore:] {
		if dir == tree {
			t.Fatalf("LEAK: the held task's tree %s was released (err=%v)", tree, err)
		}
	}
	for _, dir := range iso.landed[landedBefore:] {
		if dir == tree {
			t.Fatalf("the held task's tree %s was landed (err=%v)", tree, err)
		}
	}
}
