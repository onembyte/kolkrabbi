package cli

import (
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// agentRoster takes one fresh snapshot per plan. Discovery and logins can
// change during a session; a closure over startup state would miss them.
// The session owner calls this after newAgent resolves the app directories.
// It is not a concurrent cold-start entry point: runTasks resolves unbound
// tasks before launching children, so child goroutines never enter it.
func (a *app) agentRoster(ceiling, connector string) engine.Roster {
	roster := engine.Roster{Discovered: true, Rungs: []engine.Rung{{Model: ceiling}}}
	store := a.vendorCatalogs()
	// These adapters can open an independent conversation per child. Never
	// infer a subscription route from a gateway prefix or an ExactIDs alias.
	for _, vendor := range []string{"claude", "codex"} {
		if connector != "" && connector != vendor {
			continue
		}
		catalog, found := store.Vendors[vendor]
		if !found {
			continue
		}
		selected, found := catalog.Find(ceiling)
		if !found {
			continue
		}
		roster.Rungs[0].Vendor = vendor
		roster.Rungs[0].Efforts = append([]string(nil), selected.Efforts...)
		if selected.Rank <= 0 || selected.Status == provider.StatusGone {
			return roster
		}
		signedIn := a.connectorSignedIn(vendor)
		seen := map[string]bool{strings.ToLower(strings.TrimSpace(ceiling)): true}
		for _, row := range catalog.Visible() {
			id := strings.TrimSpace(row.ID)
			key := strings.ToLower(id)
			if id == "" || id != row.ID || row.Rank <= 0 || seen[key] {
				continue
			}
			seen[key] = true
			if row.Rank < selected.Rank {
				roster.Blocked = append(roster.Blocked, id)
				continue
			}
			// Equal ranks do not establish a lower-cost or lower-capability
			// choice. Keep the selected model for that capability level.
			if row.Rank == selected.Rank {
				continue
			}
			if !signedIn {
				roster.LoginModel = id
				continue
			}
			roster.Rungs = append(roster.Rungs, engine.Rung{Model: id, Vendor: vendor,
				Depth: len(roster.Rungs), Efforts: append([]string(nil), row.Efforts...)})
		}
		return roster
	}
	// If discovery has no row yet, retain only the selected model. The seed
	// can still describe its effort vocabulary; it cannot invent live ranks.
	for _, plan := range provider.PlanModelsFrom(store, "") {
		if (connector == "" || connector == plan.Connector) && strings.EqualFold(plan.Model, ceiling) && plan.Status != provider.StatusGone {
			roster.Rungs[0].Vendor = plan.Connector
			roster.Rungs[0].Efforts = append([]string(nil), plan.Efforts...)
			break
		}
	}
	return roster
}
