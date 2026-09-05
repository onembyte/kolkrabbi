package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// closeGrace is how long a provider process may take to exit after its stdin
// closes before Kolkrabbi terminates it.
// closeGrace is how long Close gives the child to leave on its own after its
// stdin is closed, before the group is killed. A variable so a test can walk
// the whole shutdown without spending the real five seconds.
var closeGrace = 5 * time.Second

// stderrRingBytes is how much of a provider's stderr is kept. This process
// lives for the whole session, so its stderr is not a transcript to retain —
// it is a diagnostic to retain the end of, and the end is the part that says
// why it stopped. 8 KiB holds a stack trace or a login refusal comfortably.
const stderrRingBytes = 8 << 10

var errLinesProcessClosed = errors.New("provider output delivery stopped")

// stderrRing keeps the last stderrRingBytes of a child's stderr and discards
// the rest. os/exec writes to it from its own goroutine while the reader
// goroutine may be reading it at exit, so it carries its own mutex: a
// bytes.Buffer shared across that boundary is a data race the race detector
// only catches when a child happens to be chatty at the wrong moment.
type stderrRing struct {
	mu   sync.Mutex
	buf  []byte
	over bool // true once anything has been discarded
}

func (r *stderrRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > stderrRingBytes {
		r.buf = append(r.buf[:0], r.buf[len(r.buf)-stderrRingBytes:]...)
		r.over = true
	}
	return len(p), nil
}

// String reports the retained tail, saying so when there was more. Naming the
// elision matters: "the last 8 KiB" and "all of it" lead to different next
// questions, and a reader cannot tell them apart from the text alone.
func (r *stderrRing) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.over {
		return "…(earlier stderr discarded)… " + string(r.buf)
	}
	return string(r.buf)
}

func (r *stderrRing) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buf)
}

// LinesProcess is one long-lived child process with line-delimited stdin and
// stdout. It is suitable for a provider session that accepts NDJSON requests.
type LinesProcess struct {
	cmd        *exec.Cmd
	executable string
	ctx        context.Context
	stdin      io.WriteCloser
	stdout     io.ReadCloser // kolk's end; closed by Close when a grandchild keeps the other alive
	lines      chan []byte
	stop       chan struct{}
	// writes carries outbound lines to the single writer goroutine. Buffered so
	// a Send does not wait on a child that is busy answering the last one.
	writes chan []byte
	// exited closes once the reader has finished and exitErr is final, so the
	// terminal result can be observed any number of times from any caller.
	exited  chan struct{}
	exitErr error
	// hardExit records that the child was killed rather than allowed to finish.
	// Written before exited closes, so any reader that has observed the close
	// sees it.
	hardExit bool
	once     sync.Once
}

// HardExit reports that the child was terminated by a signal that left its work
// unfinished — anything but SIGINT, which the vendor answers by completing the
// turn. A caller that resumes a conversation after this is asking the vendor to
// continue a turn nobody is waiting for. False until the child has actually
// exited, because "not dead" is not "died gracefully".
func (p *LinesProcess) HardExit() bool {
	if p == nil {
		return false
	}
	select {
	case <-p.exited:
		return p.hardExit
	default:
		return false
	}
}

// StartLinesProcess starts an executable without exposing its process details
// to provider adapters. The caller owns Close and must drain responses with
// Next.
func StartLinesProcess(ctx context.Context, executable string, args []string) (*LinesProcess, error) {
	return StartLinesProcessWithOptions(ctx, executable, args, ProcessOptions{})
}

// StartLinesProcessWithOptions is StartLinesProcess with an explicit process
// working directory. The directory is validated and canonicalized before the
// child is started, so a provider never silently falls back to the parent's
// directory after a bad capability handoff.
func StartLinesProcessWithOptions(ctx context.Context, executable string, args []string, options ProcessOptions) (*LinesProcess, error) {
	options, err := normalizeProcessOptions(options)
	if err != nil {
		return nil, err
	}
	path, err := LookPath(executable)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = options.Dir
	// Provider-owned CLIs are still untrusted from Kolkrabbi's perspective:
	// they can run vendor tools and report output back into the session. Keep
	// the normal environment, but do not give them Kolkrabbi's ambient keys.
	cmd.Env = inheritedEnv(nil)
	// Created before Start so the cancel ladder can observe the child leaving
	// and stop climbing; read() closes it once the exit status is final.
	exited := make(chan struct{})
	groupChild(cmd, exited)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening %s stdin: %w", executable, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("opening %s stdout: %w", executable, err)
	}
	var stderr stderrRing
	cmd.Stderr = &stderr
	// Wait must not hang on a pipe a grandchild kept: after the child has
	// exited, os/exec gives the copying goroutines this long, then closes the
	// pipes itself. The same grace the one-shot runner uses (V34.2a).
	cmd.WaitDelay = outputDrainTimeout
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("starting %s: %w", executable, err)
	}
	process := &LinesProcess{
		cmd: cmd, executable: executable, ctx: ctx, stdin: stdin, stdout: stdout, lines: make(chan []byte),
		stop: make(chan struct{}), exited: exited,
		writes: make(chan []byte, 8),
	}
	go process.read(stdout, &stderr)
	go process.write()
	return process, nil
}

func (p *LinesProcess) read(stdout io.Reader, stderr *stderrRing) {
	defer close(p.exited)
	defer close(p.lines)
	var err error
	if readErr := readProviderLines(stdout, func(line []byte) error {
		select {
		case p.lines <- line:
			return nil
		case <-p.stop:
			return errLinesProcessClosed
		case <-p.ctx.Done():
			return p.ctx.Err()
		}
	}); readErr != nil {
		var waitErr error
		if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
			// CommandContext invokes cmd.Cancel when ctx ends. For a provider
			// process that is the SIGINT -> SIGTERM -> SIGKILL ladder above;
			// killing here would race that ladder and can skip SIGINT entirely.
			// Wait lets the ladder own escalation while still guaranteeing a
			// reap before the process becomes observable as exited.
			waitErr = p.cmd.Wait()
		} else {
			// Reap the child on every other reader failure. In particular, a
			// provider that exceeded the line bound must not become a zombie
			// while its descendants keep the pipe alive.
			_ = killChild(p.cmd)
			waitErr = p.cmd.Wait()
		}
		p.hardExit = exitedHard(waitErr)
		// A read that failed because Close took the pipe away is the close
		// working, not a fault to report.
		select {
		case <-p.stop:
			readErr = errLinesProcessClosed
		default:
		}
		if !errors.Is(readErr, errLinesProcessClosed) {
			err = fmt.Errorf("reading %s output: %w", p.executable, readErr)
		}
	} else if waitErr := p.cmd.Wait(); waitErr != nil {
		p.hardExit = exitedHard(waitErr)
		if stderr.Len() > 0 {
			err = fmt.Errorf("provider process exited unsuccessfully: %s: %w", stderr.String(), waitErr)
		} else {
			err = waitErr
		}
	}
	p.exitErr = err
}

// Send hands one line to the provider process.
//
// The write is asynchronous, and a failed write is deliberately never returned
// to the caller. A prompt is routinely larger than the 64 KiB pipe buffer, so a
// synchronous write to a child that has already died blocks and then fails with
// EPIPE — and reporting that makes "broken pipe" the diagnosis while the
// child's real explanation ("unknown flag --nope") sits unread in stderr. The
// write error may never outrank the exit code and stderr, and the surest way to
// guarantee that is for it never to become an error here at all: the reader
// owns diagnosis, and a child that did not take this prompt is a child whose
// exit already says why.
func (p *LinesProcess) Send(line []byte) error {
	if p == nil || p.stdin == nil {
		return fmt.Errorf("provider process is not running")
	}
	payload := append(append([]byte(nil), line...), '\n')
	select {
	case p.writes <- payload:
	case <-p.exited:
		// The child is already gone. Dropping the line is right: Next is about
		// to report why, and that report is worth more than this one.
	}
	return nil
}

// write is the single writer, so lines reach a line-delimited protocol in the
// order they were sent. One goroutine per Send would not guarantee that.
func (p *LinesProcess) write() {
	for {
		select {
		case payload := <-p.writes:
			if _, err := p.stdin.Write(payload); err != nil {
				return // the reader owns diagnosis; see Send
			}
		case <-p.exited:
			return
		}
	}
}

// Next waits for the next output line or process termination.
func (p *LinesProcess) Next(ctx context.Context) ([]byte, error) {
	select {
	case line, ok := <-p.lines:
		if ok {
			return line, nil
		}
		<-p.exited
		if p.exitErr != nil {
			return nil, p.exitErr
		}
		// A caller loop must be able to tell "no more lines" from "keep going".
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes stdin and waits for the provider process to terminate.
func (p *LinesProcess) Close() error {
	if p == nil {
		return nil
	}
	p.once.Do(func() {
		close(p.stop)
		_ = p.stdin.Close()
	})
	// A provider CLI is expected to exit once its stdin closes. If one does not,
	// Kolkrabbi still owns the process and must not hang the session on exit.
	timer := time.NewTimer(closeGrace)
	defer timer.Stop()
	select {
	case <-p.exited:
		return p.exitErr
	case <-timer.C:
	}
	// The group, not the leader: a provider that will not exit on stdin close
	// is exactly the one likely to be sitting on a running tool. Then one drain
	// period for the reader to come back. If it still has not, it is parked in
	// a read on a pipe something outside the group kept open; closing kolk's
	// end returns that read, the reader Waits, and Wait is bounded by
	// WaitDelay. Close therefore returns within closeGrace + 2*outputDrainTimeout
	// whatever a grandchild does (V34.2a).
	if p.cmd.Process != nil {
		_ = killChild(p.cmd)
	}
	drain := time.NewTimer(outputDrainTimeout)
	defer drain.Stop()
	select {
	case <-p.exited:
	case <-drain.C:
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		<-p.exited
	}
	return p.exitErr
}
