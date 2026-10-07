package engine

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type executionPauseKey struct{}

// executionPause stops admission at provider/tool boundaries. It does not
// cancel in-flight side effects: their results must settle before saving.
type executionPause struct {
	mu       sync.Mutex
	err      error
	recovery bool
}

func pauseGate(ctx context.Context) *executionPause {
	gate, _ := ctx.Value(executionPauseKey{}).(*executionPause)
	return gate
}

func (p *executionPause) stopped() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *executionPause) note(err error) bool {
	if p == nil {
		return false
	}
	limit, ok := provider.Classify(err)
	if !ok || !continuity.Pausable(limit) {
		return false
	}
	p.mu.Lock()
	if p.err == nil {
		p.err = limit
	}
	p.mu.Unlock()
	return true
}

type providerCallStartKey struct{}

// onProviderCallStart makes fn run when a provider call is about to be sent:
// after the pause gate admitted it, just before the request leaves. A call
// the gate stopped was never sent and is never journaled as in flight. Only
// the call whose context carries it runs it; a compaction made on the same
// task's behalf is not that task's turn.
func onProviderCallStart(ctx context.Context, fn func()) context.Context {
	return context.WithValue(ctx, providerCallStartKey{}, fn)
}

// providerCallStarting runs the hook of the call about to be sent, if any.
func providerCallStarting(ctx context.Context) {
	if fn, ok := ctx.Value(providerCallStartKey{}).(func()); ok && fn != nil {
		fn()
	}
}

// requestRecovery closes new-task admission immediately. Already admitted
// children drain to completion so their tool results and private transcripts
// can be frozen together, even when they recover from a tool error themselves.
func (p *executionPause) requestRecovery() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.recovery = true
	p.mu.Unlock()
}

func (p *executionPause) needsRecovery() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.recovery
}

func (p *executionPause) recovered() {
	p.mu.Lock()
	p.recovery = false
	p.mu.Unlock()
}

type providerCallEndKey struct{}

// Main state belongs to the backend that actually answered the attempt. Route
// changes and retry fallbacks may make it different from the session backend.
func (a *Agent) mainProviderCall(ctx context.Context) context.Context {
	ctx = onProviderCallStart(ctx, a.beginMainProviderCall)
	return context.WithValue(ctx, providerCallEndKey{}, func(backend ChatBackend, model string) {
		a.captureMainProviderState(backend)
		vendor, effort := a.connectorFor(model), provider.EffortFrom(ctx)
		a.executionMu.Lock()
		if a.execution != nil {
			a.execution.Main.Model, a.execution.Main.Vendor, a.execution.Main.Effort = model, vendor, effort
		}
		a.executionMu.Unlock()
	})
}

func providerCallEnded(ctx context.Context, backend ChatBackend, model string) {
	if fn, ok := ctx.Value(providerCallEndKey{}).(func(ChatBackend, string)); ok {
		fn(backend, model)
	}
}

type providerToolFailureKey struct{}

func watchProviderToolFailures(ctx context.Context) (context.Context, *atomic.Bool) {
	failed := &atomic.Bool{}
	return context.WithValue(ctx, providerToolFailureKey{}, func() { failed.Store(true) }), failed
}

func providerToolFailed(ctx context.Context) {
	if fn, ok := ctx.Value(providerToolFailureKey{}).(func()); ok {
		fn()
	}
}

// Vendor tools cannot be interrupted between progress callbacks. Save once the
// response is committed; a terminal provider error instead uses RunTurn's
// error/limit boundary. Child failures are drained by the shared admission gate.
func (a *Agent) saveProviderToolFailure(ctx context.Context, failed *atomic.Bool) error {
	if !failed.Swap(false) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.saveRecovery("error")
}
