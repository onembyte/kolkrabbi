package cli

import (
	"context"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/session"
)

func (a *app) runSessions(_ context.Context, args []string) error {
	sdir := sessionsDir()

	if len(args) > 0 {
		switch args[0] {
		case "rm":
			if len(args) < 2 {
				return usagef("usage: kolk sessions rm <id>")
			}
			if err := session.Delete(sdir, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "deleted session %s\n", args[1])
			return nil
		case "clear":
			if err := session.Clear(sdir); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, "all sessions deleted.")
			return nil
		default:
			return usagef("%s", usageLine("sessions"))
		}
	}

	all, err := session.List(sdir)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(a.stdout, "no sessions yet.")
		return nil
	}
	for _, s := range all {
		fmt.Fprintf(a.stdout, "%-22s %s  %-32s msgs:%-4d %s\n",
			s.ID, s.UpdatedAt.Format("2006-01-02 15:04"), s.Model, len(s.Messages), s.Title)
	}
	fmt.Fprintln(a.stdout, "\nresume the latest with `kolk -r`, or a specific one with `kolk -s <id>`")
	return nil
}
