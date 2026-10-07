package cli

import (
	"context"
	"github.com/onembyte/kolkrabbi/internal/buildinfo"
	"strings"
	"testing"
	"unicode/utf8"
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
		buildinfo.Get().String(),         // build identity, the line installers and release checks read
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

// A retired verb must never become a prompt.
//
// Dispatch turns an unknown first word into a prompt, which is right for
// `kolk fix the failing test` and ruinous for `kolk version`: the word becomes
// a turn and the turn reaches a model. That is not hypothetical — after the
// verbs were removed, `make budgets` (which times cold start by running
// `kolk version` twenty times) sent seventy-four turns to a real subscription.
// Every retired name is refused for free, with the spelling that works.
func TestARetiredVerbIsRefusedNotSentToAModel(t *testing.T) {
	for name, slash := range retiredVerbs {
		t.Run(name, func(t *testing.T) {
			a, out, errOut := newTestApp(t, "")
			// A model call would need this; if one is attempted the test fails
			// on the assertions below rather than reaching the network.
			code := a.main(context.Background(), []string{name})
			if code == ExitOK {
				t.Fatalf("`kolk %s` succeeded; it must be refused, not run", name)
			}
			said := out.String() + errOut.String()
			if !strings.Contains(said, "is a session command now") || !strings.Contains(said, slash) {
				t.Fatalf("`kolk %s` did not say where it went:\n%s", name, said)
			}
			if !strings.Contains(said, "quote it") {
				t.Fatalf("`kolk %s` did not say how to send it as a prompt anyway:\n%s", name, said)
			}
		})
	}

	// The design this protects is intact: a genuinely unknown word, and a
	// quoted sentence that happens to start with a retired name, are prompts.
	if _, retired := retiredVerbs["fix"]; retired {
		t.Fatal("an ordinary word is being treated as a retired verb")
	}
	if _, retired := retiredVerbs["config the model"]; retired {
		t.Fatal("a quoted sentence matched a retired verb; it must be one whole argument")
	}
}

// Adopted from the V43.5 §7 item 5 review (H1): kolk help aligned every
// summary to the longest usage, and /localia's grammar is about 170 columns
// wide, so every summary sat off the right edge of an ordinary terminal. On
// both surfaces a summary starts within reach, and a usage too long for its
// column gets a row of its own.
func TestEverySlashSummaryStartsWithinReach(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"help"}); code != ExitOK {
		t.Fatalf("kolk help exit = %d", code)
	}
	var slash strings.Builder
	printSlashHelp(&slash)
	for surface, text := range map[string]string{"kolk help": out.String(), "/help": slash.String()} {
		lines := strings.Split(text, "\n")
		for _, command := range slashCommandTable {
			found := false
			for _, line := range lines {
				at := strings.Index(line, command.summary)
				if at < 0 {
					continue
				}
				found = true
				if column := utf8.RuneCountInString(line[:at]); column > 45 {
					t.Errorf("%s: /%s's summary starts at column %d", surface, command.name, column)
				}
				// A usage that fits keeps its summary beside it.
				if usage := "/" + command.name + " " + command.args; len(usage) < 40 && !strings.Contains(line, "/"+command.name) {
					t.Errorf("%s: /%s's summary is not on its row", surface, command.name)
				}
			}
			if !found {
				t.Errorf("%s: /%s's summary is missing", surface, command.name)
			}
		}
	}
}

// Adopted from the V43.5 item 5 verification (I5-4): kolk help lists /config
// and /model, but `kolk help config` answered "no such command". Help for a
// session command, with or without its slash or by its old verb, gives its
// usage and says where it runs; help for a sessions verb names kolk sessions.
func TestHelpFindsSessionCommandsAndSessionsVerbs(t *testing.T) {
	for name, want := range map[string]string{
		"config":  "usage: /config",
		"/config": "usage: /config",
		"model":   "usage: /model",
		"models":  "usage: /model",
		"resume":  "usage: /resume",
		"export":  "usage: kolk sessions",
		"fork":    "usage: kolk sessions",
	} {
		a, out, errOut := newTestApp(t, "")
		if code := a.main(context.Background(), []string{"help", name}); code != ExitOK {
			t.Errorf("kolk help %s exit = %d: %s", name, code, errOut.String())
			continue
		}
		if !strings.Contains(out.String(), want) {
			t.Errorf("kolk help %s:\n%s\nwant %q", name, out.String(), want)
		}
	}
	a, _, errOut := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"help", "nosuch"}); code == ExitOK || !strings.Contains(errOut.String(), "kolk help") {
		t.Errorf("kolk help nosuch = %d, %q; want an error that points to kolk help", code, errOut.String())
	}
}

// Adopted from the same verification (nits): an unknown config key names
// where the keys are, and /help mentions export where /session is listed.
func TestUnknownConfigKeysAndExportPointSomewhere(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	for _, key := range []string{"nosuch", "local.nosuch"} {
		err := a.runConfig(context.Background(), []string{"set", key, "1"})
		if err == nil || !strings.Contains(err.Error(), "/config lists every setting") {
			t.Errorf("/config set %s: %v", key, err)
		}
	}
	var help strings.Builder
	printSlashHelp(&help)
	if !strings.Contains(help.String(), "export") {
		t.Errorf("/help never mentions export:\n%s", help.String())
	}
}

// Adopted from the V43.5 item 5 re-check (J3): where a name is both an outside
// verb and a session command, or both a session command and a sessions verb,
// help says which is which rather than answering for only one.
func TestHelpSaysWhichOfTwoSameNamedCommandsItMeans(t *testing.T) {
	for name, wants := range map[string][]string{
		"/help": {"usage: /help"},
		"clear": {"/clear", "kolk sessions"},
	} {
		a, out, _ := newTestApp(t, "")
		if code := a.main(context.Background(), []string{"help", name}); code != ExitOK {
			t.Errorf("kolk help %s exit = %d", name, code)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(out.String(), want) {
				t.Errorf("kolk help %s:\n%s\nwant %q", name, out.String(), want)
			}
		}
	}
}
