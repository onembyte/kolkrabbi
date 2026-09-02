package shell

import (
	"fmt"
	"os"
	"path/filepath"
)

// ProcessOptions describes the small set of execution properties that a
// provider process may receive from its caller. An empty Dir preserves the
// caller's current working directory.
//
// Environment is deliberately not an option: provider processes always get
// the scrubbed inherited environment from this package. Callers must not be
// able to accidentally reintroduce ambient credentials through a convenience
// field.
type ProcessOptions struct {
	Dir string
}

// VerifiedDir is the one implementation of "this path names a real directory,
// canonically".
//
// Absolute, symlinks resolved, exists, and is a directory — four checks that
// were hand-copied three times (here, the provider execution envelope, and the
// CLI's project root) with three wordings for the same four failures. One copy
// drifts eventually, and the one that drifts is the one enforcing a boundary.
//
// The label is the caller's noun, so an error still says which directory the
// user got wrong: "workspace", "project workspace", "process working
// directory". Sharing the logic does not mean sharing the sentence.
func VerifiedDir(label, dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%s must be absolute: %q", label, dir)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolving %s %q: %w", label, dir, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("checking %s %q: %w", label, dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory: %q", label, dir)
	}
	return resolved, nil
}

func normalizeProcessOptions(options ProcessOptions) (ProcessOptions, error) {
	if options.Dir == "" {
		return options, nil
	}
	resolved, err := VerifiedDir("process working directory", options.Dir)
	if err != nil {
		return ProcessOptions{}, err
	}
	options.Dir = resolved
	return options, nil
}
