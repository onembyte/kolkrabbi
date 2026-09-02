package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVendorCatalogStoreRoundTripsAndStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vendor-models.json")
	store, err := LoadVendorCatalogs(path)
	if err != nil || len(store.Vendors) != 0 {
		t.Fatalf("missing file = %+v, %v; want an empty store", store, err)
	}
	store.Replace(VendorCatalog{Vendor: "codex", Source: "codex debug models", Models: []DiscoveredModel{{ID: "gpt-5.6-sol", Rank: 1, Status: StatusListed}}})
	if err := SaveVendorCatalogs(path, store); err != nil {
		t.Fatal(err)
	}
	again, err := LoadVendorCatalogs(path)
	if err != nil {
		t.Fatal(err)
	}
	if sol, ok := again.Vendors["codex"].Find("gpt-5.6-sol"); !ok || sol.Rank != 1 || sol.Status != StatusListed {
		t.Fatalf("reloaded = %+v", again.Vendors["codex"])
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadVendorCatalogs(path); err == nil {
		t.Fatal("a corrupt catalog file loaded as empty; it would forget every verification")
	}
}

// A turn is the proof. Verify promotes the row the turn asked for and records
// the exact id the vendor answered on; a row nobody listed is created; a
// refusal by name marks a listed row gone and says nothing about unknown names.
func TestATurnPromotesAndARefusalRetires(t *testing.T) {
	at := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	var store VendorCatalogs
	store.Replace(VendorCatalog{Vendor: "claude", Source: "gateway preview", Models: []DiscoveredModel{
		{ID: "claude-fable", ExactIDs: []string{"anthropic/claude-fable-5"}, Status: StatusUnverified},
		{ID: "claude-haiku", Status: StatusUnverified},
	}})

	store.Verify("claude", "claude-fable", "claude-fable-5-20260801", at)
	fable, _ := store.Vendors["claude"].Find("claude-fable")
	if fable.Status != StatusVerified || strings.Join(fable.ExactIDs, ",") != "claude-fable-5-20260801,anthropic/claude-fable-5" {
		t.Fatalf("after a turn: %+v, want verified with the vendor's resolved id first", fable)
	}
	if haiku, _ := store.Vendors["claude"].Find("claude-haiku"); haiku.Status != StatusUnverified {
		t.Fatalf("a turn on fable changed haiku: %+v", haiku)
	}

	store.Verify("codex", "gpt-5.6-luna", "", at)
	if luna, ok := store.Vendors["codex"].Find("gpt-5.6-luna"); !ok || luna.Status != StatusVerified || !store.Vendors["codex"].FetchedAt.Equal(at) {
		t.Fatalf("a turn on a vendor never previewed left no record: %+v", store.Vendors["codex"])
	}

	if !store.Gone("claude", "claude-haiku") {
		t.Fatal("a refusal of a listed name was not recorded")
	}
	if haiku, _ := store.Vendors["claude"].Find("claude-haiku"); haiku.Status != StatusGone {
		t.Fatalf("refused row = %+v, want gone", haiku)
	}
	if store.Gone("claude", "claude-nope") || store.Gone("gemini", "x") {
		t.Fatal("a refusal of a name nobody listed was recorded as catalog information")
	}
}

// A new discovery replaces a vendor's rows but never un-proves a turn: a row
// the listing still names keeps verified and its exact ids; a row the listing
// dropped is kept as gone so its user is told, not left wondering.
func TestReplaceCarriesVerificationForwardAndRetiresTheDropped(t *testing.T) {
	var store VendorCatalogs
	store.Replace(VendorCatalog{Vendor: "codex", Models: []DiscoveredModel{
		{ID: "gpt-5.6-sol", Status: StatusListed}, {ID: "gpt-5.6-pro", Status: StatusListed},
	}})
	store.Verify("codex", "gpt-5.6-sol", "gpt-5.6-sol", time.Now())
	store.Verify("codex", "gpt-5.6-pro", "", time.Now())

	store.Replace(VendorCatalog{Vendor: "codex", Models: []DiscoveredModel{
		{ID: "gpt-5.6-sol", Rank: 1, Status: StatusListed}, {ID: "gpt-5.7", Rank: 0, Status: StatusListed},
	}})
	sol, _ := store.Vendors["codex"].Find("gpt-5.6-sol")
	if sol.Status != StatusVerified || sol.Rank != 1 || strings.Join(sol.ExactIDs, ",") != "gpt-5.6-sol" {
		t.Fatalf("sol after re-discovery = %+v, want still verified with the new rank", sol)
	}
	pro, ok := store.Vendors["codex"].Find("gpt-5.6-pro")
	if !ok || pro.Status != StatusGone {
		t.Fatalf("a model the vendor stopped listing = %+v, %v; want kept as gone", pro, ok)
	}
	if new, ok := store.Vendors["codex"].Find("gpt-5.7"); !ok || new.Status != StatusListed {
		t.Fatalf("new row = %+v", new)
	}
	var visible []string
	for _, model := range store.Vendors["codex"].Visible() {
		visible = append(visible, model.ID)
	}
	if got := strings.Join(visible, ","); got != "gpt-5.6-sol,gpt-5.7" {
		t.Fatalf("visible = %s, want the gone row out of sight", got)
	}
}
