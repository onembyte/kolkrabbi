package cli

import (
	"context"
	"io"
	"sync"
	"time"
)

const (
	octopusGrace    = 120 * time.Millisecond
	octopusInterval = 120 * time.Millisecond
	cursorSave      = "\x1b[s"
	cursorRestore   = "\x1b[u"
	eraseToLineEnd  = "\x1b[K"
	brightMagenta   = "\x1b[95m"
	colorReset      = "\x1b[0m"
)

var octopusFrames = [...]string{
	"⠋ 🐙",
	"⠙ 🐙",
	"⠹ 🐙",
	"⠸ 🐙",
	"⠼ 🐙",
	"⠴ 🐙",
	"⠦ 🐙",
	"⠧ 🐙",
	"⠇ 🐙",
	"⠏ 🐙",
}

type animationTimer interface {
	C() <-chan time.Time
	Stop()
}

type animationClock interface {
	NewTimer(time.Duration) animationTimer
}

type realAnimationClock struct{}

func (realAnimationClock) NewTimer(delay time.Duration) animationTimer {
	return &realAnimationTimer{timer: time.NewTimer(delay)}
}

type realAnimationTimer struct {
	timer *time.Timer
}

func (t *realAnimationTimer) C() <-chan time.Time { return t.timer.C }
func (t *realAnimationTimer) Stop()               { t.timer.Stop() }

type octopusActivity struct {
	out      io.Writer
	clock    animationClock
	color    bool
	grace    time.Duration
	interval time.Duration
}

func newOctopusActivity(out io.Writer, color bool) *octopusActivity {
	return &octopusActivity{
		out: out, clock: realAnimationClock{}, color: color,
		grace: octopusGrace, interval: octopusInterval,
	}
}

func (a *octopusActivity) Start(ctx context.Context, phase string) func() {
	renderCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go a.render(renderCtx, phase, done)

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

func (a *octopusActivity) render(ctx context.Context, phase string, done chan<- struct{}) {
	defer close(done)
	rendered := false
	defer func() {
		if rendered {
			_, _ = io.WriteString(a.out, cursorRestore+eraseToLineEnd)
		}
	}()

	delay := a.grace
	frame := 0
	for {
		timer := a.clock.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
			timer.Stop()
		}

		prefix := cursorSave
		if rendered {
			prefix = cursorRestore + eraseToLineEnd
		}
		if _, err := io.WriteString(a.out, prefix+a.frame(frame, phase)); err != nil {
			return
		}
		rendered = true
		frame = (frame + 1) % len(octopusFrames)
		delay = a.interval
	}
}

func (a *octopusActivity) frame(index int, phase string) string {
	icon := octopusFrames[index]
	if a.color {
		icon = brightMagenta + icon + colorReset
	}
	return icon + " " + phase + "…"
}
