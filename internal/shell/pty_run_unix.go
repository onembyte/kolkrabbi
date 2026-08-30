//go:build darwin || linux

package shell

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// commandOnPTY gives the child the pty as its three standard descriptors and a
// session of its own, so the pty becomes its controlling terminal rather than
// kolk's. Without Setsid the child would inherit kolk's, and its full-screen UI
// would draw over the session it was launched from.
func commandOnPTY(ctx context.Context, path string, args []string, slave *os.File) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	return cmd
}
