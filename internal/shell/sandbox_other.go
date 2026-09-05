//go:build !darwin && !linux

package shell

// No enforcer here. Every sandboxed command on these platforms is refused,
// which is what fail-closed means; windows is outside plan 13's matrix.
func probeMechanism() (string, error) { return "", ErrSandboxUnsupported }

func prepareSandbox(Sandbox) (*sandboxWrap, error) { return nil, ErrSandboxUnsupported }

func networkDenySupported() (bool, string) { return false, ErrSandboxUnsupported.Error() }
