package engine

import (
	"strings"
	"testing"
)

// V34.4b, the owner's decision: the dial has a fifth rung, `ultra`, reachable
// as a word or as 5. It is its own level — not a spelling of max — with a
// budget above max on every dimension the dial governs, and its own tier.
func TestUltraIsTheFifthRungOfTheDial(t *testing.T) {
	for _, spelling := range []string{"ultra", "ULTRA", "u", "5"} {
		got, ok := NormalizeEffort(spelling)
		if !ok || got != EffortUltra {
			t.Fatalf("NormalizeEffort(%q) = %q, %v; want ultra", spelling, got, ok)
		}
	}
	if got, _ := NormalizeEffort("max"); got == EffortUltra {
		t.Fatal("max normalised to ultra; they are different rungs")
	}
	if got, _ := NormalizeEffort("xhigh"); got != EffortMax {
		t.Fatalf("xhigh = %q; the vendor's xhigh stays kolk's max", got)
	}
	if len(CanonicalEfforts) != 5 || CanonicalEfforts[4] != EffortUltra {
		t.Fatalf("CanonicalEfforts = %v, want five rungs ending in ultra", CanonicalEfforts)
	}
	for _, mode := range []string{ModeCode, ModeChat} {
		if MaxRoundsFor(mode, EffortUltra) <= MaxRoundsFor(mode, EffortMax) {
			t.Fatalf("%s: ultra rounds %d not above max %d", mode, MaxRoundsFor(mode, EffortUltra), MaxRoundsFor(mode, EffortMax))
		}
	}
	if TimeoutForEffort(EffortUltra) <= TimeoutForEffort(EffortMax) {
		t.Fatal("ultra's timeout is not above max's")
	}
	if maxTasksFor(EffortUltra) <= maxTasksFor(EffortMax) {
		t.Fatal("ultra's orchestration width is not above max's")
	}

	a := New(Options{Model: "base/model", Tiers: map[string]string{"ultra": "big/model"}})
	if err := a.SetEffort("5"); err != nil || a.Effort != EffortUltra {
		t.Fatalf("SetEffort(5) = %v, effort %q; want ultra", err, a.Effort)
	}
	if got := a.ModelForEffort(EffortUltra); got != "big/model" {
		t.Fatalf("effort.ultra.model = %q, want the ultra tier", got)
	}
	if got := a.ModelForEffort(EffortMax); got != "base/model" {
		t.Fatalf("max resolved to %q; the ultra tier is no longer max's legacy spelling", got)
	}
	err := a.SetEffort("extreme")
	if err == nil || !strings.Contains(err.Error(), "ultra (5)") {
		t.Fatalf("the error for a bad level = %v, want the five rungs named", err)
	}
}
