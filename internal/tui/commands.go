package tui

import "strings"

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
	CostFree         = "free"
	CostLocal        = "local"
	CostMetered      = "$"
)

// ModelRank maps a cost class to its position. Anything unlabelled sorts with
// the metered rows: an unknown price is not a reason to promote something.
func ModelRank(cost string) int {
	switch cost {
	case CostSubscription:
		return 0
	case CostFree:
		return 1
	case CostLocal:
		return 2
	default:
		return 3
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

// PlanSpec is the presentation subset used by the provider-login picker.
type PlanSpec struct {
	Provider string
	Name     string
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
	appendMatch := func(name string) {
		if len(suggestions) >= limit || seen[name] || !strings.HasPrefix(strings.ToLower(name), prefix) {
			return
		}
		command, ok := byName[name]
		if !ok {
			return
		}
		seen[name] = true
		suggestions = append(suggestions, command)
	}
	for _, name := range recent {
		appendMatch(name)
	}
	for _, command := range catalog {
		appendMatch(command.Name)
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
	suggestions := make([]CommandSpec, 0, min(limit, len(models)))
	for _, model := range models {
		if model.ID == "" {
			continue
		}
		if filter != "" &&
			!strings.Contains(strings.ToLower(model.ID), filter) &&
			!strings.Contains(strings.ToLower(model.Name), filter) &&
			!strings.Contains(strings.ToLower(model.Cost), filter) {
			continue
		}
		summary := model.Name
		if model.Cost != "" {
			summary = "[" + model.Cost + "]  " + summary
		}
		suggestions = append(suggestions, CommandSpec{
			Name: model.ID, Usage: prefix + model.ID, Summary: summary,
			Complete: prefix + model.ID,
		})
		if len(suggestions) == limit {
			break
		}
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
	filter := strings.ToLower(strings.TrimSpace(draft[len(prefix):]))
	suggestions := make([]CommandSpec, 0, min(limit, len(plans)))
	for _, plan := range plans {
		label := plan.Provider + " " + plan.Name
		if filter != "" && !strings.Contains(strings.ToLower(label), filter) {
			continue
		}
		suggestions = append(suggestions, CommandSpec{
			Name: label, Usage: prefix + label, Summary: "provider-owned login",
			Complete: prefix + label,
		})
		if len(suggestions) == limit {
			break
		}
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
	suggestions := make([]CommandSpec, 0, min(limit, len(settings)))
	for _, setting := range settings {
		if setting.Key == "" {
			continue
		}
		if filter != "" &&
			!strings.Contains(strings.ToLower(setting.Key), filter) &&
			!strings.Contains(strings.ToLower(setting.Summary), filter) &&
			!strings.Contains(strings.ToLower(setting.Value), filter) {
			continue
		}
		value := setting.Value
		if setting.Default {
			value += " (default)"
		}
		suggestions = append(suggestions, CommandSpec{
			Name:     setting.Key,
			Usage:    setting.Key + "  " + value,
			Summary:  setting.Summary,
			Complete: prefix + "set " + setting.Key + " ",
		})
		if len(suggestions) == limit {
			break
		}
	}
	return suggestions
}
