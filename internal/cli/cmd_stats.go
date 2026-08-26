package cli

import (
	"context"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/stats"
)

func (a *app) runStats(_ context.Context, args []string) error {
	d, err := a.resolve()
	if err != nil {
		return err
	}
	recs, skipped, err := stats.LoadCounted(d.Data)
	if err != nil {
		return err
	}
	rows := stats.Aggregate(recs)
	if len(args) > 0 && args[0] == "--json" {
		return a.printJSON(rows)
	}
	fmt.Fprint(a.stdout, stats.Render(rows))
	if skipped > 0 {
		// Numbers computed from an incomplete history must say they are
		// incomplete; a total nobody knows is short is worse than no total.
		fmt.Fprintf(a.stdout, "\nwarning: %d unreadable line(s) in %s were skipped, so these totals are incomplete\n",
			skipped, d.StatsFile())
	}
	fmt.Fprintf(a.stdout, "\nlocal data: %s (delete it any time; nothing ever leaves this machine)\n",
		d.StatsFile())
	return nil
}
