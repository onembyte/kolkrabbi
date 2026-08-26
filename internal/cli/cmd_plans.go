package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runPlans(_ context.Context, args []string) error {
	if len(args) > 1 {
		return usagef("%s", usageLine("plans"))
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		return err
	}
	filter := strings.TrimSpace(strings.Join(args, " "))
	plans := provider.Plans(filter)
	if len(plans) == 0 {
		fmt.Fprintf(a.stdout, "no plans match %q\n", filter)
		return nil
	}
	fmt.Fprintln(a.stdout, "PROVIDER     PLAN                 CONNECTOR       AUTH          BILLING       SANDBOX  STATUS")
	for _, plan := range plans {
		sandbox := "no"
		if plan.Sandbox {
			sandbox = "yes"
		}
		status := "available"
		for _, connector := range manifest.Connectors {
			if connector.Provider == plan.Provider && connector.Name == plan.Connector && connector.Enabled {
				status = "enabled"
				break
			}
		}
		fmt.Fprintf(a.stdout, "%-12s %-20s %-15s %-13s %-13s %-8s %s\n",
			plan.Provider, plan.Name, plan.Connector, plan.Auth, plan.Billing, sandbox, status)
	}
	return nil
}
