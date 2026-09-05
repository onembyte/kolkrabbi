//go:build darwin

package shell

import (
	"fmt"
	"os"
	"strings"
)

// sandboxExecPath is where Apple ships Seatbelt's command-line front end. It is
// a variable so a test can point it at nothing and watch the probe fail closed.
var sandboxExecPath = "/usr/bin/sandbox-exec"

// probeMechanism fails closed: no binary, no sandbox, and the error names the
// path so the refusal upstream can too.
func probeMechanism() (string, error) {
	info, err := os.Stat(sandboxExecPath)
	if err != nil {
		return "", fmt.Errorf("%s is not present", sandboxExecPath)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", sandboxExecPath)
	}
	return "seatbelt", nil
}

// prepareSandbox turns the policy into an argv wrapper. The profile travels
// inline (-p) rather than through a 0600 file: there is nothing secret in it --
// only paths -- and a file would need a lifetime, a cleanup, and a race with
// the process that reads it. An argument has none of those.
func prepareSandbox(p Sandbox) (*sandboxWrap, error) {
	profile, err := seatbeltProfile(p)
	if err != nil {
		return nil, err
	}
	return &sandboxWrap{Argv: func(argv []string) []string {
		return append([]string{sandboxExecPath, "-p", profile}, argv...)
	}}, nil
}

// seatbeltProfile renders the policy as SBPL. Two properties of Seatbelt shape
// it. Rules are matched on the real path, so every path is resolved through
// its symlinks first -- /tmp is /private/tmp, and a profile that names the
// unresolved path matches nothing. And the LAST matching rule wins, so the
// denylist is written after the broad allows, which is what lets ~/.ssh stay
// refused even when the root has been widened to the whole home directory.
func seatbeltProfile(p Sandbox) (string, error) {
	root, err := existingRealPath("root", p.Root)
	if err != nil {
		return "", err
	}
	if p.Temp == "" {
		return "", fmt.Errorf("sandbox policy has no temp directory")
	}
	if err := os.MkdirAll(p.Temp, 0o700); err != nil {
		return "", fmt.Errorf("sandbox temp %s: %w", p.Temp, err)
	}
	temp, err := existingRealPath("temp", p.Temp)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n")
	// A shell has to be able to start things, see itself, and read the machine.
	b.WriteString("(allow process-exec)\n(allow process-fork)\n")
	b.WriteString("(allow process-info* (target same-sandbox))\n(allow signal (target same-sandbox))\n")
	b.WriteString("(allow sysctl-read)\n(allow mach-lookup)\n(allow ipc-posix*)\n")
	// Reads everywhere: toolchains live in /usr, /opt and the home directory,
	// and a build that cannot read its compiler is not a sandbox, it is a wall.
	b.WriteString("(allow file-read*)\n")
	// Writes: the root, the temp, and the toolchain caches the policy names.
	fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplString(root))
	fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplString(temp))
	for _, w := range p.Writable {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplString(bestRealPath(w)))
	}
	b.WriteString(`(allow file-write* (literal "/dev/null") (literal "/dev/zero") (literal "/dev/stdout") (literal "/dev/stderr") (literal "/dev/tty"))` + "\n")
	// Network is one word in the policy and one line here. `network*` covers
	// outbound, inbound and bind; a shell still starts under the deny.
	if p.Network == NetworkDeny {
		b.WriteString("(deny network*)\n")
	} else {
		b.WriteString("(allow network*)\n")
	}
	// Last, so they win: the hardline credential paths, read and write.
	for _, d := range p.Deny {
		q := sbplString(bestRealPath(d))
		fmt.Fprintf(&b, "(deny file-read* (subpath %s))\n(deny file-write* (subpath %s))\n", q, q)
	}
	return b.String(), nil
}

// sbplString quotes a path for SBPL: double quotes, with the two characters
// that would end or escape the string escaped.
func sbplString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
