package engine

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/tools"
)

func mustRules(t *testing.T, lines ...string) Rules {
	t.Helper()
	rules, err := ParseRules(lines)
	if err != nil {
		t.Fatalf("parsing %v: %v", lines, err)
	}
	return rules
}

func TestARuleCanAllowWhatTheTierWouldAskAbout(t *testing.T) {
	rules := mustRules(t, "allow bash(git *)")

	// The point of a rule is to answer a question once instead of every time.
	verdict, _ := PermissionAsk.judgeWith(rules, bashOf("git status --short"))
	if verdict != VerdictAllow {
		t.Fatalf("verdict = %v, want the rule to allow it", verdict)
	}
	// Anything the rule does not cover keeps the tier's answer.
	if verdict, _ := PermissionAsk.judgeWith(rules, bashOf("rm -rf ./build")); verdict != VerdictAsk {
		t.Fatalf("verdict = %v, want the tier's answer for an uncovered command", verdict)
	}
}

func TestARuleCanDenyWhatTheTierWouldAllow(t *testing.T) {
	rules := mustRules(t, "deny write(*/migrations/*)")

	verdict, reason := PermissionFullAuto.judgeWith(rules, tools.Request{
		Tool: "write_file", Path: "/p/db/migrations/001.sql", Display: "db/migrations/001.sql",
	})
	if verdict != VerdictDeny {
		t.Fatalf("verdict = %v, want the rule to deny it", verdict)
	}
	if !strings.Contains(reason, "rule") {
		t.Fatalf("reason = %q, want it to say a rule decided", reason)
	}
}

func TestTheLastMatchingRuleWins(t *testing.T) {
	// Reading top to bottom, later lines refine earlier ones, which is how
	// every allow/deny list people already know behaves.
	rules := mustRules(t,
		"allow bash(*)",
		"deny bash(git push *)",
		"allow bash(git push origin feature/*)",
	)

	for command, want := range map[string]Verdict{
		"go test ./...":                  VerdictAllow,
		"git push origin main":           VerdictDeny,
		"git push origin feature/parser": VerdictAllow,
	} {
		if verdict, _ := PermissionAsk.judgeWith(rules, bashOf(command)); verdict != want {
			t.Fatalf("%q = %v, want %v", command, verdict, want)
		}
	}
}

func TestNoRuleCanBreachTheFloor(t *testing.T) {
	// A rule is a preference. The floor is not.
	rules := mustRules(t, "allow read(*)", "allow bash(*)")

	for _, request := range []tools.Request{
		{Tool: "read_file", Path: "/home/me/.ssh/id_ed25519", Display: "…"},
		bashOf("sudo rm -rf /var"),
	} {
		if verdict, _ := PermissionFullAuto.judgeWith(rules, request); verdict != VerdictDeny {
			t.Fatalf("%+v got %v, want the floor to hold", request, verdict)
		}
	}
}

func TestRulesCoverToolFamiliesNotJustExactNames(t *testing.T) {
	rules := mustRules(t, "deny write(*)", "allow read(*)")

	for _, tool := range []string{"write_file", "edit_file"} {
		if verdict, _ := PermissionFullAuto.judgeWith(rules, tools.Request{Tool: tool, Path: "/p/a", Display: "a"}); verdict != VerdictDeny {
			t.Fatalf("%s = %v, want write(*) to cover it", tool, verdict)
		}
	}
	for _, tool := range []string{"read_file", "list_dir"} {
		if verdict, _ := PermissionAsk.judgeWith(rules, tools.Request{Tool: tool, Path: "/elsewhere/a", Display: "…", Outside: true}); verdict != VerdictAllow {
			t.Fatalf("%s = %v, want read(*) to cover it", tool, verdict)
		}
	}
}

func TestAskIsAValidRuleDecision(t *testing.T) {
	rules := mustRules(t, "ask write(*)")

	// Narrowing a permissive tier for one thing is as useful as widening it.
	if verdict, _ := PermissionFullAuto.judgeWith(rules, tools.Request{Tool: "write_file", Path: "/p/a", Display: "a"}); verdict != VerdictAsk {
		t.Fatalf("verdict = %v, want the rule to force a prompt", verdict)
	}
}

func TestBadRulesAreRejectedWithTheLineThatFailed(t *testing.T) {
	for _, line := range []string{
		"maybe bash(*)",
		"allow",
		"allow bash",
		"allow bash(",
		"allow nonsense(*)",
	} {
		_, err := ParseRules([]string{line})
		if err == nil {
			t.Fatalf("%q was accepted", line)
		}
		if !strings.Contains(err.Error(), line) {
			t.Fatalf("error %q does not quote the offending line", err)
		}
	}
}

func TestCommentsAndBlankLinesAreIgnored(t *testing.T) {
	rules := mustRules(t, "# tests are safe", "", "   ", "allow bash(go test *)")
	if len(rules) != 1 {
		t.Fatalf("parsed %d rules, want 1", len(rules))
	}
}
