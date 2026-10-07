package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestDiscoveredTaskRoutesKeepRequestStatusAndAccountingTogether(t *testing.T) {
	srv := enginetest.New()
	defer srv.Close()
	a, _, _, records := newTestAgentInternal(t, srv, ModeAgent)
	a.SetSessionModel("new-main")
	a.Effort = EffortUltra
	a.MaxConcurrentTasks = 1
	a.Root = t.TempDir()
	reads := 0
	a.AgentRoster = func(ceiling, _ string) Roster {
		reads++
		return Roster{Rungs: []Rung{
			{Model: ceiling, Efforts: []string{"low", "high", "xhigh"}},
			{Model: "new-mid", Depth: 1, Efforts: []string{"low", "high"}},
			{Model: "new-small", Depth: 2, Efforts: []string{"low"}},
		}}
	}
	tasks := []Task{{Title: "easy", Kind: KindResearch, Level: LevelTrivial}, {Title: "ordinary", Kind: KindResearch, Level: LevelRoutine}, {Title: "hard", Kind: KindResearch, Level: LevelHard}}
	a.assignModels(tasks)
	if reads != 1 {
		t.Fatalf("catalog reads = %d", reads)
	}
	backends := map[string]*effortCapturingBackend{}
	factoryEfforts := map[string]string{}
	a.SubagentBackend = func(_ context.Context, model, _, effort string, _ SubagentCapabilities) (ChatBackend, error) {
		b := &effortCapturingBackend{}
		backends[model] = b
		factoryEfforts[model] = effort
		return b, nil
	}
	if _, err := a.runTasks(provider.WithEffort(context.Background(), EffortUltra), "work", tasks); err != nil {
		t.Fatal(err)
	}
	wantModels := []string{"new-small", "new-mid", "new-main"}
	wantEfforts := []string{"low", "low", "xhigh"}
	records.mu.Lock()
	defer records.mu.Unlock()
	for i, task := range tasks {
		if task.Model != wantModels[i] || task.Effort != wantEfforts[i] {
			t.Fatalf("route %d: %+v", i, task)
		}
		if factoryEfforts[task.Model] != task.Effort || backends[task.Model].seen != task.Effort || records.Calls[i].Effort != task.Effort {
			t.Fatalf("effort mismatch for %+v", task)
		}
	}
}

func TestDiscoveredFallbackUsesTheCeilingsOfferedEffort(t *testing.T) {
	srv := enginetest.New()
	defer srv.Close()
	a, _, _, records := newTestAgentInternal(t, srv, ModeAgent)
	a.Root = t.TempDir()
	a.SetSessionModel("selected")
	a.AgentRoster = func(ceiling, _ string) Roster {
		return Roster{Rungs: []Rung{
			{Model: ceiling, Vendor: "codex", Efforts: []string{"low", "medium", "high"}},
			{Model: "small", Vendor: "codex", Depth: 1, Efforts: []string{"low"}},
		}}
	}
	tasks := []Task{{Title: "routine", Kind: KindResearch, Level: LevelRoutine}}
	a.assignModels(tasks)
	backend := &effortCapturingBackend{}
	a.SubagentBackend = func(_ context.Context, model, _, effort string, caps SubagentCapabilities) (ChatBackend, error) {
		if caps.Provider != "codex" {
			t.Errorf("lost discovered vendor: %+v", caps)
		}
		if model == "small" {
			return nil, errors.New("cannot start")
		}
		if effort != "medium" {
			t.Errorf("fallback factory effort = %q", effort)
		}
		return backend, nil
	}
	var final SubagentStatus
	a.Subagents = func(status SubagentStatus) {
		if status.State == SubagentDone {
			final = status
		}
	}
	if _, err := a.runTasks(context.Background(), "work", tasks); err != nil {
		t.Fatal(err)
	}
	if backend.seen != "medium" || final.Model != "selected" || final.Effort != "medium" || records.Calls[0].Effort != "medium" {
		t.Fatalf("fallback mismatch: request=%q status=%+v record=%+v", backend.seen, final, records.Calls)
	}
}

func TestNewDiscoveredClaudeNameKeepsItsAdapterCapabilities(t *testing.T) {
	a := &Agent{Options: Options{Root: t.TempDir()}}
	caps := a.subagentCapabilities(KindEdit, "future-model", "", "claude")
	if caps.Provider != "claude" || !caps.NetworkAccess {
		t.Fatalf("new name lost adapter policy: %+v", caps)
	}
	a.SubagentNetwork = SubagentNetworkOff
	if a.subagentCapabilities(KindEdit, "future-model", "", "claude").NetworkAccess {
		t.Fatal("explicit network off was lost")
	}
}

func TestDiscoveredCeilingBoundsSlotsAndPlanner(t *testing.T) {
	a := &Agent{Options: Options{Model: "new-selected", Slots: map[string]string{SlotFast: "new-strong", SlotOrchestrator: "unknown-provider/model"},
		AgentRoster: func(ceiling, _ string) Roster {
			return Roster{Rungs: []Rung{{Model: ceiling}, {Model: "new-small", Depth: 1}}}
		},
	}}
	tasks := []Task{{Title: "mechanical", Kind: KindBoilerplate, Level: LevelTrivial}}
	a.assignModels(tasks)
	// Re-read 2026-09-25 (V43.5 E3): a slot outside the menu is replaced by
	// the task level's rung, not the ceiling — still never above it — while
	// the orchestrator stays on the ceiling.
	if tasks[0].Model != "new-small" || a.orchestrationModel() != "new-selected" {
		t.Fatalf("slot escaped discovered ceiling or took the ceiling's price: %+v", tasks)
	}
}

func TestBlankConfiguredSlotKeepsAutomaticTaskRouting(t *testing.T) {
	a := &Agent{Options: Options{Model: "selected", Slots: map[string]string{SlotExplore: " \t "},
		AgentRoster: func(ceiling, _ string) Roster {
			return Roster{Rungs: []Rung{{Model: ceiling}, {Model: "small", Depth: 1}}}
		},
	}}
	tasks := []Task{{Title: "read", Kind: KindResearch, Level: LevelTrivial}}
	a.assignModels(tasks)
	if tasks[0].Model != "small" {
		t.Fatalf("blank slot overrides routing: %+v", tasks)
	}
}

// A slot outside the discovered menu names nothing this run can vouch for.
// The task's own level routes it instead, which for a cheap slot is the cheap
// work it was set for, never the ceiling's price, and never above the ceiling.
func TestAnOutOfMenuSlotFallsBackToTheLevelsRung(t *testing.T) {
	var out strings.Builder
	a := &Agent{Options: Options{Model: "sel", Out: &out, Slots: map[string]string{SlotFast: "ollama/qwen3:8b", SlotExplore: "claude-haiku"},
		AgentRoster: func(ceiling, _ string) Roster {
			return Roster{Discovered: true, Rungs: []Rung{{Model: ceiling}, {Model: "mid", Depth: 1}, {Model: "small", Depth: 2}}}
		},
	}}
	tasks := []Task{
		{Title: "boilerplate", Kind: KindBoilerplate, Level: LevelTrivial},
		{Title: "look around", Kind: KindResearch, Level: LevelRoutine},
		{Title: "hard research", Kind: KindResearch, Level: LevelHard},
		{Title: "look further", Kind: KindResearch, Level: LevelRoutine},
	}
	a.assignModels(tasks)
	for i, want := range []string{"small", "mid", "sel", "mid"} {
		if tasks[i].Model != want {
			t.Errorf("%s (%s) ran on %q, want %q: %+v", tasks[i].Title, tasks[i].Level, tasks[i].Model, want, tasks)
		}
	}
	if !strings.Contains(out.String(), "ollama/qwen3:8b is outside the selected model's ranked menu; using small") {
		t.Errorf("the replacement was not announced as what runs:\n%s", out.String())
	}
	// One note per slot, naming every model its tasks run on instead.
	if !strings.Contains(out.String(), "claude-haiku is outside the selected model's ranked menu; using mid, sel\n") ||
		strings.Count(out.String(), "claude-haiku is outside") != 1 {
		t.Errorf("the explore slot's note does not name every model it routed to, once:\n%s", out.String())
	}

	// A slot the menu does vouch for is still the user's decision, and beats
	// the level: a fast slot on "mid" keeps trivial work on "mid".
	a.Slots = map[string]string{SlotFast: "MID"}
	out.Reset()
	pinned := []Task{{Title: "boilerplate", Kind: KindBoilerplate, Level: LevelTrivial}}
	a.assignModels(pinned)
	if pinned[0].Model != "mid" {
		t.Fatalf("an in-menu slot lost to the level: %+v", pinned)
	}
	// Matched case-insensitively, so it is not announced as outside the menu.
	if strings.Contains(out.String(), "outside") {
		t.Fatalf("an in-menu slot was announced as outside the menu:\n%s", out.String())
	}
}
