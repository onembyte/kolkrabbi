package agentcli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestClaudePreviewExposesEachVersionWithoutChangingFamilyAliases(t *testing.T) {
	gateway := []provider.ModelInfo{
		{ID: "anthropic/claude-opus-5", Name: "Claude Opus 5", ContextLength: 200000},
		{ID: "anthropic/claude-opus-5.5", Name: "Claude Opus 5.5", ContextLength: 1000000},
		{ID: "anthropic/claude-sonnet-5", Name: "Claude Sonnet 5", ContextLength: 1000000},
		{ID: "anthropic/claude-sonnet-5.5", Name: "Claude Sonnet 5.5", ContextLength: 1000000},
		{ID: "anthropic/claude-opus-5.5:batch"},
		{ID: "anthropic/claude-opus-5.5-fast"},
		{ID: "openai/gpt-next"},
	}
	catalog, err := (ClaudePreviewLister{Gateway: gateway}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, display, gateway string
		context              int
	}{
		{"claude-opus-5", "Claude Opus 5", "anthropic/claude-opus-5", 200000},
		{"claude-opus-5-5", "Claude Opus 5.5", "anthropic/claude-opus-5.5", 1000000},
		{"claude-sonnet-5", "Claude Sonnet 5", "anthropic/claude-sonnet-5", 1000000},
		{"claude-sonnet-5-5", "Claude Sonnet 5.5", "anthropic/claude-sonnet-5.5", 1000000},
	} {
		row, ok := catalog.Find(tc.id)
		if !ok || row.Display != tc.display || row.Context != tc.context || row.Status != provider.StatusUnverified || row.Rank != 0 || strings.Join(row.ExactIDs, ",") != tc.gateway {
			t.Fatalf("version %s = %+v, found=%v; want its own unverified native row", tc.id, row, ok)
		}
		if ClaudeVendorModel(row.ID) != tc.id {
			t.Fatalf("exact selection %q became a latest-family alias", row.ID)
		}
	}
	alias, ok := catalog.Find("claude-opus")
	if !ok || alias.Rank != 2 || alias.ExactIDs[0] != "anthropic/claude-opus-5.5" || ClaudeVendorModel(alias.ID) != "opus" {
		t.Fatalf("latest alias changed: %+v", alias)
	}
	if len(catalog.Models) != 6 {
		t.Fatalf("variants or foreign models leaked: %+v", catalog.Models)
	}
}

func TestClaudePinnedVersionsAreDiscoveredNotSeededAndDeduplicated(t *testing.T) {
	catalog, err := (ClaudePreviewLister{Gateway: []provider.ModelInfo{
		{ID: "anthropic/claude-opus-12.9", ContextLength: 900000},
		{ID: "anthropic/claude-opus-12.9", ContextLength: 900000},
		{ID: "anthropic/claude-opus-12", ContextLength: 300000},
	}}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 3 {
		t.Fatalf("duplicate pinned row or version whitelist: %+v", catalog.Models)
	}
	row, ok := catalog.Find("claude-opus-12-9")
	if !ok || row.Display != row.ID || row.Rank != 0 || row.Context != 900000 {
		t.Fatalf("future discovered version = %+v, found=%v", row, ok)
	}
	older, ok := catalog.Find("claude-opus-12")
	if !ok || older.Context != 300000 {
		t.Fatalf("older pinned version inherited family's maximum context: %+v", older)
	}
}
