package session

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestASessionNobodyIsRunningIsIdle(t *testing.T) {
	dir := t.TempDir()

	switch got := Live(dir, validTestSessionID); got {
	case StateIdle:
	case StateUnknown:
		if lockingWorks() {
			t.Fatalf("liveness = unknown on a platform that supports locking")
		}
	default:
		t.Fatalf("liveness = %v, want idle", got)
	}
}

func TestHoldingASessionMakesItLive(t *testing.T) {
	if !lockingWorks() {
		t.Skipf("advisory locks unsupported on %s", runtime.GOOS)
	}
	dir := t.TempDir()

	held, err := Hold(dir, validTestSessionID)
	if err != nil {
		t.Fatalf("holding: %v", err)
	}
	defer held.Close()

	// "Which of these is actually running" is the first question a control
	// plane has to answer, and a file's timestamp only ever guesses at it.
	if got := Live(dir, validTestSessionID); got != StateLive {
		t.Fatalf("liveness = %v, want live", got)
	}
}

func TestReleasingASessionMakesItIdleAgain(t *testing.T) {
	if !lockingWorks() {
		t.Skipf("advisory locks unsupported on %s", runtime.GOOS)
	}
	dir := t.TempDir()

	held, err := Hold(dir, validTestSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}

	// A session that crashed must not look live forever: the OS drops the
	// lock when the process goes, which is why this is a lock and not a flag
	// somebody has to remember to clear.
	if got := Live(dir, validTestSessionID); got != StateIdle {
		t.Fatalf("liveness = %v, want idle", got)
	}
}

func TestLivenessDoesNotStealTheLock(t *testing.T) {
	if !lockingWorks() {
		t.Skipf("advisory locks unsupported on %s", runtime.GOOS)
	}
	dir := t.TempDir()

	// Asking twice must not leave the asker holding it: a dashboard that
	// polls would otherwise lock out the session it is describing.
	Live(dir, validTestSessionID)
	Live(dir, validTestSessionID)

	held, err := Hold(dir, validTestSessionID)
	if err != nil {
		t.Fatalf("the session could not start after being inspected: %v", err)
	}
	_ = held.Close()
}

func TestTheLockFileIsPrivate(t *testing.T) {
	if !lockingWorks() {
		t.Skipf("advisory locks unsupported on %s", runtime.GOOS)
	}
	dir := t.TempDir()
	held, err := Hold(dir, validTestSessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	info, err := os.Stat(filepath.Join(dir, validTestSessionID+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %v, want 0600", perm)
	}
}

func TestHoldingCreatesTheDirectory(t *testing.T) {
	if !lockingWorks() {
		t.Skipf("advisory locks unsupported on %s", runtime.GOOS)
	}
	dir := filepath.Join(t.TempDir(), "sessions")

	held, err := Hold(dir, validTestSessionID)
	if err != nil {
		t.Fatalf("holding in a directory that does not exist yet: %v", err)
	}
	_ = held.Close()
}

func TestASecondHolderIsRefused(t *testing.T) {
	if !lockingWorks() {
		t.Skipf("advisory locks unsupported on %s", runtime.GOOS)
	}
	dir := t.TempDir()
	first, err := Hold(dir, validTestSessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	// Two processes editing one session's files is how a transcript ends up
	// describing work neither of them did.
	if second, err := Hold(dir, validTestSessionID); err == nil {
		_ = second.Close()
		t.Fatal("two holders of one session")
	}
}

// lockingWorks reports whether this platform supports the advisory locks
// liveness is built on. The unsupported platforms are real — lock_windows.go
// and lock_other.go both return ErrUnsupported — so these tests skip rather
// than assert a behaviour that cannot exist there.
func lockingWorks() bool {
	dir, err := os.MkdirTemp("", "kolk-lockprobe")
	if err != nil {
		return false
	}
	defer func() { _ = os.RemoveAll(dir) }()

	held, err := Hold(dir, validTestSessionID)
	if err != nil {
		return false
	}
	_ = held.Close()
	return true
}
