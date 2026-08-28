package shell

import "os"

// Replace restarts the current program as path with args, giving up this
// process. It never returns on success.
//
// On unix this is execve: the same process id, the same terminal, the same
// parent — the shell that launched kolk is not told anything happened, because
// nothing did. Windows has no execve, so there the new process is started and
// this one exits, which is the closest available behaviour.
//
// The caller must have restored the terminal first. A raw-mode terminal handed
// to a process that fails to start is a shell the user has to reset by hand.
func Replace(path string, args []string, env []string) error {
	return replaceProcess(path, args, env)
}

// SelfPath is the executable to restart into, resolved through any symlink so
// an installer that swapped the binary under a stable path is picked up.
func SelfPath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return path, nil
}
