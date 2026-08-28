//go:build darwin || linux

package term

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// resizeNotifier delivers SIGWINCH. The kernel sends it to the foreground
// process group whenever the controlling terminal's window changes, which is
// the one place a resize is reported without polling.
func resizeNotifier(*os.File) (<-chan struct{}, func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	changes := make(chan struct{}, 1)
	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-signals:
				select {
				case changes <- struct{}{}:
				default: // one is already waiting; it will repaint with the latest size
				}
			case <-done:
				return
			}
		}
	}()
	stop := func() {
		once.Do(func() {
			signal.Stop(signals)
			close(done)
			wg.Wait()
		})
	}
	return changes, stop
}
