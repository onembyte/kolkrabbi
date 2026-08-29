//go:build linux

package shell

import (
	"os/exec"
	"syscall"
)

// managedProcAttr puts the child in its own process group and asks the kernel
// to SIGTERM it when kolk's thread dies. That is the only thing that stops a
// server kolk started from outliving a kolk that was killed rather than
// closed — and a survivor would be adopted by the next session as a host
// server it must never stop.
func managedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

func killManaged(cmd *exec.Cmd) error { return killGroup(cmd) }
