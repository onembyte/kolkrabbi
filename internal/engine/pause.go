package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

// PausedError is what RunTurn returns while the session is paused: a fact
// about the session, not a failure of the turn.
type PausedError struct{ Pause continuity.Pause }

func (e *PausedError) Error() string {
	what := e.Pause.Model
	if what == "" {
		what = e.Pause.Connector
	}
	return fmt.Sprintf("paused: %s hit its %s; %s; /resume to retry the waiting turn", what, e.Pause.HumanKind(), e.Pause.RetryStatus())
}

// pauseIfWaitingHelps turns a turn that ended on a pausable limit into a pause:
// the pending input kept on the session, the dangling user message removed so
// the transcript does not claim an answer, the pause persisted, and the two
// events -- provider.limit{pause} and turn.finished{paused} -- published. It
// reports whether it did so.
func (a *Agent) pauseIfWaitingHelps(ctx context.Context, err error, pending string) (PausedError, error, bool) {
	if a.Sess == nil {
		return PausedError{}, nil, false
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return PausedError{}, ctxErr, false
	}
	limit, ok := provider.Classify(err)
	if !ok || !continuity.Pausable(limit) {
		return PausedError{}, nil, false
	}
	if limit.Model == "" {
		limit.Model = a.SessionModel()
	}
	if limit.Connector == "" {
		limit.Connector = a.connectorFor(limit.Model)
	}
	pause := continuity.PauseFor(limit, pending, time.Now())
	a.executionMu.Lock()
	var prior *continuity.Pause
	if a.execution != nil {
		prior = a.execution.LastPause
		a.execution.LastPause = &pause
	}
	a.executionMu.Unlock()
	if a.executionSnapshot() == nil {
		if msgs := a.Sess.GetMessages(); len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
			a.Sess.SetMessages(msgs[:len(msgs)-1])
		}
	}
	a.markExecutionWaiting()
	a.storeExecution()
	a.Sess.SetPaused(&pause)
	if saveErr := a.saveRecovery("pause"); saveErr != nil {
		// Keep the journal in memory, but do not arm automatic delivery or
		// publish a pause that survives only by accident in this process.
		// The run keeps no pause either: a later ordinary save must not
		// persist one the recovery point never held. Set directly, with no
		// dirty mark: a JSON flush is not the failed recovery point.
		a.Sess.SetPaused(nil)
		a.executionMu.Lock()
		if a.execution != nil {
			a.execution.LastPause = prior
			a.Sess.SetRunState(a.execution)
		}
		a.executionMu.Unlock()
		a.announcePausedChildren(false)
		return PausedError{Pause: pause}, saveErr, true
	}
	a.announcePausedChildren(true)
	a.publishLimit(limit, "pause")
	if a.Mode == ModeAgent {
		a.publishMainWork(protocol.WorkStateWaiting, protocol.WorkPhaseSchedule, "paused: waiting for allowance", a.orchestrationModel(), a.Effort)
	}
	if a.Bus != nil {
		// An estimate says when kolk checks next, not when the vendor resets.
		when := " until "
		if pause.Estimated {
			when = "; next check "
		}
		data, _ := json.Marshal(protocol.TurnFinishedData{Reason: "paused", RawReason: pause.HumanKind() + when + pause.ResetAt.UTC().Format(time.RFC3339)})
		_, _ = a.Bus.Publish(bus.Event{Turn: a.lastTurnID, Type: protocol.EventTurnFinished, Data: data})
	}
	fmt.Fprintf(a.Out, "◆ %s\n", (&PausedError{Pause: pause}).Error())
	a.printRecommendation(limit)
	return PausedError{Pause: pause}, nil, true
}

// stillPaused is a read. A reset time means resumption can be attempted; it
// cannot consume the pending input. Only Resume or a live delivery does that.
func (a *Agent) stillPaused() *PausedError {
	if a.Sess == nil {
		return nil
	}
	p := a.Sess.Paused()
	if p == nil {
		return nil
	}
	return &PausedError{Pause: *p}
}
