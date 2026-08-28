package cli

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// paidOnlyCatalog is the case the policy exists for: every tool-capable model
// costs money. Today the session quietly starts on the cheapest of them.
func paidOnlyCatalog() []provider.ModelInfo {
	cheap := provider.ModelInfo{ID: "vendor/cheap", Name: "cheap", ContextLength: 128000,
		SupportedParameters: []string{"tools"}, Description: "coding model"}
	cheap.Pricing.Prompt, cheap.Pricing.Completion = "0.0000005", "0.000001"
	dear := provider.ModelInfo{ID: "vendor/dear", Name: "dear", ContextLength: 200000,
		SupportedParameters: []string{"tools"}, Description: "coding model"}
	dear.Pricing.Prompt, dear.Pricing.Completion = "0.00001", "0.00003"
	return []provider.ModelInfo{cheap, dear}
}

// B12.13a. The default policy never starts a session on a billed model without
// being told to. A first run is exactly when someone has no idea what anything
// costs, and "cheapest available" is still a charge they did not agree to.
func TestFreeFirstNeverStartsOnAPaidModelByDefault(t *testing.T) {
	choice := applyFreeExhausted(chooseDefaultModel(paidOnlyCatalog()), engine.OnFreeExhaustedFree)
	if !choice.Free {
		t.Fatalf("the default policy chose the billed model %q", choice.Model)
	}
	if choice.Model != defaultModel {
		t.Errorf("chose %q, want the free router %q when the catalogue offers no free tool-capable model",
			choice.Model, defaultModel)
	}
	if !strings.Contains(choice.Warning, "free") {
		t.Errorf("warning %q does not explain why a router was chosen over a listed model", choice.Warning)
	}
}

// `paid` is the opt-in, and it must still say what it is doing: a charge nobody
// is told about is the same surprise whether or not it was configured.
func TestPaidPolicyTakesTheCheapestAndSaysSo(t *testing.T) {
	choice := applyFreeExhausted(chooseDefaultModel(paidOnlyCatalog()), engine.OnFreeExhaustedPaid)
	if choice.Free || choice.Model != "vendor/cheap" {
		t.Fatalf("paid policy chose %q (free=%v), want the cheapest billed model", choice.Model, choice.Free)
	}
	if !strings.Contains(choice.Warning, "charges may apply") {
		t.Errorf("warning %q does not say the session will be billed", choice.Warning)
	}
}

// `stop` is for someone who wants no substitution at all. It must refuse in a
// way that names the setting, or it is just a broken install.
func TestStopPolicyRefusesRatherThanSubstituting(t *testing.T) {
	choice := applyFreeExhausted(chooseDefaultModel(paidOnlyCatalog()), engine.OnFreeExhaustedStop)
	if choice.Model != "" {
		t.Fatalf("stop policy chose %q, want no model at all", choice.Model)
	}
	if !strings.Contains(choice.Warning, "routing.on_free_exhausted") {
		t.Errorf("refusal %q does not name the setting that changes it", choice.Warning)
	}
}

// A free model present in the catalogue is chosen under every policy: the
// setting governs what happens when free runs out, never whether free is
// preferred in the first place.
func TestEveryPolicyStillPrefersAFreeModel(t *testing.T) {
	for _, policy := range []string{engine.OnFreeExhaustedFree, engine.OnFreeExhaustedPaid, engine.OnFreeExhaustedStop} {
		choice := applyFreeExhausted(chooseDefaultModel(sampleCatalog()), policy)
		if !choice.Free {
			t.Errorf("policy %q chose the billed model %q while a free one was listed", policy, choice.Model)
		}
	}
}
