package shell

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

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
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", executable, err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := onLine(append([]byte(nil), scanner.Bytes()...)); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
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
