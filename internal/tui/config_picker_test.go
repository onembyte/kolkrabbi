package tui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The literal ask this leaf exists to answer: /config bare gets a searchable
// overlay, the same shape /model already has, rather than a static dump.
// Picking a row does not submit a command — a setting still needs its value
// typed — so the picker fills the composer's draft instead of answering with
// something meant to run on its own.
func TestConfigPickerFillsTheDraftWithSetAndTheChosenKey(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan bool, 1)
	go func() {
		_, ok := r.AskConfig(context.Background(), []SettingSpec{
			{Key: "effort", Value: "medium", Default: true, Summary: "model tier"},
			{Key: "model", Value: "openrouter/free", Summary: "default model"},
		})
		done <- ok
	}()
	waitForConfigPickerOpen(t, r)

	r.HandleKey(Key{Kind: KeyEnter})
	<-done

	r.mu.Lock()
	draft := r.controller.editor.Draft()
	r.mu.Unlock()
	if draft != "/config set effort " {
		t.Fatalf("draft = %q, want the set command ready for a value", draft)
	}
}

// A dismissed picker must not touch the draft at all.
func TestConfigPickerDismissalLeavesTheDraftAlone(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan bool, 1)
	go func() {
		_, ok := r.AskConfig(context.Background(), []SettingSpec{{Key: "effort"}})
		done <- ok
	}()
	waitForConfigPickerOpen(t, r)

	r.HandleKey(Key{Kind: KeyEscape})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Escape with no filter typed must close the overlay")
	}
	r.mu.Lock()
	draft := r.controller.editor.Draft()
	r.mu.Unlock()
	if draft != "" {
		t.Fatalf("draft = %q, want it untouched by a dismissal", draft)
	}
}

// H1's fuzzy tolerance, not just a static list: "rstrt" is not a literal
// substring of "auto_restart_after_update".
func TestConfigPickerFiltersLiveByTyping(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestConfigPicker([]SettingSpec{
		{Key: "effort", Summary: "model tier"},
		{Key: "auto_restart_after_update", Summary: "restart into the new version"},
	})
	c.HandleKey(Key{Kind: KeyText, Text: "rstrt"})

	pick := c.ConfigPicker()
	if len(pick.Entries) != 1 || pick.Entries[0].Key != "auto_restart_after_update" {
		t.Fatalf("filtered picker = %+v, want just the restart setting", pick)
	}
	if pick.Filter != "rstrt" {
		t.Fatalf("filter = %q, want the typed query", pick.Filter)
	}
}

// Escape backs out one step at a time, the same as /model: clear the filter
// first, close the overlay only once there is nothing left to clear.
func TestConfigPickerEscapeClearsTheFilterBeforeClosing(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestConfigPicker([]SettingSpec{{Key: "effort"}})
	c.HandleKey(Key{Kind: KeyText, Text: "eff"})
	c.HandleKey(Key{Kind: KeyEscape})
	if pick := c.ConfigPicker(); pick == nil || pick.Filter != "" {
		t.Fatalf("first Escape = %+v, want the filter cleared and the overlay still open", pick)
	}
	if effect := c.HandleKey(Key{Kind: KeyEscape}); !effect.PickDismissed {
		t.Fatalf("second Escape, with nothing left to clear, must dismiss the overlay: %+v", effect)
	}
}

// The value in effect is shown, so "what is my effort set to" is answered by
// opening the list rather than leaving the session to run `kolk config`.
func TestConfigPickerDrawingShowsTheValueInEffect(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestConfigPicker([]SettingSpec{
		{Key: "effort", Value: "medium", Default: true, Summary: "model tier"},
	})
	view := strings.Join(c.configPickerLines(80), "\n")
	for _, want := range []string{"effort", "medium", "(default)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not show %q:\n%s", want, view)
		}
	}
}

func waitForConfigPickerOpen(t *testing.T, r *Runtime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		open := r.controller.ConfigPicker() != nil
		r.mu.Unlock()
		if open {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the config picker never opened")
		}
		time.Sleep(time.Millisecond)
	}
}
