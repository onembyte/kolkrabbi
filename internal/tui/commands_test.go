package tui

import (
	"reflect"
	"testing"
)

func TestSlashSuggestionsShowRecentThenRemainingCommands(t *testing.T) {
	catalog := []CommandSpec{
		{Name: "help", Usage: "/help", Summary: "show commands"},
		{Name: "mode", Usage: "/mode <name>", Summary: "switch mode"},
		{Name: "model", Usage: "/model [id]", Summary: "list or switch model"},
		{Name: "update", Usage: "/update", Summary: "install update"},
		{Name: "exit", Usage: "/exit", Summary: "quit"},
	}
	history := NewCommandHistory(5)
	history.Record("/update")
	history.Record("/model vendor/id")
	history.Record("/update") // a repeated command becomes most recent, not duplicated

	got := SuggestCommands(catalog, "/", history.Recent(), 5)
	want := []string{"update", "model", "help", "mode", "exit"}
	if names := suggestionNames(got); !reflect.DeepEqual(names, want) {
		t.Fatalf("slash suggestions = %q, want %q", names, want)
	}
}

func TestSlashSuggestionsFilterLiveByCommandPrefix(t *testing.T) {
	catalog := []CommandSpec{
		{Name: "mode", Usage: "/mode <name>"},
		{Name: "model", Usage: "/model [id]"},
		{Name: "help", Usage: "/help"},
	}
	history := NewCommandHistory(5)
	history.Record("/model old")

	got := SuggestCommands(catalog, "/mo", history.Recent(), 5)
	if names := suggestionNames(got); !reflect.DeepEqual(names, []string{"model", "mode"}) {
		t.Fatalf("prefix suggestions = %q", names)
	}
	if got := SuggestCommands(catalog, "/model vendor/", history.Recent(), 5); got != nil {
		t.Fatalf("argument input kept command menu open: %#v", got)
	}
	if got := SuggestCommands(catalog, "ordinary prompt", history.Recent(), 5); got != nil {
		t.Fatalf("ordinary input opened slash menu: %#v", got)
	}
}

func suggestionNames(suggestions []CommandSpec) []string {
	names := make([]string, len(suggestions))
	for i, suggestion := range suggestions {
		names[i] = suggestion.Name
	}
	return names
}
