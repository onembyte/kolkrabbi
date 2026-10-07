//go:build !windows

package shell

import (
	"context"
	"fmt"
	"syscall"
)

// SignalManagedProcessGroup stops the detached runtime and its workers. Only
// the verified, recorded PID reaches this function; a different process group
// is refused even if that PID still exists.
func SignalManagedProcessGroup(ctx context.Context, pid int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pid <= 0 {
		return fmt.Errorf("invalid process id")
	}
	group, err := syscall.Getpgid(pid)
	if err != nil {
		return err
	}
	if group != pid {
		return fmt.Errorf("process %d is not its own managed process group", pid)
	}
	return syscall.Kill(-pid, syscall.SIGKILL)
}
