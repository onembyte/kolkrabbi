package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/protocol"
)

// conversationUnavailable stops a run whose task must continue a saved vendor
// conversation on a model that cannot start now. It is not the task's outcome:
// the task stays unresolved with its handle and its tree, and a later /resume
// continues it once its model starts.
type conversationUnavailable struct {
	task  int
	model string
	err   error
}

func (e *conversationUnavailable) Error() string {
	return fmt.Sprintf("task %d must continue its saved conversation on %s, which cannot start now: %v; its saved work is retained, /resume when it can start", e.task+1, e.model, e.err)
}

func (e *conversationUnavailable) Unwrap() error { return e.err }

// validateNativeRecovery admits a saved run at a recovery boundary. Vendor
// conversations are decided first, by recoverVendorWork, and must still be the
// session's own, at pauses and other boundaries alike; native work also needs
// the selection it was saved under.
func (a *Agent) validateNativeRecovery(run *continuity.Run) error {
	switch run.Recovery {
	case "", "pause", "error", "limit":
	default:
		return fmt.Errorf("unknown recovery boundary %q; saved work is retained", run.Recovery)
	}
	if err := recoverVendorWork(run); err != nil {
		return err
	}
	if err := a.ownsSavedConversation(run); err != nil {
		return err
	}
	if run.LastPause != nil {
		return nil
	}
	if run.Model != a.SessionModel() || run.Effort != a.Effort {
		return fmt.Errorf("saved recovery needs %s at %s; restore that selection to resume", run.Model, run.Effort)
	}
	backend, _, err := a.backendFor(a.savedMainModel(run))
	if err != nil {
		return err
	}
	if _, owned := backend.(interface{ ProviderHandle() string }); owned && !vendorOwned(run.Main) {
		return fmt.Errorf("native recovery cannot move to a vendor-owned conversation; saved work is retained")
	}
	return nil
}

// ownsSavedConversation holds a saved vendor conversation to its vendor and to
// that conversation alone: the session must still answer through the same
// vendor, on the very handle the request was saved on. Anything else would
// send the request to a conversation that never saw it. A request saved
// before these facts were journaled cannot show whose conversation the session
// holds, so a vendor-owned session refuses it rather than resend its turn.
func (a *Agent) ownsSavedConversation(run *continuity.Run) error {
	legacy := run.Main.State == ""
	if !legacy && (run.Main.ProviderState == "" || !vendorOwned(run.Main)) {
		return nil
	}
	model := a.savedMainModel(run)
	backend, _, err := a.backendFor(model)
	if err != nil {
		return err
	}
	handle, ok := backend.(interface{ ProviderHandle() string })
	if legacy {
		if ok && handle.ProviderHandle() != "" {
			return fmt.Errorf("this request was saved before kolk recorded what vendors did, and this session's vendor conversation may already hold its turn, so nothing proves it finished. The saved work is retained; inspect it, then /resume discard to set it aside")
		}
		return nil
	}
	if !ok {
		return fmt.Errorf("the saved request's conversation belongs to a vendor this session no longer uses; saved work is retained")
	}
	if vendor := a.connectorFor(model); run.Main.Vendor != "" && vendor != run.Main.Vendor {
		return fmt.Errorf("the saved request's conversation belongs to %s, and this session now reaches its model through %s; saved work is retained", run.Main.Vendor, vendor)
	}
	if handle.ProviderHandle() != run.Main.ProviderState {
		return fmt.Errorf("this session now drives a different vendor conversation from the one the saved request ran in; saved work is retained")
	}
	return nil
}

// drivesConversation says a backend continues exactly the vendor conversation
// named by handle.
func drivesConversation(backend ChatBackend, handle string) bool {
	owner, ok := backend.(interface{ ProviderHandle() string })
	return ok && owner.ProviderHandle() == handle
}

// savedMainModel is the model the saved main session last ran on, or the
// session's own when it recorded none.
func (a *Agent) savedMainModel(run *continuity.Run) string {
	if run.Main.Model != "" {
		return run.Main.Model
	}
	return a.modelFor(a.Effort)
}

// resumesConversation is the adapter's own word that it continues a confirmed
// conversation by its handle, with a new message. That is continuing a turn
// the vendor closed; no adapter can attach to one the vendor left open.
func resumesConversation(backend ChatBackend) bool {
	resumable, ok := backend.(interface{ ResumesConversation() bool })
	return ok && resumable.ResumesConversation()
}

// turnNeverStarted is the adapter's proof that its latest turn's prompt never
// reached a vendor process. Claude and codex give it; Copilot cannot.
func turnNeverStarted(backend ChatBackend) bool {
	proof, ok := backend.(interface{ TurnNeverStarted() bool })
	return ok && proof.TurnNeverStarted()
}

// turnClosed is the vendor's own word, relayed by its adapter, that it ended
// the latest turn.
func turnClosed(backend ChatBackend) bool {
	proof, ok := backend.(interface{ TurnClosed() bool })
	return ok && proof.TurnClosed()
}

// vendorOwned says a task's work ran in a vendor's own conversation.
func vendorOwned(task continuity.Task) bool {
	return task.ProviderOwned || task.ProviderState != "" || task.VendorTools != nil
}

// recoverVendorWork decides, from the journal alone and before any provider is
// opened, how each unsettled vendor conversation comes back:
//
//   - its latest prompt provably never reached the vendor, and no vendor tool
//     was reported: it starts over on a new conversation, which cannot repeat
//     anything;
//   - its last turn was committed, with nothing left unfinished: nothing can
//     be redone;
//   - the vendor closed its interrupted turn itself, no tool it started is
//     unfinished, and its confirmed conversation continues through a resuming
//     adapter: a new message continues it;
//
// and otherwise it is refused with its work retained. An open turn may still
// hold actions the vendor started, an unfinished tool may or may not have
// taken effect, and a new conversation given the transcript could do again
// what the vendor did; none of that is reconciled by asking the model to look.
// A journal saved before these facts were recorded proves none of them.
func recoverVendorWork(run *continuity.Run) error {
	legacy := run.Main.State == ""
	decide := func(task *continuity.Task, what string) error {
		if !vendorOwned(*task) {
			return nil
		}
		if legacy {
			return fmt.Errorf("%s ran in a vendor conversation saved before kolk recorded what vendors did, so nothing proves its turn finished. The saved work is retained; inspect it, then /resume discard to set it aside", what)
		}
		tools := task.VendorTools
		reported := tools != nil && (tools.LastFinishedID != "" || len(tools.Unfinished) > 0)
		unfinished := tools != nil && len(tools.Unfinished) > 0
		if task.ProviderNeverStarted && !reported {
			task.ProviderState, task.ProviderConfirmed, task.ProviderResumable = "", false, false
			task.ProviderInFlight, task.VendorTools, task.ProviderNeverStarted, task.ProviderTurnClosed = false, nil, false, false
			return nil
		}
		if !task.ProviderInFlight && !unfinished {
			return nil
		}
		var why string
		switch {
		case unfinished:
			why = "a tool the vendor started never reported finishing"
		case !task.ProviderTurnClosed:
			why = "the vendor never closed its turn, so it may still hold actions it started"
		case task.ProviderState == "" || !task.ProviderConfirmed:
			why = "the conversation it ran in cannot be reached"
		case !task.ProviderResumable:
			why = "its adapter cannot continue a conversation by its handle"
		default:
			return nil
		}
		return fmt.Errorf("%s: %s; continuing or starting over could repeat what the vendor already did. The saved work is retained; inspect it, then /resume discard to set it aside", what, why)
	}
	if err := decide(&run.Main, "this request"); err != nil {
		return err
	}
	for i := range run.Tasks {
		if run.Tasks[i].Status != "" {
			continue
		}
		if err := decide(&run.Tasks[i], fmt.Sprintf("task %d", i+1)); err != nil {
			return err
		}
	}
	return nil
}

// retireRecovery invalidates the previous replay point before another action
// can begin. The newer JSON record wins over its compressed predecessor on
// restart; a crash after this write is an uncertain interruption, not permission
// to replay the older snapshot. A failed retirement admits no work.
func (a *Agent) retireRecovery(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.executionMu.Lock()
	if a.execution == nil || (a.execution.Recovery == "" && a.execution.LastPause == nil) {
		a.executionMu.Unlock()
		return nil
	}
	reason, pause := a.execution.Recovery, a.execution.LastPause
	a.execution.Recovery, a.execution.LastPause = "", nil
	a.executionMu.Unlock()
	a.storeExecution()
	_ = a.takePending()
	if a.Sess == nil {
		return a.recoveryWriteFailed("resume", fmt.Errorf("session storage is unavailable"))
	}
	if err := a.Sess.Save(); err != nil {
		a.executionMu.Lock()
		if a.execution != nil {
			a.execution.Recovery, a.execution.LastPause = reason, pause
			a.Sess.SetRunState(a.execution)
		}
		a.executionMu.Unlock()
		return a.recoveryWriteFailed("resume", err)
	}
	return nil
}

// showCommittedReply finishes a saved answer without asking a model to produce
// it again. Running is allowed here: exceptional mid-turn snapshots can be
// between a committed response and the turn's final bookkeeping.
func (a *Agent) showCommittedReply(run *continuity.Run) bool {
	if run == nil || a.Sess == nil || run.Main.ProviderInFlight || run.Main.SafeBoundary != "provider response committed" {
		return false
	}
	messages := a.Sess.GetMessages()
	if len(messages) == 0 {
		return false
	}
	reply := messages[len(messages)-1]
	if reply.Role != "assistant" || len(reply.ToolCalls) != 0 {
		return false
	}
	fmt.Fprintf(a.Out, "%s%s%s %s\n", colorCyan, a.responseLabel(), colorReset, reply.Content)
	if a.Bus != nil {
		data, _ := json.Marshal(protocol.MessageCompletedData{Text: reply.Content})
		_, _ = a.Bus.Publish(bus.Event{Turn: a.lastTurnID, Type: protocol.EventMessageCompleted, Data: data})
	}
	return true
}
