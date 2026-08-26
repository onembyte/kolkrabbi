package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolvePath turns whatever a model produced into an absolute path and reports
// whether it lands outside the project root.
//
// Symlinks are resolved before the comparison. Checking the literal path would
// call a link that lives inside the root but points out of it "inside", which
// is a hole straight through the confinement.
//
// The target usually does not exist yet — write_file creates files — so the
// deepest existing ancestor is what gets resolved, and the remaining segments
// are appended after. An empty root disables confinement entirely.
func resolvePath(root, path string) (string, bool, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("resolving %s: %w", path, err)
	}
	absolute = realPath(absolute)
	if strings.TrimSpace(root) == "" {
		return absolute, false, nil
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false, fmt.Errorf("resolving the project root %s: %w", root, err)
	}
	absoluteRoot = realPath(absoluteRoot)

	return absolute, !within(absoluteRoot, absolute), nil
}

// within answers a question rather than reporting a failure: two paths that
// cannot be related at all — different volumes on Windows — are simply not
// within one another.
func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// realPath resolves symlinks as far down the path as it can, then re-attaches
// the parts that do not exist yet.
func realPath(absolute string) string {
	remainder := ""
	current := filepath.Clean(absolute)
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(absolute)
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
}

// describePath renders a path for a person: relative to the root when it is
// inside, absolute when it is not, so an escape is visible at a glance.
func describePath(root, absolute string, outside bool) string {
	if outside || strings.TrimSpace(root) == "" {
		return absolute
	}
	if relative, err := filepath.Rel(root, absolute); err == nil {
		return relative
	}
	return absolute
}
