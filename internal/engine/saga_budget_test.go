package engine_test

import (
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

func TestBudgetMaxChaptersStops(t *testing.T) {
	budget := engine.SagaBudget{MaxChapters: 3}
	s := &engine.SagaState{
		Chapters: make([]engine.Chapter, 3),
		Criteria: []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	reason := budget.Check(s, 0, 0)
	if reason != engine.StopMaxChapters {
		t.Errorf("got %q, want %q", reason, engine.StopMaxChapters)
	}
}

func TestBudgetMaxChaptersContinues(t *testing.T) {
	budget := engine.SagaBudget{MaxChapters: 5}
	s := &engine.SagaState{
		Chapters: make([]engine.Chapter, 2),
		Criteria: []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	reason := budget.Check(s, 0, 0)
	if reason != engine.StopNone {
		t.Errorf("got %q, want %q", reason, engine.StopNone)
	}
}

func TestBudgetDefaultMaxChapters(t *testing.T) {
	budget := engine.SagaBudget{} // zero → DefaultMaxChapters = 15
	s := &engine.SagaState{
		Chapters: make([]engine.Chapter, 15),
		Criteria: []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	reason := budget.Check(s, 0, 0)
	if reason != engine.StopMaxChapters {
		t.Errorf("got %q, want %q", reason, engine.StopMaxChapters)
	}
}

func TestBudgetCostLimitStops(t *testing.T) {
	budget := engine.SagaBudget{CostLimit: 2.50}
	s := &engine.SagaState{
		CumulativeCost: 2.50,
		Criteria:       []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	reason := budget.Check(s, 0, 0)
	if reason != engine.StopCostLimit {
		t.Errorf("got %q, want %q", reason, engine.StopCostLimit)
	}
}

func TestBudgetCostLimitContinues(t *testing.T) {
	budget := engine.SagaBudget{CostLimit: 5.00}
	s := &engine.SagaState{
		CumulativeCost: 4.99,
		Criteria:       []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	reason := budget.Check(s, 0, 0)
	if reason != engine.StopNone {
		t.Errorf("got %q, want %q", reason, engine.StopNone)
	}
}

func TestBudgetTimeoutStops(t *testing.T) {
	budget := engine.SagaBudget{Timeout: 30 * time.Minute}
	s := &engine.SagaState{
		Criteria: []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	reason := budget.Check(s, 0, 30*time.Minute)
	if reason != engine.StopTimeout {
		t.Errorf("got %q, want %q", reason, engine.StopTimeout)
	}
}

func TestBudgetDefaultTimeout(t *testing.T) {
	budget := engine.SagaBudget{} // zero → 1 hour
	s := &engine.SagaState{
		Criteria: []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	reason := budget.Check(s, 0, time.Hour)
	if reason != engine.StopTimeout {
		t.Errorf("got %q, want %q", reason, engine.StopTimeout)
	}
}

func TestBudgetDoomLoopStops(t *testing.T) {
	budget := engine.SagaBudget{}
	s := &engine.SagaState{
		Criteria: []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	reason := budget.Check(s, 3, 0)
	if reason != engine.StopDoomLoop {
		t.Errorf("got %q, want %q", reason, engine.StopDoomLoop)
	}
}

func TestBudgetDoomLoopCustomThreshold(t *testing.T) {
	budget := engine.SagaBudget{DoomThreshold: 5}
	s := &engine.SagaState{
		Criteria: []engine.AcceptanceCriterion{{Description: "x", Done: false}},
	}
	// 4 consecutive failures under threshold of 5 → continue
	reason := budget.Check(s, 4, 0)
	if reason != engine.StopNone {
		t.Errorf("got %q, want %q at 4 failures with threshold 5", reason, engine.StopNone)
	}
	// 5 → stop
	reason = budget.Check(s, 5, 0)
	if reason != engine.StopDoomLoop {
		t.Errorf("got %q, want %q at 5 failures with threshold 5", reason, engine.StopDoomLoop)
	}
}

func TestBudgetGoalCompleteStops(t *testing.T) {
	budget := engine.SagaBudget{}
	s := &engine.SagaState{
		Criteria: []engine.AcceptanceCriterion{
			{Description: "test 1", Done: true},
			{Description: "test 2", Done: true},
		},
	}
	reason := budget.Check(s, 0, 0)
	if reason != engine.StopGoalComplete {
		t.Errorf("got %q, want %q", reason, engine.StopGoalComplete)
	}
}

func TestBudgetGoalCompleteBeatsChapterLimit(t *testing.T) {
	// Goal complete should be reported even if other limits are also hit.
	budget := engine.SagaBudget{MaxChapters: 1}
	s := &engine.SagaState{
		Chapters: make([]engine.Chapter, 5),
		Criteria: []engine.AcceptanceCriterion{
			{Description: "test 1", Done: true},
		},
	}
	reason := budget.Check(s, 0, 0)
	if reason != engine.StopGoalComplete {
		t.Errorf("got %q, want %q (goal complete should take priority)", reason, engine.StopGoalComplete)
	}
}

func TestBudgetNoCriteriaNeverComplete(t *testing.T) {
	budget := engine.SagaBudget{}
	s := &engine.SagaState{
		Criteria: nil, // no criteria → can never be "complete"
	}
	reason := budget.Check(s, 0, 0)
	if reason != engine.StopNone {
		t.Errorf("got %q, want %q with no criteria", reason, engine.StopNone)
	}
}
