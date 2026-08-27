package session

import (
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
// It probes the lock without taking or creating it. Taking it would lock out
// the session being described; creating it would leave a file behind for every
// session a listing touches, which a benchmark against a real directory of 549
// sessions turned into 549 stray files and half a second of syscalls.
func Live(dir, id string) State {
	held, err := lock.Held(lockPath(dir, id))
	switch {
	case err != nil:
		// Unsupported platform, unreadable directory, anything else: say so
		// rather than reporting a state that was never observed.
		return StateUnknown
	case held:
		return StateLive
	default:
		return StateIdle
	}
}

func lockPath(dir, id string) string { return filepath.Join(dir, id+".lock") }
