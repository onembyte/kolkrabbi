package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHeldIsFalseWhenNoLockFileExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.lock")

	held, err := Held(path)
	if err != nil {
		t.Fatalf("asking about a lock that was never taken: %v", err)
	}
	if held {
		t.Fatal("a lock file that does not exist reported as held")
	}
	// Asking must not answer by creating one. A dashboard that lists sessions
	// would otherwise litter a lock file per session, per poll.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("asking created the lock file: %v", err)
	}
}

func TestHeldIsTrueWhileSomebodyHoldsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.lock")
	file, err := Try(path)
	if err != nil {
		t.Skipf("locking unsupported: %v", err)
	}
	defer file.Close()

	held, err := Held(path)
	if err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("a held lock reported as free")
	}
}

func TestHeldIsFalseAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "released.lock")
	file, err := Try(path)
	if err != nil {
		t.Skipf("locking unsupported: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	held, err := Held(path)
	if err != nil {
		t.Fatal(err)
	}
	// The file outlives the lock, which is exactly why existence is not the
	// question being asked.
	if held {
		t.Fatal("a released lock still reported as held")
	}
}

func TestHeldDoesNotStealTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.lock")
	file, err := Try(path)
	if err != nil {
		t.Skipf("locking unsupported: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if _, err := Held(path); err != nil {
			t.Fatal(err)
		}
	}

	again, err := Try(path)
	if err != nil {
		t.Fatalf("probing left the lock taken: %v", err)
	}
	_ = again.Close()
}
