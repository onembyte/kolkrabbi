package cli

import "testing"

// A line is a command only when its first word looks like one. A pasted
// absolute path begins with a slash and is not a command, and treating it as
// one threw the whole message away — which is what happened to the owner on
// 2026-09-09, whose request began with a screenshot's path.
func TestOnlyACommandWordIsACommand(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"/model gpt-5.6-luna", true},
		{"/help", true},
		{"/localia add shop 10.0.0.2:11434", true},
		{"/mdoel", true}, // a typo is still a command, and still says so
		{"/var/folders/7w/T/Screenshot 2026-09-09 at 01.24.45.png", false},
		{"/Users/francomichetti/Desktop/Screenshot.png review this", false},
		{"/etc/hosts", false},
		{"/", false},
		{"//", false},
		{"look at ./thing", false},
		{"", false},
		{"/model\nand a second line", false}, // no command spans lines
		{"/var/x.png\nreview the TUI", false},
	} {
		if got := looksLikeSlashCommand(tc.input); got != tc.want {
			t.Errorf("looksLikeSlashCommand(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
