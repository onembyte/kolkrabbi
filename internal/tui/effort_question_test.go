package tui

import (
	"strings"
	"testing"
)

func TestEffortQuestionStartsAtCurrentLevelAndKeepsDraft(t *testing.T) {
	c := NewController(Status{Lifecycle: "ready"}, defaultDraftSize)
	c.HandleKey(Key{Kind: KeyText, Text: "unfinished request"})
	c.RequestQuestion(Question{
		Title: "effort", Prompt: "Choose effort", InitialIndex: 1,
		Options: []string{"low", "medium", "high", "max", "ultra"},
	})
	if c.QuestionIndex() != 1 || !strings.Contains(c.View(80, 24), "effort") {
		t.Fatalf("current effort not highlighted in named overlay: %s", c.View(80, 24))
	}
	c.HandleKey(Key{Kind: KeyDown})
	effect := c.HandleKey(Key{Kind: KeyEnter})
	if reply := c.chosen(effect); reply.dismissed || reply.option != "high" {
		t.Fatalf("picked %q, want high", reply.option)
	}
	if c.Question() != nil || c.Snapshot().Draft != "unfinished request" {
		t.Fatalf("overlay changed composer: %+v", c.Snapshot())
	}
}

func TestQuestionInitialIndexIsClampedAndTitleIsSanitized(t *testing.T) {
	for _, index := range []int{-1, 99} {
		c := NewController(Status{}, defaultDraftSize)
		c.RequestQuestion(Question{Title: "effort\x1b[2J", InitialIndex: index, Options: []string{"low", "high"}})
		if c.QuestionIndex() != 0 || strings.Contains(c.View(80, 24), "\x1b[2J") {
			t.Fatalf("invalid index/title leaked: %q", c.View(80, 24))
		}
		if effect := c.HandleKey(Key{Kind: KeyEscape}); !effect.ChoiceDismissed {
			t.Fatal("Escape did not dismiss")
		}
	}
}
