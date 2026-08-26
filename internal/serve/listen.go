package serve

import (
	"net"
	"os"
	"strings"
)

// Listen creates a net.Listener on addr. If addr starts with "unix:" or contains a path separator,
// it binds a unix domain socket; otherwise it binds a TCP listener.
func Listen(addr string) (net.Listener, error) {
	if strings.HasPrefix(addr, "unix:") {
		sockPath := strings.TrimPrefix(addr, "unix:")
		_ = os.Remove(sockPath)
		return net.Listen("unix", sockPath)
	}
	if strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "./") {
		_ = os.Remove(addr)
		return net.Listen("unix", addr)
	}
	addr = strings.TrimPrefix(addr, "tcp:")
	return net.Listen("tcp", addr)
}
