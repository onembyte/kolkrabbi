package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestDurabilityErrorSaysReplacementWasCommitted(t *testing.T) {
	cause := errors.New("sync refused")
	err := &DurabilityError{Path: "/bin/kolk", Err: cause}
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "was replaced") ||
		!strings.Contains(err.Error(), "/bin/kolk") {
		t.Fatalf("durability error = %v", err)
	}
}

func TestWriteCreatesAndReplaces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")

	if err := Write(p, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "first" {
		t.Errorf("got %q, want first", b)
	}

	if err := Write(p, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "second" {
		t.Errorf("got %q, want second", b)
	}
}

func TestWriteHonoursPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := t.TempDir()

	for _, perm := range []os.FileMode{0o600, 0o644, 0o755} {
		p := filepath.Join(dir, fmt.Sprintf("m%o.json", perm))
		if err := Write(p, []byte("x"), perm); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != perm {
			t.Errorf("mode = %o, want %o", info.Mode().Perm(), perm)
		}
	}
}

// The temp file must live in the target's directory, or rename cannot reach it
// across a filesystem boundary — and /tmp very often is one.
func TestWriteLeavesNoDebris(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")

	for i := 0; i < 5; i++ {
		if err := Write(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v; a temp file was left behind", names)
	}
}

// Two kolk processes saving the same session is an ordinary Tuesday: a REPL in
// one terminal and `kolk -p` in another. With a fixed ".tmp" name they shred
// each other's data; with a unique one, the last writer simply wins.
func TestConcurrentWritesNeverProduceAMixture(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")

	const writers = 8
	payloads := make([]string, writers)
	for i := range payloads {
		// Distinct lengths and contents, so a torn write is unmistakable.
		payloads[i] = strings.Repeat(string(rune('a'+i)), 4096+i)
	}

	var wg sync.WaitGroup
	errs := make(chan error, writers*4)
	for round := 0; round < 4; round++ {
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if err := Write(p, []byte(payloads[i]), 0o600); err != nil {
					errs <- err
				}
			}(i)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Write: %v", err)
	}

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range payloads {
		if got == want {
			return // exactly one writer's payload, whole
		}
	}
	t.Errorf("the file is a mixture of writers, length %d; no single payload matched", len(got))
}

// A reader must see the old contents or the new ones, never an empty file.
func TestReadersNeverSeeAHalfWrittenFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	old := strings.Repeat("o", 8192)
	fresh := strings.Repeat("n", 8192)

	if err := Write(p, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var readErr error
	go func() {
		defer close(done)
		for i := 0; i < 400; i++ {
			b, err := os.ReadFile(p)
			if err != nil {
				readErr = fmt.Errorf("the file vanished mid-replace: %w", err)
				return
			}
			if s := string(b); s != old && s != fresh {
				readErr = fmt.Errorf("read a partial file of length %d", len(s))
				return
			}
		}
	}()

	for i := 0; i < 100; i++ {
		payload := fresh
		if i%2 == 0 {
			payload = old
		}
		if err := Write(p, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	<-done
	if readErr != nil {
		t.Error(readErr)
	}
}

func TestWriteReportsAMissingDirectory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope", "x.json")
	err := Write(p, []byte("x"), 0o600)
	if err == nil {
		t.Fatal("Write into a missing directory should fail; creating it silently is the caller's decision")
	}
	if !strings.Contains(err.Error(), "temporary file") {
		t.Errorf("err = %v, should say where it got stuck", err)
	}
}

// SkipDirSync gives up one fsync and nothing else: the replacement is still
// atomic, the bytes are still the new ones, and the temp file is still gone.
func TestWriteWithSkipDirSyncStillReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.json")

	if err := Write(p, []byte("boundary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteWith(p, []byte("interval"), 0o600, WriteOptions{SkipDirSync: true}); err != nil {
		t.Fatalf("interval write: %v", err)
	}

	if b, _ := os.ReadFile(p); string(b) != "interval" {
		t.Errorf("got %q, want the interval write's contents", b)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		// A skipped directory sync must not mean a skipped cleanup: debris
		// beside a session directory is how a sessions listing fills with junk.
		t.Errorf("directory holds %d entries, want only the target", len(entries))
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %v, want 0600", perm)
	}
}

// The zero value is the durable one, so a caller who never heard of options
// keeps the guarantee this package exists for.
func TestWriteDefaultsToSyncingTheDirectory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x")
	if err := WriteWith(p, []byte("durable"), 0o600, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "durable" {
		t.Errorf("got %q, want durable", b)
	}
	if (WriteOptions{}).SkipDirSync {
		t.Error("the zero WriteOptions skips the directory sync; the default must be the safe one")
	}
}
