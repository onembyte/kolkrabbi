package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The failure mode of user-facing documentation is not incompleteness. It is
// fiction: a documented command that does not exist, or that stopped existing.
// Both have already happened here — the README's own first line told people to
// run `go build -o kolk .` against a repository whose root holds no main
// package, and an error message drafted one item ago recommended `kolk doctor`,
// which is queued and unbuilt.
//
// So the rule is asymmetric on purpose. Nothing here demands that every command
// be documented — `kolk help` is generated from the table and is the complete
// reference. What is forbidden is documenting one that is not real.

var readmeCommand = regexp.MustCompile("(?:`|^)kolk ([a-z][a-z-]+)")

func TestTheReadmeNeverDocumentsACommandThatDoesNotExist(t *testing.T) {
	source, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading the README: %v", err)
	}

	// Only code contexts: prose says "kolk asks" and "kolk contacts", which are
	// sentences, not invocations.
	var invocations []string
	inFence := false
	for _, line := range strings.Split(string(source), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			if m := readmeCommand.FindStringSubmatch("`" + strings.TrimSpace(line)); m != nil {
				invocations = append(invocations, m[1])
			}
			continue
		}
		for _, m := range readmeCommand.FindAllStringSubmatch(line, -1) {
			invocations = append(invocations, m[1])
		}
	}

	if len(invocations) == 0 {
		t.Fatal("no `kolk <command>` invocations found in the README — the extraction broke, not the docs")
	}
	for _, name := range invocations {
		if lookupCommand(name) == nil {
			t.Errorf("the README documents `kolk %s`, which is not in the command table", name)
		}
	}
}

// The welcome line names three slash commands. If one is ever renamed, the
// first thing a new user is told becomes wrong, and it is the one line nobody
// re-reads.
func TestTheWelcomeOnlyNamesSlashCommandsThatExist(t *testing.T) {
	welcome := tuiWelcome(0)
	mentioned := regexp.MustCompile(`/[a-z]+`).FindAllString(welcome, -1)
	if len(mentioned) == 0 {
		t.Fatal("the welcome names no slash commands — the extraction broke, not the welcome")
	}
	for _, name := range mentioned {
		if !slashCommandExists(name) {
			t.Errorf("the welcome tells a new user about %s, which is not a slash command", name)
		}
	}
}

func slashCommandExists(name string) bool {
	for _, c := range slashCommandTable {
		if "/"+c.name == name {
			return true
		}
	}
	return false
}
