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
			!strings.Contains(strings.ToLower(model.Name), filter) {
			continue
		}
		suggestions = append(suggestions, CommandSpec{
			Name: model.ID, Usage: prefix + model.ID, Summary: model.Name,
			Complete: prefix + model.ID,
		})
		if len(suggestions) == limit {
			break
		}
	}
	return suggestions
}
