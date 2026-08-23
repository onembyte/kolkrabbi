package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/stats"
)

func (a *app) runStats(_ context.Context, args []string) error {
	recs, err := stats.Load(configDir())
	if err != nil {
		return err
	}
	rows := stats.Aggregate(recs)
	if len(args) > 0 && args[0] == "--json" {
		return a.printJSON(rows)
	}
	fmt.Fprint(a.stdout, stats.Render(rows))
	fmt.Fprintf(a.stdout, "\nlocal data: %s (delete it any time; nothing ever leaves this machine)\n",
		filepath.Join(configDir(), "stats.jsonl"))
	return nil
}
