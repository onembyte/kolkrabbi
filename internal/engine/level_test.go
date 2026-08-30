package engine

import (
	"strings"
	"testing"
)

// The planner says how much capability a task needs. It is deliberately not a
// model name: a name can escape the ceiling, a level cannot, because every
// level is an index into a ladder that begins at the model the user chose.
func TestAPlannerThatStatesALevelHasItRecorded(t *testing.T) {
	tasks := parseTasks(`[
		{"title":"wire the config key","kind":"edit","level":"routine"},
		{"title":"commit and push","kind":"boilerplate","level":"trivial"},
		{"title":"design the roster","kind":"design","level":"hard"}
	]`, 10)
	if len(tasks) != 3 {
		t.Fatalf("parsed %d tasks, want 3", len(tasks))
	}
	for index, want := range []Level{LevelRoutine, LevelTrivial, LevelHard} {
		if tasks[index].Level != want {
			t.Errorf("task %d level = %q, want %q", index, tasks[index].Level, want)
		}
	}
}

// Anything outside the closed set is left unstated rather than guessed —
// exactly as Kind already behaves. A task run on the wrong rung because a label
// was misread costs more than one run on the model the user picked.
func TestAnInventedLevelIsNotGuessed(t *testing.T) {
	for _, invented := range []string{"medium", "VERY HARD", "claude-opus", "easy", "2"} {
		tasks := parseTasks(`[{"title":"x","level":"`+invented+`"}]`, 10)
		if len(tasks) != 1 {
			t.Fatalf("%q: parsed %d tasks", invented, len(tasks))
		}
		if tasks[0].Level != LevelUnstated {
			t.Errorf("%q was accepted as level %q", invented, tasks[0].Level)
		}
	}
}

// The three real levels must survive whatever case the planner writes them in.
func TestALevelIsReadRegardlessOfCase(t *testing.T) {
	for _, spelling := range []string{"trivial", "Trivial", "TRIVIAL", "  trivial  "} {
		tasks := parseTasks(`[{"title":"x","level":"`+spelling+`"}]`, 10)
		if len(tasks) != 1 || tasks[0].Level != LevelTrivial {
			t.Errorf("%q did not read as trivial: %q", spelling, tasks[0].Level)
		}
	}
}

// The plan line is where a reader sees what a run is about to spend. A planner
// that never states a level shows as a blank column rather than as a quietly
// expensive run.
func TestAPlanLineShowsKindLevelAndModel(t *testing.T) {
	full := Task{Title: "x", Kind: KindEdit, Level: LevelRoutine, Model: "claude-sonnet"}
	if got := full.annotation(); got != "  [edit · routine · claude-sonnet]" {
		t.Errorf("annotation = %q", got)
	}
	if got := (Task{Title: "x", Level: LevelTrivial}).annotation(); got != "  [trivial]" {
		t.Errorf("level alone = %q", got)
	}
	if got := (Task{Title: "x", Kind: KindEdit, Model: "m"}).annotation(); got != "  [edit · m]" {
		t.Errorf("an unstated level must not leave an empty slot: %q", got)
	}
	if got := (Task{Title: "x"}).annotation(); got != "" {
		t.Errorf("a bare task should carry no annotation: %q", got)
	}
}

// The whole design rests on this: the planner never sees a model name, so it
// cannot name one above the ceiling. A prompt that mentions a rung would be the
// one place the guarantee leaks.
func TestThePlannerPromptNamesNoModel(t *testing.T) {
	prompt := decompositionPrompt(4)
	for _, ladder := range vendorLadders {
		for _, rung := range ladder.rungs {
			if strings.Contains(strings.ToLower(prompt), rung) {
				t.Errorf("the planner prompt names the model %q; it must ask for a level, not a name", rung)
			}
		}
	}
	// And it has to actually ask for the level, or nothing downstream has one.
	for _, level := range []string{"trivial", "routine", "hard"} {
		if !strings.Contains(prompt, level) {
			t.Errorf("the planner prompt never mentions the %q level", level)
		}
	}
}
