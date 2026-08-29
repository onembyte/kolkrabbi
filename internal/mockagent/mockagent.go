// Package mockagent writes fake vendor CLIs for tests that need to observe how
// a real child process reacts to signals.
//
// The spawn ladder cannot be tested with `sh -c` alone. Proving that
// cancellation sends SIGINT *first* and escalates — rather than reaching for
// SIGKILL, which §2.5 forbids because it destroys the vendor's `result` frame
// and invalidates the session — requires a child that deliberately ignores
// SIGINT, and one that handles it. Neither is a thing a stock POSIX tool does
// on request.
//
// These are shell scripts rather than compiled binaries on purpose: a test that
// has to run `go build` first is a test that needs a toolchain, a temp GOCACHE
// and several seconds, and none of that makes the assertion stronger. What is
// under test is the parent's signalling, not the child's language.
//
// Each script records the signals it received, one per line, in the file named
// by the MOCKAGENT_LOG environment variable. That log is the evidence: it says
// which rung actually fired, which an exit status alone cannot distinguish once
// a child chooses its own exit code.
package mockagent

// Kind names one fake vendor CLI.
type Kind string

const (
	// ExitsOnInterrupt behaves the way the vendor documents: SIGINT ends the
	// turn gracefully and the process leaves on its own. A ladder that works
	// must stop at rung 1 for this child and never escalate.
	ExitsOnInterrupt Kind = "exits-on-interrupt"

	// IgnoresInterrupt traps SIGINT and keeps running, exiting only on
	// SIGTERM. It is the child that proves escalation happens at all, and that
	// rung 2 is reached before rung 3.
	IgnoresInterrupt Kind = "ignores-interrupt"
)

// Write creates one fake vendor CLI in dir and returns its path along with the
// path of the log it will append signal names to. The caller passes the log
// path to the child as MOCKAGENT_LOG.
func Write(dir string, kind Kind) (executable, logPath string, err error) {
	return writeFake(dir, kind)
}
