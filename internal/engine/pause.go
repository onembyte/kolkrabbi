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
	return fmt.Sprintf("paused: %s hit its %s; kolk resumes at %s and spends nothing until then (/resume to try now)",
		what, e.Pause.HumanKind(), e.Pause.Resumes())
}

// pauseIfWaitingHelps turns a turn that ended on a pausable limit into a pause:
// the pending input kept on the session, the dangling user message removed so
// the transcript does not claim an answer, the pause persisted, and the two
// events -- provider.limit{pause} and turn.finished{paused} -- published. It
// reports whether it did so.
func (a *Agent) pauseIfWaitingHelps(ctx context.Context, err error, pending string) (PausedError, bool) {
	if a.Sess == nil || ctx.Err() != nil {
		return PausedError{}, false
	}
	limit, ok := provider.Classify(err)
	if !ok || !continuity.Pausable(limit) {
		return PausedError{}, false
	}
	if limit.Model == "" {
		limit.Model = a.SessionModel()
	}
	if limit.Connector == "" {
		limit.Connector = a.connectorFor(limit.Model)
	}
	pause := continuity.PauseFor(limit, pending, time.Now())
	if msgs := a.Sess.GetMessages(); len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
		a.Sess.SetMessages(msgs[:len(msgs)-1])
	}
	a.Sess.SetPaused(&pause)
	a.save()
	a.publishLimit(limit, "pause")
	if a.Bus != nil {
		data, _ := json.Marshal(protocol.TurnFinishedData{Reason: "paused", RawReason: pause.HumanKind() + " until " + pause.ResetAt.UTC().Format(time.RFC3339)})
		_, _ = a.Bus.Publish(bus.Event{Turn: a.lastTurnID, Type: protocol.EventTurnFinished, Data: data})
	}
	fmt.Fprintf(a.Out, "◆ %s\n", (&PausedError{Pause: pause}).Error())
	return PausedError{Pause: pause}, true
}

// stillPaused reports the pause that still holds, clearing one whose reset has
// passed. Resuming the pending turn is the monitor's job (V35.2b); here the
// only rule is that a paused session spends nothing until then.
func (a *Agent) stillPaused() *PausedError {
	if a.Sess == nil {
		return nil
	}
	p := a.Sess.Paused()
	if p == nil {
		return nil
	}
	if !p.ResetAt.After(time.Now()) {
		a.Sess.SetPaused(nil)
		a.save()
		if p.PendingTurn != "" {
			fmt.Fprintf(a.Out, "◆ the pause has lifted; the turn that was waiting (%q) was not re-sent\n", compactToolText(p.PendingTurn))
		}
		return nil
	}
	return &PausedError{Pause: *p}
}
