package engine

import (
	"context"
	"io"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type billingBackend struct{}

func (billingBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{Role: "assistant", Content: "ok"}, provider.Meta{Model: "grok-4.6", PromptTokens: 10, CompletionTokens: 5, Billing: provider.BillingAPIMetered}, nil
}

// The usage log keeps the billing mode beside the cost, so a metered turn the
// vendor did not price is never mistaken for a free one (V34.4c.1b.ii).
func TestTheUsageRecordCarriesTheBillingMode(t *testing.T) {
	rec := &fakeRecorder{}
	a := New(Options{Backend: billingBackend{}, Recorder: rec, Mode: ModeChat, Model: "grok-4.6", Effort: EffortMedium,
		Permission: PermissionFullAuto, Out: io.Discard, Sess: enginetest.NewFakeSession("s", "grok-4.6")})
	if err := a.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if len(rec.Calls) == 0 || rec.Calls[0].Billing != provider.BillingAPIMetered {
		t.Fatalf("recorded calls = %+v, want billing %q on the first", rec.Calls, provider.BillingAPIMetered)
	}
}

// The session remembers how its turns were billed, so the status line can say
// "metered" for a session whose vendor priced nothing rather than nothing.
func TestTheSessionRemembersItsBillingModes(t *testing.T) {
	a := New(Options{Backend: billingBackend{}, Mode: ModeChat, Model: "grok-4.6", Effort: EffortMedium,
		Permission: PermissionFullAuto, Out: io.Discard, Sess: enginetest.NewFakeSession("s", "grok-4.6")})
	if got := a.SessionBilling(); got != "" {
		t.Fatalf("billing before any turn = %q, want empty", got)
	}
	if err := a.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if got := a.SessionBilling(); got != provider.BillingAPIMetered {
		t.Fatalf("billing after a metered turn = %q, want %q", got, provider.BillingAPIMetered)
	}
}
