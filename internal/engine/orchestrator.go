package engine

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/tools"
)

// MaxTasksForEffort maps effort to orchestration width.
func MaxTasksForEffort(effort string) int {
	return maxTasksFor(effort)
}

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

	model := a.modelFor(a.Effort)
	maxTasks := maxTasksFor(a.Effort)

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

	fmt.Fprintf(a.Out, "%s◆ plan (%d tasks):%s\n", colorMag, len(tasks), colorReset)
	for i, task := range tasks {
		fmt.Fprintf(a.Out, "%s  %d. %s%s%s\n", colorDim, i+1, task.Title, task.annotation(), colorReset)
	}

	// ---- 2. delegate ----
	outcomes, err := a.runTasks(ctx, model, userInput, tasks)
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
		fmt.Fprintf(&sb, "\n\nIMPORTANT: %d of %d tasks failed or were blocked. Say plainly what could not be done and what that leaves uncertain. Do not present the answer as complete.", failures, len(tasks))
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
	return nil
}

// runTasks delegates each task in turn and returns what became of each.
//
// A run reports its failures; it does not vanish. Aborting on the first error
// discards every result produced before it, and those results already cost
// money. The only thing that stops a run is the user cancelling it, which is
// not a failure to report.
func (a *Agent) runTasks(ctx context.Context, model, userInput string, tasks []Task) ([]outcome, error) {
	outcomes := make([]outcome, len(tasks))
	results := make([]string, len(tasks))

	for i, task := range tasks {
		if blocker, blocked := blockedBy(tasks, outcomes, i); blocked {
			outcomes[i] = outcome{Status: statusBlocked, Reason: "blocked: " + blocker + " did not produce a result"}
			fmt.Fprintf(a.Out, "\n%s◆ subagent %d/%d skipped: %s — %s%s\n",
				colorDim, i+1, len(tasks), task.Title, outcomes[i].Reason, colorReset)
			continue
		}

		fmt.Fprintf(a.Out, "\n%s◆ subagent %d/%d: %s%s\n", colorMag, i+1, len(tasks), task.Title, colorReset)
		result, err := a.runSubagent(ctx, model, userInput, tasks, results, i)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		outcomes[i] = a.classify(result, err)
		results[i] = outcomes[i].Result

		switch outcomes[i].Status {
		case statusFailed:
			fmt.Fprintf(a.Out, "%s✗ %s failed: %s%s\n", colorDim, task.Title, outcomes[i].Reason, colorReset)
		case statusIncomplete:
			fmt.Fprintf(a.Out, "%s! %s did not finish; keeping what it reached%s\n", colorDim, task.Title, colorReset)
		}
	}
	return outcomes, nil
}

// plan asks the planner for a strict-JSON task list.
//
// The reply is asked for as objects and accepted as either: a planner that
// sends the flat array of strings this used to require still produces a
// working plan, it just produces one that cannot be routed.
func (a *Agent) plan(ctx context.Context, model, userInput string, maxTasks int) ([]Task, provider.Meta, error) {
	prompt := fmt.Sprintf(`Decompose the request below into at most %d concrete, self-contained tasks for coding subagents that have file and shell access but cannot talk to each other. If the request is trivial or a single step, return a single task.

Respond with ONLY a JSON array. No prose, no markdown fences. Each element is an object:

  {"title": "what to do", "kind": "edit", "needs": [1]}

"kind" is one of: edit, test, research, explain, design, boilerplate. Omit it if none fits.
"needs" lists the task numbers (counting from 1) whose results this task actually requires.
Omit "needs" or use [] when the task stands alone — do not list a task merely because it
comes earlier.

Request:
%s`, maxTasks, userInput)

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
func (a *Agent) runSubagent(ctx context.Context, model, original string, tasks []Task, results []string, idx int) (string, error) {
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
	for round := 0; round < maxRounds; round++ {
		msg, meta, err := a.streamChat(ctx, activityWorking, model, msgs, tools.Definitions(), func(tok string) {
			fmt.Fprint(a.Out, tok)
		})
		if err != nil {
			fmt.Fprintln(a.Out)
			return "", err
		}
		fmt.Fprintln(a.Out)
		a.record("subagent", meta, len(msg.ToolCalls))
		msgs = append(msgs, msg)

		if len(msg.ToolCalls) == 0 {
			return strings.TrimSpace(msg.Content), nil
		}
		for _, tc := range msg.ToolCalls {
			result, err := a.executeSubagentTool(ctx, tc)
			if err != nil {
				result = "Error: " + err.Error()
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
