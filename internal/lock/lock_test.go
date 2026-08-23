package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTryReportsTheHolderAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.lock")
	held, err := Try(path)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("lock mode = %04o, want 0600", got)
	}

	_, err = Try(path)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("second Try error = %v, want ErrBusy", err)
	}
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("second Try error %T, want *BusyError", err)
	}
	if busy.PID != os.Getpid() {
		t.Errorf("holder PID = %d, want %d", busy.PID, os.Getpid())
	}

	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := held.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	next, err := Try(path)
	if err != nil {
		t.Fatalf("Try after release: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("release removed the lock file: %v", err)
	}
}

func TestAcquireHonorsContextAndThenSucceedsAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.lock")
	held, err := Try(path)
	if err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(cancelled, path); !errors.Is(err, context.Canceled) {
		t.Errorf("Acquire with cancelled context = %v, want context.Canceled", err)
	}
	deadline, cancelDeadline := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelDeadline()
	if _, err := Acquire(deadline, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Acquire after deadline = %v, want context.DeadlineExceeded", err)
	}

	type result struct {
		lock *File
		err  error
	}
	resultCh := make(chan result, 1)
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	go func() {
		got, err := Acquire(ctx, path)
		resultCh <- result{lock: got, err: err}
	}()

	// The immediate Try assertion in the first test proves contention. This
	// short delay gives Acquire time to enter its bounded retry loop before the
	// release, without making correctness depend on a precise duration.
	time.Sleep(20 * time.Millisecond)
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}

	got := <-resultCh
	if got.err != nil {
		t.Fatalf("Acquire after release: %v", got.err)
	}
	if err := got.lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStaleMetadataIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.lock")
	if err := os.WriteFile(path, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	held, err := Try(path)
	if err != nil {
		t.Fatalf("stale metadata blocked acquisition: %v", err)
	}
	defer func() { _ = held.Close() }()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strconv.Itoa(os.Getpid())
	if strings.TrimSpace(string(body)) != want {
		t.Errorf("lock metadata = %q, want PID %s", body, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("repaired lock mode = %04o, want 0600", got)
	}
}

func TestConcurrentTryHasOneOwner(t *testing.T) {
	const contenders = 24
	path := filepath.Join(t.TempDir(), "manifest.lock")
	start := make(chan struct{})
	release := make(chan struct{})
	results := make(chan bool, contenders)

	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			held, err := Try(path)
			if err != nil {
				if !errors.Is(err, ErrBusy) {
					t.Errorf("Try: %v", err)
				}
				results <- false
				return
			}
			results <- true
			<-release
			if err := held.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}

	close(start)
	owners := 0
	for i := 0; i < contenders; i++ {
		if <-results {
			owners++
		}
	}
	close(release)
	wg.Wait()
	if owners != 1 {
		t.Errorf("simultaneous owners = %d, want 1", owners)
	}
}
