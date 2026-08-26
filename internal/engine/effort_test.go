package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestNormalizeEffort(t *testing.T) {
	tests := []struct {
		input     string
		want      string
		wantValid bool
	}{
		// Canonical
		{"low", engine.EffortLow, true},
		{"medium", engine.EffortMedium, true},
		{"high", engine.EffortHigh, true},
		{"max", engine.EffortMax, true},

		// Numeric aliases
		{"1", engine.EffortLow, true},
		{"2", engine.EffortMedium, true},
		{"3", engine.EffortHigh, true},
		{"4", engine.EffortMax, true},

		// Short aliases
		{"l", engine.EffortLow, true},
		{"m", engine.EffortMedium, true},
		{"med", engine.EffortMedium, true},
		{"h", engine.EffortHigh, true},
		{"x", engine.EffortMax, true},

		// Legacy aliases
		{"quick", engine.EffortLow, true},
		{"standard", engine.EffortMedium, true},
		{"deep", engine.EffortHigh, true},
		{"ultra", engine.EffortMax, true},

		// Trimming & Case-insensitivity
		{"  HIGH  ", engine.EffortHigh, true},
		{"MeDiUm", engine.EffortMedium, true},
		{" 1 ", engine.EffortLow, true},
		{"STANDARD", engine.EffortMedium, true},

		// Invalid inputs
		{"", "", false},
		{"   ", "", false},
		{"0", "", false},
		{"5", "", false},
		{"unknown", "", false},
		{"ultradeep", "", false},
	}

	for _, tt := range tests {
		got, ok := engine.NormalizeEffort(tt.input)
		if ok != tt.wantValid {
			t.Errorf("NormalizeEffort(%q) valid = %v, want %v", tt.input, ok, tt.wantValid)
		}
		if got != tt.want {
			t.Errorf("NormalizeEffort(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAgentSetEffort(t *testing.T) {
	ag := engine.New(engine.Options{
		Model: "mock/base",
	})

	// Default effort should be medium
	if ag.Effort != engine.EffortMedium {
		t.Fatalf("ag.Effort default = %q, want %q", ag.Effort, engine.EffortMedium)
	}

	// Set via canonical name
	if err := ag.SetEffort("high"); err != nil {
		t.Fatalf("SetEffort(high) unexpected error: %v", err)
	}
	if ag.Effort != engine.EffortHigh {
		t.Errorf("ag.Effort = %q, want %q", ag.Effort, engine.EffortHigh)
	}

	// Set via numeric alias
	if err := ag.SetEffort("1"); err != nil {
		t.Fatalf("SetEffort(1) unexpected error: %v", err)
	}
	if ag.Effort != engine.EffortLow {
		t.Errorf("ag.Effort = %q, want %q", ag.Effort, engine.EffortLow)
	}

	// Set via legacy alias
	if err := ag.SetEffort("ultra"); err != nil {
		t.Fatalf("SetEffort(ultra) unexpected error: %v", err)
	}
	if ag.Effort != engine.EffortMax {
		t.Errorf("ag.Effort = %q, want %q", ag.Effort, engine.EffortMax)
	}

	// Invalid input
	if err := ag.SetEffort("invalid"); err == nil {
		t.Errorf("SetEffort(invalid) expected error, got nil")
	}
}

func TestAgentModelForEffortResolution(t *testing.T) {
	ag := engine.New(engine.Options{
		Model: "mock/base",
		Tiers: map[string]string{
			engine.EffortHigh: "frontier/high",
			"quick":           "legacy/quick",
		},
	})

	// Unset tier inherits base model
	if got := ag.ModelForEffort(engine.EffortMedium); got != "mock/base" {
		t.Errorf("ModelForEffort(medium) = %q, want base model mock/base", got)
	}

	// Canonical tier resolves
	if got := ag.ModelForEffort(engine.EffortHigh); got != "frontier/high" {
		t.Errorf("ModelForEffort(high) = %q, want frontier/high", got)
	}
	// Numeric alias resolves canonical tier
	if got := ag.ModelForEffort("3"); got != "frontier/high" {
		t.Errorf("ModelForEffort(3) = %q, want frontier/high", got)
	}

	// Legacy tier key in Tiers resolves
	if got := ag.ModelForEffort(engine.EffortLow); got != "legacy/quick" {
		t.Errorf("ModelForEffort(low) = %q, want legacy/quick", got)
	}
	if got := ag.ModelForEffort("quick"); got != "legacy/quick" {
		t.Errorf("ModelForEffort(quick) = %q, want legacy/quick", got)
	}
}

func TestMaxRoundsFor(t *testing.T) {
	cases := []struct {
		mode   string
		effort string
		want   int
	}{
		{engine.ModeCode, engine.EffortLow, 4},
		{engine.ModeCode, engine.EffortMedium, 12},
		{engine.ModeCode, engine.EffortHigh, 24},
		{engine.ModeCode, engine.EffortMax, 50},
		{engine.ModeCode, "1", 4},
		{engine.ModeCode, "quick", 4},
		{engine.ModeCode, "ultra", 50},

		{engine.ModeChat, engine.EffortLow, 2},
		{engine.ModeChat, engine.EffortMedium, 6},
		{engine.ModeChat, engine.EffortHigh, 12},
		{engine.ModeChat, engine.EffortMax, 20},
		{engine.ModeChat, "2", 6},
		{engine.ModeChat, "standard", 6},
	}
	for _, tc := range cases {
		if got := engine.MaxRoundsFor(tc.mode, tc.effort); got != tc.want {
			t.Errorf("MaxRoundsFor(%s, %s) = %d, want %d", tc.mode, tc.effort, got, tc.want)
		}
	}
}

func TestTimeoutForEffort(t *testing.T) {
	cases := []struct {
		effort string
		want   time.Duration
	}{
		{engine.EffortLow, 30 * time.Second},
		{engine.EffortMedium, 120 * time.Second},
		{engine.EffortHigh, 300 * time.Second},
		{engine.EffortMax, 600 * time.Second},
		{"1", 30 * time.Second},
		{"quick", 30 * time.Second},
		{"ultra", 600 * time.Second},
		{"unknown", 120 * time.Second},
	}
	for _, tc := range cases {
		if got := engine.TimeoutForEffort(tc.effort); got != tc.want {
			t.Errorf("TimeoutForEffort(%s) = %v, want %v", tc.effort, got, tc.want)
		}
	}
}

func TestMaxTasksForEffort(t *testing.T) {
	cases := []struct {
		effort string
		want   int
	}{
		{engine.EffortLow, 1},
		{engine.EffortMedium, 2},
		{engine.EffortHigh, 4},
		{engine.EffortMax, 6},
		{"1", 1},
		{"quick", 1},
		{"4", 6},
		{"ultra", 6},
		{"unknown", 2},
	}
	for _, tc := range cases {
		if got := engine.MaxTasksForEffort(tc.effort); got != tc.want {
			t.Errorf("MaxTasksForEffort(%s) = %d, want %d", tc.effort, got, tc.want)
		}
	}
}

func TestTurnExceedsMaxToolRounds(t *testing.T) {
	// EffortLow allows 4 tool rounds in code mode.
	// We simulate 5 consecutive rounds of tool calls to trigger the limit.
	var steps []enginetest.Step
	for i := 0; i < 5; i++ {
		steps = append(steps, enginetest.Step{
			Text: fmt.Sprintf("Step %d", i+1),
			ToolCalls: []provider.ToolCall{{
				ID: fmt.Sprintf("call_%d", i+1),
				Function: provider.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"does_not_exist.txt"}`,
				},
			}},
		})
	}

	srv := enginetest.New(steps...)
	defer srv.Close()

	ag, _, _, _ := newTestAgent(t, srv, engine.ModeCode)
	if err := ag.SetEffort(engine.EffortLow); err != nil {
		t.Fatal(err)
	}

	err := ag.RunTurn(context.Background(), "run forever")
	if err == nil {
		t.Fatalf("expected error exceeding max rounds, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded maximum tool rounds (4) for low effort") {
		t.Errorf("error = %q, want 'exceeded maximum tool rounds (4) for low effort'", err.Error())
	}
}
