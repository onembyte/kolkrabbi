//go:build !windows && !linux

package shell

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ProcessOwnsLoopbackListener uses lsof's process/file report to bind the
// recorded PID to the exact loopback LISTEN socket. If lsof is absent or its
// answer is ambiguous, explicit stop fails closed.
func ProcessOwnsLoopbackListener(ctx context.Context, pid int, addr string) (bool, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" || pid <= 0 {
		return false, fmt.Errorf("invalid local runtime endpoint")
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return false, fmt.Errorf("invalid local runtime port")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-iTCP@"+addr, "-sTCP:LISTEN", "-Fpn")
	cmd.Env = []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/bin"}
	b, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("checking Localia listener: %w", err)
	}
	seenPID, seenAddr := false, false
	for _, line := range strings.Split(string(b), "\n") {
		seenPID = seenPID || line == "p"+strconv.Itoa(pid)
		seenAddr = seenAddr || line == "n"+addr
	}
	return seenPID && seenAddr, nil
}
