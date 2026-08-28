//go:build windows

package term

import (
	"os"
	"sync"
	"time"
)

// resizePollInterval is how often the console size is compared on Windows,
// which has no resize signal a Go program can subscribe to without owning the
// console input queue. A GetConsoleScreenBufferInfo call twice a second is
// invisible next to a keystroke.
const resizePollInterval = 500 * time.Millisecond

func resizeNotifier(output *os.File) (<-chan struct{}, func()) {
	changes := make(chan struct{}, 1)
	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		lastWidth, lastHeight := Size(output)
		ticker := time.NewTicker(resizePollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				width, height := Size(output)
				if width == lastWidth && height == lastHeight {
					continue
				}
				lastWidth, lastHeight = width, height
				select {
				case changes <- struct{}{}:
				default:
				}
			case <-done:
				return
			}
		}
	}()
	stop := func() {
		once.Do(func() {
			close(done)
			wg.Wait()
		})
	}
	return changes, stop
}
