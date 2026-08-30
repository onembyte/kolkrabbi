package tui

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
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

// H1: every suggestion surface now tolerates a scattered query, not just a
// contiguous prefix. "cfg" is not a prefix of "config" — a fuzzy match finds
// it anyway, the way Claude Code's and Codex's own slash menus do.
func TestSlashSuggestionsToleratesAScatteredQuery(t *testing.T) {
	catalog := []CommandSpec{{Name: "config", Usage: "/config"}, {Name: "help", Usage: "/help"}}
	got := SuggestCommands(catalog, "/cfg", nil, 5)
	if names := suggestionNames(got); !reflect.DeepEqual(names, []string{"config"}) {
		t.Fatalf("scattered slash query = %q, want just config", names)
	}
}

func TestModelSuggestionsFilterLiveByArgument(t *testing.T) {
	models := []ModelSpec{
		{ID: "moonshotai/kimi-k2", Name: "Kimi K2"},
		{ID: "google/gemini-2.5-flash", Name: "Gemini Flash"},
		{ID: "anthropic/claude-sonnet", Name: "Claude Sonnet"},
	}

	got := SuggestModels(models, "/model kim", 8)
	if len(got) != 1 || got[0].Usage != "/model moonshotai/kimi-k2" {
		t.Fatalf("model suggestions = %#v", got)
	}
	if got[0].Complete != "/model moonshotai/kimi-k2" {
		t.Fatalf("model completion = %q", got[0].Complete)
	}
}

// "cld" is not a literal substring of "claude" — a fuzzy match finds the row
// anyway, the way it would in Claude Code's or Codex's own model picker.
func TestModelSuggestionsToleratesAScatteredQuery(t *testing.T) {
	models := []ModelSpec{{ID: "anthropic/claude-opus", Name: "Claude Opus"}}
	got := SuggestModels(models, "/model cld", 8)
	if len(got) != 1 || got[0].Name != "anthropic/claude-opus" {
		t.Fatalf("scattered model query = %#v", got)
	}
}

// Ranking, not just filtering, is the point of moving to a score: the model a
// person meant should sit on top even when a worse match for the same
// characters was listed first.
func TestModelSuggestionsRankTheBestMatchFirst(t *testing.T) {
	models := []ModelSpec{
		{ID: "local-cloud", Name: "local-cloud"},
		{ID: "claude", Name: "claude"},
	}
	got := SuggestModels(models, "/model cld", 8)
	if len(got) != 2 || got[0].Name != "claude" {
		t.Fatalf("ranked model suggestions = %#v, want claude first", got)
	}
}

func TestControllerCompletesLiveModelSuggestion(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.SetCommands([]CommandSpec{{Name: "model", Usage: "/model [id]"}}, 5)
	controller.SetModels([]ModelSpec{{ID: "moonshotai/kimi-k2", Name: "Kimi K2"}})
	controller.HandleKey(Key{Kind: KeyText, Text: "/model kim"})

	if got := controller.Snapshot().Suggestions; len(got) != 1 ||
		got[0].Usage != "/model moonshotai/kimi-k2" {
		t.Fatalf("live model suggestions = %#v", got)
	}
	if effect := controller.HandleKey(Key{Kind: KeyTab}); effect.Submit != "" ||
		controller.Snapshot().Draft != "/model moonshotai/kimi-k2" {
		t.Fatalf("model tab completion = %#v, draft %q", effect, controller.Snapshot().Draft)
	}
}

func suggestionNames(suggestions []CommandSpec) []string {
	names := make([]string, len(suggestions))
	for i, suggestion := range suggestions {
		names[i] = suggestion.Name
	}
	return names
}

// The catalog is bigger than the window, so the window has to move — otherwise
// every command past the eighth is unreachable, which is what it was.
func TestSuggestionListScrollsThroughTheWholeCatalog(t *testing.T) {
	catalog := make([]CommandSpec, 0, 30)
	for i := range 30 {
		catalog = append(catalog, CommandSpec{Name: fmt.Sprintf("cmd%02d", i), Usage: fmt.Sprintf("/cmd%02d", i)})
	}
	c := NewController(Status{Mode: "code", Lifecycle: "ready"}, 0)
	c.SetCommands(catalog, 8)
	c.HandleKey(Key{Kind: KeyText, Text: "/cmd"})

	if len(c.suggestions) != 30 {
		t.Fatalf("offered %d of 30 commands; the catalog must not be truncated", len(c.suggestions))
	}

	// The window applies to the first frame too: opening the menu must not
	// render thirty rows and then snap to eight when a key is pressed.
	opened := c.View(100, 40)
	if strings.Contains(opened, "/cmd08") {
		t.Fatalf("the opening frame ignored the window:\n%s", opened)
	}
	if !strings.Contains(opened, "/cmd07") || !strings.Contains(opened, "↓") {
		t.Fatalf("the opening frame is missing rows or the arrow:\n%s", opened)
	}
	// Nothing is above the first row, so nothing points up.
	if strings.Contains(opened, "↑") {
		t.Fatalf("an up arrow at the top of the list points at nothing:\n%s", opened)
	}

	// Walk past the window's edge; the last command must become visible.
	for range 12 {
		c.HandleKey(Key{Kind: KeyDown})
	}
	view := c.View(100, 40)
	if !strings.Contains(view, "/cmd11") {
		t.Fatalf("selection scrolled out of view:\n%s", view)
	}
	if strings.Contains(view, "/cmd00") {
		t.Fatalf("window did not scroll — the first row is still drawn:\n%s", view)
	}
	if !strings.Contains(view, "↓") {
		t.Fatalf("no indication that the list continues below:\n%s", view)
	}

	// PageDown jumps a window at a time and stops at the end rather than wrapping.
	for range 10 {
		c.HandleKey(Key{Kind: KeyPageDown})
	}
	if c.suggestionIndex != 29 {
		t.Fatalf("page-down landed on %d, want the last row 29", c.suggestionIndex)
	}
	view = c.View(100, 40)
	if !strings.Contains(view, "/cmd29") {
		t.Fatalf("last command never became visible:\n%s", view)
	}
	// At the end of the list the down arrow has nothing left to point at, and
	// the up arrow now does: the pair is symmetric.
	if strings.Contains(view, "↓") {
		t.Fatalf("the arrow outlived the rows it pointed to:\n%s", view)
	}
	if !strings.Contains(view, "↑") {
		t.Fatalf("scrolled to the bottom, nothing marks the rows above:\n%s", view)
	}

	// And back to the top.
	for range 40 {
		c.HandleKey(Key{Kind: KeyPageUp})
	}
	if c.suggestionIndex != 0 {
		t.Fatalf("page-up landed on %d, want 0", c.suggestionIndex)
	}
}

// The picker used to show OpenRouter's catalog alone, so a Claude Max
// subscriber typing /model claude was offered the metered API rows and never
// the plan they already pay for. Cost is now the first thing each row says,
// and what bills nothing extra sorts first.
func TestModelPickerLabelsCostAndListsFreeThingsFirst(t *testing.T) {
	models := []ModelSpec{
		{ID: "anthropic/claude-opus-5", Name: "Claude Opus 5", Cost: CostMetered, Rank: ModelRank(CostMetered)},
		{ID: "claude-opus", Name: "Claude Max · via your claude login", Cost: CostSubscription, Rank: ModelRank(CostSubscription)},
		{ID: "qwen2.5-coder:14b", Name: "runs on this machine", Cost: CostLocal, Rank: ModelRank(CostLocal)},
		{ID: "z-ai/glm-5.2:free", Name: "GLM 5.2", Cost: CostFree, Rank: ModelRank(CostFree)},
	}
	sort.SliceStable(models, func(i, j int) bool { return models[i].Rank < models[j].Rank })

	got := SuggestModels(models, "/model ", 10)
	if len(got) != 4 {
		t.Fatalf("offered %d of 4 models", len(got))
	}
	wantOrder := []string{CostSubscription, CostFree, CostLocal, CostMetered}
	for index, want := range wantOrder {
		if !strings.HasPrefix(got[index].Summary, "["+want+"]") {
			t.Fatalf("row %d = %q, want the %s class", index, got[index].Summary, want)
		}
	}

	// Typing the class name finds it: "what can I use that is already paid for"
	// is a question the picker can now answer.
	subs := SuggestModels(models, "/model sub", 10)
	if len(subs) != 1 || subs[0].Name != "claude-opus" {
		t.Fatalf("filtering by cost class returned %#v", subs)
	}

	// And a subscription model is still reachable by name, ahead of the metered
	// row that shares the word.
	byName := SuggestModels(models, "/model claude", 10)
	if len(byName) < 2 || byName[0].Name != "claude-opus" {
		t.Fatalf("/model claude offered %#v; the subscription must come first", byName)
	}
}

func TestSuggestModelsUsesAnExplicitSelectionAlias(t *testing.T) {
	got := SuggestModels([]ModelSpec{{
		ID: "gpt-5.6-terra", Select: "gpt-plus-terra", Name: "ChatGPT Plus · via codex",
	}}, "/model gpt-plus", 8)
	if len(got) != 1 {
		t.Fatalf("suggestions = %#v, want one alias match", got)
	}
	if got[0].Name != "gpt-plus-terra" || got[0].Usage != "/model gpt-plus-terra" || got[0].Complete != "/model gpt-plus-terra" {
		t.Fatalf("suggestion = %#v, want the explicit selection alias", got[0])
	}
}

// The settings panel: type /config and filter the list live, instead of
// leaving the session to run `kolk config` and read it back.
func TestSettingsPickerFiltersLiveAndCompletesToSet(t *testing.T) {
	settings := []SettingSpec{
		{Key: "model", Value: "openrouter/free", Default: true, Summary: "the model a new session starts on"},
		{Key: "effort", Value: "medium", Default: true, Summary: "model tier and orchestration width"},
		{Key: "auto_restart_after_update", Value: "on", Summary: "restart into the new version after an update"},
	}

	all := SuggestSettings(settings, "/config ", 8)
	if len(all) != 3 {
		t.Fatalf("bare /config offered %d of 3 settings", len(all))
	}
	// The value in effect is on the row, and an inherited one says so.
	if !strings.Contains(all[0].Usage, "openrouter/free") || !strings.Contains(all[0].Usage, "(default)") {
		t.Fatalf("row does not show the value in effect: %q", all[0].Usage)
	}

	// Filtering matches the key, the summary and the value.
	for _, tc := range []struct{ draft, want string }{
		{"/config eff", "effort"},
		{"/config restart", "auto_restart_after_update"},
		{"/config orchestration", "effort"},
		{"/config openrouter/free", "model"},
	} {
		got := SuggestSettings(settings, tc.draft, 8)
		if len(got) != 1 || got[0].Name != tc.want {
			t.Fatalf("%q matched %#v, want just %s", tc.draft, got, tc.want)
		}
	}

	// Choosing one leaves the user typing the value, which is the only thing
	// they opened the list to do next.
	if got := all[1].Complete; got != "/config set effort " {
		t.Fatalf("completion = %q, want it ready for a value", got)
	}

	// Once they are typing that value the picker gets out of the way, rather
	// than re-offering the list against the words being typed.
	if got := SuggestSettings(settings, "/config set effort hi", 8); got != nil {
		t.Fatalf("picker fought the value being typed: %#v", got)
	}
}

// "rstrt" is not a literal substring of "restart" — a fuzzy match finds the
// row anyway, the same tolerance every other picker now has.
func TestSettingsSuggestionsToleratesAScatteredQuery(t *testing.T) {
	settings := []SettingSpec{{Key: "auto_restart_after_update", Summary: "restart into the new version"}}
	got := SuggestSettings(settings, "/config rstrt", 8)
	if len(got) != 1 {
		t.Fatalf("scattered settings query = %#v", got)
	}
}

// A subscription whose CLI is installed is not the same as one signed into:
// selecting the latter is refused with "needs the claude connector". The row
// has to carry that difference, and the command that closes it.
func TestModelPickerSeparatesSignedInFromNeedsLogin(t *testing.T) {
	models := []ModelSpec{
		{ID: "claude-opus", Cost: CostSubscriptionLogin, Rank: ModelRank(CostSubscriptionLogin),
			Name: `Claude Max · sign in first:  kolk plans login anthropic "Claude Max"`},
		{ID: "claude-sonnet", Cost: CostSubscription, Rank: ModelRank(CostSubscription),
			Name: "Claude Pro · via your claude login"},
		{ID: "anthropic/claude-opus-5", Name: "Claude Opus 5", Cost: CostMetered, Rank: ModelRank(CostMetered)},
	}
	sort.SliceStable(models, func(i, j int) bool { return models[i].Rank < models[j].Rank })

	got := SuggestModels(models, "/model claude", 10)
	if len(got) != 3 {
		t.Fatalf("offered %d of 3", len(got))
	}
	// Ready to use first, then the one a sign-in away, then anything metered.
	if !strings.HasPrefix(got[0].Summary, "["+CostSubscription+"]") {
		t.Fatalf("row 0 = %q, want the signed-in subscription", got[0].Summary)
	}
	if !strings.HasPrefix(got[1].Summary, "["+CostSubscriptionLogin+"]") {
		t.Fatalf("row 1 = %q, want the one needing a login", got[1].Summary)
	}
	if !strings.HasPrefix(got[2].Summary, "["+CostMetered+"]") {
		t.Fatalf("row 2 = %q, want the metered API row last", got[2].Summary)
	}
	// The row that cannot be selected yet says exactly what to run.
	if !strings.Contains(got[1].Summary, "kolk plans login anthropic") {
		t.Fatalf("a row that needs a login must carry the command: %q", got[1].Summary)
	}
}
