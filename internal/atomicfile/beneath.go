package atomicfile

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// errEscape is the reason WriteBeneath refuses: the path does not stay inside
// the root once links are taken into account.
var errEscape = errors.New("path escapes the root")

// beneathRel is the lexical half of the check: both paths absolute, the
// target strictly inside the root. The other half -- what the path resolves to
// on disk, right now, component by component -- is the platform's job.
func beneathRel(root, path string) ([]string, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: %s and %s must both be absolute", errEscape, root, path)
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not under %s: %w", errEscape, path, root, err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: %s is not beneath %s", errEscape, path, root)
	}
	return strings.Split(rel, string(filepath.Separator)), nil
}
