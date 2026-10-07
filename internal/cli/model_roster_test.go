package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

func TestAgentRosterFollowsFreshCatalogRanksAndExactLogins(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	store := provider.VendorCatalogs{Vendors: map[string]provider.VendorCatalog{"codex": {Vendor: "codex", Models: []provider.DiscoveredModel{
		{ID: "future-strong", Rank: 1, Status: provider.StatusListed},
		{ID: "future-selected", Rank: 2, Efforts: []string{"low", "high", "max"}, Status: provider.StatusListed},
		{ID: "future-mid", Rank: 3, Status: provider.StatusListed},
		{ID: "future-small", Rank: 4, Status: provider.StatusListed},
		{ID: "future-unknown", Rank: 0, Status: provider.StatusListed},
		{ID: "future-hidden", Rank: 5, Hidden: true, Status: provider.StatusListed},
		{ID: "future-gone", Rank: 6, Status: provider.StatusGone},
	}}}}
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	rosterFor := a.agentRoster
	if got := rosterFor("future-selected", ""); len(got.Rungs) != 1 {
		t.Fatalf("no login: %+v", got)
	}
	for _, identity := range []string{"anthropic", "openai"} {
		if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
			Provider: identity, Plan: "ChatGPT Pro", Name: "codex", LoginOwner: "provider-cli", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		got := rosterFor("future-selected", "")
		if identity == "anthropic" {
			if len(got.Rungs) != 1 {
				t.Fatalf("wrong identity: %+v", got)
			}
			continue
		}
		var ids []string
		for _, rung := range got.Rungs {
			ids = append(ids, rung.Model)
		}
		if strings.Join(ids, ",") != "future-selected,future-mid,future-small" || strings.Join(got.Blocked, ",") != "future-strong" {
			t.Fatalf("discovered roster: %+v", got)
		}
	}
	catalog := store.Vendors["codex"]
	catalog.Models[2].Status = provider.StatusGone
	store.Vendors["codex"] = catalog
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	if got := rosterFor("future-selected", ""); len(got.Rungs) != 2 || got.Rungs[1].Model != "future-small" {
		t.Fatalf("stale roster: %+v", got)
	}
	for _, model := range []string{"future-unknown", "openai/future-selected", "missing-model"} {
		if got := rosterFor(model, ""); len(got.Rungs) != 1 || got.Rungs[0].Model != model {
			t.Fatalf("guessed route for %s: %+v", model, got)
		}
	}
}

func TestDelegatedModelKeepsItsDiscoveredProviderBinding(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	store := provider.VendorCatalogs{Vendors: map[string]provider.VendorCatalog{}}
	for _, vendor := range []string{"claude", "codex"} {
		store.Vendors[vendor] = provider.VendorCatalog{Vendor: vendor, Models: []provider.DiscoveredModel{{ID: "shared-model", Rank: 1, Status: provider.StatusListed, Efforts: []string{"low"}}}}
	}
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{Provider: "openai", Plan: "ChatGPT Pro", Name: "codex", LoginOwner: "provider-cli", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	roster := a.agentRoster("shared-model", "codex")
	if roster.Ceiling().Vendor != "codex" {
		t.Fatalf("selected connector lost: %+v", roster)
	}
	caps := engine.SubagentCapabilities{Workspace: t.TempDir(), Provider: "codex"}
	backend, err := a.subagentBackend()(context.Background(), "shared-model", "code", "low", caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.(*agentcli.CodexBackend); !ok {
		t.Fatalf("wrong provider: %T", backend)
	}
	store.Vendors["codex"] = provider.VendorCatalog{Vendor: "codex"}
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	if _, err := a.subagentBackend()(context.Background(), "shared-model", "code", "low", caps); err == nil {
		t.Fatal("removed model silently changed provider")
	}
}

func TestDiscoveredAgentLaneShowsModelsPolicyAndLoginGuidance(t *testing.T) {
	dirs := isolateHome(t)
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs
	store := provider.VendorCatalogs{Vendors: map[string]provider.VendorCatalog{"codex": {Vendor: "codex", Models: []provider.DiscoveredModel{
		{ID: "new-top", Rank: 1, Status: provider.StatusListed},
		{ID: "new-selected", Rank: 2, Status: provider.StatusListed},
		{ID: "new-mid", Rank: 3, Status: provider.StatusListed},
		{ID: "new-small", Rank: 4, Status: provider.StatusListed},
	}}}}
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	ag := engine.New(engine.Options{Mode: engine.ModeAgent, Model: "new-selected", AgentRoster: a.agentRoster})
	a.reportAgentLane(ag)
	if !strings.Contains(out.String(), "/plans login can unlock new-small") {
		t.Fatalf("missing discovered login hint: %s", out)
	}
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "openai", Plan: "ChatGPT Pro", Name: "codex", LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	a.reportAgentLane(ag)
	for _, want := range []string{"new-selected → new-mid → new-small", "hard → selected · routine → next lower · trivial → lowest", "capped at new-selected — new-top out of reach"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	out.Reset()
	ag.SetSessionModel("unknown-model")
	a.reportAgentLane(ag)
	if !strings.Contains(out.String(), "no lower model is ranked and available") || strings.Contains(out.String(), "can unlock") {
		t.Fatalf("unknown model guidance: %s", out)
	}
}
