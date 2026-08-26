package tui

import (
	"strings"
	"testing"
)

func approvalFixture(t *testing.T, approval Approval) *Controller {
	t.Helper()
	c := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	c.RequestApproval(approval)
	return c
}

func typeAnswer(c *Controller, answer string) Effect {
	if answer != "" {
		c.HandleKey(Key{Kind: KeyText, Text: answer})
	}
	return c.HandleKey(Key{Kind: KeyEnter})
}

func TestTheOverlaySaysWhatAlwaysWouldMean(t *testing.T) {
	c := approvalFixture(t, Approval{
		Action: "Run shell command",
		Detail: "go test ./...",
		Rule:   "allow bash(go test *)",
	})

	lines := strings.Join(c.approvalLines(80), "\n")
	if !strings.Contains(lines, "allow bash(go test *)") {
		t.Fatalf("overlay = %q, want it to show the rule `a` would keep", lines)
	}
}

func TestAnOverlayWithNoRuleStillOffersOnlyYesAndNo(t *testing.T) {
	c := approvalFixture(t, Approval{Action: "Run shell command", Detail: "x"})

	lines := strings.Join(c.approvalLines(80), "\n")
	if !strings.Contains(lines, "[y/N]") {
		t.Fatalf("overlay = %q, want the plain prompt when nothing can be proposed", lines)
	}
}

func TestAnsweringAlwaysIsItsOwnDecision(t *testing.T) {
	c := approvalFixture(t, Approval{Action: "a", Detail: "b", Rule: "allow bash(go test *)"})

	// It must be distinguishable from a plain yes, or the rule cannot be kept.
	if effect := typeAnswer(c, "a"); effect.Decision != DecisionAllowAlways {
		t.Fatalf("decision = %v, want DecisionAllowAlways", effect.Decision)
	}
}

func TestAlwaysIsNotOfferedWhenThereIsNoRule(t *testing.T) {
	c := approvalFixture(t, Approval{Action: "a", Detail: "b"})

	// Accepting `a` with nothing to keep would silently mean "yes", which is
	// not what the person typing it asked for.
	if effect := typeAnswer(c, "a"); effect.Decision != DecisionDeny {
		t.Fatalf("decision = %v, want a refusal when there is no rule to keep", effect.Decision)
	}
}

func TestPlainAnswersAreUnchanged(t *testing.T) {
	for answer, want := range map[string]Decision{
		"y": DecisionAllow, "yes": DecisionAllow, "n": DecisionDeny, "": DecisionDeny,
	} {
		c := approvalFixture(t, Approval{Action: "a", Detail: "b", Rule: "allow bash(x)"})
		if effect := typeAnswer(c, answer); effect.Decision != want {
			t.Fatalf("%q = %v, want %v", answer, effect.Decision, want)
		}
	}
}
