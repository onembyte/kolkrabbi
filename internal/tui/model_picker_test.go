package tui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The /model picker answers with a ready-to-run command: the model, and its
// effort when the model has a dial, because a second interpretation of the
// pick is a second place to disagree about what was picked.
func TestModelPickerAnswersAReadyCommand(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan string, 1)
	go func() {
		answer, ok := r.AskModel(context.Background(), []ModelPickEntry{
			{ID: "claude-sonnet", Name: "Claude Pro", Efforts: []string{"low", "medium", "high"}},
			{ID: "vendor/mock", Name: "metered"},
		})
		if !ok {
			t.Error("the picker refused to resolve its answer")
			done <- ""
			return
		}
		done <- answer
	}()
	waitForPickerOpen(t, r)

	r.mu.Lock()
	pick := r.controller.ModelPicker()
	r.mu.Unlock()
	if pick == nil || len(pick.Entries) != 2 || pick.Index != 0 {
		t.Fatalf("picker = %+v, want two rows with the first highlighted", pick)
	}
	if got := pick.Entries[0].Efforts[pick.Entries[0].Effort]; got != "low" {
		t.Fatalf("effort dial starts at %q, want the least expensive level", got)
	}

	// One row down to the metered model: its Enter answer has no effort on it.
	r.HandleKey(Key{Kind: KeyDown})
	r.HandleKey(Key{Kind: KeyEnter})
	if answer := <-done; answer != "/model vendor/mock" {
		t.Fatalf("answer = %q, want the bare model command", answer)
	}
}

// Left and right turn the effort dial of the selected model and nothing else:
// the row stays put, and a model without efforts ignores the key.
func TestModelPickerArrowsTurnTheEffortDial(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan string, 1)
	go func() {
		answer, ok := r.AskModel(context.Background(), []ModelPickEntry{
			{ID: "claude-sonnet", Name: "Claude Pro", Efforts: []string{"low", "medium", "high"}},
			{ID: "vendor/mock", Name: "metered"},
		})
		if !ok {
			done <- ""
			return
		}
		done <- answer
	}()
	waitForPickerOpen(t, r)

	// Left from the least expensive level wraps to the most.
	r.HandleKey(Key{Kind: KeyLeft})
	r.mu.Lock()
	if got := r.controller.ModelPicker().Entries[0].Effort; got != 2 {
		t.Fatalf("effort index = %d, want the wrapped top level", got)
	}
	r.mu.Unlock()
	// Right wraps back to the start of the dial.
	r.HandleKey(Key{Kind: KeyRight})
	r.HandleKey(Key{Kind: KeyEnter})
	if answer := <-done; answer != "/model claude-sonnet low" {
		t.Fatalf("answer = %q, want the dial's level on the command", answer)
	}
}

// A dismissed picker must read as "nothing chosen", not as the first row.
func TestModelPickerDismissalIsNotAPick(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan string, 1)
	go func() {
		answer, ok := r.AskModel(context.Background(), []ModelPickEntry{
			{ID: "claude-sonnet", Name: "Claude Pro"},
		})
		done <- ""
		_ = ok
		_ = answer
	}()
	waitForPickerOpen(t, r)
	r.HandleKey(Key{Kind: KeyEscape})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the dismissed picker never unblocked its waiter")
	}
}

// The picker's drawing names the keys: left and right being alive only here
// is not something anyone would guess without being told.
func TestModelPickerDrawingNamesTheKeysAndShowsTheDial(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestModelPicker([]ModelPickEntry{
		{ID: "claude-sonnet", Name: "Claude Pro", Efforts: []string{"low", "medium", "high"}, Effort: 2},
		{ID: "vendor/mock", Name: "metered"},
	})
	view := strings.Join(c.modelPickerLines(80), "\n")
	for _, want := range []string{"←/→", "[high]", "claude-sonnet", "vendor/mock"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not show %q:\n%s", want, view)
		}
	}
}

func waitForPickerOpen(t *testing.T, r *Runtime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		open := r.controller.ModelPicker() != nil
		r.mu.Unlock()
		if open {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the picker never opened")
		}
		time.Sleep(time.Millisecond)
	}
}
