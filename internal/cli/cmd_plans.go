package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/local"
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
		// A keyed vendor row says whether kolk holds the key (env or store);
		// every other API-key row is a catalog entry kolk cannot yet use.
		if plan.Auth == "API key" && isKeyedVendor(plan.Provider) {
			status = "no key"
			if key, err := resolveVendorCredential(ctx, plan.Provider, dirs.CredentialsFile()); err == nil && !key.IsZero() {
				status = "key set"
			}
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
		fmt.Fprintln(a.stdout, "\ninstalled: sign in with  /plans login <provider> <plan>")
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
		return usagef("/plans login <provider> <plan>")
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
	// provider CLI spawned here would fight it for every keystroke — unless it
	// is given a terminal of its own. That is what the in-session runner does:
	// the frame is parked, the child draws on a pty kolk owns both ends of, and
	// the session comes back when it exits. Nothing leaves the window the user
	// is already looking at.
	if a.loginInSession != nil {
		return a.runConnectorLoginWith(ctx, dirs.ConnectorsFile(), selected, a.loginInSession)
	}
	// Failing that, the login moves to a terminal Kolkrabbi does not own — a
	// window of its own when one can be opened, and the whole screen handed
	// over when not.
	if a.terminalOwned != nil && a.terminalOwned() {
		if a.handoverWindow != nil {
			fmt.Fprintf(a.stdout, "Signing in to %s — a separate terminal window is opening for %s.\n",
				selected.Name, selected.Connector)
			fmt.Fprintln(a.stdout, "Finish there (your browser will take it from here) and this session continues as it was.")
			return a.runConnectorLoginWith(ctx, dirs.ConnectorsFile(), selected, a.handoverWindow)
		}
		// The session steps aside rather than sending the user to another
		// terminal: the screen comes down, the provider's CLI gets the keyboard
		// on a terminal nothing else is reading, and kolk comes back on the same
		// session with the connector recorded. Three manual steps became none.
		a.pendingLogin = &selected
		fmt.Fprintf(a.stdout, "Signing in to %s — kolk will hand the keyboard to %s and come back to this session.\n",
			selected.Name, selected.Connector)
		return nil
	}
	return a.runConnectorLoginWith(ctx, dirs.ConnectorsFile(), selected, a.terminalHandover())
}

// terminalHandover is the runner for wherever this command currently owns the
// terminal. The string result is the directory the login runs in, unused by
// the plain handover but part of the same shape the window runner reports.
func (a *app) terminalHandover() func(context.Context, string, []string) error {
	return func(ctx context.Context, executable string, args []string) error {
		return a.handover(ctx, executable, args, "")
	}
}

// runConnectorLoginWith hands the vendor's login to the given runner and
// records the connector afterwards. The runner owns the question of where the
// user sees the login — a dedicated window, or a terminal kolk has cleared.
func (a *app) runConnectorLoginWith(ctx context.Context, connectorsFile string, selected provider.Plan, run func(context.Context, string, []string) error) error {
	// The login subcommand, never the bare executable: running `claude` with no
	// arguments opens Claude Code itself, which is how a person asking to sign
	// in ended up inside another agent's full interface instead.
	loginArgs, known := provider.LoginArgs(selected.Connector)
	if !known {
		fmt.Fprintf(a.stdout, "kolk does not know how %s signs in; running it as-is.\n", selected.Connector)
	}
	if hint := provider.LoginHint(selected.Connector); hint != "" {
		fmt.Fprintf(a.stdout, "%s: %s\n", selected.Connector, hint)
	}
	// `ollama signin` talks to a running server — the key that gets signed in
	// is the server's — so without one the login cannot start, and a
	// connector recorded against nothing would be a claim.
	var host local.Host
	if selected.Connector == local.SidecarName {
		host = a.discoverHost(ctx)
		if host.State != local.HostRunning {
			fmt.Fprintln(a.stdout, "ollama signin needs a running Ollama server and none is listening on 127.0.0.1:11434.")
			fmt.Fprintln(a.stdout, "start one with `ollama serve` (or open the Ollama app), then run this again.")
			return nil
		}
	}
	fmt.Fprintf(a.stdout, "starting %s login; Kolkrabbi will not see credentials\n", selected.Connector)
	if err := run(ctx, selected.Connector, loginArgs); err != nil {
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
	if selected.Connector == local.SidecarName {
		if err := a.verifyOllamaConnector(ctx, connectorsFile, selected, host.Addr); err != nil {
			return err
		}
		a.reportVendorDiscovery(ctx, selected.Connector)
		return nil
	}
	fmt.Fprintf(a.stdout, "%s recorded. %s exited cleanly, which is not proof of a login;\n",
		selected.Name, selected.Connector)
	fmt.Fprintf(a.stdout, "Kolkrabbi confirms it the first time %s answers a turn.\n", selected.Connector)
	// Every login maps what the vendor offers, in front of the user, before
	// the prompt ever names a model.
	a.reportVendorDiscovery(ctx, selected.Connector)
	return nil
}

// verifyOllamaConnector asks the server whether the sign-in happened, rather
// than waiting for a turn to prove it. `ollama signin` returns as soon as the
// browser opens, so this waits for the browser half to finish — bounded, and
// with the URL printed in case the browser never came.
//
// A turn is the wrong verifier here: a local model answering proves nothing
// about ollama.com, and a verifier that could not tell the two apart would
// make Ollama Cloud the session default for someone who never signed in.
func (a *app) verifyOllamaConnector(ctx context.Context, connectorsFile string, selected provider.Plan, addr string) error {
	fmt.Fprintln(a.stdout, "waiting for the sign-in to finish in your browser (Ctrl-C stops waiting; it verifies at the next start instead)")
	deadline := time.Now().Add(a.signInBudget)
	var last local.SignInState
	for {
		last = a.signIn(ctx, addr)
		if last.SignedIn {
			if err := provider.SaveConnector(ctx, connectorsFile, provider.Connector{
				Provider: selected.Provider, Plan: selected.Name, Name: selected.Connector,
				Sandbox: selected.Sandbox, LoginOwner: "provider-cli", Enabled: true, Verified: true,
			}); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "✓ %s verified: the server is signed in on the %q plan.\n", selected.Name, last.Plan)
			return nil
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(a.signInPoll())
	}
	fmt.Fprintf(a.stdout, "%s recorded but not yet signed in.\n", selected.Name)
	if last.SignInURL != "" {
		fmt.Fprintf(a.stdout, "  sign in at %s\n", last.SignInURL)
	}
	fmt.Fprintln(a.stdout, "  kolk checks again at every start and marks it verified once the server says so.")
	return nil
}

// signInPoll paces the wait: a browser sign-in takes seconds, and a test's
// budget is milliseconds.
func (a *app) signInPoll() time.Duration {
	poll := a.signInBudget / 4
	if poll > 2*time.Second {
		return 2 * time.Second
	}
	if poll < 10*time.Millisecond {
		return 10 * time.Millisecond
	}
	return poll
}

// isKeyedVendor reports whether a provider is one whose documented API origin
// takes a key, per its disposition.
func isKeyedVendor(providerName string) bool {
	for _, vendor := range provider.KeyedVendors() {
		if vendor == providerName {
			return true
		}
	}
	return false
}
