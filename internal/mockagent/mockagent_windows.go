//go:build windows

package mockagent

import "fmt"

// Windows is refused rather than faked. These are POSIX sh scripts, and the
// thing they exist to prove — that SIGINT is sent before SIGTERM — has no
// Windows equivalent: `groupChild` there is already a documented no-op waiting
// on A13's job objects. A stub that produced *some* child would make a green
// test on Windows mean something it does not.
func writeFake(string, Kind) (executable, logPath string, err error) {
	return "", "", fmt.Errorf("mockagent: POSIX sh scripts; the signal ladder is not implemented on Windows (see A13)")
}
