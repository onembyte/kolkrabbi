package bus

import (
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
