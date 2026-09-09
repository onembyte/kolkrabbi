package cli

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/stats"
)

// Startup folded the whole usage log twice: once for the engine's slot
// selection and once for the continuity candidates (OPTIMIZATION_PLAN.md O5).
// One process, one fold — proved by moving the log underneath and seeing the
// answer stay put until something says it changed.
func TestTheUsageLogIsFoldedOncePerProcess(t *testing.T) {
	dir := t.TempDir()
	rate(t, dir, "t_1", "vendor/a", 5)

	a, _, _ := newTestApp(t, "")
	if got := a.ratingsByModel(dir)["vendor/a"]; got.Count != 1 || got.Average != 5 {
		t.Fatalf("first fold = %+v, want one rating of 5", got)
	}
	rate(t, dir, "t_2", "vendor/a", 1)
	if got := a.ratingsByModel(dir)["vendor/a"]; got.Count != 1 || got.Average != 5 {
		t.Fatalf("second read = %+v, want the fold already taken, not a second one", got)
	}

	// /rate is the one thing that changes this machine's opinion, and it says
	// so: the next reader folds again.
	a.forgetRatings()
	if got := a.ratingsByModel(dir)["vendor/a"]; got.Count != 2 || got.Average != 3 {
		t.Fatalf("after /rate = %+v, want both ratings", got)
	}
}

func rate(t *testing.T, dir, turn, model string, rating int) {
	t.Helper()
	for _, r := range []stats.Record{
		{Kind: "call", Turn: turn, Model: model},
		{Kind: "rating", Turn: turn, Rating: rating},
	} {
		if err := stats.Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}
}
