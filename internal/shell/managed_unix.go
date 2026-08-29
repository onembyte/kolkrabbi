//go:build !windows && !linux

package shell

import (
	"os/exec"
	"syscall"
)

// managedProcAttr puts the child in its own process group. There is no death
// signal outside Linux: a server kolk started can outlive a kolk that was
// killed rather than closed, and the next session will find it running and
// adopt it read-only — the safe direction, since it never stops what it did
// not start.
func managedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func killManaged(cmd *exec.Cmd) error { return killGroup(cmd) }
