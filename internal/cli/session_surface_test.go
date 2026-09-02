package cli

import (
	"context"
	"strings"
	"testing"
)

// The main usage of every command is inside a running session. This test is
// the guarantee: every command the registry advertises is reached by the
// dispatcher and answers, rather than falling through to "unknown command".
//
// The arguments are chosen to prove reachability without side effects — a
// command that would install a release, bind a port, or hand the terminal to a
// vendor CLI is given the argument shape that makes it answer and stop. What is
// proved is dispatch and response, which is the half that rots when a command
// is added to the table and not to the switch.
func TestEverySessionCommandIsReachable(t *testing.T) {
	// Argument shapes that keep a command from acting on the world.
	benign := map[string]string{
		"update": "xyz",              // refuses with usage; never reaches the network
		"dash":   "--addr nonsense",  // refuses the address; never binds a port
		"plans":  "no-such-plan-xyz", // matches nothing; no login handover
		"plogin": "no-such-plan-xyz",
		"rate":   "5",
		"key":    "", // bare prints usage; a value would write one
	}
	// Commands that end the session by contract.
	exits := map[string]bool{"exit": true, "quit": true}

	for _, command := range slashCommandTable {
		t.Run(command.name, func(t *testing.T) {
			a, ag, out := replFixture(t, "")
			seedModelCatalog(t, isolateHome(t))
			exited := a.slash(context.Background(), ag, "/"+command.name+" "+benign[command.name])
			if got := out.String(); strings.Contains(got, "unknown command") {
				t.Fatalf("/%s is in the registry but the dispatcher does not handle it: %q", command.name, got)
			}
			if exits[command.name] != exited {
				t.Fatalf("/%s exited = %v, want %v", command.name, exited, exits[command.name])
			}
		})
	}
}

// The other direction: nothing the dispatcher handles is missing from the
// registry, or it exists and cannot be discovered through /help or completion.
func TestEveryHandledCommandIsAdvertised(t *testing.T) {
	advertised := map[string]bool{}
	for _, command := range slashCommandTable {
		advertised[command.name] = true
	}
	// The aliases the switch accepts beside their canonical name. Listing them
	// here rather than in the registry keeps /help short; they are still
	// dispatched, and this test is what says so.
	aliases := map[string]string{"permission": "permissions"}
	for alias, canonical := range aliases {
		if !advertised[canonical] {
			t.Errorf("/%s is an alias for /%s, which is not advertised", alias, canonical)
		}
		a, ag, out := replFixture(t, "")
		if a.slash(context.Background(), ag, "/"+alias); strings.Contains(out.String(), "unknown command") {
			t.Errorf("/%s is documented as an alias but is not dispatched", alias)
		}
	}
}
