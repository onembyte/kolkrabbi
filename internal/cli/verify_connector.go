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
	inner engine.ChatBackend
	plan  provider.PlanModel
	// effort is the level the provider process was started with. It cannot
	// change without restarting that process, so the session has to be able to
	// say what the provider is actually running at.
	effort string
	// mode is the session mode the provider process was started with. It is
	// part of the vendor's spawn contract — chat runs with no tool in context,
	// code with the vendor's own tool loop — so a mode change restarts the
	// process exactly as an effort change does.
	mode string
	// note records provider-side state worth resuming — for Claude, the vendor
	// conversation handle — into the session file, so /model switches and later
	// Kolkrabbi runs land on the same conversation.
	note    func(string)
	confirm func(context.Context)
	explain func(error)
	// observe teaches the vendor catalog what this turn proved: the model
	// asked for answered on the vendor's resolved id, or was refused by name.
	// Every turn, not once — a session may switch models.
	observe   func(asked string, meta provider.Meta, err error)
	confirmed sync.Once
	explained sync.Once
}

// providerHandleBackend is what a backend that owns vendor state reports.
type providerHandleBackend interface {
	ProviderHandle() string
}

func (a *app) verifyingBackend(inner engine.ChatBackend, plan provider.PlanModel, mode, effort string, note func(string)) *verifyingBackend {
	if note == nil {
		note = func(string) {}
	}
	return &verifyingBackend{
		inner:  inner,
		plan:   plan,
		mode:   mode,
		effort: effort,
		note:   note,
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
		// The first failure names its actual error before offering the
		// sign-in hint. Shown without it, a parse failure inside the adapter
		// read as "you are not signed in" — to a user who was.
		explain: func(err error) {
			fmt.Fprintf(a.stderr, "%s has not answered successfully yet: %v\n", plan.Connector, err)
			fmt.Fprintf(a.stderr, "If it is not signed in, run this in another terminal:\n  /plans login %s %q\n", plan.Provider, plan.Plan)
		},
		observe: func(asked string, meta provider.Meta, err error) {
			a.recordVendorModelOutcome(plan.Connector, asked, meta, err)
		},
	}
}

func (b *verifyingBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	message, meta, err := b.inner.StreamChat(ctx, model, messages, tools, onToken)
	if b.observe != nil {
		b.observe(model, meta, err)
	}
	if err != nil {
		b.explained.Do(func() { b.explain(err) })
		return message, meta, err
	}
	b.confirmed.Do(func() { b.confirm(ctx) })
	// The handle exists before the first turn (kolk mints it), so it is noted
	// whether or not the vendor has confirmed it yet: a backend switched to
	// mid-session resumes the same conversation it left.
	if handleBackend, ok := b.inner.(providerHandleBackend); ok {
		if handle := handleBackend.ProviderHandle(); handle != "" {
			b.note(handle)
		}
	}
	return message, meta, nil
}

// ProviderHandle reports the vendor conversation the wrapped backend drives,
// so the session can pick it up on a /model switch without a turn in between.
func (b *verifyingBackend) ProviderHandle() string {
	if handleBackend, ok := b.inner.(providerHandleBackend); ok {
		return handleBackend.ProviderHandle()
	}
	return ""
}

// Close releases the provider this decorator wraps.
func (b *verifyingBackend) Close() error {
	if closer, ok := b.inner.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// noteClaudeBypassOnce names, once per session, what full-auto costs on a
// Claude child. The plan (docs/plan/04 §4.2) asks for this to be said rather
// than assumed: kolk's hardline blocklist, which survives full-auto on every
// other backend, does not exist inside the vendor's own process. The child's
// mode is fixed when it is spawned, so a later /ask does not reach it.
func (a *app) noteClaudeBypassOnce() {
	if a.claudeBypassNoted {
		return
	}
	a.claudeBypassNoted = true
	fmt.Fprintln(a.stderr, "full-auto on claude: the vendor child runs with bypassPermissions — kolk's hardline blocklist does not apply inside it, and the child keeps this mode until the session ends.")
}
