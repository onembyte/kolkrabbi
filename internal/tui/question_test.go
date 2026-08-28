package tui

import (
	"context"
	"strings"
	"testing"
	"time"
)

func openQuestion(t *testing.T) (*Runtime, chan struct {
	answer string
	ok     bool
}) {
	t.Helper()
	r := NewRuntime(RuntimeOptions{})
	done := make(chan struct {
		answer string
		ok     bool
	}, 1)
	go func() {
		answer, ok := r.Ask(context.Background(), Question{
			Prompt:  "Which database should the API use?",
			Options: []string{"Postgres", "SQLite", "MySQL"},
		})
		done <- struct {
			answer string
			ok     bool
		}{answer, ok}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		open := r.controller.Question() != nil
		r.mu.Unlock()
		if open {
			return r, done
		}
		if time.Now().After(deadline) {
			t.Fatal("the picker never opened")
		}
		time.Sleep(time.Millisecond)
	}
}

// The point of the whole checkpoint: the answer is picked with the arrow keys,
// not typed.
func TestArrowKeysAndEnterAnswerTheQuestion(t *testing.T) {
	r, done := openQuestion(t)
	r.HandleKey(Key{Kind: KeyDown})
	r.HandleKey(Key{Kind: KeyEnter})
	select {
	case result := <-done:
		if !result.ok || result.answer != "SQLite" {
			t.Errorf("answer = %q ok = %v, want SQLite", result.answer, result.ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the picker never returned an answer")
	}
}

// The model is told to put its recommendation first, so Enter alone is a real
// answer rather than an accident.
func TestEnterTakesTheFirstOption(t *testing.T) {
	r, done := openQuestion(t)
	r.HandleKey(Key{Kind: KeyEnter})
	if result := <-done; !result.ok || result.answer != "Postgres" {
		t.Errorf("answer = %q ok = %v, want Postgres", result.answer, result.ok)
	}
}

// The list is short enough to see whole, so stopping at either end would just
// be a dead key.
func TestTheSelectionWrapsAtBothEnds(t *testing.T) {
	r, done := openQuestion(t)
	r.HandleKey(Key{Kind: KeyUp}) // from the first row, round to the last
	r.HandleKey(Key{Kind: KeyEnter})
	if result := <-done; result.answer != "MySQL" {
		t.Errorf("answer = %q, want the selection to wrap to the last option", result.answer)
	}

	r2, done2 := openQuestion(t)
	for range 3 {
		r2.HandleKey(Key{Kind: KeyDown})
	}
	r2.HandleKey(Key{Kind: KeyEnter})
	if result := <-done2; result.answer != "Postgres" {
		t.Errorf("answer = %q, want the selection to wrap back to the first", result.answer)
	}
}

// Someone who can already see the answer should not have to walk to it.
func TestANumberPicksItsRowOutright(t *testing.T) {
	r, done := openQuestion(t)
	r.HandleKey(Key{Kind: KeyText, Text: "3"})
	if result := <-done; !result.ok || result.answer != "MySQL" {
		t.Errorf("answer = %q ok = %v, want MySQL", result.answer, result.ok)
	}
}

// A number with no row must not answer anything.
func TestANumberOutsideTheListDoesNothing(t *testing.T) {
	r, done := openQuestion(t)
	r.HandleKey(Key{Kind: KeyText, Text: "9"})
	select {
	case result := <-done:
		t.Fatalf("an out-of-range number answered the question: %q", result.answer)
	case <-time.After(100 * time.Millisecond):
	}
	r.HandleKey(Key{Kind: KeyEnter})
	<-done
}

// Dismissing is not answering, and must not be reported as picking anything.
func TestDismissingLeavesTheQuestionUnanswered(t *testing.T) {
	r, done := openQuestion(t)
	r.HandleKey(Key{Kind: KeyInterrupt})
	result := <-done
	if result.ok || result.answer != "" {
		t.Errorf("dismissal returned %q ok = %v, want no answer", result.answer, result.ok)
	}
	r.mu.Lock()
	still := r.controller.Question()
	r.mu.Unlock()
	if still != nil {
		t.Error("the picker stayed on screen after being dismissed")
	}
}

// An interrupted turn must take its picker down with it.
func TestACancelledTurnClosesThePicker(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		_, ok := r.Ask(ctx, Question{Prompt: "Which?", Options: []string{"a", "b"}})
		done <- ok
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		open := r.controller.Question() != nil
		r.mu.Unlock()
		if open || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Error("a cancelled question reported an answer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled question never returned")
	}
	r.mu.Lock()
	still := r.controller.Question()
	r.mu.Unlock()
	if still != nil {
		t.Error("the picker stayed on screen after the turn was cancelled")
	}
}

// Stacking questions in front of the person is worse than making the model wait.
func TestASecondQuestionIsRefusedWhileOneIsOpen(t *testing.T) {
	r, done := openQuestion(t)
	if _, ok := r.Ask(context.Background(), Question{Prompt: "Other?", Options: []string{"x", "y"}}); ok {
		t.Error("a second question was accepted while one was already open")
	}
	r.HandleKey(Key{Kind: KeyEnter})
	<-done
}

func TestThePickerShowsEveryOptionAndMarksTheSelection(t *testing.T) {
	r, done := openQuestion(t)
	r.HandleKey(Key{Kind: KeyDown})
	r.mu.Lock()
	view := r.controller.View(60, 24)
	r.mu.Unlock()
	for _, option := range []string{"Postgres", "SQLite", "MySQL"} {
		if !strings.Contains(view, option) {
			t.Errorf("option %q is not on screen", option)
		}
	}
	if !strings.Contains(view, "Which database should the API use?") {
		t.Error("the question itself is not on screen")
	}
	if !strings.Contains(view, "> 2  SQLite") {
		t.Errorf("the selected row is not marked:\n%s", view)
	}
	r.HandleKey(Key{Kind: KeyEnter})
	<-done
}
