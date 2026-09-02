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
	// one is deleted. SagaBudget left it the same day, when the old saga CLI
	// finally gave the budget guards something to guard — the rot test caught
	// that, rather than the entry sitting here claiming otherwise.

	// Overview left this list on 2026-08-27, when I27.5 gave it a caller:
	// `kolk sessions` reads the cards to warn about a shared checkout. The rot
	// test noticed within one run, which is the whole point of having it.
	//
	// ParseRules is the plural nobody needed, since the CLI parses line by line
	// to survive one bad rule.
	"ParseRules": "the CLI uses ParseRule per line so one bad rule costs one rule",

	// Triaged 2026-08-27, and nothing here says "untriaged" any more. Eleven of
	// the sixteen were deleted: four legacy effort aliases, atomicfile.WriteJSON,
	// shell.Have, shell.Quote, dash.Dist, NewClaudeSession, NewSessionDecider
	// and VerifySagaChapter. The five below each earn their place.
	"MaxTasksForEffort": "exported only so the external test package can assert orchestration width",

	// A7.4's event-to-text path, built ahead of the thing that will read it.
	//
	// Reason corrected on 2026-08-27: I26.7 has partly landed — the turn.start
	// command and its route — and this still has no caller, because what needs
	// it is the *page* half, which renders protocol events as text. Saying
	// "waiting on I26.7" now reads as waiting on something already done, so the
	// entry names the half that is actually outstanding.
	"NewPlainRenderer": "A7.4's event renderer, waiting on I26.7's client page",

	// F4 of FABLE_OPTIMIZATION.md, 2026-09-02. The discovery vocabulary has
	// four statuses and F4.1/F4.2 wire three: listed (a vendor catalog),
	// unverified (a gateway preview), gone (a refusal by name). `verified` is
	// set by the first real turn's init.model, which is F4.3's leaf; until
	// that lands this constant is named here so the vocabulary ships whole
	// and the ratchet still catches the day F4.3 forgets to use it.
	"StatusVerified": "set by F4.3 (first-turn init.model promotion); remove this entry when it lands",
}
