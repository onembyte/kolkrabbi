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

// runSagaLoop works the chapters in SAGA.md until a budget, the plan, or the
// user stops it.
//
// This is the half of the saga that was specified and never built: the state
// machine, quality gates, budget guards and artifact writer all existed and
// nothing walked the chapters, so none of it could run.
func (a *app) runSagaLoop(ctx context.Context) error {
	state, path, found, err := a.loadSaga()
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(a.stdout, "no saga here. `kolk saga <goal>` starts one, then write its chapters into SAGA.md.")
		return nil
	}
	if len(state.Chapters) == 0 {
		fmt.Fprintf(a.stdout, "saga %q has no chapters yet; nothing left to work.\n", state.Goal)
		return nil
	}
	if !hasPendingChapter(state) {
		fmt.Fprintf(a.stdout, "saga %q has nothing left to work; every chapter is finished or blocked.\n", state.Goal)
		return nil
	}

	repoDir := filepath.Dir(path)
	// Checked before a model turn, not after: every chapter ends in a commit,
	// and finding out there is no repository once a chapter's tokens are spent
	// is the wrong moment.
	if err := requireGitRepo(repoDir); err != nil {
		return err
	}

	agent, err := a.newAgent(ctx, &options{})
	if err != nil {
		return err
	}
	defer func() { _ = agent.Close() }()

	runner := &engine.SagaRunner{
		Worker:   engine.AgentWorker{Agent: agent},
		Runner:   sagaCommandRunner{shell: shell.New()},
		Detector: engine.FileGateDetector{},
		Budget: engine.SagaBudget{
			MaxChapters: state.MaxChapters,
			CostLimit:   state.CostLimit,
		},
		Write: atomicfile.Write,
		Out:   a.stdout,
	}

	reason, runErr := runner.Run(ctx, repoDir, state)
	if runErr != nil {
		return runErr
	}
	fmt.Fprintln(a.stdout, sagaStopMessage(reason, state))
	return nil
}

// hasPendingChapter reports whether anything is left to attempt.
func hasPendingChapter(state *engine.SagaState) bool {
	for _, chapter := range state.Chapters {
		switch chapter.Status {
		case engine.StatusPending, engine.StatusPlanning, engine.StatusExecuting, engine.StatusFailed:
			return true
		}
	}
	return false
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
		return fmt.Sprintf("saga %q: every acceptance criterion is met.", state.Goal)
	case engine.StopNoWork:
		return fmt.Sprintf("saga %q: no chapter left to work. Add chapters to SAGA.md to continue.", state.Goal)
	case engine.StopMaxChapters:
		return fmt.Sprintf("saga %q: stopped at the chapter limit (%d).", state.Goal, state.MaxChapters)
	case engine.StopCostLimit:
		return fmt.Sprintf("saga %q: stopped at the cost limit ($%.2f spent).", state.Goal, state.CumulativeCost)
	case engine.StopTimeout:
		return fmt.Sprintf("saga %q: stopped at the time limit.", state.Goal)
	case engine.StopDoomLoop:
		return fmt.Sprintf("saga %q: stopped after repeated failures without progress. The last chapter's verification says why.", state.Goal)
	default:
		return fmt.Sprintf("saga %q: stopped.", state.Goal)
	}
}
