package tui

import (
	"reflect"
	"strings"
	"testing"
)

func TestSuggestPlanLoginsFiltersAndCompletes(t *testing.T) {
	got := SuggestPlanLogins([]PlanSpec{
		{Provider: "anthropic", Name: "Claude Pro"},
		{Provider: "anthropic", Name: "Claude Max"},
		{Provider: "openai", Name: "ChatGPT Pro"},
	}, "/plogin claude m", 8)
	if len(got) != 1 || got[0].Complete != "/plogin anthropic Claude Max" {
		t.Fatalf("plan login suggestions = %#v", got)
	}
	if !reflect.DeepEqual(got[0].Usage, "/plogin anthropic Claude Max") {
		t.Fatalf("completion usage = %q", got[0].Usage)
	}
}

// The login picker must not offer a plan the login command refuses. Six of the
// catalogue's rows are signed into with an API key, and `kolk plans login`
// rejects anything that is not a provider CLI — so those rows were menu entries
// whose only possible outcome was an error.
func TestThePlanLoginPickerOffersOnlyWhatItCanSignInto(t *testing.T) {
	plans := []PlanSpec{
		{Provider: "anthropic", Name: "Claude Max", Auth: "provider CLI"},
		{Provider: "ollama", Name: "Ollama Pro", Auth: "provider CLI"},
		{Provider: "xai", Name: "Grok", Auth: "API key"},
		{Provider: "cohere", Name: "Cohere Developer", Auth: "API key"},
	}
	got := SuggestPlanLogins(plans, "/plogin ", 8)
	for _, suggestion := range got {
		if strings.Contains(suggestion.Name, "Grok") || strings.Contains(suggestion.Name, "Cohere") {
			t.Errorf("the picker offers %q, which the login command refuses", suggestion.Name)
		}
	}
	if len(got) != 2 {
		t.Errorf("offered %d plans, want the 2 with a provider CLI", len(got))
	}
}

// The words can arrive in any order, the same as every other picker.
func TestThePlanLoginPickerMatchesWordsInAnyOrder(t *testing.T) {
	plans := []PlanSpec{{Provider: "anthropic", Name: "Claude Max", Auth: "provider CLI"}}
	for _, draft := range []string{"/plogin claude max", "/plogin max claude", "/plogin anthropic max"} {
		if got := SuggestPlanLogins(plans, draft, 8); len(got) != 1 {
			t.Errorf("draft %q matched %d plans, want 1", draft, len(got))
		}
	}
}

// "clmx" is not a literal substring of "anthropic Claude Max" — a fuzzy match
// finds the row anyway, the same tolerance every other picker now has.
func TestThePlanLoginPickerToleratesAScatteredQuery(t *testing.T) {
	plans := []PlanSpec{{Provider: "anthropic", Name: "Claude Max", Auth: "provider CLI"}}
	if got := SuggestPlanLogins(plans, "/plogin clmx", 8); len(got) != 1 {
		t.Fatalf("scattered plan-login query = %#v", got)
	}
}

// A row that is missing an Auth is not assumed unusable: PlanSpec is a
// presentation struct and a caller that fills only the two display fields must
// still get a menu.
func TestAPlanWithNoStatedAuthIsStillOffered(t *testing.T) {
	plans := []PlanSpec{{Provider: "anthropic", Name: "Claude Max"}}
	if got := SuggestPlanLogins(plans, "/plogin ", 8); len(got) != 1 {
		t.Errorf("offered %d plans, want the one given", len(got))
	}
}
