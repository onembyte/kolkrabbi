package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTitleFromInputNeverSplitsARune(t *testing.T) {
	// A prompt in any language but English hits this: the cut is at 60 bytes.
	for _, filler := range []string{"é", "→", "🐙", "ä"} {
		for _, offset := range []int{0, 1, 2, 3} {
			s := New(t.TempDir(), "vendor/model")
			s.SetTitleFromInput(strings.Repeat("a", offset) + strings.Repeat(filler, 80))
			if !utf8.ValidString(s.Title) {
				t.Fatalf("title from %q at offset %d is not valid UTF-8: %q", filler, offset, s.Title)
			}
		}
	}
}

func TestTitleFromInputKeepsShortPromptsWhole(t *testing.T) {
	s := New(t.TempDir(), "vendor/model")
	s.SetTitleFromInput("  fix   the flaky test\n")
	if s.Title != "fix the flaky test" {
		t.Fatalf("title = %q", s.Title)
	}
}

func TestTitleFromInputDoesNotReplaceAnExistingTitle(t *testing.T) {
	s := New(t.TempDir(), "vendor/model")
	s.Title = "chosen by a person"
	s.SetTitleFromInput("something else entirely")
	if s.Title != "chosen by a person" {
		t.Fatalf("title = %q", s.Title)
	}
}

func TestAutoTitleIsMarkedAndRenamingClearsIt(t *testing.T) {
	s := New(t.TempDir(), "vendor/model")
	s.SetTitleFromInput("write a tokenizer")
	if !s.TitleIsAuto() {
		t.Fatal("a title derived from input is not a title the user chose")
	}
	// Kolkrabbi may improve its own guess; it may never overwrite a name a
	// person typed.
	s.SetTitle("the parser work")
	if s.TitleIsAuto() {
		t.Fatal("an explicitly set title is still marked automatic")
	}
	if s.Title != "the parser work" {
		t.Fatalf("title = %q", s.Title)
	}
}
