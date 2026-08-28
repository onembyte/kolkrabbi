//go:build !darwin && !linux && !windows

package term

import "os"

// resizeNotifier has no signal to offer outside the supported matrix. A nil
// channel never fires, so the screen simply keeps its startup size.
func resizeNotifier(*os.File) (<-chan struct{}, func()) {
	return nil, func() {}
}
