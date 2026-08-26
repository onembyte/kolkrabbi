package tui

import (
	"reflect"
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
