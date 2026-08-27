package cli

import (
	"strings"
	"testing"
)

// A diff is the single most likely thing to carry a secret into a prompt: it is
// the literal text of what changed, including a line that added a key.
func TestStagedDiffIsScrubbedBeforeItReachesAModel(t *testing.T) {
	const key = "sk-or-v1-0123456789abcdef0123456789abcdef0123"
	diff := "diff --git a/.env b/.env\n+OPENROUTER_API_KEY=" + key + "\n"

	prepared := prepareDiffForDrafting(diff)
	if strings.Contains(prepared, key) {
		t.Fatal("the staged diff reached the drafting prompt with a key in it")
	}
	if strings.Contains(prepared, key[:20]) {
		t.Fatal("a searchable prefix of the key survived")
	}
	if !strings.Contains(prepared, "diff --git") {
		t.Errorf("scrubbing destroyed the diff:\n%s", prepared)
	}
}

// An enormous diff must not be sent whole: it would cost more than the message
// is worth and may not fit at all. It is truncated, and the truncation says so
// rather than letting a model describe half a change as if it were all of it.
func TestAnEnormousDiffIsTruncatedVisibly(t *testing.T) {
	huge := strings.Repeat("+a line of change\n", 40000)
	prepared := prepareDiffForDrafting(huge)

	if len(prepared) > maxDraftDiffBytes+200 {
		t.Errorf("prepared diff is %d bytes, over the %d limit", len(prepared), maxDraftDiffBytes)
	}
	if !strings.Contains(prepared, "truncated") {
		t.Errorf("a truncated diff does not say so, so a model would describe half a change as all of it")
	}
}

func TestASmallDiffIsPassedThroughWhole(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n+func x() {}\n"
	if prepared := prepareDiffForDrafting(diff); prepared != diff {
		t.Errorf("a small diff was altered:\n got %q\nwant %q", prepared, diff)
	}
}

// Nothing staged is the common mistake, not an error: the answer names the
// command that fixes it rather than a failure.
func TestNothingStagedExplainsItself(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.reportNothingStaged()
	out := stdout.String()
	if !strings.Contains(out, "git add") {
		t.Errorf("the message does not say how to stage anything:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "error") {
		t.Errorf("an unstaged tree was reported as an error:\n%s", out)
	}
}

// The whole point of the item: it drafts and stops.
func TestTheDraftIsShownWithTheCommandThatWouldUseIt(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.showCommitDraft("fix: stop the doom loop counting empty turns")
	out := stdout.String()
	if !strings.Contains(out, "fix: stop the doom loop") {
		t.Errorf("the draft is not shown:\n%s", out)
	}
	if !strings.Contains(out, "git commit") {
		t.Errorf("the draft does not hand over to the command that uses it:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "nothing was committed") {
		t.Errorf("the output does not say plainly that nothing was committed:\n%s", out)
	}
}
