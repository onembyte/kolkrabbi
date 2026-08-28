package cli

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func verifiedManifest() provider.ConnectorManifest {
	return provider.ConnectorManifest{Version: 1, Connectors: []provider.Connector{
		{Provider: "anthropic", Plan: "Claude Pro", Name: "claude", Enabled: true, Verified: true},
	}}
}

// A subscription is already paid for. Spending API credit while it sits idle is
// the plainest waste this project can produce, so a session that names no model
// should reach for it first.
func TestAVerifiedSubscriptionIsPreferredOverTheGateway(t *testing.T) {
	choice := chooseSessionModel(chooseDefaultModel(sampleCatalog()), verifiedManifest())
	if !strings.Contains(strings.ToLower(choice.Model), "claude") {
		t.Errorf("session chose %q with a verified Claude subscription available", choice.Model)
	}
	if choice.Warning == "" {
		t.Error("the session did not say it had reached for a subscription")
	}
}

// "Listed" is not "enabled", and enabled is not "verified". v1.2.3 made that
// distinction honest in `kolk plans`; routing must not quietly undo it by
// treating a plan nobody signed into as a usable one.
func TestAnUnverifiedConnectorIsNotUsed(t *testing.T) {
	for _, connector := range []provider.Connector{
		{Provider: "anthropic", Plan: "Claude Pro", Name: "claude", Enabled: true, Verified: false},
		{Provider: "anthropic", Plan: "Claude Pro", Name: "claude", Enabled: false, Verified: true},
		{Provider: "anthropic", Plan: "Claude Pro", Name: "claude"},
	} {
		manifest := provider.ConnectorManifest{Version: 1, Connectors: []provider.Connector{connector}}
		choice := chooseSessionModel(chooseDefaultModel(sampleCatalog()), manifest)
		if strings.Contains(strings.ToLower(choice.Model), "claude") {
			t.Errorf("a connector that is enabled=%v verified=%v was used anyway",
				connector.Enabled, connector.Verified)
		}
	}
}

// With no subscription at all, nothing changes: the free-first discovery that
// already exists is still the right answer.
func TestNoSubscriptionLeavesTheChoiceAlone(t *testing.T) {
	withNone := chooseSessionModel(chooseDefaultModel(sampleCatalog()), provider.ConnectorManifest{Version: 1})
	direct := chooseDefaultModel(sampleCatalog())
	if withNone.Model != direct.Model {
		t.Errorf("with no connector the session chose %q, want the usual default %q",
			withNone.Model, direct.Model)
	}
}

func sampleCatalog() []provider.ModelInfo {
	m := provider.ModelInfo{ID: "vendor/free:free", Name: "free", ContextLength: 131072,
		SupportedParameters: []string{"tools"}, Description: "coding model"}
	m.Pricing.Prompt, m.Pricing.Completion = "0", "0"
	return []provider.ModelInfo{m}
}
