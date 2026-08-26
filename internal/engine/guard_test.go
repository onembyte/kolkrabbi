package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/tools"
)

func TestFullAutoLogsEveryStepOutsideTheProject(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionFullAuto, Root: "/home/me/project"}}

	allowed := agent.guard(context.Background())(tools.Request{
		Tool: "read_file", Path: "/home/me/notes/spec.md", Display: "/home/me/notes/spec.md",
		Outside: true, Summary: "check the API spec the task refers to",
	})

	if !allowed {
		t.Fatal("full-auto must proceed")
	}
	got := out.String()
	// In full-auto this line is the only account anyone will have of it.
	for _, want := range []string{"outside the project", "read_file", "/home/me/notes/spec.md", "check the API spec"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log = %q, want %q", got, want)
		}
	}
}

func TestTheLogSaysSoWhenTheModelGaveNoReason(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionFullAuto, Root: "/home/me/project"}}

	agent.guard(context.Background())(tools.Request{
		Tool: "write_file", Path: "/tmp/scratch", Display: "/tmp/scratch", Outside: true,
	})

	if !strings.Contains(out.String(), "no description given") {
		t.Fatalf("log = %q, want the missing explanation stated rather than an empty dash", out.String())
	}
}

func TestARefusalExplainsItself(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionFullAuto, Root: "/home/me/project"}}

	allowed := agent.guard(context.Background())(tools.Request{
		Tool: "read_file", Path: "/home/me/.ssh/id_ed25519", Display: "/home/me/.ssh/id_ed25519", Outside: true,
	})

	if allowed {
		t.Fatal("the floor did not hold in full-auto")
	}
	got := out.String()
	if !strings.Contains(got, "refused") || !strings.Contains(got, "credentials") {
		t.Fatalf("refusal = %q, want it to say what and why", got)
	}
}

func TestInsideTheProjectFullAutoIsSilent(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionFullAuto, Root: "/home/me/project"}}

	agent.guard(context.Background())(tools.Request{
		Tool: "write_file", Path: "/home/me/project/main.go", Display: "main.go",
	})

	// Narrating ordinary work would bury the lines that matter.
	if out.String() != "" {
		t.Fatalf("log = %q, want silence for ordinary work", out.String())
	}
}

type guardDecider struct {
	asked  int
	action string
	allow  bool
}

func (d *guardDecider) Confirm(_ context.Context, c Confirmation) bool {
	d.asked++
	d.action = c.Action
	return d.allow
}

func TestAskTierPromptsWithTheActionAndPath(t *testing.T) {
	decider := &guardDecider{allow: true}
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Permission: PermissionAsk, Decider: decider, Root: "/p"}}

	agent.guard(context.Background())(tools.Request{Tool: "write_file", Path: "/p/main.go", Display: "main.go"})

	if decider.asked != 1 {
		t.Fatalf("asked %d times, want once", decider.asked)
	}
	if !strings.Contains(decider.action, "main.go") {
		t.Fatalf("prompt said %q, want the file named", decider.action)
	}
}

func TestADeclinedActionDoesNotProceed(t *testing.T) {
	decider := &guardDecider{allow: false}
	agent := &Agent{Options: Options{Out: &strings.Builder{}, Permission: PermissionAsk, Decider: decider, Root: "/p"}}

	if agent.guard(context.Background())(tools.Request{Tool: "bash", Command: "go test ./..."}) {
		t.Fatal("a declined command was allowed to run")
	}
}
