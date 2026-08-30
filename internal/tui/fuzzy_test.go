package tui

import "testing"

// fuzzyScore is meant to replace matchesFilter everywhere a picker filters a
// list: every whitespace token of the query must still be found, in any
// order, but a token no longer needs to be one contiguous substring — "cld"
// should find "claude" the way "claude" itself already does.
func TestFuzzyScoreToleratesAScatteredToken(t *testing.T) {
	if _, ok := fuzzyScore("claude", "cld"); !ok {
		t.Fatal("a scattered subsequence of the row's own name must still match it")
	}
}

// matchesFilter's own reason to exist — order-independent tokens — must
// survive the switch: a query typed in either order still finds the row.
func TestFuzzyScorePreservesTokenOrderIndependence(t *testing.T) {
	for _, query := range []string{"claude max", "max claude"} {
		if _, ok := fuzzyScore("claude max", query); !ok {
			t.Fatalf("query %q did not match its own haystack in a different word order", query)
		}
	}
}

// An empty query is "no filter", not "match nothing" — the contract every
// existing picker already depends on.
func TestFuzzyScoreEmptyQueryMatchesEverything(t *testing.T) {
	if _, ok := fuzzyScore("anything at all", ""); !ok {
		t.Fatal("an empty query must match every row")
	}
}

// A query whose characters are not present, in order, anywhere in the row
// must not match — fuzzy tolerates gaps, not absence.
func TestFuzzyScoreRefusesWhatIsNotThere(t *testing.T) {
	if _, ok := fuzzyScore("claude max", "xyz"); ok {
		t.Fatal("characters absent from the row matched anyway")
	}
}

// The scoring exists to rank, not just filter: a query that runs contiguous
// through one row and scattered through another must rank the contiguous one
// first, so the row a person meant is usually already on top.
func TestFuzzyScoreRanksATighterRunAboveAScatteredOne(t *testing.T) {
	tight, ok := fuzzyScore("claude", "cld")
	if !ok {
		t.Fatal("claude must match cld")
	}
	scattered, ok := fuzzyScore("circled", "cld")
	if !ok {
		t.Fatal("circled must match cld")
	}
	if tight <= scattered {
		t.Fatalf("tight run scored %d, scattered scored %d; want tight strictly higher", tight, scattered)
	}
}

// A match that starts where a person's eye would look for it — the start of
// the row, or the start of a word inside it — must outrank the same
// characters starting mid-word, matching how Claude Code's and Codex's own
// pickers rank a prefix match above a buried one.
func TestFuzzyScoreRanksAWordBoundaryStartAboveAMidWordOne(t *testing.T) {
	atStart, ok := fuzzyScore("model", "mod")
	if !ok {
		t.Fatal("model must match mod")
	}
	midWord, ok := fuzzyScore("chatmode", "mod")
	if !ok {
		t.Fatal("chatmode must match mod")
	}
	if atStart <= midWord {
		t.Fatalf("word-boundary match scored %d, mid-word scored %d; want boundary strictly higher", atStart, midWord)
	}
}

// fuzzyMatches is the boolean-only entry point most call sites want, so a
// filter that never ranks does not have to name a score it will not use.
func TestFuzzyMatchesIsTheBooleanShapeOfFuzzyScore(t *testing.T) {
	if !fuzzyMatches("claude max", "max claude") {
		t.Fatal("fuzzyMatches must agree with fuzzyScore's own ok result")
	}
	if fuzzyMatches("claude max", "xyz") {
		t.Fatal("fuzzyMatches must refuse what fuzzyScore refuses")
	}
}
