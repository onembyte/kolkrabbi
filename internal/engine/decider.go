package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/onembyte/kolkrabbi/protocol"
)

// Confirmation is one side-effecting action awaiting a user decision.
type Confirmation struct {
	Action string
	Detail string
	// Rule is the standing rule that would cover this action, offered so that
	// "always" can mean something the user reads and keeps rather than an
	// invisible cache entry. Empty when there is nothing sensible to propose.
	Rule string
}

// Decider is the presentation-independent permission port.
type Decider interface {
	Confirm(context.Context, Confirmation) bool
}

// PolicyDecider allows a decider to return fine-grained permission decisions
// such as session-scoped approvals.
type PolicyDecider interface {
	Decide(context.Context, Confirmation) protocol.PermissionDecision
}

// TerminalDecider is the standard interactive decider reading from an io.Reader
// and prompting to an io.Writer.
type TerminalDecider struct {
	in  *bufio.Reader
	out io.Writer
}

// NewTerminalDecider constructs a TerminalDecider.
func NewTerminalDecider(in *bufio.Reader, out io.Writer) *TerminalDecider {
	return &TerminalDecider{in: in, out: out}
}

// Confirm prompts the user interactively on out and returns whether allowed.
func (t *TerminalDecider) Confirm(ctx context.Context, c Confirmation) bool {
	decision := t.Decide(ctx, c)
	return decision == protocol.PermissionDecisionAllow || decision == protocol.PermissionDecisionAllowSession
}

// Decide prompts interactively with allow, always, or deny choices.
func (t *TerminalDecider) Decide(ctx context.Context, c Confirmation) protocol.PermissionDecision {
	if t.in == nil || t.out == nil {
		return protocol.PermissionDecisionDeny
	}
	always := "a (always)"
	if c.Rule != "" {
		// Say what "always" will actually mean. An approval whose scope the
		// user cannot see is not one they gave.
		always = "a (" + c.Rule + ")"
	}
	fmt.Fprintf(t.out, "\n%s?%s %s\n%s%s%s\n%sAllow? [y/N/%s]: %s", colorYel, colorReset, c.Action, colorDim, c.Detail, colorReset, colorDim, always, colorReset)
	line, _ := t.in.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	switch line {
	case "y", "yes":
		return protocol.PermissionDecisionAllow
	case "a", "always":
		return protocol.PermissionDecisionAllowSession
	default:
		fmt.Fprintln(t.out, colorDim+"  skipped."+colorReset)
		return protocol.PermissionDecisionDeny
	}
}
