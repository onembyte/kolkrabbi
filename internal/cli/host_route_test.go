package cli

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/local"
)

// E5/E3b. A running Ollama is routed at startup (E5); an installed, idle one
// is routed to a lazy starter that brings it up on the first host-model turn
// (E3b). Absent is not routed: a route to nowhere is what E2's refusal exists
// to catch.
func TestARunningOrInstalledOllamaIsRoutedAndAnAbsentOneIsNot(t *testing.T) {
	for _, tc := range []struct {
		host   local.Host
		routed bool
	}{
		{local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434", Version: "0.33.1"}, true},
		{local.Host{State: local.HostInstalled, Binary: "/opt/ollama"}, true},
		{local.Host{State: local.HostAbsent}, false},
	} {
		storeFirstRunKey(t)
		a, _, _ := newTestApp(t, "")
		a.discoverHost = func(context.Context) local.Host { return tc.host }
		agent, err := a.newAgent(context.Background(), &options{})
		if err != nil {
			t.Fatal(err)
		}
		_, routed := agent.Routes["ollama"]
		if routed != tc.routed {
			t.Errorf("host %s: routed=%v, want %v", tc.host.State, routed, tc.routed)
		}
	}
}
