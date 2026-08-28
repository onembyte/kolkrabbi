//go:build windows

package shell

import (
	"os"
	"os/exec"
)

// replaceProcess starts the replacement and exits. Windows has no execve, so
// the process id changes and the parent shell sees the original exit; there is
// no way to avoid that without a launcher process.
func replaceProcess(path string, args []string, env []string) error {
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
