package engine

import (
	"errors"
	"fmt"
	"strings"
)

// errRoundsExhausted marks a subagent that ran out of tool rounds. It is a
// distinct error because what it means is different: the task did not finish,
// but work exists, and throwing that away is a second failure on top of the
// first.
var errRoundsExhausted = errors.New("exceeded the tool rounds allowed for one task")

// status is what became of one task.
type status int

const (
	statusDone status = iota
	// statusIncomplete ran out of rounds. Its partial work is kept.
	statusIncomplete
	// statusFailed errored. There is nothing to keep.
	statusFailed
	// statusBlocked was never attempted because something it needed failed.
	statusBlocked
	// statusOverBudget was never attempted because the run hit its ceiling.
	statusOverBudget
)

func (s status) String() string {
	switch s {
	case statusDone:
		return "done"
	case statusIncomplete:
		return "incomplete"
	case statusBlocked:
		return "blocked"
	case statusOverBudget:
		return "not run — over budget"
	default:
		return "failed"
	}
}

// outcome is one task's result and what became of it.
type outcome struct {
	Result string
	Status status
	Reason string
}

// classify turns a subagent's return into an outcome.
func (a *Agent) classify(result string, err error) outcome {
	switch {
	case errors.Is(err, errRoundsExhausted):
		return outcome{Result: result, Status: statusIncomplete, Reason: err.Error()}
	case err != nil:
		return outcome{Status: statusFailed, Reason: err.Error()}
	default:
		return outcome{Result: result, Status: statusDone}
	}
}

// blockedBy reports the first dependency that makes a task unrunnable.
//
// Only failed and blocked dependencies count. An incomplete one produced
// something, and a task is entitled to work with what there is — it asked for a
// result, not for a guarantee.
func blockedBy(tasks []Task, outcomes []outcome, index int) (string, bool) {
	for _, need := range tasks[index].Needs {
		if need >= len(outcomes) {
			continue
		}
		if s := outcomes[need].Status; s == statusFailed || s == statusBlocked || s == statusOverBudget {
			return tasks[need].Title, true
		}
	}
	return "", false
}

// summarise renders the outcomes for the synthesis prompt.
//
// Failures go in the prompt, not only in a log line the user has already
// scrolled past: an orchestrated answer that silently omits the third of six
// tasks that did not work is worse than no orchestration, because the reader
// has no way to know the answer is partial.
func summarise(tasks []Task, outcomes []outcome) string {
	var b strings.Builder
	for i, task := range tasks {
		fmt.Fprintf(&b, "\n%d. %s [%s]\n", i+1, task.Title, outcomes[i].Status)
		switch outcomes[i].Status {
		case statusDone:
			fmt.Fprintf(&b, "Result: %s\n", outcomes[i].Result)
		case statusIncomplete:
			fmt.Fprintf(&b, "Unfinished. What it had reached: %s\n", outcomes[i].Result)
		default:
			fmt.Fprintf(&b, "Did not run to completion: %s\n", outcomes[i].Reason)
		}
	}
	return b.String()
}

// countFailures returns how many tasks produced nothing usable.
func countFailures(outcomes []outcome) int {
	n := 0
	for _, o := range outcomes {
		if o.Status == statusFailed || o.Status == statusBlocked || o.Status == statusOverBudget {
			n++
		}
	}
	return n
}
