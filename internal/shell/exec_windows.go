//go:build windows

package shell

import (
	"context"
	"os"
	"os/exec"
	"sync"
)

// Windows is advisory until migration step 13. This is a real implementation
// rather than a stub, because the choice of interpreter changes what commands
// a model may safely emit — which is a product decision, not a porting detail.

// interpreter is resolved once. PowerShell is preferred because its command
// syntax is closer to what a model trained on Unix shells will produce, and
// because cmd.exe's quoting rules cannot express a command containing both
// kinds of quote at all.
var interpreter = sync.OnceValue(func() [2]string {
	// pwsh is PowerShell 7+: cross-platform, and present on developer machines.
	if p, err := exec.LookPath("pwsh"); err == nil {
		return [2]string{p, "pwsh"}
	}
	// powershell.exe is Windows PowerShell 5.1, shipped with every Windows.
	if p, err := exec.LookPath("powershell"); err == nil {
		return [2]string{p, "powershell"}
	}
	// cmd.exe always exists. It is the floor, not a choice.
	return [2]string{os.Getenv("ComSpec"), "cmd"}
})

func interpreterName() string { return interpreter()[1] }

func command(ctx context.Context, c Cmd) (*exec.Cmd, error) {
	path, kind := interpreter()[0], interpreter()[1]

	var cmd *exec.Cmd
	if kind == "cmd" {
		if path == "" {
			path = "cmd.exe"
		}
		cmd = exec.CommandContext(ctx, path, "/c", c.Command)
	} else {
		// -NoProfile: a developer's PowerShell profile can print banners, set
		// aliases, or take a second to load, and none of that belongs in the
		// output an agent reads back as a tool result.
		// -NonInteractive: a prompt an agent cannot answer must fail, not hang.
		cmd = exec.CommandContext(ctx, path, "-NoProfile", "-NonInteractive", "-Command", c.Command)
	}

	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}

	// Process-group teardown on Windows needs a job object, which is step 13
	// work. Until then a cancelled command kills the interpreter but may leave
	// a grandchild running — which is honest, and recorded here rather than
	// silently pretended away.
	return cmd, nil
}
