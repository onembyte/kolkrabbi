package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

func TestModelChoicesIncludeDiscoveredVersionsAndNewCodexModelsBeforeATurn(t *testing.T) {
	for _, loggedIn := range []bool{false, true} {
		t.Run(map[bool]string{false: "signed out", true: "signed in"}[loggedIn], func(t *testing.T) {
			dirs := isolateConnectorState(t)
			bin := t.TempDir()
			for _, vendor := range []string{"claude", "codex"} {
				if err := os.WriteFile(filepath.Join(bin, vendor), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)
			if loggedIn {
				signInAs(t, dirs, "anthropic", "Claude Max", "claude")
				signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
			}
			a, out, _ := newTestApp(t, "")
			a.dirs = dirs
			var store provider.VendorCatalogs
			store.Replace(provider.VendorCatalog{Vendor: "claude", Models: []provider.DiscoveredModel{
				{ID: "claude-opus", Status: provider.StatusUnverified},
				{ID: "claude-opus-5", Status: provider.StatusUnverified, Efforts: []string{"low", "high"}},
				{ID: "claude-opus-5-5", Status: provider.StatusUnverified, Efforts: []string{"low", "high"}},
				{ID: "claude-sonnet-5", Status: provider.StatusUnverified},
				{ID: "claude-sonnet-5-5", Status: provider.StatusUnverified},
			}})
			store.Replace(provider.VendorCatalog{Vendor: "codex", Models: []provider.DiscoveredModel{
				{ID: "gpt-6.1-sol", Display: "GPT-6.1-Sol", Status: provider.StatusListed, Efforts: []string{"low", "high", "xhigh"}, Rank: 1},
				{ID: "gpt-future", Status: provider.StatusListed, Rank: 2},
				{ID: "gpt-hidden", Status: provider.StatusListed, Hidden: true},
				{ID: "gpt-gone", Status: provider.StatusGone},
				{ID: "gpt-5.6-sol", Status: provider.StatusListed, Hidden: true},
			}})
			if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
				t.Fatal(err)
			}
			ag := engine.New(engine.Options{Model: "mock/model", Effort: "high", Sess: session.New(t.TempDir(), "mock/model")})
			entries := tuiModelPickEntries(context.Background(), a, ag)
			for _, spec := range tuiModels(context.Background(), a, ag) {
				if spec.ID == "gpt-6.1-sol" {
					want := tui.CostSubscriptionLogin
					if loggedIn {
						want = tui.CostSubscription
					}
					if spec.Cost != want {
						t.Errorf("discovered route cost = %s, want %s", spec.Cost, want)
					}
				}
			}
			for _, id := range []string{"claude-opus-5", "claude-opus-5-5", "claude-sonnet-5", "claude-sonnet-5-5", "gpt-6.1-sol", "gpt-future"} {
				count := 0
				for _, entry := range entries {
					if entry.ID != id {
						continue
					}
					count++
					if loggedIn && !strings.Contains(entry.Name, "via your") || !loggedIn && !strings.Contains(entry.Name, "sign in first") {
						t.Errorf("route label %s = %s", id, entry.Name)
					}
				}
				if count != 1 {
					t.Errorf("picker has %d rows for %s, want one", count, id)
				}
			}
			if err := a.printPlanModelChoices(); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"claude-opus-5-5", "gpt-6.1-sol", "gpt-future"} {
				if !strings.Contains(out.String(), "/model "+id) {
					t.Errorf("plain model choices omitted %s: %s", id, out.String())
				}
			}
			for _, entry := range entries {
				if strings.Contains(entry.ID, "gpt-hidden") || strings.Contains(entry.ID, "gpt-gone") || entry.ID == "gpt-5.6-pro" || entry.ID == "gpt-5.6-sol" {
					t.Errorf("hidden/gone/absent seed appears: %+v", entry)
				}
			}
			if loggedIn {
				manifest, _ := provider.LoadConnectors(dirs.ConnectorsFile())
				selected, err := a.resolvePlanModel("gpt-6.1-sol", manifest)
				if err != nil || selected.Plan != "ChatGPT Plus" || selected.Connector != "codex" {
					t.Fatalf("new model lost active login: %+v, %v", selected, err)
				}
				for _, id := range []string{"claude-opus-5", "claude-opus-5-5"} {
					selected, err := a.resolvePlanModel(id, manifest)
					if err != nil || selected.Model != id || selected.Plan != "Claude Max" || selected.Connector != "claude" {
						t.Fatalf("exact version lost active login: %+v, %v", selected, err)
					}
					roster := a.agentRoster(id, "claude")
					if len(roster.Rungs) != 1 || roster.Rungs[0].Model != id {
						t.Fatalf("unranked version invented a fallback ladder: %+v", roster)
					}
					args, err := agentcli.BuildClaudeSessionArgs(id, "code", "high", "", false)
					if err != nil || !strings.Contains(strings.Join(args, " "), "--model "+id+" ") {
						t.Fatalf("exact version changed at dispatch: %v, %v", args, err)
					}
				}
			}
			if strings.Contains(out.String(), `/plans login openai ""`) {
				t.Fatal("unknown discovered models invented an empty plan login")
			}
		})
	}
}
