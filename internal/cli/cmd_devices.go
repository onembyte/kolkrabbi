package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/onembyte/kolkrabbi/internal/devices"
)

// runDevices lists the devices paired with this machine.
//
// Pairing was one-way until this existed: the store could revoke, and nothing
// called it, so a device paired once stayed paired until someone hand-edited
// devices.json. A credential you cannot withdraw is not really a credential you
// granted.
func (a *app) runDevices(ctx context.Context, args []string) error {
	d, err := a.resolve()
	if err != nil {
		return err
	}
	store, err := devices.Load(d.DevicesFile())
	if err != nil {
		return fmt.Errorf("reading paired devices: %w", err)
	}

	if len(args) > 0 {
		return usagef("unknown devices command %q", args[0])
	}

	paired := store.List()
	if len(paired) == 0 {
		// A blank screen cannot be told from a command that failed.
		fmt.Fprintln(a.stdout, "no devices are paired with this machine.")
		fmt.Fprintln(a.stdout, "pair one by running `kolk serve` and arming pairing from the session.")
		return nil
	}

	w := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tLABEL\tTIER\tPAIRED\tLAST SEEN")
	for _, device := range paired {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			device.ID, device.Label, device.Tier, ago(device.Created), lastSeen(device.LastSeen))
	}
	return w.Flush()
}

// lastSeen distinguishes a device that has never connected from one that
// connected a moment ago. Printing a zero time as if it were a date is how a
// listing tells its most useful fact wrong.
func lastSeen(when time.Time) string {
	if when.IsZero() {
		return "never"
	}
	return ago(when)
}

// ago renders a timestamp as an age, because "when did I pair this" is a
// question about elapsed time and never about a clock reading.
func ago(when time.Time) string {
	if when.IsZero() {
		return "unknown"
	}
	d := time.Since(when)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
