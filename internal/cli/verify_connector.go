package cli

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// verifyingBackend confirms a subscription connector the first time its
// provider actually answers. A provider CLI that exits cleanly has proved
// nothing — quitting a login without signing in exits cleanly too — so the
// proof is a turn the user wanted anyway, which costs nothing extra.
//
// A failed turn deliberately does not demote the connector. Deciding "that
// error means you are not signed in" requires matching provider error text,
// and a false positive would disable a connector that works. Kolkrabbi says
// what it suspects instead, once, and leaves the state alone.
type verifyingBackend struct {
	inner     engine.ChatBackend
	plan      provider.PlanModel
	confirm   func(context.Context)
	explain   func()
	confirmed sync.Once
	explained sync.Once
}

func (a *app) verifyingBackend(inner engine.ChatBackend, plan provider.PlanModel) *verifyingBackend {
	return &verifyingBackend{
		inner: inner,
		plan:  plan,
		confirm: func(ctx context.Context) {
			dirs, err := a.resolve()
			if err != nil {
				return
			}
			// Best effort: a session that works must never fail because a note
			// about it could not be written.
			_ = provider.SaveConnector(ctx, dirs.ConnectorsFile(), provider.Connector{
				Provider: plan.Provider, Plan: plan.Plan, Name: plan.Connector,
				LoginOwner: "provider-cli", Enabled: true, Verified: true,
			})
		},
		explain: func() {
			fmt.Fprintf(a.stderr, "%s has not answered successfully yet. If it is not signed in, run this in another terminal:\n", plan.Connector)
			fmt.Fprintf(a.stderr, "  kolk plans login %s %q\n", plan.Provider, plan.Plan)
		},
	}
}

func (b *verifyingBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	message, meta, err := b.inner.StreamChat(ctx, model, messages, tools, onToken)
	if err != nil {
		b.explained.Do(b.explain)
		return message, meta, err
	}
	b.confirmed.Do(func() { b.confirm(ctx) })
	return message, meta, nil
}

// Close releases the provider this decorator wraps.
func (b *verifyingBackend) Close() error {
	if closer, ok := b.inner.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
