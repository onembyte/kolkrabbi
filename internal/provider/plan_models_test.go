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

func TestPlanModelsIncludeTheCompleteGPT56SubscriptionFamily(t *testing.T) {
	want := map[string]map[string]bool{
		"ChatGPT Plus": {"gpt-5.6-sol": true, "gpt-5.6-terra": true, "gpt-5.6-luna": true},
		"ChatGPT Pro":  {"gpt-5.6-pro": true, "gpt-5.6-sol": true, "gpt-5.6-terra": true, "gpt-5.6-luna": true},
	}
	got := map[string]map[string]bool{}
	for _, model := range PlanModels("gpt-5.6") {
		if got[model.Plan] == nil {
			got[model.Plan] = map[string]bool{}
		}
		got[model.Plan][model.Model] = true
	}
	for plan, models := range want {
		for model := range models {
			if !got[plan][model] {
				t.Errorf("missing %s on %s from plan catalog", model, plan)
			}
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

func TestResolvePlanModelAcceptsFriendlySubscriptionAliases(t *testing.T) {
	for _, test := range []struct {
		alias string
		model string
		plan  string
	}{
		{alias: "claude-max", model: "claude-opus", plan: "Claude Max"},
		{alias: "gpt-plus", model: "gpt-5.6-sol", plan: "ChatGPT Plus"},
		{alias: "gpt-pro", model: "gpt-5.6-pro", plan: "ChatGPT Pro"},
	} {
		t.Run(test.alias, func(t *testing.T) {
			manifest := enabledClaude()
			if test.plan != "Claude Max" {
				manifest = ConnectorManifest{Version: connectorManifestVersion, Connectors: []Connector{{
					Provider: "openai", Plan: test.plan, Name: "codex", LoginOwner: "provider-cli", Enabled: true,
				}}}
			}
			got, err := ResolvePlanModel(test.alias, manifest)
			if err != nil {
				t.Fatalf("ResolvePlanModel(%q): %v", test.alias, err)
			}
			if got.Model != test.model || got.Plan != test.plan {
				t.Fatalf("resolved = %+v, want %s on %s", got, test.model, test.plan)
			}
		})
	}
}

func TestResolvePlanModelUsesTheEnabledPlanForAnUnqualifiedSharedModel(t *testing.T) {
	for _, plan := range []string{"ChatGPT Plus", "ChatGPT Pro"} {
		t.Run(plan, func(t *testing.T) {
			got, err := ResolvePlanModel("gpt-5.6-terra", ConnectorManifest{
				Version: connectorManifestVersion,
				Connectors: []Connector{{
					Provider: "openai", Plan: plan, Name: "codex",
					LoginOwner: "provider-cli", Enabled: true,
				}},
			})
			if err != nil {
				t.Fatalf("ResolvePlanModel: %v", err)
			}
			if got.Model != "gpt-5.6-terra" || got.Plan != plan {
				t.Fatalf("resolved = %+v, want Terra on %s", got, plan)
			}
		})
	}
}

func TestResolvePlanModelChoosesTheHighestKnownTierWhenStaleRecordsOverlap(t *testing.T) {
	got, err := ResolvePlanModel("gpt-5.6-luna", ConnectorManifest{
		Version: connectorManifestVersion,
		Connectors: []Connector{
			{Provider: "openai", Plan: "ChatGPT Plus", Name: "codex", LoginOwner: "provider-cli", Enabled: true},
			{Provider: "openai", Plan: "ChatGPT Pro", Name: "codex", LoginOwner: "provider-cli", Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("ResolvePlanModel: %v", err)
	}
	if got.Model != "gpt-5.6-luna" || got.Plan != "ChatGPT Pro" {
		t.Fatalf("resolved = %+v, want Luna through the highest enabled known tier", got)
	}
}

func TestResolvePlanModelKnownTierChoiceDoesNotDependOnCatalogOrder(t *testing.T) {
	catalog := []PlanModel{
		{Provider: "openai", Plan: "ChatGPT Pro", Connector: "codex", Model: "shared", Access: "provider CLI"},
		{Provider: "openai", Plan: "ChatGPT Plus", Connector: "codex", Model: "shared", Access: "provider CLI"},
	}
	manifest := ConnectorManifest{Connectors: []Connector{
		{Provider: "openai", Plan: "ChatGPT Plus", Name: "codex", Enabled: true},
		{Provider: "openai", Plan: "ChatGPT Pro", Name: "codex", Enabled: true},
	}}
	got, err := resolvePlanModel(catalog, "shared", manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Plan != "ChatGPT Pro" {
		t.Fatalf("resolved plan = %q, want the higher tier despite reversed catalog order", got.Plan)
	}
}

func TestResolvePlanModelDoesNotLetADisabledHigherTierOverrideAnEnabledOne(t *testing.T) {
	got, err := ResolvePlanModel("gpt-5.6-terra", ConnectorManifest{Connectors: []Connector{
		{Provider: "openai", Plan: "ChatGPT Plus", Name: "codex", Enabled: true},
		{Provider: "openai", Plan: "ChatGPT Pro", Name: "codex", Enabled: false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Plan != "ChatGPT Plus" {
		t.Fatalf("resolved plan = %q, want the only enabled tier", got.Plan)
	}
}

func TestResolvePlanModelKeepsUnknownTierFamiliesAmbiguous(t *testing.T) {
	catalog := []PlanModel{
		{Provider: "vendor", Plan: "Basic", Connector: "vendor-cli", Model: "shared", Access: "provider CLI"},
		{Provider: "vendor", Plan: "Premium", Connector: "vendor-cli", Model: "shared", Access: "provider CLI"},
	}
	manifest := ConnectorManifest{Connectors: []Connector{
		{Provider: "vendor", Plan: "Basic", Name: "vendor-cli", Enabled: true},
		{Provider: "vendor", Plan: "Premium", Name: "vendor-cli", Enabled: true},
	}}
	_, err := resolvePlanModel(catalog, "shared", manifest)
	if err == nil || !strings.Contains(err.Error(), "name one of") {
		t.Fatalf("error = %v, want ambiguity for an unknown tier hierarchy", err)
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

func TestEffortForPlanKeepsALevelThePlanOffers(t *testing.T) {
	got, changed := EffortForPlan("high", []string{"low", "medium", "high", "max"})
	if got != "high" || changed {
		t.Fatalf("effort = %q changed = %v", got, changed)
	}
}

func TestEffortForPlanTreatsMaxAndXHighAsTheSameCapability(t *testing.T) {
	for _, tc := range []struct {
		requested string
		offered   []string
		want      string
	}{
		{requested: "max", offered: []string{"low", "medium", "high", "xhigh"}, want: "xhigh"},
		{requested: "xhigh", offered: []string{"low", "medium", "high", "max"}, want: "max"},
	} {
		got, changed := EffortForPlan(tc.requested, tc.offered)
		if got != tc.want || changed {
			t.Errorf("EffortForPlan(%q, %v) = (%q, %v), want (%q, false)",
				tc.requested, tc.offered, got, changed, tc.want)
		}
	}
}

func TestEffortForPlanStepsDownToTheHighestOffered(t *testing.T) {
	// Claude Pro stops at high. Sending max would have the provider decide what
	// the user meant.
	got, changed := EffortForPlan("max", []string{"low", "medium", "high"})
	if got != "high" || !changed {
		t.Fatalf("effort = %q changed = %v, want high and a reported change", got, changed)
	}
}

func TestEffortForPlanNeverSilentlyUpgrades(t *testing.T) {
	// Nothing at or below what was asked for: take the cheapest on offer, not
	// the closest, because the closest would spend more than the user chose.
	got, changed := EffortForPlan("low", []string{"medium", "high", "max"})
	if got != "medium" || !changed {
		t.Fatalf("effort = %q changed = %v, want the lowest offered", got, changed)
	}
}

func TestEffortForPlanPassesThroughWhenNothingIsAdvertised(t *testing.T) {
	got, changed := EffortForPlan("max", nil)
	if got != "max" || changed {
		t.Fatalf("effort = %q changed = %v, want an unknown plan to be left alone", got, changed)
	}
	if got, changed := EffortForPlan("", []string{"low"}); got != "" || changed {
		t.Fatalf("an unset effort must stay unset: %q %v", got, changed)
	}
}

// The two rungs the catalog did not have. Verified live on 2026-09-02 (claude
// 2.1.258): `--model haiku` and `--model fable` each completed a one-turn call;
// an invented model returned unrecognized_model at zero cost.
func TestPlanCatalogListsFableAndHaikuWithVerifiedEfforts(t *testing.T) {
	want := map[string]struct {
		plan    string
		efforts string
	}{
		"claude-haiku":  {"Claude Pro", "low,medium,high"},
		"claude-sonnet": {"Claude Pro", "low,medium,high"},
		"claude-opus":   {"Claude Max", "low,medium,high,max"},
		"claude-fable":  {"Claude Max", "low,medium,high,max"},
	}
	seen := map[string]bool{}
	for _, model := range PlanModels("anthropic") {
		expected, ok := want[model.Model]
		if !ok {
			t.Errorf("unexpected anthropic row %+v", model)
			continue
		}
		seen[model.Model] = true
		if model.Plan != expected.plan || strings.Join(model.Efforts, ",") != expected.efforts || model.Access != "provider CLI" || model.Connector != "claude" {
			t.Errorf("%s = %+v, want plan %s efforts %s", model.Model, model, expected.plan, expected.efforts)
		}
	}
	for model := range want {
		if !seen[model] {
			t.Errorf("catalog is missing %s", model)
		}
	}
}

// Tier eligibility: a Max login reaches every Claude rung; a Pro login reaches
// haiku and sonnet and is told which plan fable needs.
func TestFableNeedsMaxAndHaikuIsOnEveryClaudePlan(t *testing.T) {
	pro := ConnectorManifest{Version: connectorManifestVersion, Connectors: []Connector{
		{Provider: "anthropic", Plan: "Claude Pro", Name: "claude", LoginOwner: "provider-cli", Enabled: true},
	}}
	for _, model := range []string{"claude-haiku", "claude-sonnet", "claude-opus", "claude-fable"} {
		if _, err := ResolvePlanModel(model, enabledClaude()); err != nil {
			t.Errorf("Max login cannot select %s: %v", model, err)
		}
	}
	for _, model := range []string{"claude-haiku", "claude-sonnet"} {
		if _, err := ResolvePlanModel(model, pro); err != nil {
			t.Errorf("Pro login cannot select %s: %v", model, err)
		}
	}
	_, err := ResolvePlanModel("claude-fable", pro)
	if err == nil || !strings.Contains(err.Error(), `kolk plans login anthropic "Claude Max"`) {
		t.Fatalf("Pro login selecting fable = %v, want the Max sign-in named", err)
	}
}
