package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A debug log is the single most likely place for a key to end up in a public
// issue, so the scrubbing is not a flag and not a caller's responsibility.
func TestDebugLogScrubsEveryLineItWrites(t *testing.T) {
	const key = "sk-or-v1-0123456789abcdef0123456789abcdef0123"
	path := filepath.Join(t.TempDir(), "debug.log")

	log, err := openDebugLog(path)
	if err != nil {
		t.Fatalf("openDebugLog: %v", err)
	}
	log.Printf("authorization: Bearer %s", key)
	log.Printf("plain line with no secret")
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if strings.Contains(string(written), key) {
		t.Fatal("the debug log contains an API key in full")
	}
	if strings.Contains(string(written), key[:20]) {
		t.Fatal("the debug log contains a searchable prefix of an API key")
	}
	if !strings.Contains(string(written), "plain line with no secret") {
		t.Errorf("scrubbing ate an ordinary line:\n%s", written)
	}
}

// Off unless asked for. A diagnostic that writes itself on every run is a
// second copy of the session on disk that nobody asked to keep.
func TestNoDebugLogWithoutTheFlag(t *testing.T) {
	var log *debugLog
	// The nil log is the "off" state, and every call site must survive it
	// without a branch of its own.
	log.Printf("this must not panic and must not write anywhere")
	if err := log.Close(); err != nil {
		t.Errorf("closing a log that was never opened reported an error: %v", err)
	}
}

func TestDebugLogNamesItselfOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	log, err := openDebugLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	if log.Path() != path {
		t.Errorf("Path() = %q, want %q", log.Path(), path)
	}
}

// Every line carries the time, because "what happened just before it broke" is
// the question a debug log exists to answer.
func TestDebugLogTimestampsEveryLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	log, err := openDebugLog(path)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("one")
	log.Printf("two")
	_ = log.Close()

	written, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(written)), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2:\n%s", len(lines), written)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "20") || !strings.Contains(line, "Z ") {
			t.Errorf("line has no RFC3339 timestamp: %q", line)
		}
	}
}
