package shell

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// SecretSpawner runs a credential-store helper such as /usr/bin/security the
// way plan 05 §3.6 requires: a fixed argv, a cleared environment (only PATH
// and HOME, which the helper needs to find its keychain), a real stdin pipe
// carrying the command so no secret sits in argv, and its own session so no
// code path inside can reach the user's terminal. The caller's context is
// the deadline. It is the keystore's Spawner port; os/exec lives here.
type SecretSpawner struct{}

// LookPath finds the helper on this machine's PATH.
func (SecretSpawner) LookPath(name string) (string, error) { return exec.LookPath(name) }

// Run executes argv with stdin and returns stdout, the exit code when the
// process ran, and an error only when it could not run or the context ended.
func (SecretSpawner) Run(ctx context.Context, argv []string, stdin string) ([]byte, int, error) {
	if len(argv) == 0 {
		return nil, -1, errors.New("shell: empty argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = secretHelperEnv()
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = secretProcAttr()
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, -1, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.Bytes(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return nil, -1, err
	}
	return stdout.Bytes(), 0, nil
}
