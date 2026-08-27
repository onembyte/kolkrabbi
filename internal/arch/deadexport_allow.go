package arch

// DeadExportAllowlist names exported symbols in internal/ that nothing but a
// test refers to, and that are kept anyway.
//
// The rule beside it (TestNoExportedSymbolIsUsedOnlyByTests) exists because
// golangci-lint cannot see this class of code: `unused` treats an exported
// identifier as a package's public API, which is right for a library and wrong
// inside `internal/`, where nothing outside the module can call it. Proved by
// experiment — an obviously uncalled exported function produced `0 issues`.
//
// Everything below was found the day the rule was written. It is recorded as a
// backlog rather than dressed up as justified: three entries have a real reason
// and the rest are honestly marked untriaged. The value of committing it in
// this state is that the ratchet is live for *new* code immediately, while the
// existing set stays visible instead of being deleted on a guess.
var DeadExportAllowlist = map[string]string{
	// Genuinely exported for other packages' tests, which this rule cannot see
	// by construction: it counts non-test uses, and a test helper has none.
	"FakeCheckpointer": "internal/enginetest exists to be used by other packages' tests",
	"FakeClock":        "internal/enginetest exists to be used by other packages' tests",
	"NewFakeSession":   "internal/enginetest exists to be used by other packages' tests",

	// The rule's own allowlist. Only its test reads it, necessarily.
	"DeadExportAllowlist": "read by the rule that uses this list",

	// ChapterVerifier and FileGateDetector left this list on 2026-08-27: the
	// owner chose wire-and-drop, so they are now the live path and the ad-hoc
	// one is deleted. SagaBudget left it the same day, when `kolk saga run`
	// finally gave the budget guards something to guard — the rot test caught
	// that, rather than the entry sitting here claiming otherwise.

	// Mine, and the reason this rule earns its place: I built both two days ago
	// and wired neither. Overview is what I26.7's remote client will render;
	// ParseRules is the plural nobody needed, since the CLI parses line by line
	// to survive one bad rule.
	"Overview":   "built for I26.7, which has not landed yet",
	"ParseRules": "the CLI uses ParseRule per line so one bad rule costs one rule",

	// Untriaged, found 2026-08-27. Each needs a look: some are surface built
	// ahead of its caller, some are probably deletable.
	"Dist":              "untriaged 2026-08-27",
	"EffortDeep":        "untriaged 2026-08-27",
	"EffortQuick":       "untriaged 2026-08-27",
	"EffortStandard":    "untriaged 2026-08-27",
	"EffortUltra":       "untriaged 2026-08-27",
	"Have":              "untriaged 2026-08-27",
	"InstallRuntime":    "untriaged 2026-08-27",
	"MaxTasksForEffort": "untriaged 2026-08-27",
	"NewClaudeSession":  "untriaged 2026-08-27",
	"NewManagedRuntime": "untriaged 2026-08-27",
	"NewPlainRenderer":  "untriaged 2026-08-27",
	"NewRuntimeSpec":    "untriaged 2026-08-27",
	"NewSessionDecider": "untriaged 2026-08-27",
	"Quote":             "untriaged 2026-08-27",
	"VerifySagaChapter": "untriaged 2026-08-27",
	"WriteJSON":         "untriaged 2026-08-27",
}
