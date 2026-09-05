package shell

import (
	"errors"
	"fmt"
	"path/filepath"
)

// NetworkPolicy is what a sandbox does about the network. It is decided
// upstream -- plan 13 §7.1 for a delegated child, `allow` for the user's own
// bash tool -- and enforced here, or refused when it cannot be.
type NetworkPolicy string

// NetworkDeny arrives with the enforcement that needs it (V34.1e.3); an
// exported constant nothing reads is the kind of promise this tree refuses.
const NetworkAllow NetworkPolicy = "allow"

// Sandbox is the one policy every enforcer reads (plan 13 §7.2). Writes are
// allowed under Root and Temp and nowhere else; reads are allowed everywhere
// except Deny, which is the hardline credential list the blocklist used to
// string-match; Network is allow or deny.
//
// Root is tools.Options.Root -- the same value the path jail uses. There is
// deliberately no second setting for it that could drift from the first.
type Sandbox struct {
	Root    string
	Temp    string
	Deny    []string
	Network NetworkPolicy
}

// ErrSandboxUnsupported is the reason on a platform with no enforcer.
var ErrSandboxUnsupported = errors.New("no sandbox mechanism is available on this platform")

// mechanism reports what would enforce a Sandbox on this machine -- "seatbelt",
// "landlock v4" -- or why nothing can. It is a package variable so a test can
// stand in for the platform; production only ever reads it.
//
// V34.1e.0 ships the probe as unsupported everywhere. V34.1e.1 and .2 replace
// it per platform behind build tags; until then every sandboxed command is
// refused, which is the fail-closed behaviour the plan asks for.
var mechanism = probeMechanism

func probeMechanism() (string, error) { return "", ErrSandboxUnsupported }

// Mechanism is the read-only view the CLI uses to answer `/sandbox on`.
func Mechanism() (string, error) { return mechanism() }

// overrideMechanism swaps the probe for a test and returns the restore.
func overrideMechanism(probe func() (string, error)) (restore func()) {
	previous := mechanism
	mechanism = probe
	return func() { mechanism = previous }
}

// Refusal is the exact text a user sees when a sandboxed command cannot run.
// It names the missing capability and the one switch that changes the outcome,
// and it never pretends the command ran.
func Refusal(reason error) string {
	return fmt.Sprintf("the sandbox could not be established: %v.\n"+
		"Commands will not run unconfined while the sandbox is on. To run them anyway: /sandbox off", reason)
}

// CredentialDenylist is plan 13 §3's hardline paths as filesystem rules: the
// places a command may never read, whatever else the policy allows.
func CredentialDenylist(home, credentialsFile string) []string {
	return []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".gnupg"),
		credentialsFile,
	}
}
