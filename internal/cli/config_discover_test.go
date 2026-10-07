package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
)

// Adopted from the V43.5 §7 item 5 review (CF1, CF2): /help offers `/config
// show` for reading the settings, and it printed three of them; and the local
// runtime's tuning keys were listed only once someone had set them. Both
// views now list every setting there is, set or not.
func TestEveryConfigViewListsEverySetting(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	ctx := context.Background()
	if err := a.runConfig(ctx, []string{"set-tier", "quick", "google/gemini-2.5-flash"}); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, row := range (&config.Config{}).Settings("m", "b") {
		keys = append(keys, row.Key)
	}
	keys = append(keys, config.LocalKeys...)
	for _, view := range [][]string{{"show"}, {}} {
		out.Reset()
		if err := a.runConfig(ctx, view); err != nil {
			t.Fatalf("/config %v: %v", view, err)
		}
		for _, key := range keys {
			if !strings.Contains(out.String(), key) {
				t.Errorf("/config %v does not list %s:\n%s", view, key, out.String())
			}
		}
		if !strings.Contains(out.String(), "google/gemini-2.5-flash") {
			t.Errorf("/config %v lost the saved tier:\n%s", view, out.String())
		}
	}
}

// With no tier set, show says how to add one in the grammar the table uses,
// and following it works.
func TestConfigShowSaysHowToAddAnEffortTier(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	ctx := context.Background()
	if err := a.runConfig(ctx, []string{"show"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "/config set effort.<level> <model>") {
		t.Fatalf("show without tiers does not say how to add one:\n%s", out.String())
	}
	if err := a.runConfig(ctx, []string{"set", "effort.high", "vendor/deep-model"}); err != nil {
		t.Fatalf("the hinted command failed: %v", err)
	}
	out.Reset()
	if err := a.runConfig(ctx, []string{"show"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "effort.high") || !strings.Contains(out.String(), "vendor/deep-model") {
		t.Fatalf("show does not list the tier the hint added:\n%s", out.String())
	}
}
