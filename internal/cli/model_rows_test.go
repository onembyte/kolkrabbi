package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func discoveredStore(fetched time.Time) provider.VendorCatalogs {
	var store provider.VendorCatalogs
	store.Replace(provider.VendorCatalog{
		Vendor: "codex", Source: "codex debug models", VendorVersion: "0.149.1", FetchedAt: fetched,
		Models: []provider.DiscoveredModel{
			{ID: "gpt-5.6-sol", Rank: 1, Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, Context: 272000, Status: provider.StatusListed},
			{ID: "gpt-5.4", Rank: 16, Hidden: true, Status: provider.StatusListed},
			{ID: "gpt-4.9", Rank: 40, Status: provider.StatusGone},
		},
	})
	store.Replace(provider.VendorCatalog{
		Vendor: "claude", Source: "gateway preview of anthropic/claude-*", VendorVersion: "2.1.258", FetchedAt: fetched,
		Models: []provider.DiscoveredModel{
			{ID: "claude-fable", Rank: 1, ExactIDs: []string{"anthropic/claude-fable-5"}, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, Context: 1000000, Status: provider.StatusUnverified},
			{ID: "claude-opus", Rank: 2, ExactIDs: []string{"anthropic/claude-opus-5"}, Context: 1000000, Status: provider.StatusVerified},
		},
	})
	return store
}

// A row says how kolk knows it: the two statuses a person acts on are named,
// and the two that are unremarkable are quiet.
func TestStatusNoteNamesOnlyWhatAPersonActsOn(t *testing.T) {
	for status, want := range map[provider.ModelStatus]string{
		provider.StatusListed:     "",
		provider.StatusVerified:   "",
		provider.StatusUnverified: "unverified",
		provider.StatusGone:       "gone",
	} {
		if got := statusNote(status); got != want {
			t.Errorf("statusNote(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestContextAndAgeReadTheWayAPersonWouldSayThem(t *testing.T) {
	for tokens, want := range map[int]string{0: "", 272000: "272K", 1000000: "1M", 1050000: "1.05M", 900: "900"} {
		if got := contextWindowLabel(tokens); got != want {
			t.Errorf("contextWindowLabel(%d) = %q, want %q", tokens, got, want)
		}
	}
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ at, want string }{
		{"", "never fetched"},
		{"2026-09-02T11:59:30Z", "fetched just now"},
		{"2026-09-02T11:30:00Z", "fetched 30m ago"},
		{"2026-09-02T06:00:00Z", "fetched 6h ago"},
		{"2026-08-30T12:00:00Z", "fetched 3d ago"},
	} {
		var at time.Time
		if tc.at != "" {
			at, _ = time.Parse(time.RFC3339, tc.at)
		}
		if got := ageLabel(at, now); got != tc.want {
			t.Errorf("ageLabel(%q) = %q, want %q", tc.at, got, tc.want)
		}
	}
}

// Every list of vendor models says where the rows came from and when. A
// catalog nobody can date is how a stale list passes for a current one.
func TestVendorFooterNamesTheSourceAndTheAge(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	vendorCatalogFooter(&out, discoveredStore(now.Add(-2*time.Hour)), now)
	got := out.String()
	for _, want := range []string{
		"claude 2.1.258: gateway preview of anthropic/claude-*, fetched 2h ago",
		"codex 0.149.1: codex debug models, fetched 2h ago",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("footer = %q, want %q", got, want)
		}
	}
	var empty bytes.Buffer
	vendorCatalogFooter(&empty, provider.VendorCatalogs{}, now)
	if empty.Len() != 0 {
		t.Fatalf("a store with no vendors printed %q", empty.String())
	}
}

// The vendor's own models are their own section, never mixed into the gateway
// rows: one is a subscription and the other is billed per token.
func TestModelsShowsEachVendorsOwnSection(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), discoveredStore(time.Now().Add(-90*time.Minute))); err != nil {
		t.Fatal(err)
	}

	a.printVendorModels("")
	got := out.String()
	for _, want := range []string{
		"subscription · claude 2.1.258 — gateway preview of anthropic/claude-*, fetched 1h ago",
		"claude-fable", "→ anthropic/claude-fable-5", "(unverified)",
		"ctx 1M", "efforts low,medium,high,xhigh,max",
		"subscription · codex 0.149.1 — codex debug models",
		"gpt-5.6-sol", "ctx 272K", "ultra",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("vendor section = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "gpt-5.4") {
		t.Fatalf("a hidden vendor row was listed: %q", got)
	}
	if strings.Contains(got, "gpt-4.9") {
		t.Fatalf("a gone vendor row was listed: %q", got)
	}
	if strings.Contains(got, "claude-opus (") {
		t.Fatalf("a verified row was decorated: %q", got)
	}

	out.Reset()
	a.printVendorModels("codex")
	if filtered := out.String(); !strings.Contains(filtered, "gpt-5.6-sol") || strings.Contains(filtered, "claude-fable") {
		t.Fatalf("filtered section = %q, want codex only", filtered)
	}

	// No discovery yet: nothing to say, and no empty heading either.
	empty := isolateConnectorState(t)
	b, bout, _ := newTestApp(t, "")
	b.dirs = empty
	b.printVendorModels("")
	if bout.Len() != 0 {
		t.Fatalf("with no vendor catalog, models printed %q", bout.String())
	}
}

// pmodels carries the vendor's answer: a status column, the vendor's efforts
// and context, and the provenance footer.
func TestPlanModelsCarriesStatusContextAndProvenance(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), discoveredStore(time.Now().Add(-3*time.Hour))); err != nil {
		t.Fatal(err)
	}

	if err := a.runPlanModels(t.Context(), []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"STATUS", "CTX",
		"gpt-5.6-sol", "listed", "low,medium,high,xhigh,max,ultra", "272K", "enabled",
		"gpt-5.6-pro", "gone",
		"codex 0.149.1: codex debug models, fetched 3h ago",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pmodels = %q, want %q", got, want)
		}
	}
}

// The compact /model list says which rows are not yet proved, and why a gone
// one is there at all.
func TestBareModelChoicesSayWhatIsUnverifiedAndWhatIsGone(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "anthropic", "Claude Max", "claude")
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), discoveredStore(time.Now())); err != nil {
		t.Fatal(err)
	}

	if err := a.printPlanModelChoices(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "claude-fable") || !strings.Contains(got, "unverified until a turn confirms it") {
		t.Fatalf("choices did not mark the previewed row: %q", got)
	}
	if !strings.Contains(got, "gone: the vendor no longer lists it") {
		t.Fatalf("choices did not say what happened to a retired row: %q", got)
	}
	if strings.Contains(got, "gpt-5.6-sol · ChatGPT Plus · enabled · ") {
		t.Fatalf("a listed row was decorated: %q", got)
	}
}

// A model the user has configured that the vendor has stopped listing is
// named, with what happened to it and where to look — never silently swapped
// for something else.
func TestAConfiguredModelTheVendorDroppedIsNamedNotSwapped(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "openai", "ChatGPT Pro", "codex")
	var store provider.VendorCatalogs
	store.Replace(provider.VendorCatalog{Vendor: "codex", Source: "codex debug models", VendorVersion: "0.149.1", Models: []provider.DiscoveredModel{
		{ID: "gpt-5.6-sol", Rank: 1, Status: provider.StatusListed},
	}})
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs

	_, err := a.newAgent(t.Context(), &options{model: "gpt-5.6-pro"})
	if err == nil {
		t.Fatal("a session started on a model the vendor no longer lists")
	}
	for _, want := range []string{"gpt-5.6-pro", "codex 0.149.1", "kolk models"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("startup refusal = %q, want %q", err, want)
		}
	}
}
