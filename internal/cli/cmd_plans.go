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
	filter := strings.TrimSpace(strings.Join(args, " "))
	plans := provider.Plans(filter)
	if len(plans) == 0 {
		fmt.Fprintf(a.stdout, "no plans match %q\n", filter)
		return nil
	}
	fmt.Fprintln(a.stdout, "PROVIDER     PLAN                 CONNECTOR       AUTH          BILLING       SANDBOX")
	for _, plan := range plans {
		sandbox := "no"
		if plan.Sandbox {
			sandbox = "yes"
		}
		fmt.Fprintf(a.stdout, "%-12s %-20s %-15s %-13s %-13s %s\n",
			plan.Provider, plan.Name, plan.Connector, plan.Auth, plan.Billing, sandbox)
	}
	return nil
}
