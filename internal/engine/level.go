package engine

import "strings"

// Level is how much capability a task needs, as the planner judged it.
//
// It is deliberately not a model name. A name can escape the ceiling — the
// clamp leaves an unranked or cross-vendor name alone, on purpose — while a
// level cannot, because every level resolves to an index into a ladder whose
// first element IS the model the user selected. A model above that selection is
// therefore unrepresentable rather than caught after the fact, which is a
// stronger guarantee and a smaller one to keep.
//
// It is also not a priority. "This task matters most" and "this task needs the
// strongest model" are different claims, and conflating them is how a run ends
// up spending its allowance on whichever step the planner felt strongly about.
type Level string

const (
	// LevelUnstated is what an older or weaker planner produces, and what an
	// unrecognised word becomes.
	//
	// It binds to the ceiling — the model the user selected — on purpose. A
	// task whose difficulty could not be read is not one to hand to the
	// cheapest thing available, and binding it to the ceiling makes today's
	// behaviour and the safe answer the same answer.
	LevelUnstated Level = ""
	// LevelTrivial is mechanical: the answer is obvious once you look.
	LevelTrivial Level = "trivial"
	// LevelRoutine is ordinary implementation or analysis.
	LevelRoutine Level = "routine"
	// LevelHard needs real reasoning, is subtle, or the rest of the plan
	// depends on getting it right.
	LevelHard Level = "hard"
)

// knownLevels is the closed set. Anything else is left unstated rather than
// guessed, exactly as Kind already behaves: a task run on the wrong rung
// because a label was misread costs more than one run on the user's own model.
var knownLevels = map[Level]bool{
	LevelTrivial: true, LevelRoutine: true, LevelHard: true,
}

// normalizeLevel reads what the planner wrote. Case and surrounding space are
// forgiven because they are spelling, not meaning; an unrecognised word is not.
func normalizeLevel(stated Level) Level {
	level := Level(strings.ToLower(strings.TrimSpace(string(stated))))
	if !knownLevels[level] {
		return LevelUnstated
	}
	return level
}
