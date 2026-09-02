package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// B12.15c. Naming a plan model whose connector nobody signed into must produce
// the command that fixes it. A refusal that only says "no" costs the user a
// search through `kolk plans` for a string kolk already knows.
func TestANamedPlanModelWithoutItsConnectorSaysHowToSignIn(t *testing.T) {
	_, err := provider.ResolvePlanModelFrom(provider.VendorCatalogs{}, "claude-sonnet", provider.ConnectorManifest{Version: 1})
	if err == nil {
		t.Fatal("resolving a plan model with no connector succeeded, want a refusal")
	}
	for _, want := range []string{"claude", "kolk plans login", "anthropic"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not contain %q, so it cannot be acted on", err, want)
		}
	}
}

// B12.15c. A connector can be enabled and still have no adapter — the plan
// catalogue lists `codex` and `gemini`, and nothing in this build can drive
// either. `planBackendFor` has a branch for that, and this test is how I found
// out it cannot be reached through any supported path: `SaveConnector` refuses
// to persist a connector whose login is not provider-owned, so the state never
// reaches disk in the first place.
//
// The guard is therefore tested where it actually holds. The branch downstream
// stays as defence in depth, and is recorded as unreachable rather than left to
// look like a live failure path nobody covered.
func TestAConnectorWithAnUnsupportedLoginIsNeverPersisted(t *testing.T) {
	isolateHome(t)
	a, _, _ := newTestApp(t, "")
	d, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}

	err = provider.SaveConnector(context.Background(), d.ConnectorsFile(), provider.Connector{
		Provider: "openai", Plan: "ChatGPT Plus", Name: "codex", LoginOwner: "codex", Enabled: true,
	})
	if err == nil {
		t.Fatal("a connector kolk cannot drive was persisted, want a refusal")
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Errorf("refusal %q does not name what was rejected", err)
	}

	// And nothing was half-written: a rejected connector must not leave a file
	// that a later run reads as real.
	manifest, loadErr := provider.LoadConnectors(d.ConnectorsFile())
	if loadErr != nil {
		t.Fatalf("loading after a rejected save: %v", loadErr)
	}
	if len(manifest.Connectors) != 0 {
		t.Fatalf("the rejected connector was written anyway: %+v", manifest.Connectors)
	}
}
