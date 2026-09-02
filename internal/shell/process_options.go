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

func normalizeProcessOptions(options ProcessOptions) (ProcessOptions, error) {
	if options.Dir == "" {
		return options, nil
	}
	if !filepath.IsAbs(options.Dir) {
		return ProcessOptions{}, fmt.Errorf("process working directory must be absolute: %q", options.Dir)
	}
	resolved, err := filepath.EvalSymlinks(options.Dir)
	if err != nil {
		return ProcessOptions{}, fmt.Errorf("resolving process working directory %q: %w", options.Dir, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return ProcessOptions{}, fmt.Errorf("checking process working directory %q: %w", options.Dir, err)
	}
	if !info.IsDir() {
		return ProcessOptions{}, fmt.Errorf("process working directory is not a directory: %q", options.Dir)
	}
	options.Dir = resolved
	return options, nil
}
