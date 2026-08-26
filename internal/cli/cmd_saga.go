package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/engine"
)

// runSaga is the top-level `kolk saga` command dispatcher.
func (a *app) runSaga(_ context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("usage: kolk saga <goal | resume | status | stop | rewind>")
	}

	switch args[0] {
	case "status":
		return a.printSagaStatus()
	case "resume":
		fmt.Fprintln(a.stdout, "no saga to resume")
		return nil
	case "stop":
		fmt.Fprintln(a.stdout, "no running saga to stop")
		return nil
	case "rewind":
		fmt.Fprintln(a.stdout, "no saga chapters to rewind")
		return nil
	default:
		// Treat any other argument as a goal string.
		goal := args[0]
		if len(args) > 1 {
			// Join multi-word goals.
			for _, a := range args[1:] {
				goal += " " + a
			}

		}
		if err := a.saveSagaGoal(goal); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "saga goal set: %s\n", goal)
		return nil
	}
}

func (a *app) saveSagaGoal(goal string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("saga: resolve repository directory: %w", err)
	}
	path := filepath.Join(dir, "SAGA.md")
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
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("saga status: resolve repository directory: %w", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "SAGA.md"))
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
