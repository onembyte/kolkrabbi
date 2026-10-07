package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Handover runs a provider-owned interactive login with the user's terminal
// attached directly. The provider owns prompts and credentials; Kolkrabbi
// scrubs inherited secrets and may bind Ollama to the server being verified.
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
	cmd.Env = loginEnvironment(ctx)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s login exited unsuccessfully: %w", executable, err)
	}
	return nil
}
