package term

import "os"

// ResizeNotifier reports terminal size changes for the output terminal.
//
// The returned channel receives one value per change, coalesced: a drag that
// fires fifty events lands as one or two receives, never a backlog, because
// the only correct response to any of them is the same — probe Size again and
// repaint. On platforms with no change signal the channel is nil, which a
// select treats as "never", so callers need no special case.
//
// stop releases everything the notifier owns and is safe to call twice. The
// caller owns the notifier for exactly as long as it owns the screen.
func ResizeNotifier(output *os.File) (changes <-chan struct{}, stop func()) {
	return resizeNotifier(output)
}
