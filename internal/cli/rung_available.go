package cli

import (
	"github.com/onembyte/kolkrabbi/internal/engine"
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
	store := a.vendorCatalogs()
	return func(vendor, model string) bool {
		if !a.connectorSignedIn(vendor) {
			return false
		}
		// Availability as the vendor states it (F4.5): a rung the vendor's
		// catalog no longer lists is not offered, however long kolk's ladder
		// has named it; a vendor kolk cannot spawn for offers nothing.
		return a.vendorKnowsModel(store, vendor, model)
	}
}
