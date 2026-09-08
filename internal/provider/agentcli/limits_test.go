package agentcli

import (
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Every window the vendor reports rides on the reply's Meta, the latest
// reading per window, so the session can show where the plan stands.
func TestCollectCarriesThePlanWindows(t *testing.T) {
	_, meta, err := Collect([]Event{
		{Kind: EventLimit, LimitWindow: "seven_day", LimitUtilization: 0.4, LimitResets: 1788220800},
		{Kind: EventLimit, LimitWindow: "five_hour", LimitUtilization: 0.8, LimitResets: 1788000000},
		{Kind: EventLimit, LimitWindow: "seven_day", LimitUtilization: 0.5, LimitResets: 1788220800, LimitWarning: true},
		{Kind: EventMessageCompleted, Text: "done"},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := []provider.PlanLimit{
		{Window: "seven_day", Used: 0.5, Resets: 1788220800},
		{Window: "five_hour", Used: 0.8, Resets: 1788000000},
	}
	if len(meta.Limits) != 2 || meta.Limits[0] != want[0] || meta.Limits[1] != want[1] {
		t.Fatalf("limits = %+v, want %+v", meta.Limits, want)
	}
}

// A plain reading is not a warning: nothing is written into the transcript
// and no progress step says "plan limit" for it.
func TestAPlainLimitReadingIsQuiet(t *testing.T) {
	var seen []provider.ProgressEvent
	observe := func(event provider.ProgressEvent) { seen = append(seen, event) }
	observeProviderEvent(observe, Event{Kind: EventLimit, LimitWindow: "seven_day", LimitUtilization: 0.4}, map[string]string{})
	if len(seen) != 0 {
		t.Fatalf("a reading produced progress %+v", seen)
	}
	observeProviderEvent(observe, Event{Kind: EventLimit, LimitWindow: "seven_day", LimitUtilization: 0.9, LimitWarning: true}, map[string]string{})
	if len(seen) != 1 || seen[0].Kind != provider.ProgressLimit {
		t.Fatalf("a warning produced %+v", seen)
	}
}
