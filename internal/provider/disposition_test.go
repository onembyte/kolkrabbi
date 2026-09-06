package provider

import (
	"strings"
	"testing"
)

// Plan 24's rule, made a gate (V34.4c.0): every provider kolk names has a
// disposition — shipped, chosen, investigating or deferred — with its access
// path, billing mode and the evidence behind it; and no model row may claim a
// provider-CLI path for a provider whose disposition is not shipped.
func TestEveryProviderHasADispositionAndOnlyShippedOnesHaveCLIRows(t *testing.T) {
	seen := map[string]bool{}
	for _, plan := range planCatalog {
		if seen[plan.Provider] {
			continue
		}
		seen[plan.Provider] = true
		d, ok := dispositionFor(plan.Provider)
		if !ok {
			t.Fatalf("provider %q has plan rows and no disposition", plan.Provider)
		}
		if d.Status == "" || d.AccessPath == "" || d.Billing == "" || len(d.Evidence) == 0 {
			t.Fatalf("disposition for %q is missing a field: %+v", plan.Provider, d)
		}
		if d.Status != dispositionShipped && d.Status != dispositionDeferred && len(d.Blockers) == 0 {
			t.Fatalf("%q is %s and names nothing that blocks it", plan.Provider, d.Status)
		}
	}
	for _, row := range planModelCatalog {
		if row.Access != "provider CLI" {
			continue
		}
		d, ok := dispositionFor(row.Provider)
		if !ok || d.Status != dispositionShipped {
			t.Fatalf("model row %s/%s claims a provider-CLI path but %q is not shipped (%+v)", row.Plan, row.Model, row.Provider, d)
		}
	}
	// The owner's 2026-09-05 choice, recorded as data.
	for provider, want := range map[string]string{
		"anthropic": dispositionShipped, "openai": dispositionShipped,
		"google": dispositionChosen, "xai": dispositionChosen, "github": dispositionChosen, "perplexity": dispositionInvestigating,
		"mistral": dispositionDeferred, "deepseek": dispositionDeferred, "qwen": dispositionDeferred, "cohere": dispositionDeferred,
	} {
		d, _ := dispositionFor(provider)
		if d.Status != want {
			t.Fatalf("%s disposition = %q, want %q", provider, d.Status, want)
		}
	}
	// A chosen API-key path names the key shape redaction already knows.
	for _, provider := range []string{"xai", "google"} {
		d, _ := dispositionFor(provider)
		if d.KeyShape == "" || !strings.HasPrefix(d.APIBase, "https://") {
			t.Fatalf("%s: an API-key path needs its key shape and base URL: %+v", provider, d)
		}
	}
	if d, _ := dispositionFor("github"); d.APIBase != "" || !strings.Contains(strings.Join(d.Evidence, " "), "retired") {
		t.Fatalf("github must record GitHub Models as retired and carry no API base: %+v", d)
	}
}
