//go:build unix

package shell

import (
	"os"
	"syscall"
)

// secretProcAttr detaches the helper from the controlling terminal, so even a
// path that would call getpass(3) has no tty to read.
func secretProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// secretHelperEnv is the minimum the helper needs: where binaries are and
// whose keychain to open. Nothing else from kolk's environment crosses.
func secretHelperEnv() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
}
