package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runPlans(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "login" {
		return a.runPlanLogin(ctx, args[1:])
	}
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
	unverified := false
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
				if !connector.Verified {
					status = "unverified"
					unverified = true
				}
				break
			}
		}
		fmt.Fprintf(a.stdout, "%-12s %-20s %-15s %-13s %-13s %-8s %s\n",
			plan.Provider, plan.Name, plan.Connector, plan.Auth, plan.Billing, sandbox, status)
	}
	if unverified {
		fmt.Fprintln(a.stdout, "\nunverified: the provider CLI exited cleanly, which is not proof of a login.")
		fmt.Fprintln(a.stdout, "Kolkrabbi confirms it the first time the connector answers a turn.")
	}
	return nil
}

func (a *app) runPlanLogin(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return usagef("kolk plans login <provider> <plan>")
	}
	providerName := strings.ToLower(strings.TrimSpace(args[0]))
	planFilter := strings.TrimSpace(strings.Join(args[1:], " "))
	matches := provider.Plans(providerName)
	selected := provider.Plan{}
	for _, plan := range matches {
		if strings.EqualFold(plan.Name, planFilter) {
			selected = plan
			break
		}
	}
	if selected.Name == "" {
		return fmt.Errorf("no exact provider CLI plan %q for %s", planFilter, providerName)
	}
	if selected.Auth != "provider CLI" {
		return fmt.Errorf("%s uses %s, not a provider CLI login", selected.Name, selected.Auth)
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	// A live Kolkrabbi session reads the keyboard from its own goroutine, so a
	// provider CLI spawned here would fight it for every keystroke. The login
	// therefore moves to a terminal Kolkrabbi does not own.
	if a.terminalOwned != nil && a.terminalOwned() {
		fmt.Fprintf(a.stdout, "%s signs you in from a separate terminal, so its CLI owns the keyboard.\n\n", selected.Name)
		fmt.Fprintf(a.stdout, "  1. open another terminal\n")
		fmt.Fprintf(a.stdout, "  2. run: kolk plans login %s %q\n", selected.Provider, selected.Name)
		fmt.Fprintf(a.stdout, "  3. come back here and run /plans to confirm the connector\n\n")
		fmt.Fprintf(a.stdout, "Kolkrabbi never sees the credentials either way.\n")
		return nil
	}
	fmt.Fprintf(a.stdout, "starting %s login in this terminal; Kolkrabbi will not see credentials\n", selected.Connector)
	if err := a.handover(ctx, selected.Connector, nil, ""); err != nil {
		return err
	}
	// A provider CLI that quits without signing in also exits 0, so a clean exit
	// records the connector but proves nothing. Verification happens the first
	// time it answers.
	if err := provider.SaveConnector(ctx, dirs.ConnectorsFile(), provider.Connector{
		Provider: selected.Provider, Plan: selected.Name, Name: selected.Connector,
		Sandbox: selected.Sandbox, LoginOwner: "provider-cli", Enabled: true, Verified: false,
	}); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s recorded. %s exited cleanly, which is not proof of a login;\n",
		selected.Name, selected.Connector)
	fmt.Fprintf(a.stdout, "Kolkrabbi confirms it the first time %s answers a turn.\n", selected.Connector)
	return nil
}
