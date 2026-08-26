package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanModelsFilterAndEfforts(t *testing.T) {
	got := PlanModels("gemini")
	if len(got) == 0 {
		t.Fatal("gemini plan models should be present")
	}
	for _, model := range got {
		if model.Provider != "google" {
			t.Errorf("filter returned unrelated provider: %+v", model)
		}
		if len(model.Efforts) == 0 {
			t.Errorf("model has no effort metadata: %+v", model)
		}
	}
}

func TestPlanModelsMetadataIsComplete(t *testing.T) {
	for _, model := range PlanModels("") {
		if model.Provider == "" || model.Plan == "" || model.Connector == "" ||
			model.Model == "" || model.Access == "" || len(model.Efforts) == 0 {
			t.Errorf("incomplete plan model metadata: %+v", model)
		}
	}
}

func enabledClaude() ConnectorManifest {
	return ConnectorManifest{Version: connectorManifestVersion, Connectors: []Connector{
		{Provider: "anthropic", Plan: "Claude Max", Name: "claude", LoginOwner: "provider-cli", Enabled: true},
	}}
}

func TestResolvePlanModelReturnsAnEnabledPlanModel(t *testing.T) {
	got, err := ResolvePlanModel("claude-opus", enabledClaude())
	if err != nil {
		t.Fatal(err)
	}
	if got.Connector != "claude" || got.Plan != "Claude Max" {
		t.Fatalf("resolved = %+v", got)
	}
}

func TestResolvePlanModelIsCaseInsensitive(t *testing.T) {
	if _, err := ResolvePlanModel("  Claude-Opus  ", enabledClaude()); err != nil {
		t.Fatalf("a user typing the model with different case got %v", err)
	}
}

func TestResolvePlanModelRejectsAnUnknownReference(t *testing.T) {
	_, err := ResolvePlanModel("no-such-model", enabledClaude())
	if err == nil {
		t.Fatal("an unknown plan model must be rejected")
	}
	if !strings.Contains(err.Error(), "kolk pmodels") {
		t.Fatalf("error = %v, want it to point at the command that lists plan models", err)
	}
	if !errors.Is(err, ErrNotAPlanModel) {
		t.Fatalf("error = %v, want callers to be able to tell an ordinary model apart", err)
	}
}

func TestResolvePlanModelDoesNotTreatAnUnusableOneAsOrdinary(t *testing.T) {
	// A plan model the user cannot use yet must stop the session with its
	// reason, never fall through to an ordinary provider.
	for _, ref := range []string{"claude-opus", "gemini-2.5-flash"} {
		_, err := ResolvePlanModel(ref, ConnectorManifest{Version: connectorManifestVersion})
		if err == nil {
			t.Fatalf("%s resolved without an enabled connector", ref)
		}
		if errors.Is(err, ErrNotAPlanModel) {
			t.Fatalf("%s was reported as an ordinary model: %v", ref, err)
		}
	}
}

func TestResolvePlanModelReportsAmbiguityWithTheQualifiedForms(t *testing.T) {
	catalog := []PlanModel{
		{Provider: "anthropic", Plan: "Google AI Pro", Connector: "claude", Model: "gemini-2.5-pro", Access: "provider CLI"},
		{Provider: "anthropic", Plan: "Google AI Ultra", Connector: "claude", Model: "gemini-2.5-pro", Access: "provider CLI"},
	}
	_, err := resolvePlanModel(catalog, "gemini-2.5-pro", enabledClaude())
	if err == nil {
		t.Fatal("a model offered by two plans must not be resolved silently")
	}
	for _, want := range []string{"Google AI Pro/gemini-2.5-pro", "Google AI Ultra/gemini-2.5-pro"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to offer %q", err, want)
		}
	}
}

func TestResolvePlanModelAcceptsAPlanQualifiedReference(t *testing.T) {
	got, err := ResolvePlanModel("Google AI Ultra/gemini-2.5-pro", ConnectorManifest{
		Version: connectorManifestVersion,
		Connectors: []Connector{{
			Provider: "google", Plan: "Google AI Ultra", Name: "gemini",
			LoginOwner: "provider-cli", Enabled: true,
		}},
	})
	if err == nil {
		t.Fatal("Gemini subscription reuse is not permitted and must stay rejected")
	}
	if !strings.Contains(err.Error(), "unsupported subscription") {
		t.Fatalf("error = %v, want the access reason", err)
	}
	if got.Plan != "" {
		t.Fatalf("a rejected model must not be returned: %+v", got)
	}
}

func TestResolvePlanModelTellsTheUserHowToEnableAConnector(t *testing.T) {
	_, err := ResolvePlanModel("claude-opus", ConnectorManifest{Version: connectorManifestVersion})
	if err == nil {
		t.Fatal("a plan model whose connector is not enabled must be rejected")
	}
	if !strings.Contains(err.Error(), `kolk plans login anthropic "Claude Max"`) {
		t.Fatalf("error = %v, want the exact command that enables it", err)
	}
}

func TestResolvePlanModelRejectsADisabledConnector(t *testing.T) {
	_, err := ResolvePlanModel("claude-opus", ConnectorManifest{
		Version: connectorManifestVersion,
		Connectors: []Connector{{
			Provider: "anthropic", Plan: "Claude Max", Name: "claude",
			LoginOwner: "provider-cli", Enabled: false,
		}},
	})
	if err == nil {
		t.Fatal("an explicitly disabled connector must not be used")
	}
}

func TestResolvePlanModelExplainsWhenEveryPlanOfferingItIsUnusable(t *testing.T) {
	// Two Google plans offer gemini-2.5-pro and neither may be reused. Asking
	// the user to pick one of them would only produce a second refusal.
	_, err := ResolvePlanModel("gemini-2.5-pro", enabledClaude())
	if err == nil {
		t.Fatal("an unusable model must be refused")
	}
	if !strings.Contains(err.Error(), "unsupported subscription") {
		t.Fatalf("error = %v, want the reason rather than a choice between dead ends", err)
	}
	if strings.Contains(err.Error(), "name one of") {
		t.Fatalf("error = %v, want no pointless disambiguation", err)
	}
}
