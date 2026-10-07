package provider

import (
	"io/fs"
	"net"
	"os"
	"syscall"
	"testing"
)

// A local file error is not a transport limit. syscall.Errno has both
// methods of net.Error, so a naive check read a full disk (ENOSPC), a quota
// (EDQUOT) or a permission error as an unreachable endpoint, which pauses and
// waits instead of saying the disk failed. A real network failure still is one.
func TestAFileErrorIsNotATransportLimit(t *testing.T) {
	for name, err := range map[string]error{
		"no space":   &fs.PathError{Op: "write", Path: "/s/x.tmp", Err: syscall.ENOSPC},
		"quota":      &fs.PathError{Op: "write", Path: "/s/x.tmp", Err: syscall.EDQUOT},
		"read-only":  &os.SyscallError{Syscall: "rename", Err: syscall.EROFS},
		"bare errno": syscall.EIO,
	} {
		if limit, ok := Classify(err); ok {
			t.Errorf("%s was classified as a %s limit", name, limit.Kind)
		}
	}
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}
	if limit, ok := Classify(refused); !ok || limit.Kind != LimitTransport {
		t.Fatalf("a refused connection = %+v %v, want a transport limit", limit, ok)
	}
}
