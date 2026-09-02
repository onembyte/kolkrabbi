package provider

import (
	"context"
	"strings"
	"testing"
	"time"
)

func gatewayFixture() []ModelInfo {
	mk := func(id, name string, ctx int) ModelInfo {
		m := ModelInfo{ID: id, Name: name, ContextLength: ctx}
		return m
	}
	return []ModelInfo{
		mk("anthropic/claude-fable-5", "Anthropic: Claude Fable 5", 1000000),
		mk("anthropic/claude-fable-5:batch", "Anthropic: Claude Fable 5 (batch)", 1000000),
		mk("anthropic/claude-haiku-4.5", "Anthropic: Claude Haiku 4.5", 200000),
		mk("openai/gpt-5.6-sol", "OpenAI: GPT-5.6 Sol", 1050000),
		mk("x-ai/grok-5", "xAI: Grok 5", 256000),
	}
}

// The preview is the vendor's exact ids as the gateway publishes them, marked
// unverified, with the vendor CLI's effort set and no guess about anything the
// gateway does not know.
func TestGatewayPreviewListsExactIDsAsUnverified(t *testing.T) {
	at := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	lister := GatewayPreviewLister{
		Vendor: "claude", Prefix: "anthropic", Efforts: []string{"low", "medium", "high", "xhigh", "max"},
		Gateway: gatewayFixture(), Version: "2.1.258", Now: func() time.Time { return at },
	}
	catalog, err := lister.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Vendor != "claude" || catalog.VendorVersion != "2.1.258" || !catalog.FetchedAt.Equal(at) {
		t.Fatalf("catalog header = %+v", catalog)
	}
	if !strings.Contains(catalog.Source, "gateway preview of anthropic/") {
		t.Fatalf("source = %q", catalog.Source)
	}
	var ids []string
	for _, model := range catalog.Models {
		ids = append(ids, model.ID)
		if model.Status != StatusUnverified {
			t.Errorf("%s status = %q, want unverified until a turn confirms it", model.ID, model.Status)
		}
		if strings.Join(model.Efforts, ",") != "low,medium,high,xhigh,max" {
			t.Errorf("%s efforts = %v, want the vendor CLI's set", model.ID, model.Efforts)
		}
		if model.Rank != 0 {
			t.Errorf("%s rank = %d, want unranked (the gateway does not know)", model.ID, model.Rank)
		}
	}
	if got := strings.Join(ids, ","); got != "anthropic/claude-fable-5,anthropic/claude-haiku-4.5" {
		t.Fatalf("preview ids = %s, want the exact ids, no :batch variants, no other vendor", got)
	}
	if fable, ok := catalog.Find("ANTHROPIC/claude-fable-5"); !ok || fable.Context != 1000000 {
		t.Fatalf("Find(fable) = %+v, %v", fable, ok)
	}
}

func TestGatewayPreviewFailsLoudlyWithNothingToPreviewFrom(t *testing.T) {
	if _, err := (GatewayPreviewLister{Vendor: "claude", Prefix: "anthropic"}).Discover(context.Background()); err == nil {
		t.Fatal("an empty gateway catalog produced a preview")
	}
	if _, err := (GatewayPreviewLister{Vendor: "copilot", Prefix: "github", Gateway: gatewayFixture()}).Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "lists nothing under github/") {
		t.Fatalf("a prefix the gateway does not carry = %v, want the reason named", err)
	}
	if _, err := (GatewayPreviewLister{Vendor: "x", Gateway: gatewayFixture()}).Discover(context.Background()); err == nil {
		t.Fatal("a preview without a prefix was accepted")
	}
}

func TestNotListableIsAnAnswerNotAnOmission(t *testing.T) {
	_, err := (NotListable{Vendor: "copilot", Reason: "no catalog command and no gateway prefix"}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "copilot") || !strings.Contains(err.Error(), "no catalog command") {
		t.Fatalf("NotListable error = %v, want the vendor and the reason", err)
	}
}

// What a person sees: hidden and gone rows are out, ranked rows come first
// strongest first, unranked rows after in the order the vendor gave them.
func TestVisibleOrdersByRankAndDropsHiddenAndGone(t *testing.T) {
	catalog := VendorCatalog{Models: []DiscoveredModel{
		{ID: "unranked-a", Status: StatusUnverified},
		{ID: "third", Rank: 3, Status: StatusListed},
		{ID: "hidden", Rank: 2, Hidden: true, Status: StatusListed},
		{ID: "first", Rank: 1, Status: StatusListed},
		{ID: "gone", Rank: 1, Status: StatusGone},
		{ID: "unranked-b", Status: StatusUnverified},
	}}
	var ids []string
	for _, model := range catalog.Visible() {
		ids = append(ids, model.ID)
	}
	if got := strings.Join(ids, ","); got != "first,third,unranked-a,unranked-b" {
		t.Fatalf("visible = %s", got)
	}
}
