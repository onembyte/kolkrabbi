package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// ResumeManual keeps a paused session paused until /resume; anything else is
// auto, the default: the monitor brings the turn back when the limit lifts.
const ResumeManual = "manual"

// resumeMonitor is the one goroutine a paused session may have. It waits for
// the reset, confirms the limit lifted without spending a token, and hands the
// pending turn back; it dies with the agent or with the pause it watches.
type resumeMonitor struct {
	stop context.CancelFunc
	done chan struct{}
}

// WatchPauses gives the agent the context its resume monitors live in: the
// surface's session, so a session that ends takes its monitor with it. It arms
// a monitor for the current pause, if any, and every pause recorded later arms
// its own. Without this call a lifted pause is only announced.
func (a *Agent) WatchPauses(ctx context.Context) {
	a.resumeMu.Lock()
	a.resumeBase = ctx
	a.resumeMu.Unlock()
	a.armResume()
}

// armResume starts the monitor for the session's current pause. It reports
// false when there is nothing to watch: no pause, a manual policy, no session
// context yet, or a monitor already running.
func (a *Agent) armResume() bool {
	if a.Sess == nil || a.ResumePolicy == ResumeManual {
		return false
	}
	pause := a.Sess.Paused()
	if pause == nil {
		return false
	}
	a.resumeMu.Lock()
	defer a.resumeMu.Unlock()
	if a.resume != nil || a.resumeBase == nil {
		return false
	}
	ctx, cancel := context.WithCancel(a.resumeBase)
	monitor := &resumeMonitor{stop: cancel, done: make(chan struct{})}
	a.resume = monitor
	go func() {
		defer close(monitor.done)
		a.watchPause(ctx, *pause)
		a.resumeMu.Lock()
		if a.resume == monitor {
			a.resume = nil
		}
		a.resumeMu.Unlock()
	}()
	return true
}

// stopResumeMonitor cancels the monitor and waits for it to leave, so a closed
// agent never hands a turn to a surface that is gone.
func (a *Agent) stopResumeMonitor() {
	a.resumeMu.Lock()
	monitor := a.resume
	a.resume = nil
	a.resumeMu.Unlock()
	if monitor == nil {
		return
	}
	monitor.stop()
	<-monitor.done
}

// Resume is /resume: it lifts the pause now, whatever the clock says, and
// returns the turn that was waiting for the surface to run. False when the
// session is not paused. The monitor, if any, is dismissed first so the turn
// runs once.
func (a *Agent) Resume() (string, bool) {
	if a.Sess == nil {
		return "", false
	}
	a.stopResumeMonitor()
	pause := a.Sess.Paused()
	if pause == nil {
		return "", false
	}
	a.Sess.SetPaused(nil)
	a.save()
	a.publishLimit(pause.Limit(), "resume")
	return pause.PendingTurn, true
}

// watchPause is the monitor's loop: wait for the reset, probe, and either hand
// the turn back or move the pause to the next reset and wait again. Every
// wait goes through ResumeWait so the loop is cancellable and testable.
func (a *Agent) watchPause(ctx context.Context, pause continuity.Pause) {
	for {
		delay := time.Until(pause.ResetAt)
		if delay < 0 {
			delay = 0
		}
		if err := a.ResumeWait(ctx, delay); err != nil {
			return
		}
		lifted, err := a.probeLifted(ctx, pause)
		if ctx.Err() != nil {
			return
		}
		if lifted && err == nil {
			// The pause may have been lifted by /resume while the probe ran;
			// only the pause this monitor was armed for is ours to clear.
			current := a.Sess.Paused()
			if current == nil || !current.Since.Equal(pause.Since) {
				return
			}
			a.Sess.SetPaused(nil)
			a.save()
			a.publishLimit(pause.Limit(), "resume")
			what := pause.Model
			if what == "" {
				what = pause.Connector
			}
			if a.ResumeReady != nil {
				fmt.Fprintf(a.Out, "◆ %s is back; re-sending the turn that was waiting\n", what)
				a.ResumeReady(pause.PendingTurn)
			} else if pause.PendingTurn != "" {
				fmt.Fprintf(a.Out, "◆ %s is back; the turn that was waiting (%q) is ready to send\n", what, compactToolText(pause.PendingTurn))
			}
			return
		}
		next := time.Now().Add(provider.LimitKind(pause.Kind).DefaultCooldown())
		if next.Before(time.Now().Add(30 * time.Second)) {
			next = time.Now().Add(30 * time.Second)
		}
		pause.ResetAt = next
		if current := a.Sess.Paused(); current == nil || !current.Since.Equal(pause.Since) {
			return
		}
		a.Sess.SetPaused(&pause)
		a.save()
		reason := "still capped"
		if err != nil {
			reason = "could not be checked (" + err.Error() + ")"
		}
		fmt.Fprintf(a.Out, "◆ %s %s; next check at %s\n", pause.HumanKind(), reason, pause.Resumes())
	}
}

// probeLifted asks whether the limit is gone without spending tokens. A keyed
// gateway answers through its key status, a compatible endpoint through
// /models, a handover through its sign-in check; where nothing can be asked
// the reset clock is trusted, which is what the wait already did.
func (a *Agent) probeLifted(ctx context.Context, pause continuity.Pause) (bool, error) {
	if a.ProbeLimit != nil {
		return a.ProbeLimit(ctx, pause)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if pause.Connector != "" && a.connectorFor(pause.Model) == pause.Connector && a.HandoverSignedIn != nil && a.ConnectorName != nil {
		return a.HandoverSignedIn(pause.Connector), nil
	}
	if a.Client == nil {
		return true, nil
	}
	if a.Client.HasKey() && provider.IsOpenRouterEndpoint(a.Client.BaseURL) {
		status, err := provider.OpenRouterVerifier{}.Verify(probeCtx, a.Client.Key())
		if err != nil {
			return false, err
		}
		if pause.Kind == string(provider.LimitAccountQuota) && status.RemainingUSD != nil && *status.RemainingUSD <= 0 {
			return false, nil
		}
		return true, nil
	}
	if _, err := a.Client.ListModels(probeCtx); err != nil {
		return false, err
	}
	return true, nil
}

// NormalizeResume validates a continuity.resume value: auto (the default) or
// manual.
func NormalizeResume(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto", nil
	case ResumeManual:
		return ResumeManual, nil
	}
	return "", fmt.Errorf("%q is not auto or manual", value)
}
