package provider

import "testing"

// The bug this table exists for: `claude` with no arguments opens Claude Code,
// so asking kolk to sign in put a person inside another agent's whole interface
// with no way back. A login has to be a subcommand that signs in and exits.
func TestAConnectorLoginIsASubcommandNotTheBareCLI(t *testing.T) {
	for connector, want := range map[string][]string{
		"claude": {"auth", "login"},
		"codex":  {"login"},
		"ollama": {"signin"},
	} {
		got, known := LoginArgs(connector)
		if !known {
			t.Errorf("%s has no login subcommand, so it would open its whole CLI", connector)
			continue
		}
		if len(got) != len(want) {
			t.Errorf("%s login args = %v, want %v", connector, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s login args = %v, want %v", connector, got, want)
				break
			}
		}
	}
}

// An unverified connector falls back to the bare executable rather than to a
// guessed subcommand: the fallback runs the program the user named, while a
// wrong guess fails in a way that sounds like a broken subscription.
func TestAnUnknownConnectorReportsThatItIsUnknown(t *testing.T) {
	for _, connector := range []string{"gemini", "nothing-like-this"} {
		if args, known := LoginArgs(connector); known {
			t.Errorf("%s claims login args %v that nobody verified", connector, args)
		}
	}
}

// The table must not be editable through the accessor.
func TestLoginArgsCannotBeMutatedThroughTheAccessor(t *testing.T) {
	first, _ := LoginArgs("claude")
	first[0] = "clobbered"
	again, _ := LoginArgs("claude")
	if again[0] != "auth" {
		t.Errorf("the table was mutated through a returned slice: %v", again)
	}
}
