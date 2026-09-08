package engine

import (
	"bytes"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// The windows each call reports are the session's, latest reading per
// window, in a fixed order: the five-hour first, the seven-day next, then
// any model-specific ones by name.
func TestPlanLimitsAreKeptPerWindowInOrder(t *testing.T) {
	a := &Agent{Options: Options{Out: &bytes.Buffer{}}}
	a.record("main", provider.Meta{Billing: provider.BillingSubscription, Limits: []provider.PlanLimit{
		{Window: "seven_day_fable", Used: 0.6}, {Window: "seven_day", Used: 0.4},
	}}, 0)
	a.record("main", provider.Meta{Billing: provider.BillingSubscription, Limits: []provider.PlanLimit{
		{Window: "five_hour", Used: 0.8}, {Window: "seven_day", Used: 0.5},
	}}, 0)
	got := a.PlanLimits()
	if len(got) != 3 || got[0].Window != "five_hour" || got[1].Window != "seven_day" || got[1].Used != 0.5 || got[2].Window != "seven_day_fable" {
		t.Fatalf("plan limits = %+v", got)
	}
	if a.PlanLimits()[1].Used != 0.5 {
		t.Fatal("an older reading survived a newer one")
	}
}

// A subscription is not billed by the dollar, so the run's cost line stays
// out of the transcript; a metered session keeps it.
func TestRunCostLineStaysOutOnASubscription(t *testing.T) {
	for _, tc := range []struct {
		billing string
		want    bool
	}{{provider.BillingSubscription, false}, {provider.BillingAPIMetered, true}} {
		var out bytes.Buffer
		a := &Agent{Options: Options{Out: &out}}
		a.runSpend = &spend{}
		a.runSpend.add(1.10)
		a.record("main", provider.Meta{Billing: tc.billing, Cost: 1.10}, 0)
		a.noteRunCost()
		if got := bytes.Contains(out.Bytes(), []byte("run so far")); got != tc.want {
			t.Errorf("billing %s: run cost line shown = %v, want %v:\n%s", tc.billing, got, tc.want, out.String())
		}
	}
}
