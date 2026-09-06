package engine

import (
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// recommendation ranks the surface's candidates against the model that just
// stopped: eligibility by cooldown and task need, equivalence by the vendor
// ladder, order by the owner's rule (plan 35 §2.3, V35.3b).
func (a *Agent) recommendation(limit provider.Limit) continuity.Recommendation {
	var candidates []continuity.Candidate
	if a.Candidates != nil {
		candidates = a.Candidates()
	}
	current := continuity.Candidate{Model: limit.Model, Connector: limit.Connector, Billing: a.SessionBilling()}
	if current.Model == "" {
		current.Model = a.SessionModel()
	}
	cooling := func(connector, model string) bool {
		if a.Cooldowns == nil {
			return false
		}
		if _, ok := a.Cooldowns.Cooling(provider.ScopeAccount, connector, ""); ok {
			return true
		}
		_, ok := a.Cooldowns.Cooling(provider.ScopeModel, connector, model)
		return ok
	}
	need := continuity.Need{Tools: a.Mode != ModeChat}
	return continuity.Recommend(current, need, candidates, nil, cooling, modelRank)
}

// printRecommendation is the block after the line that says what stopped.
func (a *Agent) printRecommendation(limit provider.Limit) {
	for _, line := range a.recommendation(limit).Lines() {
		fmt.Fprintf(a.Out, "◆ %s\n", line)
	}
}
