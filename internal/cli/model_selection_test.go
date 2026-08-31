package cli

import "testing"

func TestSplitModelSelectionAcceptsCodexXHigh(t *testing.T) {
	model, effort := splitModelSelection("gpt-5.6-terra xhigh")
	if model != "gpt-5.6-terra" || effort != "max" {
		t.Fatalf("splitModelSelection = (%q, %q), want (gpt-5.6-terra, max)", model, effort)
	}
}
