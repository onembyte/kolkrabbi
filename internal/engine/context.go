package engine

import (
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// compactAtFraction is how full the window may get before Kolkrabbi compacts.
// The remaining quarter has to hold the reply and at least one tool round;
// compacting at 95% produces a session that compacts and then fails anyway.
const compactAtFraction = 0.75

// charsPerToken is the estimate used only before a provider has reported a
// real count. It is deliberately coarse and never overrides a measurement.
const charsPerToken = 4

// ContextUsage is how much of a model's context window a session is using.
//
// Measured says where Used came from. A provider's own prompt-token count is
// what it actually read; an estimate is a floor for the one moment before any
// turn has been answered, and the two must never be confused when deciding to
// throw conversation away.
type ContextUsage struct {
	Window   int // 0 when the model's window is unknown
	Used     int
	Measured bool
}

// MeasureContext reports window usage, preferring the provider's own count of
// the last request over any estimate of the messages.
func MeasureContext(window, lastPromptTokens int, messages []provider.Message) ContextUsage {
	usage := ContextUsage{Window: window}
	if lastPromptTokens > 0 {
		usage.Used, usage.Measured = lastPromptTokens, true
		return usage
	}
	characters := 0
	for _, message := range messages {
		characters += len(message.Content)
	}
	usage.Used = characters / charsPerToken
	return usage
}

// ShouldCompact reports whether the next request is projected past the
// threshold. An unknown window never compacts: Kolkrabbi does not invent a
// limit it was not told, and throwing away conversation on a guess is worse
// than a provider error the user can read.
func (u ContextUsage) ShouldCompact() bool {
	if u.Window <= 0 {
		return false
	}
	return float64(u.Used) >= float64(u.Window)*compactAtFraction
}

// Fraction is how full the window is, capped at 1. A stale catalog can make a
// provider report more than the window it advertises, and showing 140% would
// read as a bug in Kolkrabbi rather than in the catalog.
func (u ContextUsage) Fraction() float64 {
	if u.Window <= 0 {
		return 0
	}
	fraction := float64(u.Used) / float64(u.Window)
	if fraction > 1 {
		return 1
	}
	return fraction
}

// Label renders usage for the per-turn footer, or nothing when there is no
// window to compare against.
func (u ContextUsage) Label() string {
	if u.Window <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%s ctx", shortCount(u.Used), shortCount(u.Window))
}

// shortCount renders a token count the way a person reads one: a window of
// exactly 128000 is "128k", not "128.0k", so the decimal appears only when it
// carries information.
func shortCount(n int) string {
	switch {
	case n >= 1_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1_000_000)) + "M"
	case n >= 1_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1_000)) + "k"
	default:
		return fmt.Sprintf("%d", n)
	}
}

func trimZero(value string) string {
	return strings.TrimSuffix(value, ".0")
}
