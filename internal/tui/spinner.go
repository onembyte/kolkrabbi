package tui

import "time"

const spinnerInterval = 120 * time.Millisecond

// octopusFrames are tiny terminal pixel-art sprites. Each frame is kept
// monochrome here; the TUI applies its purple activity style at render time.
var spinnerFrames = [...]string{
	"●●\n▙▟",
	"●●\n▟▙",
	"●●\n▙▟",
	"●●\n▟▙",
}

type spinnerTimer interface {
	C() <-chan time.Time
	Stop()
}

type spinnerClock interface {
	NewTimer(time.Duration) spinnerTimer
}

type realSpinnerClock struct{}

func (realSpinnerClock) NewTimer(delay time.Duration) spinnerTimer {
	return &realSpinnerTimer{timer: time.NewTimer(delay)}
}

type realSpinnerTimer struct{ timer *time.Timer }

func (t *realSpinnerTimer) C() <-chan time.Time { return t.timer.C }
func (t *realSpinnerTimer) Stop()               { t.timer.Stop() }
