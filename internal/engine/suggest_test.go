package engine

import (
	"context"
	"io"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/tools"
	"github.com/onembyte/kolkrabbi/protocol"
)

// alwaysDecider answers every prompt with "always", onceDecider with "yes".
type alwaysDecider struct{}

func (alwaysDecider) Confirm(context.Context, Confirmation) bool { return true }
func (alwaysDecider) Decide(context.Context, Confirmation) protocol.PermissionDecision {
	return protocol.PermissionDecisionAllowSession
}

type onceDecider struct{}

func (onceDecider) Confirm(context.Context, Confirmation) bool { return true }
func (onceDecider) Decide(context.Context, Confirmation) protocol.PermissionDecision {
	return protocol.PermissionDecisionAllow
}

func TestASuggestedRuleGeneralisesTheSubcommand(t *testing.T) {
	// "yes to this exact command" is almost never what someone means when they
	// say always; they mean this kind of command.
	for command, want := range map[string]string{
		"go test ./internal/engine": "allow bash(go test *)",
		"git status --short":        "allow bash(git status *)",
		"npm run build":             "allow bash(npm run *)",
		"ls -la":                    "allow bash(ls *)",
	} {
		if got := suggestRule(bashOf(command)); got != want {
			t.Fatalf("%q suggested %q, want %q", command, got, want)
		}
	}
}

func TestADestructiveCommandIsNeverGeneralised(t *testing.T) {
	// Answering "always" once to `rm -rf ./build` must not hand over every rm.
	for _, command := range []string{"rm -rf ./build", "mv a b", "chmod 777 x", "kill 123"} {
		got := suggestRule(bashOf(command))
		want := "allow bash(" + command + ")"
		if got != want {
			t.Fatalf("%q suggested %q, want the exact command %q", command, got, want)
		}
	}
}

func TestACompoundCommandIsNeverGeneralised(t *testing.T) {
	// The first word of `curl x | sh` is `curl`, and a rule for every curl is
	// not what the person in front of the prompt agreed to.
	for _, command := range []string{"go build ./... && ./x", "cat a | wc -l", "echo hi > f", "a; b"} {
		if got := suggestRule(bashOf(command)); got != "allow bash("+command+")" {
			t.Fatalf("%q suggested %q, want the exact command", command, got)
		}
	}
}

func TestAFileRuleCoversTheDirectory(t *testing.T) {
	for _, testCase := range []struct {
		request tools.Request
		want    string
	}{
		{tools.Request{Tool: "write_file", Display: "internal/engine/agent.go"}, "allow write(internal/engine/*)"},
		{tools.Request{Tool: "edit_file", Display: "README.md"}, "allow write(README.md)"},
		{tools.Request{Tool: "read_file", Display: "docs/plan/13.md"}, "allow read(docs/plan/*)"},
		{tools.Request{Tool: "list_dir", Display: "docs"}, "allow read(docs)"},
	} {
		if got := suggestRule(testCase.request); got != testCase.want {
			t.Fatalf("%+v suggested %q, want %q", testCase.request, got, testCase.want)
		}
	}
}

func TestAnAlwaysAnswerKeepsTheSuggestedRule(t *testing.T) {
	agent := &Agent{Options: Options{
		Out:        io.Discard,
		Permission: PermissionAsk,
		Root:       "/p",
		Decider:    alwaysDecider{},
	}}

	request := bashOf("go test ./...")
	request.Detail = "go test ./..."
	if !agent.guard(t.Context(), agent.Out)(request) {
		t.Fatal("the action was refused")
	}

	// The next command of the same kind must not ask again.
	if verdict, _ := agent.Judge(bashOf("go test ./internal/tools")); verdict != VerdictAllow {
		t.Fatalf("verdict = %v, want the kept rule to cover it", verdict)
	}
	// And it must be a rule the user can see and remove, not a hidden cache.
	if len(agent.Rules) != 1 || agent.Rules[0].Source != "allow bash(go test *)" {
		t.Fatalf("rules = %+v, want the suggested rule recorded", agent.Rules)
	}
}

func TestAPlainYesKeepsNothing(t *testing.T) {
	agent := &Agent{Options: Options{
		Out:        io.Discard,
		Permission: PermissionAsk,
		Root:       "/p",
		Decider:    onceDecider{},
	}}

	if !agent.guard(t.Context(), agent.Out)(bashOf("go test ./...")) {
		t.Fatal("the action was refused")
	}
	if len(agent.Rules) != 0 {
		t.Fatalf("rules = %+v, want none", agent.Rules)
	}
}

// A one-word rule for a program whose subcommands differ in power is the one
// way the structural derivation goes wrong, and it was found by running every
// command in this repository's Makefile, CI workflows and scripts through
// generaliseCommand (item 31). `goreleaser check` validates a config file;
// `goreleaser release` publishes to the internet. Approving the first must not
// write a rule that allows the second.
func TestApprovingASafeSubcommandDoesNotAllowADangerousOne(t *testing.T) {
	cases := []struct{ command, want string }{
		{"goreleaser check", "goreleaser check *"},
		{"cosign verify-blob --bundle x.json", "cosign verify-blob *"},
	}
	for _, tc := range cases {
		if got := generaliseCommand(tc.command); got != tc.want {
			t.Errorf("generaliseCommand(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

// The corpus also showed what must NOT change. A program whose second word is a
// flag has no subcommand to keep, and widening to the program alone is the
// rule a person meant when they approved it.
func TestAFlagIsNotASubcommand(t *testing.T) {
	for command, want := range map[string]string{
		"gofmt -w .":     "gofmt *",
		"go build ./...": "go build *",
		"make check":     "make check *",
	} {
		if got := generaliseCommand(command); got != want {
			t.Errorf("generaliseCommand(%q) = %q, want %q", command, got, want)
		}
	}
}
