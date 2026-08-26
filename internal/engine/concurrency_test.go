package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// readerPlan is three independent tasks that only read, so nothing about the
// working tree forces them into a sequence.
const readerPlan = `[{"title":"a","kind":"research"},{"title":"b","kind":"research"},{"title":"c","kind":"research"}]`

func slowSteps(n int, delay time.Duration) []enginetest.Step {
	steps := make([]enginetest.Step, n)
	for i := range steps {
		// Identical, because which goroutine reaches the server first is not
		// something a test should depend on.
		steps[i] = enginetest.Step{Text: "did it", Delay: delay}
	}
	return steps
}

func TestIndependentReadersRunAtTheSameTime(t *testing.T) {
	steps := append([]enginetest.Step{{Text: readerPlan}}, slowSteps(3, 40*time.Millisecond)...)
	srv := enginetest.New(append(steps, enginetest.Step{Text: "answer"})...)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh

	if err := agent.runOrchestrated(context.Background(), "three lookups"); err != nil {
		t.Fatalf("run returned %v", err)
	}
	if got := srv.MaxInFlight(); got < 2 {
		t.Fatalf("max in flight = %d, want the readers to overlap", got)
	}
}

func TestNoMoreThanTheLimitRunAtOnce(t *testing.T) {
	plan := `[{"title":"a","kind":"research"},{"title":"b","kind":"research"},{"title":"c","kind":"research"},{"title":"d","kind":"research"}]`
	steps := append([]enginetest.Step{{Text: plan}}, slowSteps(4, 40*time.Millisecond)...)
	srv := enginetest.New(append(steps, enginetest.Step{Text: "answer"})...)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortMax
	agent.MaxConcurrentTasks = 2

	agent.runOrchestrated(context.Background(), "four lookups")

	if got := srv.MaxInFlight(); got > 2 {
		t.Fatalf("max in flight = %d, want at most the limit", got)
	}
}

func TestATaskWaitsForWhatItNeeds(t *testing.T) {
	plan := `[{"title":"find","kind":"research"},{"title":"read","kind":"research","needs":[1]}]`
	srv := enginetest.New(
		enginetest.Step{Text: plan},
		enginetest.Step{Text: "found", Delay: 40 * time.Millisecond},
		enginetest.Step{Text: "read"},
		enginetest.Step{Text: "answer"},
	)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	if err := agent.runOrchestrated(context.Background(), "find then read"); err != nil {
		t.Fatalf("run returned %v", err)
	}

	// Overlapping a task with something it declared it needs would hand it an
	// empty result and call that a dependency.
	if got := srv.MaxInFlight(); got != 1 {
		t.Fatalf("max in flight = %d, want a dependency to serialise", got)
	}
}

func TestTasksThatWriteNeverRunTogether(t *testing.T) {
	plan := `[{"title":"a","kind":"edit"},{"title":"b","kind":"edit"},{"title":"c","kind":"edit"}]`
	steps := append([]enginetest.Step{{Text: plan}}, slowSteps(3, 40*time.Millisecond)...)
	srv := enginetest.New(append(steps, enginetest.Step{Text: "answer"})...)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh

	agent.runOrchestrated(context.Background(), "three edits")

	// They share one working tree. Two agents editing it at once is how a run
	// produces a state neither of them intended.
	if got := srv.MaxInFlight(); got != 1 {
		t.Fatalf("max in flight = %d, want writers serialised", got)
	}
}

func TestAPlanWithNoKindsBehavesExactlyAsBefore(t *testing.T) {
	plan := `["a","b","c"]`
	steps := append([]enginetest.Step{{Text: plan}}, slowSteps(3, 30*time.Millisecond)...)
	srv := enginetest.New(append(steps, enginetest.Step{Text: "answer"})...)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh

	agent.runOrchestrated(context.Background(), "three things")

	// An unlabelled task might write. Treating it as a reader would make
	// concurrency a hazard that arrives with a weaker planner.
	if got := srv.MaxInFlight(); got != 1 {
		t.Fatalf("max in flight = %d, want an unlabelled plan to stay sequential", got)
	}
}

func TestEachTaskOutputArrivesInOnePiece(t *testing.T) {
	plan := `[{"title":"alpha","kind":"research"},{"title":"beta","kind":"research"}]`
	srv := enginetest.New(
		enginetest.Step{Text: plan},
		enginetest.Step{Text: "AAAA", Delay: 30 * time.Millisecond},
		enginetest.Step{Text: "BBBB", Delay: 30 * time.Millisecond},
		enginetest.Step{Text: "answer"},
	)
	defer srv.Close()

	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	if err := agent.runOrchestrated(context.Background(), "two lookups"); err != nil {
		t.Fatalf("run returned %v", err)
	}

	// Two agents streaming into one terminal is unreadable, which is why the
	// decided form is a per-task block rather than live panes.
	got := out.String()
	if strings.Contains(got, "ABAB") || strings.Contains(got, "ABBA") {
		t.Fatalf("output interleaved:\n%s", got)
	}
	for _, want := range []string{"AAAA", "BBBB", "alpha", "beta"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output lost %q:\n%s", want, got)
		}
	}
}
