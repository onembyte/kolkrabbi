package shell

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// LinesProcess is one long-lived child process with line-delimited stdin and
// stdout. It is suitable for a provider session that accepts NDJSON requests.
type LinesProcess struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan []byte
	done  chan error
	once  sync.Once
}

// StartLinesProcess starts an executable without exposing its process details
// to provider adapters. The caller owns Close and must drain responses with
// Next.
func StartLinesProcess(ctx context.Context, executable string, args []string) (*LinesProcess, error) {
	path, err := LookPath(executable)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening %s stdin: %w", executable, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("opening %s stdout: %w", executable, err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("starting %s: %w", executable, err)
	}
	process := &LinesProcess{
		cmd: cmd, stdin: stdin, lines: make(chan []byte), done: make(chan error, 1),
	}
	go process.read(stdout, &stderr)
	return process, nil
}

func (p *LinesProcess) read(stdout io.Reader, stderr *bytes.Buffer) {
	defer close(p.lines)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		p.lines <- append([]byte(nil), scanner.Bytes()...)
	}
	var err error
	if scanErr := scanner.Err(); scanErr != nil {
		err = scanErr
	} else if waitErr := p.cmd.Wait(); waitErr != nil {
		if stderr.Len() > 0 {
			err = fmt.Errorf("provider process exited unsuccessfully: %s: %w", stderr.String(), waitErr)
		} else {
			err = waitErr
		}
	}
	p.done <- err
}

// Send writes one line to the provider process.
func (p *LinesProcess) Send(line []byte) error {
	if p == nil || p.stdin == nil {
		return fmt.Errorf("provider process is not running")
	}
	if _, err := p.stdin.Write(append(append([]byte(nil), line...), '\n')); err != nil {
		return fmt.Errorf("writing provider request: %w", err)
	}
	return nil
}

// Next waits for the next output line or process termination.
func (p *LinesProcess) Next(ctx context.Context) ([]byte, error) {
	select {
	case line, ok := <-p.lines:
		if ok {
			return line, nil
		}
		return nil, <-p.done
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes stdin and waits for the provider process to terminate.
func (p *LinesProcess) Close() error {
	if p == nil {
		return nil
	}
	p.once.Do(func() { _ = p.stdin.Close() })
	return <-p.done
}
