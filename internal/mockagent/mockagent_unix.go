//go:build !windows

package mockagent

import (
	"fmt"
	"os"
	"path/filepath"
)

// scripts are Bash fixtures, kept deliberately small. Each blocks in a `read`
// on its stdin — a shell builtin, so there is no foreground child — rather
// than in a `sleep` child. The explicit interpreter is part of the fixture:
// /bin/sh is Bash on this workstation but Dash on the Ubuntu release runner,
// and their trap/read behavior differs enough to change which rung appears to
// have run. The ladder signals the whole process group, and a child that dies
// of SIGINT under macOS's Bash 3.2 makes the shell re-raise SIGINT on itself
// after the trap runs, so the wait status reads "signalled" even though the
// trap said `exit 0`; Linux's Bash does not. A signal that interrupts `read`
// runs the trap and returns; the `|| sleep` is only for a closed stdin, so the
// loop cannot spin.
var scripts = map[Kind]string{
	ExitsOnInterrupt: `#!/usr/bin/env bash
trap 'printf "INT\n" >> "$MOCKAGENT_LOG"; exit 0' INT
printf "ready\n"
while :; do read -r _ || sleep 0.05; done
`,
	IgnoresInterrupt: `#!/usr/bin/env bash
trap 'printf "INT\n" >> "$MOCKAGENT_LOG"' INT
trap 'printf "TERM\n" >> "$MOCKAGENT_LOG"; exit 0' TERM
printf "ready\n"
while :; do read -r _ || sleep 0.05; done
`,
	// No TERM trap on purpose: the default action kills it, so the wait status
	// is signalled rather than an exit code the script chose. That distinction
	// is the whole point of this one.
	KilledByTerminate: `#!/usr/bin/env bash
trap '' INT
printf "ready\n"
while :; do read -r _ || sleep 0.05; done
`,
	// No traps at all: SIGINT's default action kills it on the first rung, and
	// the wait status is signalled with SIGINT. A vendor that *handles* SIGINT
	// never looks like this.
	KilledByInterrupt: `#!/usr/bin/env bash
printf "ready\n"
while :; do read -r _ || sleep 0.05; done
`,
}

func writeFake(dir string, kind Kind) (executable, logPath string, err error) {
	body, ok := scripts[kind]
	if !ok {
		return "", "", fmt.Errorf("mockagent: no such fake vendor CLI: %q", kind)
	}
	executable = filepath.Join(dir, string(kind))
	if err := os.WriteFile(executable, []byte(body), 0o755); err != nil {
		return "", "", fmt.Errorf("mockagent: writing %s: %w", executable, err)
	}
	return executable, filepath.Join(dir, string(kind)+".signals"), nil
}
