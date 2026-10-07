package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

// runOrchestrated is agent mode: a planner decomposes the request into
// tasks, each task runs in an isolated subagent with its own context (zero
// context cost to the main conversation), and a synthesis call produces the
// final answer. The main session only ever sees user input -> final answer,
// so its history stays small and valid.
func (a *Agent) runOrchestrated(ctx context.Context, userInput string) (turnErr error) {
	ctx, vendorToolFailure := watchProviderToolFailures(ctx)
	ctx = context.WithValue(ctx, executionPauseKey{}, &executionPause{})
	run := a.executionSnapshot()
	if run == nil {
		// Internal callers may exercise orchestration without RunTurn.
		a.lastTurnID = xid.New(xid.Turn)
		a.beginExecution(userInput, userInput)
		defer func() { a.finishExecution(turnErr) }()
		run = a.executionSnapshot()
	}
	if run.Phase == "synthesis" && a.showCommittedReply(run) {
		return nil
	}
	// The main vendor call this run was making when it stopped, planner or
	// synthesis, is continued on its conversation rather than sent again.
	stopped := run.Main.ProviderState != "" && run.Main.ProviderInFlight
	stoppedPhase := run.Phase
	if run.Phase == "new" {
		a.Sess.AppendMessage(provider.Message{Role: "user", Content: userInput})
		a.saveFor(saveUserMessage)
		a.setExecutionPhase("plan")
		run.Phase = "plan"
	}

	model := a.orchestrationModel()

	// One accounting scope per run. Cleared afterwards so an ordinary turn is
	// never charged against an orchestration ceiling.
	a.runSpend = restoredSpend(run.Spend)
	if a.MaxRunCostUSD > 0 && (a.runSpend.limit == 0 || a.MaxRunCostUSD < a.runSpend.limit) {
		a.runSpend.limit = a.MaxRunCostUSD
	}
	defer func() {
		a.storeExecution()
		a.runSpend = nil
	}()

	// ---- 1. plan ----
	tasks := executionTasks(run)
	// A run saved in its direct phase is being resumed, not newly planned.
	resuming := run.Phase == "direct"
	problem := ""
	if run.Phase == "plan" {
		fmt.Fprintf(a.Out, "%s◆ planning (%s)…%s\n", colorMag, model, colorReset)
		a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhasePlanning, "planning tasks", model, a.Effort)
		// A stopped call continues on the model that answered it, the one
		// checked to drive the saved conversation, whatever the slots say now.
		planModel, continuing := model, stopped && stoppedPhase == "plan"
		if continuing {
			planModel = a.savedMainModel(run)
		}
		planned, why, meta, err := a.planContinuing(ctx, planModel, userInput, 0, continuing)
		if err != nil {
			a.recordFailedWork("planner", meta, a.Effort)
			return err
		}
		a.record("planner", meta, 0)
		tasks, problem = planned, why
		// Resolve once. The persisted plan is exactly what was announced and
		// executed, including effort and the discovered provider binding.
		a.assignModels(tasks)
		a.setExecutionPlan(tasks)
		a.completeMainProviderCall()
		a.storeExecution()
		if err := a.saveProviderToolFailure(ctx, vendorToolFailure); err != nil {
			return err
		}
	}

	if len(tasks) <= 1 {
		// not worth orchestrating: degrade gracefully to the normal loop,
		// reusing the user message we already appended. With no readable
		// plan nothing is lost either, since the request runs whole, but the
		// planner did not choose a single step, so that is not what is said.
		line, work := "single-step task, running directly", "running a single task directly"
		switch {
		case resuming:
			line, work = "resuming the request directly", "resuming the request directly"
		case problem != "":
			line, work = problem+"; running the request directly", "running the request directly: no readable plan"
		}
		fmt.Fprintf(a.Out, "%s◆ %s%s\n", colorMag, line, colorReset)
		a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhaseSchedule, work, model, a.Effort)
		a.setExecutionPhase("direct")
		return a.runLoop(ctx, userInput)
	}

	a.announcePlan(tasks)
	a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhaseSchedule,
		fmt.Sprintf("delegating %d tasks", len(tasks)), model, a.Effort)

	// ---- 2. delegate ----
	outcomes, err := a.runTasks(ctx, userInput, tasks)
	if err != nil {
		return err
	}
	if err := a.retireRecovery(ctx); err != nil {
		return err
	}
	a.setExecutionPhase("synthesis")

	// ---- 3. synthesize ----
	if failures := countFailures(outcomes); failures > 0 {
		fmt.Fprintf(a.Out, "\n%s◆ %d of %d tasks did not finish — the answer below is partial%s\n",
			colorMag, failures, len(tasks), colorReset)
	}
	fmt.Fprintf(a.Out, "\n%s◆ synthesizing%s\n", colorMag, colorReset)
	a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhaseSynthesis, "synthesizing the result", model, a.Effort)
	budget := briefingBudget(a.orchestrationWindow(model))
	synthesis := func(budget int) []provider.Message {
		msgs := synthesisMessages(userInput, tasks, outcomes, budget)
		if stopped && stoppedPhase == "synthesis" {
			msgs = appendMessage(msgs, continuationMessage())
		}
		return msgs
	}
	synth := synthesis(budget)
	fmt.Fprintf(a.Out, "%s%s%s ", colorCyan, a.responseLabel(), colorReset)
	onToken := func(tok string) {
		a.appendMainPartial(tok)
		fmt.Fprint(a.Out, tok)
	}
	synthModel := model
	if stopped && stoppedPhase == "synthesis" {
		synthModel = a.savedMainModel(run)
	}
	msg, meta, err := a.streamChatObserved(a.mainProviderCall(ctx), activitySynthesizing, synthModel, synth, nil, onToken, a.mainProviderProgress(synthModel, a.Effort))
	if provider.IsContextOverflow(err) {
		a.recordFailedWork("synthesis", meta, a.Effort)
		smaller := synthesis(max(128, min(budget/2, estimateTokens(synth))))
		if estimateTokens(smaller) < estimateTokens(synth) {
			fmt.Fprintln(a.Out, "\nsynthesis context was too long; retrying once with shorter result excerpts")
			msg, meta, err = a.streamChatObserved(a.mainProviderCall(ctx), activitySynthesizing, synthModel, smaller, nil, onToken, a.mainProviderProgress(synthModel, a.Effort))
		} else {
			return err
		}
	}
	if err != nil {
		fmt.Fprintln(a.Out)
		a.recordFailedWork("synthesis", meta, a.Effort)
		return err
	}
	fmt.Fprintln(a.Out)
	a.record("synthesis", meta, 0)

	// the main session only records the final answer: valid, compact history
	a.Sess.AppendMessage(provider.Message{Role: "assistant", Content: msg.Content})
	a.completeMainProviderCall()
	a.saveFor(saveAssistantMessage)
	if err := a.saveProviderToolFailure(ctx, vendorToolFailure); err != nil {
		return err
	}
	a.footer(meta)
	// The footer reports the synthesis call. What the user actually spent is
	// the whole run, and that number exists nowhere else.
	if total := a.runSpend.total(); total > 0 {
		fmt.Fprintf(a.Out, "%s  run total: $%.2f across %d tasks%s\n", colorDim, total, len(tasks), colorReset)
	}
	return nil
}

// announcePlan prints the plan as it will run, and names once where its
// writers run: each in a tree of its own (plan 36) or one at a time in the
// shared tree. A plan with no writer says nothing about trees.
func (a *Agent) announcePlan(tasks []Task) {
	fmt.Fprintf(a.Out, "%s◆ plan (%d tasks):%s\n", colorMag, len(tasks), colorReset)
	for i, task := range tasks {
		fmt.Fprintf(a.Out, "%s  %d. %s%s%s\n", colorDim, i+1, task.Title, task.annotation(), colorReset)
	}
	writers := 0
	for _, task := range tasks {
		if writesFiles(task.Kind) {
			writers++
		}
	}
	switch {
	case writers == 0:
	case a.Isolator != nil:
		fmt.Fprintf(a.Out, "%s  each writer in its own tree, landed when it finishes%s\n", colorDim, colorReset)
	default:
		fmt.Fprintf(a.Out, "%s  one tree, writers one at a time%s\n", colorDim, colorReset)
	}
}

// runTasks delegates the plan and returns what became of each task.
//
// Tasks run as soon as everything they need is resolved, up to a small
// concurrency limit. A run reports its failures; it does not vanish. Aborting
// on the first error discards results that already cost money. The only thing
// that stops a run is the user cancelling it, which is not a failure to report.
func (a *Agent) runTasks(ctx context.Context, userInput string, tasks []Task) ([]outcome, error) {
	if pauseGate(ctx) == nil {
		ctx = context.WithValue(ctx, executionPauseKey{}, &executionPause{})
	}
	gate := pauseGate(ctx)
	// Resolve any direct caller's unbound tasks here, before goroutines start.
	// Normal plans were already resolved for their announcement. No child
	// reads discovery or connector state while another child is opening.
	var unresolved Roster
	for i := range tasks {
		if tasks[i].Model != "" {
			continue
		}
		if len(unresolved.Rungs) == 0 {
			unresolved = a.roster(a.RungAvailable)
		}
		a.resolveTask(&tasks[i], unresolved)
	}
	outcomes := make([]outcome, len(tasks))
	results := make([]string, len(tasks))
	resolved := make([]bool, len(tasks))
	started := make([]bool, len(tasks))
	childTurns := make([]string, len(tasks))

	// Announce the whole plan before any goroutine can start. A live surface
	// gets stable plan order immediately, and a task that never runs because a
	// dependency or budget blocks it still has one durable identity and verdict.
	for index := range tasks {
		childTurns[index] = xid.New(xid.Turn)
		model := tasks[index].Model
		effort := a.taskEffort(tasks[index])
		status := a.queueSubagentStatus(tasks, index, childTurns[index], model, effort)
		a.notifySubagent(status)
		if step := dependencyWaitStep(tasks[index]); step != "" {
			a.updateSubagentStatus(index, SubagentWaiting, SubagentPhaseSchedule, step)
		}
	}
	if saved := a.executionSnapshot(); saved != nil && len(saved.Tasks) == len(tasks) {
		for i, task := range saved.Tasks {
			if prior, settled := executionOutcome(task); settled {
				outcomes[i], results[i], resolved[i], started[i] = prior, prior.Result, true, true
				a.setExecutionTaskState(i, continuity.TaskSettled)
			}
		}
	}

	// With a screen attached the agents have a window of their own, and the
	// transcript gets a summary: one line now, one when they finish. Without
	// one, the play-by-play below is the only place the run is visible.
	if a.liveSurface() {
		fmt.Fprintf(a.Out, "%s◆ %d agents deployed (%s)%s\n", colorMag, len(tasks), kindsOf(tasks), colorReset)
	}

	limit := a.concurrencyLimit()
	// One lock over the user's tree for the run. A writer that could not be
	// isolated holds it for its whole run; a landing holds it for the apply.
	// With no Isolator the scheduler below serialises writers itself and the
	// lock is never contended.
	tree := &sync.Mutex{}
	// Buffered to the number of tasks, so a sender never blocks. runTasks
	// returns from inside its loop when the user cancels, and with an
	// unbuffered channel every goroutine that had not yet delivered its result
	// blocked on the send forever. That leaks a goroutine today, and a vendor
	// child process per in-flight task once a subagent owns one, because the
	// deferred Close never runs.
	finished := make(chan taskRun, len(tasks))
	reports := make([]taskRun, len(tasks))
	reportReady := make([]bool, len(tasks))
	running, writing := 0, false
	budgetLimit := false
	var retainedStop error

	for {
		if running == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := a.retireRecovery(ctx); err != nil {
				return outcomes, err
			}
		}
		// Launch everything that can go now. Resolving a task without running
		// it — blocked, over budget — can unblock the next one, so this
		// sweeps until nothing more is ready.
		for running < limit && gate.stopped() == nil && !gate.needsRecovery() && retainedStop == nil {
			index, launch, ok := a.nextRunnable(tasks, outcomes, resolved, started, writing)
			if !ok {
				break
			}
			started[index] = true
			if !launch {
				// Budget exhaustion admits no more work. Finish resolving its
				// skipped tasks, then save their shared boundary once.
				budgetLimit = budgetLimit || outcomes[index].Status == statusOverBudget
				resolved[index] = true
				a.setExecutionOutcome(index, outcomes[index])
				a.storeExecution()
				continue
			}
			if a.sharesTree(tasks[index].Kind) {
				writing = true
			}
			running++
			a.runSpend.start()
			a.setExecutionTaskState(index, continuity.TaskRunning)
			go a.runOneTask(ctx, finished, userInput, tasks, results, index, childTurns[index], tree)
		}

		if running == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if gate.needsRecovery() || budgetLimit {
				// One boundary is one recovery point: when a sibling's limit
				// has closed the gate, the pause write that follows saves this
				// boundary, with every task in its final state.
				if a.Sess != nil && gate.stopped() == nil {
					reason := "error"
					if budgetLimit {
						reason = "limit"
					}
					if err := a.saveRecovery(reason); err != nil {
						return outcomes, err
					}
				}
				gate.recovered()
				budgetLimit = false
				continue
			}
			break
		}

		done := <-finished
		running--
		a.runSpend.finish()
		if ctx.Err() != nil {
			// The user asked it to stop. Nothing the remaining tasks report is
			// kept -- that would be answering a question they withdrew -- but
			// the run is not over until every one of them has stopped (V34.2e).
			// Returning earlier leaves goroutines free to write results, end
			// checkpoints, record cost and publish events into a turn the caller
			// has already declared done. Each observes the same cancelled
			// context and comes back.
			for running > 0 {
				<-finished
				running--
				a.runSpend.finish()
			}
			return nil, ctx.Err()
		}
		if a.sharesTree(tasks[done.index].Kind) {
			writing = false
		}
		var unavailable *conversationUnavailable
		if errors.As(done.err, &unavailable) {
			// Its conversation cannot open now. Like a pause, the task stays
			// unresolved with its handle and nothing more is admitted; the run
			// stops once the tasks already running come back.
			if retainedStop == nil {
				retainedStop = unavailable
			}
			a.storeExecution()
			reports[done.index], reportReady[done.index] = done, true
			continue
		}
		if gate.note(done.err) {
			// Keep this task unresolved, drain the other in-flight operations,
			// and leave the rest of the graph queued for the next attempt.
			a.storeExecution()
			reports[done.index], reportReady[done.index] = done, true
			continue
		}

		outcomes[done.index] = a.classify(done.result, done.err)
		results[done.index] = outcomes[done.index].Result
		resolved[done.index] = true
		a.setExecutionOutcome(done.index, outcomes[done.index])
		a.storeExecution()
		a.reportTaskMilestone(tasks, outcomes, done)
		reports[done.index] = done
		reportReady[done.index] = true
		if done.err != nil {
			// This error becomes a task outcome rather than the turn's return.
			// Close admission, drain the children already in flight, and persist
			// that complete boundary before starting the next queued task.
			gate.requestRecovery()
		}
	}
	// A completion milestone belongs to when the task actually resolved; its
	// full private transcript is different. Flush those buffered reports only
	// after delegation, in plan order, so concurrent readers never make a
	// review read like interleaved goroutine scheduling.
	for index := range reports {
		if reportReady[index] {
			a.flushTaskReport(tasks, reports[index])
		}
	}
	if err := gate.stopped(); err != nil {
		a.markExecutionWaiting()
		a.storeExecution()
		for i := range tasks {
			if !resolved[i] {
				a.holdPausedTask(i)
			}
		}
		return outcomes, err
	}
	if retainedStop != nil {
		a.markExecutionWaiting()
		a.storeExecution()
		return outcomes, retainedStop
	}
	if a.liveSurface() {
		fmt.Fprintf(a.Out, "%s◆ %d agents finished: %s%s\n", colorMag, len(tasks), tallyOutcomes(outcomes), colorReset)
	}
	return outcomes, nil
}

// liveSurface reports whether a screen is showing each agent's status as it
// changes, which is when the transcript should carry a summary rather than
// the play-by-play.
func (a *Agent) liveSurface() bool { return a.Subagents != nil }

// kindsOf lists the tasks' kinds in plan order, "task" for one the planner
// did not classify.
func kindsOf(tasks []Task) string {
	kinds := make([]string, 0, len(tasks))
	for _, task := range tasks {
		kind := string(task.Kind)
		if kind == "" {
			kind = "task"
		}
		kinds = append(kinds, kind)
	}
	return strings.Join(kinds, ", ")
}

// tallyOutcomes says how the run ended, counts by status, done first.
func tallyOutcomes(outcomes []outcome) string {
	counts := map[status]int{}
	for _, o := range outcomes {
		counts[o.Status]++
	}
	parts := []string{}
	for _, entry := range []struct {
		status status
		word   string
	}{{statusDone, "completed"}, {statusIncomplete, "incomplete"}, {statusFailed, "failed"}, {statusBlocked, "blocked"}, {statusOverBudget, "over budget"}} {
		if n := counts[entry.status]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, entry.word))
		}
	}
	return strings.Join(parts, ", ")
}

// taskRun is one finished subagent, with everything it printed on the way.
type taskRun struct {
	index  int
	result string
	err    error
	output string
}

// runOneTask runs a subagent into its own buffer.
//
// Buffered rather than streamed because three agents streaming into one
// terminal is unreadable. What a reader needs from a parallel run is to know
// what is happening and to get each task's output whole, not to watch tokens
// arrive from three places at once.
func (a *Agent) runOneTask(ctx context.Context, finished chan<- taskRun, userInput string, tasks []Task, results []string, index int, childTurn string, tree *sync.Mutex) {
	out := a.Out
	var buffered *bytes.Buffer
	if a.concurrencyLimit() > 1 {
		buffered = &bytes.Buffer{}
		out = buffered
	}

	model := tasks[index].Model
	effort := a.taskEffort(tasks[index])
	saved := a.executionTask(index)
	ctx = context.WithValue(ctx, childResumeKey{}, childResume{model: model, handle: saved.ProviderState})
	// A child turn of its own, so a reader can tell one subagent's work from
	// another's and from the parent's. Published around the call rather than
	// inside runSubagent: the event is about the task's lifetime, and the task
	// is what this function owns.
	a.publishSubagentStarted(tasks, index, childTurn, model, effort)

	// A writer gets a tree of its own when the Isolator can give it one (plan
	// 36). When it cannot, the task runs in the shared tree under the run's
	// lock, one writer at a time as before, and its row says why. The tree is
	// released on every path out — a cancelled run included, which is why the
	// release keeps working after the context is done.
	isolated := ""
	keepTree := false
	if saved.Workspace != "" && a.Isolator != nil {
		isolated = saved.Workspace
		tasks[index].Workspace = isolated
	} else if a.Isolator != nil && writesFiles(tasks[index].Kind) {
		a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseCheckpoint, "preparing a tree of its own")
		dir, err := a.Isolator.Isolate(ctx, a.Root, childTurn)
		if err != nil {
			a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseCheckpoint, "shared tree: "+err.Error())
		} else {
			isolated = dir
			tasks[index].Workspace = dir
		}
	}
	// Released before the task reports, like the vendor child below: a result
	// whose tree is still being removed is not a finished task, and a run
	// must not return with worktrees still going away under it. The deferred
	// call covers the early returns; the once makes the second call nothing.
	var releasedTree sync.Once
	releaseTree := func() {
		if isolated == "" || keepTree {
			return
		}
		releasedTree.Do(func() { a.Isolator.Release(context.WithoutCancel(ctx), a.Root, isolated) })
	}
	defer releaseTree()
	if a.Isolator != nil && writesFiles(tasks[index].Kind) && isolated == "" {
		tree.Lock()
		defer tree.Unlock()
	}
	capabilities := a.subagentCapabilities(tasks[index].Kind, model, isolated, tasks[index].Vendor)
	// A snapshot per writing subagent, so a task that makes a mess is
	// rewindable on its own rather than by undoing the whole turn (A33.8).
	// Only writing kinds: research and explain change no files, so a snapshot
	// for one would record a tree identical to the last.
	//
	// Both calls sit inside this function on purpose. No other writer touches
	// the user's tree while this one runs in it, which makes the window
	// between them the only moment when "what changed" means this task alone.
	// A task in its own tree changes the user's tree only when it lands, so
	// its snapshot brackets the landing instead.
	snapshot := -1
	if a.Ckpt != nil && writesFiles(tasks[index].Kind) && isolated == "" {
		if saved.Checkpoint != nil {
			snapshot = *saved.Checkpoint
		} else {
			a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseCheckpoint, "creating rollback checkpoint")
			snapshot = a.Ckpt.BeginTask(ctx, tasks[index].Title)
			a.saveChildCheckpoint(index, snapshot)
		}
	}

	// The task's own provider, opened here because this function already owns
	// the task's lifetime — its child turn, its snapshot, its result. Released
	// on every path out, including the failure below: a provider owns a child
	// process and nothing else will release it.
	a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseProvider, a.subagentOpeningStep(model, capabilities))
	own, release, openErr := a.openSubagentBackend(ctx, model, effort, tasks[index].Kind, isolated, tasks[index].Vendor)
	defer release()

	// A cheaper rung that will not spawn must not lose the task: the work still
	// needs doing, and the model the user selected can always do it. Rung 0 is
	// that model verbatim, so the fallback needs no roster to find it.
	//
	// Announced, never silent. Quietly running on a more expensive model is the
	// exact surprise this feature exists to prevent — the direction being "up
	// to what you already chose" does not make it one to discover later.
	// A task continuing a saved vendor conversation never falls back: another
	// model is another conversation, and the task's own is the only one that
	// knows what it already did.
	if openErr != nil && saved.ProviderState == "" {
		if ceiling := a.SessionModel(); ceiling != "" && ceiling != model {
			fmt.Fprintf(out, "%s  ◆ %s could not start on %s; falling back to %s%s\n",
				colorDim, tasks[index].Title, model, ceiling, colorReset)
			release()
			model = ceiling
			if tasks[index].CeilingEffort != "" {
				effort = tasks[index].CeilingEffort
			}
			// Rebuild the envelope for the fallback model, retaining the
			// plan's discovered provider binding.
			capabilities = a.subagentCapabilities(tasks[index].Kind, model, isolated, tasks[index].Vendor)
			a.updateSubagentStatusRoute(index, model, effort)
			a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseProvider, a.subagentOpeningStep(model, capabilities))
			own, release, openErr = a.openSubagentBackend(ctx, model, effort, tasks[index].Kind, isolated, tasks[index].Vendor)
			defer release()
		}
	}

	// The backend a continuation opened must drive the very conversation the
	// task was saved on. A shared session provider or another handle would
	// take the continuation to a conversation that never saw the task.
	if openErr == nil && saved.ProviderState != "" && !drivesConversation(own, saved.ProviderState) {
		openErr = fmt.Errorf("the provider opened for it does not drive that conversation")
	}
	var result string
	var err error
	tasks[index].Model, tasks[index].Effort = model, effort
	a.saveChildRoute(index, model, effort, isolated, childTurn)
	if openErr != nil {
		// One provider that will not start is not a reason to throw away what
		// the other subagents produced. The task fails; the run does not. And
		// there is no third attempt: the ceiling is the last rung there is.
		err = openErr
		if saved.ProviderState != "" {
			err = &conversationUnavailable{task: index, model: model, err: openErr}
		}
	} else {
		result, err = a.runSubagent(ctx, pinnedBackend{backend: own, model: model}, out, model, effort, buffered == nil, userInput, tasks, results, index)
	}
	// Cleanup may block before this child can report to the scheduler. Close
	// admission as soon as its final error is known, before another sibling's
	// completion can open a slot for queued work.
	// A conversation that cannot open now is held like a pause, not failed:
	// the task keeps its tree, lands nothing and is not an outcome.
	var unavailable *conversationUnavailable
	retained := errors.As(err, &unavailable)
	if err != nil && !retained {
		pauseGate(ctx).requestRecovery()
	}
	paused := pauseGate(ctx).note(err)
	if !paused && !retained && isolated != "" && pauseGate(ctx).stopped() != nil {
		err, paused = pauseGate(ctx).stopped(), true
	}
	held := paused || retained
	keepTree = held && ctx.Err() == nil
	// Closed on every path out. A task that died half-way is exactly the one
	// that leaves a tree nobody asked for, and it is the one worth rewinding.
	if snapshot >= 0 && !held {
		a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseCheckpoint, "recording task changes")
		a.Ckpt.EndTask(ctx, snapshot)
	}
	// What the task did in its own tree lands in the user's, under the run's
	// lock and its own snapshot. A cancelled run lands nothing: the user
	// withdrew the question (V34.2e). A task that failed part-way still lands
	// what it did, as a shared-tree task would have left it, so nothing is
	// lost; a patch that does not fit fails the task and says so.
	if isolated != "" && ctx.Err() == nil && !held {
		a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseCheckpoint, "landing its changes")
		if landErr := a.landTask(ctx, tree, tasks[index].Title, isolated); landErr != nil {
			pauseGate(ctx).requestRecovery()
			if err == nil {
				err = landErr
			} else {
				err = fmt.Errorf("%w; and %w", err, landErr)
			}
		}
	}
	if err != nil && !paused {
		a.updateSubagentStatus(index, SubagentFailed, SubagentPhaseComplete, "failed: "+err.Error())
	}
	// On every path out, including failure. An event that only fires on success
	// leaves a count stuck at a number that never comes down, which is worse
	// than no count at all.
	if paused {
		a.holdSubagentPaused(childTurn, index, model)
	} else {
		a.publishSubagentFinished(childTurn, index, err == nil, model, effort)
	}

	run := taskRun{index: index, result: result, err: err}
	if buffered != nil {
		run.output = buffered.String()
	}
	// Close the backend before reporting: a result whose child is still alive
	// is not a finished task. The deferred release above then has nothing left
	// to do -- it is idempotent -- and stays for the early-return paths.
	release()
	releaseTree()
	finished <- run
}

// nextRunnable finds the next task that can be started or resolved without
// running. launch is false for a task that is being resolved in place.
func (a *Agent) nextRunnable(tasks []Task, outcomes []outcome, resolved, started []bool, writing bool) (index int, launch, ok bool) {
	// Nothing more starts while the tasks in flight could carry the run past
	// its ceiling (V34.2f): wait for one to report, then decide again. Only
	// reached with something running, so the caller's wait cannot hang.
	if a.runSpend.wouldCrossInFlight() {
		return 0, false, false
	}
	for i := range tasks {
		if started[i] || !dependenciesResolved(tasks, resolved, i) {
			continue
		}
		if blocker, blocked := blockedBy(tasks, outcomes, i); blocked {
			outcomes[i] = outcome{Status: statusBlocked, Reason: "blocked: " + blocker + " did not produce a result"}
			a.blockSubagentStatus(i, outcomes[i].Reason)
			fmt.Fprintf(a.Out, "%s◆ subagent %d/%d skipped: %s — %s%s\n",
				colorDim, i+1, len(tasks), tasks[i].Title, outcomes[i].Reason, colorReset)
			return i, false, true
		}
		if a.runSpend.exhausted() {
			outcomes[i] = outcome{
				Status: statusOverBudget,
				Reason: fmt.Sprintf("the run reached its $%.2f budget after $%.2f", a.MaxRunCostUSD, a.runSpend.total()),
			}
			a.blockSubagentStatus(i, "blocked by the run budget")
			fmt.Fprintf(a.Out, "%s◆ stopping at the budget: %s never ran%s\n", colorMag, tasks[i].Title, colorReset)
			return i, false, true
		}
		if writing && writesFiles(tasks[i].Kind) {
			// One working tree. Two agents editing it at once is how a run
			// produces a state neither of them intended.
			a.updateSubagentStatus(i, SubagentWaiting, SubagentPhaseSchedule, "waiting for the shared-tree writer")
			continue
		}
		if !a.liveSurface() {
			fmt.Fprintf(a.Out, "\n%s◆ subagent %d/%d started: %s%s%s\n", colorMag, i+1, len(tasks), tasks[i].Title, tasks[i].annotation(), colorReset)
		}
		return i, true, true
	}
	return 0, false, false
}

func dependencyWaitStep(task Task) string {
	if len(task.Needs) == 0 {
		return ""
	}
	numbers := make([]string, 0, len(task.Needs))
	for _, need := range task.Needs {
		numbers = append(numbers, itoa(need+1))
	}
	label := "tasks "
	if len(numbers) == 1 {
		label = "task "
	}
	return "waiting for " + label + strings.Join(numbers, ", ")
}

// dependenciesResolved reports whether everything a task needs has an outcome.
func dependenciesResolved(tasks []Task, resolved []bool, index int) bool {
	for _, need := range tasks[index].Needs {
		if need < len(resolved) && !resolved[need] {
			return false
		}
	}
	return true
}

// writesFiles reports whether a task may change the working tree.
//
// Only reading kinds are treated as safe. An unlabelled task might write, and
// assuming otherwise would make concurrency a hazard that arrives with a weaker
// planner rather than with a decision anyone made.
// landTask applies one isolated task's work to the user's tree, under the
// run's lock and the per-task snapshot.
func (a *Agent) landTask(ctx context.Context, tree *sync.Mutex, title, dir string) error {
	tree.Lock()
	defer tree.Unlock()
	handle := -1
	if a.Ckpt != nil {
		handle = a.Ckpt.BeginTask(ctx, title)
	}
	err := a.Isolator.Land(ctx, a.Root, dir)
	if handle >= 0 {
		a.Ckpt.EndTask(ctx, handle)
	}
	if err != nil {
		return fmt.Errorf("did not land: %w", err)
	}
	return nil
}

// sharesTree reports whether a task of this kind holds the shared tree for
// the scheduler's purposes: a writer with no Isolator. A writer with one
// either gets its own tree or takes the run's lock itself.
func (a *Agent) sharesTree(kind Kind) bool {
	return a.Isolator == nil && writesFiles(kind)
}

func writesFiles(kind Kind) bool {
	return kind != KindResearch && kind != KindExplain
}

// reportTaskMilestone reports the observed resolution immediately. The full
// task transcript stays private until flushTaskReport can print every child in
// stable plan order.
func (a *Agent) reportTaskMilestone(tasks []Task, outcomes []outcome, done taskRun) {
	switch outcomes[done.index].Status {
	case statusFailed:
		fmt.Fprintf(a.Out, "%s✗ %s failed: %s%s\n", colorDim, tasks[done.index].Title, outcomes[done.index].Reason, colorReset)
	case statusIncomplete:
		fmt.Fprintf(a.Out, "%s! %s did not finish; keeping what it reached%s\n", colorDim, tasks[done.index].Title, colorReset)
	default:
		// A screen with the agents' window sees this row turn green; the
		// transcript's one line at the end says how many did.
		if !a.liveSurface() {
			fmt.Fprintf(a.Out, "%s◆ subagent %d/%d completed: %s%s\n", colorDim,
				done.index+1, len(tasks), tasks[done.index].Title, colorReset)
		}
	}
	a.noteRunCost()
}

// flushTaskReport writes only an already-buffered child transcript. It runs
// after all delegation outcomes are known, in plan order.
func (a *Agent) flushTaskReport(tasks []Task, done taskRun) {
	if done.output == "" || a.liveSurface() {
		return
	}
	fmt.Fprintf(a.Out, "\n%s◆ subagent %d/%d %s:%s\n", colorMag, done.index+1, len(tasks), tasks[done.index].Title, colorReset)
	fmt.Fprint(a.Out, done.output)
}

// concurrencyLimit is how many tasks may run at once.
func (a *Agent) concurrencyLimit() int {
	if a.MaxConcurrentTasks > 0 {
		return a.MaxConcurrentTasks
	}
	return DefaultConcurrentTasks
}

// DefaultConcurrentTasks is three: small enough that the output of that many
// agents can still be read, and rate limits rather than CPU are what binds.
//
// Exported so a surface reporting the default reads it from the package that
// applies it. internal/config sits below this one and cannot import it, so the
// literal in config/settings.go is a deliberate duplicate — this comment is
// where anyone changing the number finds out about the other copy.
const DefaultConcurrentTasks = 3

// noteRunCost shows what the run has spent so far.
//
// Visibility is most of the value here. A ceiling only helps someone who
// already decided on a number; the running total is what tells everyone else
// whether they should.
func (a *Agent) noteRunCost() {
	total := a.runSpend.total()
	// A subscription is not billed by the dollar; its measure is the plan's
	// windows, which the status meters show.
	if total <= 0 || a.sessionSpend.billingMode() == provider.BillingSubscription {
		return
	}
	if a.MaxRunCostUSD > 0 {
		fmt.Fprintf(a.Out, "%s  run so far: $%.2f of $%.2f%s\n", colorDim, total, a.MaxRunCostUSD, colorReset)
		return
	}
	fmt.Fprintf(a.Out, "%s  run so far: $%.2f%s\n", colorDim, total, colorReset)
}

// plannerPrompt is what the planner is asked for, kept apart from the request
// so a test can read it.
//
// It names no model, and must not: the point of asking for a level is that the
// planner cannot name something above the user's ceiling. A model name in this
// string would be the one place that guarantee leaks, which is why the test
// checks it against the ladders themselves rather than a hardcoded list.
func decompositionPrompt(maxTasks int) string {
	width := "as many concrete, self-contained tasks as the request needs"
	if maxTasks > 0 {
		width = fmt.Sprintf("at most %d concrete, self-contained tasks", maxTasks)
	}
	return fmt.Sprintf(`Decompose the request below into %s for coding subagents that have file and shell access but cannot talk to each other. If the request is trivial or a single step, return a single task.

Respond with ONLY a JSON array. No prose, no markdown fences. Each element is an object:

  {"title": "what to do", "kind": "edit", "level": "routine", "needs": [1]}

"kind" is one of: edit, test, research, explain, design, boilerplate. Omit it if none fits.
"level" is one of: trivial, routine, hard - how much capability the task needs, not how
important it is. trivial: mechanical; the answer is obvious once you look. routine: ordinary
implementation or analysis. hard: needs real reasoning, is subtle, or the rest of the plan
depends on getting it right. Omit it when unsure.
"needs" lists the task numbers (counting from 1) whose results this task actually requires.
Omit "needs" or use [] when the task stands alone - do not list a task merely because it
comes earlier. Keep each task meaningful; avoid splitting work merely to create more agents.`, width)
}

// plan asks the planner for a strict-JSON task list.
//
// The reply is asked for as objects and accepted as either: a planner that
// sends the flat array of strings this used to require still produces a
// working plan, it just produces one that cannot be routed.
// plan asks the planner for tasks. With none it could read, the string says
// why, so the run does not announce a single step the planner never chose.
func (a *Agent) plan(ctx context.Context, model, userInput string, maxTasks int) ([]Task, string, provider.Meta, error) {
	return a.planContinuing(ctx, model, userInput, maxTasks, false)
}

// planContinuing is plan, continuing the planner call a stopped run was
// making instead of sending it again when continuing is set.
func (a *Agent) planContinuing(ctx context.Context, model, userInput string, maxTasks int, continuing bool) ([]Task, string, provider.Meta, error) {
	budget := briefingBudget(a.orchestrationWindow(model))
	planning := func(budget int) []provider.Message {
		msgs := a.planningMessages(userInput, maxTasks, budget)
		if continuing {
			msgs = appendMessage(msgs, continuationMessage())
		}
		return msgs
	}
	msgs := planning(budget)
	msg, meta, err := a.streamChatObserved(a.mainProviderCall(ctx), activityPlanning, model, msgs, nil, a.appendMainPartial,
		a.mainProviderProgress(model, a.Effort))
	if provider.IsContextOverflow(err) {
		a.recordFailedWork("planner", meta, a.Effort)
		smaller := planning(max(128, min(budget/2, estimateTokens(msgs))))
		if estimateTokens(smaller) < estimateTokens(msgs) {
			fmt.Fprintln(a.Out, "planning context was too long; retrying once with shorter history excerpts")
			msg, meta, err = a.streamChatObserved(a.mainProviderCall(ctx), activityPlanning, model, smaller, nil, a.appendMainPartial, a.mainProviderProgress(model, a.Effort))
		} else {
			return nil, "", provider.Meta{}, err // Already accounted above.
		}
	}
	if err != nil {
		return nil, "", meta, err
	}
	tasks := parseTasks(msg.Content, maxTasks)
	if len(tasks) <= 1 {
		return tasks, planProblem(msg.Content, len(tasks)), meta, nil
	}
	return tasks, "", meta, nil
}

// runSubagent executes one task in an isolated context: its conversation
// never enters the main session, only its final summary does.
func (a *Agent) runSubagent(ctx context.Context, pinned pinnedBackend, out io.Writer, model, effort string, tokensVisible bool, original string, tasks []Task, results []string, idx int) (string, error) {
	ctx = context.WithValue(ctx, providerToolFailureKey{}, func() { pauseGate(ctx).requestRecovery() })
	ctx = context.WithValue(ctx, workAgentKey{}, idx+1)
	ctx = provider.WithEffort(ctx, effort)
	capabilities := a.subagentCapabilities(tasks[idx].Kind, model, tasks[idx].Workspace, tasks[idx].Vendor)
	cwd := capabilities.Workspace
	if cwd == "" {
		cwd = workingDir()
	}
	network := "disabled"
	if capabilities.NetworkAccess {
		network = "enabled"
	}
	var briefing strings.Builder
	fmt.Fprintf(&briefing, `You are subagent %d of %d in an orchestrated run on %s (working directory %s; network access %s). You have tools to read/write/edit files, list directories, and run shell commands. Complete ONLY your assigned task, then reply with a short result summary (what you did, key outputs, paths touched). Be efficient: few tool calls, no exploration beyond the task.

Overall request: %s
`, idx+1, len(tasks), runtime.GOOS, cwd, network, original)
	baseBriefing := briefing.String()
	dependencyBudget := briefingBudget(a.childWindow(pinned, model))
	briefing.WriteString(dependencyBriefingWithin(tasks, results, idx, dependencyBudget))

	msgs := []provider.Message{
		{Role: "system", Content: briefing.String()},
		{Role: "user", Content: "Your task: " + tasks[idx].Title},
	}
	saved := a.executionTask(idx)
	if len(saved.Messages) > 0 {
		msgs = saved.Messages
	}

	maxRounds := MaxRoundsFor(ModeCode, effort)
	// A subagent gets its own counter: two children repeating different calls
	// are two pieces of work, not one loop.
	loop := restoredLoop(saved.Loop)
	round := saved.Rounds
	continuing := saved.ProviderState != ""
	overflowRecovered := false
	_, providerOwned := pinned.backend.(interface{ ProviderHandle() string })
	window := a.childWindow(pinned, model)
	defer func() { a.saveChildConversation(idx, msgs, round, loop, pinned.backend) }()
	// Beside other agents this child's transcript is a buffer, and a live
	// surface never prints it (it shows each agent's status instead). What a
	// person must see (a compaction, and above all one that could not be
	// saved) goes to the surface itself there.
	notices := out
	if a.liveSurface() {
		notices = a.Out
	}
	for {
		if last := msgs[len(msgs)-1]; last.Role == "assistant" && len(last.ToolCalls) == 0 {
			return strings.TrimSpace(last.Content), nil
		}
		calls := pendingToolCalls(msgs)
		if len(calls) == 0 {
			if round >= maxRounds {
				break
			}
			if !providerOwned && MeasureContext(window, 0, msgs).ShouldCompact() {
				a.saveChildConversation(idx, msgs, round, loop, pinned.backend)
				msgs, _ = a.compactChild(idx, msgs, window/2, notices)
			}
			request := msgs
			if continuing {
				request = appendMessage(msgs, provider.Message{Role: "user", Content: "The previous attempt stopped before this task finished. Continue the unfinished assigned task in this saved conversation from its current state. Keep the original goal and completed work; do not repeat completed actions."})
			}
			sending := onProviderCallStart(ctx, func() { a.beginChildProviderCall(idx) })
			msg, meta, err := a.streamChatOnObserved(sending, pinned, activityWorking, model, request, a.toolsFor(ctx, ModeCode), func(tok string) {
				a.appendChildPartial(idx, tok)
				fmt.Fprint(out, tok)
			}, tokensVisible, a.subagentProviderProgress(idx))
			if err != nil {
				a.recordFailedWork("subagent", meta, effort)
				fmt.Fprintln(out)
				if !providerOwned && !overflowRecovered && provider.IsContextOverflow(err) {
					overflowRecovered = true
					a.saveChildConversation(idx, msgs, round, loop, pinned.backend)
					var changed bool
					msgs, changed = a.compactChild(idx, msgs, overflowTarget(window, msgs), notices)
					if !changed && strings.HasPrefix(msgs[0].Content, baseBriefing) {
						candidate := append([]provider.Message(nil), msgs...)
						candidate[0].Content = baseBriefing + dependencyBriefingWithin(tasks, results, idx, max(128, min(dependencyBudget/2, estimateTokens(msgs))))
						if freed := estimateTokens(msgs) - estimateTokens(candidate); freed > 0 {
							msgs, changed = a.retainChildCompaction(idx, msgs, Compaction{Messages: candidate, Replaced: 1, FreedTokens: freed}, notices)
						}
					}
					if changed {
						fmt.Fprintf(notices, "agent %d: request was too long; retrying once with the smaller context\n", idx+1)
						continue
					}
				}
				return "", err
			}
			continuing = false
			overflowRecovered = false
			fmt.Fprintln(out)
			a.recordAtEffort("subagent", meta, len(msg.ToolCalls), effort)
			msgs = append(msgs, msg)
			a.completeChildProviderCall(idx)
			round++
			if len(msg.ToolCalls) == 0 {
				return strings.TrimSpace(msg.Content), nil
			}
			calls = msg.ToolCalls
		}
		for _, tc := range calls {
			if err := pauseGate(ctx).stopped(); err != nil {
				return "", err
			}
			owner := a.subagentToolWork(idx)
			a.publishKolkToolRequested(tc, owner)
			var result string
			var err error
			executed := false
			skipped := false
			if loop.wouldRepeat(tc.Function.Name, tc.Function.Arguments) {
				denial, stop := a.answerDoomLoop(ctx, &loop, tc, true)
				if stop != nil {
					return "", stop
				}
				result = denial
				skipped = result != ""
			}
			if result == "" {
				executed = true
				a.publishKolkToolStarted(tc, owner)
				result, err = a.executeSubagentTool(ctx, tc, out, effort, tasks[idx].Workspace)
				if err != nil {
					pauseGate(ctx).requestRecovery()
					result = "Error: " + err.Error()
				}
				loop.observe(tc.Function.Name, tc.Function.Arguments, result)
			}
			if skipped {
				a.publishKolkToolSkipped(tc, owner)
			}
			a.publishKolkToolOutput(tc, result, owner)
			if executed {
				a.publishKolkToolFinished(tc, err, owner)
			}
			msgs = append(msgs, provider.Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
		a.markChildToolBoundary(idx)
	}
	// Not an empty result: whatever the last round produced is what this task
	// reached, and it is worth more than nothing to the synthesis.
	return lastText(msgs), fmt.Errorf("%w (%d rounds)", errRoundsExhausted, maxRounds)
}

// lastText is the most recent thing the subagent actually said, which is the
// closest thing to a partial result an unfinished task has.
func lastText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			return strings.TrimSpace(msgs[i].Content)
		}
	}
	return ""
}

func workingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "?"
	}
	return cwd
}
