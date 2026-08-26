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

func TestTheOverlayShowsEveryLineOfADiff(t *testing.T) {
	detail := "@@ -1,3 +1,3 @@\n package main\n-const Port = 8080\n+const Port = 9090"
	c := approvalFixture(t, Approval{Action: "Edit file config.go", Detail: detail})

	lines := c.approvalLines(80)

	// Structure, not substrings: flattening the diff onto one row keeps every
	// substring and destroys the only thing that made it readable. Asserting
	// Contains alone passes on exactly the renderer this test exists to reject.
	rowOf := func(want string) int {
		for i, line := range lines {
			if strings.Contains(line, want) {
				return i
			}
		}
		t.Fatalf("overlay lost %q:\n%s", want, strings.Join(lines, "\n"))
		return -1
	}
	removed, added := rowOf("-const Port = 8080"), rowOf("+const Port = 9090")
	if removed == added {
		t.Fatalf("the two sides of the change share a row:\n%s", strings.Join(lines, "\n"))
	}
	if context := rowOf(" package main"); context == removed || context == added {
		t.Fatalf("context shares a row with a change:\n%s", strings.Join(lines, "\n"))
	}
}

func TestALongDetailIsBoundedInTheOverlay(t *testing.T) {
	var detail strings.Builder
	for i := range 200 {
		detail.WriteString("+line ")
		detail.WriteByte(byte('0' + i%10))
		detail.WriteString("\n")
	}
	c := approvalFixture(t, Approval{Action: "Write file big.txt", Detail: detail.String()})

	// An overlay taller than the terminal pushes its own question off screen.
	if got := len(c.approvalLines(80)); got > 40 {
		t.Fatalf("overlay is %d lines tall", got)
	}
}

func TestEachDiffLineIsStillSanitised(t *testing.T) {
	c := approvalFixture(t, Approval{Action: "Edit", Detail: "-safe\n+\x1b[31mred\x1b[0m\n-tail"})

	joined := strings.Join(c.approvalLines(80), "\n")
	if strings.Contains(joined, "\x1b[31m") {
		t.Fatalf("an escape survived into the overlay: %q", joined)
	}
	if !strings.Contains(joined, "red") {
		t.Fatalf("sanitising ate the text: %q", joined)
	}
}
