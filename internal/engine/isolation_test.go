package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// fakeIsolator stands in for the worktree plumbing (plan 36): it hands out a
// directory per task and records what the engine asked of it.
type fakeIsolator struct {
	mu      sync.Mutex
	isolate error
	// failLanding fails the nth landing, counted from one; zero fails none.
	failLanding int
	isolated    []string
	landed      []string
	released    []string
}

func (f *fakeIsolator) Isolate(_ context.Context, root, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isolate != nil {
		return "", f.isolate
	}
	dir := filepath.Join(root, ".worktrees", name)
	f.isolated = append(f.isolated, dir)
	return dir, nil
}

func (f *fakeIsolator) Land(_ context.Context, _, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.landed = append(f.landed, dir)
	if f.failLanding == len(f.landed) {
		return errors.New("git apply: patch failed: a.txt:1")
	}
	return nil
}

func (f *fakeIsolator) Release(_ context.Context, _, dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, dir)
}

// counts reads the fake under its lock.
func (f *fakeIsolator) counts() (isolated, landed, released int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.isolated), len(f.landed), len(f.released)
}

func twoEdits() []Task {
	return []Task{
		{Title: "edit one", Kind: KindEdit, Model: "gpt-5.6-luna"},
		{Title: "edit two", Kind: KindEdit, Model: "gpt-5.6-luna"},
	}
}

// Two writers with an isolator run at the same time, each in its own tree,
// and each tree is landed and released. This is the queue the owner watched
// on 2026-09-06, no longer a queue.
func TestIsolatedWritersRunTogetherAndEachTreeLands(t *testing.T) {
	step := enginetest.Step{Text: "edited", Delay: 80 * time.Millisecond}
	srv := enginetest.New(step, step)
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 2
	iso := &fakeIsolator{}
	a.Isolator = iso
	ckpt := &enginetest.FakeCheckpointer{}
	a.Ckpt = ckpt
	var seen statusRecorder
	a.Subagents = seen.record

	tasks := twoEdits()
	outcomes, err := a.runTasks(context.Background(), "two edits", tasks)
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.MaxInFlight(); got < 2 {
		t.Fatalf("max in flight = %d; isolated writers still queued", got)
	}
	for i, o := range outcomes {
		if o.Status != statusDone {
			t.Fatalf("task %d = %+v", i+1, o)
		}
	}
	for _, status := range seen.statuses {
		if strings.Contains(status.Step, "shared-tree writer") {
			t.Fatalf("an isolated writer waited for the shared tree: %+v", status)
		}
	}
	if isolated, landed, released := iso.counts(); isolated != 2 || landed != 2 || released != 2 {
		t.Fatalf("isolated %d, landed %d, released %d, want 2 each", isolated, landed, released)
	}
	for i := range tasks {
		if tasks[i].Workspace == "" || !strings.Contains(tasks[i].Workspace, ".worktrees") {
			t.Fatalf("task %d ran in %q, not its own tree", i+1, tasks[i].Workspace)
		}
	}
	if tasks[0].Workspace == tasks[1].Workspace {
		t.Fatal("both tasks were given the same tree")
	}
	// The snapshot brackets the landing, once per task: the user's tree
	// changes only then.
	if len(ckpt.Tasks) != 2 || len(ckpt.Ended) != 2 {
		t.Fatalf("snapshots begun %d ended %d, want 2 and 2 around the landings", len(ckpt.Tasks), len(ckpt.Ended))
	}
	assertStatusStep(t, seen.task(1), SubagentWorking, "landing")
}

// When a tree cannot be isolated the task still runs, in the shared tree, one
// writer at a time as before, and its row says why.
func TestAnIsolationFailureFallsBackToTheSharedTreeOneAtATime(t *testing.T) {
	step := enginetest.Step{Text: "edited", Delay: 60 * time.Millisecond}
	srv := enginetest.New(step, step)
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 2
	iso := &fakeIsolator{isolate: errors.New("git worktree: not a repository")}
	a.Isolator = iso
	var seen statusRecorder
	a.Subagents = seen.record

	outcomes, err := a.runTasks(context.Background(), "two edits", twoEdits())
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range outcomes {
		if o.Status != statusDone {
			t.Fatalf("task %d = %+v", i+1, o)
		}
	}
	if got := srv.MaxInFlight(); got != 1 {
		t.Fatalf("max in flight = %d; two writers shared one tree", got)
	}
	assertStatusStep(t, seen.task(1), SubagentWorking, "shared tree: git worktree: not a repository")
	if _, landed, released := iso.counts(); landed != 0 || released != 0 {
		t.Fatalf("nothing was isolated, yet landed %d released %d", landed, released)
	}
}

// A patch that will not land fails its task, names the reason, and leaves the
// other tasks' results alone. The tree is released either way.
func TestALandingThatDoesNotFitFailsOnlyItsTask(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "edited one"}, enginetest.Step{Text: "edited two"})
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 1
	iso := &fakeIsolator{failLanding: 2}
	a.Isolator = iso

	outcomes, err := a.runTasks(context.Background(), "two edits", twoEdits())
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Status != statusDone {
		t.Fatalf("task 1 = %+v, want done", outcomes[0])
	}
	if outcomes[1].Status != statusFailed || !strings.Contains(outcomes[1].Reason, "did not land") || !strings.Contains(outcomes[1].Reason, "a.txt") {
		t.Fatalf("task 2 = %+v, want a failure that says it did not land and names a.txt", outcomes[1])
	}
	if _, _, released := iso.counts(); released != 2 {
		t.Fatalf("released %d trees, want 2", released)
	}
}

// The plan print names the choice once per run, so a user watching four
// writers start together knows why, and one watching them queue knows too.
func TestThePlanPrintSaysWhereWritersRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		iso  Isolator
		want string
	}{
		{"isolated", &fakeIsolator{}, "each writer in its own tree"},
		{"shared", nil, "one tree, writers one at a time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := enginetest.New(enginetest.Step{Text: "edited one"}, enginetest.Step{Text: "edited two"})
			defer srv.Close()
			a, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
			a.Isolator = tc.iso
			a.announcePlan(twoEdits())
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("plan print:\n%s\nwant %q", out.String(), tc.want)
			}
		})
	}
}
