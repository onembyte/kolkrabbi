package engine

import (
	"fmt"
	"strings"

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
	for i := range candidates {
		candidates[i].Preferred = a.isPreferred(candidates[i])
	}
	return continuity.Recommend(current, need, candidates, a.Order, cooling, modelRank)
}

// isPreferred reports whether a candidate is on the person's own list, by
// its plan-qualified reference or its bare model.
func (a *Agent) isPreferred(c continuity.Candidate) bool {
	for _, name := range a.Preferred {
		if strings.EqualFold(name, c.Ref()) || strings.EqualFold(name, c.Model) {
			return true
		}
	}
	return false
}

// chain is what ContinueOn walks: the recommendation's equivalents, or the
// person's preferred list when they chose it.
func (a *Agent) chain(limit provider.Limit) []continuity.Candidate {
	rec := a.recommendation(limit)
	if strings.EqualFold(a.Select, "preferred") {
		var candidates []continuity.Candidate
		if a.Candidates != nil {
			candidates = a.Candidates()
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
		return continuity.PreferredChain(rec.Current, continuity.Need{Tools: a.Mode != ModeChat}, candidates, a.Preferred, cooling)
	}
	return rec.Equivalent
}

// printRecommendation is the block after the line that says what stopped.
func (a *Agent) printRecommendation(limit provider.Limit) {
	for _, line := range a.recommendation(limit).Lines() {
		fmt.Fprintf(a.Out, "◆ %s\n", line)
	}
}
