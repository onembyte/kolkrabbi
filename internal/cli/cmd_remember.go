package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// projectMemoryCandidates mirrors the engine's lookup order so /remember writes
// where the agent will actually read.
var projectMemoryCandidates = []string{"KOLKRABBI.md", "AGENTS.md"}

// runRemember appends one line of standing guidance.
//
// Only the user may write memory. An agent that can edit its own standing
// instructions unprompted is an agent whose behaviour cannot be explained by
// reading the repository, so this is a command and never a tool.
func (a *app) runRemember(args []string) error {
	toProject := false
	kept := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--project" {
			toProject = true
			continue
		}
		kept = append(kept, arg)
	}
	note := strings.TrimSpace(strings.Join(kept, " "))
	if note == "" {
		return usagef("usage: /remember [--project] <what to remember>")
	}

	path, err := a.memoryTarget(toProject)
	if err != nil {
		return err
	}
	if err := appendMemoryLine(path, note); err != nil {
		return err
	}
	scope := "your notes"
	if toProject {
		scope = "this project's notes"
	}
	// Say what was written and where. A note the user cannot find is a note
	// they cannot correct.
	fmt.Fprintf(a.stdout, "added to %s (%s):\n  %s\n", scope, path, note)
	return nil
}

func (a *app) memoryTarget(toProject bool) (string, error) {
	if !toProject {
		d, err := a.resolve()
		if err != nil {
			return "", err
		}
		return d.MemoryFile(), nil
	}
	for _, candidate := range projectMemoryCandidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// None exists yet: create the first name the engine looks for, so the note
	// is read back rather than sitting in a file nothing loads.
	return projectMemoryCandidates[0], nil
}

func appendMemoryLine(path, note string) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := fmt.Fprintf(file, "- %s\n", note); err != nil {
		return fmt.Errorf("writing to %s: %w", path, err)
	}
	return file.Close()
}
