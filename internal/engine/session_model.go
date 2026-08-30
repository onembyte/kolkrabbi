package engine

// Model and Backend are the only two Options fields that change after the Agent
// is built, and both change from a goroutine other than the one reading them.
//
// The metered fallback swaps them when a subscription runs out, and it runs
// inside streamChat — which in an orchestrated run means inside a subagent
// goroutine, while every other subagent is reading the model on its way to a
// spawn and backendFor is reading the backend on every routed call.
//
// Left unguarded that is a data race the detector catches, and in a release
// build it is a torn read: a model name that is neither the old one nor the new
// one reaching a vendor process as an argument. It would surface as a vendor
// error about an unknown model, which is a long way from where the bug is.
//
// Everything else on Options is written once at construction, before any
// goroutine exists, and is deliberately not covered here: a mutex around fields
// that never change would say they might.

// SessionModel is the model the session is running on right now.
func (a *Agent) SessionModel() string {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.Model
}

// SetSessionModel changes it. Exported because the surface layer switches
// models too — `/model`, and the plan-backend restarts — and must not write the
// field behind the lock's back.
func (a *Agent) SetSessionModel(model string) {
	a.modelMu.Lock()
	defer a.modelMu.Unlock()
	a.Model = model
}

// sessionBackend is where a routed call goes when no route claims it.
func (a *Agent) sessionBackend() ChatBackend { return a.SessionBackend() }

// SessionBackend is the provider the session is talking to right now.
// Exported for the same reason SetSessionBackend is: the surface reads it to
// ask what kind of provider is answering, and a turn may be in flight.
func (a *Agent) SessionBackend() ChatBackend {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.Backend
}

// SetSessionBackend swaps the provider the session talks to.
func (a *Agent) SetSessionBackend(backend ChatBackend) {
	a.modelMu.Lock()
	defer a.modelMu.Unlock()
	a.Backend = backend
}
