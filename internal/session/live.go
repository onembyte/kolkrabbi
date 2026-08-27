package session

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/lock"
)

// State is whether a process is currently running a session.
type State int

const (
	// StateUnknown means this platform cannot say. Reported honestly rather
	// than guessed at: a dashboard that shows "idle" for every session on
	// Windows is worse than one that admits it does not know.
	StateUnknown State = iota
	// StateIdle means nothing is running it.
	StateIdle
	// StateLive means a process holds it right now.
	StateLive
)

func (s State) String() string {
	switch s {
	case StateLive:
		return "live"
	case StateIdle:
		return "idle"
	default:
		return "unknown"
	}
}

// Hold marks a session as being run by this process, until the returned handle
// is closed.
//
// An advisory lock rather than a flag in a file, because the interesting case
// is the one nobody writes code for: a session whose process was killed. The OS
// drops a lock when the process goes, so a crashed session stops looking live
// without anything having to notice that it crashed.
func Hold(dir, id string) (*lock.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return lock.Try(lockPath(dir, id))
}

// Live reports whether a session is being run right now.
//
// It answers by trying to take the lock and immediately giving it back, which
// is why the release is not optional: a dashboard that polls this would
// otherwise lock out the session it is describing.
func Live(dir, id string) State {
	held, err := lock.Try(lockPath(dir, id))
	switch {
	case err == nil:
		// Nobody had it. Give it straight back.
		_ = held.Close()
		return StateIdle
	case errors.Is(err, lock.ErrBusy):
		return StateLive
	default:
		// Unsupported platform, unreadable directory, anything else: say so
		// rather than reporting a state that was never observed.
		return StateUnknown
	}
}

func lockPath(dir, id string) string { return filepath.Join(dir, id+".lock") }
