//go:build windows

package shell

import (
	"context"
	"fmt"
	"syscall"
)

func ProcessIdentity(ctx context.Context, pid int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if pid <= 0 {
		return "", fmt.Errorf("invalid process id %d", pid)
	}
	const queryLimitedInformation = 0x1000
	h, err := syscall.OpenProcess(queryLimitedInformation, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return "", err
	}
	if exited.HighDateTime != 0 || exited.LowDateTime != 0 {
		return "", fmt.Errorf("process %d exited", pid)
	}
	return fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime), nil
}
