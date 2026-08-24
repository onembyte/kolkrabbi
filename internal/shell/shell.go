// Package shell runs external processes. It is the only package in the tree
// permitted to import os/exec — enforced by internal/arch.
//
// That rule exists for one reason: process execution is the single largest
// source of platform divergence in an agentic CLI, and it is divergence that
// cannot be discovered by reading code. `bash -c "echo $HOME"` and
// `powershell -Command "echo $HOME"` disagree about quoting, about globbing,
// about what a backslash means, and about what happens to a child when the
// parent is killed. Confining all of it here is what lets the engine, the
// providers and the tools stay honestly platform-free.
package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout bounds a command that never finishes. It is the prototype's
// value, kept deliberately: an agent that hangs on an interactive prompt is far
// more common than one running a legitimately long job, and a long job can ask
// for more.
const DefaultTimeout = 120 * time.Second

// outputDrainTimeout bounds the gap after the direct shell exits but a
// background descendant still owns stdout or stderr. The command timeout can
// no longer help at that point: os/exec has observed a successful exit and is
// waiting only for pipe EOF. A short grace period preserves ordinary buffered
// output without letting an intentional `nohup ... &` freeze an agent turn.
const outputDrainTimeout = 500 * time.Millisecond

// Cmd is one command to run. Command is a shell command line, not an argv:
// models write shell, and pretending otherwise means reimplementing a parser
// that is already installed on the machine.
type Cmd struct {
	Command string        // the command line, interpreted by the platform shell
	Dir     string        // working directory; "" means the process's own
	Env     []string      // extra KEY=VALUE entries, appended to the environment
	Timeout time.Duration // 0 means DefaultTimeout
	Stdin   io.Reader     // usually nil: an agent's command must not block on input
}

// Result is what a finished command produced.
//
// It deliberately holds no error. Run's returned error means "this command
// could not be run to a conclusion" — a cancelled turn — and nothing else; a
// command that ran and failed is a successful Run with a failing Result. That
// split matters because a non-zero exit is information the model should see and
// react to, not a failure that aborts the turn.
type Result struct {
	Output   string // stdout and stderr interleaved, as a person would see them
	ExitCode int    // 0 on success; -1 when the process never produced a code
	TimedOut bool   // the command was killed for exceeding its timeout
	Failure  string // "" on success; otherwise a sentence explaining what went wrong
}

// OK reports whether the command succeeded.
func (r Result) OK() bool { return r.Failure == "" }

// Shell runs commands. It is an interface so that the engine can be handed a
// recording or refusing implementation in a test or a sandbox, without any of
// its callers knowing which one they have.
type Shell interface {
	// Run executes a command to completion and returns everything it wrote.
	Run(ctx context.Context, c Cmd) (Result, error)

	// Name reports the interpreter in use, for error messages and `kolk doctor`.
	Name() string
}

// New returns the platform shell: bash on Unix, PowerShell (falling back to
// cmd.exe) on Windows.
func New() Shell { return &platformShell{} }

type platformShell struct{}

func (s *platformShell) Name() string { return interpreterName() }

func (s *platformShell) Run(ctx context.Context, c Cmd) (Result, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd, err := command(cctx, c)
	if err != nil {
		// The command could not even be built — a missing interpreter. That is
		// a broken machine, not a failed command, so it aborts.
		return Result{ExitCode: -1, Failure: err.Error()}, err
	}
	cmd.WaitDelay = outputDrainTimeout

	out, runErr := cmd.CombinedOutput()
	res := Result{Output: string(out), ExitCode: exitCodeOf(runErr)}

	switch {
	case runErr == nil:
		return res, nil

	// The direct shell succeeded, but an intentional background descendant
	// retained its output descriptor. Close only Kolk's capture pipe and keep
	// the command successful: retrying it could start the service twice.
	case errors.Is(runErr, exec.ErrWaitDelay):
		res.ExitCode = 0
		if res.Output != "" && !strings.HasSuffix(res.Output, "\n") {
			res.Output += "\n"
		}
		res.Output += fmt.Sprintf(
			"[background process kept command output open; capture detached after %s and it may still be running]\n",
			outputDrainTimeout,
		)
		return res, nil

	// A cancelled turn is not a failed command, and the caller has to be able
	// to tell the difference: this is the one case that aborts rather than
	// reporting. Exit 130 by shell convention.
	case errors.Is(ctx.Err(), context.Canceled):
		res.ExitCode = 130
		res.Failure = "cancelled"
		return res, ctx.Err()

	// A timeout must say so. "signal: killed" is what the prototype surfaced,
	// and it sends whoever reads it looking for the wrong bug.
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		res.Failure = fmt.Sprintf("command timed out after %s", timeout)
		return res, nil

	default:
		res.Failure = runErr.Error()
		return res, nil
	}
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// LookPath finds an executable, translating the one error people misread.
//
// Since Go 1.19 exec.LookPath refuses a match found in the current directory
// and returns exec.ErrDot. The raw message ("cannot run executable found
// relative to current directory") reads like a bug in the tool rather than a
// deliberate refusal, so it is replaced with one that says what to type.
func LookPath(file string) (string, error) {
	path, err := exec.LookPath(file)
	if err == nil {
		return path, nil
	}
	if errors.Is(err, exec.ErrDot) {
		return "", fmt.Errorf("%s was found in the current directory, not on your PATH. "+
			"Run it as ./%s if that is really what you meant", file, file)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("%s is not installed, or not on your PATH", file)
	}
	return "", err
}

// Have reports whether an executable is available. It is the question callers
// actually ask before offering a feature that depends on one.
func Have(file string) bool {
	_, err := LookPath(file)
	return err == nil
}

// Quote renders a command line for display. It is for humans — a confirmation
// prompt, a log line — and is never fed back to a shell.
func Quote(c Cmd) string {
	s := strings.TrimSpace(c.Command)
	if c.Dir != "" {
		return c.Dir + " $ " + s
	}
	return "$ " + s
}
