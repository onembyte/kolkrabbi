package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// runSagaLoop works one bounded chapter in SAGA.md with the current session
// agent and then stops at the wake boundary. The next ordinary prompt carrying
// /saga is the next wake; note is what that prompt said, when it was not the
// goal, and reaches the planner and the worker for this wake only.
//
// This is the half of the saga that was specified and never built: the state
// machine, quality gates, budget guards and artifact writer all existed and
// nothing walked the chapters, so none of it could run.
//
// There is deliberately no "nothing left to work" check here. Every planned
// chapter being done is not the end of a saga; it is the moment the planner is
// asked for the next chapter, or says the goal is met. A guard that returned
// early on that state — written for the old multi-chapter loop, where planning
// happened inside the same call — stopped every inline saga after its first
// chapter and reported the rest as finished. Terminal status is the executor's
// to judge, from the artifact's own Status line.
func (a *app) runSagaLoop(ctx context.Context, agent *engine.Agent, opening sagaOpening) error {
	if agent == nil {
		return fmt.Errorf("saga: current agent is required")
	}
	// The artifact openSaga just read, not a second read of the same file: it
	// parsed SAGA.md to decide whether this wake starts, continues or resets a
	// saga, and nothing can have changed it since.
	state, note := opening.state, opening.note
	if state == nil {
		fmt.Fprintln(a.stdout, "no active SAGA plan; include /saga in a request to start one.")
		return nil
	}

	repoDir := filepath.Dir(opening.path)
	// Checked before a model turn, not after: every chapter ends in a commit,
	// and finding out there is no repository once a chapter's tokens are spent
	// is the wrong moment.
	if err := requireGitRepo(repoDir); err != nil {
		return err
	}

	runner := &engine.SagaRunner{
		Planner:  engine.AgentPlanner{Agent: agent, Note: note},
		Worker:   engine.AgentWorker{Agent: agent, Note: note},
		Repairer: engine.AgentRepairer{Agent: agent},
		Runner:   sagaCommandRunner{shell: shell.New()},
		Detector: engine.FileGateDetector{},
		Budget:   sagaWakeBudget(state),
		Write:    atomicfile.Write,
		Out:      a.stdout,
	}

	reason, runErr := runner.RunWake(ctx, repoDir, state)
	if runErr != nil {
		// The failed chapter is already persisted by RunChapter. Keep the
		// non-zero error for automation, but make the recovery command visible
		// instead of leaving a user with a bare provider or gate failure.
		fmt.Fprintln(a.stdout, sagaWakeRetryMessage(state.Goal))
		return runErr
	}
	fmt.Fprintln(a.stdout, sagaStopMessage(reason, state))
	return nil
}

func sagaWakeRetryMessage(goal string) string {
	return fmt.Sprintf("saga %q: wake stopped before completion; include /saga in your next request to retry.", goal)
}

// sagaWakeBudget carries every limit SAGA.md records into the wake. All of
// them: a budget built from three of the four lines left the doom threshold
// at its default, so a saga allowed five strikes in its own artifact was
// blocked at three.
func sagaWakeBudget(state *engine.SagaState) engine.SagaBudget {
	return engine.SagaBudget{
		MaxChapters:   state.MaxChapters,
		CostLimit:     state.CostLimit,
		DoomThreshold: state.MaxStrikes,
	}
}

// requireGitRepo refuses to start a saga where its chapters cannot be committed.
func requireGitRepo(dir string) error {
	for at := dir; ; {
		if info, err := os.Stat(filepath.Join(at, ".git")); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return nil
		}
		parent := filepath.Dir(at)
		if parent == at {
			return fmt.Errorf("saga: %s is not inside a git repository, and every chapter ends in a commit", dir)
		}
		at = parent
	}
}

// sagaStopMessage says why the loop stopped, in the words the reason means.
//
// "goal-complete" and "no-work" are different endings and a run that conflated
// them would report success for a plan that simply ran out.
func sagaStopMessage(reason engine.StopReason, state *engine.SagaState) string {
	switch reason {
	case engine.StopGoalComplete:
		return fmt.Sprintf("saga %q: every acceptance criterion is met. SAGA.md is finished; the next /saga request archives it and starts a new saga.", state.Goal)
	case engine.StopNoWork:
		return fmt.Sprintf("saga %q: no chapter left to work. Add chapters to SAGA.md to continue.", state.Goal)
	case engine.StopWake:
		return fmt.Sprintf("saga %q: wake complete at chapter %d. Include /saga in your next request for the next chapter.", state.Goal, state.ActiveChapter)
	case engine.StopMaxChapters:
		return fmt.Sprintf("saga %q: stopped at the chapter limit (%d).", state.Goal, state.MaxChapters)
	case engine.StopCostLimit:
		return fmt.Sprintf("saga %q: stopped at the cost limit ($%.2f spent).", state.Goal, state.CumulativeCost)
	case engine.StopTimeout:
		return fmt.Sprintf("saga %q: stopped at the time limit.", state.Goal)
	case engine.StopDoomLoop:
		return fmt.Sprintf("saga %q: %s failures without progress. The last chapter's verification says why. SAGA.md is blocked; the next /saga request archives it and starts a new saga.",
			state.Goal, doomLoopPhrase)
	default:
		return fmt.Sprintf("saga %q: stopped.", state.Goal)
	}
}
