package provider

import (
	"context"
	"strings"
	"testing"
)

// V34.4c.1b.ii: every reply says how it is billed, from the client's origin —
// the gateway prices each call, a keyed vendor meters tokens it does not price
// in the reply, a compatible endpoint is unknown, the host is local. Plan 24:
// record the billing mode honestly; never label an estimate as the vendor's.
func TestEveryReplyNamesItsBillingModeFromTheOrigin(t *testing.T) {
	meta := func(c *Client) Meta {
		rec := &bodyRecorder{}
		if c.auth != nil {
			c.auth.Base = rec
		} else {
			c.HTTPClient.Transport = rec
		}
		_, m, _ := c.StreamChat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, nil)
		return m
	}
	xai, err := NewVendorClient("xai", "xai-"+strings.Repeat("0", 24))
	if err != nil {
		t.Fatal(err)
	}
	if got := meta(xai).Billing; got != BillingAPIMetered {
		t.Fatalf("vendor billing = %q, want %q", got, BillingAPIMetered)
	}
	if got := meta(newCanonicalOpenRouterClient(t, "sk-or-v1-"+strings.Repeat("0", 24))).Billing; got != BillingGateway {
		t.Fatalf("gateway billing = %q, want %q", got, BillingGateway)
	}
	if got := meta(NewCompatibleClient("http://compatible.invalid/v1")).Billing; got != BillingUnknown {
		t.Fatalf("compatible billing = %q, want %q", got, BillingUnknown)
	}
	if got := meta(NewHostClient("127.0.0.1:1")).Billing; got != BillingLocal {
		t.Fatalf("host billing = %q, want %q", got, BillingLocal)
	}
}
