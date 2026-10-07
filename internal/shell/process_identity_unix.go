//go:build !windows && !linux

package shell

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ProcessIdentity distinguishes a saved PID from a later process reusing it.
// It is used only to decide whether a runtime may be reused, never to kill one.
func ProcessIdentity(ctx context.Context, pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid process id %d", pid)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "stat=", "-o", "lstart=", "-o", "comm=")
	cmd.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
	b, err := cmd.Output()
	value := strings.TrimSpace(string(b))
	if err != nil || value == "" || strings.HasPrefix(value, "Z") {
		return "", fmt.Errorf("process %d is not running", pid)
	}
	// The state changes with scheduling; start time and executable do not.
	_, identity, ok := strings.Cut(value, " ")
	if !ok {
		return "", fmt.Errorf("cannot identify process %d", pid)
	}
	return strings.TrimSpace(identity), nil
}
