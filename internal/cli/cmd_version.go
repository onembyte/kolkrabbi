package cli

import (
	"context"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/buildinfo"
)

func (a *app) runVersion(_ context.Context, args []string) error {
	info := buildinfo.Get()
	if len(args) > 0 && args[0] == "--json" {
		return a.printJSON(info)
	}
	fmt.Fprintln(a.stdout, info)
	return nil
}
