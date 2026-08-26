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
