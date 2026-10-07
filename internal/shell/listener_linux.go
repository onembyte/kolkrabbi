//go:build linux

package shell

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessOwnsLoopbackListener proves that a process holds the LISTEN socket
// for the exact recorded IPv4 loopback address. A heartbeat alone could be a
// different process that took over the same port.
func ProcessOwnsLoopbackListener(ctx context.Context, pid int, addr string) (bool, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" || pid <= 0 {
		return false, fmt.Errorf("invalid local runtime endpoint")
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return false, fmt.Errorf("invalid local runtime port")
	}
	f, err := os.Open("/proc/net/tcp")
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	want := fmt.Sprintf("0100007F:%04X", number)
	inodes := make(map[string]bool)
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		fields := strings.Fields(scan.Text())
		if len(fields) >= 10 && fields[1] == want && fields[3] == "0A" {
			inodes[fields[9]] = true
		}
	}
	if err := scan.Err(); err != nil {
		return false, err
	}
	if len(inodes) == 0 {
		return false, nil
	}
	fds, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		return false, err
	}
	for _, fd := range fds {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		link, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", fd.Name()))
		if err == nil && strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") && inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] {
			return true, nil
		}
	}
	return false, nil
}
