package cli

import (
	"fmt"
	"io"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// writeAdvice appends the "what happened, what to do" pair to a printed error,
// when the failure is one the provider layer recognises.
//
// It exists as a function rather than as three copies because a turn fails in
// three places — a one-shot command, the plain REPL and the TUI — and the two
// interactive ones are where a person actually meets a 401 or a 429. Advice
// that only the one-shot path prints would miss the common case.
func writeAdvice(w io.Writer, err error) {
	advice, ok := provider.Advise(err)
	if !ok {
		return
	}
	fmt.Fprintf(w, "  %s\n", advice.Summary)
	fmt.Fprintf(w, "  %s\n", advice.NextAction)
}
