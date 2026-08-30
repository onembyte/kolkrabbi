package cli

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// A model is on the menu only when its vendor is signed in THROUGH kolk.
// Crossing to a subscription the user holds costs nothing at the margin; the
// same hop with no connector lands on a metered API and bills them, which is
// the ceiling violated sideways instead of upward.
func TestACheaperRungNeedsItsVendorSignedInThroughKolk(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs

	// Nothing signed in: nothing is offered.
	if a.rungAvailable()("claude", "claude-haiku") {
		t.Error("a rung was offered with no connector signed in")
	}

	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "anthropic", Plan: "Claude Max", Name: "claude",
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if !a.rungAvailable()("claude", "claude-haiku") {
		t.Error("a signed-in vendor's own rung was refused")
	}
	// Signing in to claude says nothing about codex.
	if a.rungAvailable()("codex", "gpt-5.6-sol") {
		t.Error("signing in to claude made a codex rung available")
	}
}

// A ladder rung is a ranking string, not a promise that a vendor CLI accepts
// it. A roster built on the pass-through translation would offer models the
// adapter cannot spawn, and the failure would land on the first task.
func TestARungTheAdapterCannotSpawnIsNotOffered(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "anthropic", Plan: "Claude Max", Name: "claude",
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	available := a.rungAvailable()
	if !available("claude", "claude-haiku") {
		t.Error("a real rung was refused")
	}
	if available("claude", "claude-invented") {
		t.Error("a model the adapter has never heard of was offered")
	}
}

// A connector recorded but not enabled is not a login.
func TestADisabledConnectorOffersNothing(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "anthropic", Plan: "Claude Max", Name: "claude",
		LoginOwner: "provider-cli", Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	if a.rungAvailable()("claude", "claude-haiku") {
		t.Error("a disabled connector still offered a rung")
	}
}
