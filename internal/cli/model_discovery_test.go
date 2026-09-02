package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

// The port contract, enforced: a connector without a lister cannot exist.
// Every plan kolk can name resolves to a lister, and every lister either
// answers or says why it cannot — never nil, never an empty success.
func TestEveryConnectorCanListItsModels(t *testing.T) {
	gateway := []provider.ModelInfo{
		{ID: "anthropic/claude-fable-5", Name: "Claude Fable 5", ContextLength: 1000000},
		{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro", ContextLength: 1000000},
		{ID: "x-ai/grok-5", Name: "Grok 5", ContextLength: 256000},
		{ID: "perplexity/sonar-pro", Name: "Sonar Pro", ContextLength: 200000},
		{ID: "mistralai/mistral-large", Name: "Mistral Large", ContextLength: 128000},
		{ID: "deepseek/deepseek-r1", Name: "DeepSeek R1", ContextLength: 128000},
		{ID: "qwen/qwen3-coder", Name: "Qwen3 Coder", ContextLength: 256000},
		{ID: "cohere/command-a", Name: "Command A", ContextLength: 256000},
	}
	seen := map[string]bool{}
	for _, plan := range provider.Plans("") {
		if seen[plan.Connector] {
			continue
		}
		seen[plan.Connector] = true
		lister := modelListerFor(plan.Connector, gateway)
		if lister == nil {
			t.Errorf("connector %q (%s) has no model lister; a vendor without one cannot be registered", plan.Connector, plan.Name)
			continue
		}
		switch l := lister.(type) {
		case provider.NotListable:
			if l.Reason == "" {
				t.Errorf("connector %q is NotListable without a reason", plan.Connector)
			}
		case provider.GatewayPreviewLister, agentcli.ClaudePreviewLister:
			catalog, err := l.Discover(context.Background())
			if err != nil {
				t.Errorf("connector %q gateway preview: %v", plan.Connector, err)
				continue
			}
			for _, model := range catalog.Models {
				if model.Status != provider.StatusUnverified {
					t.Errorf("connector %q previewed %s as %q, want unverified before any turn", plan.Connector, model.ID, model.Status)
				}
			}
		}
	}
	if modelListerFor("no-such-vendor", gateway) != nil {
		t.Fatal("an unknown connector produced a lister")
	}
}

func TestClaudePreviewCarriesTheVendorEffortSet(t *testing.T) {
	gateway := []provider.ModelInfo{{ID: "anthropic/claude-sonnet-5", Name: "Claude Sonnet 5", ContextLength: 1000000}}
	catalog, err := modelListerFor("claude", gateway).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].ID != "claude-sonnet" || strings.Join(catalog.Models[0].Efforts, ",") != "low,medium,high,xhigh,max" || catalog.Models[0].ExactIDs[0] != "anthropic/claude-sonnet-5" {
		t.Fatalf("claude preview = %+v, want the family row with the CLI's five efforts and the exact gateway id", catalog.Models)
	}
}

func TestOllamaListerAsksTheCloudCatalogAndReportsFailure(t *testing.T) {
	lister := ollamaCloudLister{list: func(context.Context) ([]local.CloudCatalogModel, error) {
		return []local.CloudCatalogModel{{Name: "qwen3-coder:480b-cloud", Parameters: "480B"}}, nil
	}}
	catalog, err := lister.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].ID != "qwen3-coder:480b-cloud" || catalog.Models[0].Status != provider.StatusListed {
		t.Fatalf("ollama catalog = %+v", catalog.Models)
	}
	down := ollamaCloudLister{list: func(context.Context) ([]local.CloudCatalogModel, error) { return nil, errors.New("dial tcp: timeout") }}
	if _, err := down.Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("unreachable cloud catalog = %v, want the reason", err)
	}
}

type fakeVendorLister struct {
	catalog provider.VendorCatalog
	err     error
	calls   *int
}

func (f fakeVendorLister) Discover(context.Context) (provider.VendorCatalog, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.catalog, f.err
}

func fakeRegistry(calls map[string]*int, catalogs map[string]provider.VendorCatalog, errs map[string]error) func(string, []provider.ModelInfo) provider.ModelLister {
	return func(connector string, _ []provider.ModelInfo) provider.ModelLister {
		if calls[connector] == nil {
			calls[connector] = new(int)
		}
		return fakeVendorLister{catalog: catalogs[connector], err: errs[connector], calls: calls[connector]}
	}
}

// Every start maps every signed-in vendor, behind the prompt, and the model
// commands read the result. A vendor not signed in is not asked.
func TestStartupDiscoversEveryEnabledConnectorInTheBackground(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "anthropic", "Claude Max", "claude")
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	// Recorded but disabled: signed out, or never confirmed. Not asked.
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "google", Plan: "Google AI Pro", Name: "gemini", LoginOwner: "provider-cli", Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	calls := map[string]*int{}
	a.modelLister = fakeRegistry(calls, map[string]provider.VendorCatalog{
		"claude": {Vendor: "claude", Source: "gateway preview", VendorVersion: "2.1.258", Models: []provider.DiscoveredModel{{ID: "claude-fable", Rank: 1, Status: provider.StatusUnverified}}},
		"codex":  {Vendor: "codex", Source: "codex debug models", VendorVersion: "0.149.1", Models: []provider.DiscoveredModel{{ID: "gpt-5.6-sol", Rank: 1, Status: provider.StatusListed}}},
	}, nil)

	a.refreshVendorCatalogsInBackground(context.Background(), nil)
	a.background.Wait()

	store, err := provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Vendors["codex"].Find("gpt-5.6-sol"); !ok {
		t.Fatalf("codex was not mapped at startup: %+v", store.Vendors)
	}
	if _, ok := store.Vendors["claude"].Find("claude-fable"); !ok {
		t.Fatalf("claude was not mapped at startup: %+v", store.Vendors)
	}
	if calls["gemini"] != nil || calls["ollama"] != nil {
		t.Fatalf("a vendor not signed in was asked: %v", calls)
	}
	if *calls["codex"] != 1 || *calls["claude"] != 1 {
		t.Fatalf("each vendor is asked once per start: %v", calls)
	}
}

// Every login maps that connector in front of the user and says what it
// found; a vendor that will not answer is reported with the reason and keeps
// its last catalog.
func TestLoginDiscoversThatConnectorAndSaysWhatItFound(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	signInAs(t, dirs, "anthropic", "Claude Max", "claude")
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs
	calls := map[string]*int{}
	a.modelLister = fakeRegistry(calls, map[string]provider.VendorCatalog{
		"codex": {Vendor: "codex", Source: "codex debug models", VendorVersion: "0.149.1", Models: []provider.DiscoveredModel{
			{ID: "gpt-5.6-sol", Rank: 1, Status: provider.StatusListed}, {ID: "gpt-5.4", Rank: 16, Hidden: true, Status: provider.StatusListed},
		}},
	}, nil)

	a.reportVendorDiscovery(context.Background(), "codex")
	got := out.String()
	for _, want := range []string{"codex 0.149.1", "1 models listed by codex debug models", "gpt-5.6-sol", "`kolk models`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("login report = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "gpt-5.4") {
		t.Fatalf("login report named a hidden model: %q", got)
	}
	if calls["claude"] != nil {
		t.Fatal("a login for codex asked claude")
	}

	out.Reset()
	a.modelLister = fakeRegistry(calls, nil, map[string]error{"codex": errors.New("`codex debug models` failed: not logged in")})
	a.reportVendorDiscovery(context.Background(), "codex")
	if !strings.Contains(out.String(), "could not be listed") || !strings.Contains(out.String(), "not logged in") {
		t.Fatalf("a failing vendor was not reported with its reason: %q", out.String())
	}
	store, _ := provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if _, ok := store.Vendors["codex"].Find("gpt-5.6-sol"); !ok {
		t.Fatal("a failed discovery blanked the last catalog")
	}
}

// A model verified under one vendor version is not proved under the next.
func TestAVendorVersionChangeForgetsWhatATurnHadVerified(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs
	var store provider.VendorCatalogs
	store.Replace(provider.VendorCatalog{Vendor: "codex", VendorVersion: "0.149.1", Models: []provider.DiscoveredModel{{ID: "gpt-5.6-sol", Status: provider.StatusListed}}})
	store.Verify("codex", "gpt-5.6-sol", "gpt-5.6-sol", time.Now())
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}

	a.modelLister = fakeRegistry(map[string]*int{}, map[string]provider.VendorCatalog{
		"codex": {Vendor: "codex", Source: "codex debug models", VendorVersion: "0.150.0", Models: []provider.DiscoveredModel{{ID: "gpt-5.6-sol", Rank: 1, Status: provider.StatusListed}}},
	}, nil)
	a.reportVendorDiscovery(context.Background(), "codex")
	if !strings.Contains(out.String(), "version changed") {
		t.Fatalf("report = %q, want the version change named", out.String())
	}
	store, _ = provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	sol, _ := store.Vendors["codex"].Find("gpt-5.6-sol")
	if sol.Status != provider.StatusListed {
		t.Fatalf("after a version change: %+v, want listed, not still verified", sol)
	}

	// Same version: the turn's proof carries forward.
	a.modelLister = fakeRegistry(map[string]*int{}, map[string]provider.VendorCatalog{
		"codex": {Vendor: "codex", VendorVersion: "0.150.0", Models: []provider.DiscoveredModel{{ID: "gpt-5.6-sol", Status: provider.StatusListed}}},
	}, nil)
	store.Verify("codex", "gpt-5.6-sol", "", time.Now())
	_ = provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store)
	a.reportVendorDiscovery(context.Background(), "codex")
	store, _ = provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if sol, _ := store.Vendors["codex"].Find("gpt-5.6-sol"); sol.Status != provider.StatusVerified {
		t.Fatalf("same version re-discovery lost the turn's proof: %+v", sol)
	}
}

// The login path itself: after the connector is recorded, the vendor is
// mapped before the command returns, and the user sees what it found.
func TestPlanLoginRunsTheVendorMappingBeforeReturning(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs
	a.modelLister = fakeRegistry(map[string]*int{}, map[string]provider.VendorCatalog{
		"codex": {Vendor: "codex", Source: "codex debug models", VendorVersion: "0.149.1", Models: []provider.DiscoveredModel{{ID: "gpt-5.6-sol", Rank: 1, Status: provider.StatusListed}}},
	}, nil)
	selected := provider.Plan{Provider: "openai", Name: "ChatGPT Plus", Connector: "codex", Auth: "provider CLI", Billing: "subscription", Sandbox: true}
	noLogin := func(context.Context, string, []string) error { return nil }

	if err := a.runConnectorLoginWith(context.Background(), dirs.ConnectorsFile(), selected, noLogin); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ChatGPT Plus recorded") {
		t.Fatalf("login output = %q", got)
	}
	if !strings.Contains(got, "codex 0.149.1: 1 models listed by codex debug models: gpt-5.6-sol") {
		t.Fatalf("login did not map the vendor before returning:\n%s", got)
	}
	store, err := provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Vendors["codex"].Find("gpt-5.6-sol"); !ok {
		t.Fatalf("login left no vendor catalog: %+v", store.Vendors)
	}
}

// Availability as the vendor states it. Once codex has been asked, the ladder's
// gpt-5.6-pro is not a rung the roster may descend to, and the vendor's
// gpt-5.5 is spawnable although kolk's seed never heard of it; a vendor not
// yet asked answers from the seed.
func TestRungAvailabilityFollowsTheVendorCatalog(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	signInAs(t, dirs, "anthropic", "Claude Max", "claude")
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs

	before := a.rungAvailable()
	if !before("codex", "gpt-5.6-pro") || before("codex", "gpt-5.5") {
		t.Fatal("before discovery the seed must answer: gpt-5.6-pro yes, gpt-5.5 no")
	}

	var store provider.VendorCatalogs
	store.Replace(provider.VendorCatalog{Vendor: "codex", VendorVersion: "0.149.1", Models: []provider.DiscoveredModel{
		{ID: "gpt-5.6-sol", Rank: 1, Status: provider.StatusListed}, {ID: "gpt-5.5", Rank: 7, Status: provider.StatusListed},
	}})
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	after := a.rungAvailable()
	if after("codex", "gpt-5.6-pro") {
		t.Fatal("gpt-5.6-pro is still a rung after the vendor stopped listing it")
	}
	if !after("codex", "gpt-5.5") || !after("codex", "gpt-5.6-sol") {
		t.Fatal("a model the vendor lists is not available")
	}
	if !after("claude", "claude-haiku") {
		t.Fatal("a vendor not yet asked stopped answering from its seed")
	}

	// A gone model named at the prompt is refused with what happened to it.
	manifest, _ := provider.LoadConnectors(dirs.ConnectorsFile())
	_, err := a.resolvePlanModel("gpt-5.6-pro", manifest)
	if !errors.Is(err, provider.ErrModelGone) {
		t.Fatalf("selecting a gone model = %v, want ErrModelGone", err)
	}
	if got, err := a.resolvePlanModel("gpt-5.5", manifest); err != nil || got.Model != "gpt-5.5" {
		t.Fatalf("selecting a discovered model = %+v, %v", got, err)
	}
	if !strings.Contains(fmt.Sprint(a.planModels("gpt-5.5")), "gpt-5.5") {
		t.Fatal("pmodels does not carry the discovered model")
	}
}
