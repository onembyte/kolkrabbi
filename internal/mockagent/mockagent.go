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

	// KilledByTerminate ignores SIGINT and traps nothing else, so SIGTERM's
	// default action kills it. It is the only one of these whose wait status is
	// actually *signalled*: the other two choose their own exit code, which is
	// what a well-behaved vendor does and therefore cannot demonstrate the case
	// that invalidates a conversation. This is the child a hard exit looks like.
	KilledByTerminate Kind = "killed-by-terminate"

	// KilledByInterrupt traps nothing, so SIGINT's default action kills it on
	// rung 1. It is the case that separates "the vendor handled SIGINT and
	// ended its turn" from "SIGINT killed a process that had no handler": the
	// first exits with a code of its own and leaves a result frame, the second
	// is signalled and leaves nothing. They must not be treated alike.
	KilledByInterrupt Kind = "killed-by-interrupt"
)

// Write creates one fake vendor CLI in dir and returns its path along with the
// path of the log it will append signal names to. The caller passes the log
// path to the child as MOCKAGENT_LOG.
func Write(dir string, kind Kind) (executable, logPath string, err error) {
	return writeFake(dir, kind)
}
