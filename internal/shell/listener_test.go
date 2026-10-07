//go:build !windows

package shell

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

func TestProcessOwnsOnlyItsExactLoopbackListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable: %v", err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok, err := ProcessOwnsLoopbackListener(ctx, os.Getpid(), listener.Addr().String())
	if err != nil || !ok {
		t.Fatalf("this process owns %s: ok=%v err=%v", listener.Addr(), ok, err)
	}
	ok, err = ProcessOwnsLoopbackListener(ctx, os.Getpid()+1, listener.Addr().String())
	if err == nil && ok {
		t.Fatal("another PID was credited with this listener")
	}
}
