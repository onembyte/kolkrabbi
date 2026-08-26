package engine

import (
	"context"
	"encoding/json"
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
	for i, t := range tasks {
		fmt.Fprintf(a.Out, "%s  %d. %s%s\n", colorDim, i+1, t, colorReset)
	}

	// ---- 2. delegate ----
	results := make([]string, 0, len(tasks))
	for i, task := range tasks {
		fmt.Fprintf(a.Out, "\n%s◆ subagent %d/%d: %s%s\n", colorMag, i+1, len(tasks), task, colorReset)
		res, err := a.runSubagent(ctx, model, userInput, tasks, results, i)
		if err != nil {
			return fmt.Errorf("subagent %d: %w", i+1, err)
		}
		results = append(results, res)
	}

	// ---- 3. synthesize ----
	fmt.Fprintf(a.Out, "\n%s◆ synthesizing%s\n", colorMag, colorReset)
	var sb strings.Builder
	sb.WriteString("Original request:\n" + userInput + "\n\nCompleted tasks and their results:\n")
	for i, t := range tasks {
		fmt.Fprintf(&sb, "\n%d. %s\nResult: %s\n", i+1, t, results[i])
	}
	sb.WriteString("\nWrite the final answer to the original request based on this work. Be concise; report what was done, key findings, and anything the user must know. Do not repeat raw task output verbatim.")

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

// plan asks the planner for a strict-JSON task list.
func (a *Agent) plan(ctx context.Context, model, userInput string, maxTasks int) ([]string, provider.Meta, error) {
	prompt := fmt.Sprintf(`Decompose the request below into at most %d concrete, self-contained tasks for coding subagents that have file and shell access but cannot talk to each other. Order matters: later tasks may depend on earlier results. If the request is trivial or a single step, return a single task.

Respond with ONLY a JSON array of task strings. No prose, no markdown fences.

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
	tasks := parseTaskList(msg.Content, maxTasks)
	return tasks, meta, nil
}

// parseTaskList tolerantly extracts a JSON string array from model output.
func parseTaskList(s string, maxTasks int) []string {
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start == -1 || end == -1 || end < start {
		return nil
	}
	var tasks []string
	if err := json.Unmarshal([]byte(s[start:end+1]), &tasks); err != nil {
		return nil
	}
	out := tasks[:0]
	for _, t := range tasks {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	if len(out) > maxTasks {
		out = out[:maxTasks]
	}
	return out
}

// runSubagent executes one task in an isolated context: its conversation
// never enters the main session, only its final summary does.
func (a *Agent) runSubagent(ctx context.Context, model, original string, tasks, results []string, idx int) (string, error) {
	cwd := workingDir()
	var briefing strings.Builder
	fmt.Fprintf(&briefing, `You are subagent %d of %d in an orchestrated run on %s (working directory %s). You have tools to read/write/edit files, list directories, and run shell commands. Complete ONLY your assigned task, then reply with a short result summary (what you did, key outputs, paths touched). Be efficient: few tool calls, no exploration beyond the task.

Overall request: %s
`, idx+1, len(tasks), runtime.GOOS, cwd, original)
	if len(results) > 0 {
		briefing.WriteString("\nResults from earlier tasks:\n")
		for i, r := range results {
			fmt.Fprintf(&briefing, "%d. %s -> %s\n", i+1, tasks[i], r)
		}
	}

	msgs := []provider.Message{
		{Role: "system", Content: briefing.String()},
		{Role: "user", Content: "Your task: " + tasks[idx]},
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
			result, err := a.executeTool(ctx, tc)
			if err != nil {
				result = "Error: " + err.Error()
			}
			msgs = append(msgs, provider.Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
	}
	return "", fmt.Errorf("exceeded %d tool rounds without finishing", maxRounds)
}

func workingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "?"
	}
	return cwd
}
