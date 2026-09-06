package cli

import (
	"context"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/stats"
)

// continuityCandidates is what could continue the work when the session's
// model stops (plan 35 §2.3, V35.3b): every model row of every connector
// that has answered a turn, billed as the plan's turn, with the model a
// routing word resolved to and the person's own rating; and, when the
// gateway has a key, the gateway models that sit on a vendor ladder, so a
// paid peer can be named without listing hundreds of strangers. Keyed vendor
// origins join once their live listers exist. Nothing here spends a token.
func (a *app) continuityCandidates(ctx context.Context) []continuity.Candidate {
	dirs, err := a.resolve()
	if err != nil {
		return nil
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		return nil
	}
	ratings, _ := stats.RatingsByModel(dirs.Data)
	store := a.vendorCatalogs()
	var out []continuity.Candidate
	for _, connector := range manifest.Connectors {
		if !connector.Enabled || !connector.Verified {
			continue
		}
		for _, row := range provider.PlanModelsFrom(store, "") {
			if row.Connector != connector.Name || !strings.EqualFold(row.Plan, connector.Plan) || row.Access != "provider CLI" || row.Status == provider.StatusGone {
				continue
			}
			c := continuity.Candidate{Model: row.Model, Connector: row.Connector, Plan: row.Plan, Billing: provider.BillingSubscription, Context: row.Context}
			if catalog, ok := store.Vendors[row.Connector]; ok {
				if discovered, found := catalog.Find(row.Model); found && len(discovered.ExactIDs) > 0 {
					c.Exact = discovered.ExactIDs[0]
				}
			}
			if r, ok := ratings[row.Model]; ok {
				c.Rating, c.Ratings = r.Average, r.Count
			}
			out = append(out, c)
		}
	}
	if cred, err := resolveOpenRouterCredential(ctx, dirs.CredentialsFile()); err == nil && !cred.IsZero() {
		for _, model := range a.catalog {
			if _, _, ranked := engine.RankModel(model.ID); !ranked {
				continue
			}
			c := continuity.Candidate{Model: model.ID, Connector: "openrouter", Billing: provider.BillingGateway,
				Free: provider.ModelIsFree(model), Context: model.ContextLength, LacksTools: len(model.SupportedParameters) > 0 && !supportsTools(model)}
			if r, ok := ratings[model.ID]; ok {
				c.Rating, c.Ratings = r.Average, r.Count
			}
			out = append(out, c)
		}
	}
	return out
}

func supportsTools(model provider.ModelInfo) bool {
	for _, p := range model.SupportedParameters {
		if p == "tools" || p == "tool_choice" {
			return true
		}
	}
	return false
}
