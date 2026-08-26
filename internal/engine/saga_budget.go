package engine

import "time"

// StopReason describes why a saga was halted.
type StopReason string

const (
	StopNone         StopReason = ""              // saga may continue
	StopGoalComplete StopReason = "goal-complete" // all acceptance criteria verified
	StopMaxChapters  StopReason = "max-chapters"  // chapter limit reached
	StopCostLimit    StopReason = "cost-limit"    // dollar budget exhausted
	StopTimeout      StopReason = "timeout"       // execution time exceeded
	StopDoomLoop     StopReason = "doom-loop"     // consecutive failures without progress
)

// DefaultMaxChapters is the chapter ceiling when the user provides none.
const DefaultMaxChapters = 15

// DefaultCostLimit is the dollar ceiling when the user provides none.
const DefaultCostLimit = 5.00

// DefaultDoomThreshold is the number of consecutive failed/no-progress
// chapters before the saga is halted to prevent runaway spending.
const DefaultDoomThreshold = 3

// SagaBudget holds the configurable limits for a saga run.
type SagaBudget struct {
	MaxChapters   int           // 0 → DefaultMaxChapters
	CostLimit     float64       // 0 → DefaultCostLimit
	Timeout       time.Duration // 0 → 1 hour
	DoomThreshold int           // 0 → DefaultDoomThreshold
}

func (b SagaBudget) maxChapters() int {
	if b.MaxChapters > 0 {
		return b.MaxChapters
	}
	return DefaultMaxChapters
}

func (b SagaBudget) costLimit() float64 {
	if b.CostLimit > 0 {
		return b.CostLimit
	}
	return DefaultCostLimit
}

func (b SagaBudget) timeout() time.Duration {
	if b.Timeout > 0 {
		return b.Timeout
	}
	return time.Hour
}

func (b SagaBudget) doomThreshold() int {
	if b.DoomThreshold > 0 {
		return b.DoomThreshold
	}
	return DefaultDoomThreshold
}

// Check evaluates the current saga state against every configured stop
// condition. It returns the first reason to halt, or StopNone if the saga
// may continue.
func (b SagaBudget) Check(s *SagaState, consecutiveFailures int, elapsed time.Duration) StopReason {
	// 1. Goal complete — all acceptance criteria met.
	if allCriteriaMet(s.Criteria) {
		return StopGoalComplete
	}

	// 2. Chapter limit.
	if len(s.Chapters) >= b.maxChapters() {
		return StopMaxChapters
	}

	// 3. Cost limit.
	if s.CumulativeCost >= b.costLimit() {
		return StopCostLimit
	}

	// 4. Timeout.
	if elapsed >= b.timeout() {
		return StopTimeout
	}

	// 5. Doom-loop detector.
	if consecutiveFailures >= b.doomThreshold() {
		return StopDoomLoop
	}

	return StopNone
}

func allCriteriaMet(criteria []AcceptanceCriterion) bool {
	if len(criteria) == 0 {
		return false
	}
	for _, c := range criteria {
		if !c.Done {
			return false
		}
	}
	return true
}
