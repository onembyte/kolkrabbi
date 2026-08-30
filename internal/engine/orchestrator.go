package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/tools"
	"github.com/onembyte/kolkrabbi/internal/xid"
)

// MaxTasksForEffort is orchestration width for one effort level.
//
// Exported solely so the external test package can assert the width table,
// which is behaviour worth pinning and unreachable from outside otherwise. It
// is listed in arch.DeadExportAllowlist for exactly that reason rather than
// being mistaken for something production calls.
func MaxTasksForEffort(effort string) int { return maxTasksFor(effort) }

func maxTasksFor(effort string) int {
	eff, ok := NormalizeEffort(effort)
	if !ok {
		eff = EffortMedium
	}
	switch eff {
	case EffortLow:
		return 1
	case EffortHigh:
		return 4
	case EffortMax:
		return 6
	default: // EffortMedium
		return 2
	}
}

// runOrchestrated is agent mode: a planner decomposes the request into
// tasks, each task runs in an isolated subagent with its own context (zero
// context cost to the main conversation), and a synthesis call produces the
// final answer. The main session only ever sees user input -> final answer,
// so its history stays small and valid.
func (a *Agent) runOrchestrated(ctx context.Context, userInput string) error {
	a.Sess.AppendMessage(provider.Message{Role: "user", Content: userInput})
	a.save()

	model := a.orchestrationModel()
	maxTasks := maxTasksFor(a.Effort)

	// One accounting scope per run. Cleared afterwards so an ordinary turn is
	// never charged against an orchestration ceiling.
	a.runSpend = &spend{limit: a.MaxRunCostUSD}
	defer func() { a.runSpend = nil }()

	// ---- 1. plan ----
	fmt.Fprintf(a.Out, "%s◆ planning (%s)…%s\n", colorMag, model, colorReset)
	tasks, meta, err := a.plan(ctx, model, userInput, maxTasks)
	if err != nil {
		return err
	}
	a.record("planner", meta, 0)

	if len(tasks) <= 1 {
		// not worth orchestrating: degrade gracefully to the normal loop,
		// reusing the user message we already appended.
		fmt.Fprintf(a.Out, "%s◆ single-step task, running directly%s\n", colorMag, colorReset)
		msgs := a.Sess.GetMessages()
		if len(msgs) > 0 {
			a.Sess.SetMessages(msgs[:len(msgs)-1])
		}
		a.save()
		return a.runLoop(ctx, userInput)
	}

	// Routing is resolved before anything is printed, so the plan a person
	// reads is the plan that runs, models included.
	a.assignModels(tasks)

	fmt.Fprintf(a.Out, "%s◆ plan (%d tasks):%s\n", colorMag, len(tasks), colorReset)
	for i, task := range tasks {
		fmt.Fprintf(a.Out, "%s  %d. %s%s%s\n", colorDim, i+1, task.Title, task.annotation(), colorReset)
	}

	// ---- 2. delegate ----
	outcomes, err := a.runTasks(ctx, userInput, tasks)
	if err != nil {
		return err
	}

	// ---- 3. synthesize ----
	if failures := countFailures(outcomes); failures > 0 {
		fmt.Fprintf(a.Out, "\n%s◆ %d of %d tasks did not finish — the answer below is partial%s\n",
			colorMag, failures, len(tasks), colorReset)
	}
	fmt.Fprintf(a.Out, "\n%s◆ synthesizing%s\n", colorMag, colorReset)
	var sb strings.Builder
	sb.WriteString("Original request:\n" + userInput + "\n\nTasks and what became of them:\n")
	sb.WriteString(summarise(tasks, outcomes))
	sb.WriteString("\nWrite the final answer to the original request based on this work. Be concise; report what was done, key findings, and anything the user must know. Do not repeat raw task output verbatim.")
	if failures := countFailures(outcomes); failures > 0 {
		// The reader cannot see the task list. If the answer does not say what
		// is missing from it, nothing will.
		fmt.Fprintf(&sb, "\n\nIMPORTANT: %d of %d tasks failed, were blocked, or were stopped by the run's budget. Say plainly what could not be done and what that leaves uncertain. Do not present the answer as complete.", failures, len(tasks))
	}

	synth := []provider.Message{
		{Role: "system", Content: "You are the orchestrator's synthesis step. You produce the final user-facing answer from completed subagent work."},
		{Role: "user", Content: sb.String()},
	}
	fmt.Fprintf(a.Out, "%s%s%s ", colorCyan, a.responseLabel(), colorReset)
	msg, meta, err := a.streamChat(ctx, activitySynthesizing, model, synth, nil, func(tok string) {
		fmt.Fprint(a.Out, tok)
	})
	if err != nil {
		fmt.Fprintln(a.Out)
		return err
	}
	fmt.Fprintln(a.Out)
	a.record("synthesis", meta, 0)

	// the main session only records the final answer: valid, compact history
	a.Sess.AppendMessage(provider.Message{Role: "assistant", Content: msg.Content})
	a.save()
	a.footer(meta)
	// The footer reports the synthesis call. What the user actually spent is
	// the whole run, and that number exists nowhere else.
	if total := a.runSpend.total(); total > 0 {
		fmt.Fprintf(a.Out, "%s  run total: $%.2f across %d tasks%s\n", colorDim, total, len(tasks), colorReset)
	}
	return nil
}

// runTasks delegates the plan and returns what became of each task.
//
// Tasks run as soon as everything they need is resolved, up to a small
// concurrency limit. A run reports its failures; it does not vanish. Aborting
// on the first error discards results that already cost money. The only thing
// that stops a run is the user cancelling it, which is not a failure to report.
func (a *Agent) runTasks(ctx context.Context, userInput string, tasks []Task) ([]outcome, error) {
	outcomes := make([]outcome, len(tasks))
	results := make([]string, len(tasks))
	resolved := make([]bool, len(tasks))
	started := make([]bool, len(tasks))

	limit := a.concurrencyLimit()
	// Buffered to the number of tasks, so a sender never blocks. runTasks
	// returns from inside its loop when the user cancels, and with an
	// unbuffered channel every goroutine that had not yet delivered its result
	// blocked on the send forever. That leaks a goroutine today, and a vendor
	// child process per in-flight task once a subagent owns one, because the
	// deferred Close never runs.
	finished := make(chan taskRun, len(tasks))
	running, writing := 0, false

	for {
		// Launch everything that can go now. Resolving a task without running
		// it — blocked, over budget — can unblock the next one, so this
		// sweeps until nothing more is ready.
		for running < limit {
			index, launch, ok := a.nextRunnable(tasks, outcomes, resolved, started, writing)
			if !ok {
				break
			}
			started[index] = true
			if !launch {
				resolved[index] = true
				continue
			}
			if writesFiles(tasks[index].Kind) {
				writing = true
			}
			running++
			go a.runOneTask(ctx, finished, userInput, tasks, results, index)
		}

		if running == 0 {
			break
		}

		done := <-finished
		running--
		if ctx.Err() != nil {
			// The user asked it to stop. Draining the rest and reporting them
			// as failures would be answering a question they withdrew.
			return nil, ctx.Err()
		}
		if writesFiles(tasks[done.index].Kind) {
			writing = false
		}

		outcomes[done.index] = a.classify(done.result, done.err)
		results[done.index] = outcomes[done.index].Result
		resolved[done.index] = true
		a.reportTask(tasks, outcomes, done)
	}
	return outcomes, nil
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
func (a *Agent) runOneTask(ctx context.Context, finished chan<- taskRun, userInput string, tasks []Task, results []string, index int) {
	out := a.Out
	var buffered *bytes.Buffer
	if a.concurrencyLimit() > 1 {
		buffered = &bytes.Buffer{}
		out = buffered
	}

	model := tasks[index].Model
	if model == "" {
		// Routing normally happens before the run; resolving here as well
		// keeps a task that arrived without one from asking for an empty model.
		model = a.modelForKind(tasks[index].Kind)
	}
	// A child turn of its own, so a reader can tell one subagent's work from
	// another's and from the parent's. Published around the call rather than
	// inside runSubagent: the event is about the task's lifetime, and the task
	// is what this function owns.
	childTurn := xid.New(xid.Turn)
	a.publishSubagentStarted(tasks, index, childTurn)
	// A snapshot per writing subagent, so a task that makes a mess is
	// rewindable on its own rather than by undoing the whole turn (A33.8).
	// Only writing kinds: research and explain change no files, so a snapshot
	// for one would record a tree identical to the last.
	//
	// Both calls sit inside this function on purpose. The scheduler will not
	// start another writer until this one returns, which makes the window
	// between them the only moment when "what changed" means this task alone.
	snapshot := -1
	if a.Ckpt != nil && writesFiles(tasks[index].Kind) {
		snapshot = a.Ckpt.BeginTask(ctx, tasks[index].Title)
	}

	// The task's own provider, opened here because this function already owns
	// the task's lifetime — its child turn, its snapshot, its result. Released
	// on every path out, including the failure below: a provider owns a child
	// process and nothing else will release it.
	own, release, openErr := a.openSubagentBackend(ctx, model)
	defer release()

	// A cheaper rung that will not spawn must not lose the task: the work still
	// needs doing, and the model the user selected can always do it. Rung 0 is
	// that model verbatim, so the fallback needs no roster to find it.
	//
	// Announced, never silent. Quietly running on a more expensive model is the
	// exact surprise this feature exists to prevent — the direction being "up
	// to what you already chose" does not make it one to discover later.
	if openErr != nil {
		if ceiling := a.SessionModel(); ceiling != "" && ceiling != model {
			fmt.Fprintf(a.Out, "%s  ◆ %s could not start on %s; falling back to %s%s\n",
				colorDim, tasks[index].Title, model, ceiling, colorReset)
			release()
			model = ceiling
			own, release, openErr = a.openSubagentBackend(ctx, model)
			defer release()
		}
	}

	var result string
	var err error
	if openErr != nil {
		// One provider that will not start is not a reason to throw away what
		// the other subagents produced. The task fails; the run does not. And
		// there is no third attempt: the ceiling is the last rung there is.
		err = openErr
	} else {
		result, err = a.runSubagent(ctx, pinnedBackend{backend: own, model: model}, out, model, userInput, tasks, results, index)
	}
	// Closed on every path out. A task that died half-way is exactly the one
	// that leaves a tree nobody asked for, and it is the one worth rewinding.
	if snapshot >= 0 {
		a.Ckpt.EndTask(ctx, snapshot)
	}
	// On every path out, including failure. An event that only fires on success
	// leaves a count stuck at a number that never comes down, which is worse
	// than no count at all.
	a.publishSubagentFinished(childTurn, index, err == nil, model)

	run := taskRun{index: index, result: result, err: err}
	if buffered != nil {
		run.output = buffered.String()
	}
	finished <- run
}

// nextRunnable finds the next task that can be started or resolved without
// running. launch is false for a task that is being resolved in place.
func (a *Agent) nextRunnable(tasks []Task, outcomes []outcome, resolved, started []bool, writing bool) (index int, launch, ok bool) {
	for i := range tasks {
		if started[i] || !dependenciesResolved(tasks, resolved, i) {
			continue
		}
		if blocker, blocked := blockedBy(tasks, outcomes, i); blocked {
			outcomes[i] = outcome{Status: statusBlocked, Reason: "blocked: " + blocker + " did not produce a result"}
			fmt.Fprintf(a.Out, "%s◆ subagent %d/%d skipped: %s — %s%s\n",
				colorDim, i+1, len(tasks), tasks[i].Title, outcomes[i].Reason, colorReset)
			return i, false, true
		}
		if a.runSpend.exhausted() {
			outcomes[i] = outcome{
				Status: statusOverBudget,
				Reason: fmt.Sprintf("the run reached its $%.2f budget after $%.2f", a.MaxRunCostUSD, a.runSpend.total()),
			}
			fmt.Fprintf(a.Out, "%s◆ stopping at the budget: %s never ran%s\n", colorMag, tasks[i].Title, colorReset)
			return i, false, true
		}
		if writing && writesFiles(tasks[i].Kind) {
			// One working tree. Two agents editing it at once is how a run
			// produces a state neither of them intended.
			continue
		}
		fmt.Fprintf(a.Out, "\n%s◆ subagent %d/%d started: %s%s%s\n", colorMag, i+1, len(tasks), tasks[i].Title, tasks[i].annotation(), colorReset)
		return i, true, true
	}
	return 0, false, false
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
func writesFiles(kind Kind) bool {
	return kind != KindResearch && kind != KindExplain
}

// reportTask prints one finished task: its buffered output, then its verdict.
func (a *Agent) reportTask(tasks []Task, outcomes []outcome, done taskRun) {
	if done.output != "" {
		fmt.Fprintf(a.Out, "\n%s◆ subagent %d/%d %s:%s\n", colorMag, done.index+1, len(tasks), tasks[done.index].Title, colorReset)
		fmt.Fprint(a.Out, done.output)
	}
	switch outcomes[done.index].Status {
	case statusFailed:
		fmt.Fprintf(a.Out, "%s✗ %s failed: %s%s\n", colorDim, tasks[done.index].Title, outcomes[done.index].Reason, colorReset)
	case statusIncomplete:
		fmt.Fprintf(a.Out, "%s! %s did not finish; keeping what it reached%s\n", colorDim, tasks[done.index].Title, colorReset)
	}
	a.noteRunCost()
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
	if total <= 0 {
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
	return fmt.Sprintf(`Decompose the request below into at most %d concrete, self-contained tasks for coding subagents that have file and shell access but cannot talk to each other. If the request is trivial or a single step, return a single task.

Respond with ONLY a JSON array. No prose, no markdown fences. Each element is an object:

  {"title": "what to do", "kind": "edit", "level": "routine", "needs": [1]}

"kind" is one of: edit, test, research, explain, design, boilerplate. Omit it if none fits.
"level" is one of: trivial, routine, hard - how much capability the task needs, not how
important it is. trivial: mechanical; the answer is obvious once you look. routine: ordinary
implementation or analysis. hard: needs real reasoning, is subtle, or the rest of the plan
depends on getting it right. Omit it when unsure.
"needs" lists the task numbers (counting from 1) whose results this task actually requires.
Omit "needs" or use [] when the task stands alone - do not list a task merely because it
comes earlier.`, maxTasks)
}

// plan asks the planner for a strict-JSON task list.
//
// The reply is asked for as objects and accepted as either: a planner that
// sends the flat array of strings this used to require still produces a
// working plan, it just produces one that cannot be routed.
func (a *Agent) plan(ctx context.Context, model, userInput string, maxTasks int) ([]Task, provider.Meta, error) {
	prompt := decompositionPrompt(maxTasks) + "\n\nRequest:\n" + userInput

	msgs := []provider.Message{
		{Role: "system", Content: "You are a planning module. You output only strict JSON."},
		{Role: "user", Content: prompt},
	}
	msg, meta, err := a.streamChat(ctx, activityPlanning, model, msgs, nil, nil)
	if err != nil {
		return nil, meta, err
	}
	return parseTasks(msg.Content, maxTasks), meta, nil
}

// runSubagent executes one task in an isolated context: its conversation
// never enters the main session, only its final summary does.
func (a *Agent) runSubagent(ctx context.Context, pinned pinnedBackend, out io.Writer, model, original string, tasks []Task, results []string, idx int) (string, error) {
	cwd := workingDir()
	var briefing strings.Builder
	fmt.Fprintf(&briefing, `You are subagent %d of %d in an orchestrated run on %s (working directory %s). You have tools to read/write/edit files, list directories, and run shell commands. Complete ONLY your assigned task, then reply with a short result summary (what you did, key outputs, paths touched). Be efficient: few tool calls, no exploration beyond the task.

Overall request: %s
`, idx+1, len(tasks), runtime.GOOS, cwd, original)
	briefing.WriteString(dependencyBriefing(tasks, results, idx))

	msgs := []provider.Message{
		{Role: "system", Content: briefing.String()},
		{Role: "user", Content: "Your task: " + tasks[idx].Title},
	}

	maxRounds := MaxRoundsFor(ModeCode, a.Effort)
	// A subagent gets its own counter: two children repeating different calls
	// are two pieces of work, not one loop.
	var loop doomLoop
	for round := 0; round < maxRounds; round++ {
		msg, meta, err := a.streamChatOn(ctx, pinned, activityWorking, model, msgs, tools.Definitions(), func(tok string) {
			fmt.Fprint(out, tok)
		})
		if err != nil {
			fmt.Fprintln(out)
			return "", err
		}
		fmt.Fprintln(out)
		a.record("subagent", meta, len(msg.ToolCalls))
		msgs = append(msgs, msg)

		if len(msg.ToolCalls) == 0 {
			return strings.TrimSpace(msg.Content), nil
		}
		for _, tc := range msg.ToolCalls {
			var result string
			if loop.wouldRepeat(tc.Function.Name, tc.Function.Arguments) {
				denial, stop := a.answerDoomLoop(ctx, &loop, tc, true)
				if stop != nil {
					return "", stop
				}
				result = denial
			}
			if result == "" {
				var err error
				result, err = a.executeSubagentTool(ctx, tc, out)
				if err != nil {
					result = "Error: " + err.Error()
				}
				loop.observe(tc.Function.Name, tc.Function.Arguments, result)
			}
			msgs = append(msgs, provider.Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
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
