//go:build linux

package shell

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

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
		return 0, fmt.Errorf("landlock is not available on this kernel (%w): it needs Linux 5.13 or newer with the Landlock LSM enabled", errno)
	default:
		return 0, fmt.Errorf("landlock probe failed: %w", errno)
	}
}

// landlockABIProbe is landlockABI behind a variable, so a test can stand in
// for a kernel this machine does not have -- an ABI 3 that cannot deny TCP.
var landlockABIProbe = landlockABI

func probeMechanism() (string, error) {
	abi, err := landlockABIProbe()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("landlock v%d", abi), nil
}

// prepareSandbox re-executes kolk in front of the command and hands it the
// policy in the environment. Nothing is enforced in this process; the child
// does that, after fork and before exec, where Go cannot otherwise reach.
func prepareSandbox(p Sandbox) (*sandboxWrap, error) {
	// Validate here, in the parent, exactly as Seatbelt does: an unresolvable
	// root is a policy that cannot be established, and it is refused before
	// any child is forked to discover the same thing at exit 125.
	if _, err := existingRealPath("root", p.Root); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Temp) == "" {
		return nil, fmt.Errorf("sandbox policy has no temp directory")
	}
	if err := os.MkdirAll(p.Temp, 0o700); err != nil {
		return nil, fmt.Errorf("sandbox temp %s: %w", p.Temp, err)
	}
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

// applyLandlock builds the ruleset from the policy and restricts this process
// with it. Everything after this call -- including the execve into bash -- is
// confined, and confinement is inherited by every descendant.
//
// Landlock is allow-only: there is no deny rule, and a rule on a directory
// grants access to everything beneath it. So the read grant cannot simply name
// "/" -- that would include ~/.ssh. Instead grantReads walks from "/" and grants
// each directory whole unless a denylist path lies beneath it, in which case it
// descends and grants the children one by one, skipping the denied ones. Only
// the ancestors of denylist paths are ever enumerated; everything else is one
// rule. The failure mode is over-denying a denylist path's siblings, and
// TestEscape9 exists to catch it.
func applyLandlock(p Sandbox) error {
	abi, err := landlockABIProbe()
	if err != nil {
		return err
	}
	handled := fsAccessForABI(abi)
	attr := unix.LandlockRulesetAttr{Access_fs: handled}
	rs, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %w", errno)
	}
	ruleset := int(rs)
	defer func() { _ = unix.Close(ruleset) }()

	root, err := existingRealPath("root", p.Root)
	if err != nil {
		return err
	}
	if strings.TrimSpace(p.Temp) == "" {
		return fmt.Errorf("sandbox policy has no temp directory")
	}
	if err := os.MkdirAll(p.Temp, 0o700); err != nil {
		return fmt.Errorf("sandbox temp %s: %w", p.Temp, err)
	}
	temp, err := existingRealPath("temp", p.Temp)
	if err != nil {
		return err
	}

	deny := make([]string, 0, len(p.Deny))
	for _, d := range p.Deny {
		deny = append(deny, bestRealPath(d))
	}

	// Reads and execution, everywhere the denylist does not reach.
	readAccess := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR)
	if err := grantTree(ruleset, "/", deny, readAccess, readAccess, 0); err != nil {
		return err
	}

	// Writes: the root, the temp, and the toolchain caches. The caches are
	// created first, because a toolchain that has to create ~/go/pkg/mod would
	// need MAKE_DIR on ~/go/pkg, and that is not a grant we make.
	writable := []string{root, temp}
	for _, w := range p.Writable {
		_ = os.MkdirAll(w, 0o755)
		writable = append(writable, bestRealPath(w))
	}
	// The same tree walk as for reads: a root widened to the whole home must
	// not carry ~/.ssh with it, and Landlock cannot say "except" -- it can
	// only not grant. TestEscape4 is the case.
	for _, w := range writable {
		if err := grantTree(ruleset, w, deny, handled, handled, 0); err != nil {
			return fmt.Errorf("granting writes under %s: %w", w, err)
		}
	}
	// Device files a shell writes to. Missing ones (no tty in CI) are skipped.
	for _, dev := range []string{"/dev/null", "/dev/zero", "/dev/tty"} {
		_ = addRule(ruleset, dev, unix.LANDLOCK_ACCESS_FS_READ_FILE|unix.LANDLOCK_ACCESS_FS_WRITE_FILE, handled)
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_NO_NEW_PRIVS): %w", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(ruleset), 0, 0); errno != 0 {
		return fmt.Errorf("landlock_restrict_self: %w", errno)
	}
	return nil
}

// fsAccessForABI is the filesystem access set the running kernel understands.
// Asking for a bit it does not know is EINVAL, so the set grows with the ABI.
func fsAccessForABI(abi int) uint64 {
	access := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO | unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM)
	if abi >= 2 {
		access |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		access |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 5 {
		access |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	return access
}

// fileOnlyAccess is the subset of access bits that make sense on a regular
// file; a rule on a file carrying a directory bit is EINVAL.
const fileOnlyAccess = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
	unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV

// addRule attaches one path-beneath rule. access is intersected with what the
// ruleset handles and, for a non-directory, with the file-compatible bits.
func addRule(ruleset int, path string, access, handled uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = unix.Close(fd) }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	access &= handled
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		access &= fileOnlyAccess
	}
	if access == 0 {
		return nil
	}
	rule := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(fd)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset),
		uintptr(unix.LANDLOCK_RULE_PATH_BENEATH), uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("landlock_add_rule %s: %w", path, errno)
	}
	return nil
}

// grantTree grants dir whole when nothing denied lies beneath it, and
// otherwise descends one level and grants each child that is not itself
// denied. It is the only way to express "this tree, except that path" in a
// model with no deny rule, and it serves reads and writes alike. Children that
// cannot be opened -- /root, a vanished temp -- are skipped: an access that
// would have failed anyway is not a policy failure. Only depth 0 is fatal.
func grantTree(ruleset int, dir string, deny []string, access, handled uint64, depth int) error {
	if depth > 64 {
		return fmt.Errorf("denylist enumeration too deep under %s", dir)
	}
	for _, d := range deny {
		if d == dir {
			return nil // denied outright: no rule, nothing beneath it either
		}
	}
	if !anyBeneath(deny, dir) {
		if err := addRule(ruleset, dir, access, handled); err != nil {
			if depth == 0 {
				return err
			}
			return nil
		}
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if depth == 0 {
			return fmt.Errorf("reading %s: %w", dir, err)
		}
		return nil
	}
	for _, e := range entries {
		if err := grantTree(ruleset, filepath.Join(dir, e.Name()), deny, access, handled, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// anyBeneath reports whether any denied path lies strictly under dir.
func anyBeneath(deny []string, dir string) bool {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for _, d := range deny {
		if strings.HasPrefix(d, prefix) {
			return true
		}
	}
	return false
}
