package engine

// effortForTask translates the planner's capability level into the reasoning
// budget that task actually receives. An omitted or unrecognised level keeps
// the user's session choice; guessing downward there would make an older or
// weaker planner silently reduce quality.
func effortForTask(level Level, sessionEffort string) string {
	switch normalizeLevel(level) {
	case LevelTrivial:
		return EffortLow
	case LevelRoutine:
		return EffortMedium
	case LevelHard:
		return EffortMax
	default:
		effort, ok := NormalizeEffort(sessionEffort)
		if !ok {
			return EffortMedium
		}
		return effort
	}
}
