package serve

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// Listen creates a net.Listener on addr. If addr starts with "unix:" or contains a path separator,
// it binds a unix domain socket; otherwise it binds a TCP listener.
func Listen(addr string) (net.Listener, error) {
	if strings.HasPrefix(addr, "unix:") {
		sockPath := strings.TrimPrefix(addr, "unix:")
		if err := removeSocket(sockPath); err != nil {
			return nil, err
		}
		return net.Listen("unix", sockPath)
	}
	if strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "./") {
		if err := removeSocket(addr); err != nil {
			return nil, err
		}
		return net.Listen("unix", addr)
	}
	addr = strings.TrimPrefix(addr, "tcp:")
	return net.Listen("tcp", addr)
}

// removeSocket clears a stale Unix socket without turning a typo into an
// arbitrary file deletion. net.Listen cannot replace an existing socket, but
// regular files and directories must be left untouched.
func removeSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket Unix address %s", path)
	}
	return os.Remove(path)
}
