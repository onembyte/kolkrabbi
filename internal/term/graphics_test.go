package term

import "testing"

func TestInlineImagesRespectTerminalAndMultiplexerBoundaries(t *testing.T) {
	for _, tc := range []struct{ terminal, program, tmux, want string }{
		{"xterm-kitty", "", "", "kitty"},
		{"xterm-256color", "ghostty", "", "kitty"},
		{"xterm-256color", "iTerm.app", "", "iterm"},
		{"xterm-256color", "WezTerm", "", "iterm"},
		{"xterm-256color", "Apple_Terminal", "", ""},
		{"xterm-kitty", "", "active", ""},
	} {
		get := func(key string) string {
			return map[string]string{"TERM": tc.terminal, "TERM_PROGRAM": tc.program, "TMUX": tc.tmux}[key]
		}
		if got := inlineImagesFor(get, true); got != tc.want {
			t.Fatalf("%+v = %q", tc, got)
		}
		if got := inlineImagesFor(get, false); got != "" {
			t.Fatal("disabled colour/output still allows images")
		}
	}
}

// Zellij is a multiplexer like tmux and screen: it sets ZELLIJ (to "0", which
// is still set), and an image escape sent through it without passthrough
// leaves two blank cells where the 🐙 fallback belonged.
func TestZellijUsesTheTextFallback(t *testing.T) {
	for _, env := range []map[string]string{
		{"TERM": "xterm-kitty", "ZELLIJ": "0"},
		{"TERM_PROGRAM": "ghostty", "ZELLIJ": "0"},
		{"TERM_PROGRAM": "iTerm.app", "ZELLIJ": "0"},
		{"TERM_PROGRAM": "WezTerm", "ZELLIJ": "0"},
	} {
		if got := inlineImagesFor(func(key string) string { return env[key] }, true); got != "" {
			t.Errorf("%v inside Zellij = %q, want the text fallback", env, got)
		}
	}
}
