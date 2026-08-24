package engine

import "context"

// Confirmation is one side-effecting action awaiting a user decision.
type Confirmation struct {
	Action string
	Detail string
}

// Decider is the presentation-independent permission port. Terminal, daemon,
// and future desktop clients can each ask through their own event loop without
// making the engine read a particular stdin stream.
type Decider interface {
	Confirm(context.Context, Confirmation) bool
}
