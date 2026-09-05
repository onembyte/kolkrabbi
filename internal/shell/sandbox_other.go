//go:build !darwin

package shell

// No enforcer here yet. V34.1e.2 adds Landlock for linux and narrows this tag to
// !darwin && !linux; until then every sandboxed command on these platforms is
// refused, which is what fail-closed means.
func probeMechanism() (string, error) { return "", ErrSandboxUnsupported }

func prepareSandbox(Sandbox) (sandboxWrap, error) { return nil, ErrSandboxUnsupported }
