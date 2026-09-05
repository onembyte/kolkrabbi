package shell

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Both enforcers match on real paths -- Seatbelt by design, Landlock because a
// rule is attached to an opened path -- so every policy path is resolved
// through its symlinks before it becomes a rule. /tmp is /private/tmp on macOS
// and a profile that names the unresolved path matches nothing.

// existingRealPath resolves a path that must exist. A root that does not is a
// policy that cannot be established, and that is a refusal, not a guess.
func existingRealPath(label, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("sandbox policy has no %s directory", label)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("sandbox %s %s: %w", label, path, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("sandbox %s %s cannot be resolved: %w", label, path, err)
	}
	return real, nil
}

// bestRealPath resolves what exists and cleans what does not. A denylist entry
// for a ~/.gnupg that was never created still deserves its rule.
func bestRealPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return filepath.Clean(abs)
}

// RealPath is bestRealPath for callers outside the package: the path with its
// links resolved where they exist and cleaned where they do not. The checkpoint
// store records it beside every path it backs up, and compares it again before
// a restore, so a link planted in between is a difference rather than a write.
func RealPath(path string) string { return bestRealPath(path) }
