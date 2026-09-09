package bus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/protocol"
)

// benchDeltaPayload is one streamed content delta of ~1 KB, the shape
// engine.RunTurn publishes once per token.
func benchDeltaPayload(tb testing.TB) json.RawMessage {
	tb.Helper()
	data, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: strings.Repeat("x", 1024)})
	if err != nil {
		tb.Fatalf("marshal delta payload: %v", err)
	}
	return data
}

// BenchmarkPublish is the O0.1 baseline for OPTIMIZATION_PLAN.md O1: the cost
// of one Publish with and without a spill file, so the fsync-per-token claim
// has a number attached before anything moves.
//
// One iteration is one Publish, not a whole turn: O1's success criterion is
// "spill within 3x of memory", which is a per-publish ratio, and a fixed batch
// of 10,000 publishes per op would make the spill variant take about half a
// minute per iteration. Multiply ns/op by the token count for a turn. The
// spill-bytes/op metric is what the file grows by per event, which is the
// second half of the finding: nothing rotates it.
func BenchmarkPublish(b *testing.B) {
	payload := benchDeltaPayload(b)

	b.Run("memory", func(b *testing.B) {
		bus, err := New(testSession, Options{})
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := bus.Publish(Event{Turn: testTurn, Type: protocol.EventMessageDelta, Data: payload}); err != nil {
				b.Fatalf("Publish: %v", err)
			}
		}
		b.StopTimer()
		b.ReportMetric(0, "spill-bytes/op")
	})

	b.Run("spill", func(b *testing.B) {
		spill := filepath.Join(b.TempDir(), "events.ndjson")
		bus, err := New(testSession, Options{SpillPath: spill})
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := bus.Publish(Event{Turn: testTurn, Type: protocol.EventMessageDelta, Data: payload}); err != nil {
				b.Fatalf("Publish: %v", err)
			}
		}
		b.StopTimer()
		if err := bus.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
		info, err := os.Stat(spill)
		if err != nil {
			b.Fatalf("stat spill: %v", err)
		}
		b.ReportMetric(float64(info.Size())/float64(b.N), "spill-bytes/op")
	})
}

// BenchmarkPublishTurn is the same cost expressed the way a person feels it:
// one op is a 10,000-delta answer, which is what OPTIMIZATION_PLAN.md O1 means
// by "a 2,000-token answer spends seconds in fsync". Skipped by default under
// -short because the spill variant is tens of seconds per iteration.
func BenchmarkPublishTurn(b *testing.B) {
	const deltas = 10_000
	payload := benchDeltaPayload(b)

	for _, tc := range []struct{ name, spillDir string }{{name: "memory"}, {name: "spill", spillDir: b.TempDir()}} {
		b.Run(tc.name, func(b *testing.B) {
			if testing.Short() {
				b.Skip("a 10,000-publish turn against a spill file is tens of seconds")
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				options := Options{}
				if tc.spillDir != "" {
					options.SpillPath = filepath.Join(tc.spillDir, fmt.Sprintf("turn-%d.ndjson", i))
				}
				bus, err := New(testSession, options)
				if err != nil {
					b.Fatalf("New: %v", err)
				}
				b.StartTimer()
				for d := 0; d < deltas; d++ {
					if _, err := bus.Publish(Event{Turn: testTurn, Type: protocol.EventMessageDelta, Data: payload}); err != nil {
						b.Fatalf("Publish: %v", err)
					}
				}
				b.StopTimer()
				if err := bus.Close(); err != nil {
					b.Fatalf("Close: %v", err)
				}
				b.StartTimer()
			}
		})
	}
}
