package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/xid"
)

// ResumeManual keeps a paused session paused until /resume; anything else is
// auto, the default: the monitor brings the turn back when the limit lifts.
const ResumeManual = "manual"

// resumeMonitor is the one goroutine a paused session may have. It waits for
// the reset, confirms the limit lifted without spending a token, and hands the
// pending turn back; it dies with the agent or with the pause it watches.
type resumeMonitor struct {
	stop      context.CancelFunc
	done      chan struct{}
	discarded bool // protected by resumeMu; a revoked delivery cannot restore its pause
}

// WatchPauses gives the agent the context its resume monitors live in: the
// surface's session, so a session that ends takes its monitor with it. It arms
// a monitor for the current pause, if any, and every pause recorded later arms
// its own. The returned cleanup cancels and joins resume delivery before a
// surface releases its resources. Without this call the pause remains available
// to /resume. Cleanup and Close must run outside a ResumeReady callback.
func (a *Agent) WatchPauses(ctx context.Context) func() {
	a.stopResumeMonitor()
	// Delivery may have been accepted by a surface just before it closed.
	// The journal owns the request until RunTurn actually finishes it.
	if a.Sess != nil && a.Sess.Paused() == nil {
		if run := a.Sess.RunState(); run != nil && run.Phase != "done" && run.Phase != "stopped" && run.LastPause != nil {
			a.Sess.SetPaused(run.LastPause)
			a.saveFor(savePause)
		}
	}
	a.resumeMu.Lock()
	if a.resumeClosed {
		a.resumeMu.Unlock()
		return a.closeResume
	}
	if a.resumeStop != nil {
		a.resumeStop()
	}
	a.resumeParent = ctx
	a.resumeBase, a.resumeStop = context.WithCancel(ctx)
	a.resumeMu.Unlock()
	a.armResume()
	return a.closeResume
}

// armResume starts the monitor for the session's current pause. It reports
// false when there is nothing to watch: no pause, a manual policy, no session
// context yet, or a monitor already running.
func (a *Agent) armResume() bool {
	if a.Sess == nil || a.ResumePolicy == ResumeManual || a.ResumeReady == nil {
		return false
	}
	a.resumeMu.Lock()
	defer a.resumeMu.Unlock()
	if a.resumeClosed || a.resume != nil || a.resumeBase == nil || a.resumeBase.Err() != nil {
		return false
	}
	pause := a.Sess.Paused()
	if pause == nil {
		return false
	}
	ctx, cancel := context.WithCancel(a.resumeBase)
	monitor := &resumeMonitor{stop: cancel, done: make(chan struct{})}
	a.resume = monitor
	// Under resumeMu, with the generation checked live above: QuiesceResume
	// ends a generation under the same lock, so no goroutine is added once it
	// has begun waiting.
	a.resumeRunning.Add(1)
	go func() {
		defer a.resumeRunning.Done()
		defer cancel()
		ready := a.watchPause(ctx, *pause)
		a.resumeMu.Lock()
		owned := a.resume == monitor
		if owned {
			a.resume = nil
		}
		current := a.Sess.Paused()
		deliver := owned && ready != nil && !a.resumeClosed && ctx.Err() == nil &&
			a.ResumeReady != nil && current != nil && current.Since.Equal(ready.Since)
		// The delivered turn runs in a context marked with the claim it was
		// delivered under, so it can tell when it finally runs whether that
		// claim is still its own.
		deliveryCtx := ctx
		if deliver {
			a.ensureResumeJournal(*ready)
			a.Sess.SetPaused(nil)
			if run := a.Sess.RunState(); run != nil {
				a.claimRunLocked(run.ID)
				deliveryCtx = context.WithValue(ctx, deliveryTicketKey{}, a.resumeClaims)
			}
			// Close marks resumeClosed under this lock before waiting, so no
			// delivery can be added after shutdown starts joining callbacks.
			a.resumeDeliveries.Add(1)
			if a.resumeDelivering == nil {
				a.resumeDelivering = make(map[*resumeMonitor]context.CancelFunc)
			}
			a.resumeDelivering[monitor] = cancel
		}
		// A callback can call Resume/ContinueOn without joining itself.
		// Close separately joins the complete delivery lifetime below.
		close(monitor.done)
		a.resumeMu.Unlock()
		retryDelivery := false
		if deliver {
			func() {
				defer a.resumeDeliveries.Done()
				defer func() {
					a.resumeMu.Lock()
					delete(a.resumeDelivering, monitor)
					a.resumeMu.Unlock()
				}()
				a.saveFor(saveResume)
				a.publishLimit(ready.Limit(), "resume")
				what := ready.Model
				if what == "" {
					what = ready.Connector
				}
				if strings.TrimSpace(ready.PendingTurn) == "" {
					fmt.Fprintf(a.Out, "◆ %s is back; pause lifted\n", what)
					return
				}
				fmt.Fprintf(a.Out, "◆ %s is back; continuing the turn that was waiting\n", what)
				if !a.ResumeReady(deliveryCtx, ready.PendingTurn) {
					// A closing surface can decline delivery. Keep the input
					// available, but never replace a newer pause from a turn.
					a.resumeMu.Lock()
					run := a.Sess.RunState()
					discarded := run != nil && run.Phase == "stopped" && run.Input == ready.PendingTurn
					if a.Sess.Paused() == nil && !discarded && !monitor.discarded {
						a.resumeClaim = ""
						retryDelivery = !a.resumeClosed && ctx.Err() == nil
						if retryDelivery {
							// A busy surface can decline without disabling auto
							// resume. Back off instead of spinning on a past reset.
							ready.ResetAt, ready.Estimated = time.Now().Add(30*time.Second), true
						}
						a.Sess.SetPaused(ready)
					}
					a.resumeMu.Unlock()
					a.saveFor(savePause)
					if retryDelivery {
						fmt.Fprintln(a.Out, "◆ waiting for the session to accept the turn; retrying in 30s")
					}
				}
			}()
		}
		// A synchronous or very fast asynchronous callback may have paused
		// again. A stale probe may also have met a newer pause. Never rearm
		// an accepted pause or a cancelled lifecycle.
		if ctx.Err() == nil {
			if next := a.Sess.Paused(); next != nil && (retryDelivery || !next.Since.Equal(pause.Since)) {
				a.armResume()
			}
		}
	}()
	return true
}

// closeResume retires the watcher and cancels/joins all delivered callbacks.
// Watcher completion and callback completion are distinct: a callback may
// invoke Resume itself, but Close must still wait for it to leave.
func (a *Agent) closeResume() {
	a.resumeMu.Lock()
	a.resumeClosed = true
	if a.resumeStop != nil {
		a.resumeStop()
	}
	a.resumeMu.Unlock()
	a.stopResumeMonitor()
	a.resumeDeliveries.Wait()
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

// deliveryTicketKey marks the context a delivered turn runs in with the claim
// count it was delivered under.
type deliveryTicketKey struct{}

// deliveryStale reports that ctx carries a delivery whose claim is no longer
// its own: while the delivery waited for the surface, another turn continued
// the run and gave the claim back, or the run was claimed again since.
func (a *Agent) deliveryStale(ctx context.Context) bool {
	ticket, ok := ctx.Value(deliveryTicketKey{}).(uint64)
	if !ok {
		return false
	}
	a.resumeMu.Lock()
	defer a.resumeMu.Unlock()
	return a.resumeClaim == "" || a.resumeClaims != ticket
}

// claimRunLocked hands run id to one turn. The count tells that turn's
// release apart from a later claim on the same run: a delivery armed inside a
// turn can claim its run again before that turn ends. Called under resumeMu.
func (a *Agent) claimRunLocked(id string) {
	a.resumeClaim = id
	a.resumeClaims++
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
	a.resumeMu.Lock()
	defer a.resumeMu.Unlock()
	pause := a.Sess.Paused()
	if pause == nil {
		if run := a.Sess.RunState(); run != nil && (run.LastPause != nil || run.Recovery != "") && run.ID != a.resumeClaim && run.Phase != "done" && run.Phase != "stopped" {
			a.claimRunLocked(run.ID)
			return run.Input, true
		}
		return "", false
	}
	a.ensureResumeJournal(*pause)
	if run := a.Sess.RunState(); run != nil {
		a.claimRunLocked(run.ID)
	}
	a.Sess.SetPaused(nil)
	a.saveFor(saveResume)
	a.publishLimit(pause.Limit(), "resume")
	return pause.PendingTurn, true
}

// Older sessions have only pending input. Make even that delivery durable
// before lifting its pause, without inventing a task graph that was not saved.
func (a *Agent) ensureResumeJournal(pause continuity.Pause) {
	if strings.TrimSpace(pause.PendingTurn) == "" {
		return
	}
	if run := a.Sess.RunState(); run != nil && run.Phase != "done" && run.Phase != "stopped" {
		return
	}
	a.Sess.SetRunState(&continuity.Run{Version: 1, ID: xid.New(xid.Turn), Input: pause.PendingTurn,
		Prompt: pause.PendingTurn, Root: a.Root, Mode: a.Mode, Model: a.SessionModel(), Effort: a.Effort,
		Phase: "new", LastPause: &pause, Spend: continuity.Spend{Limit: a.MaxRunCostUSD}})
}

// DiscardPending abandons an idle unfinished request, retaining its history,
// files and worktrees. Surfaces serialize this with turns and slash mutations.
// A delivery waiting on the surface is cancelled, never joined under its lock.
func (a *Agent) DiscardPending() bool {
	if a.Sess == nil || a.turnDepth.Load() != 0 {
		return false
	}
	a.stopResumeMonitor()
	a.resumeMu.Lock()
	defer a.resumeMu.Unlock()
	run, pause := a.Sess.RunState(), a.Sess.Paused()
	if (run == nil || run.Phase == "done" || run.Phase == "stopped") && pause == nil {
		return false
	}
	for monitor, cancel := range a.resumeDelivering {
		monitor.discarded = true
		cancel()
	}
	if run == nil || run.Phase == "done" || run.Phase == "stopped" {
		run = &continuity.Run{Version: 1, ID: xid.New(xid.Turn), Input: pause.PendingTurn}
	}
	run.Phase = "stopped"
	a.Sess.SetRunState(run)
	a.Sess.SetPaused(nil)
	a.claimRunLocked(run.ID)
	a.saveFor(saveResume)
	return true
}

// watchPause is the monitor's loop: wait for the reset, probe, and either hand
// the turn back or move the pause to the next reset and wait again. Every
// wait goes through ResumeWait so the loop is cancellable and testable.
func (a *Agent) watchPause(ctx context.Context, pause continuity.Pause) *continuity.Pause {
	if a.ResumeReady == nil {
		return nil
	}
	for {
		delay := time.Until(pause.ResetAt)
		if delay < 0 {
			delay = 0
		}
		if err := a.ResumeWait(ctx, delay); err != nil {
			return nil
		}
		lifted, err := a.probeLifted(ctx, pause)
		if ctx.Err() != nil {
			return nil
		}
		if lifted && err == nil {
			// The pause may have been lifted by /resume while the probe ran;
			// only the pause this monitor was armed for is ours to clear.
			current := a.Sess.Paused()
			if current == nil || !current.Since.Equal(pause.Since) {
				return nil
			}
			return &pause
		}
		next := time.Now().Add(provider.LimitKind(pause.Kind).DefaultCooldown())
		if next.Before(time.Now().Add(30 * time.Second)) {
			next = time.Now().Add(30 * time.Second)
		}
		// kolk's own next check, not a reset the vendor named.
		pause.ResetAt, pause.Estimated = next, true
		if current := a.Sess.Paused(); current == nil || !current.Since.Equal(pause.Since) {
			return nil
		}
		a.Sess.SetPaused(&pause)
		a.saveFor(savePause)
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
	// A handover only when the host itself names the model's connector:
	// connectorFor's gateway fallback is no connector anybody signed into, and
	// its sign-in would answer no for ever.
	if a.ConnectorName != nil && a.HandoverSignedIn != nil && pause.Connector != "" && a.ConnectorName(pause.Model) == pause.Connector {
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

// NormalizeContinuityMode validates continuity.mode: off (the default) or on.
func NormalizeContinuityMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "off":
		return "off", nil
	case "on":
		return "on", nil
	}
	return "", fmt.Errorf("%q is not off or on", value)
}

// NormalizeContinuitySelect validates continuity.select: auto (the default),
// preferred or ask.
func NormalizeContinuitySelect(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto", nil
	case "preferred", "ask":
		return strings.ToLower(strings.TrimSpace(value)), nil
	}
	return "", fmt.Errorf("%q is not auto, preferred or ask", value)
}

// NormalizeContinuityOrder validates continuity.order: the three groups
// subscription, paid and free (subs and metered are accepted spellings),
// each once, in any order.
func NormalizeContinuityOrder(words []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, word := range words {
		w := strings.ToLower(strings.TrimSpace(word))
		switch w {
		case "subs", "subscriptions":
			w = "subscription"
		case "metered", "keys", "key":
			w = "paid"
		case "subscription", "paid", "free":
		case "":
			continue
		default:
			return nil, fmt.Errorf("%q is not subscription, paid or free", word)
		}
		if seen[w] {
			return nil, fmt.Errorf("%q is named twice", w)
		}
		seen[w] = true
		out = append(out, w)
	}
	if len(out) != 3 {
		return nil, fmt.Errorf("name all three of subscription, paid and free")
	}
	return out, nil
}
