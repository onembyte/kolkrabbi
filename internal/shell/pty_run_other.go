//go:build !darwin && !linux

package shell

import (
	"context"
	"os"
	"os/exec"
)

// commandOnPTY is never reached on a platform without a pty: term.OpenPTY
// refuses first with ErrNoPTY, and the caller falls back. It exists so the
// package compiles for every release target.
func commandOnPTY(ctx context.Context, path string, args []string, slave *os.File) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	return cmd
}
