package engine

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func messagesOf(chars int) []provider.Message {
	return []provider.Message{{Role: "user", Content: strings.Repeat("x", chars)}}
}

func TestMeasureContextPrefersTheProvidersOwnCount(t *testing.T) {
	// The provider reports what it actually read. An estimate that disagrees
	// with a measurement is just a worse measurement.
	usage := MeasureContext(128_000, 12_345, messagesOf(4_000))
	if !usage.Measured || usage.Used != 12_345 {
		t.Fatalf("usage = %+v, want the reported count", usage)
	}
}

func TestMeasureContextEstimatesOnlyBeforeAnyTurn(t *testing.T) {
	usage := MeasureContext(128_000, 0, messagesOf(4_000))
	if usage.Measured {
		t.Fatal("an estimate must never claim to be a measurement")
	}
	// Four characters per token, used as a floor.
	if usage.Used != 1_000 {
		t.Fatalf("estimate = %d, want 1000", usage.Used)
	}
}

func TestMeasureContextWithNoKnownWindowNeverCompacts(t *testing.T) {
	usage := MeasureContext(0, 500_000, messagesOf(10))
	if usage.Window != 0 {
		t.Fatalf("window = %d, want unknown", usage.Window)
	}
	// Kolkrabbi does not invent a limit it was not told, however full the
	// session looks.
	if usage.ShouldCompact() {
		t.Fatal("an unknown window must never trigger compaction")
	}
	if usage.Fraction() != 0 {
		t.Fatalf("fraction = %v, want no claim about an unknown window", usage.Fraction())
	}
}

func TestShouldCompactAtThreeQuartersOfTheWindow(t *testing.T) {
	for _, tt := range []struct {
		used int
		want bool
	}{
		{used: 0, want: false},
		{used: 95_999, want: false},
		{used: 96_000, want: true},
		{used: 127_000, want: true},
	} {
		usage := MeasureContext(128_000, tt.used, nil)
		if got := usage.ShouldCompact(); got != tt.want {
			t.Fatalf("used %d of 128000: ShouldCompact() = %v, want %v", tt.used, got, tt.want)
		}
	}
}

func TestContextUsageLabelIsEmptyWithoutAWindow(t *testing.T) {
	if label := MeasureContext(0, 100, nil).Label(); label != "" {
		t.Fatalf("label = %q, want nothing claimed", label)
	}
	label := MeasureContext(128_000, 12_345, nil).Label()
	if !strings.Contains(label, "12.3k") || !strings.Contains(label, "128k") {
		t.Fatalf("label = %q, want a readable used-of-window", label)
	}
}

func TestContextUsageNeverExceedsTheWindowInItsFraction(t *testing.T) {
	// A provider may report more than the catalog's window if the catalog is
	// stale. Reporting 140% would look like a bug in Kolkrabbi.
	usage := MeasureContext(1_000, 1_400, nil)
	if usage.Fraction() > 1 {
		t.Fatalf("fraction = %v, want it capped at the window", usage.Fraction())
	}
	if !usage.ShouldCompact() {
		t.Fatal("an over-full window must still ask for compaction")
	}
}

func TestFooterShowsHowFullTheWindowIs(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Mode: ModeCode, ContextWindow: 128_000}}

	agent.footer(provider.Meta{Model: "vendor/model", PromptTokens: 12_345, CompletionTokens: 100})

	if !strings.Contains(out.String(), "12.3k/128k ctx") {
		t.Fatalf("footer = %q, want the window usage", out.String())
	}
}

func TestFooterSaysNothingAboutAnUnknownWindow(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Mode: ModeCode}}

	agent.footer(provider.Meta{Model: "vendor/model", PromptTokens: 12_345})

	if strings.Contains(out.String(), "ctx") {
		t.Fatalf("footer = %q, want no claim about an unknown window", out.String())
	}
}
