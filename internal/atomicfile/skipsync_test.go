package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

// SkipFileSync gives up the fsync, never the atomicity: the replacement is
// still all-or-nothing, and what a reader gets afterwards is the new file.
// It exists for the session header (OPTIMIZATION_PLAN.md O6), which is
// rebuilt from the transcript that was written durably.
func TestSkipFileSyncStillReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "header.json")
	if err := Write(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := WriteOptions{SkipDirSync: true, SkipFileSync: true}
	if err := WriteWith(path, []byte("second"), 0o600, opts); err != nil {
		t.Fatalf("WriteWith: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "second" {
		t.Fatalf("contents = %q, want the replacement", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	// And no temp file is left beside it: an unsynced write is still a
	// cleaned-up one.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d files, want only the target: %v", len(entries), entries)
	}
}
