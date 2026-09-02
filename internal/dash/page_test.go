package dash

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/stats"
)

func day(n int) time.Time {
	return time.Date(2026, 8, n, 12, 0, 0, 0, time.UTC)
}

func sampleRecords() []stats.Record {
	return []stats.Record{
		{Kind: "call", Time: day(1), Turn: "t1", Model: "vendor/big", Mode: "code", Effort: "high",
			PromptTokens: 1000, CompletionTokens: 100, Cost: 0.50, Ms: 900},
		{Kind: "call", Time: day(2), Turn: "t2", Model: "vendor/small", Mode: "chat", Effort: "low",
			PromptTokens: 500, CompletionTokens: 50, Cost: 0.01, Ms: 200},
		{Kind: "call", Time: day(2), Turn: "t3", Model: "vendor/big", Mode: "code", Effort: "high",
			PromptTokens: 2000, CompletionTokens: 200, Cost: 1.25, Ms: 1500},
		{Kind: "rating", Time: day(2), Turn: "t3", Rating: 5},
	}
}

func TestPageShowsTheLeaderboardAndSpend(t *testing.T) {
	page := Page(sampleRecords(), 0, nil, nil)

	for _, want := range []string{"vendor/big", "vendor/small", "$1.76", "/dash"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
	// Charts are drawn on the server; a page that needs scripting to show a
	// number is a page that shows nothing in half the places it is opened.
	if strings.Contains(strings.ToLower(page), "<script") {
		t.Fatal("the page contains script")
	}
	if !strings.Contains(page, "<svg") {
		t.Fatal("the spend chart was not drawn")
	}
}

func TestPageIsWellFormedMarkup(t *testing.T) {
	// A hand-built page is easy to get subtly wrong; browsers hide that and
	// screenshots do not.
	decoder := xml.NewDecoder(strings.NewReader(Page(sampleRecords(), 0, nil, nil)))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity
	for {
		_, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return
			}
			t.Fatalf("page is not well-formed: %v", err)
		}
	}
}

func TestPageEscapesModelNames(t *testing.T) {
	records := []stats.Record{{
		Kind: "call", Time: day(1), Turn: "t1",
		Model: `vendor/<img src=x onerror=alert(1)>`, Mode: "code", Cost: 0.1,
	}}

	page := Page(records, 0, nil, nil)

	// A model id comes from a provider catalog, which is not something
	// Kolkrabbi controls.
	if strings.Contains(page, "<img src=x") {
		t.Fatalf("a model name was not escaped: %q", page)
	}
	if !strings.Contains(page, "&lt;img") {
		t.Fatalf("the escaped name is missing: %q", page)
	}
}

func TestPageWithNoDataExplainsWhatWillAppear(t *testing.T) {
	page := Page(nil, 0, nil, nil)

	if strings.Contains(page, "<svg") {
		t.Fatal("an empty dashboard drew an axis with no line on it")
	}
	for _, want := range []string{"No usage recorded yet", "kolk"} {
		if !strings.Contains(page, want) {
			t.Fatalf("empty state is missing %q: %q", want, page)
		}
	}
}

func TestPageDeclaresIncompleteTotals(t *testing.T) {
	page := Page(sampleRecords(), 3, nil, nil)

	if !strings.Contains(page, "incomplete") || !strings.Contains(page, "3") {
		t.Fatalf("skipped lines were not surfaced: %q", page)
	}
}

func TestPageShowsEffortAndMode(t *testing.T) {
	page := Page(sampleRecords(), 0, nil, nil)
	for _, want := range []string{"high", "low", "code", "chat"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page is missing the %q breakdown", want)
		}
	}
}

func TestEffortBreakdownFoldsLegacyNames(t *testing.T) {
	// Older records carry the pre-E7.1 spellings. quick/standard/deep/ultra and
	// low/medium/high/max are the same four levels, and showing both spellings
	// splits one level's spend across two rows that look like two levels.
	records := []stats.Record{
		{Kind: "call", Time: day(1), Turn: "t1", Model: "m", Effort: "standard", Cost: 1},
		{Kind: "call", Time: day(1), Turn: "t2", Model: "m", Effort: "medium", Cost: 2},
		{Kind: "call", Time: day(1), Turn: "t3", Model: "m", Effort: "ultra", Cost: 4},
	}

	page := Page(records, 0, nil, nil)

	if strings.Contains(page, ">standard<") || strings.Contains(page, ">ultra<") {
		t.Fatalf("legacy effort spellings were shown as separate levels: %q", page)
	}
	if !strings.Contains(page, ">medium<") || !strings.Contains(page, ">max<") {
		t.Fatalf("canonical levels missing: %q", page)
	}
	// The two medium rows are one level and must be added together.
	if !strings.Contains(page, "$3.00") {
		t.Fatalf("legacy and canonical spend were not combined: %q", page)
	}
}

func TestPageListsRecentSessions(t *testing.T) {
	records := append(sampleRecords(),
		stats.Record{Kind: "call", Time: day(3), Session: "sess-a", Turn: "t9",
			Model: "vendor/big", Mode: "code", Cost: 0.75, PromptTokens: 900})

	page := Page(records, 0, nil, nil)

	if !strings.Contains(page, "sess-a") {
		t.Fatalf("recent sessions are missing: %q", page)
	}
	if !strings.Contains(page, "$0.75") {
		t.Fatalf("session cost is missing: %q", page)
	}
}

func TestSessionsWithoutAnIDAreNotListed(t *testing.T) {
	// Records from before sessions were tagged carry no id. A blank row in a
	// session table is a row nobody can act on.
	page := Page(sampleRecords(), 0, nil, nil)
	if strings.Contains(page, "<h2>Recent sessions</h2>") {
		t.Fatalf("an untagged history produced an empty session table: %q", page)
	}
}
