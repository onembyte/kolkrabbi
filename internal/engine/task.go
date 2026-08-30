package engine

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Kind is what sort of work a task is, as the planner judged it. It exists so
// the router has something to route on; it is deliberately a short closed list,
// because a taxonomy nobody can hold in their head is one the planner will
// apply inconsistently.
type Kind string

const (
	// KindUnknown is what an older or weaker planner produces, and it routes to
	// the session model — which is what happens today.
	KindUnknown     Kind = ""
	KindEdit        Kind = "edit"
	KindTest        Kind = "test"
	KindResearch    Kind = "research"
	KindExplain     Kind = "explain"
	KindDesign      Kind = "design"
	KindBoilerplate Kind = "boilerplate"
)

// knownKinds is the closed set. Anything else is left unknown rather than
// guessed: a task routed to the wrong model on a misread label costs more than
// one routed to the default.
var knownKinds = map[Kind]bool{
	KindEdit: true, KindTest: true, KindResearch: true,
	KindExplain: true, KindDesign: true, KindBoilerplate: true,
}

// Task is one unit of orchestrated work.
type Task struct {
	// Title is what to do, as the planner wrote it.
	Title string
	// Kind is what sort of work it is. Empty when the planner did not say.
	Kind Kind
	// Level is how much capability it needs. Empty when the planner did not
	// say, which binds to the model the user selected.
	Level Level
	// Needs holds the indices of tasks whose results this one requires.
	//
	// This is the difference between "task 4 comes after task 3" and "task 4
	// needs what task 3 produced". The orchestrator has always assumed the
	// first and implemented the second by accident, by pasting every earlier
	// result into every later briefing.
	Needs []int
	// Model is resolved by the router. Empty until then.
	Model string
}

// annotation is what is shown beside a task in the printed plan. A run that
// silently treats tasks differently is a run whose cost nobody can account for.
func (t Task) annotation() string {
	parts := make([]string, 0, 3)
	if t.Kind != KindUnknown {
		parts = append(parts, string(t.Kind))
	}
	// Omitted rather than shown as a gap: a planner that never states a level
	// should read as a column of blanks, not as an empty slot in every row.
	if t.Level != LevelUnstated {
		parts = append(parts, string(t.Level))
	}
	if t.Model != "" {
		parts = append(parts, t.Model)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  [" + strings.Join(parts, " · ") + "]"
}

// planTask is the wire shape. The planner is a model, so both the rich object
// and a bare string have to parse: whatever we ask for, a weaker model will
// sometimes send the flat array that works today, and that must keep working.
type planTask struct {
	Title string `json:"title"`
	Kind  Kind   `json:"kind"`
	Level Level  `json:"level"`
	Needs []int  `json:"needs"`
	// stated records whether this task said anything about its dependencies,
	// which is not the same as saying it has none.
	stated bool
}

func (p *planTask) UnmarshalJSON(b []byte) error {
	var title string
	if err := json.Unmarshal(b, &title); err == nil {
		p.Title = title
		return nil
	}
	type wire planTask
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*p = planTask(w)
	p.stated = true
	return nil
}

// parseTasks tolerantly extracts a plan from planner output.
//
// Tolerance is the requirement, not a nicety: this is the one place where a
// model's reply becomes control flow, and a strict parser here means a run that
// fails on a stray sentence rather than on anything the user did.
func parseTasks(reply string, maxTasks int) []Task {
	start := strings.Index(reply, "[")
	end := strings.LastIndex(reply, "]")
	if start == -1 || end == -1 || end < start {
		return nil
	}
	var planned []planTask
	if err := json.Unmarshal([]byte(reply[start:end+1]), &planned); err != nil {
		return nil
	}

	tasks := make([]Task, 0, len(planned))
	for _, one := range planned {
		title := strings.TrimSpace(one.Title)
		if title == "" {
			continue
		}
		kind := one.Kind
		if !knownKinds[kind] {
			kind = KindUnknown
		}
		tasks = append(tasks, Task{Title: title, Kind: kind, Level: normalizeLevel(one.Level), Needs: one.Needs})
		if !one.stated {
			// A planner that said nothing about dependencies gets the
			// assumption the sequential run already makes: everything before.
			tasks[len(tasks)-1].Needs = allEarlier(len(tasks) - 1)
		}
		if len(tasks) == maxTasks {
			break
		}
	}
	return resolveNeeds(tasks)
}

// allEarlier lists every task before this one, in the planner's own 1-based
// numbering so that resolveNeeds treats it exactly like a stated dependency.
func allEarlier(index int) []int {
	if index == 0 {
		return nil
	}
	needs := make([]int, index)
	for i := range needs {
		needs[i] = i + 1
	}
	return needs
}

// resolveNeeds converts the planner's 1-based task numbers into indices and
// drops the ones that cannot mean anything.
//
// The planner counts from 1 because that is how the plan was shown to it. A
// dependency on a later task is a cycle; on a missing one it is a briefing that
// would silently omit what the task said it needed. Both are dropped rather
// than repaired: a guess about what the planner meant is worse than a task that
// runs with less context than it asked for.
func resolveNeeds(tasks []Task) []Task {
	for i := range tasks {
		if tasks[i].Needs == nil {
			continue
		}
		kept := make([]int, 0, len(tasks[i].Needs))
		seen := map[int]bool{}
		for _, stated := range tasks[i].Needs {
			index := stated - 1
			if index < 0 || index >= i || seen[index] {
				continue
			}
			seen[index] = true
			kept = append(kept, index)
		}
		tasks[i].Needs = kept
	}
	return tasks
}

// dependencyBriefing renders the results one task declared it needs.
//
// Only the declared ones. Handing every task everything is how one subagent's
// tangent becomes every later subagent's context, and it is the reason a long
// plan gets worse as it goes.
func dependencyBriefing(tasks []Task, results []string, index int) string {
	var b strings.Builder
	for _, need := range tasks[index].Needs {
		if need >= len(results) || strings.TrimSpace(results[need]) == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\nResults you asked for:\n")
		}
		fmt.Fprintf(&b, "%d. %s -> %s\n", need+1, tasks[need].Title, results[need])
	}
	return b.String()
}
