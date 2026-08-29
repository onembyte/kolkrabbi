package shell

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// ManagedProcess is a child process whose environment and lifecycle are
// explicitly owned by Kolk.
type ManagedProcess struct {
	cmd  *exec.Cmd
	done chan error
	once sync.Once
	err  error
}

// StartManagedProcess starts an executable with exactly the supplied
// environment. It is intended for isolated local runtimes, not user tools.
func StartManagedProcess(ctx context.Context, executable string, args, env []string) (*ManagedProcess, error) {
	path, err := LookPath(executable)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append([]string(nil), env...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// Its own process group, so stopping it stops the workers it spawned —
	// an inference server forks runners, and a Kill on the parent alone
	// leaves them holding the GPU. On Linux the group also dies with kolk.
	cmd.SysProcAttr = managedProcAttr()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting managed process %s: %w", executable, err)
	}
	process := &ManagedProcess{cmd: cmd, done: make(chan error, 1)}
	go func() {
		process.done <- cmd.Wait()
	}()
	return process, nil
}

// Close terminates the process and waits for its exit. It is idempotent.
func (p *ManagedProcess) Close() error {
	if p == nil {
		return nil
	}
	p.once.Do(func() {
		var killErr error
		if p.cmd.Process != nil {
			killErr = killManaged(p.cmd)
		}
		waitErr := <-p.done
		if killErr == nil {
			return
		}
		p.err = waitErr
	})
	return p.err
}

// Pid is the process id, for a transcript line that names what was started.
func (p *ManagedProcess) Pid() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
