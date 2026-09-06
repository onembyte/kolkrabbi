package dash

import (
	"strings"
	"testing"
)

// MANY: a live card says what source control is doing in its working tree —
// the branch and how many files differ — and says nothing where git said
// nothing.
func TestACardSaysWhatSourceControlIsDoing(t *testing.T) {
	page := Sessions([]SessionCard{
		{ID: "s1", Name: "one", Model: "m", CWD: "/w", Live: true, Branch: "main", Dirty: 3, VCSKnown: true},
		{ID: "s2", Name: "two", Model: "m", CWD: "/w2", Live: true, Branch: "fix/x", VCSKnown: true},
		{ID: "s3", Name: "three", Model: "m", CWD: "/tmp/nogit", Live: false},
	}, nil)
	for _, want := range []string{"on main · 3 files changed", "on fix/x · clean"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}
	if strings.Count(page, "on ") != 2 {
		t.Fatalf("a card without git state got a vcs line:\n%s", page)
	}
}
