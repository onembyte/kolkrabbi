package tui

import (
	"sort"
	"strings"
)

// CommandSpec is the presentation subset of one slash command. Dispatch stays
// in the CLI; the TUI only needs the canonical name, usage, and summary.
type CommandSpec struct {
	Name     string
	Usage    string
	Summary  string
	Complete string
}

// ModelSpec is the presentation subset of one provider model.
type ModelSpec struct {
	ID   string
	Name string
	// Cost is what picking this model does to the user's money, in one word:
	// sub (already paid for), free, local (their own hardware), or $ (metered).
	// The picker previously showed OpenRouter's catalog alone, so a Claude Max
	// subscriber typing /model claude was offered the metered API rows and not
	// the plan they were already paying for.
	Cost string
	// Rank orders the classes: what costs nothing extra comes first.
	Rank int
}

// Model cost classes, in the order the picker lists them.
const (
	CostSubscription = "sub"
	// CostSubscriptionLogin is a plan whose CLI is installed but not signed
	// into. It is still free to use once the sign-in happens, so it sorts with
	// the subscriptions rather than being hidden — the row carries the command
	// that makes it usable.
	CostSubscriptionLogin = "sub·login"
	CostFree              = "free"
	CostLocal             = "local"
	CostMetered           = "$"
)

// ModelRank maps a cost class to its position. Anything unlabelled sorts with
// the metered rows: an unknown price is not a reason to promote something.
func ModelRank(cost string) int {
	switch cost {
	case CostSubscription:
		return 0
	case CostSubscriptionLogin:
		// After the ones ready to use, before anything that bills.
		return 1
	case CostFree:
		return 2
	case CostLocal:
		return 3
	default:
		return 4
	}
}

// SettingSpec is one row of the settings picker: the key, the value in
// effect, and what it does.
type SettingSpec struct {
	Key     string
	Value   string
	Summary string
	Default bool
}

// PlanSpec is the presentation subset used by the provider-login picker. Auth
// is carried so the picker can offer only what a login can actually act on:
// an API-key plan is signed into with a key, not with a provider CLI.
type PlanSpec struct {
	Provider string
	Name     string
	Auth     string
}

// scoredSpec pairs a row with how well it matched, so a whole candidate set
// can be scored before any of it is truncated to a limit — the row that fits
// best is not necessarily the first one a catalog scan happens to reach.
type scoredSpec struct {
	spec  CommandSpec
	score int
}

// rankByScore orders matches best first. The sort is stable, so ties keep the
// order they were given in — which matters most for an empty query, where
// every score ties at zero and this leaves the caller's own order (cost rank,
// catalog order, whatever it was) untouched rather than reshuffled.
func rankByScore(matches []scoredSpec) []CommandSpec {
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	specs := make([]CommandSpec, len(matches))
	for index, match := range matches {
		specs[index] = match.spec
	}
	return specs
}

// CommandHistory retains bounded unique names in most-recent-first order.
type CommandHistory struct {
	limit  int
	recent []string
}

// NewCommandHistory returns an empty process-local command history.
func NewCommandHistory(limit int) *CommandHistory {
	if limit <= 0 {
		limit = 8
	}
	return &CommandHistory{limit: limit}
}

// Record remembers the command name from one submitted slash line.
func (h *CommandHistory) Record(line string) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return
	}
	name := strings.TrimPrefix(fields[0], "/")
	if name == "" {
		return
	}
	next := []string{name}
	for _, existing := range h.recent {
		if existing != name {
			next = append(next, existing)
		}
		if len(next) == h.limit {
			break
		}
	}
	h.recent = next
}

// Recent returns a defensive newest-first copy.
func (h *CommandHistory) Recent() []string {
	return append([]string(nil), h.recent...)
}

// SuggestCommands shows command names only while the first slash word is being
// typed. Recent matches lead; the canonical catalog supplies the remainder.
func SuggestCommands(catalog []CommandSpec, draft string, recent []string, limit int) []CommandSpec {
	if !strings.HasPrefix(draft, "/") || strings.ContainsAny(draft, " \t\r\n") {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	prefix := strings.ToLower(strings.TrimPrefix(draft, "/"))
	byName := make(map[string]CommandSpec, len(catalog))
	for _, command := range catalog {
		byName[command.Name] = command
	}
	seen := make(map[string]bool, len(catalog))
	suggestions := make([]CommandSpec, 0, min(limit, len(catalog)))
	// Recent commands lead, in the order they were used. That recency signal
	// is worth more than how well the letters line up, so this bucket is not
	// reordered by score the way the rest of the catalog is below.
	for _, name := range recent {
		if len(suggestions) >= limit || seen[name] || !fuzzyMatches(name, prefix) {
			continue
		}
		command, ok := byName[name]
		if !ok {
			continue
		}
		seen[name] = true
		suggestions = append(suggestions, command)
	}
	// The remainder is ranked by match quality, not catalog declaration order,
	// so scanning the whole catalog finds the best name even when it is not
	// the first one that happens to match.
	remaining := make([]scoredSpec, 0, len(catalog))
	for _, command := range catalog {
		if seen[command.Name] {
			continue
		}
		score, ok := fuzzyScore(command.Name, prefix)
		if !ok {
			continue
		}
		remaining = append(remaining, scoredSpec{spec: command, score: score})
	}
	for _, command := range rankByScore(remaining) {
		if len(suggestions) >= limit {
			break
		}
		suggestions = append(suggestions, command)
	}
	if len(suggestions) == 0 {
		return nil
	}
	return suggestions
}

// SuggestModels filters model choices while the /model argument is being
// typed. Results use command-shaped presentation so the existing suggestion
// menu and keyboard navigation remain unchanged.
func SuggestModels(models []ModelSpec, draft string, limit int) []CommandSpec {
	const prefix = "/model "
	if !strings.HasPrefix(strings.ToLower(draft), prefix) {
		return nil
	}

	if limit <= 0 {
		limit = 8
	}
	filter := strings.ToLower(strings.TrimSpace(draft[len(prefix):]))
	matches := make([]scoredSpec, 0, len(models))
	for _, model := range models {
		if model.ID == "" {
			continue
		}
		score, ok := fuzzyScoreFields([]string{model.ID, model.Name, model.Cost}, filter)
		if !ok {
			continue
		}
		summary := model.Name
		if model.Cost != "" {
			summary = "[" + model.Cost + "]  " + summary
		}
		matches = append(matches, scoredSpec{score: score, spec: CommandSpec{
			Name: model.ID, Usage: prefix + model.ID, Summary: summary,
			Complete: prefix + model.ID,
		}})
	}
	suggestions := rankByScore(matches)
	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions
}

// SuggestPlanLogins filters provider plans while /plogin is being typed.
func SuggestPlanLogins(plans []PlanSpec, draft string, limit int) []CommandSpec {
	const prefix = "/plogin "
	if !strings.HasPrefix(strings.ToLower(draft), prefix) {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	filter := strings.TrimSpace(draft[len(prefix):])
	matches := make([]scoredSpec, 0, len(plans))
	for _, plan := range plans {
		// A plan signed into with an API key cannot be signed into here: the
		// command refuses it outright. Offering it would be a menu entry whose
		// only outcome is an error message.
		if plan.Auth != "" && plan.Auth != "provider CLI" {
			continue
		}
		label := plan.Provider + " " + plan.Name
		score, ok := fuzzyScoreFields([]string{plan.Provider, plan.Name}, filter)
		if !ok {
			continue
		}
		matches = append(matches, scoredSpec{score: score, spec: CommandSpec{
			Name: label, Usage: prefix + label, Summary: "provider-owned login",
			Complete: prefix + label,
		}})
	}
	suggestions := rankByScore(matches)
	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions
}

// SuggestSettings filters the settings list as `/config ` is typed, so the
// question "what is my effort set to" is answered by typing it rather than by
// leaving the session to run `kolk config`.
//
// Selecting a row completes to `/config set <key> `, which is the only thing a
// person opens this list to do next.
func SuggestSettings(settings []SettingSpec, draft string, limit int) []CommandSpec {
	const prefix = "/config "
	lower := strings.ToLower(draft)
	if !strings.HasPrefix(lower, prefix) {
		return nil
	}
	// `/config set …` is already past the picker: the user has chosen a key and
	// is typing its value, and re-offering the list would fight the typing.
	rest := strings.TrimSpace(draft[len(prefix):])
	if strings.HasPrefix(strings.ToLower(rest), "set ") || strings.HasPrefix(strings.ToLower(rest), "get ") {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	filter := strings.ToLower(rest)
	matches := make([]scoredSpec, 0, len(settings))
	for _, setting := range settings {
		if setting.Key == "" {
			continue
		}
		score, ok := fuzzyScoreFields([]string{setting.Key, setting.Summary, setting.Value}, filter)
		if !ok {
			continue
		}
		value := setting.Value
		if setting.Default {
			value += " (default)"
		}
		matches = append(matches, scoredSpec{score: score, spec: CommandSpec{
			Name:     setting.Key,
			Usage:    setting.Key + "  " + value,
			Summary:  setting.Summary,
			Complete: prefix + "set " + setting.Key + " ",
		}})
	}
	suggestions := rankByScore(matches)
	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions
}
