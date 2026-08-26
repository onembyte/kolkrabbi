//go:build !darwin && !linux

package diskspace

// Kolkrabbi has no verified way to measure free space on this platform, and a
// managed local model is not supported here yet. Unknown is the honest answer:
// the fit planner refuses on unknown, so nothing is pulled on a promise that
// was never checked.
func free(string) (uint64, bool) { return 0, false }
