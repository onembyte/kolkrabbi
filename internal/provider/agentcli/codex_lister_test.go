package agentcli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func codexCatalogFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/codex_debug_models_2026-09-02.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The vendor's catalog, as captured on 2026-09-02, is what kolk knows — not
// the rungs kolk wrote down on 08-30. Eight models; the flagship first; the
// hidden ones hidden, not missing; `ultra` arrives without a code change; and
// `gpt-5.6-pro`, which kolk's own table names, is not there.
func TestCodexCatalogIsWhatTheVendorListsNotWhatKolkWroteDown(t *testing.T) {
	at := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	catalog, err := ParseCodexModelCatalog(codexCatalogFixture(t), "0.149.1", at)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Vendor != "codex" || catalog.VendorVersion != "0.149.1" || !catalog.FetchedAt.Equal(at) || catalog.Source != "codex debug models" {
		t.Fatalf("catalog header = %+v", catalog)
	}
	if len(catalog.Models) != 8 {
		t.Fatalf("models = %d, want the vendor's eight", len(catalog.Models))
	}

	sol, ok := catalog.Find("gpt-5.6-sol")
	if !ok || sol.Rank != 1 || sol.Hidden || sol.Status != provider.StatusListed || sol.Context != 272000 || sol.DefaultEffort != "low" || sol.Display != "GPT-5.6-Sol" {
		t.Fatalf("sol = %+v", sol)
	}
	if got := strings.Join(sol.Efforts, ","); got != "low,medium,high,xhigh,max,ultra" {
		t.Fatalf("sol efforts = %s, want the vendor's six including ultra", got)
	}
	if hidden, ok := catalog.Find("gpt-5.4"); !ok || !hidden.Hidden {
		t.Fatalf("gpt-5.4 = %+v, want present and hidden", hidden)
	}
	if _, ok := catalog.Find("gpt-5.6-pro"); ok {
		t.Fatal("gpt-5.6-pro is in the parsed catalog; the vendor does not list it")
	}

	var visible []string
	for _, model := range catalog.Visible() {
		visible = append(visible, model.ID)
	}
	if got := strings.Join(visible, ","); got != "gpt-5.6-sol,gpt-5.6-terra,gpt-5.6-luna,gpt-5.5,gpt-5.2" {
		t.Fatalf("visible = %s, want the vendor's listed models in its priority order", got)
	}
}

// A vendor that adds a field must not break discovery; a vendor that answers
// with something else must not be mistaken for "nothing offered".
func TestCodexCatalogToleratesNewFieldsAndRefusesTheWrongShape(t *testing.T) {
	extended := []byte(`{"models":[{"slug":"gpt-7","display_name":"GPT-7","visibility":"list","priority":1,"context_window":1,"supported_reasoning_levels":[{"effort":"low","description":"x","brand_new_field":true}],"another_new_field":{"deep":1}}],"catalog_version":9}`)
	catalog, err := ParseCodexModelCatalog(extended, "9.9.9", time.Now())
	if err != nil {
		t.Fatalf("a catalog with new fields was refused: %v", err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].ID != "gpt-7" {
		t.Fatalf("catalog = %+v", catalog.Models)
	}
	for name, raw := range map[string][]byte{
		"not json":   []byte("codex: error: not logged in"),
		"empty list": []byte(`{"models":[]}`),
		"no slugs":   []byte(`{"models":[{"display_name":"x"}]}`),
	} {
		if _, err := ParseCodexModelCatalog(raw, "", time.Now()); err == nil {
			t.Errorf("%s was accepted as a catalog", name)
		}
	}
}

// Discover runs the two vendor commands and records the version; a vendor
// that will not run is an error with the reason, not an empty success.
func TestCodexListerRunsTheVendorAndRecordsItsVersion(t *testing.T) {
	var asked [][]string
	lister := CodexLister{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			asked = append(asked, args)
			if args[0] == "--version" {
				return []byte("codex-cli 0.149.1\n"), nil
			}
			return codexCatalogFixture(t), nil
		},
		Now: func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
	}
	catalog, err := lister.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if catalog.VendorVersion != "0.149.1" || len(catalog.Models) != 8 {
		t.Fatalf("catalog = version %q, %d models", catalog.VendorVersion, len(catalog.Models))
	}
	if len(asked) != 2 || strings.Join(asked[1], " ") != "debug models" {
		t.Fatalf("vendor commands = %v, want --version then debug models", asked)
	}

	missing := CodexLister{Run: func(context.Context, ...string) ([]byte, error) {
		return nil, errors.New(`exec: "codex": executable file not found in $PATH`)
	}}
	if _, err := missing.Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("a missing vendor CLI = %v, want the reason surfaced", err)
	}
}

var _ provider.ModelLister = CodexLister{}

// The real vendor, when asked for. Gated because it runs the installed CLI;
// `KOLK_LIVE_VENDOR=1 go test ./internal/provider/agentcli -run Live -v` is
// how an owner checks that today's codex still answers `debug models`.
func TestLiveCodexCatalogAnswers(t *testing.T) {
	if os.Getenv("KOLK_LIVE_VENDOR") == "" {
		t.Skip("set KOLK_LIVE_VENDOR=1 to ask the installed codex")
	}
	catalog, err := CodexLister{}.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("codex %s listed %d models via %q; visible: %d", catalog.VendorVersion, len(catalog.Models), catalog.Source, len(catalog.Visible()))
	for _, model := range catalog.Visible() {
		t.Logf("  %-18s rank=%d efforts=%v ctx=%d", model.ID, model.Rank, model.Efforts, model.Context)
	}
	if len(catalog.Models) == 0 || catalog.VendorVersion == "" {
		t.Fatalf("live catalog = %+v", catalog)
	}
}
