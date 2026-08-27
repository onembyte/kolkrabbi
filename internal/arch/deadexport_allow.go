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

	// Triaged 2026-08-27, and nothing here says "untriaged" any more. Eleven of
	// the sixteen were deleted: four legacy effort aliases, atomicfile.WriteJSON,
	// shell.Have, shell.Quote, dash.Dist, NewClaudeSession, NewSessionDecider
	// and VerifySagaChapter. The five below each earn their place.
	"MaxTasksForEffort": "exported only so the external test package can assert orchestration width",

	// Built and unreachable on purpose: the managed local runtime cannot start
	// until L13.5b4 pins a reviewed release with its checksum, which is blocked
	// on the owner. Deleting them would mean rebuilding them the day that
	// decision is made.
	"InstallRuntime":    "managed runtime, blocked on L13.5b4 pinning a release",
	"NewManagedRuntime": "managed runtime, blocked on L13.5b4 pinning a release",
	"NewRuntimeSpec":    "managed runtime, blocked on L13.5b4 pinning a release",

	// A7.4's event-to-text path, built ahead of the thing that will read it.
	// I26.7's remote client needs exactly this — protocol events rendered as
	// terminal text — so it is waiting for a consumer, not left over from one.
	"NewPlainRenderer": "A7.4's event renderer, waiting on I26.7's remote client",
}
