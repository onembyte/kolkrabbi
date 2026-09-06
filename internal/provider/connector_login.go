package provider

// A connector's login is a subcommand, not the CLI itself.
//
// Running the bare executable starts the provider's whole interactive
// application: `claude` with no arguments opens Claude Code, which is what a
// person saw after `/plogin anthropic claude max` — another agent's full
// interface, in place of the one they were using, with no indication of how to
// get back. The login they asked for was somewhere inside it.
//
// Each connector names the subcommand that signs in and exits. Anything not
// listed here falls back to the bare executable, which is the old behaviour and
// is at least never wrong about which program to run.
var connectorLoginArgs = map[string][]string{
	// `claude auth login` signs in to the Anthropic account and returns.
	// `claude setup-token` is the long-lived-token path and deliberately not
	// used: it issues a credential kolk would then be responsible for, and the
	// whole point of a provider-CLI connector is that the provider keeps it.
	"claude": {"auth", "login"},
	"codex":  {"login"},
	// `ollama signin` opens a browser, waits for the sign-in and exits. Unlike
	// `claude`, the bare `ollama` binary prints usage rather than opening an
	// application, so the fallback would not have been harmful here — the
	// subcommand is named anyway because it is the one that actually signs in.
	"ollama": {"signin"},
	// gemini is deliberately absent. Its login subcommand was not verified
	// against an installed CLI, and a guessed subcommand is worse than the
	// fallback: the fallback runs the program the user named, while a wrong
	// guess runs something that does not exist and reports a failure that
	// sounds like their subscription is broken.
}

// LoginArgs are the arguments that make a connector sign in and exit. The
// second result is false when nothing is known for it, so a caller can say so
// rather than silently launching an application.
func LoginArgs(connector string) ([]string, bool) {
	args, ok := connectorLoginArgs[connector]
	if !ok {
		return nil, false
	}
	// Copied: a caller that appends to this must not edit the table.
	out := make([]string, len(args))
	copy(out, args)
	return out, true
}

// connectorLoginHints are what kolk tells a person before running a connector
// that has no login subcommand: how to sign in inside it and how to leave.
// A connector with a subcommand needs none.
var connectorLoginHints = map[string]string{
	// Copilot CLI (docs read 2026-09-05): sign in with `/login` inside the
	// CLI, or export a fine-grained token with the "Copilot Requests"
	// permission as COPILOT_GITHUB_TOKEN (GH_TOKEN and GITHUB_TOKEN also
	// work, in that order of precedence).
	"copilot": "inside copilot, type /login and follow the browser, then /exit to come back here; " +
		"or, before starting kolk, export a fine-grained token with the \"Copilot Requests\" permission as COPILOT_GITHUB_TOKEN",
}

// LoginHint is the sentence for a connector that signs in inside its own
// interface; empty for one that has a login subcommand or is unknown.
func LoginHint(connector string) string {
	return connectorLoginHints[connector]
}
