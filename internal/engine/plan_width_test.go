package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func TestLowEffortKeepsEveryPlannedTask(t *testing.T) {
	var plan []string
	for i := 0; i < 12; i++ {
		plan = append(plan, fmt.Sprintf(`{"title":"task %d","kind":"research","level":"trivial","needs":[]}`, i+1))
	}
	steps := []enginetest.Step{{Text: "[" + strings.Join(plan, ",") + "]"}}
	for i := 0; i < 12; i++ {
		steps = append(steps, enginetest.Step{Text: fmt.Sprintf("result %d", i+1), Delay: 20 * time.Millisecond})
	}
	steps = append(steps, enginetest.Step{Text: "all twelve complete"})
	srv := enginetest.New(steps...)
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.Effort = EffortLow
	a.MaxConcurrentTasks = 2
	if err := a.RunTurn(context.Background(), "twelve independent tasks"); err != nil {
		t.Fatal(err)
	}
	if len(srv.Requests) != 14 {
		t.Fatalf("provider calls = %d, want planner + 12 children + synthesis", len(srv.Requests))
	}
	if got := srv.MaxInFlight(); got != 2 {
		t.Fatalf("max concurrent calls = %d, want 2", got)
	}
}

func TestRoutineTaskUsesTheNearestLowerModel(t *testing.T) {
	a := rosterAgent("claude-opus")
	tasks := []Task{{Title: "ordinary change", Level: LevelRoutine}}
	a.assignModels(tasks)
	if tasks[0].Model != "claude-sonnet" {
		t.Fatalf("routine model = %q, want claude-sonnet", tasks[0].Model)
	}
}
