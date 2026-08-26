//go:build darwin || linux

package diskspace

import "syscall"

// Free reports space available to an unprivileged writer, which is the
// number that decides whether a pull can succeed. Bfree counts blocks reserved
// for root as well, and would promise space Kolkrabbi cannot use.
func Free(path string) (uint64, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, false
	}
	if stat.Bsize <= 0 {
		return 0, false
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), true
}
