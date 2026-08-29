package provider

import "testing"

func TestPlansFilterAcrossProviderAndPlanFields(t *testing.T) {
	if got := Plans("gemini"); len(got) != 3 {
		t.Fatalf("Gemini plans = %d, want 3", len(got))
	}
	if got := Plans("pro"); len(got) < 3 {
		t.Fatalf("Pro plans = %d, want at least 3", len(got))
	}
	if got := Plans("subscription"); len(got) < 5 {
		t.Fatalf("subscription plans = %d, want at least 5", len(got))
	}
}

func TestPlansAreCredentialFreeMetadata(t *testing.T) {
	for _, plan := range Plans("") {
		if plan.Provider == "" || plan.Name == "" || plan.Connector == "" ||
			plan.Auth == "" || plan.Billing == "" {
			t.Fatalf("incomplete plan metadata: %#v", plan)
		}
	}
}

// A plan is described the way people describe it — a provider and a tier, in
// whatever order comes to mind. Matching each field against the whole filter
// meant the words had to land inside ONE field, in the order that field prints:
// "claude max" worked only because it is verbatim the plan's name.
func TestPlansMatchesEveryWordInAnyField(t *testing.T) {
	for _, filter := range []string{
		"claude max",
		"max claude",
		"anthropic max",
		"max anthropic",
		"anthropic claude max",
	} {
		got := Plans(filter)
		found := false
		for _, plan := range got {
			if plan.Name == "Claude Max" {
				found = true
			}
		}
		if !found {
			t.Errorf("Plans(%q) did not find Claude Max; got %d rows", filter, len(got))
		}
	}
}

// Every word has to match, or a filter stops narrowing anything.
func TestPlansRequiresEveryWordToMatch(t *testing.T) {
	if got := Plans("anthropic perplexity"); len(got) != 0 {
		t.Errorf("Plans(\"anthropic perplexity\") = %d rows, want none: no plan is both", len(got))
	}
}

func TestPlansWithNoFilterReturnsEverything(t *testing.T) {
	if len(Plans("")) != len(planCatalog) || len(Plans("   ")) != len(planCatalog) {
		t.Error("an empty filter must not narrow the catalogue")
	}
}

// The user has an ollama.com subscription and asked for it by name. It is not
// the local Ollama that `kolk localia` runs on this machine.
func TestTheOllamaPlanIsInTheCatalogue(t *testing.T) {
	got := Plans("ollama")
	if len(got) == 0 {
		t.Fatal("`kolk plans ollama` finds nothing")
	}
	plan := got[0]
	if plan.Provider != "ollama" || plan.Connector != "ollama" {
		t.Errorf("plan = %#v", plan)
	}
	if plan.Auth != "provider CLI" {
		t.Errorf("auth = %q, want provider CLI: `ollama signin` is how you sign in", plan.Auth)
	}
	// Sandbox means the vendor's CLI enforces its own tool-execution jail.
	// `ollama run` has no such flag; it runs inference, not an agent.
	if plan.Sandbox {
		t.Error("the ollama row claims a sandbox the CLI does not have")
	}
}
