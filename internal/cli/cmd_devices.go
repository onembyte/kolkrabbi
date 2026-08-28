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
		if args[0] != "revoke" {
			return usagef("unknown devices command %q", args[0])
		}
		if len(args) < 2 {
			return usagef("usage: kolk devices revoke <id>")
		}
		return a.revokeDevice(store, d.DevicesFile(), args[1])
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

// revokeDevice removes one paired device and writes the store back.
//
// The write is the point. A revoke that stays in memory is a revoke that lasts
// until the process exits, which is the opposite of what the person asking for
// it believes they have done.
func (a *app) revokeDevice(store *devices.Store, file, id string) error {
	// Read the label before removing it: afterwards there is nothing left to
	// name, and "revoked <id>" makes someone go and look up which one that was.
	label := ""
	for _, device := range store.List() {
		if device.ID == id {
			label = device.Label
		}
	}

	if !store.Revoke(id) {
		// Naming what is there turns a typo into a correction rather than a
		// second guess. Reporting success would be worse than this error: it
		// would tell someone a device they still worry about is gone.
		return usagef("no device %q is paired.%s", id, pairedList(store))
	}
	if err := store.Save(file); err != nil {
		return fmt.Errorf("saving the device list: %w", err)
	}
	fmt.Fprintf(a.stdout, "revoked %s (%s). its token no longer authenticates.\n", label, id)
	return nil
}

func pairedList(store *devices.Store) string {
	paired := store.List()
	if len(paired) == 0 {
		return " nothing is paired with this machine."
	}
	out := " paired:"
	for _, device := range paired {
		out += fmt.Sprintf("\n  %s  %s", device.ID, device.Label)
	}
	return out
}
