//go:build windows

package shell

import (
	"os/exec"
	"syscall"
)

// managedProcAttr on Windows is nothing yet. A job object with
// KILL_ON_JOB_CLOSE is what would make the child die with kolk; it is not
// built, and this stub says so rather than claiming it. Close kills the direct
// child only, so runners it spawned may survive.
func managedProcAttr() *syscall.SysProcAttr { return nil }

func killManaged(cmd *exec.Cmd) error { return cmd.Process.Kill() }
