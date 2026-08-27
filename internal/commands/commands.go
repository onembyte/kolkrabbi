// Package commands loads markdown files that act as slash commands.
//
// A file is a command: `.kolk/commands/review.md` is `/review`. The body is the
// prompt, sent as a **user turn** rather than a system prompt, because a command
// is a thing the user said — and it carries no permissions of its own. What the
// model then does is judged exactly as if the user had typed it, which is what
// keeps a command from being a way around the tier.
package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxCommandBytes caps one command, matching the project-memory cap it sits
// beside. A prompt that does not fit is worse than one that is cut: it costs
// the window before the work starts.
const maxCommandBytes = 16 * 1024

// argumentsPlaceholder is replaced by whatever followed the command.
const argumentsPlaceholder = "$ARGUMENTS"

// builtIns are names a file may not take.
//
// A command file that could shadow `/undo` would make the one command a person
// reaches for when something has gone wrong mean whatever a repository says it
// means. Cloning a repository must not redefine the tools you use to inspect
// it — the same instinct G16.3 applies to hooks.
var builtIns = map[string]bool{
	"help": true, "exit": true, "quit": true, "undo": true, "rewind": true,
	"compact": true, "mode": true, "effort": true, "model": true, "key": true,
	"permissions": true, "ask": true, "auto-approve": true, "full-auto": true,
	"plan": true, "remember": true, "rate": true, "changes": true, "diff": true,
	"new": true, "clear": true, "commit": true, "pr": true, "doctor": true,
	"saga": true, "plans": true, "plogin": true, "pmodels": true, "localia": true,
}

// Command is one markdown file.
type Command struct {
	Name        string
	Description string // from front matter; shown by /help
	Body        string
	Source      string // the file it came from, so a message can name it
}

// Prompt renders the command with the arguments the user typed.
//
// `$ARGUMENTS` is replaced wherever it appears — a command may name its
// argument twice — and when the body does not mention it the arguments are
// appended instead, so a command still composes with what was typed after it.
func (c Command) Prompt(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if strings.Contains(c.Body, argumentsPlaceholder) {
		return strings.ReplaceAll(c.Body, argumentsPlaceholder, arguments)
	}
	if arguments == "" {
		return c.Body
	}
	return c.Body + "\n\n" + arguments
}

// Load reads every command available here, project first.
//
// The order is the precedence: `.kolk/commands` in the project, then the user's
// own, then `.claude/commands` — Claude Code's directory is **read, not
// converted**, because someone who already wrote them should not have to move
// them to try this, and the formats are close enough that divergence would be
// ours to explain.
func Load(projectRoot, userConfigDir string) []Command {
	byName := map[string]Command{}
	// Later directories must not overwrite earlier ones: the first to define a
	// name wins, which is what "project over user" means.
	for _, dir := range []string{
		filepath.Join(projectRoot, ".kolk", "commands"),
		filepath.Join(userConfigDir, "commands"),
		filepath.Join(projectRoot, ".claude", "commands"),
	} {
		for _, command := range readDir(dir) {
			if _, taken := byName[command.Name]; !taken {
				byName[command.Name] = command
			}
		}
	}

	found := make([]Command, 0, len(byName))
	for _, command := range byName {
		found = append(found, command)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return found
}

func readDir(dir string) []Command {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []Command
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		command, ok := read(filepath.Join(dir, name))
		if ok {
			found = append(found, command)
		}
	}
	return found
}

func read(path string) (Command, bool) {
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	if name == "" || builtIns[name] {
		// A built-in name is refused rather than renamed: silently loading it
		// as something else would leave a person typing a command that is not
		// the one they wrote.
		return Command{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return Command{}, false
	}

	description, body := splitFrontMatter(string(raw))
	return Command{
		Name:        name,
		Description: description,
		Body:        capAtLineBoundary(strings.TrimSpace(body)),
		Source:      path,
	}, true
}

// splitFrontMatter reads the optional `---` block. Only `description` is
// honoured: whether a command may declare a mode or an effort is item 16's open
// question, left out of v1 deliberately, because that turns a command from
// "expands to a prompt" into a thing that reconfigures the session.
func splitFrontMatter(text string) (description, body string) {
	if !strings.HasPrefix(text, "---") {
		return "", text
	}
	rest := strings.TrimPrefix(text, "---")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", text
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		if key, value, found := strings.Cut(line, ":"); found && strings.TrimSpace(key) == "description" {
			description = strings.TrimSpace(value)
		}
	}
	return description, strings.TrimPrefix(rest[end:], "\n---")
}

// capAtLineBoundary trims to the cap without splitting a line, the way project
// memory already does: half a sentence of instruction is worse than none.
func capAtLineBoundary(body string) string {
	if len(body) <= maxCommandBytes {
		return body
	}
	cut := []byte(body[:maxCommandBytes])
	if boundary := bytes.LastIndexByte(cut, '\n'); boundary > 0 {
		cut = cut[:boundary]
	}
	return string(cut)
}
