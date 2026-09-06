package dash

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/stats"
)

// A model whose turns are metered by a vendor that does not price them in the
// reply is shown as metered and unpriced, never as $0.00 — a free-looking row
// for a paid key is the estimate-as-billing lie plan 24 forbids, inverted.
func TestAnUnpricedMeteredModelIsNotShownAsFree(t *testing.T) {
	records := []stats.Record{
		{Kind: "call", Time: day(1), Turn: "t1", Model: "grok-4.6", Effort: "medium", PromptTokens: 100, CompletionTokens: 20, Billing: "api-metered"},
		{Kind: "call", Time: day(1), Turn: "t2", Model: "gateway/model", Effort: "medium", Cost: 0.5, Billing: "gateway"},
		{Kind: "call", Time: day(1), Turn: "t3", Model: "claude-opus", Effort: "medium", Billing: "subscription"},
	}
	page := Page(records, 0, nil, nil)
	grok := page[strings.Index(page, "grok-4.6"):]
	grok = grok[:strings.Index(grok, "</tr>")]
	if strings.Contains(grok, "$0.00") || !strings.Contains(grok, "metered") {
		t.Fatalf("metered unpriced row = %q, want 'metered', not $0.00", grok)
	}
	opus := page[strings.Index(page, "claude-opus"):]
	opus = opus[:strings.Index(opus, "</tr>")]
	if strings.Contains(opus, "$0.00") || !strings.Contains(opus, "subscription") {
		t.Fatalf("subscription row = %q, want 'subscription', not $0.00", opus)
	}
	if !strings.Contains(page, "$0.50") {
		t.Fatal("the priced gateway row lost its cost")
	}
}
