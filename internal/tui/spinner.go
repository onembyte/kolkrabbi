package tui

import "time"

const spinnerInterval = 120 * time.Millisecond

var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
