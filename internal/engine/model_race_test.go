package engine

import (
	"sync"
	"testing"
)

// The metered fallback swaps the session's model and backend from a SUBAGENT
// goroutine, while the ceiling and the fast lane read the model from every
// other subagent goroutine at the same time. Run under -race this is the proof;
// without the detector it is a wrong model name in a spawn argument, which
// looks like a vendor error rather than a data race.
func TestTheSessionModelIsSafeToReadWhileItChanges(t *testing.T) {
	agent := &Agent{Options: Options{Model: "claude-sonnet"}}

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Readers: what every in-flight subagent does on its way to a spawn.
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 200 {
				_ = agent.underCeiling("claude-opus")
				_ = agent.FastLaneModel()
			}
		}()
	}
	// Writer: what the metered fallback does when a plan limit lands.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range 200 {
			agent.SetSessionModel("openrouter/free")
			agent.SetSessionModel("claude-sonnet")
		}
	}()

	close(start)
	wg.Wait()
}

// The same for the backend, which backendFor reads on every routed call while
// moveToMetered swaps it.
func TestTheSessionBackendIsSafeToReadWhileItChanges(t *testing.T) {
	agent := &Agent{Options: Options{Model: "claude-sonnet"}}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 200 {
				_ = agent.sessionBackend()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range 200 {
			agent.SetSessionBackend(nil)
		}
	}()
	close(start)
	wg.Wait()
}

// The metered fallback is the real writer: it swaps the model AND the backend
// when a subscription runs out, from inside streamChat — which in an
// orchestrated run is a subagent goroutine, while every other subagent is
// reading the model on its way to a spawn.
func TestAMeteredSwitchDuringAWideRunIsRaceFree(t *testing.T) {
	agent := &Agent{Options: Options{Model: "claude-sonnet"}}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 100 {
				_ = agent.modelForKind(KindEdit)
				_, _, _ = agent.backendFor(agent.SessionModel())
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range 100 {
			agent.moveToMetered("openrouter/free")
		}
	}()
	close(start)
	wg.Wait()

	if got := agent.SessionModel(); got != "openrouter/free" {
		t.Errorf("session model = %q after the switch, want the metered one", got)
	}
}

// Switching from the surface has to move what the ceiling holds to, or a user
// who picks a stronger model would still be capped at the old one.
func TestSwitchingModelsChangesWhatTheCeilingHoldsTo(t *testing.T) {
	agent := &Agent{Options: Options{Model: "claude-haiku"}}
	if got := agent.underCeiling("claude-opus"); got != "claude-haiku" {
		t.Fatalf("under a haiku ceiling, opus resolved to %q", got)
	}
	agent.SetSessionModel("claude-opus")
	if got := agent.underCeiling("claude-opus"); got != "claude-opus" {
		t.Errorf("after switching to opus, opus resolved to %q", got)
	}
	// And the new ceiling still refuses what is above it.
	if got := agent.underCeiling("claude-fable"); got != "claude-opus" {
		t.Errorf("fable escaped the new opus ceiling: %q", got)
	}
}
