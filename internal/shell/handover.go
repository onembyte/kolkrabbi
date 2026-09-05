package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Handover runs a provider-owned interactive login with the user's terminal
// attached directly. Kolkrabbi supplies no prompt, pipe, environment override,
// or credential input; the provider owns the complete authentication flow.
func Handover(ctx context.Context, executable string, args []string, dir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := LookPath(executable)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	// The same environment the delegated children get: normal configuration,
	// no credential-shaped variables. This child is a vendor CLI signing the
	// user in through its own login; the parent's keys are not its business,
	// and "Kolkrabbi will not see credentials" was printed a moment ago -- the
	// line has to hold in both directions.
	cmd.Env = inheritedEnv(nil)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s login exited unsuccessfully: %w", executable, err)
	}
	return nil
}
