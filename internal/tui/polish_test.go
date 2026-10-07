package tui

import (
	"strings"
	"testing"
)

func TestModelPickerKeepsActiveEffortBesideLongModel(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestModelPicker([]ModelPickEntry{{
		ID: "anthropic/claude-opus", Name: "subscription · " + strings.Repeat("long description ", 8),
		Efforts: []string{"low", "medium", "high", "max"}, Effort: 3,
	}})
	for _, width := range []int{40, 60, 80, 120} {
		rows := c.modelPickerLines(width)
		row := rows[3]
		if !strings.Contains(row, "claude-opus") || !strings.Contains(row, "[max]") {
			t.Fatalf("width %d hides model or active effort: %q", width, row)
		}
		if cellWidth(row) > width {
			t.Fatalf("width %d overflow: %q", width, row)
		}
	}
}

func TestFooterKeepsModelAndEffortTogetherBeforeSessionTitle(t *testing.T) {
	m := New(Status{Model: "claude-opus", Effort: "max", Mode: "agent", Sandbox: "off",
		Approval: "ask", SessionName: strings.Repeat("a very long title ", 12)})
	for _, width := range []int{40, 60, 80, 120} {
		view := m.View(width, 24)
		found := false
		for _, row := range strings.Split(view, "\n") {
			if strings.Contains(row, "claude-opus") && strings.Contains(row, "effort max") {
				found = true
			}
		}
		if !found {
			t.Fatalf("width %d hides the model/effort pair:\n%s", width, view)
		}
	}
}

// Realistic model IDs are dated and vendor-prefixed. Once one is clipped,
// the fields after the effort overflow the row, and the row's own clip must
// not land on the effort: "effort hig…" hides exactly what the footer keeps.
func TestFooterNeverClipsTheEffortOfARealisticModel(t *testing.T) {
	for _, model := range []string{"anthropic/claude-sonnet-4", "anthropic/claude-opus-4-20250514", "openai/gpt-5.1-codex-max", "ollama/qwen2.5-coder:7b"} {
		for _, effort := range []string{"high", "max", "xhigh", "medium"} {
			m := New(Status{Model: model, Effort: effort, Mode: "agent", Sandbox: "off", Context: "12%", Cost: "$0.04",
				SessionName: "fix the footer", Folder: "~/src/kolkrabbi"})
			for width := 30; width <= 140; width++ {
				found := false
				for _, row := range strings.Split(m.View(width, 24), "\n") {
					if strings.Contains(row, "model ") && strings.Contains(row, "effort "+effort) {
						found = true
					}
				}
				if !found {
					t.Fatalf("%s/%s at width %d clips the effort:\n%s", model, effort, width, m.View(width, 24))
				}
			}
			// With nothing after the effort the row has no ellipsis to make
			// room for: a model that exactly fits is shown whole.
			alone := New(Status{Model: model, Effort: effort})
			exact := cellWidth(statusIndent + "model " + model + " · effort " + effort)
			if view := alone.View(exact, 24); !strings.Contains(view, "model "+model+" · effort "+effort) {
				t.Fatalf("%s/%s alone at its exact width %d was clipped:\n%s", model, effort, exact, view)
			}
		}
	}
}

func TestAgentWindowKeepsStateModelAndEffortWhenSummaryDoesNotFit(t *testing.T) {
	s := AgentStatus{Index: 1, State: "working", Model: "anthropic/claude-opus-4-20250514", Effort: "max", Summary: strings.Repeat("task ", 20)}
	row := agentWindowRow(s, 32)
	if cellWidth(row) > 32 || !strings.Contains(row, "working") || !strings.Contains(row, "max") || !strings.Contains(row, "opus") {
		t.Fatalf("worker identity was hidden: %q", row)
	}
}

func TestCompactActivityKeepsTheWheelBeforeClipping(t *testing.T) {
	for width := 4; width < 30; width++ {
		m := New(Status{Mode: "agent", Lifecycle: "thinking"})
		m.SetActivity(activityLine(0, "thinking"))
		view := m.View(width, 10)
		if !strings.Contains(view, "⠋") {
			t.Fatalf("width %d hides activity:\n%s", width, view)
		}
		if width >= cellWidth("⠋ thinking…") && !strings.Contains(view, "⠋ thinking…") {
			t.Fatalf("width %d clips the phase before removing the icon:\n%s", width, view)
		}
		for _, row := range strings.Split(view, "\n") {
			if cellWidth(row) > width {
				t.Fatalf("width %d overflow: %q", width, row)
			}
		}
	}
}

func TestShortFooterKeepsPauseBeforeOtherStatus(t *testing.T) {
	m := New(Status{Mode: "agent", Model: "claude-opus", Effort: "max", Approval: "ask",
		Paused: "paused · allowance · resumes 15:04"})
	for _, height := range []int{5, 6, 7} {
		view := m.View(160, height)
		if !strings.Contains(view, "resumes 15:04") {
			t.Fatalf("height %d hides the pause:\n%s", height, view)
		}
	}
}

func TestFooterShowsPauseAndIndependentCooldown(t *testing.T) {
	m := New(Status{Paused: "paused until 15:04", Cooling: "account cooling until 15:10"})
	if view := m.View(80, 24); !strings.Contains(view, "15:04") || !strings.Contains(view, "15:10") {
		t.Fatalf("pause hid an independent cooldown:\n%s", view)
	}
}

func TestModelPickerSanitizesExpandedEffortDial(t *testing.T) {
	entry := ModelPickEntry{ID: "model", Efforts: []string{"lo\nw", "\x1b[2Jhigh"}, Effort: 1}
	row := modelPickerRow("> ", entry, true, 80)
	if strings.ContainsAny(row, "\n\r\x1b") || !strings.Contains(row, "[high]") {
		t.Fatalf("unsafe effort display: %q", row)
	}
}

func TestModelPickerOwnsAndNormalizesEffortSelection(t *testing.T) {
	for _, index := range []int{-1, 9} {
		entries := []ModelPickEntry{{ID: "model", Efforts: []string{"low", "high"}, Effort: index}}
		c := NewController(Status{}, defaultDraftSize)
		c.RequestModelPicker(entries)
		entries[0].Efforts[0] = "changed"
		c.modelPickerLines(80)
		if got := c.handleModelPickerKey(Key{Kind: KeyEnter}).PickModel; got != "/model model low" {
			t.Fatalf("invalid effort index %d resolved %q", index, got)
		}
	}
}
