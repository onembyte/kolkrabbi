//go:build !windows

package shell

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// interpreterName is what Run will actually invoke.
func interpreterName() string { return "bash" }

// command builds the process. `bash -c` is byte-compatible with the prototype
// on purpose: the tool tests assert on real command output, and changing the
// interpreter would change that output in ways unrelated to this refactor.
func command(ctx context.Context, c Cmd) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", c.Command)
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}

	// Put the child in its own process group so a kill reaches everything it
	// started. Without this, `npm test &` or a shell pipeline leaves orphans
	// running after a cancelled turn — and in a long-lived daemon those
	// accumulate until something runs out of file descriptors.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Kill the group, not just the leader. exec.CommandContext's default
	// Cancel only signals the direct child.
	cmd.Cancel = func() error { return killGroup(cmd) }

	return cmd, nil
}

// groupChild applies the same rule to a long-lived child that command() applies
// to a one-shot: its own process group, and a cancel that reaches the group
// rather than the leader. The reason is stronger here, not weaker — a provider
// CLI runs its own tool loop, so `bash`, `npm test` and any language server it
// starts are kolk's grandchildren, and this child outlives every turn of a
// session rather than one command.
func groupChild(cmd *exec.Cmd, exited <-chan struct{}) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Cancel returns nil rather than killing: the ladder owns termination from
	// here, and os/exec then waits for the child to leave on its own. Returning
	// an error here would make the ladder's careful teardown moot by reporting
	// the cancellation as the process's failure.
	// The schedule is read here, on the goroutine that starts the child, and
	// captured. Reading the package variables from inside the ladder goroutine
	// instead is a data race against a test that adjusts them — found by the
	// race detector rather than reasoned about, and worth keeping fixed this
	// way: the graces belong to the child as configured at spawn, not to
	// whatever the package happens to hold when cancellation lands.
	schedule := []rung{{syscall.SIGINT, sigintGrace}, {syscall.SIGTERM, sigtermGrace}}
	cmd.Cancel = func() error {
		go cancelLadder(cmd, exited, schedule)
		return nil
	}
}

// rung is one step of the cancel ladder: a signal, and how long the child is
// given to act on it before the next step.
type rung struct {
	signal syscall.Signal
	grace  time.Duration
}

// Ladder graces, from §2.5. Variables rather than constants so a test can walk
// all three rungs without spending seven seconds proving arithmetic.
var (
	sigintGrace  = 5 * time.Second
	sigtermGrace = 2 * time.Second
)

// cancelLadder walks SIGINT → SIGTERM → SIGKILL against the process group,
// stopping as soon as the child leaves.
//
// Starting at SIGINT is not politeness. The vendor documents that SIGINT ends
// the turn gracefully and **still produces a result frame**, which is the only
// authority for continuity: it carries the turn's accounting, so a cancelled
// turn is not a hole in the dashboard. Worse, §2.5's starred rule — a
// SIGTERM/SIGKILL exit *invalidates the vendor session*, because the vendor
// resumes an unfinished turn on --resume and would silently execute the tool
// calls kolk already told the user were cancelled, editing files after a
// "cancelled" turn. Reaching for SIGKILL first is therefore a correctness
// failure, not a rudeness.
func cancelLadder(cmd *exec.Cmd, exited <-chan struct{}, schedule []rung) {
	for _, rung := range schedule {
		if err := signalGroup(cmd, rung.signal); err != nil {
			return // already gone
		}
		timer := time.NewTimer(rung.grace)
		select {
		case <-exited:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	_ = signalGroup(cmd, syscall.SIGKILL)
}

// exitedHard reports that the child was terminated by a signal rather than
// leaving on its own, which means its work is unfinished.
//
// The predicate is "signalled", with **no exception for SIGINT**, and the
// reason is worth stating because the obvious reading of §2.5 says otherwise.
// §2.5 records that the vendor answers SIGINT by ending the turn gracefully and
// still producing a result frame — but a process that *handles* a signal exits
// with a code of its own choosing, and its wait status is not signalled at all.
// A status that IS signalled means no handler ran: no result frame, an
// unfinished turn, and a conversation the vendor would continue on --resume.
// SIGINT included.
//
// An earlier draft of this function excluded SIGINT. Mutation testing found it:
// removing the exclusion changed no test, because the test meant to cover it
// used a child that exits cleanly and therefore never reached the branch. A
// line no test can kill is a line worth re-reading, and this one was wrong as
// well as dead — it would have called a SIGINT-killed vendor resumable.
func exitedHard(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled()
}

// signalGroup sends one signal to the child's entire process group.
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return syscall.ESRCH
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		// The group may already be gone, or Setpgid may not have taken; fall
		// back to the process itself rather than leaving it running.
		return cmd.Process.Signal(sig)
	}
	return nil
}

// killChild terminates a child and everything it started.
func killChild(cmd *exec.Cmd) error { return killGroup(cmd) }

// killGroup signals the child's entire process group. A negative pid means
// "the group whose id is this", which is why Setpgid above is load-bearing.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		// The group may already be gone, or Setpgid may not have taken; fall
		// back to the process itself rather than leaving it running.
		return cmd.Process.Kill()
	}
	return nil
}
