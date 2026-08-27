package tui

import (
	"strings"
	"testing"
)

// Shift+Tab arrives as CSI Z. Before this it fell into the unknown-CSI branch,
// which is indistinguishable from the key doing nothing at all.
func TestDecoderReadsShiftTabAsItsOwnKey(t *testing.T) {
	decoder := NewDecoder()
	keys := decoder.Feed([]byte("\x1b[Z"))
	if len(keys) != 1 || keys[0].Kind != KeyShiftTab {
		t.Fatalf("shift+tab decoded as %#v", keys)
	}
	// Tab still completes. The two keys must not collapse into one.
	if keys := decoder.Feed([]byte("\t")); len(keys) != 1 || keys[0].Kind != KeyTab {
		t.Fatalf("tab decoded as %#v", keys)
	}
}

func TestShiftTabAsksTheSurfaceToCycleTheTier(t *testing.T) {
	controller := NewController(Status{Mode: "code", Approval: "ask"}, 1024)
	effect := controller.HandleKey(Key{Kind: KeyShiftTab})
	if !effect.CyclePermission {
		t.Fatalf("shift+tab did not request a tier change: %#v", effect)
	}
	// The controller does not decide what the next tier is: that belongs to the
	// engine. It must not have invented one.
	if got := controller.View(80, 10); !strings.Contains(got, "⏵ ask") {
		t.Fatalf("controller changed the tier by itself:\n%s", got)
	}
}

// A completion list is open; Tab completes, so Shift+Tab must not.
func TestShiftTabCyclesEvenWithSuggestionsOpen(t *testing.T) {
	controller := NewController(Status{Mode: "code", Approval: "ask"}, 1024)
	controller.SetCommands([]CommandSpec{{Name: "mode", Usage: "/mode <name>"}}, 5)
	controller.HandleKey(Key{Kind: KeyText, Text: "/"})
	if effect := controller.HandleKey(Key{Kind: KeyShiftTab}); !effect.CyclePermission {
		t.Fatalf("shift+tab was swallowed by the suggestion list: %#v", effect)
	}
}

func TestRuntimeShowsTheTierTheSurfaceReturns(t *testing.T) {
	calls := 0
	runtime := NewRuntime(RuntimeOptions{
		Status: Status{Mode: "code", Approval: "ask"},
		CyclePermission: func() string {
			calls++
			return "full-auto"
		},
	})
	runtime.HandleKey(Key{Kind: KeyShiftTab})
	if calls != 1 {
		t.Fatalf("cycle called %d times, want 1", calls)
	}
	if got := runtime.Snapshot().Status.Approval; got != "full-auto" {
		t.Fatalf("footer tier = %q, want the one the surface returned", got)
	}
}

// A runtime with no cycle seam must not panic or pretend the tier changed.
func TestShiftTabIsInertWithoutASurfaceSeam(t *testing.T) {
	runtime := NewRuntime(RuntimeOptions{Status: Status{Mode: "code", Approval: "ask"}})
	runtime.HandleKey(Key{Kind: KeyShiftTab})
	if got := runtime.Snapshot().Status.Approval; got != "ask" {
		t.Fatalf("tier changed with no seam wired: %q", got)
	}
}
