//go:build windows

package shell

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr {
	const detachedProcess = 0x00000008
	const newProcessGroup = 0x00000200
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | newProcessGroup, HideWindow: true}
}
