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
	recs, err := stats.Load(d.Data)
	if err != nil {
		return err
	}
	rows := stats.Aggregate(recs)
	if len(args) > 0 && args[0] == "--json" {
		return a.printJSON(rows)
	}
	fmt.Fprint(a.stdout, stats.Render(rows))
	fmt.Fprintf(a.stdout, "\nlocal data: %s (delete it any time; nothing ever leaves this machine)\n",
		d.StatsFile())
	return nil
}
