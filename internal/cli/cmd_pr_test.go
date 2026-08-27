package cli

import (
	"strings"
	"testing"
)

// The subject lines of a branch's commits are what a PR description is made of,
// and they can carry a secret exactly like a diff can.
func TestBranchLogIsScrubbedBeforeDrafting(t *testing.T) {
	const key = "sk-or-v1-0123456789abcdef0123456789abcdef0123"
	log := "abc1234 fix: rotate the key to " + key + "\ndef5678 docs: explain it\n"

	prepared := prepareLogForDrafting(log)
	if strings.Contains(prepared, key) || strings.Contains(prepared, key[:20]) {
		t.Fatal("a key survived into the drafting prompt")
	}
	if !strings.Contains(prepared, "docs: explain it") {
		t.Errorf("scrubbing destroyed the log:\n%s", prepared)
	}
}

func TestALongBranchLogIsTruncatedVisibly(t *testing.T) {
	long := strings.Repeat("abc1234 some commit subject line\n", 5000)
	prepared := prepareLogForDrafting(long)
	if len(prepared) > maxDraftLogBytes+200 {
		t.Errorf("prepared log is %d bytes, over the %d limit", len(prepared), maxDraftLogBytes)
	}
	if !strings.Contains(prepared, "truncated") {
		t.Error("a truncated log does not say so, so a model would describe part of a branch as all of it")
	}
}

// Nothing to propose is not a failure: the branch simply has no commits the
// base does not.
func TestNothingToProposeExplainsItself(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.reportNothingToPropose("main")
	out := stdout.String()
	if !strings.Contains(out, "main") {
		t.Errorf("the message does not name the base it compared against:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "error") {
		t.Errorf("an up-to-date branch was reported as an error:\n%s", out)
	}
}

// Pull requests are GitHub-only through gh, on purpose. A machine without it
// should be told what to install, not what broke.
func TestAMissingGhSaysWhatToInstall(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.reportMissingGh()
	out := stdout.String()
	if !strings.Contains(out, "gh") {
		t.Errorf("the message does not name the tool:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "error") {
		t.Errorf("a missing optional tool was reported as an error:\n%s", out)
	}
}

// A branch nobody has pushed cannot have a pull request, and the fix is one
// command that the message should name.
func TestAnUnpushedBranchIsToldHowToPush(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.reportNoUpstream("feature/x")
	out := stdout.String()
	if !strings.Contains(out, "git push") || !strings.Contains(out, "feature/x") {
		t.Errorf("the message does not name the push that would fix it:\n%s", out)
	}
}

// Drafting is where the model helps; running it is a confirmation like any
// other. The draft arrives with the command that would use it, and nothing runs.
func TestThePullRequestDraftHandsOverRatherThanRunning(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.showPullRequestDraft("main", "fix: stop the doom loop", "It counted empty turns.\n")
	out := stdout.String()
	for _, want := range []string{"fix: stop the doom loop", "It counted empty turns.", "gh pr create", "--base main"} {
		if !strings.Contains(out, want) {
			t.Errorf("the draft omits %q:\n%s", want, out)
		}
	}
	if !strings.Contains(strings.ToLower(out), "nothing was opened") {
		t.Errorf("the output does not say plainly that no pull request was opened:\n%s", out)
	}
}

// A title with a quote in it must not break the command that is handed over.
func TestTheHandoverQuotesTheTitle(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.showPullRequestDraft("main", `fix: don't split a rune`, "body")
	if !strings.Contains(stdout.String(), `'fix: don'\''t split a rune'`) {
		t.Errorf("the title is not shell-quoted, so the handover would not run:\n%s", stdout.String())
	}
}
