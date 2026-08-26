package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

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

// SessionDecider caches approvals with session scope.
type SessionDecider struct {
	underlying Decider
	mu         sync.RWMutex
	rules      map[string]protocol.PermissionDecision
}

// NewSessionDecider wraps an underlying decider with session caching.
func NewSessionDecider(underlying Decider) *SessionDecider {
	return &SessionDecider{
		underlying: underlying,
		rules:      make(map[string]protocol.PermissionDecision),
	}
}

// Confirm checks session rules before consulting the underlying decider.
func (s *SessionDecider) Confirm(ctx context.Context, c Confirmation) bool {
	decision := s.Decide(ctx, c)
	return decision == protocol.PermissionDecisionAllow || decision == protocol.PermissionDecisionAllowSession
}

// Decide checks cached session rules and retains allow_session decisions.
func (s *SessionDecider) Decide(ctx context.Context, c Confirmation) protocol.PermissionDecision {
	key := c.Action + "::" + c.Detail

	s.mu.RLock()
	if cached, ok := s.rules[key]; ok {
		s.mu.RUnlock()
		return cached
	}
	s.mu.RUnlock()

	var decision protocol.PermissionDecision
	if pd, ok := s.underlying.(PolicyDecider); ok {
		decision = pd.Decide(ctx, c)
	} else if s.underlying != nil && s.underlying.Confirm(ctx, c) {
		decision = protocol.PermissionDecisionAllow
	} else {
		decision = protocol.PermissionDecisionDeny
	}

	if decision == protocol.PermissionDecisionAllowSession {
		s.mu.Lock()
		s.rules[key] = decision
		s.mu.Unlock()
	}

	return decision
}
