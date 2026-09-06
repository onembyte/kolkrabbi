package provider

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPlanModelsFilterAndEfforts(t *testing.T) {
	got := PlanModelsFrom(VendorCatalogs{}, "gemini")
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
	for _, model := range PlanModelsFrom(VendorCatalogs{}, "") {
		if model.Provider == "" || model.Plan == "" || model.Connector == "" ||
			model.Model == "" || model.Access == "" {
			t.Errorf("incomplete plan model metadata: %+v", model)
		}
		// A row on the vendor's `auto` has no dial: Copilot refuses an
		// effort on it (observed 2026-09-06). Every named model carries one.
		if len(model.Efforts) == 0 && model.Model != "auto" {
			t.Errorf("model has no effort metadata: %+v", model)
		}
	}
}

func TestPlanModelsIncludeTheCompleteGPT56SubscriptionFamily(t *testing.T) {
	want := map[string]map[string]bool{
		"ChatGPT Plus": {"gpt-5.6-sol": true, "gpt-5.6-terra": true, "gpt-5.6-luna": true},
		"ChatGPT Pro":  {"gpt-5.6-pro": true, "gpt-5.6-sol": true, "gpt-5.6-terra": true, "gpt-5.6-luna": true},
	}
	got := map[string]map[string]bool{}
	for _, model := range PlanModelsFrom(VendorCatalogs{}, "gpt-5.6") {
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
	got, err := ResolvePlanModelFrom(VendorCatalogs{}, "claude-opus", enabledClaude())
	if err != nil {
		t.Fatal(err)
	}
	if got.Connector != "claude" || got.Plan != "Claude Max" {
		t.Fatalf("resolved = %+v", got)
	}
}

func TestResolvePlanModelIsCaseInsensitive(t *testing.T) {
	if _, err := ResolvePlanModelFrom(VendorCatalogs{}, "  Claude-Opus  ", enabledClaude()); err != nil {
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
			got, err := ResolvePlanModelFrom(VendorCatalogs{}, test.alias, manifest)
			if err != nil {
				t.Fatalf("ResolvePlanModelFrom(VendorCatalogs{}, %q): %v", test.alias, err)
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
			got, err := ResolvePlanModelFrom(VendorCatalogs{}, "gpt-5.6-terra", ConnectorManifest{
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
	got, err := ResolvePlanModelFrom(VendorCatalogs{}, "gpt-5.6-luna", ConnectorManifest{
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
	got, err := ResolvePlanModelFrom(VendorCatalogs{}, "gpt-5.6-terra", ConnectorManifest{Connectors: []Connector{
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
	_, err := ResolvePlanModelFrom(VendorCatalogs{}, "no-such-model", enabledClaude())
	if err == nil {
		t.Fatal("an unknown plan model must be rejected")
	}
	if !strings.Contains(err.Error(), "/pmodels") {
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
		_, err := ResolvePlanModelFrom(VendorCatalogs{}, ref, ConnectorManifest{Version: connectorManifestVersion})
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
	got, err := ResolvePlanModelFrom(VendorCatalogs{}, "Google AI Ultra/gemini-2.5-pro", ConnectorManifest{
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
	_, err := ResolvePlanModelFrom(VendorCatalogs{}, "claude-opus", ConnectorManifest{Version: connectorManifestVersion})
	if err == nil {
		t.Fatal("a plan model whose connector is not enabled must be rejected")
	}
	if !strings.Contains(err.Error(), `/plans login anthropic "Claude Max"`) {
		t.Fatalf("error = %v, want the exact command that enables it", err)
	}
}

func TestResolvePlanModelRejectsADisabledConnector(t *testing.T) {
	_, err := ResolvePlanModelFrom(VendorCatalogs{}, "claude-opus", ConnectorManifest{
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
	_, err := ResolvePlanModelFrom(VendorCatalogs{}, "gemini-2.5-pro", enabledClaude())
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
	for _, model := range PlanModelsFrom(VendorCatalogs{}, "anthropic") {
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
		if _, err := ResolvePlanModelFrom(VendorCatalogs{}, model, enabledClaude()); err != nil {
			t.Errorf("Max login cannot select %s: %v", model, err)
		}
	}
	for _, model := range []string{"claude-haiku", "claude-sonnet"} {
		if _, err := ResolvePlanModelFrom(VendorCatalogs{}, model, pro); err != nil {
			t.Errorf("Pro login cannot select %s: %v", model, err)
		}
	}
	_, err := ResolvePlanModelFrom(VendorCatalogs{}, "claude-fable", pro)
	if err == nil || !strings.Contains(err.Error(), `/plans login anthropic "Claude Max"`) {
		t.Fatalf("Pro login selecting fable = %v, want the Max sign-in named", err)
	}
}

// The plan catalog as the vendors describe it. Codex on 2026-09-02: the seed's
// gpt-5.6-pro is gone, gpt-5.5 arrives on every Codex tier with the vendor's
// efforts including ultra, and Sol keeps its tiers but takes the vendor's
// efforts and status. Claude, previewed: the family rows carry unverified.
// A vendor never asked keeps its seed rows, marked unverified.
func TestDerivedPlanCatalogIsWhatTheVendorsSaid(t *testing.T) {
	var store VendorCatalogs
	store.Replace(VendorCatalog{Vendor: "codex", VendorVersion: "0.149.1", Models: []DiscoveredModel{
		{ID: "gpt-5.6-sol", Rank: 1, Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, Context: 272000, Status: StatusListed},
		{ID: "gpt-5.6-terra", Rank: 2, Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, Status: StatusListed},
		{ID: "gpt-5.6-luna", Rank: 3, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, Status: StatusListed},
		{ID: "gpt-5.5", Rank: 7, Efforts: []string{"low", "medium", "high", "xhigh"}, Status: StatusListed},
		{ID: "gpt-5.4", Rank: 16, Hidden: true, Status: StatusListed},
	}})
	store.Replace(VendorCatalog{Vendor: "claude", Models: []DiscoveredModel{
		{ID: "claude-fable", Rank: 1, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, Context: 1000000, Status: StatusUnverified},
		{ID: "claude-opus", Rank: 2, Status: StatusVerified},
	}})

	derived := DerivePlanModels(store)
	find := func(plan, model string) (PlanModel, bool) {
		for _, row := range derived {
			if row.Plan == plan && strings.EqualFold(row.Model, model) {
				return row, true
			}
		}
		return PlanModel{}, false
	}
	pro, _ := find("ChatGPT Pro", "gpt-5.6-pro")
	if pro.Status != StatusGone {
		t.Fatalf("gpt-5.6-pro = %+v, want gone: the vendor does not list it", pro)
	}
	sol, _ := find("ChatGPT Plus", "gpt-5.6-sol")
	if sol.Status != StatusListed || strings.Join(sol.Efforts, ",") != "low,medium,high,xhigh,max,ultra" || sol.Context != 272000 {
		t.Fatalf("sol = %+v, want the vendor's efforts (with ultra) and context", sol)
	}
	for _, plan := range []string{"ChatGPT Plus", "ChatGPT Pro"} {
		if row, ok := find(plan, "gpt-5.5"); ok {
			t.Fatalf("gpt-5.5 on %s = %+v; a discovered model carries no tier and is listed only once a turn verified it (V34.4a)", plan, row)
		}
	}
	if _, ok := find("ChatGPT Plus", "gpt-5.4"); ok {
		t.Fatal("a hidden vendor row became a plan row")
	}
	fable, _ := find("Claude Max", "claude-fable")
	if fable.Status != StatusUnverified || fable.Context != 1000000 {
		t.Fatalf("fable = %+v, want the preview's status and context", fable)
	}
	if opus, _ := find("Claude Max", "claude-opus"); opus.Status != StatusVerified {
		t.Fatalf("opus = %+v, want the turn's verification carried into the plan row", opus)
	}
	if haiku, _ := find("Claude Pro", "claude-haiku"); haiku.Status != StatusGone {
		t.Fatalf("haiku = %+v, want gone: the vendor was asked and did not name it", haiku)
	}
	gemini, _ := find("Google AI Pro", "gemini-2.5-pro")
	if gemini.Status != StatusUnverified {
		t.Fatalf("a vendor never asked = %+v, want its seed row marked unverified", gemini)
	}
	if bare := PlanModelsFrom(VendorCatalogs{}, ""); bare[0].Status != StatusUnverified {
		t.Fatalf("with no vendor asked, a seed row = %+v, want unverified", bare[0])
	}
}

// Resolution reads the derived catalog: a discovered model resolves, a gone
// model is refused by name with the vendor's version, and an unknown name is
// still not a plan model.
func TestResolvePlanModelFromTheVendorCatalog(t *testing.T) {
	codex := ConnectorManifest{Version: connectorManifestVersion, Connectors: []Connector{
		{Provider: "openai", Plan: "ChatGPT Plus", Name: "codex", LoginOwner: "provider-cli", Enabled: true},
	}}
	var store VendorCatalogs
	store.Replace(VendorCatalog{Vendor: "codex", VendorVersion: "0.149.1", Models: []DiscoveredModel{
		{ID: "gpt-5.6-sol", Rank: 1, Status: StatusListed}, {ID: "gpt-5.5", Rank: 7, Status: StatusListed},
	}})
	if got, err := ResolvePlanModelFrom(store, "gpt-5.5", codex); err != nil || got.Model != "gpt-5.5" || got.Plan != "ChatGPT Plus" {
		t.Fatalf("a discovered model = %+v, %v", got, err)
	}
	_, err := ResolvePlanModelFrom(store, "gpt-5.6-pro", codex)
	if !errors.Is(err, ErrModelGone) || !strings.Contains(err.Error(), "codex 0.149.1 does not list gpt-5.6-pro") {
		t.Fatalf("a gone model = %v, want ErrModelGone naming the vendor and version", err)
	}
	if _, err := ResolvePlanModelFrom(store, "gpt-9", codex); !errors.Is(err, ErrNotAPlanModel) {
		t.Fatalf("an unknown name = %v, want ErrNotAPlanModel", err)
	}
	if _, err := ResolvePlanModelFrom(VendorCatalogs{}, "gpt-5.6-pro", ConnectorManifest{Version: connectorManifestVersion, Connectors: []Connector{{Provider: "openai", Plan: "ChatGPT Pro", Name: "codex", LoginOwner: "provider-cli", Enabled: true}}}); err != nil {
		t.Fatalf("with no vendor catalog the seed still resolves: %v", err)
	}
}

// Once discovery fills the Claude catalog, a family row offers
// [low medium high xhigh max]. Folding xhigh into max and returning the first
// spelling at that rank sent `-e max` to the vendor as --effort xhigh, which
// the vendor treats as a different level. An exact spelling wins; the fold
// still serves a catalog that offers only one of the two.
func TestMaxStaysMaxWhenTheVendorOffersBothXhighAndMax(t *testing.T) {
	both := []string{"low", "medium", "high", "xhigh", "max"}
	if got, down := EffortForPlan("max", both); got != "max" || down {
		t.Fatalf("max on %v = %q downgraded=%v, want max", both, got, down)
	}
	if got, down := EffortForPlan("xhigh", both); got != "xhigh" || down {
		t.Fatalf("xhigh on %v = %q downgraded=%v, want xhigh", both, got, down)
	}
	if got, down := EffortForPlan("max", []string{"low", "medium", "high", "xhigh"}); got != "xhigh" || down {
		t.Fatalf("max on an xhigh-only catalog = %q downgraded=%v, want xhigh without a downgrade", got, down)
	}
	if got, down := EffortForPlan("xhigh", []string{"low", "medium", "high", "max"}); got != "max" || down {
		t.Fatalf("xhigh on a max-only catalog = %q downgraded=%v, want max without a downgrade", got, down)
	}
}

// V34.4a, the owner's conservative default: a model the vendor lists and the
// seed never heard of carries no tier, so it is listed on the connector's tiers
// only after its first answered turn verifies it. Until then it is reachable by
// name on the plan the signed-in connector has, and nowhere else — never a
// keyed model by default, never a row on a tier nobody has seen reach it.
func TestADiscoveredModelIsListedOnTiersOnlyOnceATurnVerifiedIt(t *testing.T) {
	var store VendorCatalogs
	store.Replace(VendorCatalog{Vendor: "codex", Models: []DiscoveredModel{
		{ID: "gpt-5.5", Rank: 7, Efforts: []string{"low", "medium"}, Context: 200000, Status: StatusListed},
	}})
	rows := func(model string) []PlanModel {
		var out []PlanModel
		for _, row := range DerivePlanModels(store) {
			if strings.EqualFold(row.Model, model) {
				out = append(out, row)
			}
		}
		return out
	}
	if got := rows("gpt-5.5"); len(got) != 0 {
		t.Fatalf("unverified discovered model has plan rows %+v, want none", got)
	}

	signedIn := ConnectorManifest{Connectors: []Connector{{Provider: "openai", Plan: "ChatGPT Pro", Name: "codex", Enabled: true}}}
	resolved, err := ResolvePlanModelFrom(store, "gpt-5.5", signedIn)
	if err != nil {
		t.Fatalf("by name on the signed-in plan: %v", err)
	}
	if resolved.Plan != "ChatGPT Pro" || resolved.Connector != "codex" || resolved.Provider != "openai" ||
		resolved.Access != "provider CLI" || resolved.Status != StatusListed || strings.Join(resolved.Efforts, ",") != "low,medium" || resolved.Context != 200000 {
		t.Fatalf("resolved = %+v, want the vendor's row on the plan the connector is signed into", resolved)
	}
	if _, err := ResolvePlanModelFrom(store, "ChatGPT Pro/gpt-5.5", signedIn); err != nil {
		t.Fatalf("plan-qualified by name: %v", err)
	}
	_, err = ResolvePlanModelFrom(store, "gpt-5.5", ConnectorManifest{})
	if err == nil || errors.Is(err, ErrNotAPlanModel) || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("not signed in: err = %v; want the connector named, never a keyed model", err)
	}

	store.Verify("codex", "gpt-5.5", "gpt-5.5", time.Now())
	got := rows("gpt-5.5")
	plans := map[string]bool{}
	for _, row := range got {
		if row.Status != StatusVerified {
			t.Fatalf("row after verification = %+v, want verified", row)
		}
		plans[row.Plan] = true
	}
	if !plans["ChatGPT Plus"] || !plans["ChatGPT Pro"] || len(got) != 2 {
		t.Fatalf("verified discovered model rows = %+v, want one per tier the connector uses", got)
	}
}

// A plan that stops at max clamps kolk's ultra to max and says so; a plan
// that lists ultra keeps it.
func TestUltraClampsToWhatThePlanOffers(t *testing.T) {
	got, clamped := EffortForPlan("ultra", []string{"low", "medium", "high", "max"})
	if got != "max" || !clamped {
		t.Fatalf("ultra on a max plan = %q, clamped %v; want max, true", got, clamped)
	}
	got, clamped = EffortForPlan("ultra", []string{"low", "medium", "high", "xhigh", "ultra"})
	if got != "ultra" || clamped {
		t.Fatalf("ultra on a plan listing it = %q, clamped %v; want ultra, false", got, clamped)
	}
	if got, _ := EffortForPlan("max", []string{"low", "high", "xhigh"}); got != "xhigh" {
		t.Fatalf("max on an xhigh plan = %q, want xhigh", got)
	}
}
