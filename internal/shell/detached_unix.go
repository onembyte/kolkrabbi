//go:build !windows

package shell

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
