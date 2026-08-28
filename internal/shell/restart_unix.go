//go:build !windows

package shell

import "syscall"

func replaceProcess(path string, args []string, env []string) error {
	// argv[0] is the program name by convention, so the new process reports
	// itself the way the old one did.
	return syscall.Exec(path, append([]string{path}, args...), env)
}
