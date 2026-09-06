package agentcli

import (
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// A handover's reply is subscription usage, whatever the vendor's frame says
// about cost: kolk never turns a plan's turn into a dollar figure.
func TestAHandoverReplyIsBilledAsSubscription(t *testing.T) {
	_, meta, err := Collect([]Event{{Kind: EventMessageDelta, Text: "ok"}, {Kind: EventMessageCompleted}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Billing != provider.BillingSubscription {
		t.Fatalf("handover billing = %q, want %q", meta.Billing, provider.BillingSubscription)
	}
}
