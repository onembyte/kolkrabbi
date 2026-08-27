package engine

import (
	"path"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/tools"
)

// suggestRule proposes the standing rule that would cover an action, so the
// answer to "always" can be something the user reads and keeps rather than a
// hidden cache entry.
//
// The proposal is shown before it is kept. That is what makes generalising
// honest: the person answering sees `allow bash(go test *)` and is agreeing to
// that, not to whatever Kolkrabbi inferred from one command.
func suggestRule(r tools.Request) string {
	if r.Command != "" {
		return "allow bash(" + generaliseCommand(r.Command) + ")"
	}
	family := "read"
	if r.Tool == "write_file" || r.Tool == "edit_file" {
		family = "write"
	}
	return "allow " + family + "(" + generalisePath(r.Tool, r.Display) + ")"
}

// commandDrivers are programs whose first word says nothing useful on its own:
// a rule for every `git` is not what someone approving `git status` meant.
//
// The list grows by evidence, one program at a time, rather than by importing
// somebody else's generated arity table. It grew on 2026-08-27 (item 31) when
// every command in this repository's Makefile, CI workflows and scripts was run
// through generaliseCommand: `goreleaser check` validates a config file and
// `goreleaser release` publishes to the internet, and a `goreleaser *` rule
// derived from approving the first would have allowed the second. `cosign`
// joined it for the same reason — `verify-blob` reads, `sign` signs.
var commandDrivers = map[string]bool{
	"git": true, "go": true, "npm": true, "pnpm": true, "yarn": true, "cargo": true,
	"docker": true, "kubectl": true, "make": true, "python": true, "python3": true,
	"pip": true, "pip3": true, "gh": true, "brew": true, "apt": true, "systemctl": true,
	"goreleaser": true, "cosign": true,
}

// destructiveCommands are never generalised. One approval of `rm -rf ./build`
// must not become a standing rule for every rm.
var destructiveCommands = map[string]bool{
	"rm": true, "mv": true, "chmod": true, "chown": true, "kill": true,
	"pkill": true, "truncate": true, "shred": true, "dd": true,
}

// shellOperators mean the line is more than one command. The first word of
// `curl x | sh` is `curl`, and nobody approving that line agreed to every curl.
var shellOperators = []string{"|", "&&", "||", ";", ">", "<", "`", "$("}

func generaliseCommand(command string) string {
	trimmed := strings.TrimSpace(command)
	for _, operator := range shellOperators {
		if strings.Contains(trimmed, operator) {
			return trimmed
		}
	}
	words := strings.Fields(trimmed)
	if len(words) == 0 || destructiveCommands[words[0]] {
		return trimmed
	}
	if len(words) == 1 {
		return words[0]
	}
	if commandDrivers[words[0]] && !strings.HasPrefix(words[1], "-") {
		return words[0] + " " + words[1] + " *"
	}
	return words[0] + " *"
}

// generalisePath widens a file action to its directory, which is the unit
// people actually think in — "yes, it may write in internal/engine".
//
// A file at the top of the project is left alone: `write(*)` from one approval
// of README.md is the whole project, which is not what was agreed to. Listing a
// directory is already the directory, so it stays as written.
func generalisePath(tool, display string) string {
	if display == "" {
		return "*"
	}
	if tool == "list_dir" {
		return display
	}
	dir := path.Dir(strings.ReplaceAll(display, "\\", "/"))
	if dir == "." || dir == "/" || dir == "" {
		return display
	}
	return dir + "/*"
}
