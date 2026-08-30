package engine

import "testing"

// The roster is what a run may spend on. Rung 0 IS the model the user selected,
// and everything after it is cheaper — so "above the ceiling" is not something
// the roster can express, rather than something it has to reject.
func TestTheRosterBeginsAtTheModelTheUserSelected(t *testing.T) {
	agent := &Agent{Options: Options{Model: "claude-sonnet"}}
	roster := agent.roster(func(string, string) bool { return true })

	if len(roster.Rungs) == 0 {
		t.Fatal("a session on a ranked model has no roster")
	}
	first := roster.Rungs[0]
	if first.Depth != 0 {
		t.Errorf("the first rung is at depth %d, want 0", first.Depth)
	}
	// Verbatim, not a ladder string: a ladder rung is a MATCH PREFIX, and
	// handing one to a backend as a model id would ask a provider for a model
	// nobody offers.
	if first.Model != "claude-sonnet" {
		t.Errorf("rung 0 model = %q, want the session's own model verbatim", first.Model)
	}
	for _, rung := range roster.Rungs {
		if got := ClampToCeiling(rung.Model, "claude-sonnet"); got != rung.Model {
			t.Errorf("rung %q is above the ceiling it was built from", rung.Model)
		}
	}
}

// Cheaper rungs appear only when something says the vendor can actually run
// them. A roster that offers what cannot be spawned is a menu of errors.
func TestACheaperRungAppearsOnlyWhenItsVendorCanRunIt(t *testing.T) {
	agent := &Agent{Options: Options{Model: "claude-sonnet"}}

	none := agent.roster(func(string, string) bool { return false })
	if len(none.Rungs) != 1 {
		t.Errorf("with no vendor available the roster has %d rungs, want only the ceiling", len(none.Rungs))
	}

	all := agent.roster(func(string, string) bool { return true })
	if len(all.Rungs) < 2 {
		t.Fatalf("with the vendor available the roster has %d rungs, want the cheaper ones too", len(all.Rungs))
	}
	if all.Rungs[1].Model != "claude-haiku" {
		t.Errorf("the rung below sonnet is %q, want claude-haiku", all.Rungs[1].Model)
	}
	if all.Rungs[1].Depth != 1 {
		t.Errorf("the second rung is at depth %d, want 1", all.Rungs[1].Depth)
	}
}

// The ceiling is always present, even when the availability answer is no: the
// session is already running on it, so there is nothing to check.
func TestTheCeilingIsNeverCheckedForAvailability(t *testing.T) {
	agent := &Agent{Options: Options{Model: "claude-opus"}}
	var asked []string
	roster := agent.roster(func(_, model string) bool {
		asked = append(asked, model)
		return false
	})
	if len(roster.Rungs) != 1 || roster.Rungs[0].Model != "claude-opus" {
		t.Fatalf("roster = %+v, want just the session model", roster.Rungs)
	}
	// The session model itself is never asked about: it is already running, so
	// the question has been answered by the session existing.
	for _, model := range asked {
		if model == "claude-opus" {
			t.Error("availability was checked for the model the session is already running on")
		}
	}
	// Opus sits at depth 1, so the two rungs below it are what gets asked.
	if len(asked) != 2 {
		t.Errorf("availability was asked for %v, want the two rungs below opus", asked)
	}
}

// A session on a model kolk has not ranked gets a roster of exactly itself. It
// is not an error and not an empty menu: everything runs on the user's model,
// which is what happens today.
func TestAnUnrankedSessionModelHasARosterOfItself(t *testing.T) {
	agent := &Agent{Options: Options{Model: "openrouter/some-model"}}
	roster := agent.roster(func(string, string) bool { return true })
	if len(roster.Rungs) != 1 {
		t.Fatalf("roster has %d rungs, want 1", len(roster.Rungs))
	}
	if roster.Rungs[0].Model != "openrouter/some-model" {
		t.Errorf("rung = %q, want the session model", roster.Rungs[0].Model)
	}
	if roster.Rungs[0].Vendor != "" {
		t.Errorf("vendor = %q, want none for an unranked model", roster.Rungs[0].Vendor)
	}
}

// Selecting the cheapest rung leaves nowhere to go down to, which is a roster
// of one and not a bug.
func TestSelectingTheCheapestRungLeavesOnlyItself(t *testing.T) {
	agent := &Agent{Options: Options{Model: "claude-haiku"}}
	roster := agent.roster(func(string, string) bool { return true })
	if len(roster.Rungs) != 1 {
		t.Errorf("roster has %d rungs, want only the cheapest", len(roster.Rungs))
	}
}

// Every rung id the roster can emit must be one the ceiling can rank, or a
// model could be routed that the clamp cannot see.
func TestEveryRungIDTheRosterEmitsIsRankable(t *testing.T) {
	for _, ladder := range vendorLadders {
		for _, id := range LadderRungIDs(ladder.name) {
			if _, _, known := modelRank(id); !known {
				t.Errorf("rung id %q is not rankable by the ceiling", id)
			}
		}
	}
	if got := LadderRungIDs("no-such-vendor"); got != nil {
		t.Errorf("an unknown vendor produced rungs: %v", got)
	}
}
