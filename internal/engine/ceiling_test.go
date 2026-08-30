package engine

import (
	"strings"
	"testing"
)

// The rule, in the user's own example: "if i select sonnet model, and select
// agentic, only sonnet and haiku have to be available. not opus or fable".
func TestSelectingSonnetPutsOpusAndFableOutOfReach(t *testing.T) {
	const ceiling = "claude-sonnet"
	for _, above := range []string{"claude-opus", "claude-fable"} {
		if got := ClampToCeiling(above, ceiling); got != ceiling {
			t.Errorf("ClampToCeiling(%q, %q) = %q, want the ceiling: a model above it was allowed",
				above, ceiling, got)
		}
	}
	// Downward is the whole point: mechanical work belongs on the cheap model.
	if got := ClampToCeiling("claude-haiku", ceiling); got != "claude-haiku" {
		t.Errorf("routing down to haiku was blocked: got %q", got)
	}
	// And the ceiling itself is obviously allowed.
	if got := ClampToCeiling(ceiling, ceiling); got != ceiling {
		t.Errorf("the selected model itself was rewritten to %q", got)
	}
}

// What a session can honestly state: which models the ceiling refuses.
func TestTheModelsAboveTheCeilingAreNamed(t *testing.T) {
	got := ModelsAboveCeiling("claude-sonnet")
	want := []string{"claude-fable", "claude-opus"}
	if len(got) != len(want) {
		t.Fatalf("blocked = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blocked = %v, want %v", got, want)
		}
	}
	// Selecting the strongest model refuses nothing, so there is nothing to say.
	if top := ModelsAboveCeiling("claude-fable"); len(top) != 0 {
		t.Errorf("the strongest model reported %v as out of reach", top)
	}
	// And the cheapest rung refuses everything above it.
	if bottom := ModelsAboveCeiling("claude-haiku"); len(bottom) != 3 {
		t.Errorf("the cheapest model reported %v out of reach, want three", bottom)
	}
	// A model on no ladder makes no claim either way.
	if unranked := ModelsAboveCeiling("mystery-model"); unranked != nil {
		t.Errorf("an unranked model invented a ladder: %v", unranked)
	}
}

// "and the same for other vendor subs connections (codex, ollama, gemini, etc)"
func TestTheCeilingAppliesToEveryVendor(t *testing.T) {
	for _, tc := range []struct{ above, ceiling string }{
		{"gpt-5.6-pro", "gpt-5.6-sol"},
		{"gemini-2.5-pro", "gemini-2.5-flash"},
		{"gemini-2.5-ultra", "gemini-2.5-pro"},
	} {
		if got := ClampToCeiling(tc.above, tc.ceiling); got != tc.ceiling {
			t.Errorf("ClampToCeiling(%q, %q) = %q, want the ceiling", tc.above, tc.ceiling, got)
		}
	}
}

// A ceiling on one vendor says nothing about another's bill, and silently
// rewriting a model configured for a different provider would be its own
// surprise.
func TestACeilingDoesNotReachAcrossVendors(t *testing.T) {
	if got := ClampToCeiling("gpt-5.6-pro", "claude-haiku"); got != "gpt-5.6-pro" {
		t.Errorf("a claude ceiling rewrote a codex model to %q", got)
	}
	if got := ClampToCeiling("some-openrouter/model-x", "claude-haiku"); got != "some-openrouter/model-x" {
		t.Errorf("a claude ceiling rewrote an unrelated model to %q", got)
	}
}

// A model nobody ranked is never clamped: a ceiling that guessed would be worse
// than one that admits it does not know.
func TestAnUnrankedModelIsLeftAlone(t *testing.T) {
	if got := ClampToCeiling("mystery-model", "also-unknown"); got != "mystery-model" {
		t.Errorf("two unranked models were compared anyway: %q", got)
	}
	if got := ModelsAboveCeiling("mystery-model"); got != nil {
		t.Errorf("an unranked model invented a ladder: %v", got)
	}
}

// The catalogue prefixes some ids with a provider and a plan model carries
// none. Both have to land on the same rung or the ceiling leaks.
func TestAProviderPrefixDoesNotHideTheRung(t *testing.T) {
	if got := ClampToCeiling("anthropic/claude-opus", "claude-sonnet"); got != "claude-sonnet" {
		t.Errorf("a prefixed model escaped the ceiling: %q", got)
	}
	if got := ClampToCeiling("claude-opus-4-1", "claude-sonnet"); got != "claude-sonnet" {
		t.Errorf("a versioned model escaped the ceiling: %q", got)
	}
}

// An empty choice takes the ceiling rather than nothing at all.
func TestAnEmptyChoiceBecomesTheCeiling(t *testing.T) {
	if got := ClampToCeiling("", "claude-sonnet"); got != "claude-sonnet" {
		t.Errorf("an empty model resolved to %q", got)
	}
}

// Every rung has to work as a ceiling: itself allowed, everything above it not.
func TestEveryRungIsAUsableCeiling(t *testing.T) {
	for _, ladder := range vendorLadders {
		for rung, model := range ladder.rungs {
			if got := ClampToCeiling(model, model); got != model {
				t.Errorf("%s rewrote itself to %s", model, got)
			}
			for _, above := range ladder.rungs[:rung] {
				if got := ClampToCeiling(above, model); got != model {
					t.Errorf("under ceiling %s, %s was allowed (got %s)", model, above, got)
				}
			}
			for _, below := range ladder.rungs[rung+1:] {
				if got := ClampToCeiling(below, model); got != below {
					t.Errorf("under ceiling %s, cheaper %s was clamped to %s", model, below, got)
				}
			}
		}
	}
}

// The ladders must not overlap, or a model would have two ranks and the ceiling
// would depend on map order.
func TestNoModelSitsOnTwoLadders(t *testing.T) {
	seen := map[string]string{}
	for _, ladder := range vendorLadders {
		for _, rung := range ladder.rungs {
			if other, clash := seen[rung]; clash {
				t.Errorf("%q is on both the %s and %s ladders", rung, other, ladder.name)
			}
			seen[rung] = ladder.name
			for existing, owner := range seen {
				if existing != rung && owner != ladder.name && strings.HasPrefix(rung, existing) {
					t.Errorf("%q (%s) is a prefix of %q (%s)", existing, owner, rung, ladder.name)
				}
			}
		}
	}
}

// The ceiling has to bind the real routing path, not just the helper. A slot
// configured with a stronger model is the case that matters: the user set it
// once, and selected their model just now.
func TestRoutingHoldsEveryKindToTheSelectedModel(t *testing.T) {
	agent := &Agent{Options: Options{
		Model: "claude-sonnet",
		Slots: map[string]string{
			SlotOrchestrator: "claude-fable",
			SlotWorker:       "claude-opus",
			SlotExplore:      "claude-haiku",
			SlotFast:         "claude-haiku",
		},
	}}

	for kind, slot := range kindSlots {
		got := agent.modelForKind(kind)
		if _, _, known := modelRank(got); !known {
			t.Errorf("kind %v routed to an unranked model %q", kind, got)
			continue
		}
		if got := ClampToCeiling(got, "claude-sonnet"); got != agent.modelForKind(kind) {
			t.Errorf("kind %v (slot %s) routed above the ceiling", kind, slot)
		}
		if got == "claude-opus" || got == "claude-fable" {
			t.Errorf("kind %v routed to %q, above the selected claude-sonnet", kind, got)
		}
	}

	// The orchestrator's own calls are held to it too — it is the one slot
	// someone is most tempted to point at the strongest model.
	if got := agent.orchestrationModel(); got == "claude-fable" || got == "claude-opus" {
		t.Errorf("the orchestrator ran on %q, above the selected claude-sonnet", got)
	}
}

// Routing down is untouched: that is the whole reason the slots exist.
func TestRoutingDownIsStillAllowed(t *testing.T) {
	agent := &Agent{Options: Options{
		Model: "claude-opus",
		Slots: map[string]string{SlotFast: "claude-haiku"},
	}}
	if got := agent.modelForKind(KindBoilerplate); got != "claude-haiku" {
		t.Errorf("mechanical work routed to %q, want the cheap model it was pointed at", got)
	}
}

// With no ceiling worth the name, nothing is rewritten — a session on an
// OpenRouter model must behave exactly as it did before.
func TestAnUnrankedSessionModelChangesNothing(t *testing.T) {
	agent := &Agent{Options: Options{
		Model: "openrouter/some-model",
		Slots: map[string]string{SlotWorker: "claude-opus"},
	}}
	if got := agent.modelForKind(KindEdit); got != "claude-opus" {
		t.Errorf("an unranked session model rewrote the slot to %q", got)
	}
}

// An id can arrive in three shapes and all three name the same rung. Matching
// only the tail left `claude/haiku` unranked — and an unranked model is never
// clamped, so a whole namespace would have been invisible to the ceiling.
func TestEverySpellingOfAModelFindsItsRung(t *testing.T) {
	for _, spelling := range []string{"claude-opus", "anthropic/claude-opus", "claude/opus"} {
		if got := ClampToCeiling(spelling, "claude-sonnet"); got != "claude-sonnet" {
			t.Errorf("%q escaped the ceiling: got %q", spelling, got)
		}
	}
	// And the cheap end still routes through under each spelling.
	for _, spelling := range []string{"claude-haiku", "claude/haiku", "anthropic/claude-haiku"} {
		if got := ClampToCeiling(spelling, "claude-sonnet"); got != spelling {
			t.Errorf("%q was clamped when it is below the ceiling: got %q", spelling, got)
		}
	}
}
