package cli

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// A subscription shows no dollars: the plan's windows are its measure, and
// the cost cell says the billing mode instead.
func TestASubscriptionShowsNoDollars(t *testing.T) {
	if got := costLabel(1.10, provider.BillingSubscription); got != "subscription" {
		t.Fatalf("cost label = %q, want subscription", got)
	}
	if got := costLabel(1.10, provider.BillingAPIMetered); got != "$1.10" {
		t.Fatalf("metered label = %q", got)
	}
}

// The window names the vendor uses become short labels on the meters.
func TestPlanWindowLabels(t *testing.T) {
	for window, want := range map[string]string{
		"five_hour": "5h", "seven_day": "7d", "seven_day_fable": "fable", "seven_day_opus": "opus", "monthly": "monthly",
	} {
		if got := planWindowLabel(window); got != want {
			t.Errorf("label(%q) = %q, want %q", window, got, want)
		}
	}
}
