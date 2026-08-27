package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// doomLoopPhrase is the one sentence fragment both doom-loop stops use: the
// saga's, when three chapters fail without progress, and a turn's, when one
// call repeats without progress.
//
// It is a constant so the two cannot drift apart quietly. A user who meets both
// should recognise the second from the first; two vocabularies for one failure
// teach them that the words do not mean anything in particular.
const doomLoopPhrase = "stopped after repeated"

// writeAdvice appends the "what happened, what to do" pair to a printed error,
// when the failure is one the provider layer recognises.
//
// It exists as a function rather than as three copies because a turn fails in
// three places — a one-shot command, the plain REPL and the TUI — and the two
// interactive ones are where a person actually meets a 401 or a 429. Advice
// that only the one-shot path prints would miss the common case.
func writeAdvice(w io.Writer, err error) {
	// The doom-loop stop is handled here rather than in provider.Advise
	// because DoomLoopError is an engine type and Advise lives a layer below
	// the engine: L3 cannot see L4. The surface is the right place for it
	// anyway — this is where the two stops have to sound like each other.
	var loop *engine.DoomLoopError
	if errors.As(err, &loop) {
		fmt.Fprintf(w, "  %s calls to %s without progress: same arguments, same result, %d times\n",
			doomLoopPhrase, loop.Tool, loop.Repeats)
		fmt.Fprintln(w, "  Ask for something different, or `/undo` to take the turn back. "+
			"Raising effort will not help — the limit is three whatever the budget.")
		return
	}

	advice, ok := provider.Advise(err)
	if !ok {
		return
	}
	fmt.Fprintf(w, "  %s\n", advice.Summary)
	fmt.Fprintf(w, "  %s\n", advice.NextAction)
}
