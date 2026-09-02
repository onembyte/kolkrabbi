package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/engine"
)

// sagaArtifactPath resolves SAGA.md for the project the user is working on,
// not for whichever directory they happen to be standing in. An inline SAGA
// prompt from a package directory must not leave a stray artifact there or
// hide the real one from the project log.
func sagaArtifactPath() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("saga: resolve working directory: %w", err)
	}
	if root, ok := ancestorContaining(start, ".git"); ok {
		return filepath.Join(root, "SAGA.md"), nil
	}
	// Not a repository: honour an artifact a normal ancestor already owns.
	// A shared scratch directory such as /tmp is not a project boundary: its
	// SAGA.md must not seize every unrelated temporary checkout below it.
	if root, ok := ancestorSagaArtifact(start); ok {
		return filepath.Join(root, "SAGA.md"), nil
	}
	return filepath.Join(start, "SAGA.md"), nil
}

func ancestorSagaArtifact(start string) (string, bool) {
	for dir, first := start, true; ; {
		// A caller standing in a shared directory can still use its own
		// explicit artifact. Descendants stop before inheriting that directory.
		if !first && isWorldWritableDir(dir) {
			return "", false
		}
		if _, err := os.Stat(filepath.Join(dir, "SAGA.md")); err == nil {
			return dir, true
		}
		if isWorldWritableDir(dir) {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir, first = parent, false
	}
}

func isWorldWritableDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir() && info.Mode().Perm()&0o002 != 0
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

func (a *app) saveSagaGoal(goal string) error {
	path, err := sagaArtifactPath()
	if err != nil {
		return err
	}
	if err := requireGitRepo(filepath.Dir(path)); err != nil {
		return err
	}
	state := &engine.SagaState{
		Goal:          goal,
		Started:       time.Now(),
		Status:        engine.SagaStatusInProgress,
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
