package dash

import (
	"strings"
	"testing"
)

// Item 27: blocked is the decisive field. A session waiting on a prompt has
// stopped and needs a person, and it looks exactly like one thinking hard — so
// it must be visible at a glance, not a column somebody scans for.
func TestABlockedCardIsUnmissable(t *testing.T) {
	html := renderSessionCards([]SessionCard{
		{ID: "s_a", Name: "the busy one", Model: "m", Live: true},
		{ID: "s_b", Name: "the stuck one", Model: "m", Live: true, BlockedOn: "bash"},
	})
	blocked := strings.Index(html, "the stuck one")
	busy := strings.Index(html, "the busy one")
	if blocked < 0 || busy < 0 {
		t.Fatalf("a card is missing:\n%s", html)
	}
	if blocked > busy {
		t.Error("the blocked session is not listed first, so it can be scrolled past")
	}
	if !strings.Contains(html, "blocked") {
		t.Errorf("nothing on the page says the word:\n%s", html)
	}
	if !strings.Contains(html, "bash") {
		t.Errorf("the card does not say what it is waiting on:\n%s", html)
	}
}

// The page and `kolk sessions` must agree rather than grow two vocabularies.
func TestTheCardUsesTheSameWordsAsTheListing(t *testing.T) {
	html := renderSessionCards([]SessionCard{{ID: "s_a", Name: "n", Model: "m", Live: true}})
	if !strings.Contains(html, "live") {
		t.Errorf("a running session is not called live, which is what the listing calls it:\n%s", html)
	}
	idle := renderSessionCards([]SessionCard{{ID: "s_b", Name: "n", Model: "m"}})
	if !strings.Contains(idle, "idle") {
		t.Errorf("a stopped session is not called idle:\n%s", idle)
	}
}

// A session id, a title and a working directory are all user-controlled text
// arriving in HTML.
func TestCardTextIsEscaped(t *testing.T) {
	html := renderSessionCards([]SessionCard{{
		ID:   "s_a",
		Name: `<script>alert('x')</script>`,
		CWD:  `"/tmp/<b>`,
	}})
	if strings.Contains(html, "<script>") {
		t.Fatalf("a session title was rendered as markup:\n%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("the title was dropped rather than escaped:\n%s", html)
	}
}

// Two live sessions in one checkout will edit each other's files, and each
// one's undo takes back the other's work. The page says so, like the listing.
func TestASharedCheckoutIsWarnedAbout(t *testing.T) {
	html := renderSharedCheckouts([]SharedCheckout{{Dir: "/w/p", Sessions: []string{"s_a", "s_b"}}})
	for _, want := range []string{"/w/p", "s_a", "s_b", "same directory"} {
		if !strings.Contains(html, want) {
			t.Errorf("the warning omits %q:\n%s", want, html)
		}
	}
}

func TestNoSessionsRendersNothingRatherThanAnEmptyBox(t *testing.T) {
	if html := renderSessionCards(nil); strings.TrimSpace(html) != "" {
		t.Errorf("an empty card list produced markup:\n%s", html)
	}
	if html := renderSharedCheckouts(nil); strings.TrimSpace(html) != "" {
		t.Errorf("no shared checkouts produced markup:\n%s", html)
	}
}

// Cost is a number people act on; a session with no recorded calls prints
// nothing rather than $0.00, exactly as the listing decided in I27.4.
func TestCostAppearsOnlyWhenRecorded(t *testing.T) {
	with := renderSessionCards([]SessionCard{{ID: "s_a", Name: "n", Cost: 4.2, CostKnown: true}})
	if !strings.Contains(with, "$4.20") {
		t.Errorf("a recorded cost is not shown:\n%s", with)
	}
	without := renderSessionCards([]SessionCard{{ID: "s_b", Name: "n"}})
	if strings.Contains(without, "$0.00") {
		t.Errorf("a session with no calls was reported as costing nothing:\n%s", without)
	}
}
