package shell

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// Provider CLI output is line-delimited JSON, but a tool result can put a
// large amount of text on one physical line. One MiB was an arbitrary
// Scanner ceiling, not a protocol limit, and it made a valid Codex turn fail
// with bufio.ErrTooLong. Keep a finite bound so a broken or hostile provider
// cannot make Kolkrabbi retain an unbounded line while allowing the large tool
// results these CLIs actually emit.
const maxProviderLineBytes = 16 * 1024 * 1024

var errProviderLineTooLarge = errors.New("provider output line exceeds 16 MiB")

// readProviderLine reads one provider line without Scanner's fixed token
// ceiling or ReadString's unbounded allocation. A final unterminated line is
// accepted for compatibility with Scanner's ScanLines split function.
func readProviderLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		switch {
		case err == nil:
			payload := fragment[:len(fragment)-1]
			if len(line)+len(payload) > maxProviderLineBytes {
				return nil, errProviderLineTooLarge
			}
			line = append(line, payload...)
			if n := len(line); n > 0 && line[n-1] == '\r' {
				line = line[:n-1]
			}
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			if len(line)+len(fragment) > maxProviderLineBytes {
				return nil, errProviderLineTooLarge
			}
			line = append(line, fragment...)
		case errors.Is(err, io.EOF):
			if len(line) == 0 && len(fragment) == 0 {
				return nil, io.EOF
			}
			if len(line)+len(fragment) > maxProviderLineBytes {
				return nil, errProviderLineTooLarge
			}
			line = append(line, fragment...)
			if n := len(line); n > 0 && line[n-1] == '\r' {
				line = line[:n-1]
			}
			return line, nil
		default:
			return nil, err
		}
	}
}

func readProviderLines(stdout io.Reader, onLine func([]byte) error) error {
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := readProviderLine(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := onLine(line); err != nil {
			return err
		}
	}
}

// RunLines runs an executable with direct stdin and delivers stdout one line
// at a time. It is intended for provider-owned NDJSON protocols.
func RunLines(ctx context.Context, executable string, args []string, stdin io.Reader, onLine func([]byte) error) error {
	path, err := LookPath(executable)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("opening %s stdout: %w", executable, err)
	}
	exited := make(chan struct{})
	groupChild(cmd, exited)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", executable, err)
	}
	defer close(exited)
	if err := readProviderLines(stdout, onLine); err != nil {
		_ = killChild(cmd)
		_ = cmd.Wait()
		return fmt.Errorf("reading %s output: %w", executable, err)
	}
	if err := cmd.Wait(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%s exited unsuccessfully: %s: %w", executable, stderr.String(), err)
		}
		return fmt.Errorf("%s exited unsuccessfully: %w", executable, err)
	}
	return nil
}
