//go:build !windows

package mockagent

import (
	"fmt"
	"os"
	"path/filepath"
)

// scripts are POSIX sh, kept deliberately small. `while ...; do sleep 0.05;
// done` rather than a bare `wait`, because a trap in sh runs only when the
// foreground command returns: a long sleep would delay the handler past the
// grace being measured and make the test about sleep granularity.
var scripts = map[Kind]string{
	ExitsOnInterrupt: `#!/bin/sh
trap 'printf "INT\n" >> "$MOCKAGENT_LOG"; exit 0' INT
printf "ready\n"
while true; do sleep 0.05; done
`,
	IgnoresInterrupt: `#!/bin/sh
trap 'printf "INT\n" >> "$MOCKAGENT_LOG"' INT
trap 'printf "TERM\n" >> "$MOCKAGENT_LOG"; exit 0' TERM
printf "ready\n"
while true; do sleep 0.05; done
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
