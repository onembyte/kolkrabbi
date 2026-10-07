package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *Agent) executionSnapshot() *continuity.Run {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	a.materializePartialsLocked()
	return a.execution.Clone()
}

// A surface can accept delivery just before Ctrl+C cancels the turn. No work
// began, so return that claim to the saved pause for an honest /resume retry.
func (a *Agent) retainUnstartedResume(input string) {
	if a.Sess == nil {
		return
	}
	a.resumeMu.Lock()
	defer a.resumeMu.Unlock()
	run := a.Sess.RunState()
	if run == nil || run.Input != input || (run.LastPause == nil && run.Recovery == "") || run.Phase == "done" || run.Phase == "stopped" {
		return
	}
	a.resumeClaim = ""
	if run.LastPause != nil && a.Sess.Paused() == nil {
		a.Sess.SetPaused(run.LastPause)
		a.saveFor(savePause)
	}
}

func (a *Agent) beginExecution(input, prompt string) {
	providerState := ""
	if a.Sess != nil {
		providerState = a.Sess.ProviderStateName()
	}
	a.executionMu.Lock()
	a.execution = &continuity.Run{Version: 1, ID: a.lastTurnID, Input: input, Prompt: prompt,
		Root: a.Root, Mode: a.Mode, Model: a.SessionModel(), Effort: a.Effort, Phase: "new",
		// A recorded handle is noted the moment a backend owns one, before
		// the vendor accepted it: it is never confirmation by itself.
		Main:  continuity.Task{State: continuity.TaskQueued, ProviderState: providerState},
		Spend: continuity.Spend{Limit: a.MaxRunCostUSD}}
	a.partialMain, a.partialTask, a.attemptBefore = nil, nil, nil
	a.executionMu.Unlock()
	a.storeExecution()
}

func (a *Agent) restoreExecution(ctx context.Context, input string) (bool, error) {
	if a.Sess == nil {
		return false, nil
	}
	run := a.Sess.RunState()
	if run == nil || run.Phase == "done" || run.Phase == "stopped" {
		return false, nil
	}
	if run.Version != 1 {
		return false, fmt.Errorf("cannot resume execution format %d; its saved work is retained", run.Version)
	}
	if run.LastPause == nil && run.Recovery == "" {
		return false, errors.New("saved work was interrupted outside a completed recovery boundary; inspect its history, then /resume discard to start a new request without replaying uncertain actions")
	}
	if err := a.validateNativeRecovery(run); err != nil {
		return false, err
	}
	if run.Input != input {
		return false, errors.New("an unfinished request is saved; resume that request before starting another")
	}
	if filepath.Clean(run.Root) != filepath.Clean(a.Root) {
		return false, fmt.Errorf("unfinished work belongs to %s; reopen that project to resume", run.Root)
	}
	if run.Mode != a.Mode {
		return false, fmt.Errorf("unfinished work uses %s mode; restore that mode to resume", run.Mode)
	}
	switch run.Phase {
	case "new", "plan", "tasks", "synthesis", "direct":
	default:
		return false, fmt.Errorf("unknown saved execution phase %q; its work is retained", run.Phase)
	}
	if !validExecutionTaskState(run.Main.State) || run.Main.Rounds < 0 {
		return false, errors.New("invalid saved main task state; its work is retained")
	}
	for i, task := range run.Tasks {
		if task.Rounds < 0 || task.Title == "" || task.Model == "" {
			return false, fmt.Errorf("invalid saved task %d; its work is retained", i+1)
		}
		if !validExecutionTaskState(task.State) ||
			task.State == continuity.TaskSettled && task.Status == "" ||
			task.State != "" && task.State != continuity.TaskSettled && task.Status != "" {
			return false, fmt.Errorf("invalid saved state for task %d; its work is retained", i+1)
		}
		for _, dependency := range task.Needs {
			if dependency < 0 || dependency >= i {
				return false, fmt.Errorf("invalid saved dependency for task %d; its work is retained", i+1)
			}
		}
		if task.Status != "" {
			switch task.Status {
			case "done", "incomplete", "failed", "blocked", "not run — over budget":
			default:
				return false, fmt.Errorf("invalid saved outcome for task %d; its work is retained", i+1)
			}
			continue
		}
		if task.Workspace != "" {
			if a.Isolator == nil {
				return false, fmt.Errorf("task %d needs its saved worktree; enable isolation to resume", i+1)
			}
			if err := a.Isolator.Resume(ctx, a.Root, task.ChildTurn, task.Workspace); err != nil {
				return false, fmt.Errorf("cannot resume task %d: %w; its saved work is retained; /resume discard abandons this request", i+1, err)
			}
		}
	}
	// Re-resolve only unfinished work when the user explicitly changes the
	// ceiling. A saved vendor conversation cannot be moved to another provider
	// without its private transcript, so retain it and explain the restriction.
	if run.Model != a.SessionModel() || run.Effort != a.Effort {
		if run.Main.ProviderState != "" {
			return false, fmt.Errorf("unfinished provider conversation needs %s at %s; restore that selection or /resume discard", run.Model, run.Effort)
		}
		for _, task := range run.Tasks {
			if task.Status == "" && task.ProviderState != "" {
				return false, fmt.Errorf("unfinished provider conversation needs %s at %s; restore that selection to resume", run.Model, run.Effort)
			}
		}
		tasks := executionTasks(run)
		for i := range tasks {
			tasks[i].Model, tasks[i].Vendor, tasks[i].Effort, tasks[i].CeilingEffort = "", "", "", ""
		}
		a.assignModels(tasks)
		for i, task := range tasks {
			if run.Tasks[i].Status == "" {
				run.Tasks[i].Model, run.Tasks[i].Vendor = task.Model, task.Vendor
				run.Tasks[i].Effort, run.Tasks[i].CeilingEffort = task.Effort, task.CeilingEffort
			}
		}
		run.Model, run.Effort = a.SessionModel(), a.Effort
	}
	a.executionMu.Lock()
	a.execution = run
	// A restored call's saved partial stays until a new call begins.
	a.partialMain, a.partialTask, a.attemptBefore = nil, nil, nil
	a.executionMu.Unlock()
	return true, nil
}

func validExecutionTaskState(state string) bool {
	switch state {
	case "", continuity.TaskQueued, continuity.TaskRunning, continuity.TaskWaiting, continuity.TaskSettled:
		return true
	default:
		return false
	}
}

func (a *Agent) storeExecution() {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution == nil || a.Sess == nil {
		return
	}
	if a.runSpend != nil {
		a.execution.Spend = a.runSpend.snapshot()
	}
	a.materializePartialsLocked()
	a.Sess.SetRunState(a.execution)
	a.markDirty()
}

func (a *Agent) finishExecution(err error) {
	a.executionMu.Lock()
	if a.execution != nil {
		var paused *PausedError
		var recoveryFailed *RecoverySaveError
		switch {
		case err == nil:
			a.execution.Phase = "done"
			a.execution.Main.State = continuity.TaskSettled
		case errors.As(err, &paused):
			// Keep the exact phase and settled work for the next attempt.
		case errors.As(err, &recoveryFailed):
			// saveRecovery already copied the complete in-memory state into the
			// session. Do not follow its failed compressed write with a plain JSON
			// flush that could look like the promised recovery point.
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			a.execution.Phase = "stopped"
		default:
			// A surfaced provider/planner/synthesis error has a recovery point.
			// Preserve its exact phase; the resume validator decides whether the
			// saved boundary is safe to continue.
		}
	}
	a.executionMu.Unlock()
	var recoveryFailed *RecoverySaveError
	if !errors.As(err, &recoveryFailed) {
		a.storeExecution()
	}
	a.executionMu.Lock()
	a.execution = nil
	a.partialMain, a.partialTask, a.attemptBefore = nil, nil, nil
	a.executionMu.Unlock()
}

func (a *Agent) setExecutionPlan(tasks []Task) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution == nil {
		return
	}
	a.execution.Tasks = make([]continuity.Task, len(tasks))
	a.execution.Main.State = continuity.TaskWaiting
	for i, task := range tasks {
		// Queued until launched: waiting on a dependency is not a pause.
		a.execution.Tasks[i] = continuity.Task{Title: task.Title, Kind: string(task.Kind), Level: string(task.Level),
			Needs: append([]int(nil), task.Needs...), Model: task.Model, Vendor: task.Vendor,
			Effort: task.Effort, CeilingEffort: task.CeilingEffort, Workspace: task.Workspace, State: continuity.TaskQueued}
	}
	a.execution.Phase = "tasks"
}

func executionTasks(run *continuity.Run) []Task {
	tasks := make([]Task, len(run.Tasks))
	for i, task := range run.Tasks {
		tasks[i] = Task{Title: task.Title, Kind: Kind(task.Kind), Level: Level(task.Level),
			Needs: append([]int(nil), task.Needs...), Model: task.Model, Vendor: task.Vendor,
			Effort: task.Effort, CeilingEffort: task.CeilingEffort, Workspace: task.Workspace}
	}
	return tasks
}

func (a *Agent) setExecutionOutcome(index int, result outcome) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution == nil || index >= len(a.execution.Tasks) {
		return
	}
	task := &a.execution.Tasks[index]
	task.Status, task.Result, task.Reason = result.Status.String(), result.Result, result.Reason
	task.State = continuity.TaskSettled
}

func executionOutcome(task continuity.Task) (outcome, bool) {
	if task.Status == "" {
		return outcome{}, false
	}
	state := statusFailed
	for _, candidate := range []status{statusDone, statusIncomplete, statusFailed, statusBlocked, statusOverBudget} {
		if candidate.String() == task.Status {
			state = candidate
			break
		}
	}
	return outcome{Status: state, Result: task.Result, Reason: task.Reason}, true
}

func (a *Agent) setExecutionPhase(phase string) {
	a.executionMu.Lock()
	if a.execution != nil {
		a.execution.Phase = phase
		switch phase {
		case "plan", "direct", "synthesis":
			a.execution.Main.State = continuity.TaskRunning
		}
	}
	a.executionMu.Unlock()
	a.storeExecution()
}

func (a *Agent) executionTask(index int) continuity.Task {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution == nil || index < 0 || index >= len(a.execution.Tasks) {
		return continuity.Task{}
	}
	a.materializePartialsLocked()
	return a.execution.Tasks[index].Clone()
}

func (a *Agent) saveChildConversation(index int, messages []provider.Message, rounds int, loop doomLoop, backend ChatBackend) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution == nil || index < 0 || index >= len(a.execution.Tasks) {
		return
	}
	task := &a.execution.Tasks[index]
	task.Messages, task.Rounds = messages, rounds
	task.Loop = savedLoop(loop)
	_, task.ProviderOwned = backend.(interface{ ProviderHandle() string })
	task.ProviderResumable = resumesConversation(backend)
	// An attempt that never arrived keeps the conversation the task was on,
	// whatever handle the adapter holds now.
	kept := a.settleAttempt(index, task, backend)
	if state, ok := backend.(interface{ ProviderHandle() string }); ok && !kept {
		handle, confirmed := state.ProviderHandle(), false
		if state, ok := backend.(interface{ ProviderHandleConfirmed() bool }); ok {
			confirmed = state.ProviderHandleConfirmed()
		}
		// Confirmation is a fact about a handle, kept once learned: a resumed
		// process that fails before the vendor's init does not unlearn it.
		if handle != "" && handle == task.ProviderState && task.ProviderConfirmed {
			confirmed = true
		}
		task.ProviderState, task.ProviderConfirmed = handle, confirmed
	}
	*task = task.Clone()
}

func (a *Agent) setExecutionTaskState(index int, state string) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution != nil && index >= 0 && index < len(a.execution.Tasks) &&
		(a.execution.Tasks[index].Status == "" || state == continuity.TaskSettled) {
		a.execution.Tasks[index].State = state
	}
}

// materializePartialsLocked copies what each live provider call has streamed
// into the journal. strings.Builder.String does not copy, so a store costs
// nothing per byte already streamed. Called with executionMu held.
func (a *Agent) materializePartialsLocked() {
	if a.execution == nil {
		return
	}
	if a.partialMain != nil {
		a.execution.Main.PartialOutput = a.partialMain.String()
	}
	for index, partial := range a.partialTask {
		if index >= 0 && index < len(a.execution.Tasks) {
			a.execution.Tasks[index].PartialOutput = partial.String()
		}
	}
}

func (a *Agent) beginMainProviderCall() {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution != nil {
		a.rememberAttempt(-1, &a.execution.Main, a.partialMain)
		a.execution.Main.State = continuity.TaskRunning
		a.execution.Main.PartialOutput = ""
		a.execution.Main.ProviderInFlight = true
		a.partialMain = &strings.Builder{}
	}
}

func (a *Agent) appendMainPartial(text string) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution != nil && a.partialMain != nil {
		a.partialMain.WriteString(text)
	}
}

func (a *Agent) completeMainProviderCall() {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	a.partialMain = nil
	if a.execution != nil {
		a.execution.Main.PartialOutput = ""
		a.execution.Main.ProviderInFlight = false
		a.execution.Main.SafeBoundary = "provider response committed"
		// A committed response ends the vendor's turn, tools included.
		if tools := a.execution.Main.VendorTools; tools != nil {
			tools.Unfinished = nil
		}
	}
}

func (a *Agent) beginChildProviderCall(index int) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution != nil && index >= 0 && index < len(a.execution.Tasks) {
		task := &a.execution.Tasks[index]
		a.rememberAttempt(index, task, a.partialTask[index])
		task.State = continuity.TaskRunning
		task.PartialOutput = ""
		task.ProviderInFlight = true
		if a.partialTask == nil {
			a.partialTask = make(map[int]*strings.Builder)
		}
		a.partialTask[index] = &strings.Builder{}
	}
}

func (a *Agent) appendChildPartial(index int, text string) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if partial := a.partialTask[index]; a.execution != nil && partial != nil {
		partial.WriteString(text)
	}
}

func (a *Agent) completeChildProviderCall(index int) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	delete(a.partialTask, index)
	if a.execution != nil && index >= 0 && index < len(a.execution.Tasks) {
		task := &a.execution.Tasks[index]
		task.PartialOutput = ""
		task.ProviderInFlight = false
		task.SafeBoundary = "provider response committed"
		if task.VendorTools != nil {
			task.VendorTools.Unfinished = nil
		}
	}
}

func (a *Agent) markMainToolBoundary() {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution != nil {
		a.execution.Main.SafeBoundary = "tool results committed"
	}
}

func (a *Agent) markChildToolBoundary(index int) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution != nil && index >= 0 && index < len(a.execution.Tasks) {
		a.execution.Tasks[index].SafeBoundary = "tool results committed"
	}
}

// markExecutionWaiting turns running work into waiting work at a pause or an
// error: a continuation, with its own transcript and handle. Work that never
// started stays queued.
func (a *Agent) markExecutionWaiting() {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution == nil {
		return
	}
	if a.execution.Main.State == continuity.TaskRunning {
		a.execution.Main.State = continuity.TaskWaiting
	}
	for i := range a.execution.Tasks {
		task := &a.execution.Tasks[i]
		if task.Status == "" && task.State == continuity.TaskRunning {
			task.State = continuity.TaskWaiting
		}
	}
}

func (a *Agent) saveChildRoute(index int, model, effort, workspace, childTurn string) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution == nil || index < 0 || index >= len(a.execution.Tasks) {
		return
	}
	task := &a.execution.Tasks[index]
	task.Model, task.Effort, task.Workspace = model, effort, workspace
	if task.ChildTurn == "" {
		task.ChildTurn = childTurn
	}
}

func (a *Agent) saveChildCheckpoint(index, handle int) {
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution != nil && index >= 0 && index < len(a.execution.Tasks) {
		a.execution.Tasks[index].Checkpoint = &handle
	}
}

func savedLoop(loop doomLoop) continuity.ToolLoop {
	return continuity.ToolLoop{Last: loop.last, Repeats: loop.repeats, Reported: loop.reported,
		Denied: loop.denied, Recent: append([]string(nil), loop.recent...)}
}

func restoredLoop(loop continuity.ToolLoop) doomLoop {
	return doomLoop{last: loop.Last, repeats: loop.Repeats, reported: loop.Reported,
		denied: loop.Denied, recent: append([]string(nil), loop.Recent...)}
}

func (a *Agent) saveMainProgress(rounds int, loop doomLoop) {
	a.executionMu.Lock()
	if a.execution != nil {
		a.execution.Main.Rounds, a.execution.Main.Loop = rounds, savedLoop(loop)
	}
	a.executionMu.Unlock()
	a.storeExecution()
}

func (a *Agent) captureMainProviderState(backend ChatBackend) {
	handle, confirmed := "", false
	if backend != nil {
		if state, ok := backend.(interface{ ProviderHandle() string }); ok {
			handle = state.ProviderHandle()
		}
		if state, ok := backend.(interface{ ProviderHandleConfirmed() bool }); ok {
			confirmed = state.ProviderHandleConfirmed()
		}
	}
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution != nil {
		_, a.execution.Main.ProviderOwned = backend.(interface{ ProviderHandle() string })
		main := &a.execution.Main
		main.ProviderResumable = resumesConversation(backend)
		// An attempt that never arrived keeps the conversation the request was
		// on, whatever handle the adapter holds now.
		if a.settleAttempt(-1, main, backend) {
			return
		}
		if handle != "" && handle == a.execution.Main.ProviderState && a.execution.Main.ProviderConfirmed {
			confirmed = true
		}
		a.execution.Main.ProviderState = handle
		a.execution.Main.ProviderConfirmed = confirmed
	}
}

// A provider can report billable work before ending on a limit. Preserve that
// observed usage, without inventing a paid call for a rejected empty request.
func (a *Agent) recordFailedWork(role string, meta provider.Meta, effort string) {
	if meta.Cost > 0 || meta.PromptTokens > 0 || meta.CompletionTokens > 0 || meta.ToolCalls > 0 {
		a.recordAtEffort(role, meta, meta.ToolCalls, effort)
	}
}

// pendingToolCalls finds only the unanswered calls in the latest tool round.
// Earlier rounds may reuse IDs, so their answers cannot satisfy this round.
func pendingToolCalls(messages []provider.Message) []provider.ToolCall {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		answered := map[string]bool{}
		for _, result := range messages[i+1:] {
			if result.Role != "tool" {
				return nil
			}
			if result.Role == "tool" {
				answered[result.ToolCallID] = true
			}
		}
		var pending []provider.ToolCall
		for _, call := range messages[i].ToolCalls {
			if !answered[call.ID] {
				pending = append(pending, call)
			}
		}
		return pending
	}
	return nil
}

// attemptFacts is what the journal knew about a task's vendor turn before an
// attempt began, kept so an attempt that never reached the vendor can leave
// it exactly as it found it.
type attemptFacts struct {
	state    string
	inFlight bool
	partial  string
}

// rememberAttempt keeps the task's facts as the attempt about to begin finds
// them. index -1 is the main session. Called with executionMu held.
func (a *Agent) rememberAttempt(index int, task *continuity.Task, partial *strings.Builder) {
	if a.attemptBefore == nil {
		a.attemptBefore = make(map[int]attemptFacts)
	}
	text := task.PartialOutput
	if partial != nil {
		text = partial.String()
	}
	a.attemptBefore[index] = attemptFacts{state: task.State, inFlight: task.ProviderInFlight, partial: text}
}

// settleAttempt records what the backend proves about its latest attempt.
// An attempt that reached the vendor is delivered for good. One that provably
// never did, after an earlier one that did, changed nothing the vendor holds,
// so the task keeps what it was before that attempt: its state, whether a
// turn was open, the words that turn streamed, and (left untouched here) its
// closure. Only a task no attempt ever reached is recorded as never started.
// Called with executionMu held.
func (a *Agent) settleAttempt(index int, task *continuity.Task, backend ChatBackend) (kept bool) {
	neverStarted := turnNeverStarted(backend)
	if !neverStarted {
		task.ProviderDelivered = true
	}
	task.ProviderNeverStarted = neverStarted && !task.ProviderDelivered
	if !neverStarted || !task.ProviderDelivered {
		task.ProviderTurnClosed = turnClosed(backend)
		return false
	}
	if before, ok := a.attemptBefore[index]; ok {
		task.State, task.ProviderInFlight = before.state, before.inFlight
		task.PartialOutput = before.partial
		if index < 0 {
			a.partialMain = nil
		} else {
			delete(a.partialTask, index)
		}
	}
	return true
}
