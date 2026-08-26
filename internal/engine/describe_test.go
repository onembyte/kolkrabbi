package engine

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func toolCall(name, args string) provider.ToolCall {
	return provider.ToolCall{Function: provider.FunctionCall{Name: name, Arguments: args}}
}

func TestAShortPathIsShownWhole(t *testing.T) {
	got := describeToolCall(toolCall("write_file", `{"path":"internal/engine/agent.go"}`))
	if got != "Writing file — internal/engine/agent.go" {
		t.Fatalf("got %q", got)
	}
}

func TestALongPathKeepsTheFilename(t *testing.T) {
	// The path that actually broke CI, from a macOS runner.
	long := "/private/var/folders/df/djsxfhc17x95674wsm_g8s980000gn/T/TestE2E_ToolLoopWithPersistenceAndRewind3553888001/001/hello.txt"
	got := describeToolCall(toolCall("write_file", `{"path":"`+long+`"}`))

	// The end of a path is what says which file this is. Truncating it away
	// leaves a person approving "somewhere under /private/var".
	if !strings.HasSuffix(got, "hello.txt") {
		t.Fatalf("got %q, want it to end with the filename", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("got %q, want the elision to be visible", got)
	}
	if len([]rune(got)) > 140 {
		t.Fatalf("got %d runes, too long for one line", len([]rune(got)))
	}
}

func TestALongCommandKeepsItsBeginning(t *testing.T) {
	command := "go test ./internal/engine -run TestSomething -count=1 -race -v " + strings.Repeat("-tag x ", 20)
	got := describeToolCall(toolCall("bash", `{"command":"`+command+`"}`))

	// A command reads left to right: what it is matters more than its last
	// flag, which is the opposite of a path.
	if !strings.Contains(got, "Running command — go test ./internal/engine") {
		t.Fatalf("got %q, want the command's beginning", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q, want it truncated at the end", got)
	}
}

func TestTheArgumentPayloadNeverReachesTheLine(t *testing.T) {
	got := describeToolCall(toolCall("write_file", `{"path":"a.txt","content":"OPENROUTER_API_KEY=whatever"}`))
	if strings.Contains(got, "OPENROUTER_API_KEY") {
		t.Fatalf("the payload reached the activity line: %q", got)
	}
}

func TestControlCharactersCannotDrawOnTheTerminal(t *testing.T) {
	escaped := "a" + string(rune(27)) + "[31mb.txt"
	got := describeToolCall(toolCall("read_file", `{"path":"`+escaped+`"}`))
	if strings.ContainsRune(got, rune(27)) {
		t.Fatalf("an escape survived: %q", got)
	}
}
