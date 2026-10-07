//go:build linux

package shell

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ProcessIdentity includes the boot ID, kernel start tick and executable.
// A registry PID is never sufficient authority to signal a process.
func ProcessIdentity(ctx context.Context, pid int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if pid <= 0 {
		return "", fmt.Errorf("invalid process id %d", pid)
	}
	base := "/proc/" + strconv.Itoa(pid)
	b, err := os.ReadFile(base + "/stat")
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return "", fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" {
		return "", fmt.Errorf("process %d is not running", pid)
	}
	exe, err := os.Readlink(base + "/exe")
	if err != nil {
		return "", err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(boot)) + ":" + fields[19] + ":" + exe, nil
}
