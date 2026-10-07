package term

import "os"

// InlineImages identifies terminals that can draw the two-cell brand icon.
// Multiplexers (tmux, screen, Zellij) need separate passthrough negotiation;
// use the text fallback there, and whenever colour or terminal output was
// explicitly disabled.
func InlineImages() string {
	return inlineImagesFor(os.Getenv, Color() && CanAnimate())
}

func inlineImagesFor(getenv func(string) string, enabled bool) string {
	if !enabled || getenv("TMUX") != "" || getenv("STY") != "" || getenv("ZELLIJ") != "" {
		return ""
	}
	if getenv("TERM") == "xterm-kitty" {
		return "kitty"
	}
	switch getenv("TERM_PROGRAM") {
	case "ghostty":
		return "kitty"
	case "iTerm.app", "WezTerm":
		return "iterm"
	}
	return ""
}
