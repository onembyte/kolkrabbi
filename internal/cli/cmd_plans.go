package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

func (a *app) runPlans(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "login" {
		return a.runPlanLogin(ctx, args[1:])
	}
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
	filter := strings.TrimSpace(strings.Join(args, " "))
	plans := provider.Plans(filter)
	if len(plans) == 0 {
		fmt.Fprintf(a.stdout, "no plans match %q\n", filter)
		return nil
	}
	unverified, installed := false, false
	fmt.Fprintln(a.stdout, "PROVIDER     PLAN                 CONNECTOR       AUTH          BILLING       SANDBOX  STATUS")
	for _, plan := range plans {
		sandbox := "no"
		if plan.Sandbox {
			sandbox = "yes"
		}
		// "listed" and not "available": this row is in the provider matrix and
		// nothing is configured for it. Calling that available told a user on a
		// fresh machine that fifteen providers were ready to use, while the
		// website said "no adapter yet" about the same fifteen.
		status := "listed"
		// A provider CLI that is on PATH is one command away from working, and
		// saying so is the difference between a matrix and an answer. The table
		// said "listed" for all fifteen rows, which told a user with claude and
		// codex already installed exactly as much as it told a user with
		// neither.
		if plan.Auth == "provider CLI" && a.connectorInstalled(plan.Connector) {
			status = "installed"
			installed = true
		}
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
	// One line, not a legend. The table says what it says; a page of prose
	// under it is read once and skipped forever after.
	if installed {
		fmt.Fprintln(a.stdout, "\ninstalled: sign in with  kolk plans login <provider> <plan>")
	}
	if unverified {
		fmt.Fprintln(a.stdout, "\nunverified: the provider CLI exited cleanly, which is not proof of a login.")
		fmt.Fprintln(a.stdout, "Kolkrabbi confirms it the first time the connector answers a turn.")
	}
	return nil
}

// connectorInstalled reports whether a provider CLI is on PATH. Presence is
// not a login — the connector says so itself when it first answers a turn —
// but absence is decisive, and that is what makes the row actionable.
func (a *app) connectorInstalled(connector string) bool {
	// An "-api" connector is a key, not a binary: there is nothing on PATH to
	// find, and reporting it as missing would be wrong.
	if connector == "" || strings.HasSuffix(connector, "-api") {
		return false
	}
	_, err := shell.LookPath(connector)
	return err == nil
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
		// The session steps aside rather than sending the user to another
		// terminal: the screen comes down, the provider's CLI gets the keyboard
		// on a terminal nothing else is reading, and kolk comes back on the same
		// session with the connector recorded. Three manual steps became none.
		a.pendingLogin = &selected
		fmt.Fprintf(a.stdout, "Signing in to %s — kolk will hand the keyboard to %s and come back to this session.\n",
			selected.Name, selected.Connector)
		return nil
	}
	return a.runConnectorLogin(ctx, dirs.ConnectorsFile(), selected)
}

// runConnectorLogin hands the terminal to the provider's own CLI and records
// the connector afterwards. The caller must already own an unshared terminal:
// in a session that means after the screen is down.
func (a *app) runConnectorLogin(ctx context.Context, connectorsFile string, selected provider.Plan) error {
	fmt.Fprintf(a.stdout, "starting %s login in this terminal; Kolkrabbi will not see credentials\n", selected.Connector)
	if err := a.handover(ctx, selected.Connector, nil, ""); err != nil {
		return err
	}
	// A provider CLI that quits without signing in also exits 0, so a clean exit
	// records the connector but proves nothing. Verification happens the first
	// time it answers.
	if err := provider.SaveConnector(ctx, connectorsFile, provider.Connector{
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
