package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// ManagedProcess is a child process whose environment and lifecycle are
// explicitly owned by Kolk.
type ManagedProcess struct {
	cmd    *exec.Cmd
	done   chan error
	exited chan struct{}
	once   sync.Once
	err    error
}

// StartManagedProcess starts an executable with exactly the supplied
// environment. It is intended for isolated local runtimes, not user tools.
func StartManagedProcess(ctx context.Context, executable string, args, env []string) (*ManagedProcess, error) {
	return startRuntimeProcess(ctx, executable, args, env, false)
}

// StartDetachedProcess starts a runtime that may outlive Kolk. Close remains
// available to roll back a failed startup. Its stdio is attached to the null
// device, never pipes serviced by the parent, and it has no parent-death signal.
func StartDetachedProcess(ctx context.Context, executable string, args, env []string) (*ManagedProcess, error) {
	return startRuntimeProcess(ctx, executable, args, env, true)
}

func startRuntimeProcess(ctx context.Context, executable string, args, env []string, detached bool) (*ManagedProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := LookPath(executable)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	if detached {
		cmd = exec.Command(path, args...)
	}
	cmd.Env = append([]string(nil), env...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// Its own process group, so stopping it stops the workers it spawned —
	// an inference server forks runners, and a Kill on the parent alone
	// leaves them holding the GPU. On Linux the group also dies with kolk.
	cmd.SysProcAttr = managedProcAttr()
	if detached {
		cmd.SysProcAttr = detachedProcAttr()
		// io.Discard creates parent-owned copier pipes in os/exec. A real file
		// lets this child keep running after the parent and its goroutines exit.
		null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		defer func() { _ = null.Close() }()
		cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting managed process %s: %w", executable, err)
	}
	process := &ManagedProcess{cmd: cmd, done: make(chan error, 1), exited: make(chan struct{})}
	go func() {
		process.done <- cmd.Wait()
		close(process.exited)
	}()
	return process, nil
}

// Exited reports a reaped process without consuming its completion result.
func (p *ManagedProcess) Exited() bool {
	select {
	case <-p.exited:
		return true
	default:
		return false
	}
}

// Close terminates the process and waits for its exit. It is idempotent.
func (p *ManagedProcess) Close() error {
	if p == nil {
		return nil
	}
	p.once.Do(func() {
		// Wait has already reaped this process. Its old PID/group may now
		// belong to somebody else, so completion is checked before signalling.
		select {
		case <-p.done:
			return
		default:
		}
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
