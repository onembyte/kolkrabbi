package bus

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/protocol"
)

func TestSpillAppendsExactNDJSONFramesToDisk(t *testing.T) {
	tempDir := t.TempDir()
	spillPath := filepath.Join(tempDir, "events.ndjson")

	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	b, err := New(testSession, Options{
		SpillPath: spillPath,
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	e1 := publishText(t, b, "hello")
	e2 := publishText(t, b, "world")

	// Since O1 the frames are written by the bus's writer goroutine, so a test
	// that reads the file behind Publish's back has to wait for it. Nothing in
	// the replay path needs this: Subscribe and Close flush on their own.
	if err := b.flushSpill(); err != nil {
		t.Fatalf("flushSpill: %v", err)
	}

	info, err := os.Stat(spillPath)
	if err != nil {
		t.Fatalf("Stat spill file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("spill file mode = %o, want 0600", info.Mode().Perm())
	}

	content, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Verify the file content can be decoded into the exact envelopes
	f, err := os.Open(spillPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	var decoded []protocol.Envelope
	err = protocol.DecodeStream(f, protocol.StreamNDJSON, func(env protocol.Envelope) error {
		decoded = append(decoded, env)
		return nil
	})
	if err != nil {
		t.Fatalf("DecodeStream: %v (raw content: %q)", err, string(content))
	}

	if len(decoded) != 2 {
		t.Fatalf("decoded %d envelopes, want 2", len(decoded))
	}
	if decoded[0].Seq != e1.Seq || decoded[1].Seq != e2.Seq {
		t.Fatalf("decoded sequences = [%d, %d], want [%d, %d]", decoded[0].Seq, decoded[1].Seq, e1.Seq, e2.Seq)
	}
}

func TestSpillEnablesReplayingEvictedCursorsFromDisk(t *testing.T) {
	tempDir := t.TempDir()
	spillPath := filepath.Join(tempDir, "events.ndjson")

	// Limit in-memory buffer to 2 events
	b, err := New(testSession, Options{
		MaxEvents: 2,
		SpillPath: spillPath,
		Clock:     fixedClock(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	for i := 1; i <= 5; i++ {
		publishText(t, b, "message")
	}

	// In memory only sequences 4 and 5 are retained.
	// But because SpillPath is present, Subscribe(0) should replay all 5 events from disk.
	sub0, err := b.Subscribe(0)
	if err != nil {
		t.Fatalf("Subscribe(0) with spill failed: %v", err)
	}
	assertSequences(t, sub0.Replay(), 1, 2, 3, 4, 5)

	// Subscribe from cursor 2 should replay 3, 4, 5
	sub2, err := b.Subscribe(2)
	if err != nil {
		t.Fatalf("Subscribe(2) with spill failed: %v", err)
	}
	assertSequences(t, sub2.Replay(), 3, 4, 5)
}

func TestSpillRecoversSessionOnReopen(t *testing.T) {
	tempDir := t.TempDir()
	spillPath := filepath.Join(tempDir, "events.ndjson")

	// Create and write 3 events
	b1, err := New(testSession, Options{
		SpillPath: spillPath,
		Clock:     fixedClock(),
	})
	if err != nil {
		t.Fatalf("New 1: %v", err)
	}
	publishText(t, b1, "one")
	publishText(t, b1, "two")
	publishText(t, b1, "three")
	b1.Close()

	// Reopen the same spill path with a new bus instance
	b2, err := New(testSession, Options{
		SpillPath: spillPath,
		Clock:     fixedClock(),
	})
	if err != nil {
		t.Fatalf("New 2: %v", err)
	}
	defer b2.Close()

	// Fourth event must receive sequence 4
	e4 := publishText(t, b2, "four")
	if e4.Seq != 4 {
		t.Fatalf("published seq = %d, want 4 on resumed session", e4.Seq)
	}

	// Subscribe from 0 returns all 4 events
	sub, err := b2.Subscribe(0)
	if err != nil {
		t.Fatalf("Subscribe(0) on resumed session: %v", err)
	}
	assertSequences(t, sub.Replay(), 1, 2, 3, 4)
}

// TestSpillFileIsRewrittenFromTheRetainedWindowWhenItPassesItsCap is the O1.4
// bound. Before it, the file grew for the life of the session with nothing to
// rotate it. The replay contract is the retained window, so rebuilding the file
// from that window loses nothing anybody was promised — and the cursors it can
// no longer serve are refused rather than silently skipped.
func TestSpillFileIsRewrittenFromTheRetainedWindowWhenItPassesItsCap(t *testing.T) {
	const (
		window   = 4
		capBytes = 4 << 10
		total    = 400
	)
	tempDir := t.TempDir()
	spillPath := filepath.Join(tempDir, "events.ndjson")

	b, err := New(testSession, Options{
		MaxEvents:     window,
		MaxSpillBytes: capBytes,
		SpillPath:     spillPath,
		Clock:         fixedClock(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 1; i <= total; i++ {
		publishText(t, b, fmt.Sprintf("message %03d", i))
	}
	if err := b.flushSpill(); err != nil {
		t.Fatalf("flushSpill: %v", err)
	}

	onDisk := readSpill(t, spillPath)
	if len(onDisk) == 0 {
		t.Fatal("the rewritten spill file is empty")
	}
	oldest := onDisk[0].Seq
	if oldest == 1 {
		t.Fatalf("the file still starts at the first event, so %d events never crossed the %d byte cap", total, capBytes)
	}
	for i, env := range onDisk {
		if env.Seq != oldest+uint64(i) {
			t.Fatalf("spill sequence %d at index %d, want %d", env.Seq, i, oldest+uint64(i))
		}
	}
	if onDisk[len(onDisk)-1].Seq != total {
		t.Fatalf("last spilled sequence = %d, want %d", onDisk[len(onDisk)-1].Seq, total)
	}
	// Everything the bus still holds in memory is on disk: the rewrite is
	// bounded below by the window, not by the cap.
	if oldest > total-window+1 {
		t.Fatalf("oldest on disk = %d, want at most %d so the retained window survives", oldest, total-window+1)
	}

	info, err := os.Stat(spillPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	largest := 0
	for _, env := range onDisk {
		frame, err := protocol.EncodeNDJSON(env)
		if err != nil {
			t.Fatalf("EncodeNDJSON: %v", err)
		}
		if len(frame) > largest {
			largest = len(frame)
		}
	}
	if info.Size() > capBytes+int64(largest) {
		t.Fatalf("spill file = %d bytes, want at most the %d byte cap plus one %d byte frame", info.Size(), capBytes, largest)
	}

	// A cursor the rewritten file can still serve is served whole.
	sub, err := b.Subscribe(oldest - 1)
	if err != nil {
		t.Fatalf("Subscribe(%d): %v", oldest-1, err)
	}
	replay := sub.Replay()
	if len(replay) != len(onDisk) || replay[0].Seq != oldest || replay[len(replay)-1].Seq != total {
		t.Fatalf("replay from %d = %d events %d..%d, want %d events %d..%d",
			oldest-1, len(replay), replay[0].Seq, replay[len(replay)-1].Seq, len(onDisk), oldest, total)
	}

	// One older than that is expired, not a replay with a hole in it.
	if _, err := b.Subscribe(oldest - 2); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("Subscribe(%d) error = %v, want ErrCursorExpired", oldest-2, err)
	}

	// The rewrite leaves no temporary file behind, and the session resumes from
	// the rewritten file with its sequence intact.
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "events.ndjson" {
		t.Fatalf("spill directory holds %d entries, want the spill file alone", len(entries))
	}

	resumed, err := New(testSession, Options{MaxEvents: window, MaxSpillBytes: capBytes, SpillPath: spillPath, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("New(resume): %v", err)
	}
	defer resumed.Close()
	if next := publishText(t, resumed, "after the rewrite"); next.Seq != total+1 {
		t.Fatalf("sequence after resuming a rewritten file = %d, want %d", next.Seq, total+1)
	}
}

// TestCloseFlushesEveryFrameStillQueued is the other half of O1.3: with the
// per-event fsync gone, Close is the durability point. Nothing may be lost
// between the last Publish and the last close.
func TestCloseFlushesEveryFrameStillQueued(t *testing.T) {
	const total = 500
	spillPath := filepath.Join(t.TempDir(), "events.ndjson")

	b, err := New(testSession, Options{
		SpillPath:  spillPath,
		SyncPolicy: SyncNever,
		Clock:      fixedClock(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i := 1; i <= total; i++ {
		publishText(t, b, "message")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	onDisk := readSpill(t, spillPath)
	if len(onDisk) != total {
		t.Fatalf("Close left %d frames on disk, want %d", len(onDisk), total)
	}
	if onDisk[0].Seq != 1 || onDisk[total-1].Seq != total {
		t.Fatalf("spilled sequences run %d..%d, want 1..%d", onDisk[0].Seq, onDisk[total-1].Seq, total)
	}
}

// TestReplayFromDiskWaitsForFramesStillInTheWriterQueue is the invariant the
// writer goroutine could most easily have broken: a cursor answered from the
// file must not stop at whatever happened to be flushed, leaving a gap between
// the replay and the first live event.
func TestReplayFromDiskWaitsForFramesStillInTheWriterQueue(t *testing.T) {
	const total = 500
	spillPath := filepath.Join(t.TempDir(), "events.ndjson")

	b, err := New(testSession, Options{
		MaxEvents:  1,
		SpillPath:  spillPath,
		SyncPolicy: SyncNever,
		Clock:      fixedClock(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	for i := 1; i <= total; i++ {
		publishText(t, b, "message")
	}
	sub, err := b.Subscribe(1)
	if err != nil {
		t.Fatalf("Subscribe(1): %v", err)
	}
	replay := sub.Replay()
	if len(replay) != total-1 {
		t.Fatalf("replay after cursor 1 = %d events, want %d", len(replay), total-1)
	}
	for i, env := range replay {
		if env.Seq != uint64(i+2) {
			t.Fatalf("replay sequence %d at index %d, want %d", env.Seq, i, i+2)
		}
	}
}

func readSpill(t *testing.T, path string) []protocol.Envelope {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open spill: %v", err)
	}
	defer f.Close()

	var envelopes []protocol.Envelope
	err = protocol.DecodeStream(f, protocol.StreamNDJSON, func(env protocol.Envelope) error {
		envelopes = append(envelopes, env)
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("DecodeStream: %v", err)
	}
	return envelopes
}
