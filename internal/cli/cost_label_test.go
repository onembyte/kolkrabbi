package cli

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// The status line's cost: a figure where one is known; where none is, the
// billing mode in a word — a metered session is never blank as if free, and a
// subscription session says so; a local or unknown session says nothing.
func TestTheCostLabelSaysTheBillingModeWhereNothingIsPriced(t *testing.T) {
	for _, tc := range []struct {
		total   float64
		billing string
		want    string
	}{
		{0.42, provider.BillingGateway, "$0.42"},
		{0.42, "mixed", "$0.42 · +metered"},
		{0, provider.BillingAPIMetered, "metered"},
		{0, provider.BillingSubscription, "subscription"},
		{0, provider.BillingLocal, ""},
		{0, provider.BillingUnknown, ""},
		{0, "", ""},
	} {
		if got := costLabel(tc.total, tc.billing); got != tc.want {
			t.Fatalf("costLabel(%v, %q) = %q, want %q", tc.total, tc.billing, got, tc.want)
		}
	}
}
