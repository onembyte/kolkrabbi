package tui

import (
	"strings"
	"testing"
)

// Plan windows draw as meters in the status area: the used part in grey,
// what remains in purple, the percent beside, one meter per window on a
// row of their own. No windows, no row.
func TestPlanMetersDrawUsedGreyAndRemainingPurple(t *testing.T) {
	m := New(Status{Mode: "agent", Lifecycle: "ready", Limits: []PlanMeter{{Label: "5h", Used: 0.8}, {Label: "7d", Used: 0.5}}})
	var meters *viewRow
	for _, row := range m.viewRows(120, 20, 0) {
		if len(row.spans) > 0 {
			r := row
			meters = &r
			break
		}
	}
	if meters == nil {
		t.Fatal("no row carries the meters")
	}
	text := ""
	for _, span := range meters.spans {
		text += span.text
	}
	for _, want := range []string{"5h ", " 80%", "7d ", " 50%"} {
		if !strings.Contains(text, want) {
			t.Fatalf("meters row %q lacks %q", text, want)
		}
	}
	// The first meter: label, used, remaining, percent. Used is grey (the
	// muted meta style), remaining purple, twelve cells between them.
	// Heavy is spent, light is left, so the bar reads without colour too.
	var used, remaining *styledSpan
	for i := range meters.spans {
		span := &meters.spans[i]
		switch {
		case used == nil && strings.Contains(span.text, "━"):
			used = span
		case used != nil && remaining == nil && strings.Contains(span.text, "─"):
			remaining = span
		}
	}
	if used == nil || remaining == nil {
		t.Fatalf("meters row has no bar spans: %+v", meters.spans)
	}
	if used.style != styleMeta || remaining.style != stylePurple {
		t.Fatalf("used style %v remaining style %v, want grey then purple", used.style, remaining.style)
	}
	if cellWidth(used.text)+cellWidth(remaining.text) != planMeterCells || cellWidth(used.text) != 10 {
		t.Fatalf("bar = %q + %q, want %d cells with 10 used", used.text, remaining.text, planMeterCells)
	}
	if rows := New(Status{Mode: "agent"}).viewRows(120, 20, 0); func() bool {
		for _, row := range rows {
			if len(row.spans) > 0 {
				return true
			}
		}
		return false
	}() {
		t.Fatal("a session with no plan windows drew a meters row")
	}
}
