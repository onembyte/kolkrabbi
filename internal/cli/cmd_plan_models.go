package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runPlanModels(_ context.Context, args []string) error {
	if len(args) > 1 {
		return usagef("%s", usageLine("pmodels"))
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		return err
	}
	models := provider.PlanModels(strings.TrimSpace(strings.Join(args, " ")))
	if len(models) == 0 {
		fmt.Fprintf(a.stdout, "no plan models match %q\n", strings.Join(args, " "))
		return nil
	}
	fmt.Fprintln(a.stdout, "PROVIDER     PLAN                 CONNECTOR       MODEL                 EFFORTS              AUTH")
	for _, model := range models {
		status := model.Access
		if status != "unsupported subscription" {
			status = "available"
			for _, connector := range manifest.Connectors {
				if connector.Provider == model.Provider && connector.Name == model.Connector && connector.Enabled {
					status = "enabled"
					break
				}
			}
		}
		fmt.Fprintf(a.stdout, "%-12s %-20s %-15s %-21s %-20s %s\n",
			model.Provider, model.Plan, model.Connector, model.Model, strings.Join(model.Efforts, ","), status)
	}
	return nil
}
