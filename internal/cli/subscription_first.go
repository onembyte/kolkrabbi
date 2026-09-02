package cli

import (
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// chooseSessionModel picks the session's model, reaching for a subscription
// before the gateway.
//
// A subscription is already paid for. Spending API credit while it sits idle is
// the plainest waste this project can produce, and until now nothing in model
// selection knew connectors existed: a machine with a signed-in Claude plan
// still started on a free gateway model and billed metered ones when it needed
// something stronger.
//
// **Enabled and verified, never merely listed.** `kolk plans` shows every plan
// in the matrix, and v1.2.3 made the difference honest: `listed` means it is in
// the matrix with nothing configured here. Routing must not undo that by
// treating a plan nobody signed into as a usable one — a connector that has
// never answered a turn is a promise, not a capability.
// It takes the choice already made rather than re-deriving it: startup injects
// its own chooser, and calling the real one again from here would ignore that
// seam — which is exactly how this first broke two existing tests.
func chooseSessionModel(catalog []provider.PlanModel, fallback defaultModelChoice, manifest provider.ConnectorManifest) defaultModelChoice {
	if plan, ok := verifiedPlanModel(catalog, manifest); ok {
		return defaultModelChoice{
			Model: plan.Model,
			Free:  true, // to the user: already paid for, so this turn costs nothing new
			Warning: fmt.Sprintf("using your %s subscription (%s); `/model` picks a gateway model instead",
				plan.Plan, plan.Model),
		}
	}
	return fallback
}

// verifiedPlanModel returns the first model served by a connector that is both
// enabled and verified.
//
// The order is the plan catalogue's, so the answer is stable: two runs on one
// machine choose the same plan, and a session nobody can predict is a session
// nobody trusts.
func verifiedPlanModel(catalog []provider.PlanModel, manifest provider.ConnectorManifest) (provider.PlanModel, bool) {
	usable := map[string]bool{}
	for _, connector := range manifest.Connectors {
		if connector.Enabled && connector.Verified {
			usable[strings.ToLower(connector.Name)] = true
		}
	}
	if len(usable) == 0 {
		return provider.PlanModel{}, false
	}
	for _, plan := range catalog {
		if usable[strings.ToLower(plan.Connector)] {
			return plan, true
		}
	}
	return provider.PlanModel{}, false
}
