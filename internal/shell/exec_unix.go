//go:build !windows

package shell

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// interpreterName is what Run will actually invoke.
func interpreterName() string { return "bash" }

// command builds the process. `bash -c` is byte-compatible with the prototype
// on purpose: the tool tests assert on real command output, and changing the
// interpreter would change that output in ways unrelated to this refactor.
func command(ctx context.Context, c Cmd) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", c.Command)
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}

	// Put the child in its own process group so a kill reaches everything it
	// started. Without this, `npm test &` or a shell pipeline leaves orphans
	// running after a cancelled turn — and in a long-lived daemon those
	// accumulate until something runs out of file descriptors.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Kill the group, not just the leader. exec.CommandContext's default
	// Cancel only signals the direct child.
	cmd.Cancel = func() error { return killGroup(cmd) }

	return cmd, nil
}

// groupChild applies the same rule to a long-lived child that command() applies
// to a one-shot: its own process group, and a cancel that reaches the group
// rather than the leader. The reason is stronger here, not weaker — a provider
// CLI runs its own tool loop, so `bash`, `npm test` and any language server it
// starts are kolk's grandchildren, and this child outlives every turn of a
// session rather than one command.
func groupChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd) }
}

// killChild terminates a child and everything it started.
func killChild(cmd *exec.Cmd) error { return killGroup(cmd) }

// killGroup signals the child's entire process group. A negative pid means
// "the group whose id is this", which is why Setpgid above is load-bearing.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		// The group may already be gone, or Setpgid may not have taken; fall
		// back to the process itself rather than leaving it running.
		return cmd.Process.Kill()
	}
	return nil
}
