package cli

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// The status line states the sandbox explicitly -- "off" is a word, not an
// absence -- because the one rule that survived the switch to opt-in is that
// the state is always visible where the user is already looking.
func TestStatusLineAlwaysStatesTheSandbox(t *testing.T) {
	isolateConnectorState(t)
	_, ag, _ := replFixture(t, "")

	if got := tuiStatus(ag, "ready", "~").Sandbox; got != "off" {
		t.Fatalf("Sandbox = %q with no policy, want the word off", got)
	}
	ag.SetSandbox(&shell.Sandbox{Root: t.TempDir(), Temp: t.TempDir(), Network: shell.NetworkAllow})
	got := tuiStatus(ag, "ready", "~").Sandbox
	if name, err := shell.Mechanism(); err == nil {
		if got != name {
			t.Fatalf("Sandbox = %q with a policy, want the mechanism %q", got, name)
		}
	} else if got != "on, unenforced" {
		t.Fatalf("Sandbox = %q with a policy and no mechanism; want %q", got, "on, unenforced")
	}
}
