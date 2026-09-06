//go:build windows

package shell

import (
	"os"
	"syscall"
)

func secretProcAttr() *syscall.SysProcAttr { return nil }

func secretHelperEnv() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "USERPROFILE=" + os.Getenv("USERPROFILE")}
}
