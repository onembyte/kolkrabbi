package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/tools"
)

// A subagent has no terminal. Prompting from one is either a deadlock or a
// prompt the user reads as coming from the main session, and answers about the
// wrong work.
func TestASubagentNeverPrompts(t *testing.T) {
	decider := &guardDecider{allow: true}
	var out strings.Builder
	agent := &Agent{Options: Options{
		Out: &out, Permission: PermissionAsk, Decider: decider, Root: "/p",
	}}

	allowed := agent.subagentGuard(context.Background(), agent.Out)(tools.Request{
		Tool: "write_file", Path: "/p/main.go", Display: "main.go",
	})

	if decider.asked != 0 {
		t.Fatalf("a subagent asked the user %d times", decider.asked)
	}
	if allowed {
		t.Fatal("an action that would need asking must be refused in a subagent")
	}
}

func TestASubagentRefusalExplainsHowToAllowIt(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionAsk, Root: "/p"}}

	agent.subagentGuard(context.Background(), agent.Out)(tools.Request{
		Tool: "bash", Command: "go test ./...",
	})

	got := out.String()
	// Widening what subagents may do is done by choosing a tier, which is
	// reviewable, not by answering a prompt nobody saw.
	if !strings.Contains(got, "subagent") || !strings.Contains(got, "auto-approve") {
		t.Fatalf("refusal = %q, want it to name the way to allow this", got)
	}
}

func TestASubagentStillDoesWhatTheTierAllows(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionAutoApprove, Root: "/p"}}

	// auto-approve lets edits inside the project through, for a subagent as
	// much as for the main session.
	if !agent.subagentGuard(context.Background(), agent.Out)(tools.Request{
		Tool: "write_file", Path: "/p/main.go", Display: "main.go",
	}) {
		t.Fatal("a subagent was refused something its tier allows")
	}
	// A command still needs asking under auto-approve, so a subagent cannot.
	if agent.subagentGuard(context.Background(), agent.Out)(tools.Request{Tool: "bash", Command: "go test ./..."}) {
		t.Fatal("a subagent ran a command its tier would have asked about")
	}
}

func TestTheFloorStillAppliesInASubagent(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionFullAuto, Root: "/p"}}

	if agent.subagentGuard(context.Background(), agent.Out)(tools.Request{
		Tool: "read_file", Path: "/home/me/.ssh/id_ed25519", Display: "…", Outside: true,
	}) {
		t.Fatal("the floor did not hold inside a subagent")
	}
}

func TestAFullAutoSubagentWorksUnattended(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionFullAuto, Root: "/p"}}

	// This is the point of full-auto: orchestration that does not stall on a
	// question nobody is there to answer.
	for _, request := range []tools.Request{
		{Tool: "write_file", Path: "/p/main.go", Display: "main.go"},
		{Tool: "bash", Command: "go build ./..."},
	} {
		if !agent.subagentGuard(context.Background(), agent.Out)(request) {
			t.Fatalf("full-auto subagent was blocked on %s", request.Tool)
		}
	}
}
