// Package diskspace answers how much room is left where Kolkrabbi wants to
// write. It is a platform package because the answer comes from a syscall that
// differs per OS, and an adapter must not carry that divergence itself.
//
// Every implementation reports "unknown" rather than a number it could not
// measure. Callers refuse on unknown, so a confident zero or a guessed total
// would be worse than no answer at all.
package diskspace

import "path/filepath"

// Free reports the space available where the given path will live. A directory
// Kolkrabbi has not created yet must not read as unmeasurable, so a path that
// does not exist is measured at its nearest existing ancestor: that is the
// filesystem it will occupy, which is the number the decision actually needs.
func Free(path string) (uint64, bool) {
	for dir := filepath.Clean(path); ; {
		if bytes, ok := free(dir); ok {
			return bytes, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return 0, false
		}
		dir = parent
	}
}
