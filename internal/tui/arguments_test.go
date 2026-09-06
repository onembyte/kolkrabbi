package tui

import (
	"strings"
	"testing"
)

func modeCatalog() []CommandSpec {
	return []CommandSpec{
		{Name: "mode", Usage: "/mode <chat|code|agent>", Summary: "switch mode",
			Choices: []Choice{{Words: []string{"chat", "code", "agent"}}}},
		{Name: "permissions", Usage: "/permissions [ask|auto-approve|full-auto]",
			Choices: []Choice{{Words: []string{"ask", "auto-approve", "full-auto"}}}},
		{Name: "key", Usage: "/key [<provider>] | - | --why | --backend <keychain|file>",
			Choices: []Choice{
				{Words: []string{"openrouter", "google", "xai", "-", "--why", "--backend"}},
				{After: []string{"--backend"}, Words: []string{"keychain", "file"}},
			}},
		{Name: "help", Usage: "/help"},
	}
}

// typed is a fresh composer with the catalog and the given draft typed in.
func typed(catalog []CommandSpec, draft string) *Controller {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.SetCommands(catalog, 5)
	controller.HandleKey(Key{Kind: KeyText, Text: draft})
	return controller
}

func names(controller *Controller) string {
	return strings.Join(suggestionNames(controller.Snapshot().Suggestions), ",")
}

// Every command with a fixed vocabulary completes it: the word being typed is
// matched against the words allowed at that position, Tab fills the match,
// and Enter then runs the command — the way Claude Code's composer does.
func TestArgumentSuggestionsCompleteACommandsWords(t *testing.T) {
	controller := typed(modeCatalog(), "/mode age")
	if got := names(controller); got != "agent" {
		t.Fatalf("/mode age suggested %q, want agent", got)
	}
	if effect := controller.HandleKey(Key{Kind: KeyTab}); effect.Submit != "" || controller.Snapshot().Draft != "/mode agent" {
		t.Fatalf("tab = %#v, draft %q, want the draft filled and nothing sent", effect, controller.Snapshot().Draft)
	}
	if effect := controller.HandleKey(Key{Kind: KeyEnter}); effect.Submit != "/mode agent" {
		t.Fatalf("enter after tab = %#v, want /mode agent sent", effect)
	}
	for draft, want := range map[string]string{
		"/mode ":            "chat,code,agent", // an empty argument lists every word
		"/permissions fa":   "full-auto",       // a scattered query still finds its word
		"/key --backend ke": "keychain",        // a second position has its own vocabulary
		"/help me":          "",                // no vocabulary, no suggestion
		"/mode zzz":         "",                // an unknown word gets no guess
	} {
		if got := names(typed(modeCatalog(), draft)); got != want {
			t.Errorf("%q suggested %q, want %q", draft, got, want)
		}
	}
	controller = typed(modeCatalog(), "/key --backend ke")
	controller.HandleKey(Key{Kind: KeyTab})
	if got := controller.Snapshot().Draft; got != "/key --backend keychain" {
		t.Fatalf("draft after tab = %q", got)
	}
}

// Enter with nothing highlighted and a partial word fills the first match
// instead of sending a command that would be refused; the next Enter sends
// it. A word typed in full is sent at once.
func TestEnterFillsTheOnlyArgumentMatchBeforeSending(t *testing.T) {
	controller := typed(modeCatalog(), "/mode age")
	if effect := controller.HandleKey(Key{Kind: KeyEnter}); effect.Submit != "" || controller.Snapshot().Draft != "/mode agent" {
		t.Fatalf("enter on a partial word = %#v, draft %q", effect, controller.Snapshot().Draft)
	}
	if effect := controller.HandleKey(Key{Kind: KeyEnter}); effect.Submit != "/mode agent" {
		t.Fatalf("second enter = %#v", effect)
	}
	controller = typed(modeCatalog(), "/mode code")
	if effect := controller.HandleKey(Key{Kind: KeyEnter}); effect.Submit != "/mode code" {
		t.Fatalf("enter on a full word = %#v, draft %q, suggestions %q; want it sent", effect, controller.Snapshot().Draft, names(controller))
	}
	controller = typed(modeCatalog(), "/mode c")
	if effect := controller.HandleKey(Key{Kind: KeyEnter}); effect.Submit != "" || controller.Snapshot().Draft != "/mode chat" {
		t.Fatalf("enter on two matches = %#v, draft %q", effect, controller.Snapshot().Draft)
	}
}

// /config's verbs take a setting key after them, and /plans login takes the
// same provider-plan pairs /plogin does.
func TestVerbsCompleteTheirOwnArguments(t *testing.T) {
	catalog := []CommandSpec{
		{Name: "config", Usage: "/config [get <k> | set <k> <v> | unset <k> | show]",
			Choices: []Choice{{Words: []string{"get", "set", "unset", "show"}}}},
		{Name: "plans", Usage: "/plans [filter] | login <provider> <plan>",
			Choices: []Choice{{Words: []string{"login"}}}},
	}
	fresh := func(draft string) *Controller {
		controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
		controller.SetCommands(catalog, 5)
		controller.SetSettings([]SettingSpec{{Key: "theme", Value: "kolkrabbi", Default: true, Summary: "the look"}, {Key: "effort", Value: "medium", Default: true}})
		controller.SetPlans([]PlanSpec{{Provider: "anthropic", Name: "Claude Max", Auth: "provider CLI"}})
		controller.HandleKey(Key{Kind: KeyText, Text: draft})
		return controller
	}
	if got := names(fresh("/config se")); !strings.Contains(","+got+",", ",set,") {
		t.Fatalf("/config se suggested %q, want the verb among them", got)
	}
	controller := fresh("/config unset th")
	if got := names(controller); got != "theme" {
		t.Fatalf("/config unset th suggested %q", got)
	}
	controller.HandleKey(Key{Kind: KeyTab})
	if got := controller.Snapshot().Draft; got != "/config unset theme" {
		t.Fatalf("draft = %q", got)
	}
	controller = fresh("/plans login cla")
	if got := controller.Snapshot().Suggestions; len(got) != 1 || got[0].Complete != "/plans login anthropic Claude Max" {
		t.Fatalf("/plans login cla suggested %#v", got)
	}
}
