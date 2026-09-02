package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// sagaOpening is what an inline /saga request found in the project and what
// was decided about it.
type sagaOpening struct {
	// goal is the saga's goal after the decision: the request text for a new
	// saga, the persisted goal for one already in flight.
	goal string
	// note is the request text when it is not the goal. A saga in flight
	// keeps its goal; what the user typed at the wake is passed to the planner
	// and the worker as a note instead of replacing the goal. Empty when the
	// text is the goal or restates it.
	note string
	// notice is one line telling the user what was decided, or empty when
	// there was nothing to decide.
	notice string
	// state and path are the parsed artifact and where it lives. openSaga has
	// just read and parsed SAGA.md to decide all of the above, so the wake
	// takes that parse rather than reading the same file again: two reads and
	// two parses per wake, for a file that cannot have changed in between.
	state *engine.SagaState
	path  string
}

// openSaga decides what an inline /saga request means for the project's
// SAGA.md and writes the artifact when a saga starts.
//
// Three cases, one rule each. No artifact: the text is the goal of a new saga.
// A saga in flight: its goal stands, and the text is a note for this wake —
// `next chapter /saga` and `retry /saga` are how the wake messages tell the
// user to continue, and a goal that became "retry" would plan against the
// wrong thing for the rest of the run. A saga that finished (completed or
// blocked): it is archived beside SAGA.md and the text starts a new one,
// because a terminal artifact is a restart boundary and a request that lands
// on one is asking for something new, not for the old answer again.
func (a *app) openSaga(text string) (sagaOpening, error) {
	path, err := sagaArtifactPath()
	if err != nil {
		return sagaOpening{}, err
	}
	if err := requireGitRepo(filepath.Dir(path)); err != nil {
		return sagaOpening{}, err
	}

	var notice string
	body, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		existing, parseErr := engine.ParseSagaMarkdown(string(body))
		if parseErr != nil {
			return sagaOpening{}, fmt.Errorf("saga: parse SAGA.md: %w", parseErr)
		}
		if !sagaFinished(existing) {
			opening := sagaOpening{goal: existing.Goal, state: existing, path: path}
			if !strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(existing.Goal)) {
				opening.note = text
				opening.notice = fmt.Sprintf("saga %q: continuing; your note for this wake: %q", existing.Goal, text)
			}
			return opening, nil
		}
		archive, archiveErr := archiveSaga(path, existing, time.Now())
		if archiveErr != nil {
			return sagaOpening{}, archiveErr
		}
		notice = fmt.Sprintf("saga %q was %s; archived to %s. starting a new saga %q.",
			existing.Goal, existing.Status, filepath.Base(archive), text)
	case !os.IsNotExist(readErr):
		return sagaOpening{}, fmt.Errorf("saga: read SAGA.md: %w", readErr)
	}

	state := &engine.SagaState{
		Goal:          text,
		Started:       time.Now(),
		Status:        engine.SagaStatusInProgress,
		ActiveChapter: 1,
		MaxChapters:   engine.DefaultMaxChapters,
		CostLimit:     engine.DefaultCostLimit,
		MaxStrikes:    engine.DefaultMaxStrikes,
	}
	if err := atomicfile.Write(path, []byte(engine.FormatSagaMarkdown(state)), 0o600); err != nil {
		return sagaOpening{}, err
	}
	return sagaOpening{goal: text, notice: notice, state: state, path: path}, nil
}

// sagaFinished reports whether the artifact is at a terminal status. The
// executor treats these as authoritative and will not reopen them; a wake
// that lands on one therefore has to start something new or do nothing.
func sagaFinished(state *engine.SagaState) bool {
	return state.Status == engine.SagaStatusCompleted || state.Status == engine.SagaStatusBlocked
}

// archiveSaga moves a finished SAGA.md aside, named by when that saga started
// so the archives sort in the order the work happened. Nothing is deleted: the
// chapter log is the only record of what the saga tried, and a new saga in the
// same repository is exactly when someone wants to read it.
func archiveSaga(path string, state *engine.SagaState, now time.Time) (string, error) {
	stamp := state.Started
	if stamp.IsZero() {
		stamp = now
	}
	base := strings.TrimSuffix(path, ".md") + "." + stamp.UTC().Format("20060102-150405")
	archive := base + ".md"
	for n := 2; ; n++ {
		if _, err := os.Lstat(archive); os.IsNotExist(err) {
			break
		}
		archive = fmt.Sprintf("%s-%d.md", base, n)
	}
	if err := os.Rename(path, archive); err != nil {
		return "", fmt.Errorf("saga: archive finished SAGA.md: %w", err)
	}
	return archive, nil
}
