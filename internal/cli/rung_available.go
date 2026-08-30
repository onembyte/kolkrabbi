package cli

import (
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

// rungAvailable answers, for the engine, whether a cheaper model can actually
// be run on this machine.
//
// Two questions, both of which have to be yes:
//
//  1. Is the vendor signed in *through kolk*? Crossing to a subscription the
//     user holds costs nothing at the margin; the same hop with no connector
//     lands on a metered API and bills them. That is the ceiling violated
//     sideways instead of upward, and it is the one the user named directly:
//     "only if codex was loged in also in kolk".
//
//  2. Does the adapter know how to spawn that model? A ladder rung is a
//     ranking string, not a promise that a vendor CLI accepts it.
//
// Enabled rather than Verified, deliberately. Verified means kolk has seen the
// connector answer a real turn, which is a stronger claim — but requiring it
// would keep a freshly signed-in vendor off the menu until it happened to be
// used, so signing in would appear to do nothing. Enabled makes the login take
// effect immediately, and a rung that then fails to open is handled where that
// failure belongs.
func (a *app) rungAvailable() engine.RungAvailable {
	return func(vendor, model string) bool {
		if !a.connectorSignedIn(vendor) {
			return false
		}
		switch strings.ToLower(vendor) {
		case "claude":
			return agentcli.ClaudeKnowsModel(model)
		case "codex":
			return agentcli.CodexKnowsModel(model)
		default:
			// A vendor kolk can sign into but cannot yet spawn a chosen model
			// for offers nothing. Saying no here is what keeps the roster from
			// promising a rung that would fail at the first task.
			return false
		}
	}
}
