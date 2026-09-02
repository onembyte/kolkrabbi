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
			seedModelCatalog(t, a.dirs)
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

// `kolk help` is the front door: it says what Kolkrabbi is, which build and
// licence, both surfaces in full, and what it can do — because it is the one
// command someone runs before they know anything.
func TestHelpIsTheFrontDoor(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"help"}); code != ExitOK {
		t.Fatalf("kolk help exit = %d", code)
	}
	got := out.String()
	for _, want := range []string{
		"chat, code, and ordered agents", // what it is
		"Apache-2.0",                     // licence
		"version ",                       // build
		"Open a session",                 // the normal way in
		"kolk -r",                        // resume
		"Inside the session, everything is a /command", // where the commands are
		"Outside a session there are four commands and no more",
		"What it can do:",
		"three modes", "an effort dial", "any provider", "permission tiers",
		"local accounting", "checkpoints", "project memory",
		"OPENROUTER_API_KEY", "KOLK_CONFIG_DIR",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("kolk help does not mention %q", want)
		}
	}
	// Both registries in full, so nothing is discoverable only by reading source.
	for _, sc := range slashCommandTable {
		if !strings.Contains(got, "/"+sc.name) {
			t.Errorf("kolk help omits the in-session command /%s", sc.name)
		}
	}
	for _, c := range commandTable() {
		if !strings.Contains(got, "kolk "+c.name) {
			t.Errorf("kolk help omits the outside-session command %q", c.name)
		}
	}
	// And it never advertises a verb that was retired.
	for _, gone := range []string{"kolk config", "kolk key", "kolk stats", "kolk completion"} {
		if strings.Contains(got, gone) {
			t.Errorf("kolk help still advertises %q", gone)
		}
	}
}
