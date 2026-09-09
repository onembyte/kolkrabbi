package stats

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkRatingsByModel is the O0.4 baseline for OPTIMIZATION_PLAN.md O5:
// startup folds the whole of stats.jsonl to learn how models were rated, and
// does it twice (cli/run.go:830 and cli/candidates.go:29), so double these
// numbers for what a cold `kolk` pays before it draws anything.
//
// The record mix is a session's real shape: four calls per rated turn (a tool
// loop), one rating line per turn, across a handful of models.
func BenchmarkRatingsByModel(b *testing.B) {
	models := []string{"vendor/a", "vendor/b", "vendor/c", "vendor/d"}
	for _, records := range []int{1_000, 20_000} {
		b.Run(fmt.Sprintf("%dk", records/1000), func(b *testing.B) {
			dir := b.TempDir()
			base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
			for i := 0; i < records; i++ {
				turn := fmt.Sprintf("t_%08d", i/5)
				record := Record{
					Kind: "call", Time: base.Add(time.Duration(i) * time.Second),
					Session: fmt.Sprintf("s_%06d", i/50), Turn: turn, Mode: "agent", Role: "main",
					Model: models[i%len(models)], PromptTokens: 4000, CompletionTokens: 600,
					Cost: 0.0031, Billing: "gateway", Ms: 2400, ToolCalls: 2,
				}
				if i%5 == 4 {
					record = Record{Kind: "rating", Time: record.Time, Session: record.Session, Turn: turn, Rating: i%5 + 1}
				}
				if err := Append(dir, record); err != nil {
					b.Fatalf("Append: %v", err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ratings, err := RatingsByModel(dir)
				if err != nil {
					b.Fatalf("RatingsByModel: %v", err)
				}
				if len(ratings) == 0 {
					b.Fatal("no ratings folded from a log that contains them")
				}
			}
		})
	}
}
