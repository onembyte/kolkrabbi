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
	models := provider.PlanModels(strings.TrimSpace(strings.Join(args, " ")))
	if len(models) == 0 {
		fmt.Fprintf(a.stdout, "no plan models match %q\n", strings.Join(args, " "))
		return nil
	}
	fmt.Fprintln(a.stdout, "PROVIDER     PLAN                 CONNECTOR       MODEL                 EFFORTS")
	for _, model := range models {
		fmt.Fprintf(a.stdout, "%-12s %-20s %-15s %-21s %s\n",
			model.Provider, model.Plan, model.Connector, model.Model, strings.Join(model.Efforts, ","))
	}
	return nil
}
