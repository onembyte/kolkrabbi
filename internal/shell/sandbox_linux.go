//go:build linux

package shell

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// x/sys ships Landlock's constants and attr structs but not the three syscall
// wrappers, so the calls are raw. The numbers are the same on every linux arch.

// landlockABI asks the kernel which Landlock ABI it speaks. 0 with an error
// means none: too old (< 5.13), or built without the LSM, or it is not in the
// active lsm= list.
func landlockABI() (int, error) {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, uintptr(unix.LANDLOCK_CREATE_RULESET_VERSION))
	switch {
	case errno == 0:
		return int(abi), nil
	case errors.Is(errno, unix.ENOSYS), errors.Is(errno, unix.EOPNOTSUPP):
		return 0, fmt.Errorf("Landlock is not available on this kernel (%v): it needs Linux 5.13 or newer with the Landlock LSM enabled", errno)
	default:
		return 0, fmt.Errorf("Landlock probe failed: %v", errno)
	}
}

func probeMechanism() (string, error) {
	abi, err := landlockABI()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("landlock v%d", abi), nil
}

// prepareSandbox re-executes kolk in front of the command and hands it the
// policy in the environment. Nothing is enforced in this process; the child
// does that, after fork and before exec, where Go cannot otherwise reach.
func prepareSandbox(p Sandbox) (*sandboxWrap, error) {
	self, err := SelfPath()
	if err != nil {
		return nil, fmt.Errorf("locating kolk for the sandbox child: %w", err)
	}
	policy, err := encodeLandlockPolicy(p)
	if err != nil {
		return nil, err
	}
	return &sandboxWrap{
		Argv: func(argv []string) []string { return landlockArgv(self, argv) },
		Env:  []string{landlockChildEnv + "=1", landlockPolicyEnv + "=" + policy},
	}, nil
}

// landlockChildMain is this process as the confined child. It applies the
// ruleset, strips its own trigger from the environment, and becomes the
// command. Any failure before exec is a refusal: the command does not run.
func landlockChildMain(args []string, stderr io.Writer) int {
	policy, err := decodeLandlockPolicy(os.Getenv(landlockPolicyEnv))
	if err != nil {
		fmt.Fprintf(stderr, "kolk: sandbox child: %v. The command was not run.\n", err)
		return 125
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "kolk: sandbox child: no command to run.")
		return 125
	}
	if err := applyLandlock(policy); err != nil {
		fmt.Fprintf(stderr, "kolk: sandbox child: %v. The command was not run.\n", err)
		return 125
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "kolk: sandbox child: %v\n", err)
		return 126
	}
	if err := syscall.Exec(path, args, stripLandlockEnv(os.Environ())); err != nil {
		fmt.Fprintf(stderr, "kolk: sandbox child: exec %s: %v\n", path, err)
		return 126
	}
	return 0 // unreachable: Exec does not return on success
}

// applyLandlock builds and applies the ruleset. V34.1e.2a ships it as a
// refusal on purpose: a child with no rules that execs the command anyway is
// a sandbox that quietly is not one. V34.1e.2b replaces this body.
func applyLandlock(Sandbox) error {
	return errors.New("the Landlock ruleset is not implemented yet (V34.1e.2b); refusing rather than running unconfined")
}
