package cli

import (
	"sort"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// subscriptionModelChoices is presentation, not entitlement. The conservative
// plan matrix does not assign a newly discovered model to every subscription
// tier. A picker can still offer it by name through the actual enabled login,
// or show a vendor sign-in instruction without inventing a plan. Use one store
// snapshot for derivation, visibility and resolution; never probe inference.
func (a *app) subscriptionModelChoices(manifest provider.ConnectorManifest) []provider.PlanModel {
	store := a.vendorCatalogs()
	derived := provider.DerivePlanModels(store)
	out := make([]provider.PlanModel, 0, len(derived))
	seen := map[string]bool{}
	for _, row := range derived {
		if row.Status == provider.StatusGone {
			continue
		}
		if catalog, known := store.Vendors[row.Connector]; known && row.Access == "provider CLI" {
			discovered, found := catalog.Find(row.Model)
			if !found || discovered.Hidden || discovered.Status == provider.StatusGone {
				continue
			}
		}
		out = append(out, row)
		seen[row.Connector+"\x00"+strings.ToLower(row.Model)] = true
	}
	// A sorted vendor list keeps the picker stable across map iterations.
	var names []string
	for name := range store.Vendors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		providerName := ""
		for _, row := range derived {
			if row.Connector == name && row.Access == "provider CLI" {
				providerName = row.Provider
				break
			}
		}
		if providerName == "" {
			continue // unsupported or API-only connectors are not subscriptions
		}
		for _, discovered := range store.Vendors[name].Visible() {
			key := name + "\x00" + strings.ToLower(discovered.ID)
			if strings.TrimSpace(discovered.ID) == "" || seen[key] {
				continue
			}
			seen[key] = true
			row := provider.PlanModel{
				Provider: providerName, Connector: name, Model: discovered.ID,
				Efforts: append([]string(nil), discovered.Efforts...), Context: discovered.Context,
				Status: discovered.Status, Access: "provider CLI",
			}
			if selected, err := provider.ResolvePlanModelFrom(store, discovered.ID, manifest); err == nil && selected.Connector == name {
				row = selected
			}
			out = append(out, row)
		}
	}
	return out
}

func subscriptionLoginCommand(plan provider.PlanModel) string {
	command := "/plans login " + plan.Provider
	if plan.Plan != "" {
		command += " " + strconv.Quote(plan.Plan)
	}
	return command
}
