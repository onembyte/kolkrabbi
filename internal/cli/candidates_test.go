package cli

import (
	"context"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/stats"
)

// V35.3b: the surface assembles what could continue the work — every model
// row of every verified connector, billed as the plan's turn, with the model
// a routing word resolved to and the person's own rating — and never a
// connector that is enabled but has not answered yet.
func TestCandidatesComeFromVerifiedConnectorsWithRatingsAndExactIds(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	signInAs(t, dirs, "github", "Copilot Free", "copilot")
	markVerified(t, dirs, "codex")
	markVerified(t, dirs, "copilot")
	signInAs(t, dirs, "anthropic", "Claude Max", "claude") // enabled, never answered
	var store provider.VendorCatalogs
	store.Verify("copilot", "auto", "gpt-5.6-luna", now())
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	if err := stats.Append(dirs.Data, stats.Record{Kind: "call", Turn: "t1", Model: "gpt-5.6-sol"}); err != nil {
		t.Fatal(err)
	}
	if err := stats.Append(dirs.Data, stats.Record{Kind: "rating", Turn: "t1", Rating: 4}); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	got := map[string]struct {
		plan, billing, exact string
		rating               float64
	}{}
	for _, c := range a.continuityCandidates(context.Background()) {
		got[c.Connector+"/"+c.Model] = struct {
			plan, billing, exact string
			rating               float64
		}{c.Plan, c.Billing, c.Exact, c.Rating}
	}
	sol, ok := got["codex/gpt-5.6-sol"]
	if !ok || sol.plan != "ChatGPT Plus" || sol.billing != "subscription" || sol.rating != 4 {
		t.Fatalf("sol = %+v, %v; want the Plus row, subscription, rated 4", sol, ok)
	}
	auto, ok := got["copilot/auto"]
	if !ok || auto.exact != "gpt-5.6-luna" {
		t.Fatalf("copilot auto = %+v, %v; want the model auto chose as exact", auto, ok)
	}
	if _, ok := got["claude/claude-fable"]; ok {
		t.Fatal("a connector that never answered was offered as a continuation")
	}
}

func markVerified(t *testing.T, dirs paths.Dirs, connector string) {
	t.Helper()
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range manifest.Connectors {
		if c.Name != connector {
			continue
		}
		c.Verified = true
		if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), c); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("no connector %q to verify", connector)
}

func now() time.Time { return time.Now() }
