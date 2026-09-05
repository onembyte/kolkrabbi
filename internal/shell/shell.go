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
	"os"
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
	// Sandbox confines the command when set (plan 13 §7.2). nil means the
	// user has not turned it on, which is the default.
	Sandbox *Sandbox
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
	// Dropped counts the bytes the child wrote past maxCapture, which were read
	// and discarded rather than kept. Zero for ordinary output.
	Dropped int64
}

// maxCapture bounds what Run keeps of a child's output. The tool layer cuts a
// result to 12k characters for the model, but it can only cut what has been read
// into memory, so the bound has to be here: a command that prints a gigabyte
// costs a megabyte, the first one, and a count.
const maxCapture = 1 << 20

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

// inheritedEnv keeps normal process configuration while withholding common
// credential variables from model- and hook-authored shell commands. Output
// scrubbing cannot prevent `curl -d "$OPENROUTER_API_KEY" ...` from exfiltrating
// a key before it reaches the transcript. Explicit Cmd.Env entries remain
// available to tightly scoped callers such as hooks.
func inheritedEnv(extra []string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if sensitiveEnvName(name) {
			continue
		}
		env = append(env, entry)
	}
	return append(env, extra...)
}

func sensitiveEnvName(name string) bool {
	upper := strings.ToUpper(name)
	if upper == "OPENROUTER_API_KEY" || upper == "SSH_AUTH_SOCK" {
		return true
	}
	// A denylist by name shape, not an allowlist: a delegated coding child
	// runs the repository's own build tools, which read GOFLAGS, NVM_DIR,
	// CARGO_HOME and whatever else the user's shell set, and an allowlist
	// would have to know them all. The shapes below are what credentials
	// look like in the wild; _ACCESS_KEY catches AWS_SECRET_ACCESS_KEY, which
	// the _SECRET suffix did not, _PAT the GitHub/Azure spelling, and
	// _AUTHTOKEN npm's `//registry.npmjs.org/:_authToken`, which the _TOKEN
	// suffix did not (F7.3's reviewer found it).
	//
	// The vendor's own API key is on this list on purpose. A claude or codex
	// child that receives ANTHROPIC_API_KEY or OPENAI_API_KEY bills the API
	// instead of the plan the user signed in with, and switching someone's
	// bill from a subscription to metered because a variable happened to be
	// exported is the spend rule violated sideways. Subscription children
	// authenticate through the vendor's own login, never through the parent
	// environment.
	for _, suffix := range []string{"_API_KEY", "_TOKEN", "_PASSWORD", "_PASSWD", "_SECRET", "_CREDENTIAL", "_ACCESS_KEY", "_PAT", "_PASSPHRASE", "_AUTHTOKEN"} {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	// Anywhere in the name, not only at the end: OPENAI_API_KEY_BACKUP and
	// MY_SECRET_2 are still credentials. TOKEN stays suffix-only because
	// TOKENIZERS_PARALLELISM is a build variable, not a credential.
	for _, fragment := range []string{"API_KEY", "PRIVATE_KEY", "SECRET", "PASSWORD"} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

func (s *platformShell) Name() string { return interpreterName() }

func (s *platformShell) Run(ctx context.Context, c Cmd) (Result, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// A policy this machine cannot enforce is refused before anything is
	// built. This is a Result and not an error on purpose: an error aborts the
	// turn, and "I would not run this, here is why and here is the switch" is
	// exactly the kind of thing the model should read and pass on.
	var wrap *sandboxWrap
	if c.Sandbox != nil {
		if _, err := mechanism(); err != nil {
			return Result{ExitCode: -1, Failure: Refusal(err)}, nil
		}
		w, err := prepareSandbox(*c.Sandbox)
		if err != nil {
			return Result{ExitCode: -1, Failure: Refusal(err)}, nil
		}
		wrap = w
		c.Env = append(c.Env, w.Env...)
		// The policy's temp IS the child's temp. Otherwise every tool that
		// honours TMPDIR -- go, npm, pip -- writes its scratch outside the
		// sandbox and is refused for doing what it was told.
		c.Env = append(c.Env, "TMPDIR="+c.Sandbox.Temp)
	}

	cmd, err := command(cctx, c, wrap)
	if err != nil {
		// The command could not even be built — a missing interpreter. That is
		// a broken machine, not a failed command, so it aborts.
		return Result{ExitCode: -1, Failure: err.Error()}, err
	}
	cmd.WaitDelay = outputDrainTimeout

	// CombinedOutput with a ceiling: both streams into one bounded writer, in
	// the order a person would have seen them.
	out := &capture{limit: maxCapture}
	cmd.Stdout, cmd.Stderr = out, out
	runErr := cmd.Run()
	res := Result{Output: out.String(), ExitCode: exitCodeOf(runErr), Dropped: out.dropped}

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

// Quote renders a command line for display. It is for humans — a confirmation
