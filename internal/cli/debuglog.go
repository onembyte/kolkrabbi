package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/redact"
)

// debugLog is the file `--debug` writes.
//
// It records what the event stream cannot. Every session already writes a
// scrubbed NDJSON log of protocol events — tool calls, permissions, message
// completions — so duplicating those here would be a second copy of the same
// facts. This file is for the things that have no event: which model was
// chosen and why, what the effort dial resolved to, where the key came from,
// what a retry decided. Diagnostics for whoever maintains kolk, not a record
// for a client to replay.
//
// Two rules hold it. **Off unless asked for**, because a diagnostic that writes
// itself on every run is a second copy of the session that nobody chose to
// keep. And **every line is scrubbed on the way in**, not on the way out and
// not by its caller: a debug log is the single most likely place for a key to
// reach a public issue, and a scrubber a caller can forget is a scrubber that
// will be forgotten.
type debugLog struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// openDebugLog creates the file, replacing any earlier log for the same
// session: two runs of one session id are two attempts at the same problem, and
// the second is the one being diagnosed.
func openDebugLog(path string) (*debugLog, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &debugLog{file: file, path: path}, nil
}

// Printf writes one scrubbed, timestamped line.
//
// A nil *debugLog is the "off" state and every method tolerates it, so no call
// site needs a branch — which is what keeps `--debug` from growing an `if`
// beside every interesting line and then losing one.
func (d *debugLog) Printf(format string, args ...any) {
	if d == nil || d.file == nil {
		return
	}
	line := redact.Scrub(fmt.Sprintf(format, args...))
	line = strings.ReplaceAll(line, "\n", "\n    ")

	d.mu.Lock()
	defer d.mu.Unlock()
	_, _ = fmt.Fprintf(d.file, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
}

// Path is where the file is, so a session can name it once at the end.
func (d *debugLog) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

func (d *debugLog) Close() error {
	if d == nil || d.file == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	err := d.file.Close()
	d.file = nil
	return err
}
