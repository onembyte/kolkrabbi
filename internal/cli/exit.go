package cli

import (
	"context"
	"errors"
	"fmt"
)

// Process exit codes. These are a contract with shells, CI jobs and the
// desktop shell, so they are defined once, here, and nowhere else.
const (
	ExitOK        = 0   // the run succeeded
	ExitError     = 1   // something failed at run time
	ExitUsage     = 2   // the command line was wrong
	ExitBudget    = 3   // a budget or limit stopped the run (reserved for saga)
	ExitInterrupt = 130 // SIGINT — 128 + 2, by shell convention
)

// UsageError marks a bad command line. It exits 2 rather than 1 so a script
// can tell "you typed it wrong" apart from "the model call failed".
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func usagef(format string, a ...any) error { return &UsageError{Msg: fmt.Sprintf(format, a...)} }

// BudgetError marks a run stopped by a spend or step limit. Nothing returns it
// yet; it exists so the exit-code table is complete before saga needs it.
type BudgetError struct{ Msg string }

func (e *BudgetError) Error() string { return e.Msg }

// GuidedError is a failure that ends in commands the user can paste. The
// zero-config promise is only kept if a first-run failure always says exactly
// what to type next, so that obligation is encoded in a type rather than left
// to whoever writes the next error message.
type GuidedError struct {
	Msg            string
	Hint           []string
	ActionRequired bool
}

func (e *GuidedError) Error() string { return e.Msg }

func guided(msg string, hint ...string) error { return &GuidedError{Msg: msg, Hint: hint} }

func guidedAction(msg string, hint ...string) error {
	return &GuidedError{Msg: msg, Hint: hint, ActionRequired: true}
}

// exitCode maps an error to the process exit code.
func exitCode(err error) int {
	var (
		usage  *UsageError
		budget *BudgetError
		guided *GuidedError
	)
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, context.Canceled):
		return ExitInterrupt
	case errors.As(err, &usage):
		return ExitUsage
	case errors.As(err, &budget):
		return ExitBudget
	case errors.As(err, &guided) && guided.ActionRequired:
		return ExitUsage
	default:
		return ExitError
	}
}
