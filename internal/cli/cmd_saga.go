package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/engine"
)

// runSaga is the top-level `kolk saga` command dispatcher.
func (a *app) runSaga(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("usage: kolk saga <goal | run | resume | status | stop | rewind>")
	}

	switch args[0] {
	case "status":
		return a.printSagaStatus()
	case "resume":
		return a.resumeSaga(ctx)
	case "stop":
		return a.stopSaga()
	case "rewind":
		return a.rewindSaga()
	case "run":
		return a.runSagaLoop(ctx)
	default:
		goal := strings.Join(args, " ")
		if err := a.saveSagaGoal(goal); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "saga goal set: %s\n", goal)
		return nil
	}
}

// sagaArtifactPath resolves SAGA.md for the project the user is working on,
// not for whichever directory they happen to be standing in. Running
// `kolk saga` from a package directory used to leave a stray SAGA.md there and
// hide the real one from `kolk saga status`.
func sagaArtifactPath() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("saga: resolve working directory: %w", err)
	}
	if root, ok := ancestorContaining(start, ".git"); ok {
		return filepath.Join(root, "SAGA.md"), nil
	}
	// Not a repository: honour an artifact an ancestor already owns.
	if root, ok := ancestorContaining(start, "SAGA.md"); ok {
		return filepath.Join(root, "SAGA.md"), nil
	}
	return filepath.Join(start, "SAGA.md"), nil
}

func ancestorContaining(start, entry string) (string, bool) {
	for dir := start; ; {
		if _, err := os.Stat(filepath.Join(dir, entry)); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// loadSaga reads the project's saga, reporting whether one exists at all so
// each subcommand can answer honestly instead of assuming there is none.
func (a *app) loadSaga() (*engine.SagaState, string, bool, error) {
	path, err := sagaArtifactPath()
	if err != nil {
		return nil, "", false, err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, path, false, nil
	}
	if err != nil {
		return nil, path, false, fmt.Errorf("saga: read SAGA.md: %w", err)
	}
	state, parseErr := engine.ParseSagaMarkdown(string(body))
	if parseErr != nil {
		return nil, path, true, fmt.Errorf("saga: parse SAGA.md: %w", parseErr)
	}
	return state, path, true, nil
}

// resumeSaga continues a saga from its artifact.
//
// docs/plan/10-saga-loop.md calls SAGA.md "the authoritative resume anchor
// (`kolk saga resume`)", so this is the spec's verb for continuing and `run` is
// the alias. Both work whatever chapters are outstanding — the loop is
// idempotent, so starting and resuming are the same act.
//
// Until S10.6 this printed "the saga loop is not wired to this command yet".
// S10.6 wired it and nothing walked back to the sentence saying otherwise,
// which is precisely the failure gate 8 exists to catch — written the day
// before, and broken by the next checkpoint.
func (a *app) resumeSaga(ctx context.Context) error {
	state, _, found, err := a.loadSaga()
	if err != nil {
		return err
	}
	if found && state.Goal != "" {
		fmt.Fprintf(a.stdout, "saga %q is %s at chapter %d of %d\n",
			state.Goal, state.Status, state.ActiveChapter, state.MaxChapters)
	}
	return a.runSagaLoop(ctx)
}

func (a *app) rewindSaga() error {
	state, _, found, err := a.loadSaga()
	if err != nil {
		return err
	}
	if !found || len(state.Chapters) == 0 {
		fmt.Fprintln(a.stdout, "no saga chapters to rewind")
		if found {
			fmt.Fprintf(a.stdout, "saga %q has not recorded a chapter yet\n", state.Goal)
		}
		return nil
	}
	last := state.Chapters[len(state.Chapters)-1]
	fmt.Fprintf(a.stdout, "saga %q would rewind chapter %d (%s, %s)\n",
		state.Goal, last.Number, last.Title, last.Status)
	fmt.Fprintln(a.stdout, "rewinding is not wired to this command yet")
	return nil
}

func (a *app) stopSaga() error {
	state, path, found, err := a.loadSaga()
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(a.stdout, "no running saga to stop")
		return nil
	}
	if state.Status == "stopped" {
		fmt.Fprintf(a.stdout, "saga %q is already stopped\n", state.Goal)
		return nil
	}
	state.Status = "stopped"
	if err := atomicfile.Write(path, []byte(engine.FormatSagaMarkdown(state)), 0o600); err != nil {
		return fmt.Errorf("saga: record the stop: %w", err)
	}
	fmt.Fprintf(a.stdout, "saga %q stopped at chapter %d\n", state.Goal, state.ActiveChapter)
	return nil
}

func (a *app) saveSagaGoal(goal string) error {
	path, err := sagaArtifactPath()
	if err != nil {
		return err
	}
	state := &engine.SagaState{
		Goal:          goal,
		Started:       time.Now(),
		Status:        "in-progress",
		ActiveChapter: 1,
		MaxChapters:   engine.DefaultMaxChapters,
		CostLimit:     engine.DefaultCostLimit,
		MaxStrikes:    engine.DefaultMaxStrikes,
	}
	if body, readErr := os.ReadFile(path); readErr == nil {
		existing, parseErr := engine.ParseSagaMarkdown(string(body))
		if parseErr != nil {
			return fmt.Errorf("saga: parse SAGA.md: %w", parseErr)
		}
		existing.Goal = goal
		state = existing
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("saga: read SAGA.md: %w", readErr)
	}
	return atomicfile.Write(path, []byte(engine.FormatSagaMarkdown(state)), 0o600)
}

func (a *app) printSagaStatus() error {
	path, err := sagaArtifactPath()
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintln(a.stdout, "no active saga")
		return nil
	}
	if err != nil {
		return fmt.Errorf("saga status: read SAGA.md: %w", err)
	}
	_, err = a.stdout.Write(body)
	return err
}
