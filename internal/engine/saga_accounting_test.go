package engine

import (
	"context"
	"os"
	"testing"
	"time"
)

// meteredPlanner and meteredRepairer spend against a shared session meter the
// way the real agent-backed adapters do: through the agent's session cost, not
// through the worker's result.
type meteredPlanner struct {
	meter  *float64
	cost   float64
	titles []string
}

func (p *meteredPlanner) Next(_ context.Context, _ string, done []Chapter) (string, error) {
	*p.meter += p.cost
	if len(done) >= len(p.titles) {
		return "", nil
	}
	return p.titles[len(done)], nil
}

type meteredRepairer struct {
	meter *float64
	cost  float64
	calls int
}

func (r *meteredRepairer) Repair(context.Context, Chapter, string) error {
	*r.meter += r.cost
	r.calls++
	return nil
}

// A saga's ceiling is only a ceiling if everything the saga spends counts
// against it. The worker's spend did; the planner's and the repair turn's did
// not, so a saga could plan and repair its way past its own budget.
func TestPlannerAndRepairSpendCountAgainstTheSagaBudget(t *testing.T) {
	var meter float64
	planner := &meteredPlanner{meter: &meter, cost: 0.30, titles: []string{"first"}}
	repairer := &meteredRepairer{meter: &meter, cost: 0.20}
	runner := &scriptedRunner{replies: map[string]CommandResult{
		"git status --porcelain": {Output: " M main.go\n"},
		"go test ./...":          {ExitCode: 1, Failure: "tests failed"},
	}}
	executor := &SagaRunner{
		Planner:  planner,
		Worker:   &workerSpy{cost: 0},
		Repairer: repairer,
		Runner:   runner,
		Detector: fixedDetector{{Name: "test", Command: "go test ./..."}},
		Budget:   SagaBudget{MaxChapters: 10, CostLimit: 0.40},
		Write:    func(string, []byte, os.FileMode) error { return nil },
		Now:      func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) },
		Spent:    func() float64 { return meter },
	}
	state := &SagaState{Goal: "make it work"}

	// The first wake plans, works, fails its gates, repairs once, fails again
	// and rolls back -- a failed chapter, so the wake returns an error before
	// the budget is consulted. The spend is what this leaf is about.
	if _, err := executor.RunWake(context.Background(), "/repo", state); err == nil {
		t.Fatal("a chapter that failed its gates twice reported success")
	}
	if repairer.calls != 1 {
		t.Fatalf("repair turns = %d, want exactly one", repairer.calls)
	}
	if state.CumulativeCost < 0.49 || state.CumulativeCost > 0.51 {
		t.Fatalf("cumulative cost = %.2f; planner (0.30) and repair (0.20) spend did not reach the saga budget", state.CumulativeCost)
	}
	// The next wake opens with the budget check, which now sees the whole spend.
	reason, _ := executor.RunWake(context.Background(), "/repo", state)
	if reason != StopCostLimit {
		t.Fatalf("stop reason = %q, want %q: the $0.40 ceiling was crossed by planning and repair", reason, StopCostLimit)
	}
}
