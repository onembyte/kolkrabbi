package shell

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// The Landlock enforcer has to run in the child, after fork and before exec,
// and Go has no pre-exec hook. So the child is kolk itself, re-executed in
// front of the command, told what to do by two environment variables rather
// than an argv verb: the outside-a-session surface is four commands and no
// more, and this is not a fifth.
//
// The policy travels as JSON. It holds paths and a network word, nothing
// secret. The child strips both variables before it execs the command, or a
// `kolk` run inside the sandbox would believe it is the child.
const (
	landlockChildEnv  = "KOLK_LANDLOCK_CHILD"
	landlockPolicyEnv = "KOLK_LANDLOCK_POLICY"
)

func encodeLandlockPolicy(p Sandbox) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("encoding sandbox policy: %w", err)
	}
	return string(b), nil
}

func decodeLandlockPolicy(s string) (Sandbox, error) {
	var p Sandbox
	if strings.TrimSpace(s) == "" {
		return p, fmt.Errorf("the sandbox policy is empty")
	}
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return p, fmt.Errorf("decoding sandbox policy: %w", err)
	}
	return p, nil
}

// stripLandlockEnv removes exactly the two trigger variables and nothing else.
func stripLandlockEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, landlockChildEnv+"=") || strings.HasPrefix(kv, landlockPolicyEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// landlockArgv is the wrapper's shape: kolk itself, then the command unchanged.
func landlockArgv(self string, argv []string) []string {
	return append([]string{self}, argv...)
}

// MaybeRunAsLandlockChild is the internal entry, checked by the CLI before it
// builds anything. With the variable unset it does nothing and reports so. With
// it set, this process is the confined child: it applies the ruleset and
// replaces itself with the command, and never returns to the CLI.
//
// Exit codes: 125 when confinement could not be established (the command did
// not run), 126 when it was established but the command could not be executed.
func MaybeRunAsLandlockChild(args []string, stderr io.Writer) (handled bool, code int) {
	if os.Getenv(landlockChildEnv) != "1" {
		return false, 0
	}
	return true, landlockChildMain(args, stderr)
}
