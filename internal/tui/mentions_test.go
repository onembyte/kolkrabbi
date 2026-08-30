package tui

import (
	"strings"
	"testing"
)

var projectFiles = []string{
	"README.md",
	"internal/engine/agent.go",
	"internal/engine/agent_test.go",
	"internal/tui/model.go",
	"main.go",
}

func TestAtCompletesFilePaths(t *testing.T) {
	got := SuggestFiles(projectFiles, "look at @agent", 8)

	if len(got) == 0 {
		t.Fatal("no suggestions for @agent")
	}
	for _, suggestion := range got {
		if !strings.Contains(suggestion.Name, "agent") {
			t.Fatalf("suggested %q for @agent", suggestion.Name)
		}
	}
}

func TestCompletingReplacesOnlyTheMention(t *testing.T) {
	got := SuggestFiles(projectFiles, "please read @model", 8)
	if len(got) == 0 {
		t.Fatal("no suggestions")
	}

	// The rest of what someone typed is not the completion's to rewrite.
	want := "please read @internal/tui/model.go"
	if got[0].Complete != want {
		t.Fatalf("completing gives %q, want %q", got[0].Complete, want)
	}
}

func TestAFinishedMentionIsNotReCompleted(t *testing.T) {
	// Once someone has typed past a mention they are editing a sentence, not
	// a path, and rewriting it under them would be the completion taking over.
	if got := SuggestFiles(projectFiles, "please read @model and explain", 8); len(got) != 0 {
		t.Fatalf("got %v, want nothing once the mention is behind the cursor", got)
	}
}

func TestAnEmptyMentionOffersTheProject(t *testing.T) {
	got := SuggestFiles(projectFiles, "see @", 3)
	if len(got) != 3 {
		t.Fatalf("got %d suggestions, want the limit", len(got))
	}
}

func TestTextWithNoMentionSuggestsNothing(t *testing.T) {
	for _, draft := range []string{"", "just a sentence", "/model gpt", "email a@b.com"} {
		if got := SuggestFiles(projectFiles, draft, 8); len(got) != 0 {
			t.Fatalf("%q suggested %v", draft, got)
		}
	}
}

func TestOnlyTheMentionBeingTypedCompletes(t *testing.T) {
	// An earlier, finished mention must not be re-completed when someone
	// starts a second one.
	got := SuggestFiles(projectFiles, "@main.go and @tui", 8)
	if len(got) == 0 {
		t.Fatal("no suggestions for the second mention")
	}
	if !strings.HasPrefix(got[0].Complete, "@main.go and @") {
		t.Fatalf("completing rewrote the earlier mention: %q", got[0].Complete)
	}
}

// "mdl" is not a literal substring of "model.go" — a fuzzy match finds the
// file anyway, the same tolerance every other picker now has.
func TestAtMentionsToleratesAScatteredQuery(t *testing.T) {
	got := SuggestFiles(projectFiles, "see @mdl", 8)
	if len(got) != 1 || got[0].Name != "internal/tui/model.go" {
		t.Fatalf("scattered mention query = %#v", got)
	}
}

func TestAMentionThatMatchesNothingSuggestsNothing(t *testing.T) {
	if got := SuggestFiles(projectFiles, "@zzzzz", 8); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestMentionsBeatCommandsInTheComposer(t *testing.T) {
	c := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	c.SetCommands([]CommandSpec{{Name: "/model", Usage: "/model", Summary: "pick a model"}}, 8)
	c.SetFiles(projectFiles)

	c.HandleKey(Key{Kind: KeyText, Text: "read @agent"})

	suggestions := c.Snapshot().Suggestions
	if len(suggestions) == 0 {
		t.Fatal("typing a mention offered nothing")
	}
	if !strings.Contains(suggestions[0].Name, "agent") {
		t.Fatalf("suggestions = %+v, want files", suggestions)
	}
}
