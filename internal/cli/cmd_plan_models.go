package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runPlanModels(_ context.Context, args []string) error {
	// A filter is one search string, and the next line joins these with
	// spaces — so refusing more than one word rejected exactly the
	// multi-word filter this command was written to accept.
	// `pmodels claude max` reported a usage error.
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		return err
	}
	models := a.planModels(strings.TrimSpace(strings.Join(args, " ")))
	if len(models) == 0 {
		fmt.Fprintf(a.stdout, "no plan models match %q\n", strings.Join(args, " "))
		return nil
	}
	fmt.Fprintln(a.stdout, "PROVIDER     PLAN                 CONNECTOR       MODEL                 STATUS       EFFORTS              CTX     AUTH")
	for _, model := range models {
		status := model.Access
		if status != "unsupported subscription" {
			status = "listed"
			for _, connector := range manifest.Connectors {
				if connector.Provider == model.Provider && connector.Name == model.Connector && connector.Enabled {
					status = "enabled"
					break
				}
			}
		}
		// Two different questions, two columns: what the vendor says about
		// the model, and what this machine can do with the connector.
		known := string(model.Status)
		if known == "" {
			known = "-"
		}
		fmt.Fprintf(a.stdout, "%-12s %-20s %-15s %-21s %-12s %-20s %-7s %s\n",
			model.Provider, model.Plan, model.Connector, model.Model,
			known, effortsLabel(model.Efforts), contextWindowLabel(model.Context), status)
	}
	vendorCatalogFooter(a.stdout, a.vendorCatalogs(), time.Now())
	return nil
}

// printPlanModelChoices is the compact, command-oriented view used by bare
// model selection. `pmodels` remains the complete matrix; this view answers a
// different question: what can I type right now, and what sign-in command is
// missing when I cannot use it yet?
func (a *app) printPlanModelChoices() error {
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		return err
	}
	models := a.planModels("")
	if len(models) == 0 {
		return nil
	}

	fmt.Fprintln(a.stdout, "\nsubscription models (use the shortcut or exact model id):")
	for _, model := range models {
		ref := model.Model
		if shortcut := provider.SubscriptionModelShortcutFor(model.Plan, model.Model); shortcut != "" {
			ref = shortcut + " → " + model.Model
		}
		status := model.Access
		if model.Access == "provider CLI" {
			status = fmt.Sprintf("sign in: /plans login %s %q", model.Provider, model.Plan)
			for _, connector := range manifest.Connectors {
				if connector.Provider == model.Provider && connector.Name == model.Connector && connector.Enabled {
					status = "enabled"
					break
				}
			}
		}
		fmt.Fprintf(a.stdout, "  /model %-43s · %-16s · %s%s\n", ref, model.Plan, status, planModelStatusSuffix(model))
	}
	return nil
}
